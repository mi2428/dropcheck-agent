package render

import (
	"strings"
	"testing"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/pipeline"
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
		mode := ""
		if full {
			mode = command.WifiRenderModeFull
			result.Payload = &controlpb.CommandResult_WifiDiagnostics{WifiDiagnostics: &controlpb.WifiDiagnostics{Status: status}}
		}
		out, err := CommandResult("qa", result, command.Options{Detail: full, WifiRenderMode: mode}, pipeline.FormatText, Presentation{Interactive: true, Detail: full})
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
