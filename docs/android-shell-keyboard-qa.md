# Android Shell keyboard QA

Date: 2026-11-07. Scope: the system-IME plus on-screen extra-key surface, result controls, and their input/lifecycle boundaries. This is not certification of every Android device, IME, or network operation.

## Changes and reproduced defects

- Removed permanent per-result copy/inspect rows and their obsolete resources. Native text selection remains available. The contextual **Full result** action opens either the complete retained in-memory result or the existing paged, file-backed safe values. Large-result disclosure points to that contextual action instead of a nonexistent button.
- Docked the input and extra keys together above the IME. Transcript reflow no longer detaches the editor. This fixes the observed offscreen startup prompt and separates input editing from transcript scrolling/selection.
- Disabled baseline alignment in key rows so fallback-font arrow glyphs do not clip neighboring buttons. Keys and the input retain 48dp minimum targets. Configured default label contrast is 9.74:1; terminal text is 21:1. These palette calculations are not a complete accessibility certification.
- Use two aligned key rows in portrait and one horizontally scrollable, intrinsic-width row in landscape. At 200% landscape text scaling the original two-row arrangement consumed nearly all output height; the compact arrangement preserves the labels and target sizes and leaves at least two editor-text lines for results in the measured configuration.
- Esc hides the IME without discarding hardware input focus. Result delivery does not reopen the keyboard or steal selection focus. Help and empty Enter preserve the hidden-IME state.
- Replaced the old IME-show timing path with the native `WindowInsetsController`, available below the project's minimum API level. Its show request is applied when the window gains control; no compatibility dependency or retry loop was added.
- Handle terminal Ctrl-C and ordinary Enter before IME processing. Enter ownership is decided on key-down so confirming an active IME composition cannot accidentally submit a command on key-up. Canceled raw Enter is consumed without execution. Ctrl-C repeat events cannot erase the retained next draft after cancellation completes.
- Replaced scaled-space field padding with native tab stops, matching the existing table strategy. The old native test reproduced different value-column positions for Japanese and Latin labels; the same assertions now pass.

The offscreen input, native field-column mismatch, premature composition submission, canceled raw Enter, and held-Ctrl-C draft loss were observed before their fixes. Native regression checks retain normal controls as well as rejection/cancellation paths. No real Wi-Fi mutation or external probe was used to reproduce them.

## Native checks

The connected test handset is a Pixel 9 running Android 17 / API 37, with a 1080×2424 display, existing density override 356, Japanese system locale, and the existing Gboard IME. Replacement installs used `-r -t`, without uninstall, data clear, `-g`, permission changes, or IME replacement.

`ShellKeyboardQaInstrumentation` covers:

1. Cold-start input visibility without a preparatory focus/scroll operation.
2. Actual input/key/IME coordinates, clipping, all key labels, 48dp areas, and horizontal reachability.
3. Injected screen taps for completion, selection replacement, and emoji/combining/ZWJ cursor movement.
4. Real long press and real menu taps for in-memory and file-backed full values, without permanent result controls.
5. Real IME-generated composition and physical Enter confirmation without early submission.
6. Canceled raw Enter, with normal raw Enter as a positive control.
7. Held physical Ctrl-C across action completion without next-draft loss.
8. Busy-history and cancellation preserving the real IME composing range.
9. Physical editing and exactly-once local submission after Esc.
10. Physical cancellation after Esc and late completion without reopening the IME.

These are 12 independently reported cases: the paired busy and Esc checks have separate outcomes. Synthetic latch actions and a local-only `show cli` command are used; acquisition commands are not dispatched.

Initial keyboard matrix: **84/84 cases passed**, with explicit successful instrumentation results in all seven configurations. The later 91-case pre-commit recheck below adds an IME-transition regression that this initial matrix did not cover.

