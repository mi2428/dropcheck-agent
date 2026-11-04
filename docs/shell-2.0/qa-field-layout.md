# Field-colon placement and table boundaries

Date: 2026-11-04. Scope: PC and Android Shell field/value rendering and table separation across show, request, test, monitor, reports, local metadata, MLO and structured ADB supplements. No device installation, installed ADB command, scan, radio change or external probe was used.

## Contract and shared fixes

- A field's colon immediately follows its label. Padding after the colon aligns values, with one space after the longest label. Wrapped values hang beneath the value column; narrow layouts keep the colon attached to the final label segment.
- PC owned fields and legacy KV producers share one final wrapper. Private tab boundaries distinguish fields and generated multiline continuations; arbitrary raw text, URLs and value colons are not parsed back into fields. The Wi-Fi summary retains its existing value column while moving padding after the colon.
- Android labels no longer contain generated trailing padding. Native field alignment measures each label including its colon and puts the measured space after the colon. Narrow table/scan record layouts use the same field renderer. Logical/copy fields remain single-space records; pixel alignment is a native-layout concern, not a UTF-16 padding approximation.
- Tables have one blank line above their header and below their last row. The shared writers normalize adjacent/existing blank boundaries, empty tables and lossless record layouts. Native separator lines are included in table fitting budgets. The independent structured ADB MLO table is covered separately.
- Table/Scan logical record projections also have outer separation. Explicit line filters keep their semantics: match-all retains boundaries, find may remove the leading boundary, and except-empty removes blanks. No formatter guesses a table header from raw text or silently restores filtered-out lines. JSON data, literal identities, typed outcomes, unknown/zero/false, safe values and full detail access retain their meanings.
- The preceding unfiltered-history fix remains intact: all submitted commands and drafts stay in session memory, without eligibility filtering or disk history. Input recall is independent from output redaction.

## Adversarial QA and review

One visible top-level OpenCode worker was configured as `openai/gpt-6.1-sol-fast#xhigh`. The supervisor verified its catalog ID/variant, model environment, original interactive shell, visible Herdr-Worker/model/variant screen and actual startup response before assigning work. Provider-internal reasoning metadata and measured token usage were not available.

The initial independent fixtures reproduced pre-colon padding, summary labels carrying padding, inconsistent human metadata columns and missing table boundaries. Initial totals were Go 11 top-level tests with 9 failures and Android 14 tests with 11 failures; these totals also included deliberately reviewed expectations that exceeded the actual rendering contract. In particular, tests were corrected explicitly rather than treating these as product defects:

- A filter deliberately deleting blank lines must not have them reinserted afterward.
- Android logical records do not prove native glyph/pixel positions and do not need a custom terminal-width implementation.
- An 11-character IPv6 value cannot be required to remain unwrapped in ten available columns; the corrected assertion checks complete reconstructed content, every continuation's indent and the width limit.
- Tests of private field builders inspect their final rendered output, not their internal tab encoding.

Accepted visible colon/spacing requirements were not skipped or relaxed. The independently written synthetic ADB regression was adopted into the ordinary `adbdiag` test package, not left as an overlay-only check. Existing tests that mandated a shared colon column were replaced with value-column and attached-colon checks; content/privacy/outcome assertions remain active.

## Verification

Final independent re-QA passed with unchanged accepted expectations: **30 Go top-level / 99 leaf checks**, **1 ADB top-level / 2 synthetic leaf checks**, and **35 focused Android tests**, with zero failures/skips. The re-QA found one additional real defect before completion: a narrow empty PC table lost its known headers. The shared fallback now displays those headers vertically without inventing a data row; the unchanged failing regression and a public empty-DNS fixture both pass. The equivalent native empty-header path and full Scan-record outer boundaries were also corrected and instrumented.

Final supervisor gates passed after the last source/test changes: all Go packages; **48 race-instrumented real-readline PTY cases**; race suites for render/app/shell/pipeline/session/adbdiag; Controller build; **260 Android unit tests / 47 suites, zero failures/errors/skips**; Android lint with no issues; application and instrumentation APK builds. Binding drift, formatting, modernize, vet/staticcheck, whitespace and local documentation checks passed separately. Go used 1.26.8 with `GOFLAGS=-p=1`. No commit or push is part of this follow-up.

The native instrumentation includes measured value-start coordinates, CJK labels, two-space table gaps, table outer boundaries, adjacent/empty tables, existing blank lines, long labels and colon attachment. It is built, not installed or executed. JVM tests do not certify native font fallback, real pixels, TextView terminal blank-line display, accessibility, clipboard UI or IME behavior. This is a bounded adversarial corpus, not proof that every nested payload/pipe combination or device is defect-free.
