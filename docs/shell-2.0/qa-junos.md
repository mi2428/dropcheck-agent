# Junos-inspired Shell cutover: field audit and adversarial QA

**Later contract:** raw references previously accepted in `full` have moved to explicit debug views. Structured IE/hex evidence remains in show. See [the raw/debug follow-up](qa-raw-debug.md); the results below record the original cutover contract.

Date: 2026-11-03. Scope: both interactive Shells, local synthetic execution, generated contracts, and native PC PTY input. Baseline: `ebe9dbc`. No device installation, live radio/Network operation, commit, push, or GitHub setting change was performed. Prior hardware results are historical and do not validate this cutover.

## New adversarial round

This is a new round after the implementation, not a renaming of the earlier QA suites. New `junos_adversarial_test.go` files cover Go Shell/app/harness/render boundaries; `JunosAdversarialQaTest.kt` covers Android. The existing suites remain active, with obsolete interactive fixtures migrated and contradictory synthetic successes corrected rather than weakening validators.

| Finding or attack | Evidence and correction |
| --- | --- |
| Stale PASS after rejected abbreviated tests | New tests initially failed for `te wi require-ip BAD`, malformed link/lab, invalid pipeline/quotes and unsupported tests. Exact-string rejection recognition missed unique prefixes. Both parsers now recognize the canonical test hierarchy before producing a bounded rejection. The app test verifies that the new rejection replaces historical success without dispatch. |
| Deep help prefix lost completion | `h req wi ` initially had no connect candidates. Help-path completion now follows the same unique-prefix rule as execution, without echoing values or executing anything. |
| Running Ctrl-C was not actually handled by the proposed callback alone | A new real PTY case started a local fake ADB executable, then sent Ctrl-C. The first run exited on SIGINT instead of completing the command: readline restores cooked mode and pauses reading after Enter. The command boundary now explicitly owns raw mode, blanks the reusable prompt, resumes key reading, cancels the current context and drops other typeahead. |
| Pending terminal reader hung on close | The same PTY exercise exposed readline's FillableStdin not closing its underlying CancelableStdin after active reading was enabled. The Shell now explicitly owns/closes that reader before closing readline. The test verifies one fake acquisition only, nine unstarted sources, cancellation, a new local version command, clean exit, and no race warning. |
| Missing nested Network WifiInfo and historical assertion evidence | The field audit found saved configuration/subscription IDs and raw/IE/availability references missing from the NetworkCapabilities WifiInfo view, and assertion snapshots absent from historical Wi-Fi test output. Both views now retain these independently sourced observations. New marker fixtures check the requested values and absence of raw credentials; PC checks widths 0/24/40/80/120. |
| Process-global profile history outlived Shell sessions | Removed the obsolete AgentLinkProfile global last-report store. The actual Shell session now owns the latest result; new tests check replacement of PASS and independence of fresh sessions. Existing profile tests still verify the returned typed outcomes, stages and evidence rather than an obsolete global history API. |
| Failed Go tests could be masked by the task runner | A failing unrestricted Go run was followed by a successful PTY command and `make test` returned success on this macOS make. The test recipe now explicitly enables failure propagation. Negative control `make test TARGET=controller GO=false` returns nonzero before invoking PTY tests. |
| Forged fresh success | For each of five mandatory metadata keys, Go tests remove it, set false, or duplicate it. None may pass. Android tests feed STATUS_OK without proof through the actual EHT composition function and require non-success with retained reference AP data. |
| Identifier/capability/cancel attacks | Numeric and BSSID-shaped SSIDs remain exact SSIDs; config zero is accepted; malformed/negative/overflow IDs are rejected. A typed forget without `wifi.forget.typed` and an already canceled acquisition produce zero runner calls. |
| Partial deadline and source evidence | A local fake executable writes safe reference text then blocks. A 500 ms whole-collection budget retains that failed timed-out attempt and explicitly marks the other nine sources not collected. Optional host supplement acquisition retains parsed partial output alongside errors. No installed ADB is involved. |
| History, filters and output forgery | New allowlist checks exclude broad/raw/ID/default/scan/probe inputs. Shared fixtures cover blank lines, maximum last/trim counts, Unicode positions and stage order. Existing identity-newline, credential-encoding, JSON and filtered-failure attacks remain active. |

