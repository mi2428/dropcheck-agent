# Android Shell keyboard QA

Initial QA: 2026-11-07. Latest keyboard-policy update: 2026-11-08. Scope: the system-IME plus on-screen extra-key surface, result controls, and their input/lifecycle boundaries. This is not certification of every Android device, IME, or network operation.

## Current keyboard policy

- Shell requests English with `imeHintLocales` and ASCII capability with `IME_FLAG_FORCE_ASCII`; it does not change the system IME or keyboard settings. The connected Gboard displays English QWERTY and commits literal ASCII. Android documents these as requests, not a guarantee that every third-party IME obeys them. Unicode paste and input-connection handling remain supported.
- All orientations use one row of 14 Dropcheck-specific chips, horizontally scrollable when needed: `Tab`, `↑`, `↓`, `C-c`, `?`, `sh wifi status`, `request wifi scan`, `sh wifi status ip`, `| match`, `| count`, `| display json`, quote, pipe and `sh permissions`. The order follows expected workflow frequency, not telemetry. Default-Network, Wi-Fi-test and last-test chips were removed, not their Shell commands. `sh` uses the existing unique-prefix grammar. IME dismissal, horizontal cursor movement, slash and hyphen remain on the standard keyboard, with existing hardware-key behavior retained.
- Chip dimensions retain a 38.4dp width floor and 37.44dp height floor. Labels use 16sp system sans-serif, natural width above the floor and one line without equal-width stretching. Larger fonts can enlarge a chip to avoid clipping. `C-c` retains the existing Ctrl-C cancellation/idle-draft clearing behavior. Accessible names, native long-press tooltips and keyboard focus remain. These are deliberately compact touch targets, not 48dp targets.
- The extension surface uses the connected Gboard dark palette, sampled from a native screenshot: background `#1C1918`, keys `#312B2A`, text `#EEE3E2`, with 10dp rounded corners, native pressed ripples, 4dp key gaps and 4dp vertical surface padding. Label contrast is 11.08:1. Android does not expose another IME's private theme/font; this is a measured visual match using the system sans-serif font, not automatic synchronization with arbitrary IME themes.
- Query and filter chips only insert text at the cursor or replace the current selection. They never submit a command, acquire state, refresh a scan, or mutate connectivity automatically. Filter insertions include separating spaces. Pressing Enter remains a separate deliberate action.
- The attached-window runner has 22 cases, including frame-observed taps with the IME visible, literal English key taps, real toolbar swipes, all seven command/filter insertions checked against the parser, and real-touch `C-c` interruption with draft preservation. The scan shortcut is checked as a typed fresh-scan request and the IP shortcut as a typed IP-status read, without executing either from a chip tap. Swiping cannot activate a chip and insertion cannot execute a command. Synthetic `InputConnection` checks separately retain composition, busy-history and cancellation boundaries.
- Before the tail correction below, the 16-chip configuration passed its then-current 14 cases in portrait and landscape (28/28), geometry at 200% font scale, `ShellInputInstrumentation`, JVM322/53 suites, Lint0 and both APK builds. The row exceeded the viewport even in landscape, and all chips were reachable. Logs/images are under `agent/build/qa/dropcheck-chips/`. Those checks did not establish correct scrolling after actual command submission.

## Inline terminal input

The fixed, padded input bar has been removed. The live `self>` prompt is one natural-height text line immediately after the last result in the same scrolling content. It scrolls away with older history instead of covering that history. Only the existing one-row shortcut strip remains docked above the IME. Typing or moving the input cursor resumes the tail; merely receiving a result while reading history does not.

Command echoes and in-memory recall retain the exact submitted input, including whitespace, quoting, credentials and URL values. The placeholder/redaction helper and ineffective intermediate styled-string builder were removed. `AgentPresentationBlock.commandLine` retains local input verbatim instead of routing it through result-text sanitization. Parsing still uses the existing trimmed execution boundary; result formatting remains a separate path. The ordinary history budgets and overflow/full-result handling still apply. Credentials entered at the prompt are intentionally visible under this policy.

