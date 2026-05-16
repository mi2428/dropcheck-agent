# dropcheck-agent

ADB-controlled Android probes for automated Wi-Fi and access-network end-to-end tests. Born at ShowNet, built for every event NOC.

## Overview

Android handsets act as field probes for event NOCs.
Instead of checking connectivity from the infrastructure side, each check measures from the same user-facing access network that attendees and staff devices use.

The controller is the operator-facing entry point for live checks and automation.
The agent is the on-device probe, and it can also keep collecting standalone measurements when the controller is absent.
Saved measurements can be exported into the event observability stack for after-the-fact review.

```mermaid
sequenceDiagram
  autonumber
  actor NOC as NOC
  participant Controller as Controller
  participant App as Android App
  participant WiFi as Wi-Fi under test
  participant O11y as O11y Stack
  participant MCP as MCP Server
  actor AI as AI Agent

  par NOC interactive control through Controller shell
    NOC->>Controller: shell command
    Controller->>App: ADB-started gRPC operation
    App->>WiFi: connect, inspect, or probe
    WiFi-->>App: link and probe result
    App-->>Controller: typed result
    Controller-->>NOC: shell output
  and AI interactive control through MCP Server
    AI->>MCP: MCP tool call
    MCP->>Controller: controller operation
    Controller->>App: ADB-started gRPC operation
    App->>WiFi: inspect or probe
    WiFi-->>App: link and probe result
    App-->>Controller: typed result
    Controller-->>MCP: tool result
    MCP-->>AI: structured response
  and Standalone festa results collected into O11y
    NOC->>Controller: configure and run standalone festa
    Controller->>App: persist config or run once
    App->>WiFi: scheduled checks
    WiFi-->>App: measurement result
    App->>O11y: upload standalone archive
    O11y-->>NOC: metrics and dashboards
  and NOC direct Android live mode
    NOC->>App: on-device live mode command
    App->>WiFi: connect using stored target
    WiFi-->>App: connection result
    App-->>NOC: terminal, widget, local log
  end
```

## Requirements

For the Android agent, use an Android 12+ test device with USB debugging enabled. The debug APK is installed over ADB:

```console
$ make install SERIAL=35251JEHN00258
```

Wi-Fi provisioning uses Android privileged Wi-Fi APIs. For `wifi connect`, `wifi cycle`, and `wifi forget`, make the app a device owner on a fresh, unmanaged test device.

> [!WARNING]
> Device Owner mode is for dedicated test handsets. Do not enable it on a personal daily-driver device. Android can prevent removing a Device Owner unless the APK is `android:testOnly`; the debug APK installed by `make install` is test-only, but a release or manually modified APK may require a factory reset to remove once it owns the device.

Before setting Device Owner, the device must have no existing Device Owner or Profile Owner, no secondary users, and no local accounts. Removing a Google account from the web is not enough; remove every account from **Settings > Passwords & accounts** on the device, then verify:

```console
$ adb -s 35251JEHN00258 shell dpm list-owners
$ adb -s 35251JEHN00258 shell pm list users
$ adb -s 35251JEHN00258 shell dumpsys account | grep -E 'Accounts:|Account \{'
```

If `dumpsys account` still shows accounts, remove them from device Settings. If Device Owner setup still fails with an already set-up/provisioned-device error, use a freshly factory-reset test device, or reset a test device with Android's test harness mode:

```console
$ adb -s 35251JEHN00258 shell cmd testharness enable
```

`cmd testharness enable` performs a factory reset. After the reset, skip account sign-in during setup, enable USB debugging, install the agent, and set Device Owner:

```console
$ make install SERIAL=35251JEHN00258
$ adb -s 35251JEHN00258 shell dpm set-device-owner io.dropcheck.agent/.DeviceAdminReceiver
$ adb -s 35251JEHN00258 shell dpm list-owners
```

For debug APKs installed by `make install`, Device Owner can usually be removed during test teardown with:

```console
$ adb -s 35251JEHN00258 shell dpm remove-active-admin io.dropcheck.agent/.DeviceAdminReceiver
```

Grant Wi-Fi visibility permissions, or open the app once and approve the prompts:

