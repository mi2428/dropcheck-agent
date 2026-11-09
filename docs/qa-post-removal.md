# Post-removal architecture and Go-library QA

Date: 2026-11-05. Audit baseline: `3e4fb2b` on `develop`. Line references below identify that baseline, not the subsequent cleanup's shifted line numbers. Scope: the repository after removal of the one-shot CLI, standalone workflow TUI and Go test DSL.

## Implementation status

The original findings below are retained as historical evidence, not as a claim that every issue is still open. The subsequent remediation has been implemented without restoring the retired CLI/DSL and without commits, publication or live-device operations.

| Finding | Implemented boundary | Maintained verification |
| --- | --- | --- |
| R1 | Common connect/cycle builders and final dispatch validation reject invalid credentials | `command/credentials_test.go`, public constructor and external-consumer tests |
| R2 | The real JSONL writer projects a detached event through shared `safevalue`; evaluation/raw acquisition is not presentation data | `tuirun/export_boundary_test.go`, `safevalue/safevalue_test.go` |
| R3 | Private Client lifetime, opaque Operation, detached credential-free Agents and pinned connection identity | Public fake-backed external-module lifecycle tests and control snapshot/replacement tests |
| R4 | Stable agent/target/check IDs drive queue, detail matching and failure-rate aggregation | `tui/producer_identity_test.go`, `tuistate/producer_boundary_test.go` |
| R5 | Negative AP count/minimum is rejected; known empty scan remains a valid zero | `tuirun/wifi_expect_test.go` |
| R6 | Pure plan validation runs before session/ADB startup | `app/tui_lifecycle_test.go` marker-only fake ADB checks |
| R7 | Safe subprocess errors preserve cancel/deadline causes without exposing token-bearing raw errors | Marker-synchronized ADB tests and public in-flight cancel tests |
| R8 | Idempotent Close returns bounded, device-specific aggregate cleanup failures, including partial startup | `session/cleanup_test.go`, public external lifecycle and Shell cleanup tests |
| A1/A7 | Owned typed inputs, deliberate public results/payload names, explicit lifecycle/logging contracts and executable Examples | Public API tests, compiled hardware example, runnable synthetic Examples, callback tests |
| A3 | Shell passes `shell.Command` and `pipeline.Pipeline` directly; shared selector-slot helper replaces repeated dispatch switches | Existing Shell/command fixtures, safe-output and completion tests |
| A5/A6 | Actual preview metadata, compact UI retention, producer duration with known-zero state and restored scoped read-only diagnostics | Producer/reducer/hook tests and large-payload retention assertions |
| Shutdown | Explicit consumer exit releases UI delivery waits without dropping durable sinks or owned cleanup | Small-buffer, absent-consumer and 128-target fake-only shutdown tests |
| A2/A4/distribution | Neutral `internal/acquire`, one live presentation path, canonical module and reproducible pinned generation without implicit installation | Acquisition/adapter/public parity tests, render tests and versioned offline consumer |

The first implementation gate passed `go test ./... -count=1`, including the external fake-backed successful public consumer. Initial test failures exposed invalid synthetic credentials and UI fixtures that relied on invented planned steps; those fixtures were corrected to explicit valid inputs and actual compiled preview metadata, without a production fallback or weaker assertions. Acquisition/adapter/public/render/Shell integration tests then passed. Module migration and the offline versioned consumer passed their checks, and the full generated descriptor differs only in `go_package`, not any wire contract. Final normal/race/PTY/lint/build and Android results follow.

### Final remediation verification

The canonical module is `github.com/mi2428/dropcheck-agent/controller`. Verification used cached Go 1.26.8 and serialized controller test invocations; no production logic changed after the final full Go gates.

