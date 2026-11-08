# In-app Log Viewer history

This is the viewer reached from Agent Shell or a widget tap. Unlike the widget's
latest-only snapshot, it provides full retained-history scrollback.

## Resource bounds and navigation

- A native ListView recycles visible TextViews. Each disk page has at most
  256 physical rows and a 256 KiB byte budget. The app retains a moving window
  of at most 512 rows / 512 Ki UTF-16 source characters; only attached rows are
  decorated and laid out. Retained history is never one large styled string.
- Older/Newer/Latest buttons and normal count labels were removed. Reaching
  the upper/lower edge loads the adjacent page automatically. Exclusive
  generation/byte-offset cursors distinguish even identical physical rows.
- Only the actual tail follows new arrivals. A press invalidates pending reads;
  touches and flings prevent adapter updates and paging/anchor application.
  Between gestures, paging restores the visible stable row ID and pixel offset.
  Storage warnings overlay rather than resize the list.
- One background queue performs disk reads. Live events coalesce at a minimum
  100 ms interval. A hidden viewer does not refresh. Reopening/resuming requests
  fresh data without replaying a large startup tail or matching line contents.
- Successful empty reads and failed reads settle instead of continually
  rescheduling an empty/error page. A new event or explicit reopen retries;
  stop/start catches up even when events arrived while the listener was absent.
- The current `terminal.log` and closed, numbered segments retain approximately
  4 MiB per generation, with a 64 MiB steady-state capacity cap. Whole oldest
  generations are removed only when necessary. There is no age-based expiry,
  per-row front rewrite, or guaranteed number of retained hours/days.
- Existing oversized files receive a one-time bounded migration. Expired/corrupt
  cursors, failed migration and incomplete migration state are explicit errors,
  not silent redirection to unrelated data.
- Storage faults remain visible. Unsaved live rows have a separate bounded
  256-row / 256-Ki-character buffer with explicit overflow counts. Failed writes
  cannot be recovered from disk after restart. Recovery is reported without
  duplicating the successfully persisted recovery row.

The 8,000-character line bound remains. Bounds do not guarantee unlimited input
rate, disk latency, or zero cost for a maximum-length wrapped row. No new database
or dependency was added.

## Verified results

On 2026-11-09, Pixel 9 / API 37, font scale 0.85 and density override 356:

| Saved rows | Initial loaded rows | Attached row views | Median page read (11 warm reads) |
| ---: | ---: | ---: | ---: |
| 2,000 | 256 | 57 | 5.449 ms |
| 200,000 | 256 | 57 | 4.927 ms |

The reconstructed old 3,000-row TextView needed 3,871 ms on the UI thread for
500 appends/front trims. The new 500-event persisted burst used three adapter
refreshes in 566 ms. These are different rendering/storage workloads, not a
universal speedup ratio. Fixed page/window/view bounds are the regression gate.

- JVM **358 tests / 61 suites**, zero failures/errors/skips; Lint zero issues;
  both APK builds and the generated Shell contract check passed.
- Native history runner: **13 cases**. Includes automatic backward/forward
  paging, row/pixel anchoring during live arrival, held-tail and fling protection,
  burst coalescing, hidden inactivity, stopped-listener catch-up, empty/error
  inactivity, storage-failure live delivery and actual MainActivity swipes.
- Existing real-log inode/prefix/non-truncation assertions passed. Synthetic
  stores use their own cache directories. Ordinary Service diagnostics may
  append to the real log, but it is never replaced or cleared.
- Widget latest-only acceptance and Service/Shell regressions are recorded in
  [widget QA](qa-widgets.md). Core runtime quality changes and coverage limits
  are recorded in [runtime QA](qa-android-runtime.md).

Final build-local evidence is under `agent/build/qa/latest-widget/` (`app.txt`,
`app.png`, build checks and regressions). Earlier `log-history/` and `log-scroll/`
results are historical and include superseded button/index implementations.

## Native reproduction

```sh
./gradlew --offline :agent:testDebugUnitTest :agent:lintDebug \
  :agent:assembleDebug :agent:assembleDebugAndroidTest
adb -s <approved-serial> install -r -t agent/build/outputs/apk/debug/agent-debug.apk
adb -s <approved-serial> install -r -t \
  agent/build/outputs/apk/androidTest/debug/agent-debug-androidTest.apk
adb -s <approved-serial> shell am instrument -w -r \
  io.dropcheck.agent.test/io.dropcheck.agent.LogHistoryQaInstrumentation
```

The runner attaches the production viewer to a Dialog and checks that file
resolution is not on the UI thread. Synthetic drag releases include stationary
MOVE samples so a natural fling is not mistaken for an anchor jump; fling
deferral is tested separately. Collect images before deleting test cache files.
No network mutation is performed. All OEM/locale/font-scale/Doze conditions,
TalkBack, clipboard export and power-loss migration recovery remain outside
this acceptance scope.