```console
$ adb -s 35251JEHN00258 shell pm grant io.dropcheck.agent android.permission.ACCESS_FINE_LOCATION
$ adb -s 35251JEHN00258 shell pm grant io.dropcheck.agent android.permission.ACCESS_BACKGROUND_LOCATION
$ adb -s 35251JEHN00258 shell pm grant io.dropcheck.agent android.permission.NEARBY_WIFI_DEVICES
```

Keep Location enabled on the device; Android hides SSID, BSSID, scan, and MLO details without it.

## Features

The controller/agent toolchain has several entry points that share the same typed agent operations.
Use the controller for ad-hoc checks, the shell for field work, MCP for model/tool orchestration, standalone mode for unattended handset-side runs, and Festival when the check should be a repeatable Go test.

### Controller CLI and shell

The controller starts an ADB-backed gRPC session to one or more Android agents.
One-shot CLI commands are scriptable and can emit text or JSON; the interactive shell adds prompts, completion, context help, output filters, and configure/request submodes.

The controller builds three binaries:

```console
$ make build TARGET=controller
+ mkdir -p dist
+ go build -ldflags -X\ dropcheck/controller/internal/version.Version=0.9.0-dirty -o dist/dropcheck ./cmd/dropcheck
+ mkdir -p dist
+ go build -ldflags -X\ dropcheck/controller/internal/version.Version=0.9.0-dirty -o dist/dropcheck-mcp ./cmd/dropcheck-mcp
+ mkdir -p dist
+ go build -ldflags -X\ dropcheck/controller/internal/version.Version=0.9.0-dirty -o dist/dropcheck-ingester ./cmd/dropcheck-ingester
```

`dist/dropcheck` supports:

- **Wi-Fi and IP inspection:** `wifi status`, `wifi diagnostics`, `wifi mlo`, `wifi scan`, `wifi capabilities`, and `ip status`.
- **Wi-Fi control:** `connect`, `disconnect`, `forget`, `wait connected`, `assert`, `reconnect`, and `cycle`.
- **Network probes from the handset:** `ping`, `traceroute`, `path-mtu`, `global-ip`, `dns`, `http`, and `download`.

Common examples:

```console
$ controller/dist/dropcheck --serial R5CT12345 shell
$ controller/dist/dropcheck --serial R5CT12345 --format json show wifi status
$ controller/dist/dropcheck --serial R5CT12345 show wifi scan fresh all --timeout 9000
$ controller/dist/dropcheck --serial R5CT12345 request ping 1.1.1.1 --count 5
```

A short interactive session, with verbose startup lines omitted and network values shown as examples:

```console
$ controller/dist/dropcheck --serial R5CT12345 shell
dropcheck: selected agent=R5CT12345
press '?' for context help, or type 'help' for commands
R5CT12345# show wifi ?
  status                   Current Wi-Fi connection and IP state
  diagnostics              Wi-Fi status, capabilities, networks, and scan
  mlo                      Connected and nearby MLO state
  scan                     Cached or fresh scan results
  capabilities             Device Wi-Fi capabilities
R5CT12345# show wifi status | match "^  (ssid|bssid|band|validated)[[:space:]]"
  ssid                           ShowNet
  bssid                          aa:bb:cc:dd:ee:ff
  band                           6ghz
  validated                      true
R5CT12345# request
R5CT12345(request)# ping 1.1.1.1 count 3 | match "^Ping:"
Ping: host=1.1.1.1 status=ok transmitted=3 received=3 loss=0.0% min/avg/max=10.20/12.40/16.30ms interface=wlan0 elapsed=428ms
R5CT12345(request)# exit
R5CT12345# exit
```

### Android agent

Install the Android agent on a test handset:

```console
$ make install SERIAL=R5CT12345
+ ./gradlew :agent:assembleDebug -PdropcheckVersion=0.9.0-dirty
+ adb -s R5CT12345 install -r -t agent/build/outputs/apk/debug/agent-debug.apk
```

The agent executes controller requests, records structured local logs, renders widgets, and can run a small on-device shell.
Its `use NAME` command connects to Wi-Fi targets configured under the standalone `live` festa, which is useful when a handset is in the field without the controller attached.