The PTY driver submits at real input boundaries rather than racing a second command against busy-state typeahead suppression. Search/history can redraw an empty buffer before Enter; the driver drains that redraw burst before treating the next prompt as ready. Its test binary is race-instrumented. All executable fixtures and identifiers are synthetic.

## Field and source audit

The audit traced `control.proto` payload families through NetworkRepository/WifiProtoMapper and both default/projection render paths. It is an audit of retained safe protocol observations, not proof that every Android build supplies every API or timestamp. All diagnostic lists/errors are retained in full views, including unknown keys. Unknown availability is not interpreted from a scalar zero. Network snapshots retain stricter identity/time/count/availability/error checks.

| Payload/field group | Default placement and source/availability policy |
| --- | --- |
| WifiStatus: enabled/state/active Network/count/permissions/observation fields | Connection/radio section, including permissions and collector state/error/source metadata. Status and its nested IP acquisition are explicitly independent. Connection-only projects out unrelated IP/capability/AP acquisition from the displayed subject. |
| WifiConnection identity, saved network ID, state, RSSI/signal, frequency/channel/width, link/current/max rates, MAC, hidden/restricted, subscription/redaction, Passpoint data | Connection overview plus incremental detail; no interactive verbosity switch. Saved ID is labelled configuration ID, not Connectivity ID. Sentinel/unknown values and API availability remain distinct. Passpoint and maximum-rate data use existing capability records; repeated encodings are not a second measurement. |
| MLO connection/link identity, state/radio/current/max rates, associated/affiliated sets | Connection/AP link records with identity/rate availability. Reported max-rate scalars without availability proof are labelled as such, not certified limits. EHT relation/coverage/readiness/warnings are interpretations referring to these captures, not new atomic/roaming evidence. |
| Connection/AP information elements | ID, extension ID, byte count and safe hexadecimal bytes are retained, including unknown/vendor elements. Known roaming/security/HE/EHT/UORA/MU-EDCA/spatial-reuse/6 GHz interpretation reuses existing decoders. Equivalent numeric/hex encodings and decoded bitfields are derived representations, not extra observations; original IE bytes remain the authoritative retained source. No private PSK is an advertised IE field. |
| WifiScan fields/errors/results | Cached/fresh request/source/update/wait/band/elapsed evidence, full diagnostic fields, per-result boot-clock age and explicit errors. A scan's refresh flag does not certify every AP entry as newly observed. SSID/BSSID filters only select the returned evidence; affiliated BSSID matching is retained. |
| AP identity, capability string, RSSI/radio/center frequencies, timestamps, PHY/security, WifiSsid representation, operator/venue/Passpoint, responder/ranging/TWT flags | Full AP records and existing capability/security detail, with raw/IE/availability references. `brief` deliberately suppresses deep records; `mlo` only restricts subjects. Indexed table/detail references avoid repeating identity as another capture. |
| IpStatus network/interface/transports/validation/internet/MTU, addresses/routes/DNS/DHCP/private DNS/NAT64, capabilities/bandwidth/signal/specifier/owner/subscriptions/enterprise IDs/domains/proxy/WoL/RA | IP records, selected physical Wi-Fi view, and each contextual Network. An inactive private-DNS state does not hide a supplied server-name observation. WifiInfo from each NetworkCapabilities snapshot is labelled separately and retains its own fields/IE/raw/availability, rather than being deduplicated against WifiManager by equal values. |
| WifiCapabilities fields and supported/unsupported sets | Device section/projection includes all known and unknown values. False, unsupported, unknown and conflicting supported/unsupported lists are distinct. Android EHT presentation reuses this captured evidence rather than making a second Wi-Fi-standard API query. |
| WifiDiagnostics networks and active flags | Each independently collected Network/IP section retains its own identity and source. Full/EHT presentation includes the shared interpretation; EHT selectors omit unrelated connection/AP/Network records without suppressing requested-source errors. The bundle has no per-constituent atomic-success contract and says so. |
| Raw WifiInfo, ScanResult, MloLink, NetworkCapabilities and LinkProperties | Explicitly labelled safe raw references, not a replacement for typed availability or proof of a second observation. Credential/control sanitization applies before layout/filter/copy/Inspect. Dangerous strings are not retained in new PC historical assertion snapshots. |
| Tests and monitor | Latest Wi-Fi assertion includes checks, messages, elapsed time, errors and the retained status/IP snapshot. Link/lab keep bounded credential-free typed reports. A recognized rejected attempt replaces success. Monitor retains event times/messages/fields/status snapshots, not only an event count. Historical results cannot authorize new work. |
| PC host supplements and explicit ADB | Existing bounded RA/MLO supplements retain separate PC/ADB source/receipt labels and visible missing prerequisites/errors. The explicit ten/five/one-source plan retains original commands, serial, capture times, exit status, elapsed time, stdout/stderr/error and not-collected sections. Cross-source text is never deduplicated by equality. |