| Check | Result |
| --- | --- |
| `make test TARGET=controller` | PASS: all Go packages, external fake-backed public lifecycle, offline versioned consumer and 50 real-readline PTY cases |
| Versioned distribution | PASS through an isolated local file proxy, no `replace` or external network; public transitive dependencies exclude tui/UI/render |
| `GOFLAGS='-race -p=1' go test -count=1 ./...` | PASS, including race instrumentation inherited by the external consumer child |
| Formatting/modernizer/vet/staticcheck | PASS with staticcheck 0.8.2; a test-only `SplitSeq` modernization and repeated-Close assertion cleanup were followed by focused normal/race checks |
| Controller build | PASS with the canonical version linker symbol |
| Device-free tagged E2E and tagged vet | PASS with `DROPCHECK_E2E_LIVE=0`; live cases skipped |
| Protobuf generation | PASS twice with the existing exact pins; identical output hashes; only the Go namespace option differs from the baseline descriptor |
| Generator safety/CI definition | Wrong/missing tools rejected before output changes; generation never installs/fetches. CI explicitly provisions the same pins; YAML structure and existing security restrictions checked |
| Local godoc/Examples | Explicit Client methods, typed option fields and result ownership visible; synthetic Examples and successful external payload processing passed. Offline godoc printed lookup warnings; remote docs were not queried |
| Android unit/lint/debug and instrumentation APK builds | PASS offline with forced task execution and no build cache: 306 unit tests across 51 suites, zero failures/errors/skips; lint reported zero issues; both APKs built, not installed |

The canonical module and versioned archive checks do **not** establish remote publication: a future controller submodule release needs its own `controller/vX.Y.Z` tag. No commit, push, tag or release was created. Go 1.26.0 and remote CI were not run here. No installed ADB, handset, live scan/probe/mutation, device instrumentation run, native clipboard/IME/TalkBack acceptance or memory benchmark is claimed. Gradle 9.8.2 reported deprecation warnings concerning a future Gradle 10 upgrade; that upgrade is not part of this change. Uncooperative custom hook/writer code must still honor its cancellation/close contract; the concrete UI consumer lifecycle and owned cleanup are covered by maintained tests.

## 1. Prioritized decisions

At the audit baseline the basic Go facade was usable through a local module replacement, but was **not yet a mature public-library boundary**. Adding godoc alone could not repair its exposed transport ownership, inconsistent validation and cleanup/error contracts. The findings and bounded remediation decisions below describe that baseline; the implementation status above supersedes their original open/closed state. A replacement execution engine is not justified.

| Priority | Work | Evidence | Bounded next step |
| --- | --- | --- | --- |
| Now | Reject invalid connect/cycle credentials at the shared operation boundary | Reproduced: short credentials pass both constructors and final validation | Reuse existing credential validation before any dispatch; retain valid/default cases |
| Now | Protect the actual tui JSONL export, not an unused report serializer | Reproduced: configured PSK is removed, but a URL token survives sanitizer → JSONL writer | Put safe export projection at the real export boundary, without mutating acquisition/evaluation data |
| Now, before public API stabilization | Close the public transport/token escape | Reproduced synthetic token serialization; exposed server/member access verified in source | Private session ownership, credential-free detached agent snapshots and explicit public operations |
| Now | Stop false-positive AP-count checks and dashboard identity conflation | Reproduced negative-minimum PASS and duplicate-ID queue/log failures | Validate nonnegative counts; use existing ordinal IDs, not names/types, in the UI |
| Next | Validate a complete tui plan before starting ADB/session resources | Reproduced: an invalid plan reaches the fake ADB executable | Call the existing pure validator before `Open`; assert zero ADB calls for invalid plans |
| Next | Preserve context causes and expose cleanup failure | Mid-flight cancellation cause loss reproduced; cleanup suppression verified in source | Preserve safe error causes and aggregate bounded device-specific cleanup failures |
| Next | Define the public result, lifecycle, logging and godoc contracts | Public declarations and external-module test inspected | Name required result types, document actual defaults/ownership/callback behavior, add executable examples |
| Next | Remove the remaining redundant rendering/Shell/state projections | Product caller tracing and recent change history | Direct command handoff, one live presentation path, existing preview IDs/metadata, compact UI retention |
| Next | Resolve disconnected tui diagnostics and duration production | No hook invocation or duration assignment in product producers | Restore the promised bounded behavior or explicitly retire the complete feature graph |
| Later | Bound aggregate UI shutdown delivery; decide versioned module distribution | Structural risk and an explicitly documented distribution constraint | Small absent-consumer test; canonical module path only when ordinary remote consumption is required |

