# Cross-surface presentation consistency

Date: 2026-11-05. Scope: PC/Android Shell generated headings, fields, tables and composite values; ADB supplements; local/debug/history output; Controller CLI and TUI presentation. Request-name and earlier metadata corrections are preserved. No live device operation, installation, scan, probe, commit or push is included.

## Contract and corrections

The contract is role-based, not a global separator rewrite. Headings are colonless; peer fields use attached colons, padding after the colon and a shared value column. Fields are indented two spaces relative to their heading. Tables retain column headings, a two-space gap, one blank line on each side and lossless record fallback. AGE remains left-aligned seconds and `eap_wpa3_enterprise` remains `wpa3-eap`. Composite values may contain `key=value`; raw/native text, literal identities, URLs, IPv6 and logs are not reparsed into fields.

- ADB scalar and nested RA records now share the owned field renderer instead of separate tabwriter/byte-width rules. Host metadata and body facts are composed before layout. Link statistics and CLI device inventory use the existing shared table rules.
- Supplement callbacks defer display width only. Collection completes and captures its receipt time once; formatting at several widths neither changes that time nor repeats collection. Source sanitization precedes owned record composition. Explicit raw bundle JSON retains its separate raw-data contract.
- Android full Wi-Fi/IP/scan-evidence details now emit typed fields rather than pre-padded text. Capabilities, scan and diagnostic metadata use the same field roles. Native layout supplies the relative indent and bounds deep narrow continuations; logical/filter/copy labels keep their existing boundaries.
- HE/EHT/BSS/puncturing/MCS facts have named parent fields, retaining inner components and complete flags. Security, roaming, RNR, Multiple BSSID and decoded element output remains intact as named multiline values. AP/radio/IE/SDK facts, warnings and missing observations no longer mix unlabelled generated values into peer fields.
- Long CJK summary labels use display widths. Dispatch errors and missing acquisition metadata use shared records. Local version and wrong-payload errors use the existing Android field/block path. Check/TUI section headings are colonless; detailed TUI inspection reuses the shared report renderer, preserves acquired targets and recovery facts, and distinguishes the selected step from whole-run core counts.

IE item detail remains in `show`; whole-object raw dumps remain debug-only. Typed outcomes, zero/false/unknown distinctions, acquisition targets, session input/draft recall, credentials, explicit pipeline exclusions, Inspect/copy values and independent CLI/JSON meanings remain protected by regression checks. No formatter framework, arbitrary-text parser or dependency was added.

## Independent adversarial QA

One visible top-level OpenCode worker, `style-qa`, uses `openai/gpt-6.1-sol-fast#xhigh`. Its model/variant, worker environment, interactive shell, visible identity and actual startup reply were verified. The same worker owns four dedicated regression files and is reused across rounds; production changes remain supervisor-owned. Measured token usage is unavailable.

The unchanged initial baseline reproduced eleven defect families: Go had 10 failing top-level tests out of 16 and 13 failing leaf cases out of 350; Android had 8 failures out of 13, including an unused string projection. The corpus covers widths 0/8/24/40/80/120, CJK/combining/ZWJ text, scalar/table distinctions, warnings, raw/JSON safety, identities and historical targets. Source-only candidates were kept separate from executed evidence.

The second round migrated tests to the intentional owned-record/callback stage boundary without removing or weakening their colon, indent or column assertions. It added two Go and two Kotlin HE/EHT fixtures for complete flags, coloring, puncturing, warnings and zero/false/unknown values. Its app run stopped at an existing-test format/type error after the callback change; the supervisor corrected that error and reran the complete package successfully.

The third round confirmed that all original 33 Go/Kotlin tests passed and added rich connected/scan MLO roles, RA/source composition and deep multiline continuation checks. It reproduced one new defect: at width 24, a deeply indented multiline field kept twelve spaces for its first value but only four for later lines. The supervisor reproduced the failure and fixed the shared wrapper to retain the preceding owned field's continuation depth, rather than patching individual screens. The expanded Go corpus had 364 leaf cases with only this new case failing before the correction; Android had 16 successful focused tests.

The final limited recheck passed all **25 Go top-level tests / 364 leaf cases**, including the original failure and a deep-to-shallow multiline transition. The supervisor independently verified those counts and results. The **16 Android focused tests** remained successful; all expectation-bearing assertions were retained. Test-only Go modernizer changes were mechanical and left role, identity, privacy and outcome checks intact. No confirmed counterexample remains in this corpus.

## Final verification

Passed on the corrected source:

- All Go packages and **50 real-readline PTY cases** via `make test TARGET=controller`; complete `go test -race -count=1 ./...`.
- `make fmt-check`, `make modernize-check`, `make lint TARGET=controller` (vet and cached offline-built staticcheck 0.8.2), generated-contract drift, whitespace checks and Controller build. No dependency or configuration change was needed.
- Device-free tagged E2E schema/parser/expectation/PMTU-timeout checks, without enabling live E2E.
- Android **306 JVM tests / 51 suites**, failures/errors/skips all zero; **zero lint issues**; application and instrumentation APK builds, including the latest wrong-payload and narrow native-layout fixtures. Instrumentation was built, not installed or executed. The existing Gradle deprecation notice remains.
- Independent handoffs, actual fixtures and final source changes reviewed by the supervisor; documentation links/fences and PTY script syntax checked. The worker exited normally, its dedicated tab was closed and layout ownership disabled. HEAD/index are unchanged; all edits remain uncommitted and unpushed.

One combined verification invocation reached its 120-second shell limit after all normal Go/PTY checks, during the race stage. The remaining complete race run and Controller build were executed separately and passed; the timeout was not treated as a successful check.

## Limits

Android native instrumentation is synthetic and device-free in design, but building it is not evidence that it executed. Real pixels, fonts, IME, overflow/Inspect/clipboard UI, accessibility and live device/network behavior remain unverified. Explicit CLI compact summaries, navigation lists, logs and raw references are intentional separate roles. A bounded adversarial corpus cannot prove all possible payloads or devices defect-free.