The prompt was subsequently restored from `%` to `self>` in both the live editor and command echoes. Focused portrait/landscape geometry, verbatim echo/recall and touch-editing checks passed (6/6), together with JVM325/53 suites, zero-issue Lint and both APK builds. Native images are under `agent/build/qa/self-prompt/`; the earlier full matrices below were not rerun for this text-only change.

The editor stays attached while earlier output views are rebuilt. Its draft, selection and synchronous composing span survive reflow without recreating the input connection. The scroll viewport ends above the shortcut strip and IME; hidden keyboard height is no longer simulated by a large padding area inside the transcript. Native caret-visibility requests use the same tail coordinator rather than starting competing smooth-scroll animations. This remains Dropcheck Shell, not a general-purpose terminal emulator.

`geometry` checks that the editor is the final transcript child, directly touches the preceding result, occupies a single font-height line, and is absent from the fixed strip. `burst-tail` checks that repeated renders never detach it. These checks complement the existing input, composition, history, cancellation and selection tests.

### Current lower-third reopening and prompt policy

With the IME hidden, a short tap in the lower third of the Shell output viewport focuses the live input, moves to its tail and requests the keyboard. This applies to both background and read-only result text, consistently while idle or busy. The upper two thirds do not implicitly reopen input. Tapping the input itself still uses the native editor behavior. The threshold is measured within the output viewport, not across the keyboard or shortcut strip.

The ScrollView observes descendant gestures once. Long presses, drags/flings and explicit controls keep their normal behavior; a long press in the lower third can still select output and open Full result without showing the keyboard. This replaces the earlier input-only policy below rather than retaining both policies.

With the IME already visible, a short tap on output/background does not transfer focus from the editor or move the viewport. The pending child click is canceled before native selectable TextView focus handling; long presses, scrolling gestures and actual controls still follow their native paths. No scroll-back correction, delayed focus restoration or keyboard reopen retry is used to hide a jump.

The current `tap-policy` case checks background taps at 15%, 50% and 85%, a real lower-third result-word tap, and both ends of the input, in idle and busy states. `gesture-tail` performs its real history swipe with the IME hidden and verifies that the swipe does not reopen it. `prompt` checks exact raw echoes and recall for whitespace, a synthetic passphrase, a malformed command and URL credentials/parameters. `result-context` covers native long-press inspection with the IME initially shown and hidden. Fresh-scan chips remain insertion-only; QA does not refresh scans or mutate Wi-Fi.

### Visible-IME tap regression and verbatim command echoes

The new `ime-visible-tap` check reproduced a landscape output-word tap moving from **2749px to 2574px**, with drawn-frame positions **2627px, 2574px**, while the IME closed. Native tracing identified the path: selectable result TextView `onTouchEvent` acquired focus from the command editor, requested its own visible rectangle/caret position, and the IME/ScrollView layout change scrolled the viewport. This was not a deliberate lower-third keyboard-open request; that request already required the IME to be hidden.

The fix prevents that short output click from becoming a native focus transfer while the IME is visible, at the shared output dispatch boundary. The new check observes scroll frames and input focus, not just the final position, at upper/middle/lower background and result-word points while idle and busy. It continues observing after layout settles to cover delayed focus/IME effects. Native long-press selection, controls, explicit typing/command tail-following and the hidden-IME lower-third shortcut remain separate regression checks. Temporary tracing was removed. Reproduction evidence is under `agent/build/qa/command-echo-tap/`.

Final coverage passed **84/84 logical case/configuration combinations**: all 22 cases in normal portrait/landscape and eight affected cases in 320dp, 200dp, local RTL, 200% portrait and 200% landscape conditions. The affected group adds visible-IME frame/focus observation to geometry, hidden-IME taps, raw echoes/recall, row swipes, insertion, native selection and `C-c`. It includes **168 tap-policy touches**, **36 command-tail submissions**, and raw echoes containing synthetic test credentials/URLs only. Native images confirm verbatim echoes and a visible keyboard while reading history after output taps. All reported runs returned explicit PASS and `INSTRUMENTATION_CODE: -1`.