The cleanup in this change does not silently resolve these behavioral or public-API decisions. Confirmed remaining defects are listed separately from removed dead code.

## 2. Confirmed correctness and reliability risks

### R1. The public mutation path bypasses credential validation

**Evidence:** `controller/internal/command/validate.go:97-104,128-130`, `operation_builders.go:292-327,647-667`, `credentials.go:8`. `dropcheck.WiFiConnect` and `WiFiCycle` reuse those builders; `Client.Run` reaches the shared validator without passing through the Shell checks in `interactive.go:164,233` or `app/shell_commands.go:27`.

An isolated test used `Passphrase: "short", Security: "wpa2"`; both connect and cycle passed constructor and pre-dispatch validation. A valid BSSID can also be supplied. On Android, `CommandExecutor.kt:205` reaches `WifiConnector.kt:91`, whose BSSID-conflict cleanup can remove saved configurations at `:194-208` before the privileged add operation. No device mutation or saved-profile loss was executed or observed in this audit.

**Remediation/tests:** validate at the shared connect boundary, not in another frontend. Characterize short/control-containing/non-hex credentials, valid 8/63-byte and 64-hex values, and the currently supported omitted-credential behavior. Verify zero dispatch for rejected operations. Effort small; confidence high.

### R2. TUI JSONL has a different secret boundary from Shell presentation

**Evidence:** `tuirun/compile.go:446-514` removes configured network PSKs from owned result clones; `tuirun/jsonl.go:78` marshals the resulting event directly. HTTP URLs may contain queries (`command/validate.go:204-214`). Other credential-shaped strings are not necessarily configured PSKs. Shell has a different sanitizer in `render/safe_text.go`; the old `SafeReportJSON` was not called by the product JSONL path.

An isolated sanitizer-to-real-JSONL-writer probe confirmed that a configured synthetic PSK was redacted while a different synthetic URL token remained. This proves the export boundary mismatch, not an actual credential disclosure on a handset. Raw Go results are intentionally acquisition data; that is not the defect.

**Remediation/tests:** keep typed acquisition and evaluation unchanged. Apply safe projection to the actual export and test synthetic query tokens, encoded credential keys, authorization fields, raw diagnostics and literal `password=Guest` SSIDs. Move only the required shared sanitizer into an acyclic boundary; do not add a second serializer/framework. Effort medium; confidence high.

### R3. The public client exposes transport internals and authentication material

**Evidence:** `pkg/dropcheck/client.go:20-35` aliases `control.AgentInfo` and embeds `*session.Session`. The embedded fields (`session/session.go:41-49`) expose `Server` and `Token`. `Client.Agents[i].Hello` also exposes the hello token, and agent copies share the hello pointer with server state (`control/server.go:230-238`). An isolated synthetic `Agent` JSON serialization retained the authentication token.

External code can also reach `c.Server.Run(..., op.Command)`, bypassing `Client.Run`'s validation, typed-forget capability check and pinned-session selection. This is an accidental same-process API/ownership escape, **not evidence of a remote attacker bypassing authentication**. The normal `RunPinned` implementation and connection-owned delivery are protective and must remain.

**Remediation/tests:** keep a private session; expose detached, credential-free agent snapshots, explicit close and intentional reselection only. Move shared connection startup into an internal boundary used by both app and facade, rather than exposing `Server` so the app can reuse the public client. Test public surface, serialization, snapshot mutation isolation and refusal of stale sessions. Effort medium; confidence high.

### R4. Dashboard names/types replace existing stable identities

**Evidence:** `tui/run_queue.go:309-337` merges by lowercased name, not `Step.ID`; `tui/detail.go:1141-1167` selects target/check evidence by names or overlapping type/operation tokens. The runtime already assigns ordinal identities in `tuirun/compile.go:121-138`.

Isolated probes confirmed two distinct IDs named `same` collapse to one queue row, and a different check ID matches a selected check solely because both are `ping`. The display can therefore omit a configured check or mix investigation evidence. Core acquisition and primary runtime IDs are not shown to be wrong.

**Remediation/tests:** preserve target/check IDs through queue and modal selection. Use real producer events for duplicate names, same-type/different-ID checks, repeated rounds and different targets; retain filtering/navigation behavior. Effort medium; confidence high.

