package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/watchrun"
)

func TestSafeReportJSONRedactsBeforeEncodingWithoutMutatingTypedReport(t *testing.T) {
	report := watchrun.Report{Problems: []watchrun.Problem{{Message: "https://example.test/path?token=unshared-credential"}}, Steps: []watchrun.StepReport{{Attempts: []watchrun.Attempt{{Result: watchrun.OperationResult{Raw: &controlpb.CommandResult{
		Message: "authorization: Bearer unshared-credential", Payload: &controlpb.CommandResult_IpStatus{IpStatus: &controlpb.IpStatus{Wifi: &controlpb.WifiConnection{Ssid: "Lab token=public-literal", Bssid: "aa:bb:cc:dd:ee:ff"}, ObservationFields: []*controlpb.DiagnosticField{{Key: "token", Value: "unshared-credential"}}}},
	}}}}}}}
	before, _ := json.Marshal(report)
	data, err := SafeReportJSON(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"unshared-credential", "token=unshared-credential"} {
		if bytes.Contains(data, []byte(secret)) {
			t.Fatalf("export leaked %q: %s", secret, data)
		}
	}
	for _, kept := range []string{"Lab token=public-literal", "aa:bb:cc:dd:ee:ff"} {
		if !strings.Contains(string(data), kept) {
			t.Fatalf("lost literal identity %q: %s", kept, data)
		}
	}
	after, _ := json.Marshal(report)
	if !bytes.Equal(before, after) {
		t.Fatal("export changed original typed report")
	}
}
