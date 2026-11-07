# Go API

`github.com/mi2428/dropcheck-agent/controller/pkg/dropcheck` exposes basic Android agent operations without a CLI, scenario DSL, public watch plan or public transport. Internal Shell and watch use the same session factory and the single `internal/acquire` atomic/compound acquisition implementation. The public package and Shell call acquisition directly; watch adds only retry/expectation/report policy through a private adapter. Acquisition does not depend on watch, UI, rendering, session startup or this public facade. The canonical module path identifies the controller submodule; this change does not publish a remote version.

## Local consumer setup

In a separate consumer module, use a local replacement (adjust the absolute path):

```go
module example.test/consumer

go 1.26.0

require github.com/mi2428/dropcheck-agent/controller v0.0.0

replace github.com/mi2428/dropcheck-agent/controller => /absolute/path/to/dropcheck-agent/controller
```

```go
package main

import (
    "context"
    "errors"
    "fmt"
    "os"
    "time"

    "github.com/mi2428/dropcheck-agent/controller/pkg/dropcheck"
)

func inspect(ctx context.Context) (err error) {
    client, err := dropcheck.Open(ctx, dropcheck.Options{Serial: "DEVICE_SERIAL"})
    if err != nil {
        return err // Includes any partial-startup cleanup failure.
    }
    defer func() { err = errors.Join(err, client.Close()) }()

    agents := client.Agents()
    if len(agents) == 0 {
        return errors.New("no connected agent")
    }
    probe, err := dropcheck.Ping(dropcheck.PingOptions{
        Host: "192.0.2.1", Count: 1, Timeout: 5 * time.Second,
    })
    if err != nil {
        return err
    }
    probe, err = probe.WithNetwork(dropcheck.NetworkSelector{SSID: "123", NetworkID: 42})
    if err != nil {
        return err
    }
    result, err := client.Run(ctx, agents[0], probe)
    if err != nil {
        return err // result.Parts can still contain partial acquisitions.
    }
    if result.Raw == nil || result.Raw.GetStatus() != dropcheck.StatusOK {
        return errors.New("agent acquisition failed")
    }
    for _, finding := range result.Findings {
        if !finding.Passed || finding.Missing {
            return errors.New("acquisition evidence failed validation")
        }
    }
    ping := result.Raw.GetPing()
    if ping == nil || ping.Received == nil {
        return errors.New("received packet observation is missing")
    }
    fmt.Println("received:", *ping.Received)
    return nil
}

func main() {
    ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
    defer cancel()
    if err := inspect(ctx); err != nil {
        // inspect has already observed Close, including failure after success.
        fmt.Fprintln(os.Stderr, err)
    }
}
```

Do not call `log.Fatal`/`os.Exit` inside a function relying on deferred cleanup. Return first, preserving both operation and cleanup errors. `ExampleOpen` compiles this lifetime/selector/result pattern; the synthetic `ExampleResult` and `ExampleOperation_WithNetwork` also run without hardware.

## Versioned consumption

The local recipe uses `v0.0.0` as a placeholder resolved by the replacement. Once an actual `controller/vX.Y.Z` release tag exists, a consumer can remove the replacement and require `github.com/mi2428/dropcheck-agent/controller vX.Y.Z`, selecting a published version. No remote tag or publication is claimed here.

This unsuffixed module path supports v0/v1 module versions. A future v2+ Go API requires Go's major-version path suffix (such as `/v2`); the product's operator-surface version does not automatically choose the Go module version.

`tools/test_go_distribution.py` separately builds a synthetic `v0.0.0` archive in an isolated local file proxy and checks a consumer with no replacement or network access. It verifies the versioned module layout, typed API and absence of watch/UI dependencies, not remote availability.

## Ownership and lifetime

- `Open(ctx, Options) (*Client, error)` starts an authenticated session. Empty ADB path/package/listen address use `adb`, `DefaultPackageName`, and `127.0.0.1:0`. Explicit `Serial` skips discovery; otherwise only ready inventory rows are started. The library does not read `ADB_SERIAL` or credential environment variables. Startup context cancellation stops startup, not a successfully returned client's lifetime.
- `Devices(ctx, adbPath) ([]Device, error)` returns all inventory states without service startup or reverse forwarding. The caller owns each `Details` map. In-flight startup/inventory cancellation and deadlines support `errors.Is`, without exposing token-bearing subprocess errors.
- `client.Agents() []Agent` explicitly acquires current, detached, credential-free snapshots. Slices and capabilities are caller-owned. No `Server`, `Token`, `Hello` or embedded session is public. Editing snapshot fields never changes routing or authorizes capabilities. A snapshot belongs to its originating client/connection; stale snapshots fail rather than silently selecting a replacement. Fetch a new snapshot to reselect deliberately.
- `client.Run(ctx, agent, operation) (Result, error)` permits concurrent calls and cancels an individual call when its context ends. Nil/zero clients return `ErrClientNotOpen`; clients being closed return `ErrClientClosed`. A Client must not be copied after use.
- `client.Close() error` is concurrent-safe and idempotent. It cancels active Run calls, stops transport, and removes reverse rules without disconnecting Wi-Fi or forgetting profiles. Device-specific failures are aggregated; every call returns the same saved error, without retries. Reverse removals share a five-second deadline and run concurrently, with up to one extra second for subprocess reaping (plus bounded server completion). A failed Open includes partial cleanup errors.