### R5. A negative AP minimum is an always-satisfied threshold

**Evidence:** `tuirun/wifi_expect.go:60-72,143-149` parses `min_count` without a nonnegative range check. The isolated probe compiled `min_count: -1` and observed `Passed:true` for a known empty scan (`0 >= -1`).

**Remediation/tests:** reject negative `count` and `min_count` at compilation. Keep `count: 0` valid: an explicitly observed empty scan is not missing data. Effort small; confidence high.

### R6. TUI's pure validation is not on the pre-startup product path

**Evidence:** `app/tui.go:41-74` calls `LoadFile`, then `Open`, then `Compile`. `tui/config.go:222` can return a plan whose runtime constraints have not been validated. `tuirun.Validate` has only test callers. For example, an observed read-only target with an active ping but no active-probe authorization is accepted by YAML construction and rejected by runtime validation.

**Counterevidence:** `Compile` runs before `Execute`; this does not demonstrate that the invalid plan itself performs Wi-Fi mutation. The unnecessary side effect is ADB discovery/service/session startup before rejection, and startup errors can hide the actual configuration error.

The isolated probe first confirmed the fixture passed YAML construction but failed the shared validator, then observed that `runTUI` invoked a marker-only fake ADB executable. No real ADB or service was started.

**Remediation/tests:** call the existing pure validator before `Open`; do not duplicate the validation rules. Require fake ADB invocation count zero for malformed policies, forbidden read-only probes and invalid connection data. Effort small; confidence high.

### R7. In-flight startup/inventory cancellation loses its error cause

**Evidence:** `adb/adb.go:190-192` returns the context error, but `:135-151` rebuilds it with string formatting only. `Devices` and early `Open` stages traverse that layer. The isolated fake-process probe waited for a startup marker, canceled, and received text containing `context canceled` for which `errors.Is(err, context.Canceled)` was false.

**Counterevidence:** already-canceled `Open`/`Devices` are handled correctly, and later session waiting uses `%w`; the external-module test only covers cancellation before work starts.

**Remediation/tests:** retain safe context causes without reintroducing raw token-bearing subprocess error text. Test mid-flight cancel/deadline and token redaction together. Effort small; confidence high.

### R8. Close hides reverse-rule cleanup failure

**Evidence:** `session/session.go:196-218` ignores each `RemoveReverse` error. Cleanup shares a five-second context across devices, and `sync.Once` prevents a later close from retrying. Both public close and partial-start failure cleanup use this path. `docs/go-api.md` describes resource release without an observable cleanup result.

This is source-confirmed failure suppression, not a measured residual rule on a device. gRPC shutdown and duplicate-close suppression are implemented correctly.

**Remediation/tests:** preserve bounded cleanup and non-mutation of Wi-Fi; aggregate per-device failures into an observable close/startup outcome. Characterize normal/failed removal, multi-device deadline exhaustion, partial startup and repeated close before changing the API. Effort medium; confidence high in suppression, runtime recovery unmeasured.

## 3. Maintainability and architectural hotspots

### A1. Public library contracts should be owned, nameable and discoverable

`pkg/dropcheck/operations.go:9-19,28-94`, `client.go:20-35` and `tuirun/runtime.go:95-112` expose internal aliases rather than a deliberate public type inventory. Getters and selector assignment work, but consumers cannot name several finding/part/payload/enum types needed for ordinary helper signatures and synthetic successful results. `Operation.Options` also exposes presentation-only fields and mutable slices.

The actual package godoc shows alias references without the underlying field/default documentation; the promoted `Close` is not discoverable there, and 26 of 27 constructors lack public comments. Internal comments and the Markdown guide are useful counterevidence, not a complete public contract. The current Markdown example also calls `log.Fatal` after registering deferred close; `log.Fatal` calls `os.Exit(1)` and skips that defer on failure.

**Direction:** first settle the private-client/agent boundary in R3. Expose only required result/part/finding/enum names, document nil/known-zero/false/Missing, units/defaults, ownership and close semantics, then add compiled examples including successful result processing and failure cleanup. Keep a single public facade unless actual usage requires another package; wholesale protocol relocation is not required. Before public stabilization, also document a real local `go.mod` replacement recipe. Effort medium; confidence high.

