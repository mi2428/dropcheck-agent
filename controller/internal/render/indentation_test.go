package render

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
)

func requireIndent(t *testing.T, text, prefix string, want int) {
	t.Helper()
	for line := range strings.SplitSeq(text, "\n") {
		if strings.HasPrefix(strings.TrimLeft(line, " "), prefix) {
			if got := len(line) - len(strings.TrimLeft(line, " ")); got != want {
				t.Fatalf("%s depth=%d, want %d: %q", prefix, got, want, line)
			}
			return
		}
	}
	t.Fatalf("missing %q in:\n%s", prefix, text)
}

func TestExplicitIndentationComposesAcrossFieldsTablesAndWidths(t *testing.T) {
	for _, width := range []int{24, 40, 80, 120} {
		for _, depth := range []int{0, 2, 4} {
			t.Run(fmt.Sprintf("%d/%d", width, depth), func(t *testing.T) {
				var body strings.Builder
				writeRecord(&body, "SSID", "Lab")
				writeDisplayTable(&body, []displayTableColumn{{header: "BSSID"}, {header: "dBm"}}, [][]string{{"11:11:11:11:11:11", "-43"}}, width-depth)
				logical := body.String()
				for nesting := 0; nesting < depth; nesting += 2 {
					var nested strings.Builder
					writeNested(&nested, logical)
					logical = nested.String()
				}
				out := wrapPresentation(logical, width)
				requireIndent(t, out, "SSID:", depth)
				// Table-to-record fallback must not change the left edge.
				requireIndent(t, out, "BSSID", depth)
				for line := range strings.SplitSeq(out, "\n") {
					if displayWidth(line) > width {
						t.Fatalf("width %d exceeded by %q", width, line)
					}
				}
			})
		}
	}
}

func TestScanHierarchyAndGroupingForNormalBriefAndMLO(t *testing.T) {
	ap := &controlpb.WifiScanResult{Ssid: strings.Repeat("Lab", 16), Bssid: "11:11:11:11:11:11", RssiDbm: -43, FrequencyMhz: 5240, WifiStandard: "802.11be", ApMldMacAddress: "11:11:11:11:11:12", OperatorFriendlyName: "owner", SecurityDetails: &controlpb.WifiSecurityDetails{RsnPresent: true, RsnVersion: 1}, InformationElements: []*controlpb.WifiInformationElement{{Id: 48, BytesHex: "0102"}}}
	scan := &controlpb.WifiScan{Results: []*controlpb.WifiScanResult{ap}}
	for _, brief := range []bool{false, true} {
		for _, mlo := range []bool{false, true} {
			grouped := false
			for _, width := range []int{0, 40, 90, 200} {
				options := command.Options{WifiScanBrief: brief, WifiScanMLO: mlo}
				var body strings.Builder
				renderAdaptiveScan(&body, scan, options, Presentation{Width: width})
				out := wrapPresentation(body.String(), width)
				if strings.Contains(out, "\nAP  BSSID") {
					grouped = true
					requireIndent(t, out, "SSID:", 0)
					requireIndent(t, out, "AP  BSSID", 0)
					if !strings.Contains(out, "\nSSID: "+ap.GetSsid()+"\n") {
						t.Fatal("group heading inherited unrelated metadata padding")
					}
				}
				if !brief {
					// Inspect AP-owned values, not the optional overview/group table.
					_, detail, ok := strings.Cut(out, "[+] AP 1\n")
					if !ok {
						t.Fatalf("AP detail missing: %s", out)
					}
					requireIndent(t, detail, "SSID:", 2)
					requireIndent(t, detail, "FLAGS:", 2)
					requireIndent(t, detail, "Wi-Fi Security Details", 2)
					requireIndent(t, detail, "rsn:", 4)
					requireIndent(t, detail, "AP properties", 2)
					requireIndent(t, detail, "Operator:", 4)
					requireIndent(t, detail, "Information Elements", 2)
					requireIndent(t, detail, "IE 1:", 4)
				}
			}
			if !grouped {
				t.Fatalf("grouped layout not exercised: brief=%t mlo=%t", brief, mlo)
			}
		}
	}
}

func TestNetworkBundleAndMLOSectionDepths(t *testing.T) {
	ip := &controlpb.IpStatus{NetworkId: "7", Addresses: []string{"192.0.2.7/24"}}
	var network strings.Builder
	renderIPView(&network, ip, Presentation{})
	out := wrapPresentation(network.String(), 0)
	requireIndent(t, out, "Network:", 0)
	requireIndent(t, out, "IPv4", 0)
	requireIndent(t, out, "Addr:", 2)
	requireIndent(t, out, "IPv6 RA (android)", 0)
	requireIndent(t, out, "source:", 2)
	var bundle strings.Builder
	renderDiagnosticBundle(&bundle, &controlpb.WifiDiagnostics{Status: &controlpb.WifiStatus{IpStatus: ip}}, command.Options{}, Presentation{})
	out = wrapPresentation(bundle.String(), 0)
	requireIndent(t, out, "Summary", 0)
	requireIndent(t, out, "Constituent status:", 2)
	requireIndent(t, out, "Network:", 2)
	requireIndent(t, out, "IPv4", 2)
	requireIndent(t, out, "Addr:", 4)
	for _, current := range []*controlpb.WifiConnection{nil, {Bssid: "11:11:11:11:11:11"}} {
		var relation strings.Builder
		renderWifiMLOCurrentRelation(&relation, current, nil, nil)
		out = wrapPresentation(relation.String(), 0)
		label := "Observation:"
		if current != nil {
			label = "connected_bssid:"
		}
		requireIndent(t, out, "Current AP Relation", 0)
		requireIndent(t, out, label, 2)
	}
	var affiliated strings.Builder
	renderWifiMLOAffiliatedLinkBlocks(&affiliated, wifiMLOGroup{}, &controlpb.WifiScanResult{AffiliatedMloLinks: []*controlpb.MloLinkInfo{{LinkId: 1}}}, nil)
	out = wrapPresentation(affiliated.String(), 0)
	requireIndent(t, out, "affiliated_links", 2)
	requireIndent(t, out, "[", 4)
	requireIndent(t, out, "Identity:", 6)
	requireIndent(t, out, "Radio:", 6)
}
