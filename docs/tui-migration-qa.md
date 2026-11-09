# TUI migration and interactive controls QA

Date: 2026-11-09. Source baseline: `3c64d6b`. No device, installed ADB, network probe, commit or push was used for these checks.

## Current contract

- Entry: `cd controller && go run ./cmd/dropcheck tui -c ../examples/tui.yml`. The former monitoring command is rejected, not aliased. This does not restore the removed standalone plan-selection/review workflow.
- Configuration, execution and UI-neutral state packages are `internal/tuiconfig`, `internal/tuirun`, and `internal/tuistate`. `internal/tui` remains the Bubble Tea presentation layer. The initial JSONL event is `tui_started`; the old serialized event spelling is not retained as a compatibility path.
- Scope/action selection uses stable agent/target/check IDs. Skip and Cancel require explicit `y`; Enter, `n` and Escape do not execute them. Cancel lasts for the remaining run, while Skip lasts for the current round. A child cannot resume a paused parent. Finished, unknown and canceled scopes fail closed.
- Control delivery runs in a `tea.Cmd`, not inside `Update`. The existing reliable event delivery and one-second delivery failure boundary are unchanged. Pause continues to consume the existing policy deadline.
- Details are frozen snapshots with viewport scrolling and explicit refresh/full-screen controls. A resize reflows the same snapshot; live events do not replace its text or reading position. The newest 500 retained history entries and 120 related logs are presentation windows, not reduced retention. JSONL is the full-stream export option.
- Bubble Tea is 2.1.1; Bubbles 2.2.1 and Lip Gloss 2.0.6 are retained. Existing Bubbles list/viewport components are reused, with their required indirect dependencies; no separate UI framework was introduced.

## Adversarial checks

| Boundary | Evidence |
| --- | --- |
| Old command refusal, new help, invalid options before startup | `app/tui_entry_qa_test.go`, `app/run_test.go`, `app/tui_lifecycle_test.go`; marker-only fake ADB remains untouched |
| Empty explicit target must not silently select all agents | Empty config/target/JSONL values and values on valueless flags are rejected |
| Rename completeness | `tools/test_tui.py` scans tracked/untracked owned source paths and text; only reserved protocol field names, unrelated observer prose, and explicit negative tests are exempt; generated descriptors are not rewritten |
| Actual producer/reducer pause state | `tuistate/control_progress_test.go`: current round, single/multi-agent Pause/Resume, completed peer and skipped-target identity |
| Scoped control and confirmation | `tui/actions_test.go`, `tuirun/controls_state_test.go`: identical checks on different agents, exact scope, parent pause, canceled/finished/unknown rejection and explicit confirmation |
| Full event channel | `TestCtrlZUpdateReturnsWhileTheEventChannelIsFull` verifies capacity one is full, `Update` returns within 100ms, asynchronous delivery completes while events are drained, and the run stays alive |
| Snapshot, limits and safe values | `tui/inspector_test.go`: live updates, scroll, refresh, resize, full screen, source retention, credential/control projection and literal SSID preservation |
| Cache invalidation | `tui/frame_test.go`: history events invalidate derived data, ticks expire time-window summaries, log/repaint reuse is safe, and filtering never mutates cached rows |
| Real terminal behavior | `tools/test_tui.py`: actual Bubble Tea program and input decoder in a private PTY, synthetic runners only; scoped pause/resume, confirmation dismissal, snapshot scrolling/live events, resize, global pause/resume, Escape and clean shutdown |
| Existing public/Shell boundaries | Offline versioned module consumer excludes all private TUI/config/runtime/state/render packages; existing real-readline PTY recall/replay and cancellation checks remain in `make test TARGET=controller` |

The race-instrumented scheduling test uses the actual model/event loop without terminal layout in its test wrapper. Real layout and decoding are exercised separately by the PTY test. This keeps a control-scheduling deadline independent of the substantial race-instrumentation cost of rendering every synthetic event.

## Performance

Apple M4 Pro, Go 1.26.8, 160x48, eight checks, 20,000 successful plus 2,000 failed synthetic history entries. Final figures are medians of three samples, five benchmark iterations per sample. The earlier audit used an equivalent synthetic workload; these are local measurements, not production latency guarantees.

| Path | Earlier audit | Current |
| --- | --- | --- |
| Cold View | 41.133ms / 38.787MiB allocated | 32.582ms / 18.101MiB |
| Unchanged repaint | No separate warm-cache measurement | 9.400ms / 2.931MiB |
| Tab after derived rows are available | 4.765ms / 5.377MiB | 5.492us / 4.509KiB |

`BenchmarkTUIFrame` explicitly separates cold computation, repaint, and keyboard movement. The improvement comes from shared derived rows, event/tick invalidation, removing oversized aggregate preallocation and temporary timeline copies, and typed attempt keys. Retained histories and measurements are not dropped to obtain these results.

## Verification notes

- The full controller Make target passed with `GOFLAGS=-p=1`, including Go packages, generated Shell contract drift, offline distribution, Shell PTY and TUI PTY checks. The first parallel run hit two existing fake-process deadlines in `adb`/`adbdiag`; both passed in isolation and in the serial full run, without increasing deadlines or editing those tests.
- `go vet` and Staticcheck 2026.2.2 passed. Staticcheck was built offline from the already cached module. Its subprocess `go` and `GOROOT` must match Go 1.26.8; using the unrelated default Go 1.27 toolchain with a Go 1.26-built analyzer fails before analysis.
- The changed app/TUI/config/runtime/state packages pass `go fix -diff` with no suggestions. Repository-wide `make modernize-check` still reports existing suggestions in unchanged `internal/render/render_test.go`; that unrelated file was deliberately left alone.
- The pre-existing README Gradle version and both Wrapper changes were preserved. Reconstructing the original three-file diff reproduces SHA256 `a353c20ce86c9d06a7e5fca7b3b65d80fc5c560d805e0ee9ac17ddc261e71746`.
- Final `go test -count=1 ./...` and `go test -race -count=1 ./...` both passed with `GOFLAGS=-p=1`. Final TUI PTY/name checks, `make lint`, `make fmt-check`, `go mod tidy -diff`, scoped `go fix -diff`, and `git diff --check` also passed. The additional empty-option and resize-dismissal regressions passed under race instrumentation. Real Android rendering, transport and device measurements were not exercised.