### A2. The library's atomic contract still belongs to the tui package

`Client.Run` and Shell both depend on `tuirun.ExecuteOperation`, while `Result` aliases its `OperationResult`. That package also owns plan compilation, policy, event sinks, JSONL and cleanup. Shared acquisition is the right implementation, but its placement and exported result types make changes to the private tui design propagate into the public contract.

This is a semantic/maintenance coupling, not proof of a bloated library dependency closure: the dependency inspection found no terminal or YAML packages in the public package's transitive imports. If that ownership boundary is changed, extract only the existing atomic acquisition/validation/result contract and adapt the tui policy around it. Do not create a second execution engine. Add cross-caller parity tests first. Effort medium; confidence medium-high.

### A3. A Shell command traverses three near-identical DTOs

The shared JSON inventory generates syntax, not complete dispatch. Go maps paths in `command/interactive.go:121-295`, copies `Parsed` into `shell.Command` in `shell/parser.go:93`, then copies it again into an app-local `shellCommand` in `app/shell_bridge.go:30-48`. The bridge retains old naming/ignored-state wrappers from the earlier package split. Dead `Detail`/downstream `Path` transport was found here and removed by this cleanup.

For a new RPC-backed subcommand the current maintenance path includes the portable inventory/fixtures, both language mappings, operation builder/validation/deadline, payload verification, safe presentation/metadata, optional tui metric compilation and public wrapper/types/examples. New fields also traverse every DTO copy. The capability registry and fixtures help detect omissions, but do not make these mappings exhaustive or prove all public constructors.

**Direction:** use the existing `shell.Command` directly at the app boundary and perform pipeline conversion at the output boundary; remove ignored-state compatibility wrappers during that local change. Keep effect/target/security decisions explicit and the shared generated inventory authoritative. A generic command/plugin registry would add more machinery than it removes. Tests must cover a complete producer-to-dispatch/result path, not just successful parsing. Effort small-medium; confidence high.

### A4. Legacy presentation still increases feature fan-out

The cleanup removes the old independent JSON serializers, but the noninteractive probe rendering path remains in `render/render.go:854-1022` alongside `render/interactive_request.go`, including `render/traceroute.go`'s stdout-based evaluation. Actual product command rendering selects interactive presentation. Probe extensions therefore still have obsolete formatting/tests to consider beside typed rendering and the authoritative typed evaluator.

**Direction:** after live-path characterization tests are in place, delete those legacy probe branches and move their useful numeric-presence, identity and secret assertions onto the live presentation. Keep `render.Report` for historical checks and `SanitizedResult` for retained Wi-Fi test data. Do not replace the whole layout engine. Effort medium; confidence high in reachability, deletion must preserve the useful assertions.

### A5. TUI producers and UI state disagree about the plan and retained data

- `tuistate/types.go:31-57` synthesizes connect/wait steps even for observed targets or disabled wait, instead of using existing preview `Connect`/`Wait` metadata. This can show steps that runtime never executes; runtime mutation suppression itself is correct.
- `PassingCheck.Report` (`types.go:82`, assigned at `state.go:95`) has no product reader. `log.go:25` retains full events even though modal readers use compact scope/line/finding metadata. These references can keep cloned acquisition payloads and retry histories alive beyond the core's recent rounds. Count bounds exist; no OOM or latency regression was measured.

**Direction:** fix R4's ID boundary and use core preview metadata for planned steps. Drop unused bulk references at the UI reducer's retention boundary, not from the core report/JSONL or sink-isolation clone. Verify producer→reducer→queue/modal behavior and large synthetic payload retention. Effort small-medium; confidence high in unused references, performance impact unmeasured.

### A6. Disconnected features must not be mistaken for optional abstractions

`tuirun/runtime.go:72-86` declares failure-cause interfaces, but no producer invokes them. `app/tui.go:160-285` implements an unreachable diagnostic graph, including the sole producer of text consumed by `tuistate/connect_state.go:35`. `Event.Duration` is also never assigned by product producers; duration/average/maximum UI tests use hand-authored events.

