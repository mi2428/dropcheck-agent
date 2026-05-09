package command

import (
	"testing"
	"time"

	"dropcheck/controller/internal/controlpb"
)

func TestTimeoutForControllerDeadlines(t *testing.T) {
	tests := []struct {
		name string
		cmd  *controlpb.RunCommand
		want time.Duration
	}{
		{
			name: "connect uses requested agent timeout without extra slack",
			cmd: &controlpb.RunCommand{Command: &controlpb.RunCommand_ConnectWifi{
				ConnectWifi: &controlpb.ConnectWifi{TimeoutMs: 25000},
			}},
			want: 25 * time.Second,
		},
		{
			name: "fresh scan keeps controller slack after agent wait",
			cmd: &controlpb.RunCommand{Command: &controlpb.RunCommand_GetFreshWifiScan{
				GetFreshWifiScan: &controlpb.GetFreshWifiScan{TimeoutMs: 12000},
			}},
			want: 17 * time.Second,
		},
		{
			name: "cycle multiplies per-cycle connect and pause deadline",
			cmd: &controlpb.RunCommand{Command: &controlpb.RunCommand_CycleWifi{
				CycleWifi: &controlpb.CycleWifi{
					Count:   3,
					Connect: &controlpb.ConnectWifi{TimeoutMs: 1000},
					PauseMs: 250,
				},
			}},
			want: 43*time.Second + 750*time.Millisecond,
		},
		{
			name: "global ip all families runs both family probes",
			cmd: &controlpb.RunCommand{Command: &controlpb.RunCommand_GlobalIp{
				GlobalIp: &controlpb.GlobalIp{Family: controlpb.IpFamily_IP_FAMILY_ALL},
			}},
			want: 13 * time.Second,
		},
		{
			name: "global ip single family uses one probe timeout",
			cmd: &controlpb.RunCommand{Command: &controlpb.RunCommand_GlobalIp{
				GlobalIp: &controlpb.GlobalIp{
					Family:    controlpb.IpFamily_IP_FAMILY_IPV4,
					TimeoutMs: 9000,
				},
			}},
			want: 12 * time.Second,
		},
		{
			name: "standalone run once allows long on-device festa execution",
			cmd: &controlpb.RunCommand{Command: &controlpb.RunCommand_RunStandaloneOnce{
				RunStandaloneOnce: &controlpb.RunStandaloneOnce{Festa: "smoke"},
			}},
			want: 30 * time.Minute,
		},
		{
			name: "unknown command gets bounded fallback",
			cmd:  &controlpb.RunCommand{},
			want: 15 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TimeoutFor(tt.cmd); got != tt.want {
				t.Fatalf("TimeoutFor() = %s, want %s", got, tt.want)
			}
		})
	}
}
