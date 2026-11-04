# Raw-only debug separation

Date: 2026-11-04. This follow-up supersedes the earlier rule that `show wifi status full` includes whole-object raw references. The change is local-only, without device installation, live scans/probes, commits or push.

## Contract

- Ordinary snapshot rendering omits whole-object WifiInfo, MloLink, ScanResult, LinkProperties and NetworkCapabilities string dumps, including full/detail and historical/test/monitor snapshots. It preserves structured IE `id/extension/bytes/hex`, security/capability hex, numeric bit fields, availability, zero/false values, literal identities and typed outcomes.
- Five portable `debug android` commands reuse passive existing RPCs: Wi-Fi info, cached scan, and explicit-Network capabilities/link-properties/WifiInfo. The Network ID namespace remains distinct from Wi-Fi configuration IDs. Every network debug view pins a canonical explicit ID and rejects cross-Network/contradictory identities without fallback.
- Raw-only output retains minimal result/source/target/latency/error metadata and normal sanitization. It does not append structured summaries, IE tables or optional PC host supplements. Empty/missing raw data is a visible failure; failed partial data stays failed and remains reference evidence. Debug is a new acquisition, not a saved replay of a previous show.
- Interactive raw ADB collection moves from `show adb` to `debug adb`; all ten allowlisted sources, target pinning, whole-collection deadlines, cancellation, per-source capture status and existing sanitization remain in the same collector. Android does not claim host ADB availability.
- The independent PC one-shot CLI gains the debug namespace without renaming its existing supported commands. Shared structured human/result-JSON projections omit string dumps; wire acquisition schemas and evaluation are unchanged. No arbitrary shell, privilege escalation, active scan, new dependency or cross-source structured-field merge is added.

## Regression coverage

- Shared grammar fixtures include each new command, invalid/missing/overflowed IDs, duplicate/incompatible filters and rejected active-scan syntax.
- Go parser tests verify passive RPC reuse, literal scan filtering, exact Network ID propagation, one-shot debug entry points, removal of interactive `show adb` and preservation of independent one-shot spellings.
- Go raw-renderer tests cover only the requested source, partial failure/privacy, absent/wrong/mismatched payloads, detached rendering, recursive structured JSON omission and retained IE/security hex. The previous Unicode/long-raw layout corpus now exercises debug at widths 8/24/40/80/120 rather than requiring raw dumps in show.
- Android tests exercise the same source projections, failed status/safe JSON, literal filters, unsupported host debug, target mismatch and unavailable raw behavior. Full-bundle fixtures still require structured/IE evidence and now reject every whole-object marker.
- An application regression ensures Android raw debug neither collects nor appends ADB supplements. Real-readline PTY coverage includes debug recall and raw-ADB cancellation through the existing fake executable, never installed ADB.

## Verification and limits

Final gates passed:

- `GOTOOLCHAIN=go1.26.8 GOFLAGS=-p=1 make test TARGET=controller`: all Go packages and **50 real-readline PTY cases**, including raw-debug recall and fake-ADB cancellation.
- Race tests for command/render/app/shell/pipeline/harness/linuxcli; `go vet`, staticcheck 2026.1, modernize, generated-contract drift, formatting and whitespace checks.
- Controller build; Android **264 unit tests / 48 suites, zero failures/errors/skips**; Android lint with no issues; application and instrumentation APK builds.
- Local documentation links/fences, Python syntax and explicit new-file formatting/whitespace checks.

Existing full/raw assertions were updated only for the approved placement change; structured values and itemized IE hex remain required. The Unicode/long-raw corpus was preserved under debug. A new disconnected-WifiInfo fixture initially omitted its intended `STATUS_OK`; it was corrected explicitly, without teaching production to treat unspecified status as success.

No new live-device result is claimed. APK build success does not validate real native pixels, fonts, Inspect/copy UI, IME, device permissions, OEM dump formats or live ADB behavior. Existing uncommitted history and field/table-layout fixes are preserved.