History `53253e8` removed the old producer calls/duration assignment. Transport warning logs and attempt timestamps still exist. Deleting only the interfaces would hide the missing product behavior; reconnecting everything without a requirement could add unwanted ADB traffic.

**Direction:** decide which diagnostic/timing contract is still intended. Restore bounded hooks/duration from the real producer if required, otherwise remove the complete implementation/display/test graph explicitly. Characterize actual events, cancellation and acquisition cost first. Effort medium; confidence high in disconnected producers, intended feature scope needs a decision.

### A7. Logging callbacks need an actual library contract

`session/session.go:35-37,158-175` describes WARN/ERROR delivery, but a supplied callback receives INFO/DEBUG and accepted notifications too. `control/session.go:162-190` invokes it on stream goroutines: it may run concurrently across agents, and blocking a callback delays that stream's result handling. Startup/discovery stderr is separate.

The default nil-handler filtering and internal server documentation partially explain this, but the public alias does not. Document actual levels, concurrency, bounded handler work and stderr behavior; test representative events and ordering. Do not automatically add another asynchronous queue or recover caller panics. Effort small; confidence high.

## 4. QA safety net and refactoring prerequisites

- **Independent probes versus the passing suite:** seven isolated Go test probes, loaded through an overlay outside repository source, deliberately assert the missing safety properties. Credential rejection, URL-token export safety, token-free public agent serialization, in-flight cancel classification, nonnegative AP counts, distinct dashboard identities and preflight-before-ADB currently fail those checks. These are not passing regression tests or live-device evidence.
- **External-consumer coverage:** the current external module test proves import through local replacement, constructors/selectors/status types, pre-canceled startup/inventory and an unconnected client error. It does not exercise successful `Run`, public payload construction/processing, cleanup failure, agent serialization, stale-session reselection, callback behavior or in-flight startup cancellation.
- **Source-of-truth coverage:** all 42 portable syntax rows have shared fixtures, and Android's capability test covers protobuf command cases. Several result/deadline/metric/public-wrapper switches and DTO mappings remain handwritten. Add an explicit extension checklist and one complete boundary test for a new function rather than another registry.
- **Producer coverage:** hand-authored UI events let missing duration producers, duplicate IDs and invented planned steps pass. Test real compiled previews/events through the reducer and UI selectors. Keep synthetic rendering tests for narrow layout, Unicode, unknown versus zero/false, filters and safe full values.
- **Security coverage:** do not remove token/authorization/credential assertions with obsolete JSON-only tests. Move useful assertions to the real safe text/Shell envelope. Separately add actual tui export tests for R2; validating an unused sanitizer does not protect JSONL.
- **Operational tests:** use marker-synchronized fake subprocess cancellation and serialized controller test invocations. Preserve existing deadlines; previous concurrent runs exposed a 500ms fake-process scheduling flake. Public close needs failure/partial-start fixtures, and aggregate cancellation needs a permanently absent consumer fixture.

## 5. Not worth changing now

- The tui Plan/Check/Expectation model, retries/stability/repeat, owned disconnect/forget cleanup, per-agent concurrency/round barrier, compound fresh acquisition, typed payload validation and JSONL delivery are live requirements. Do not delete them merely because the fluent authoring DSL was removed.
- Do not resurrect the one-shot CLI/test DSL, add public scenario/tui authoring, build a command plugin framework or move all generated protocol code into public packages.
- Numeric string options are documented and share parsers; wholesale conversion is not a demonstrated correctness fix. If a field needs an explicit zero/default distinction, change that field with its boundary test. False requirement flags intentionally mean “not required,” not missing observation data.
- A qualified remote module path is a real distribution prerequisite, but the current guide explicitly supports local replacement. `go list -m dropcheck/controller@v0.0.0` rejects the unqualified path. Choose a canonical path and test replacement-free consumption only when versioned external distribution is required; do not guess a publication destination.
- UI cancellation can accumulate one-second sink-delivery waits after its consumer has exited (`app/tui.go:107-112`, `tuirun/execute.go:187-193`). Small plans fit the buffer and each wait is bounded. Treat aggregate shutdown time as a structural risk requiring a scaled/absent-consumer test, not as a measured production hang.
- Keep Android permission/caller boundaries, native Inspect/copy/quota, raw/debug separation, typed nil/known-zero/false distinctions and immutable acquired identities. This audit provides no reason to weaken them.

