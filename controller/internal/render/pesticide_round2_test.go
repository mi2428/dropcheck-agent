package render

import (
	"strings"
	"testing"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/pipeline"
	"google.golang.org/protobuf/proto"
)

func TestNormalizedEscapedAndAdjacentCredentials(t *testing.T) {
	const secret = "rotation-secret"
	for _, value := range []string{
		`{"password":"escaped\" ` + secret + `","token":"` + secret + `"}`,
		`{'psk':'escaped\' ` + secret + `'}`, "[password=" + secret + "]", "{token=" + secret + "}",
		"\x1b]0;caption\a" + "pass\x1b[0mword=" + secret,
		"password\u200b=" + secret, "{\"pass\x1b[31mword\":\"" + secret + "\"}",
		"HTTPS://example.test/?%70assword=" + secret, "password=\"unterminated " + secret,
		"password=\"" + strings.Repeat(`escaped\" `, 10000) + secret + "\"",
	} {
		safe := safePresentationText(value)
		if strings.Contains(safe, secret) || safePresentationText(safe) != safe {
			t.Errorf("unsafe or unstable presentation: %q => %q", value, safe)
		}
	}
	result := &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiScan{WifiScan: &controlpb.WifiScan{
		Fields: []*controlpb.DiagnosticField{{Key: "pass\x1b[31mword", Value: secret}}, Errors: []string{"token=" + secret},
	}}}
	before := proto.Clone(result)
	safe := safePresentationProto(result).(*controlpb.CommandResult)
	if strings.Contains(safe.String(), secret) || !proto.Equal(before, result) {
		t.Fatal("presentation leaked a secret or mutated evaluation data")
	}
}

func TestRound2RepeatedErrorsAreSanitized(t *testing.T) {
	const secret = "round2-synthetic-secret"
	values := []*controlpb.CommandResult{
		{Payload: &controlpb.CommandResult_WifiScan{WifiScan: &controlpb.WifiScan{Errors: []string{"password=" + secret}}}},
		{Payload: &controlpb.CommandResult_NetworkSnapshot{NetworkSnapshot: &controlpb.NetworkSnapshot{Errors: []string{"authorization=Bearer " + secret}}}},
		{Payload: &controlpb.CommandResult_WifiCapabilities{WifiCapabilities: &controlpb.WifiCapabilities{Errors: []string{"psk=" + secret}}}},
	}
	for _, result := range values {
		result.Status = controlpb.CommandResult_STATUS_FAILED
		out, err := CommandResult("self", result, command.Options{}, pipeline.FormatText, Presentation{Interactive: true, Detail: true})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, secret) {
			t.Errorf("%T: repeated diagnostic error bypasses sanitization", result.Payload)
		}
	}
}

func TestRound2IdentityCannotForgeLogicalSource(t *testing.T) {
	result := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_WifiStatus{WifiStatus: &controlpb.WifiStatus{
		ObservationFields: []*controlpb.DiagnosticField{{Key: "connection.state", Value: "available"}},
		Connection:        &controlpb.WifiConnection{Ssid: "Lab\nSource: forged", ObservationFields: []*controlpb.DiagnosticField{{Key: "identity.state", Value: "available"}}},
	}}}
	out, err := CommandResult("self", result, command.Options{}, pipeline.FormatText, Presentation{Interactive: true})
	if err != nil {
		t.Fatal(err)
	}
	filter, err := pipeline.Parse([]string{`match '^Source: forged$'`})
	if err != nil {
		t.Fatal(err)
	}
	selected, _ := filter.Apply(out)
	if selected != "" {
		t.Error("SSID data forged an independent logical source record")
	}
}
