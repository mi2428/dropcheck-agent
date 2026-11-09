package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/pipeline"
	"github.com/mi2428/dropcheck-agent/controller/internal/tuirun"
)

func TestReportPresentationRedactsBeforeShellJSONWithoutMutatingTypedReport(t *testing.T) {
	report := tuirun.Report{Problems: []tuirun.Problem{{Message: "https://example.test/path?token=unshared-credential"}}, Steps: []tuirun.StepReport{{Attempts: []tuirun.Attempt{{Result: tuirun.OperationResult{Raw: &controlpb.CommandResult{
		Message: "authorization: Bearer unshared-credential", Payload: &controlpb.CommandResult_IpStatus{IpStatus: &controlpb.IpStatus{Wifi: &controlpb.WifiConnection{Ssid: "Lab token=public-literal", Bssid: "aa:bb:cc:dd:ee:ff", ObservationFields: testObservationFields("identity")}, ObservationFields: []*controlpb.DiagnosticField{{Key: "token", Value: "unshared-credential"}}}},
	}}}}}}}
	before, _ := json.Marshal(report)
	text, err := Report(report, Presentation{Detail: true})
	if err != nil {
		t.Fatal(err)
	}
	data, err := pipeline.JSON(text, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"unshared-credential", "token=unshared-credential"} {
		if strings.Contains(data, secret) || strings.Contains(text, secret) {
			t.Fatalf("export leaked %q: %s", secret, data)
		}
	}
	for _, kept := range []string{"Lab token=public-literal", "aa:bb:cc:dd:ee:ff"} {
		if !strings.Contains(data, kept) || !strings.Contains(text, kept) {
			t.Fatalf("lost literal identity %q: %s", kept, data)
		}
	}
	safe := SanitizedResult(report.Steps[0].Attempts[0].Result.Raw)
	if safe.GetMessage() != "authorization: <redacted>" || safe.GetIpStatus().GetObservationFields()[0].GetValue() != "<redacted>" {
		t.Fatal("safe snapshot leaked nested credentials")
	}
	after, _ := json.Marshal(report)
	if !bytes.Equal(before, after) {
		t.Fatal("export changed original typed report")
	}
}