Drive the agent from the controller for live measurements:

```console
$ controller/dist/dropcheck --serial R5CT12345 show devices
SEL  #  AGENT    ADB SERIAL  DEVICE              SDK  APP    CONNECTED
*    1  agent-1  R5CT12345   Google Pixel 9      35   0.9.0-dirty  2026-05-06T09:00:00Z

$ controller/dist/dropcheck --serial R5CT12345 show wifi scan fresh all --timeout 9000
Latency: 1420ms
Wi-Fi Scan
  requested_band                 all
  results                        2
  total                          2
  errors                         0
  fresh_scan_wait_completed      true
  fresh_scan_elapsed_ms          1382

SSID     BSSID              RSSI  BAND  FREQ  STANDARD  SECURITY  FLAGS  AP_MLD             AP_LINK  AFFILIATED
ShowNet  aa:bb:cc:dd:ee:ff  -48   6ghz  6135  11be      wpa3_sae  -      02:00:00:00:00:01  1        2
ShowNet  11:22:33:44:55:66  -55   5ghz  5745  11ax      wpa3_sae  -      <none>             -        0

$ controller/dist/dropcheck --serial R5CT12345 request ping 1.1.1.1 --count 5
Latency: 634ms
Ping: host=1.1.1.1 status=ok transmitted=5 received=5 loss=0.0% min/avg/max=10.20/12.40/16.30ms interface=wlan0 elapsed=634ms

5 packets transmitted, 5 received, 0% packet loss
rtt min/avg/max/mdev = 10.200/12.400/16.300/1.900 ms
```

### MCP server

`controller/dist/dropcheck-mcp` runs an MCP stdio server backed by the same controller session machinery.
A smoke check can initialize the stdio server and list the current tool inventory without starting an Android session:

```text
$ (
  printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"readme-smoke","version":"0.1.0"}}}'
  sleep 0.5
  printf '%s\n' '{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}'
  printf '%s\n' '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'
  sleep 0.5
) | controller/dist/dropcheck-mcp 2>/dev/null \
  | jq -r 'select(.id==1).result.serverInfo.name, select(.id==2).result.tools[].name'

dropcheck-mcp
dropcheck_adb_diagnostics
dropcheck_agents
dropcheck_command
dropcheck_dns
dropcheck_download
dropcheck_global_ip
dropcheck_http
dropcheck_ip_status
dropcheck_path_mtu
dropcheck_ping
dropcheck_run
dropcheck_session_start
dropcheck_session_stop
dropcheck_standalone_clear_runs
dropcheck_standalone_config
dropcheck_standalone_config_edit
dropcheck_standalone_run
dropcheck_standalone_run_once
dropcheck_standalone_runs
dropcheck_standalone_status
dropcheck_traceroute
dropcheck_wifi_assert
dropcheck_wifi_capabilities
dropcheck_wifi_connect
dropcheck_wifi_cycle
dropcheck_wifi_diagnostics
dropcheck_wifi_disconnect
dropcheck_wifi_forget
dropcheck_wifi_mlo
dropcheck_wifi_monitor
dropcheck_wifi_reconnect
dropcheck_wifi_scan
dropcheck_wifi_scan_detail
dropcheck_wifi_status
dropcheck_wifi_wait_connected
```

It also exposes `dropcheck://session` and `dropcheck://agents` resources; standalone resource templates for config, status, runs, and one run archive; and prompts for connectivity, MLO investigation, and NOC smoke checks.

The comprehensive MCP live test starts `dropcheck-mcp` through MCP CommandTransport and drives a real Android agent over ADB. It connects, disconnects, forgets Wi-Fi, edits standalone config, runs saved standalone archives, and clears synced archives, so run it only against a dedicated Device Owner test handset.

```console
$ export DROPCHECK_WIFI_PSK='...'
$ (
  cd controller
  DROPCHECK_E2E_LIVE=1 \
    ADB_SERIAL=R5CT12345 \
    DROPCHECK_E2E_WIFI_SSID=ShowNet \
    go test -tags 'e2e mcp_live_full' ./integration/mcp \
      -run TestMCPServerCommandTransportComprehensiveLive -count=1 -v
)
```

