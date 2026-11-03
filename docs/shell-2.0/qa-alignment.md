# Shell alignment: adversarial QA

Date: 2026-11-04. Scope: the current PC/Android Shell presentation changes, including help, scan, request results, test reports, monitor/cycle data, MLO, local metadata, filters and safe copy/JSON. No device, installed ADB, scan, probe, radio mutation, install or push was used.

## Requested display contract

- `AGE(s)` is left-aligned. Integer arithmetic preserves exact milliseconds as seconds with up to three decimal places, including values above 2^53 and the uint64 maximum. Missing is `?`, not zero.
- Tables use at least two display-space widths between columns. `eap_wpa3_enterprise` has the exact alias `wpa3-eap`; unknown security spellings are retained.
- Structured fields share value columns and hanging continuation indentation. PC uses display-cell widths; Android uses native text measurement/spans. Long labels or values select a lossless narrower layout, not truncation.
- Comparable AP, probe, address, cycle and test-stage/metric observations use tables. MLO metadata and comparison rows retain Field/Table roles at their generation points rather than being flattened into text before native layout.
- Raw/native reference text is not parsed as fields from its colons. JSON bypasses human alignment. Filters operate on complete safe logical output before width reflow; exact retained Android field facts preserve their roles through line filters.

## Independent QA and reproduced defects

The independent QA worker was configured as `openai/gpt-6.1-sol-fast#xhigh`. The supervisor verified the catalog ID/variant, model environment, visible Herdr-Worker identity, `GPT-6.1 Sol Fast / xhigh` screen and an actual startup response before assigning work. The provider's internal reasoning metadata was not separately queried. Earlier read-only source observations from another model were not used as adversarial-QA acceptance evidence.

The initial independent pass left failing regressions in `controller/internal/render/alignment_independent_qa_test.go` and `agent/src/test/java/io/dropcheck/agent/AlignmentIndependentQaTest.kt`: five of eight Go top-level tests and three of six Android tests failed. Not every defect below was introduced by this diff; these are defects reproduced in the inspected tree, not a claim about their original introduction.

| Reproduced defect | Shared fix |
| --- | --- |
| PC display normalization collapsed distinct tab/space SSIDs before filtering, dropping the selected AP or admitting another BSSID | Select literal identities on one detached raw observation first, then sanitize that selection; remove duplicate post-sanitization selectors. Preserve acquired totals and the original proto. |
| A canceled acquisition's separately rendered Reason exposed a credential marker | Sanitize that reason at its existing free-text boundary; do not indiscriminately re-redact typed SSIDs. |
| Arbitrary long diagnostic keys split the FIELD/VALUE header and rows even when each wrapped line met the width budget | Render the original DiagnosticField pairs as aligned typed KV, not a table that is later hard-wrapped. |
| PC scan detail omitted the same AP's safe Raw reference | Share the scan reference-evidence renderer, without a new acquisition or duplicate decoded detail. |
| PC interactive traceroute omitted max hops, payload size and exit code | Retain the three typed settings/completion fields. |
| Android traceroute omitted observation errors and made missing reached-target indistinguishable from observed false | Preserve observationError and hasReachedTarget as typed fields in display, safe copy/JSON and Inspect values. |
| Android lowercase diagnostic `ssid` redacted a legitimate credential-looking SSID | Match the existing identity-label allowlist case-insensitively while retaining credential redaction for free text and secret keys. |

Additional checks reproduced a wrapped long label losing indentation, and MLO's known KV/table rows losing their roles before native layout. The common wrappers now retain indentation, and MLO shares one typed generation path. String-only callers project those same parts. New MLO role tests failed before that fix and passed afterward. Multiline native table cells explicitly select record layout rather than being measured as a single-line table.

## Coverage and integration checks

- All 20 PC result payload alternatives receive a smoke test at 8/24/40/80/120 columns. The independent pass additionally populated all 20 families, including request/test/monitor/cycle/MLO, with long CJK, combining characters, ZWJ emoji, IPv6, missing/zero/false and failed/partial results.
- Exact AGE boundaries include 0/999/1000/1001, values near 2^53, and MaxUint64-1/MaxUint64. Table tests check AGE's actual left edge, two-cell gaps and wide-character columns, not just output length.
- 27-AP cases retain every BSSID and long credential-looking identity through logical filters and safe presentation JSON. Raw/URL colon boundaries, input-proto immutability, acquisition failure and credential markers have explicit checks.
- The supervisor's integrated Go all-package tests, 11 real-readline PTY cases, six relevant race suites and Controller build passed. The PTY driver uses a local fake process, not installed ADB or an Agent RPC.
- Integrated Android verification passed: **244 unit tests in 46 suites, zero failures/errors/skips**, no lint issues, and application/instrumentation APK builds. The native instrumentation fixtures assert measured CJK field columns, two-space native table gaps, AGE values, multiline cells and wrapped indented reference text; they were built, not executed.
- The same configured Sol Fast/xhigh worker completed independent post-fix re-QA: **14 Go top-level tests plus 44 subtests, and 24 focused Android tests**, zero failures/skips. The original seven reproductions remain unchanged and now pass. Additional ANSI-normalization collisions, SSID/BSSID selection, acquired totals, original-proto/one-shot-JSON preservation, and multiline raw-continuation cases also passed. No new reproduced defect remained in that pass.

Checks used `GOTOOLCHAIN=go1.26.8 GOFLAGS=-p=1`, the repository Make targets, CI-pinned staticcheck v0.7.0, and the repository Gradle tasks. Generated bindings, formatting, modernizers, vet/staticcheck and whitespace all passed separately from functional assertions. Final full integration checks were repeated after the independent worker's last test-only additions. No commit or push is part of this follow-up.

## Limits

Unit tests verify typed layout roles and logical values, not real font/pixel positions. Actual Android rendering, font fallback, IME/physical keys, clipboard/Inspect UI, rotation/accessibility, device timing and radio/network behavior remain unverified. Full-safe-value storage limits and every nested optional-field/pipe permutation are not exhaustively covered. These checks do not justify a claim that every possible output or device is free of alignment defects.
