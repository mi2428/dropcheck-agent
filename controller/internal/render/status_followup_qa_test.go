package render

import (
	"strings"
	"testing"
	"unicode"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"google.golang.org/protobuf/proto"
)

func TestStatusFollowupQACredentialsInExpandedFields(t *testing.T) {
	status := &controlpb.WifiStatus{State: "token=qa-framework-secret", Connection: &controlpb.WifiConnection{
		Ssid: "password=Guest", PasspointProviderFriendlyName: "password=qa-provider-secret", ApplicableRedactions: "token=qa-redaction-secret",
		ObservationFields: []*controlpb.DiagnosticField{{Key: "identity.state", Value: "available"}},
	}, IpStatus: &controlpb.IpStatus{NetworkId: "24", HttpProxy: "https://proxy-user:qa-proxy-secret@example.test:8443/", NetworkSpecifier: "passphrase=qa-specifier-secret",
		PrivateDnsActive: true, PrivateDnsServerName: "password=qa-dns-secret",
		Ipv6Ra:            []*controlpb.DiagnosticField{{Key: " password ", Value: "qa-padded-key-secret"}},
		ObservationFields: []*controlpb.DiagnosticField{{Key: "link_properties.state", Value: "available"}, {Key: "capabilities.state", Value: "available"}},
	}}
	before := proto.Clone(status)
	for _, full := range []bool{false, true} {
		result := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_FAILED, Message: "known acquisition failure", Payload: &controlpb.CommandResult_WifiStatus{WifiStatus: status}}
		mode := command.WifiRenderModeStatus
		if full {
			mode = command.WifiRenderModeFull
			result.Payload = &controlpb.CommandResult_WifiDiagnostics{WifiDiagnostics: &controlpb.WifiDiagnostics{Status: status}}
		}
		out, err := CommandResult("qa", result, command.Options{Detail: full, WifiRenderMode: mode}, Presentation{Detail: full})
		if err != nil {
			t.Fatal(err)
		}
		for _, marker := range []string{"qa-framework-secret", "qa-provider-secret", "qa-redaction-secret", "qa-proxy-secret", "qa-specifier-secret", "qa-dns-secret", "qa-padded-key-secret"} {
			if strings.Contains(out, marker) {
				t.Errorf("full=%t leaked %s", full, marker)
			}
		}
		if !strings.Contains(out, "password=Guest") || !strings.Contains(out, "FAILED") || !strings.Contains(out, "known acquisition failure") || !proto.Equal(status, before) {
			t.Fatal("sanitization lost identity/outcome or changed acquisition data")
		}
	}
}

func TestStatusFollowupQAUnicodeWidthsAndIdentityInjection(t *testing.T) {
	identity := "password=Guest 東京e\u0301👩\u200d🔬"
	status := &controlpb.WifiStatus{Connection: &controlpb.WifiConnection{
		Ssid: identity + "\nSource: forged", ObservationFields: []*controlpb.DiagnosticField{{Key: "identity.state", Value: "available"}},
	}, IpStatus: &controlpb.IpStatus{NetworkId: "24", Addresses: []string{"2001:db8:ffff:ffff:ffff:ffff:ffff:ffff/128"},
		ObservationFields: []*controlpb.DiagnosticField{{Key: "link_properties.state", Value: "available"}},
	}}
	for _, width := range []int{0, 8, 12, 16, 24, 40, 60, 80} {
		out, err := CommandResult("qa", &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_WifiStatus{WifiStatus: status}},
			command.Options{WifiRenderMode: command.WifiRenderModeStatus}, Presentation{Width: width})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, "\nSource: forged\n") {
			t.Fatalf("width=%d identity forged a source record", width)
		}
		compact := strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return -1
			}
			return r
		}, out)
		for _, want := range []string{"password=Guest東京e\u0301👩\u200d🔬Source:forged", "2001:db8:ffff:ffff:ffff:ffff:ffff:ffff/128"} {
			if !strings.Contains(compact, want) {
				t.Errorf("width=%d lost %q", width, want)
			}
		}
		if width != 0 {
			for line := range strings.SplitSeq(out, "\n") {
				if displayWidth(line) > width {
					t.Errorf("width=%d overflow=%d", width, displayWidth(line))
					break
				}
			}
		}
	}
}
