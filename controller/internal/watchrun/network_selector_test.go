package watchrun

import (
	"testing"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/controlpb"
	"google.golang.org/protobuf/proto"
)

func TestSelectedNetworkIdentityIsBoundAndPortable(t *testing.T) {
	const bssid = "aa:bb:cc:dd:ee:ff"
	plan := Plan{Networks: []Network{{Name: "observed", ReadOnly: true, SSID: "Lab", BSSID: bssid, NetworkID: "949"}}, Checks: []Check{{Name: "ip status", Operation: command.IPStatusOperation()}}}
	compiled, err := compilePlan(plan, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	selector := compiled.targets[0].checks[0].operation.Command.GetGetIpStatus().GetSelector()
	if selector.GetSsid() != "Lab" || selector.GetBssid() != bssid || selector.GetNetworkId() != "949" {
		t.Fatalf("bound selector=%+v", selector)
	}
	wire, err := proto.Marshal(selector)
	if err != nil {
		t.Fatal(err)
	}
	var decoded controlpb.NetworkSelector
	if err := proto.Unmarshal(wire, &decoded); err != nil || !proto.Equal(selector, &decoded) {
		t.Fatalf("wire selector=%+v err=%v", &decoded, err)
	}
	for _, network := range []Network{
		{Name: "bad", ReadOnly: true, SSID: "Lab", BSSID: "not-a-mac"},
		{Name: "bad", ReadOnly: true, SSID: "Lab", NetworkID: "other"},
		{Name: "bad", ReadOnly: true, SSID: "Lab", BSSID: bssid},
	} {
		checks := []Check{{Name: "ip status", Operation: command.IPStatusOperation()}}
		if network.BSSID == bssid {
			checks = []Check{{Name: "wifi status", Operation: command.WifiStatusOperation()}}
		}
		if err := Validate(Plan{Networks: []Network{network}, Checks: checks}); err == nil {
			t.Fatalf("accepted unsafe selector or unselectable status: %+v", network)
		}
	}
}
