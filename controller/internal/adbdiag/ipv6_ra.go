package adbdiag

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"dropcheck/controller/internal/adb"
)

const ipv6RACommandTimeout = 8 * time.Second

var ipv6RASysctlKeys = []string{
	"accept_ra",
	"accept_ra_defrtr",
	"accept_ra_pinfo",
	"accept_ra_rtr_pref",
	"accept_ra_min_lft",
	"accept_ra_min_hop_limit",
	"accept_ra_from_local",
}

// IPv6RASummary is a best-effort ADB snapshot of host-side IPv6 RA handling.
type IPv6RASummary struct {
	Interface         string
	DefaultRoute      string
	DefaultGateways   []string
	AcceptRA          string
	AcceptRADefrtr    string
	AcceptRAPinfo     string
	AcceptRARtrPref   string
	AcceptRAMinLft    string
	AcceptRAMinHopLft string
	AcceptRAFromLocal string
}

// CollectIPv6RA reads interface-local IPv6 RA knobs and default-route state via adb.
//
// This is supplement-only data: failures are ignored so normal Wi-Fi status
// commands continue to work even when some adb reads are unavailable.
func CollectIPv6RA(ctx context.Context, client adb.Client, iface string) IPv6RASummary {
	if client.Timeout <= 0 {
		client.Timeout = ipv6RACommandTimeout
	}
	iface = strings.TrimSpace(iface)
	if iface == "" {
		return IPv6RASummary{}
	}
	summary := IPv6RASummary{Interface: iface}
	if result, _ := client.Run(ctx, "shell", "ip", "-6", "route", "show", "table", "all"); strings.TrimSpace(result.Stdout) != "" {
		lines := strings.Split(strings.TrimSpace(result.Stdout), "\n")
		present, gateways := summarizeIPv6DefaultRoutes(lines)
		if present {
			summary.DefaultRoute = "present"
		} else {
			summary.DefaultRoute = "missing"
		}
		summary.DefaultGateways = gateways
	}
	for key, value := range collectIPv6RASysctls(ctx, client, iface) {
		switch key {
		case "accept_ra":
			summary.AcceptRA = value
		case "accept_ra_defrtr":
			summary.AcceptRADefrtr = value
		case "accept_ra_pinfo":
			summary.AcceptRAPinfo = value
		case "accept_ra_rtr_pref":
			summary.AcceptRARtrPref = value
		case "accept_ra_min_lft":
			summary.AcceptRAMinLft = value
		case "accept_ra_min_hop_limit":
			summary.AcceptRAMinHopLft = value
		case "accept_ra_from_local":
			summary.AcceptRAFromLocal = value
		}
	}
	if summary.Empty() {
		return IPv6RASummary{}
	}
	return summary
}

// RenderIPv6RASummary renders ADB IPv6 RA state for text output.
func RenderIPv6RASummary(summary IPv6RASummary) string {
	if summary.Empty() {
		return ""
	}
	var b strings.Builder
	b.WriteString("ADB IPv6 RA\n")
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	rows := []kvRow{
		kv("interface", summary.Interface),
		kv("default_route", summary.DefaultRoute),
		kv("default_gateways", strings.Join(summary.DefaultGateways, ",")),
		kv("accept_ra", summary.AcceptRA),
		kv("accept_ra_defrtr", summary.AcceptRADefrtr),
		kv("accept_ra_pinfo", summary.AcceptRAPinfo),
		kv("accept_ra_rtr_pref", summary.AcceptRARtrPref),
		kv("accept_ra_min_lft", summary.AcceptRAMinLft),
		kv("accept_ra_min_hop_limit", summary.AcceptRAMinHopLft),
		kv("accept_ra_from_local", summary.AcceptRAFromLocal),
	}
	for _, row := range rows {
		if row.label == "" || row.value == "" {
			continue
		}
		_, _ = fmt.Fprintf(tw, "  %s\t%s\n", row.label, row.value)
	}
	_ = tw.Flush()
	return b.String()
}

// Empty reports whether the summary contains any user-visible data.
func (s IPv6RASummary) Empty() bool {
	return s.DefaultRoute == "" &&
		len(s.DefaultGateways) == 0 &&
		s.AcceptRA == "" &&
		s.AcceptRADefrtr == "" &&
		s.AcceptRAPinfo == "" &&
		s.AcceptRARtrPref == "" &&
		s.AcceptRAMinLft == "" &&
		s.AcceptRAMinHopLft == "" &&
		s.AcceptRAFromLocal == ""
}

func collectIPv6RASysctls(ctx context.Context, client adb.Client, iface string) map[string]string {
	base := shellQuote("/proc/sys/net/ipv6/conf/" + iface)
	script := "base=" + base + "; for n in " + strings.Join(ipv6RASysctlKeys, " ") + "; do " +
		"p=\"$base/$n\"; " +
		"if [ -r \"$p\" ]; then printf '%s=' \"$n\"; cat \"$p\"; fi; " +
		"done"
	result, _ := client.Run(ctx, "shell", "sh", "-c", shellQuote(script))
	values := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(result.Stdout), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || key == "" || value == "" {
			continue
		}
		values[key] = value
	}
	return values
}

func summarizeIPv6DefaultRoutes(routes []string) (bool, []string) {
	present := false
	gatewaySet := map[string]struct{}{}
	for _, raw := range routes {
		route := strings.TrimSpace(raw)
		if route == "" {
			continue
		}
		gateway := ""
		switch {
		case strings.Contains(route, "->"):
			next := strings.TrimSpace(strings.SplitN(route, "->", 2)[1])
			gateway = strings.TrimSpace(strings.SplitN(next, " ", 2)[0])
		case strings.Contains(route, "via "):
			next := strings.TrimSpace(strings.SplitN(route, "via ", 2)[1])
			gateway = strings.TrimSpace(strings.SplitN(next, " ", 2)[0])
		}
		isIPv6Default := strings.HasPrefix(route, "::/0") ||
			(strings.HasPrefix(route, "default") && (gateway == "::" || strings.Contains(gateway, ":")))
		if !isIPv6Default {
			continue
		}
		present = true
		if gateway != "" && gateway != "::" {
			gatewaySet[gateway] = struct{}{}
		}
	}
	gateways := make([]string, 0, len(gatewaySet))
	for gateway := range gatewaySet {
		gateways = append(gateways, gateway)
	}
	sort.Strings(gateways)
	return present, gateways
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