Input, lifecycle and both IME-on/off Activity presentation runners passed as well. The supplemental passive/session runner initially failed its fixed-landscape recreation wait. Its fixture assumed a portrait start; it now chooses the opposite of the actual starting orientation and still requires a new Activity and matching configuration. The focused rerun observed `before=2 expected=1` and passed all nine cases without increasing the five-second recreation wait. No product code changed after the 84-case matrix. The initial failure is retained as `read-only-initial-failure.txt` rather than counted as a pass.

Fresh JVM **325 cases across 53 suites**, zero-issue Lint and both APK builds passed. This added a runnable verbatim-presentation check covering credentials, URL values, quoting, malformed syntax, styling, full values and line mapping. Existing result-presentation checks remain separate from the explicitly verbatim local-input boundary.

Final built/installed APK hashes match. Exact display size/density, font scale, rotation settings, active IME, app data-directory inode and ADB reverse baseline were verified. Native before/after, raw-echo and large-font images were inspected; owned screenshot caches were removed. User-owned wrapper changes and the Git index were preserved. The README's command-transcript policy was updated without reverting its pre-existing Gradle-version change. Changes remain uncommitted.

### Previous keyboard-extension verification

The keyboard-extension follow-up passed **77/77 logical case/configuration combinations**: all 21 then-current cases in normal portrait and landscape, and seven affected cases in each of the 320dp, 200dp, local RTL, 200% portrait and 200% landscape conditions. Those seven covered geometry/palette/rounding/typeface, tap policy, prompt, real row swipes, all insertion chips, native Full result selection and `C-c`. This included **84 real tap-policy touches** and **36 command-tail submissions**, with explicit all-pass reports and `INSTRUMENTATION_CODE: -1`. Input, lifecycle, passive/session and both IME-on/off Activity presentation runners also passed. This revision preceded verbatim echoes and the visible-IME focus regression above.

The initial style check exposed edge clipping caused by padding on the horizontal viewport. Horizontal spacing now belongs to the individual keys' margins instead; full-key reachability assertions were retained and passed. Native portrait/landscape and large-text images were inspected, including the initial row alongside the unchanged English Gboard. The fresh-scan chip was only inserted and parsed, never submitted by QA.

Fresh offline checks passed **324 JVM cases across 53 suites**, zero Lint issues and both APK builds. Built/installed APK hashes match; exact display, density, font scale, rotation, active IME and app data-directory baselines were verified. Owned screenshot caches were removed after collection. User-owned wrapper changes and the Git index were preserved. Evidence is under `agent/build/qa/keyboard-extension/`; changes remain uncommitted. The earlier 57-case and 70-case records below describe their own revisions and are not reused as results for this policy/style update.

### Previous input-only policy and verification (superseded)

An earlier revision removed the lower-third zone. Before the usability reversal above, after dismissing the IME:

- Tapping output/background does not request the IME or jump to the live prompt, regardless of vertical position or whether an action is running. Read-only output retains native text selection and the contextual Full result action.
- Tapping the actual input line uses normal editor behavior to focus input and reopen the IME, including while an action runs. It does not execute a command or discard the next draft.
- Explicitly opening the Shell still focuses the prompt and requests the IME, as before. Shortcut chips retain their existing edit/help/history/cancellation actions; no background tap is treated as a shortcut to input.

At that revision the `tap-policy` native case rejected the former 85%-height reopening behavior. Its background/input checks verified IME state, history position, draft/selection retention and action state. `result-context` also started with the IME hidden and checked that long-press inspection did not reopen it. The current short-tap expectations have since been changed as described above.

The new `prompt` case checks `dropcheck>` in the styled command prefix, its exact color-span boundary, the redacted display history and the live editor at the tail. Native screenshots verify the painted prompt as well. No command-redaction behavior was changed.