Intentional exclusions: credentials and unsafe control sequences; unrelated subjects selected out by a projection; deep AP evidence explicitly omitted by `brief`; duplicate mathematical/bit/hex derivations when the canonical safe IE/raw representation and interpretation already preserve the information; and unavailable Android observations that have no payload. An unavailable value is labelled, not invented. Android may show a bounded visible block with explicit full-safe-value access. No claim is made that every retained value is simultaneously on screen.

## Reproduction and verification

Use the supported Go 1.26.8 toolchain. The host default Go 1.27.2 can run the tests, but CI-pinned staticcheck v0.7.0 cannot decode that compiler's export format; this is a toolchain mismatch, not a suppressed code diagnostic. Run staticcheck with Go 1.26.8. No new project dependency is required.

```sh
GOTOOLCHAIN=go1.26.8 GOFLAGS=-p=1 make test TARGET=controller
GOTOOLCHAIN=go1.26.8 GOFLAGS=-p=1 make lint TARGET=controller
GOTOOLCHAIN=go1.26.8 make build TARGET=controller
(cd controller && GOTOOLCHAIN=go1.26.8 GOFLAGS=-p=1 go test -race -count=1 ./...)
./gradlew :agent:testDebugUnitTest :agent:assembleDebug :agent:assembleDebugAndroidTest :agent:lintDebug
make check-shell
make fmt-check
GOTOOLCHAIN=go1.26.8 make modernize-check
git diff --check
```

Where staticcheck is not installed, execute the CI-pinned `honnef.co/go/tools/cmd/staticcheck@v0.7.0` with `go run` under the same toolchain; do not alter the project module to install it. Pinned protoc 35.0 regeneration is separately checked byte-for-byte against the checked-out Go bindings.

Final recorded Android result: **222 unit tests, zero failures/errors/skips**, lint success, application APK and instrumentation APK built. Instrumentation was compiled but not installed or executed. The 282-case E2E table parses locally; all independent CLI rows remain byte-identical, while 197 interactive rows were migrated. Real PC PTY coverage comprises eight history/privacy cases plus running cancellation; neither installed ADB nor a real agent is used.

**Final gate passed:** Go all-package tests; all-package race tests, with the app package repeated after the final obsolete-helper cleanup; race-instrumented PTY tests; go vet and CI-pinned staticcheck v0.7.0 under Go 1.26.8; Controller build; Android unit/lint/app/instrumentation builds; Shell binding drift; byte-identical pinned protobuf regeneration; format, modernize and whitespace checks. Local documentation links resolve. No delegated result was accepted merely on a worker's claim.

One early simultaneous unrestricted Go/Gradle run exceeded two existing five-second local fake-process deadlines. The immediate serialized adb/app rerun passed with unchanged timeouts; final runs use `GOFLAGS=-p=1`. No validator or test was weakened to hide that contention.

## Remaining limits

- No post-cutover real-device acceptance: Android API/permission/vendor-specific behavior, Device Owner mutations, active scan throttling/callback cleanup, Network transport binding, measured touch/IME/rotation/copy/TalkBack interaction and human readability remain unverified live. Building instrumentation is not a pass for those behaviors.
- The synthetic width/field checks are selected adversarial markers plus code/field tracing, not exhaustive combinations of every firmware, permission state or malicious payload. Future protocol/collector fields must extend the audit and regression data.
- Passive constituents have different acquisition times; missing observation timestamps remain unknown. Host receipt times are not invented Android observation times. Optional host supplement errors do not certify or invalidate Android radio success.
- Nothing here establishes absolute quality or permits device operations, commit, deployment or push. The final report states only the checks actually completed.