| Configuration | Measured root width | Font scale | Result |
| --- | ---: | ---: | --- |
| Portrait | 1080px | 0.85 | 12/12 |
| Landscape | 2424px | 0.85 | 12/12 |
| Narrow 280dp Shell root | 623px | 0.85 | 12/12 |
| Portrait, larger text | 1080px | 1.3 | 12/12 |
| Portrait, largest text | 1080px | 2.0 | 12/12 |
| Landscape, largest text | 2424px | 2.0 | 12/12 |
| Local RTL Shell root | 1080px | 0.85 | 12/12 |

The exact original font scale, 0.85, was restored in `finally`. The result logs and matrix summary are under `agent/build/qa/keyboard-live/`. A prior narrow-view missed-tap fixture was corrected by waiting for a refreshed, visible, stationary menu target. The final full run passed without tap retries; this does not establish a long-run flake rate.

Fresh offline checks passed: **322 JVM cases across 53 suites**, no failures/errors/skips, **zero Lint issues**, and both APK builds; all 92 requested Gradle tasks executed. Gradle still reports deprecated features incompatible with Gradle 10; this does not prevent the current cached 9.9.0 build.

Additional native runs passed:

- `ShellInputInstrumentation`: editing, composition, extra keys, cancellation and viewer switching.
- `ShellLifecycleInstrumentation`: recreation during execution, exactly-once delivery, exception/retry and finish interruption.
- `PresentationInstrumentation`: 24 native layout cases, three credential cases, one identity boundary and five invalid endpoints.
- `ActualActivityPresentationInstrumentation`: 11 widths with IME hidden and 11 with IME shown; anchor, input, output selection and no permanent result controls. Accessibility nodes were measurable in 10/11 and 11/11 respectively; the remaining case is not claimed as measured.
- `ShellReadOnlyInstrumentation`: nine passive/session cases, including rotation. Its obsolete colon-suffixed metric assertion was replaced with retained typed-table and current JSON-record checks; it then passed without connectivity changes. The observed network itself did **not** pass dual-family readiness: IPv6 default routes and DNS servers were absent and were reported explicitly, not converted into success.

The final APK byte hashes match the installed files. Display size, density override, font scale, rotation settings, active IME, empty ADB reverse rules and the original app data-directory inode were checked. No uninstall, data clear, permission grant/revoke or IME switch was performed. The complete initial runtime-grant list was not persisted, so a full permission-state equality comparison is not claimed. A supplementary build/Lint run passed after the test-only passive assertion correction.

## Ordinary launch and capture limitation

Native screenshots and all keyboard geometry/touch assertions pass. After an ordinary non-instrumented cold launch, ADB `screencap` repeatedly omitted the key row and IME, despite the focused editor/key geometry and input-method service reporting an active input view. An additional native geometry run and its native screenshot again passed. The operator then confirmed that the editor, extra keys and Gboard were all visible on the physical screen. No ordinary-launch rendering defect was established; the capture discrepancy's exact cause remains unverified. No fallback, privacy bypass, IME replacement or speculative rendering fix was added.

Use the existing native `capture=true` path for autonomous screenshot-based inspection on this handset rather than relying on the inconsistent ADB image or repeatedly waiting for operator responses. The final native portrait image and six condition-specific geometry images were visually inspected. Owned device-cache screenshots and temporary UI dumps were removed after collection. Display settings were restored, and the review agent, dedicated tab and layout registration were closed. Follow-up reviewed changes are committed by concern; user-owned wrapper changes remain separate.

## Test-fixture corrections

### Adversarial pre-commit recheck

A subsequent independent source review identified a missing transition test: opening the IME while already reading the transcript tail could leave the tail beneath the controls and disable following of incoming results. The new real-tap `ime-tail-follow` case failed before correction. `updateShellContentPadding` now records whether the reader was at the tail before changing padding and reuses the existing tail scroll only in that case. The same test also checks that opening the IME and appending results do not move a reader who is viewing older history. No new state, dependency, or scrolling framework was introduced.