Tap/prompt follow-up verification passed **57/57 logical case/configuration combinations**: all 21 current cases in normal portrait and landscape, plus geometry/tap-policy/prompt in 320dp, 200dp, local RTL, 200% portrait and 200% landscape configurations. This includes **70 real injected taps** and **36 command-tail submissions** with the IME shown/hidden. Each native run returned an explicit all-pass report and `INSTRUMENTATION_CODE: -1`. The earlier 70-case tail matrix below predates this follow-up and is not counted as a fresh 70-case run.

Input, lifecycle, passive command/session and both IME-on/off Activity presentation runners also passed. Fresh JVM **324/53 suites**, zero-issue Lint and both APK builds passed. Native portrait/landscape, narrow and large-font prompt images were inspected. Built/installed APK hashes match; exact display, font, rotation, IME and app data-directory baselines were verified, and owned screenshot caches were removed. User-owned wrapper changes and the Git index were preserved. Evidence is under `agent/build/qa/tap-policy/`; changes remain uncommitted. This is handset-specific QA, not certification of other IMEs/OEMs.

The final ordinary `am start -W` returned a launch-wait timeout. An immediate, separate system-state check confirmed MainActivity was top-resumed, visible and fully drawn (`reportedDrawn=true`, `allDrawn=true`, `nowVisible=true`), with no new crash for its process. No longer timeout or automatic relaunch was used; the wait-report discrepancy remains unverified and is not counted as a startup-performance pass.

## Command-output tail regression

The new `command-tail` check reproduced `show wifi status` stopping **3,796px before the tail**, with the IME visible and no manual scrolling after submission. `burst-tail` independently reproduced batched appends stopping **1,794px before the tail**. A later status-only growth check also reproduced a **115px** gap without appending a result. Content-height changes now preserve the previous tail state as well. The old `prepare()` helper explicitly moved to the tail before testing; passing keyboard geometry or waiting for command completion did not prove that the completed output was visible.

Shell had competing posted operations: output requested a smooth scroll using possibly stale content height, while every render restored its previous anchor, and asynchronous completion rendered twice. Shell now reuses `TerminalAutoScrollState`, resolves one coalesced position immediately before drawing after layout, and gives an explicit tail request priority over stale anchors. Width reflow is no longer posted behind position restoration. Completion no longer redraws a second time. Recreation retains whether the reader was following the tail, rather than restoring an old tail pixel offset over a new layout.

- Submitting a nonempty command resumes following, including from older history. Successful, invalid, filtered-failure and empty results use the same position path.
- A real history-scroll gesture cancels queued movement. Incoming results and IME changes retain the older block/offset instead of pulling the reader back. A later explicit command resumes following.
- `command-tail` checks 18 read-only/local submissions per configuration: Wi-Fi status, cached scan, IP status, permissions, count, JSON, local CLI output and an invalid command with the IME shown/hidden, plus submission from history in both IME states. Current reads exercise `sh` aliases; the cached-scan command remains passive, distinct from the new fresh-scan insertion chip. The check never scrolls to repair the result after submission.
- `burst-tail` covers same-frame appends, repeated renders and the attached editor; `async-tail` covers line/structured/filtered-failure/empty results, tail/history, and next-draft selection/focus; `reflow-tail` changes output width, viewport height and status-only content height; `gesture-tail` uses a real vertical swipe during an action, checks anchored completion, then resumes through typing and an explicit command. The Ctrl-C case also checks its result position.
- `ShellLifecycleInstrumentation` additionally checks in-flight completion at the tail and both tail/history restoration across recreation. Its new history assertion initially reproduced restoration to zero before width reflow was moved ahead of position resolution.

Evidence for this regression is stored separately under `agent/build/qa/shell-tail/`; earlier 84/91/28-case records are not used as proof that these new assertions already passed.

### Final tail and inline-input verification (2026-11-08)

