# 2.0 surface reduction

Date: 2026-11-05. Baseline: `02a9e8c`. Keep PC/Android Shell, Android widgets and `dropcheck tui`; remove standalone one-shot CLI, the separate `dropcheck tui` workflow and the Go test-authoring DSL. Ordinary regression tests remain part of development.

## Implementation boundary

- Removed the CLI router/parser/aliases and separate Plan selection/Preview/Once/N/Review UI, including unused selection/rerun runtime and dedicated fixtures. Retired roots fail before ADB startup, without compatibility aliases.
- Removed the `testing.T` adapter, Go scenario builders and fluent metric/selector subpackages. The required tui execution, flat YAML expectations, controls, timing, typed acquisitions and cleanup remain private in `internal/tuirun`; this is not a renamed public harness.
- Preserved tui YAML, monitoring dashboard, headless mode and JSONL, along with Shell typed failures, source/target retention, credentials, full input/draft recall, filters and full-safe-value access. Surviving E2E rows retain their original IDs/content/order; only retired CLI rows were removed.
- A single [public Go package](../go-api.md) reuses existing connection, operation and acquisition code. It exposes device inventory and basic operations, not scenario/tui authoring. An external-module test imports no internal package. Public typed SSID/configuration-ID forgetting remains unambiguous.
- Android source, protocol, shared Shell grammar and dependency versions are unchanged. No device installation, live scan/probe or network configuration operation was performed.

## Review findings

The first reduction retained runtime safety regressions while replacing their DSL setup with plain data. The supervisor independently ran tui/core/Shell/dashboard tests. Later review removed leftover test-only one-shot modes from the app layer instead of keeping obsolete branches merely for old tests.

A new E2E restore path initially used Go string quoting for Shell input. A runnable Unicode fixture showed that the Shell lexer treated an escaped ZWJ as literal `u200d`, changing the SSID. This was returned for correction at the shared quoting/input boundary, with literal-value and typed-envelope checks retained.

## Final checks and size

Final review is complete. Size is measured as physical Go source lines against the baseline, excluding generated `controlpb` files and separating `*_test.go`; moves and newly added public API files are included.

| Source | Before | After | Net reduction |
| --- | ---: | ---: | ---: |
| Production Go | 36,853 lines / 140 files | 29,759 lines / 122 files | **7,094 lines (19.25%)** |
| Go tests | 20,926 lines / 91 files | 19,046 lines / 88 files | 1,880 lines |

- `make test TARGET=controller`: all Go packages and all 50 real-readline PTY history/draft/credential/cancellation cases passed; no ADB or agent calls in the PTY fixtures.
- `go test -race -count=1 ./...` with `GOFLAGS=-p=1`, run alone: all packages passed. An earlier overlapping run hit the existing 500ms fake-process partial-output deadline in `adbdiag`; process-launch/cancel/reap diagnostics then passed, the unchanged deadline assertion passed ten isolated race repetitions, and the complete serial race run passed. Scheduling contention is consistent with the failure, not proven as its exclusive cause. No timeout or assertion was relaxed.
- `DROPCHECK_E2E_LIVE=0 go test -tags=e2e ./integration/e2e -count=1`: passed, with the live matrix skipped. The 236 retained rows are exactly the baseline's 136 Shell and 100 parser/help rows, including IDs, contents and order. Unicode literal round trips, runtime input rejection before execution, explicit cleanup transport and typed-envelope failures passed.
- The external-module public API check, typed numeric-SSID/configuration-ID-zero checks, inventory and pre-dispatch validation passed. Full vet/staticcheck, formatting, read-only modernizer, tagged E2E vet/modernizer, controller build and whitespace checks passed. Retired executable roots were rejected before ADB; retired implementation imports/modes were absent.
- Android unit/lint/main-APK/test-APK tasks succeeded offline using unchanged-source Gradle cache. Retained reports contain 306 tests in 51 suites, zero failures/errors/skips and zero lint issues; no instrumentation ran.
- Changed documentation links/fences passed. Android, protocol, shared Shell grammar, tools, CI and dependency manifests have no diff. Verification preceded the separate commits; no push was performed.

## Limits

Device-free tests and cached unchanged-source Android unit/lint/APK tasks do not establish real pixels, widgets, IME, clipboard, accessibility or live device/network acceptance. The migrated live E2E restore flow is validated with fake executables and parsers, not on a handset. Historical QA records retain the behavior and counts of their recorded revisions.
