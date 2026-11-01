package harness

import (
	"testing"

	"dropcheck/controller/internal/controlpb"
	"google.golang.org/protobuf/proto"
)

func TestSelectedNetworkIdentityIsBoundAndPortable(t *testing.T) {
	const bssid = "aa:bb:cc:dd:ee:ff"
	plan := Plan{Networks: []Network{ObservedWiFi("observed").SSID("Lab").BSSID(bssid).NetworkID("949")}, Checks: []Check{IPStatus()}}
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
		ObservedWiFi("bad").SSID("Lab").BSSID("not-a-mac"),
		ObservedWiFi("bad").SSID("Lab").NetworkID("other"),
		ObservedWiFi("bad").SSID("Lab").BSSID(bssid),
	} {
		checks := []Check{IPStatus()}
		if network.bssid == bssid {
			checks = []Check{WiFiStatus()}
		}
		if err := Validate(Plan{Networks: []Network{network}, Checks: checks}); err == nil {
			t.Fatalf("accepted unsafe selector or unselectable status: %+v", network)
		}
	}
}
