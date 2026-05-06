package adbdiag

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"text/tabwriter"
	"time"

	"dropcheck/controller/internal/adb"
)

const (
	mloCommandTimeout  = 8 * time.Second
	dumpsysWifiMLOGrep = `dumpsys wifi | grep -Ei 'MLO|MLD|TID-To-Link|wifi_link_count|mlo_mode|Link Stats from link_id|Dump of BSSID to Affiliated BSSID mapping' | tail -n 20`
)

// MLOSummary is the controller-side, best-effort MLO view extracted from adb.
type MLOSummary struct {
	Source               string
	TIDToLinkSupported   string
	APMLDAddress         string
	APMLOLinkID          string
	APMLOAffiliatedLinks string
	VendorData           string
	MLOMode              string
	WifiLinkCount        string
	LinkStats            []MLOLinkStat
}

// MLOLinkStat is one adb-reported Wi-Fi link stats row.
type MLOLinkStat struct {
	LinkID       string
	State        string
	RadioID      string
	FrequencyMHz string
	RSSI         string
	RSSIMgmt     string
	ChannelWidth string
	BeaconRx     string
}

type kvRow struct {
	label string
	value string
}

func kv(label string, value any) kvRow {
	return kvRow{label: label, value: fmt.Sprint(value)}
}

func writeSection(b *strings.Builder, title string) {
	if b.Len() > 0 {
		current := b.String()
		if !strings.HasSuffix(current, "\n") {
			b.WriteByte('\n')
		}
		if !strings.HasSuffix(current, "\n\n") {
			b.WriteByte('\n')
		}
	}
	b.WriteString(title)
	b.WriteByte('\n')
}

func writeKVSection(b *strings.Builder, title string, rows ...kvRow) {
	writeSection(b, title)
	tw := tabwriter.NewWriter(b, 0, 0, 2, ' ', 0)
	for _, row := range rows {
		if row.label == "" || row.value == "" {
			continue
		}
		_, _ = fmt.Fprintf(tw, "  %s\t%s\n", row.label, row.value)
	}
	_ = tw.Flush()
}

// CollectMLO runs a short adb-only MLO supplement for show wifi status.
//
// It intentionally returns an empty summary instead of an error; adb support is
// diagnostic-only and must not make the normal agent Wi-Fi status fail.
func CollectMLO(ctx context.Context, client adb.Client) MLOSummary {
	if client.Timeout <= 0 {
		client.Timeout = mloCommandTimeout
	}
	summary := MLOSummary{Source: "adb"}
	if result, _ := client.Run(ctx, "shell", "cmd", "wifi", "status"); strings.TrimSpace(result.Stdout) != "" {
		summary.merge(ParseMLOSummary(result.Stdout))
	}
	if result, _ := client.Run(ctx, "shell", dumpsysWifiMLOGrep); strings.TrimSpace(result.Stdout) != "" {
		summary.merge(ParseMLOSummary(result.Stdout))
	}
	if summary.Empty() {
		return MLOSummary{}
	}
	summary.Source = "adb"
	return summary
}

// ParseMLOSummary extracts known MLO fields from cmd wifi status or dumpsys wifi text.
func ParseMLOSummary(text string) MLOSummary {
	summary := MLOSummary{}
	summary.TIDToLinkSupported = extractMLOLabel(text, "Is TID-To-Link negotiation supported by the AP")
	summary.APMLDAddress = extractMLOLabel(text, "AP MLD Address")
	summary.APMLOLinkID = extractMLOLabel(text, "AP MLO Link Id")
	summary.APMLOAffiliatedLinks = extractMLOLabel(text, "AP MLO Affiliated links")
	summary.VendorData = extractMLOLabel(text, "Vendor Data")
	summary.MLOMode = extractKeyValue(text, "mlo_mode")
	summary.WifiLinkCount = extractKeyValue(text, "wifi_link_count")
	summary.LinkStats = parseMLOLinkStats(text)
	return summary
}