## Operations and validation

Operations are opaque immutable values, copied safely for concurrent reuse. The zero value is invalid. There is no public raw command or presentation-options escape. Constructors perform pure validation; Run revalidates before dispatch. Operations cover Wi-Fi status/diagnostics/capabilities/scan/EHT/connect/disconnect/forget/wait/assert/monitor/reconnect/cycle, Networks/IP/permissions, ping/traceroute/PMTU/public IP/DNS/HTTP/download. There are no scenario retries.

- Numeric inputs use Go numbers, not numeric strings. Zero means the documented default; positive values must fit Android's signed 32-bit range, with narrower cycle count/pause, traceroute-hop and HTTP-status limits. `Network(id)` requires a positive Android Network ID; `DefaultNetwork()` is explicit.
- Durations use `time.Duration`: zero selects the documented default, except `WiFiAssert` where zero is immediate. Nonzero values must be positive whole milliseconds, at most `2147483647ms`. Negative, fractional-millisecond and overflowing values fail; no truncation or wrapping occurs. See each constructor/option's godoc for exact defaults.
- `operation.WithNetwork(NetworkSelector{SSID: ..., BSSID: ..., NetworkID: ...})` returns a separate bound operation for IPStatus or any network probe. Zero selector means default network. Selectors are checked at selection time, not proof of uninterrupted association. SSID remains literal, including spaces, Unicode and numeric names. `NetworkID: 0` is unspecified, not a saved-profile ID.
- `WiFiForgetSSID("123")` and `WiFiForgetConfigID(0)` return `(Operation, error)` and preserve the distinction between a numeric SSID and known configuration ID zero. Both require the **actual connection's** `wifi.forget.typed` capability; no ambiguous legacy fallback exists.
- Connect/cycle credentials must be 8..63 valid UTF-8 bytes without control characters, or 64 hex digits. Empty/short/control-containing/invalid-UTF-8/non-hex credentials fail before dispatch. Empty/`auto` security asks Android to infer PSK/SAE from cached scans; it does **not** request an open network or waive credentials. Shell's separate explicit/default-credential resolution is unchanged. Passphrases and SSIDs are never trimmed.
- Constructors copy retained input slices, such as `TracerouteOptions.Via`. WiFiEHT with `Fresh` retains both required scan/diagnostic acquisitions and independent per-part results. EHT results are unfiltered acquisition data; SSID/BSSID filters are Shell presentation options, not public API inputs.

## Results

`Result`, `Part` and `Finding` are public owned types with no watch scope, plan, display options or transport members. `CommandResult`, status/family/DNS enums, primary success-payload types, supporting observation types and `*Payload` wrappers are intentionally nameable from this package. Protocol commands, stream frames and transport services are not re-exported; detailed protocol extensions are not automatically made public.

Go errors indicate validation/transport failures. Agent failures remain in `Raw.Status`. A reported `StatusOK` can still have a missing/inconsistent-payload Finding. Inspect both. `Finding.Missing` means unavailable evidence, not an observed zero or false. Optional protobuf pointers preserve known zero/false versus nil; getters alone do not. Non-optional scalars require the accompanying observation metadata to establish availability.

`Parts` records each attempted acquisition in execution order with pinned AgentID/SessionID, command ID and host Started/Ended times. Their difference is host elapsed time; `Raw.ElapsedMs` is agent-reported runtime. `Raw` and per-part payloads are separate snapshots. A transport error can return partial Parts with nil Raw.

All raw payloads, findings and logs are **acquisition data, not presentation-sanitized output**. Protect sensitive values before logging/exporting. Mutating an acquired result does not affect later runs or the client's private state.

## Logging

`Options.OnLog func(LogEvent)` receives all agent log levels, including DEBUG accepted-command notifications. Calls run inline on stream goroutines and may overlap across agents; handlers must be concurrency-safe, short and nonblocking. A blocked handler delays that stream's result delivery. Do not synchronously call Close from a handler. No extra queue or panic recovery is added.

A nil callback filters DEBUG/INFO and writes other levels (including unspecified/unknown) to stderr. Discovery, skipped-device and startup/connected diagnostics always use stderr separately, even with a custom callback. Event times are peer-provided, or host receipt time when omitted.

## Maintained boundary checks

`TestExternalModuleBasicAPI` creates a separate consumer module using the replacement above. Fake ADB processes and loopback gRPC peers exercise nameable success payloads, public-member restrictions, literal typed selectors, snapshot/result isolation, capability forgery refusal, stale and foreign snapshots, successful/failed/malformed Run results, concurrent Run/Close, repeated successful/failed Close, callback levels, and pre-/mid-flight cancellation. ADB's marker-synchronized tests separately cover cancellation/deadline classification together with token redaction; session tests cover partial startup and bounded multi-device cleanup. These checks do not use installed ADB, physical devices or external network probes.