Use `-v` when debugging; the test logs every MCP protocol call, tool call, progress notification, and logging message with Wi-Fi passphrases redacted.
Use MCP when another tool should run checks without shelling out to the CLI grammar for every operation.
For host-file writes, use the CLI directly: `sync standalone runs` is intentionally not exposed through MCP.

MCP clients start the server over stdio. Build the controller first, then register or enable the binary with the client.
Keep Wi-Fi passphrases out of prompts and config files where possible; export an environment variable in the shell that launches the MCP client and ask the agent to use `passphrase_env`.
After changing MCP configuration, start a new Claude Code or Codex session from a shell with those environment variables exported.

#### Claude Code

Claude Code project-scoped MCP servers live in `.mcp.json` at the repository root. This repository includes a project-scoped `dropcheck` server that runs `${DROPCHECK_MCP_BIN:-./controller/dist/dropcheck-mcp}` and passes through `DROPCHECK_WIFI_PSK` when it is set. `.claude/` is for Claude Code settings, agents, commands, and local state; it is not the current project-scoped MCP server definition path.

Build the server binary, export any secrets in the shell that starts Claude Code, then verify or approve the server with `/mcp`:

```console
$ make build TARGET=controller
$ export DROPCHECK_WIFI_PSK='...'
$ claude
> /mcp
```

For a private per-project override instead of the checked-in `.mcp.json`, use local scope. Claude Code stores that in `~/.claude.json`, not in this repository:

```console
$ claude mcp add --transport stdio --scope local dropcheck -- "$PWD/controller/dist/dropcheck-mcp"
$ claude mcp get dropcheck
```

#### Codex

Register `dropcheck-mcp` as an MCP server:

```console
$ make build TARGET=controller
$ export DROPCHECK_WIFI_PSK='...'
$ codex mcp add dropcheck -- "$PWD/controller/dist/dropcheck-mcp"
$ codex mcp get dropcheck
```

### Standalone measurement and observability

Standalone mode stores "festa" configurations on the handset.
A festa contains Wi-Fi groups to connect to, wait policies, and DNS/ping/HTTP checks.
The agent can run them on a schedule or once on demand, then save a protobuf archive locally.

The controller can inspect and export those archives:

```console
$ controller/dist/dropcheck --serial R5CT12345 request standalone run once --festa shownet --save
$ controller/dist/dropcheck --serial R5CT12345 show standalone runs --limit 5
$ controller/dist/dropcheck --serial R5CT12345 sync standalone runs --output out/standalone --mark-synced
```

For unattended observability, the Android agent uploads standalone archives to MinIO-compatible storage.
`dist/dropcheck-ingester` consumes MinIO notifications or batch backfills, converts archives into metrics, and pushes them to Pushgateway for Prometheus and Grafana.

Configure uploads by setting a path-style bucket/prefix URL and the management Wi-Fi used before upload:

```console
$ controller/dist/dropcheck --serial R5CT12345 configure set standalone upload to http://192.168.50.10:8080/dropcheck/incoming
$ controller/dist/dropcheck --serial R5CT12345 configure set standalone upload via wifi essid NOC passphrase upload-secret security auto timeout 5s
```

### Festival DSL

Festival tests are Go tests that connect to a requested Wi-Fi target, wait for the expected link state, run typed checks, and fail with normal Go test output.
The DSL supports retries and stable checks, and it can also evaluate saved standalone archives without a connected Android agent.

Available check builders include Wi-Fi status, MLO diagnostics, scan and scan-detail, Wi-Fi capabilities, IP status, ping, DNS, HTTP, download, traceroute, path MTU, and global IP.

