package safevalue

import (
	"strings"
	"testing"

	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"google.golang.org/protobuf/proto"
)

func TestSafeProjectionPreservesAcquisitionAndLiteralIdentity(t *testing.T) {
	for _, value := range []string{
		"authorization: Bearer syntheticsecret",
		"https://user:syntheticsecret@example.invalid/?%74oken=syntheticsecret&n=0#api_key=syntheticsecret",
		"https://example.invalid/#token%3Dsyntheticsecret",
		"https://example.invalid/#/callback?%74oken=syntheticsecret&n=0",
		"\x1b[31mpassword\x1b[0m=syntheticsecret",
	} {
		if got := Text(value); strings.Contains(got, "syntheticsecret") {
			t.Errorf("credential remained in %q", got)
		}
	}
	const identity = "password=Guest"
	raw := &controlpb.CommandResult{Message: "token=syntheticsecret", Payload: &controlpb.CommandResult_IpStatus{IpStatus: &controlpb.IpStatus{
		Wifi:              &controlpb.WifiConnection{Ssid: identity, Bssid: "aa:bb:cc:dd:ee:ff"},
		ObservationFields: []*controlpb.DiagnosticField{{Key: "password", Value: "syntheticsecret"}, {Key: "ssid", Value: identity}},
	}}}
	before := proto.Clone(raw)
	copy := Proto(raw).(*controlpb.CommandResult)
	if !proto.Equal(raw, before) || copy == raw || copy.GetIpStatus() == raw.GetIpStatus() {
		t.Fatal("safe projection mutated or shared acquisition data")
	}
	if copy.GetIpStatus().GetWifi().GetSsid() != identity || Field("ssid", identity) != identity || copy.GetIpStatus().GetObservationFields()[1].GetValue() != identity {
		t.Fatal("literal identity was redacted")
	}
	if strings.Contains(copy.String(), "syntheticsecret") || copy.GetIpStatus().GetObservationFields()[0].GetValue() != "<redacted>" {
		t.Fatal("nested credential was retained")
	}
	if Proto(nil) != nil {
		t.Fatal("nil input must remain nil")
	}
	SanitizeProto((*controlpb.CommandResult)(nil))
}
