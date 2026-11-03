# Status display follow-up: adversarial QA

Date: 2026-11-04. Scope: the help/history improvements, lightweight status overview, focused EHT view, and hierarchical compact layout following the Junos cutover. All reproductions are synthetic and device-free; this is not live Android UI acceptance.

## Reproduced defects and fixes

| Attack | Before the fix | Root-cause fix and regression |
| --- | --- | --- |
| Credentials in framework state, Passpoint names, redaction metadata, proxy URLs, NetworkSpecifier and Private DNS strings | PC overview and full diagnostics retained synthetic credentials in newly exposed fields | Sanitize these free-form protobuf fields before composition, without changing the original measurement. `TestStatusFollowupQACredentialsInExpandedFields` checks seven independent markers, typed identity retention and failed outcomes. |
| Nested assignments such as `provider=password=...` | Android compound summary values bypassed the generic credential matcher | Recognize `=` as a credential-key boundary in both text sanitizers. `StatusFollowupQaTest.nestedCredentialAssignmentsAreRedactedBeforeCopy` and the compound overview/full tests cover text and safe JSON. |
| A whitespace-padded credential key in a generic IPv6 RA field | PC generic-field normalization and Android's flattened full RA text exposed a bare credential value | Normalize generic keys at the PC sanitizer, and sanitize Android RA values while their field keys are still available. The overview/full credential tests exercise the exact padded key. |
| SSIDs literally named `password=Guest` and `token=Guest` in focused EHT results | Flattening every identity into free-form analysis text caused false credential redaction | Preserve connected identities and the filtered AP identity index as typed fields/table cells, separate from free-form analysis. `StatusFollowupQaTest.focusedEhtRetainsLiteralTypedIdentities` verifies both literal values, typed roles and exclusion by a nonmatching filter. |

Each defect was reproduced by a failing regression before its fix. The repaired tests also ensure that sanitization does not turn a typed failure into success or mutate acquired PC data.

## Other adversarial checks

- Terminal widths 8/12/16/24/40/60/80 and unwrapped output retain Unicode, combining characters, joined emoji and long IPv6 values. Identity newlines cannot forge an independent source record (`TestStatusFollowupQAUnicodeWidthsAndIdentityInjection`).
- Overview text and safe JSON still dispatch exactly one existing status RPC and zero ADB commands. Full diagnostics retain their separate acquisition path and host-source availability reporting.
- Hierarchy, aligned value columns, continuation indentation, compact scalar components, raw-dump exclusion, session-only safe history, and literal identities have dedicated Go/Android regressions. The real-readline driver also covers positive Ctrl-P/Ctrl-N recall and draft restoration.

## Verification and limits

Passed on the final source tree and again on the staged status snapshot:

- `GOTOOLCHAIN=go1.26.8 GOFLAGS=-p=1 make test TARGET=controller`: all Go packages and 11 real-readline PTY cases passed.
- `./gradlew :agent:testDebugUnitTest :agent:lintDebug :agent:assembleDebug :agent:assembleDebugAndroidTest --console=plain`: 230 unit tests, no failures/errors/skips, no lint issues, and both APK builds passed.
- Binding drift, Go formatting and modernizer checks passed. The snapshot has no Git metadata, so its Go formatting was checked directly rather than with the Git-based `fmt-check` target.

Also passed on the final source tree: race checks for app/command/render/shell/pipeline/session, all 11 PTY cases under the race detector, `go vet`, CI-pinned staticcheck v0.7.0, and whitespace checks. Go checks used 1.26.8 to match the checker-supported toolchain. Staged help, history and safety revisions received separate relevant Go/Android checks before commit; the complete staged status revision repeated the full package/unit/lint/build checks above.

The supervisor performed this QA locally. A proposed independent worker failed its no-work startup check because provider authentication was unavailable; it received no assignment and contributed no review. Its pane/tab and layout registration were cleaned up.

Instrumentation APKs are built, not installed or executed. Hardware keys/IME, actual font/layout/rotation/accessibility behavior, device-specific timing and real radio/network behavior remain unverified. No installed ADB, live acquisition, external probe, mutation or push is part of this QA.