```go
//go:build festival

package festival_test

import (
	"testing"
	"time"

	f "dropcheck/controller/internal/festival"
	"dropcheck/controller/internal/festival/capabilities"
	"dropcheck/controller/internal/festival/dns"
	"dropcheck/controller/internal/festival/ip"
	"dropcheck/controller/internal/festival/ping"
	"dropcheck/controller/internal/festival/scan"
	"dropcheck/controller/internal/festival/wifi"
)

func TestShowNetWiFi(t *testing.T) {
	f.Run(t, f.Plan{
		Name: "shownet-wifi",
		Networks: []f.Network{
			// Connect to one AP, not just any AP advertising the SSID.
			f.WiFi("noc-6ghz").
				SSID("ShowNet").
				BSSID("aa:bb:cc:dd:ee:ff").
				PSKEnv("DROPCHECK_FESTIVAL_WIFI_PSK").
				Security("wpa3").
				Band("6ghz").
				// Wait until Android says the network has validated internet.
				RequireValidated(true).
				WaitTimeout(45 * time.Second).
				// Remove the test network from the handset during cleanup.
				ForgetAfter(true),
		},
		Checks: []f.Check{
			// Check the current Wi-Fi link after association.
			f.WiFiStatus().
				Expect(
					wifi.Enabled().IsTrue(),
					wifi.SSID().Eq("ShowNet"),
					wifi.BSSID().Eq("aa:bb:cc:dd:ee:ff"),
					wifi.Band().Eq("6ghz"),
					wifi.Standard().Eq("be"),
					wifi.TxLinkSpeedMbps().Ge(1000),
					wifi.AssociatedMLOLinkCount().Ge(1),
				).
				Retry(3, 2*time.Second),
			// Force a fresh scan and verify the target AP advertisement.
			f.WiFiScan().
				Fresh().
				Band("6ghz").
				Timeout(10*time.Second).
				Expect(
					scan.ResultCount().Ge(1),
					scan.APs().
						SSID("ShowNet").
						BSSID("aa:bb:cc:dd:ee:ff").
						Standard("be").
						ChannelWidth("320mhz").
						Security("wpa3_sae").
						Exists(),
				),
			// Assert that the handset can run the requested Wi-Fi mode.
			f.WiFiCapabilities().
				Expect(
					capabilities.Band("6ghz").Supported(),
					capabilities.Standard("be").Supported(),
					capabilities.Security("wpa3_sae").Supported(),
				),
			// Check layer-3 provisioning from Android's active network.
			f.IPStatus().
				Expect(
					ip.Validated().IsTrue(),
					ip.Internet().IsTrue(),
					ip.DefaultRoute().IsTrue(),
					ip.DNSServerCount().Ge(1),
					ip.MTU().Ge(1280),
				),
			// Run active reachability checks through the connected Wi-Fi.
			f.Ping("1.1.1.1").
				Count(5).
				Expect(
					ping.Received().Eq(5),
					ping.LossPercent().Eq(0),
					ping.AvgLatency().Le(50*time.Millisecond),
				).
				Retry(2, time.Second),
			// Confirm resolver behavior, not only raw IP reachability.
			f.DNS("www.wide.ad.jp").
				A().
				Expect(
					dns.AnswerCount().Ge(1),
					dns.Elapsed().Le(time.Second),
				),
		},
	})
}
```

```console
$ ADB_SERIAL=R5CT12345 DROPCHECK_FESTIVAL_WIFI_PSK=secret go test -tags festival -run TestShowNetWiFi -v ./integration/festival
=== RUN   TestShowNetWiFi
=== RUN   TestShowNetWiFi/shownet-wifi
=== RUN   TestShowNetWiFi/shownet-wifi/noc-6ghz
=== RUN   TestShowNetWiFi/shownet-wifi/noc-6ghz/connect
=== RUN   TestShowNetWiFi/shownet-wifi/noc-6ghz/wait_connected
=== RUN   TestShowNetWiFi/shownet-wifi/noc-6ghz/wifi_status
=== RUN   TestShowNetWiFi/shownet-wifi/noc-6ghz/wifi_scan_fresh
=== RUN   TestShowNetWiFi/shownet-wifi/noc-6ghz/wifi_capabilities
=== RUN   TestShowNetWiFi/shownet-wifi/noc-6ghz/ip_status
=== RUN   TestShowNetWiFi/shownet-wifi/noc-6ghz/ping_1.1.1.1
=== RUN   TestShowNetWiFi/shownet-wifi/noc-6ghz/dns_www.wide.ad.jp
--- PASS: TestShowNetWiFi (16.84s)
PASS
ok  	dropcheck/controller/integration/festival	17.208s
```

## License

MIT