This extends the native runner from 12 to 13 cases. The original 84-case record above predates this added transition; it is not evidence that the missing case already passed. The extended matrix passed **91/91 cases** across the same seven configurations, with explicit successful instrumentation results and font-scale restoration. Logs are under `agent/build/qa/commit-recheck/`. Input, lifecycle and both IME-on/off Activity presentation suites also passed again; the latter checked 22 width cases, with accessibility nodes measured in 20/22 and the other two explicitly unmeasured. Fresh JVM/Lint/APK checks passed again. Native screenshots confirm the retained tail marker above the controls and preservation of the older-history view, including 200% landscape text.

The Go recheck passed all packages, versioned local-proxy consumption, 50 real-Readline cases, formatting, vet, CI-pinned Staticcheck 0.7.0 with Go 1.26.8, and race checks for the four changed runtime/API packages. The first unrestricted package-parallel run hit two existing process-start/deadline fixture limits on a busy host; the existing startup diagnostic and isolated cases passed, followed by the full suite with package parallelism bounded to two. Neither deadlines nor assertions were relaxed. An old global Staticcheck binary was rejected as incompatible rather than counted as a pass.

### Earlier fixture corrections

- An artificial `InputConnection` composing span is not proof of the active IME's internal state. Composition checks now tap the configured real IME key and observe its actual composing range.
- Hardware character mapping is IME-dependent; navigation/deletion checks do not assume that an injected Latin key produces a particular character.
- Text-selection action menus can be rendered by SystemUI. A menu-label accessibility node need not itself support `ACTION_CLICK`; the test uses real screen taps rather than mistaking that for a product failure.
- Coordinate settling includes transcript scrolling, not just the fixed input panel. Exact index labels distinguish the value index from background result text. A context-menu target must also be visible and stationary before its single injected tap; the test does not retry failed taps or weaken the retained-value assertion.
- After changing font scale, a cold launch was measured at approximately six seconds. The test now uses the project's standard `startActivitySync` lifecycle wait instead of an unrelated four-second polling limit. Host instrumentation runs remain bounded. This is not a startup-performance benchmark or SLA claim.

## Reproduction

From the repository root, run the JVM/Lint gates and build the two debug APKs. `--offline` requires the documented SDK and Gradle dependencies to be cached already; it is not a first-install command.

```sh
./gradlew --offline :agent:testDebugUnitTest :agent:lintDebug \
  :agent:assembleDebug :agent:assembleDebugAndroidTest \
  -PdropcheckVersion="$(make -s version)"
```

Use one explicitly approved serial throughout. On a test handset, install both APKs without clearing data or granting permissions automatically:

```sh
adb -s <approved-serial> install -r -t agent/build/outputs/apk/debug/agent-debug.apk
adb -s <approved-serial> install -r -t agent/build/outputs/apk/androidTest/debug/agent-debug-androidTest.apk
```

Unlock the handset, retain its existing IME, and run:

```sh
adb -s <approved-serial> shell am instrument -w -r \
  -e orientation portrait -e composingKey あ \
  io.dropcheck.agent.test/io.dropcheck.agent.ShellKeyboardQaInstrumentation
```

`composingKey` must name a visible key on the current IME that creates a composing range; a missing prerequisite is a failure, not a skipped pass. Optional `case`, `orientation`, `widthDp`, `rtl`, and `capture` arguments narrow or extend the check. Width and rotation are Activity-local. Host-driven font-scale changes must be restored to their exact baseline, including on failure.

Only an explicit all-pass report plus `INSTRUMENTATION_CODE: -1` counts as success. A zero ADB process exit code is insufficient. Capture files use the `keyboard-qa-` prefix in the app cache and must be collected and removed after inspection.

## Limits

Testing with TalkBack enabled, other IMEs/OEMs, API 31–36 hardware, floating keyboards, every possible input sequence, clipboard payload transfer, and network functionality are not certified by this work. The narrow test changes the Shell root width, not the operating system's multi-window mode or dialog window width. Native accessibility-node and target-size checks do not replace user testing with assistive technology. This QA did not revert unrelated Go, wrapper, or earlier Kotlin work; follow-up commit review keeps user-owned wrapper changes separate.

Reference: [native inset control semantics](https://developer.android.com/reference/android/view/WindowInsetsController#show(int)), updated 2026-08-03 and checked 2026-11-07.