All **70 logical case/configuration combinations passed**, including **126 command-input tail checks**. The two IME subruns count as one logical `command-tail` case per configuration; every subrun also returned an explicit all-pass report and `INSTRUMENTATION_CODE: -1`.

| Configuration | Root width | Font scale | Logical cases |
| --- | ---: | ---: | ---: |
| Portrait | 1080px | 0.85 | 19/19 |
| Landscape | 2424px | 0.85 | 19/19 |
| Narrow 320dp window | 712px | 0.85 | 6/6 |
| Narrow 200dp window | 445px | 0.85 | 6/6 |
| Local RTL root | 1080px | 0.85 | 6/6 |
| Portrait, largest text | 1080px | 2.0 | 7/7 |
| Landscape, largest text | 2424px | 2.0 | 7/7 |

The six-case subset is command/burst/async/reflow/gesture tail plus IME following; the largest-text runs add geometry. After the test-only native-height measurement correction, both normal geometry checks and both largest-text configurations were repeated. No product-code change occurred between these final matrix segments. Representative native images were inspected in normal, narrow, RTL and largest-text portrait/landscape conditions.

Five additional native runs passed: input, lifecycle, passive command/session checks, and Activity presentation with the IME hidden/shown. The latter covered 22 widths with anchor/selection retention, with accessibility nodes measured in 20/22; the other two are not claimed as measured. The passive runner still reported missing IPv6 default routes/DNS explicitly, not as a network-readiness pass.

Final offline gates passed **324 JVM cases across 53 suites**, with no failures/errors/skips, zero Lint issues and both APK builds. Installed APK hashes match the built files. Exact display size/density, font scale, rotation settings, active IME, app data-directory inode and empty ADB reverse rules were verified. Owned native screenshot caches were removed after collection. User-owned wrapper changes and the unstaged index state were preserved. Changes remain uncommitted.

## Changes and reproduced defects

- Removed permanent per-result copy/inspect rows and their obsolete resources. Native text selection remains available. The contextual **Full result** action opens either the complete retained in-memory result or the existing paged, file-backed safe values. Large-result disclosure points to that contextual action instead of a nonexistent button.
- Kept the extra keys docked above the IME while moving the editor into the transcript as described above. Reflow leaves the live editor attached, and its prompt is visible at the tail on startup.
- Disabled baseline alignment in key rows so fallback-font arrow glyphs do not clip neighboring buttons. The toolbar uses the dimensions and measured palette above; the inline editor uses a natural text line instead of a 48dp bar. Current chip label contrast is 11.08:1; terminal text is 21:1. These calculations are not a complete accessibility certification.
- Use one horizontally scrollable, intrinsic-width key row in every orientation. At 200% landscape text scaling the original two-row arrangement consumed nearly all output height; the earlier landscape correction was extended to portrait in the follow-up above.
- Esc hides the IME without discarding hardware input focus. Result delivery does not reopen the keyboard or steal selection focus. Help and empty Enter preserve the hidden-IME state.
- Replaced the old IME-show timing path with the native `WindowInsetsController`, available below the project's minimum API level. Its show request is applied when the window gains control; no compatibility dependency or retry loop was added.
- Handle terminal Ctrl-C and ordinary Enter before IME processing. Enter ownership is decided on key-down so confirming an active IME composition cannot accidentally submit a command on key-up. Canceled raw Enter is consumed without execution. Ctrl-C repeat events cannot erase the retained next draft after cancellation completes.
- Replaced scaled-space field padding with native tab stops, matching the existing table strategy. The old native test reproduced different value-column positions for Japanese and Latin labels; the same assertions now pass.

The offscreen input, native field-column mismatch, premature composition submission, canceled raw Enter, and held-Ctrl-C draft loss were observed before their fixes. Native regression checks retain normal controls as well as rejection/cancellation paths. No real Wi-Fi mutation or external probe was used to reproduce them.

## Native checks