## 6. Original audit scope, cleanup and verification

The supervisor mapped all Go packages, shared schema/inventory/generator, build/CI, current documentation and recent hotspot history. Two fresh read-only auditors independently inspected public API/session/control/runner and tui/runtime/render/state. Shell parsing/help/dispatch, extension fan-out and cross-language registry/fixture coverage were independently traced by the supervisor. Android parser, registry and Wi-Fi mutation ordering were sampled; its UI/collectors were not a new exhaustive audit. Generated bindings were not treated as cleanup targets.

Applied cleanup is limited to proven-unused internal output paths/helpers/metadata and dead Shell transport fields:

- Removed independent protobuf/report/raw-debug/error/agent JSON branches, their obsolete format/command-ID parameters, `CommandResultEnvelope`, `renderProtoMessage`, `structuredResult` and the unused exported internal `render.OperationResult` wrapper. The live text → safe `ok/lines` Shell envelope and private historical-report rendering remain.
- Removed `adbdiag.Collect`, unused tui `Emitter`/`MultiSink`, and unread preview `SecretPresent`/`CapabilityPreflight`. `CollectInteractive`, actual band preflight, PSK protection, per-sink isolation, durable JSONL and existing cleanup remain.
- Removed unused grammar `Unsupported`, dead detail/path transport through Shell DTOs, the unused help/default-EHT wrappers, a private tokenizer forwarding layer and the now-unused app output-format constant. The unused pipeline format enum/default-selection method is gone too; its only live state is now a private JSON-selection boolean with an explicit zero/default assertion. The EHT-default assertion calls the live options constructor and checks its error.
- Migrated useful legacy JSON assertions to safe text/Shell envelope and retained sanitized snapshots: nested credentials, URL tokens, authorization, literal SSID/BSSID, typed IE/security hex, numeric raw bits, unknown versus known zero, width/Unicode/filter behavior and non-mutation of acquisition data. The renderer/ADB test-function count stayed 150. Only the removed `MultiSink`'s dedicated test was retired; live execution-sink isolation/failure/cleanup tests remain.

Including moved/new test files and excluding generated `controlpb`, controller production Go decreased **29,759 → 29,422 lines** (122 → 121 files), a net **337-line reduction**. Tests increased 19,046 → 19,099 lines with 88 files. Behavioral defects and larger architecture changes above remain open.

Final independent checks used cached Go 1.26.8 with `GOTOOLCHAIN=local GOFLAGS=-p=1 GOPROXY=off GOSUMDB=off`, with controller test invocations serialized:

- `make test TARGET=controller`: all Go packages, the external-module public API test and all 50 real-readline PTY cases passed on the final source; PTY fixture ADB/agent calls were zero.
- `go test -race -count=1 ./...`: all packages passed, run alone after the preceding task completed.
- `make fmt-check modernize-check lint TARGET=controller`: formatting, read-only modernizer, vet and installed staticcheck 0.8.2 passed. Additional gofmt inspection included the new test file.
- Device-free `DROPCHECK_E2E_LIVE=0 go test -tags=e2e ./integration/e2e -count=1` and tagged vet passed; live execution remained skipped. `make build TARGET=controller`, documentation checks and whitespace checks passed.
- Android/protocol/shared grammar source, dependencies, configuration and public API declarations have no diff. Go 1.26.0, CI runners, Android/Gradle, hardware behavior, memory benchmarks and aggregate cancellation latency were not rerun or measured here.

The seven independent adversarial probes **failed as expected**, establishing remaining defects/boundary risks; they were not included in or misreported as the passing regression suite. Their small sources and overlay manifest are retained with the supervisor's audit artifacts for follow-up. The implementation remains uncommitted and unpushed.

No installed ADB, handset, live network scan/probe/mutation, dependency update, permission/configuration change, commit or push is part of this audit. Real authentication tokens and credentials were not inspected; probes use fixed synthetic values. Cached historical Android/JVM/APK success from the previous task is not a new verification result for this audit.
