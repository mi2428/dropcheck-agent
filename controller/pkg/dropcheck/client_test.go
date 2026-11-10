package dropcheck

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/control"
	"github.com/mi2428/dropcheck-agent/controller/internal/session"
)

func TestTypedForgetAndOwnedOptions(t *testing.T) {
	ssid, err := WiFiForgetSSID("123")
	if err != nil || ssid.impl.Command.GetForgetWifi().GetSsid() != "123" || ssid.impl.Command.GetForgetWifi().GetTarget() != "" {
		t.Fatal("numeric SSID changed")
	}
	config, err := WiFiForgetConfigID(0)
	if err != nil || config.impl.Command.GetForgetWifi().GetSelector() == nil || config.impl.Command.GetForgetWifi().GetConfigId() != 0 {
		t.Fatal("known zero configuration ID lost")
	}
	if _, err := WiFiForgetSSID(""); err == nil {
		t.Fatal("empty SSID accepted")
	}
	if _, err := WiFiForgetConfigID(-1); err == nil {
		t.Fatal("negative ID accepted")
	}
	via := []string{"192.0.2.1"}
	op, err := Traceroute(TracerouteOptions{Host: "example.test", Via: via})
	if err != nil {
		t.Fatal(err)
	}
	via[0] = "changed"
	if op.impl.Options.TracerouteRequiredHops[0] != "192.0.2.1" {
		t.Fatal("constructor borrowed caller's slice")
	}
}

func TestPublicNumericAndDurationBounds(t *testing.T) {
	for _, duration := range []time.Duration{-time.Millisecond, time.Nanosecond, time.Millisecond + time.Nanosecond, time.Duration(math.MaxInt32+1) * time.Millisecond, time.Duration(math.MaxInt64)} {
		if _, err := Ping(PingOptions{Host: "example.test", Timeout: duration}); err == nil {
			t.Fatal("invalid duration accepted")
		}
	}
	for _, duration := range []time.Duration{0, time.Millisecond, time.Duration(math.MaxInt32) * time.Millisecond} {
		if _, err := Ping(PingOptions{Host: "example.test", Timeout: duration}); err != nil {
			t.Fatal(err)
		}
	}
	op, err := Ping(PingOptions{Host: "192.0.2.1"})
	if err != nil || op.impl.Command.GetPing().Count != 3 || op.impl.Command.GetPing().TimeoutMs != 9000 {
		t.Fatal("ping defaults changed")
	}
	for _, opts := range []PingOptions{{Host: ""}, {Host: "--option"}, {Host: "example.test", Count: math.MaxUint32}, {Host: "example.test", Size: math.MaxUint32}, {Host: "example.test", Count: math.MaxInt32}} {
		if _, err := Ping(opts); err == nil {
			t.Fatal("invalid ping accepted")
		}
	}
	if _, err := Traceroute(TracerouteOptions{Host: "example.test", MaxHops: 256}); err == nil {
		t.Fatal("hop limit accepted")
	}
	if _, err := HTTP("example.test", 600, 0); err == nil {
		t.Fatal("HTTP status accepted")
	}
	if _, err := PathMTU(PathMTUOptions{Host: "example.test", MinMTU: 1500, MaxMTU: 1000}); err == nil {
		t.Fatal("MTU bounds accepted")
	}
	if _, err := Network(0); err == nil {
		t.Fatal("Network zero accepted")
	}
	if _, err := WiFiMonitor(121*time.Second, time.Second); err == nil {
		t.Fatal("monitor duration would be silently clamped")
	}
	if _, err := WiFiMonitor(time.Second, time.Millisecond); err == nil {
		t.Fatal("monitor interval would be silently clamped")
	}
	if _, err := WiFiCycle(WiFiCycleOptions{WiFiConnectOptions: WiFiConnectOptions{SSID: "Lab", Passphrase: "12345678"}, Count: 101}); err == nil {
		t.Fatal("cycle count accepted")
	}
}

func TestEveryNetworkSelectorIsTypedAndDetached(t *testing.T) {
	ping, _ := Ping(PingOptions{Host: "192.0.2.1"})
	trace, _ := Traceroute(TracerouteOptions{Host: "192.0.2.1"})
	mtu, _ := PathMTU(PathMTUOptions{Host: "192.0.2.1"})
	ip, _ := GlobalIP("all", 0)
	dns, _ := DNS("example.test", "ALL", 0)
	http, _ := HTTP("example.test", 0, 0)
	download, _ := Download("https://example.test/file", 0)
	for _, op := range []Operation{IPStatus(), ping, trace, mtu, ip, dns, http, download} {
		bound, err := op.WithNetwork(NetworkSelector{SSID: " 123 日本語 ", BSSID: "aa:bb:cc:dd:ee:ff", NetworkID: 42})
		if err != nil {
			t.Fatal(err)
		}
		cmd, _, _ := command.BuildRunCommand(bound.impl)
		selector := *command.NetworkSelectorField(cmd)
		if selector.GetSsid() != " 123 日本語 " || selector.GetNetworkId() != "42" {
			t.Fatal("selector changed")
		}
		original, _, _ := command.BuildRunCommand(op.impl)
		if (*command.NetworkSelectorField(original)).GetNetworkId() != "" {
			t.Fatal("WithNetwork mutated original operation")
		}
		if _, err := op.WithNetwork(NetworkSelector{NetworkID: -1}); err == nil {
			t.Fatal("negative ID accepted")
		}
		if _, err := op.WithNetwork(NetworkSelector{BSSID: "invalid"}); err == nil {
			t.Fatal("invalid BSSID accepted")
		}
	}
	if _, err := WiFiStatus().WithNetwork(NetworkSelector{}); err == nil {
		t.Fatal("unsupported selector silently discarded")
	}
}

func TestClientZeroClosedAndExecutionRevalidation(t *testing.T) {
	for _, client := range []*Client{nil, {}} {
		if _, err := client.Run(t.Context(), Agent{}, WiFiStatus()); !errors.Is(err, ErrClientNotOpen) {
			t.Fatal(err)
		}
		if err := client.Close(); err != nil {
			t.Fatal(err)
		}
	}
	lifetime, cancel := context.WithCancel(t.Context())
	client := &Client{session: &session.Session{Server: control.NewServer("synthetic", nil)}, lifetime: lifetime, cancel: cancel}
	op, _ := Ping(PingOptions{Host: "example.test"})
	op.impl.Command.GetPing().Host = "--invalid"
	if _, err := client.Run(t.Context(), Agent{}, op); err == nil || !strings.Contains(err.Error(), "host") {
		t.Fatalf("validation did not precede dispatch: %v", err)
	}
	if client.Close() != nil {
		t.Fatal("Close failed")
	}
	if _, err := client.Run(t.Context(), Agent{}, WiFiStatus()); !errors.Is(err, ErrClientClosed) {
		t.Fatal(err)
	}
	if client.Agents() != nil {
		t.Fatal("closed snapshots returned")
	}
}
