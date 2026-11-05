# Basic Go API

The public package is `github.com/mi2428/dropcheck-agent/controller/pkg/dropcheck`. It exposes the same basic typed operations used by PC Shell and watch, without a one-shot CLI, Go test DSL, public Plan or public watch runner. Existing internal operation builders, validation, authenticated transport and compound-acquisition handling remain the implementation; the public package does not duplicate them.

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/mi2428/dropcheck-agent/controller/pkg/dropcheck"
)

func main() {
    ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
    defer cancel()
    client, err := dropcheck.Open(ctx, dropcheck.Options{Serial: "DEVICE_SERIAL"})
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    result, err := client.Run(ctx, client.Agents[0], dropcheck.WiFiStatus())
    if err != nil {
        log.Fatal(err)
    }
    if result.Raw.GetStatus() != dropcheck.StatusOK {
        log.Fatalf("acquisition status: %s", result.Raw.GetStatus())
    }
    for _, finding := range result.Findings {
        if !finding.Passed {
            log.Fatalf("invalid acquisition: %s", finding.Metric)
        }
    }
    fmt.Println(result.Raw.GetWifiStatus().GetState())
}
```

## Operations and lifetime

- `Open` uses an explicit `Options.Serial` when supplied, otherwise discovers ready ADB devices. Select an acquired `Agent` from `Client.Agents`; execution pins its current session rather than silently selecting another agent. `Options.OnLog` accepts `func(dropcheck.LogEvent)`.
- `Devices(ctx, adbPath)` lists the full ADB inventory, including offline/unauthorized devices, without starting services or reverse forwarding. An empty path selects the normal `adb` executable.
- `Run` accepts a basic `Operation` and returns a typed `Result`. Wi-Fi status/diagnostics/capabilities, scan/EHT, Networks/IP/permissions, connect/disconnect/forget/wait/assert/monitor/reconnect/cycle, ping, traceroute, PMTU, external IP, DNS, HTTP and download have constructors in this package.
- Constructor option aliases reuse the current operation builders, including their string-valued numeric options and millisecond timeout fields. Invalid input is finally validated before dispatch. For example, `Ping(PingOptions{Host: "192.0.2.1", Count: "1", Timeout: "5000"})` returns an operation and an error. There is no command-string parsing step.
- `WiFiForgetSSID("123")` keeps the numeric SSID literal; `WiFiForgetConfigID(0)` selects a known zero configuration ID. Typed forgetting requires the agent capability and never falls back to ambiguous legacy target interpretation.
- `NetworkSelector` is available for typed per-Network operation selectors. Command results and status constants are also nameable from this single public package; callers need not import any `internal` package.
- Cancel the context to cancel startup or an operation. `Close` releases the transport and ADB reverse rules; it is idempotent and does not disconnect Wi-Fi or forget a saved network. Wi-Fi mutation remains an explicit operation.
- `Run` does not add scenario retries. An explicitly requested fresh EHT operation retains its required scan/diagnostic acquisitions and per-part results, using the same path as Shell/watch.

Transport/input failures are returned as errors. Agent outcomes remain in `Result.Raw.Status`; acquisition-validation findings remain in `Result.Findings`, including missing or inconsistent payloads. Inspect both rather than inferring success from text or treating absent data as zero/false. `Result.Parts` retains constituent acquisition identities and timing.

Returned protobuf payloads are acquisition data, not presentation-sanitized text. Consumers must protect credentials and other sensitive values before logging or exporting raw payloads. The existing Shell presentation and widget safety boundaries are unchanged.

`TestExternalModuleBasicAPI` compiles and exercises the public API from a separate temporary Go module using a local module replacement. It checks public types, constructors, typed outcomes and cancellation without ADB or a device. In this repository the module path remains `github.com/mi2428/dropcheck-agent/controller`; external consumers must resolve that module using their existing local/workspace/module replacement setup.