// RenderMLOSummary renders adb MLO data for inclusion in show wifi status text.
func RenderMLOSummary(summary MLOSummary) string {
	if summary.Empty() {
		return ""
	}
	var b strings.Builder
	rows := []kvRow{}
	if summary.TIDToLinkSupported != "" {
		rows = append(rows, kv("tid_to_link", summary.TIDToLinkSupported))
	}
	if summary.APMLDAddress != "" {
		rows = append(rows, kv("ap_mld", summary.APMLDAddress))
	}
	if summary.APMLOLinkID != "" {
		rows = append(rows, kv("ap_link_id", summary.APMLOLinkID))
	}
	if summary.MLOMode != "" {
		rows = append(rows, kv("mlo_mode", summary.MLOMode))
	}
	if summary.WifiLinkCount != "" {
		rows = append(rows, kv("wifi_link_count", summary.WifiLinkCount))
	}
	if len(rows) == 0 {
		rows = append(rows, kv("available", "true"))
	}
	writeKVSection(&b, "ADB MLO", rows...)
	if summary.APMLOAffiliatedLinks != "" {
		writeKVSection(&b, "ADB MLO Affiliated", kv("links", summary.APMLOAffiliatedLinks))
	}
	if summary.VendorData != "" && summary.VendorData != "<none>" {
		writeKVSection(&b, "ADB MLO Vendor Data", kv("value", summary.VendorData))
	}
	if len(summary.LinkStats) > 0 {
		writeSection(&b, "ADB MLO Link Stats")
		tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "ID\tSTATE\tRADIO\tFREQ\tRSSI\tMGMT\tWIDTH\tBEACON")
		for _, link := range summary.LinkStats {
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				empty(link.LinkID, "-"),
				empty(link.State, "-"),
				empty(link.RadioID, "-"),
				empty(link.FrequencyMHz, "-"),
				empty(link.RSSI, "-"),
				empty(link.RSSIMgmt, "-"),
				empty(link.ChannelWidth, "-"),
				empty(link.BeaconRx, "-"),
			)
		}
		_ = tw.Flush()
	}
	return b.String()
}

// Empty reports whether the summary has any user-visible MLO data.
func (s MLOSummary) Empty() bool {
	return s.TIDToLinkSupported == "" &&
		s.APMLDAddress == "" &&
		s.APMLOLinkID == "" &&
		s.APMLOAffiliatedLinks == "" &&
		s.VendorData == "" &&
		s.MLOMode == "" &&
		s.WifiLinkCount == "" &&
		len(s.LinkStats) == 0
}

func (s *MLOSummary) merge(other MLOSummary) {
	if s.TIDToLinkSupported == "" {
		s.TIDToLinkSupported = other.TIDToLinkSupported
	}
	if s.APMLDAddress == "" {
		s.APMLDAddress = other.APMLDAddress
	}
	if s.APMLOLinkID == "" {
		s.APMLOLinkID = other.APMLOLinkID
	}
	if s.APMLOAffiliatedLinks == "" {
		s.APMLOAffiliatedLinks = other.APMLOAffiliatedLinks
	}
	if s.VendorData == "" {
		s.VendorData = other.VendorData
	}
	if s.MLOMode == "" {
		s.MLOMode = other.MLOMode
	}
	if s.WifiLinkCount == "" {
		s.WifiLinkCount = other.WifiLinkCount
	}
	for _, link := range other.LinkStats {
		upsertMLOLinkStat(&s.LinkStats, link)
	}
}

func extractMLOLabel(text string, label string) string {
	labels := []string{
		"Is TID-To-Link negotiation supported by the AP",
		"AP MLD Address",
		"AP MLO Link Id",
		"AP MLO Affiliated links",
		"Vendor Data",
	}
	next := make([]string, 0, len(labels)-1)
	for _, candidate := range labels {
		if !strings.EqualFold(candidate, label) {
			next = append(next, regexp.QuoteMeta(candidate))
		}
	}
	pattern := `(?is)` + regexp.QuoteMeta(label) + `\s*:\s*(.*?)(?:,\s*(?:` + strings.Join(next, "|") + `)\s*:|\n|$)`
	match := regexp.MustCompile(pattern).FindStringSubmatch(text)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func extractKeyValue(text string, key string) string {
	pattern := `(?i)\b` + regexp.QuoteMeta(key) + `=([^,\s]+)`
	match := regexp.MustCompile(pattern).FindStringSubmatch(text)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func parseMLOLinkStats(text string) []MLOLinkStat {
	parts := strings.Split(text, "Link Stats from link_id=")
	stats := make([]MLOLinkStat, 0, len(parts)-1)
	for _, part := range parts[1:] {
		segment := "link_id=" + part
		if newline := strings.IndexByte(segment, '\n'); newline >= 0 {
			segment = segment[:newline]
		}
		values := parseCommaKeyValues(segment)
		link := MLOLinkStat{
			LinkID:       values["link_id"],
			State:        values["state"],
			RadioID:      values["radio_id"],
			FrequencyMHz: values["frequency_mhz"],
			RSSI:         values["rssi"],
			RSSIMgmt:     values["rssi_mgmt"],
			ChannelWidth: values["channel_width"],
			BeaconRx:     values["beacon_rx"],
		}
		if link.LinkID != "" {
			upsertMLOLinkStat(&stats, link)
		}
	}
	return stats
}

func parseCommaKeyValues(segment string) map[string]string {
	values := make(map[string]string)
	for _, field := range strings.Split(segment, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(field), "=")
		if !ok || key == "" {
			continue
		}
		if _, exists := values[key]; !exists {
			values[key] = strings.TrimSpace(value)
		}
	}
	return values
}

func upsertMLOLinkStat(stats *[]MLOLinkStat, link MLOLinkStat) {
	for i, existing := range *stats {
		if existing.LinkID == link.LinkID {
			(*stats)[i] = link
			return
		}
	}
	*stats = append(*stats, link)
}