The connected test handset is a Pixel 9 running Android 17 / API 37, with a 1080×2424 display, existing density override 356, Japanese system locale, and the existing Gboard IME. Replacement installs used `-r -t`, without uninstall, data clear, `-g`, permission changes, or IME replacement.

The original keyboard matrix covered:

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

- The Activity anchor check now waits for width restoration to complete before selecting a genuinely non-tail history position, and waits for settled layout after rendering. The previous fixture could sample an unfinished width reflow while still at the tail, then mislabel the subsequent tail correction as an anchor regression. Its offset and native-selection assertions remain intact. This preparatory wait does not scroll after a command result or repair a failed tail assertion.
- Prompt-height QA compares the actual native `Layout.height` plus compound padding with the editor's measured height. A `Paint.fontMetricsInt` estimate incorrectly rejected Android's 64px single-line layout at 200% text scaling. The corrected assertion still rejects a padded/minimum-height bar, requires exactly one line and requires direct adjacency to the preceding result. Normal portrait/landscape geometry was repeated after this test-only correction.
- Synthetic composing-span retention is checked synchronously during reflow, before the real IME can reconcile an artificial span with its own state. Attached-editor and draft retention are also checked after layout settles. This is not a claim about the real IME's composing state.
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
  -e orientation portrait -e capture true -e case command-tail -e ime true \
  io.dropcheck.agent.test/io.dropcheck.agent.ShellKeyboardQaInstrumentation
adb -s <approved-serial> shell am instrument -w -r \
  -e orientation portrait -e capture true -e case command-tail -e ime false \
  io.dropcheck.agent.test/io.dropcheck.agent.ShellKeyboardQaInstrumentation
adb -s <approved-serial> shell am instrument -w -r \
  -e orientation portrait -e capture true \
  -e case ime-tail-follow,burst-tail,async-tail,reflow-tail,gesture-tail \
  io.dropcheck.agent.test/io.dropcheck.agent.ShellKeyboardQaInstrumentation
adb -s <approved-serial> shell am instrument -w -r \
  io.dropcheck.agent.test/io.dropcheck.agent.ShellInputInstrumentation
```

Optional `case` (one name or a comma-separated list), `orientation`, `widthDp`, `rtl`, and `capture` arguments narrow or extend the check. For `command-tail`, `ime=true` or `ime=false` splits its 18 submissions into two nine-command runs; omitting it tests both states. Omitting `case` runs all 22 cases. For the latest correction, use `-e case ime-visible-tap,prompt,result-context` as a focused group. Use bounded groups rather than extending a timed-out host wait: the initial combined RTL command run reached its host limit and was not counted as a pass. Each case and command emits a phase status. No Japanese composing-key prerequisite is required. Width and rotation are Activity-local. Host-driven font-scale changes must be restored to their exact baseline, including on failure. The command-tail case reads existing state only: it does not refresh scans, change connectivity or run external probes.

Only an explicit all-pass report plus `INSTRUMENTATION_CODE: -1` counts as success. A zero ADB process exit code is insufficient. Capture files use the `keyboard-qa-` prefix in the app cache and must be collected and removed after inspection.

## Limits

Testing with TalkBack enabled, other IMEs/OEMs, API 31–36 hardware, floating keyboards, every possible input sequence, clipboard payload transfer, and network functionality are not certified by this work. The narrow test changes the Shell root width, not the operating system's multi-window mode or dialog window width. Native accessibility-node and target-size checks do not replace user testing with assistive technology. This QA did not revert unrelated Go, wrapper, or earlier Kotlin work; follow-up commit review keeps user-owned wrapper changes separate.

Reference: [native inset control semantics](https://developer.android.com/reference/android/view/WindowInsetsController#show(int)), updated 2026-08-03 and checked 2026-11-07.

Reference: [ASCII keyboard request and locale hints](https://developer.android.com/reference/android/view/inputmethod/EditorInfo#IME_FLAG_FORCE_ASCII), checked 2026-11-08.
