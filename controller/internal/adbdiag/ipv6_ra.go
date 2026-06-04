package adbdiag

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/netip"
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
	Advertisements    []IPv6RAAdvertisement
}

// IPv6RAAdvertisement is one router advertisement cached by Android's IpClient.
type IPv6RAAdvertisement struct {
	Source         string
	Destination    string
	LastSeen       string
	RouterLifetime string
	HopLimit       uint8
	FlagsHex       string
	Prefixes       []IPv6RAPrefix
}

// IPv6RAPrefix is one prefix information option carried by a router advertisement.
type IPv6RAPrefix struct {
	Prefix            string
	ValidLifetime     string
	PreferredLifetime string
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
	if result, _ := client.Run(ctx, "shell", "dumpsys", "network_stack"); strings.TrimSpace(result.Stdout) != "" {
		summary.Advertisements = parseIPv6RAAdvertisements(result.Stdout, iface)
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
	for i, ad := range summary.Advertisements {
		rows = append(rows, kv(fmt.Sprintf("ra_%d", i+1), renderIPv6RAAdvertisement(ad)))
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
		s.AcceptRAFromLocal == "" &&
		len(s.Advertisements) == 0
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

func parseIPv6RAAdvertisements(text string, iface string) []IPv6RAAdvertisement {
	lines := strings.Split(text, "\n")
	target := "IpClient." + strings.TrimSpace(iface)
	inTarget := false
	wantHex := false
	var current *IPv6RAAdvertisement
	ads := make([]IPv6RAAdvertisement, 0)
	flush := func() {
		if current == nil {
			return
		}
		ads = upsertIPv6RAAdvertisement(ads, *current)
		current = nil
		wantHex = false
	}
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "IpClient.") {
			if line == target {
				flush()
				inTarget = true
				continue
			}
			if strings.HasPrefix(line, target+" ") {
				continue
			}
			if line != target {
				flush()
				inTarget = false
				continue
			}
		}
		if !inTarget {
			continue
		}
		if strings.HasPrefix(line, "RA ") {
			flush()
			ad := parseIPv6RASummaryLine(line)
			current = &ad
			continue
		}
		if current == nil {
			continue
		}
		if strings.HasPrefix(line, "Last seen ") && strings.HasSuffix(line, " ago") {
			current.LastSeen = strings.TrimSuffix(strings.TrimPrefix(line, "Last seen "), " ago")
			continue
		}
		if line == "Last match:" {
			wantHex = true
			continue
		}
		if wantHex {
			wantHex = false
			if parsed, ok := parseIPv6RARawHex(line); ok {
				current.merge(parsed)
			}
		}
	}
	flush()
	return ads
}

func parseIPv6RASummaryLine(line string) IPv6RAAdvertisement {
	line = strings.TrimSpace(strings.TrimPrefix(line, "RA "))
	source, rest, ok := strings.Cut(line, " -> ")
	if !ok {
		return IPv6RAAdvertisement{}
	}
	destination, rest, ok := strings.Cut(rest, " ")
	if !ok {
		return IPv6RAAdvertisement{Source: strings.TrimSpace(source)}
	}
	lifetime, rest, _ := strings.Cut(strings.TrimSpace(rest), " ")
	return IPv6RAAdvertisement{
		Source:         strings.TrimSpace(source),
		Destination:    strings.TrimSpace(destination),
		RouterLifetime: strings.TrimSpace(lifetime),
		Prefixes:       parseIPv6RAPrefixSummary(strings.Fields(rest)),
	}
}

func parseIPv6RAPrefixSummary(fields []string) []IPv6RAPrefix {
	if len(fields) < 2 {
		return nil
	}
	out := make([]IPv6RAPrefix, 0)
	seen := map[string]struct{}{}
	for i := 0; i+1 < len(fields); i += 2 {
		prefix := strings.TrimSpace(fields[i])
		lifetimes := strings.TrimSpace(fields[i+1])
		valid, preferred, ok := strings.Cut(lifetimes, "/")
		if prefix == "" || !ok {
			continue
		}
		item := IPv6RAPrefix{
			Prefix:            prefix,
			ValidLifetime:     valid,
			PreferredLifetime: preferred,
		}
		key := item.Prefix + "|" + item.ValidLifetime + "|" + item.PreferredLifetime
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func parseIPv6RARawHex(value string) (IPv6RAAdvertisement, bool) {
	hexText := strings.Join(strings.Fields(strings.TrimSpace(value)), "")
	if hexText == "" {
		return IPv6RAAdvertisement{}, false
	}
	frame, err := hex.DecodeString(hexText)
	if err != nil || len(frame) < 14+40+16 {
		return IPv6RAAdvertisement{}, false
	}
	if binary.BigEndian.Uint16(frame[12:14]) != 0x86dd {
		return IPv6RAAdvertisement{}, false
	}
	ipOffset := 14
	if frame[ipOffset]>>4 != 6 || frame[ipOffset+6] != 58 {
		return IPv6RAAdvertisement{}, false
	}
	icmpOffset := ipOffset + 40
	if len(frame) < icmpOffset+16 || frame[icmpOffset] != 134 {
		return IPv6RAAdvertisement{}, false
	}
	src, ok := netip.AddrFromSlice(frame[ipOffset+8 : ipOffset+24])
	if !ok {
		return IPv6RAAdvertisement{}, false
	}
	dst, ok := netip.AddrFromSlice(frame[ipOffset+24 : ipOffset+40])
	if !ok {
		return IPv6RAAdvertisement{}, false
	}
	ad := IPv6RAAdvertisement{
		Source:         src.String(),
		Destination:    dst.String(),
		HopLimit:       frame[icmpOffset+4],
		FlagsHex:       fmt.Sprintf("0x%02x", frame[icmpOffset+5]),
		RouterLifetime: fmt.Sprintf("%ds", binary.BigEndian.Uint16(frame[icmpOffset+6:icmpOffset+8])),
	}
	for optionOffset := icmpOffset + 16; optionOffset+2 <= len(frame); {
		optionType := frame[optionOffset]
		optionUnits := int(frame[optionOffset+1])
		if optionUnits == 0 {
			break
		}
		optionLen := optionUnits * 8
		if optionOffset+optionLen > len(frame) {
			break
		}
		option := frame[optionOffset : optionOffset+optionLen]
		if optionType == 3 && optionLen >= 32 {
			prefixAddr, ok := netip.AddrFromSlice(option[16:32])
			if ok {
				prefix := netip.PrefixFrom(prefixAddr, int(option[2])).Masked().String()
				ad.Prefixes = append(ad.Prefixes, IPv6RAPrefix{
					Prefix:            prefix,
					ValidLifetime:     formatIPv6RALifetime(binary.BigEndian.Uint32(option[4:8])),
					PreferredLifetime: formatIPv6RALifetime(binary.BigEndian.Uint32(option[8:12])),
				})
			}
		}
		optionOffset += optionLen
	}
	ad.Prefixes = dedupeIPv6RAPrefixes(ad.Prefixes)
	return ad, true
}

func formatIPv6RALifetime(value uint32) string {
	if value == ^uint32(0) {
		return "infinite"
	}
	return fmt.Sprintf("%ds", value)
}

func dedupeIPv6RAPrefixes(values []IPv6RAPrefix) []IPv6RAPrefix {
	if len(values) < 2 {
		return values
	}
	out := make([]IPv6RAPrefix, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		key := value.Prefix + "|" + value.ValidLifetime + "|" + value.PreferredLifetime
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func renderIPv6RAAdvertisement(ad IPv6RAAdvertisement) string {
	if ad.Source == "" && len(ad.Prefixes) == 0 {
		return ""
	}
	parts := make([]string, 0, 8)
	if ad.Source != "" {
		parts = append(parts, "src="+ad.Source)
	}
	if ad.Destination != "" {
		parts = append(parts, "dst="+ad.Destination)
	}
	if ad.RouterLifetime != "" {
		parts = append(parts, "router_lifetime="+ad.RouterLifetime)
	}
	if ad.LastSeen != "" {
		parts = append(parts, "last_seen="+ad.LastSeen)
	}
	if ad.HopLimit != 0 {
		parts = append(parts, fmt.Sprintf("hop_limit=%d", ad.HopLimit))
	}
	if ad.FlagsHex != "" {
		parts = append(parts, "flags="+ad.FlagsHex)
	}
	if len(ad.Prefixes) > 0 {
		prefixes := make([]string, 0, len(ad.Prefixes))
		for _, prefix := range ad.Prefixes {
			item := prefix.Prefix
			if prefix.ValidLifetime != "" {
				item += " valid=" + prefix.ValidLifetime
			}
			if prefix.PreferredLifetime != "" {
				item += " preferred=" + prefix.PreferredLifetime
			}
			prefixes = append(prefixes, item)
		}
		parts = append(parts, "prefixes="+strings.Join(prefixes, " | "))
	}
	return strings.Join(parts, " ")
}

func upsertIPv6RAAdvertisement(values []IPv6RAAdvertisement, item IPv6RAAdvertisement) []IPv6RAAdvertisement {
	if item.Source == "" && item.Destination == "" && len(item.Prefixes) == 0 {
		return values
	}
	for i := range values {
		if values[i].Source == item.Source && values[i].Destination == item.Destination {
			values[i].merge(item)
			return values
		}
	}
	return append(values, item)
}

func (ad *IPv6RAAdvertisement) merge(other IPv6RAAdvertisement) {
	if ad.Source == "" {
		ad.Source = other.Source
	}
	if ad.Destination == "" {
		ad.Destination = other.Destination
	}
	if ad.LastSeen == "" {
		ad.LastSeen = other.LastSeen
	}
	if ad.RouterLifetime == "" {
		ad.RouterLifetime = other.RouterLifetime
	}
	if ad.HopLimit == 0 {
		ad.HopLimit = other.HopLimit
	}
	if ad.FlagsHex == "" {
		ad.FlagsHex = other.FlagsHex
	}
	if len(other.Prefixes) == 0 {
		return
	}
	ad.Prefixes = dedupeIPv6RAPrefixes(append(ad.Prefixes, other.Prefixes...))
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
