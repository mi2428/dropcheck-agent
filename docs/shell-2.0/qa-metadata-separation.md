# Independent metadata fields and retained identities

Date: 2026-11-05. Scope: PC/Android Shell result metadata, debug headers, local profile metadata, historical reports, Controller TUI, line filters and oversized-result retention. Existing request-name changes remain intact. No device operation, installation, scan, probe, commit or push is part of this change.

## Confirmed defects and corrections

- Normal/debug producers embedded `Target:` inside the `Source` value. Both labels now have independent records/typed fields. Android's string projection also separates `Status` and `Message`.
- Historical reports passed a scope description as the acquired target; cleanup used a fixed word, and TUI measurements used an empty target. Acquisition headers now resolve the recorded agent independently from the check/SSID scope. Session-local Wi-Fi history snapshots the acquisition's agent label rather than using the current selection.
- Android filters previously discarded the role of unchanged multiline/availability fields and could promote identical raw lines into fields. Filtering now carries original line indices; only complete, unchanged owned field spans recover their roles. Explicit removal, trimming, final blank lines, failure footers and safe presentation JSON retain their line contracts.
- Oversized-result metadata retention omitted independent status/message/latency/availability facts. The shared, directly tested summary selection retains these facts and full safe values remain in the existing file-backed Inspect/copy path.
- Local profile metadata and the retained link-report string projection no longer join separately labelled facts. Android Wi-Fi assertions use independent check/expected/actual/passed/message fields. Expected/actual identities retain their semantic labels, so credential-looking SSIDs remain literal while free-form messages and secret-valued facts remain redacted.

Values are not reparsed by their colons. Literal SSIDs, raw data, URLs, IPv6, intentional compact `key=value` summaries, zero/false/unknown distinctions and typed partial/canceled outcomes remain separate from generated metadata. Standard protobuf JSON gains no Source/Target keys; existing raw-debug JSON keeps its independent keys. Explicit ADB raw bundle JSON remains a distinct raw acquisition boundary, not the safe interactive presentation JSON.

## Independent adversarial QA

One visible top-level OpenCode worker uses `openai/gpt-6.1-sol-fast#xhigh`. Catalog model/variant, worker-only environment, original interactive shell, visible Herdr-Worker identity and an actual startup reply were verified before assignment. The same worker is reused for review rounds; no invisible descendants are used. Measured token usage is unavailable.

The initial unchanged regression corpus reproduced 36 failing Go leaf cases out of 38 (7 top-level tests, 5 failing) and 8 failing Android JVM tests out of 14. These include ordinary command families, all five raw-debug views, widths 0/8/24/40/80/120, historical results, independent filter exclusion and presentation JSON. All initial cases passed after the first fixes. Subsequent review extended the corpus rather than weakening initial expectations.

The second round reproduced two further history-budget violations: a short visible field retaining an oversized hidden wire value, and an oversized first table bypassing summary selection. Both now leave full values in the file while keeping the summary bounded. Three typed-boundary cases also showed a report part borrowing the aggregate agent when its own agent was different, unlisted or missing. Per-part scope resolution now preserves each identity or explicitly reports unavailable metadata. These synthetic cases do not claim that normal core acquisition produces mixed-agent parts or that an Activity crash was observed.

Final independent checks passed **41 Go leaf cases / 8 top-level tests** and **24 focused Android JVM tests**. The additions include 260 pipeline-line comparisons, ten exact JSON/outcome comparisons, duplicate raw/owned text, complete availability/wire-value retention, real safe-value file reads and expected/actual identity/message protection. The only change to an original expectation-bearing fixture was the semantically identical Go literal modernization from `proto.Bool(false)` to `new(false)`; assertions were not weakened or skipped.

## Final verification

Passed on the corrected source:

- All Go packages and **50 real-readline PTY cases** via `make test TARGET=controller`; race checks for render/app/tui/shell/pipeline/command.
- `make lint TARGET=controller`, formatting, modernizer, generated-contract drift and whitespace checks; Controller build; device-free tagged PMTU timeout regression.
- Android **290 JVM tests / 50 suites**, zero failures/errors/skips; `:agent:lintDebug` with **zero lint issues**; application and instrumentation APK builds. Gradle's existing deprecation notice remains. Instrumentation was built, not installed or executed.
- Independent worker results, actual added tests, targeted source changes and retained safe-value behavior reviewed by the supervisor. Documentation links/fences/whitespace and PTY script syntax checked. Existing request-name changes remain uncommitted; no commit or push was made.

The installed staticcheck binary was built with Go 1.25 and could not analyze the Go 1.26 project. Cached staticcheck v0.8.2 source was compiled offline with Go 1.26.8 into an ignored build directory; the normal lint target then passed. No dependency or project configuration was changed.

## Limits

Native instrumentation covers generated headers, measured layout and synthetic assertion rendering, but build success is not execution evidence. Real Android pixels, fonts, IME, overflow/Inspect/clipboard UI, accessibility and live device/network behavior remain unverified. This bounded adversarial corpus does not prove that every possible payload, pipeline or device is defect-free.
