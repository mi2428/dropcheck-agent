# Widget maintenance QA

## Latest-log widget contract

The widget and app share the saved terminal log, not a rendering strategy.
The widget is a latest-only, colored TextView. It does not provide manual
scrollback or try to infer the launcher's scroll/touch state. Tapping it opens
the app's full saved-history viewer.

- Read at most 80 recent physical rows through the bounded history page API.
  Keep at most 32,768 UTF-16 display characters, including inserted wrapping
  opportunities and separators. Older widget rows are omitted, not deleted
  from saved history. One snapshot is shared by all placed instances.
- New events coalesce on `WidgetUpdateQueue`: 500 ms initial debounce and at
  least five seconds after a completed render before another render. Requests
  arriving during rendering are retained. No new request means no periodic
  log read. This is near-real-time, not a five-second background-delivery SLA.
- The newest text remains at the bottom after append, wrapping, and resize.
  Empty input uses the placeholder. Storage faults are visible; a successful
  read clears the read fault. A write-failure warning is appended where the
  latest-only widget can display it.
- Display truncation does not split a supplementary Unicode code point.
  These are display bounds, not a new retention policy or a second log store.

The unreleased RemoteViewsService/sparse-index experiment was removed completely,
including its manifest service, row layout, indexing API and tests. On the tested
Android 37 build, the framework converted that service to an eager collection:
only 581 of 1,746 indexed rows arrived, and subsequent latest rows were omitted.
The new widget does not send a history collection or depend on that conversion.

## Clock and shared lifecycle

`WidgetUpdateQueue` serializes rendering, retains an in-flight arrival and applies
the provider's debounce/minimum interval. Final removal cancels queued work;
later installation can request work again.

The Clock provider owns its non-wakeup, inexact one-minute alarm, connectivity
PendingIntent, Wi-Fi broadcasts and bounded event follow-ups. Duplicate Service
observation and ten-second polling were removed. The Service retains the
foreground lifetime needed for Clock location access. `WIDGET_START` and
`WIDGET_STOP` preserve an active controller session, and session completion uses
the latest start ID when stopping. Native XML auto-sizing fixes small-size time
clipping without replacing TextClock's time/date updates.

## Verification

On 2026-11-09, Pixel 9 / API 37, density override 356 and font scale 0.85:

- JVM: **358 tests / 61 suites**, zero failures/errors/skips. Debug Lint: zero
  issues. Both APK builds and `make check-shell` passed.
- Widget native runner: **14 cases**, including a 200,000-row isolated history,
  burst append, maximum text payload, wrapped final row, resize, empty input,
  storage failure/recovery, tap routing, and Clock fields at 180x96 and 320x180 dp.
- An actual temporary AppWidgetHost registration verified AppWidgetManager
  delivery, two new batches of ordinary Service diagnostics, the five-second
  interval, latest-row visibility, and tap routing. Largest measured complete
  RemoteViews parcel: **65,448 bytes**, below the runner's 128 KiB gate.
- The user's existing Clock and Log widget IDs were preserved. `lifecycle=log`
  creates and removes only a test-owned Log widget. The older clean-device Clock
  removal/held-loopback-session tests passed on 2026-11-08; they were not rerun by
  removing the user's currently placed Clock.
- Same-UID start/stop and invalid-session observer checks, three separate-UID
  DUMP denials, and two passive Controller gRPC sessions passed. Shell input,
  presentation, Activity lifecycle/recreation and read-only regressions passed.

Native screenshots were inspected. Build-local evidence is under
`agent/build/qa/latest-widget/`; earlier `widgets/` and `log-scroll/` evidence
describes earlier implementations, not final latest-only acceptance.

## Safe reproduction and cleanup

```sh
./gradlew --offline :agent:testDebugUnitTest :agent:lintDebug \
  :agent:assembleDebug :agent:assembleDebugAndroidTest
adb -s <approved-serial> install -r -t agent/build/outputs/apk/debug/agent-debug.apk
adb -s <approved-serial> install -r -t \
  agent/build/outputs/apk/androidTest/debug/agent-debug-androidTest.apk
adb -s <approved-serial> shell am instrument -w -r -e capture true \
  -e lifecycle log \
  io.dropcheck.agent.test/io.dropcheck.agent.WidgetQaInstrumentation
```

Synthetic logs use cache contexts, never replacement of the real saved log.
The temporary host adopts only BIND_APPWIDGET and releases its IDs, host and
identity in `finally`. Isolated contexts can temporarily update placed widgets;
restore them from the real context after testing. Collect native images before
removing test-owned cache files. Instrumentation tears down its process, so
restore the normal WIDGET_START lifetime when a user Clock remains placed.
`lifecycle=true` additionally requires no existing Clock or agent Service and
must not be used to remove a user's widget.

No external probes, fresh scans, connection changes, permission changes or global
display/IME changes were performed. OEM launchers, Doze/background scheduling,
all locales/font scales, actual TalkBack operation and clipboard export are not
certified by these checks.
