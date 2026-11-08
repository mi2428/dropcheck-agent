# Android runtime implementation QA

## Priorities

- **Now, delivered:** serialize gRPC writes with completion; move main-thread
  Wi-Fi snapshot waits off the UI thread; reject invalid session setup before
  starting observers and stop an otherwise unowned Service. Keep command
  grammar, measurement policy, observation values and authentication unchanged.
- **Next:** extend device coverage to API 31/32, permission-denied observations
  and other OEM/background conditions when dedicated test devices are available.
  No speculative runtime framework or measurement rewrite is warranted.
- **Later:** if revisiting process-wide Network binding, retain both the primary
  operation exception and a simultaneous restore failure. Current
  `withNetworkBinding` fails visibly on restoration failure but may replace the
  primary exception. Do not hide failed restoration with a warning/fallback.

## Confirmed boundaries and small fixes

`GrpcSessionClient` serialized `onNext` but completed the observer outside that
lock. Cancellation during shutdown can still produce a command result. A small
Android-independent `StreamWriteGuard` now serializes writes and completion and
prevents writes after terminal completion. Existing send-failure warnings and
session termination notification remain. A fake observer/latch test checks an
in-flight write versus completion, ordering, one-time completion and late writes.

`WifiEventLogger.capture` was called synchronously from Service startup and
main-thread broadcasts. `networkWifiInfo` may await a location-inclusive callback
for up to one second; even a granted caller can receive redacted on-demand
capabilities. The shared capture entry now dispatches main-thread calls to its
existing executor, retaining inline callback/poll behavior. Submission and
shutdown share a lock. Native instrumentation confirms a successful snapshot
from a main-thread request is delivered off-main, not just an error log.

`AgentService` started Wi-Fi observers before validating a session's port/token.
Invalid requests now start no observers and stop the Service if neither a live
session nor a Clock owns it. They do not cancel an existing session or Clock.
The native same-UID check verifies invalid requests do not start observers;
the no-Clock stop branch was source-reviewed, not tested by removing a user Clock.

An initial audit claim that the default network callback runs on main was
rejected: Android's [ConnectivityManager implementation](https://android.googlesource.com/platform/packages/modules/Connectivity/+/refs/heads/main/framework/src/android/net/ConnectivityManager.java)
uses `ConnectivityThread.getInstanceLooper()` for its default internal Handler.
That path was deliberately not changed.

## Maintainability and areas left alone

The audit covered the previously untouched measurement engine and policies,
native child-process ownership, gRPC session lifecycle, Wi-Fi observation and
HE/EHT/MLO/security-element decoding, event logging, Device Admin marker and
the existing connection-icon, screen-dimming and background-location UI paths.
Audit baseline was `develop@21116b5` plus the explicitly identified current
working-tree integration; it was not a PR review or an audit of the PC runtime.

`NetworkCheckExecutor`, `NetworkCheckPolicy`, `NativeProbeProcess`, raw Wi-Fi
mapping/decoding and Device Admin did not yield a high-value reason to rewrite
them. Existing tests cover process output limits/reaping/interruption, DNS
cancellation, HTTP cleanup, Network identity association, measurement unknowns,
and representative IE layouts. Missing new code is not missing functionality,
and absence of a new test is not proof of a defect.

## Verification and coverage limits

Final JVM: **358 tests / 61 suites**, no failures/errors/skips. Debug Lint zero;
both APKs and `make check-shell` passed. Pixel 9 / API 37 device checks passed:
off-main snapshot, same-UID start/stop/invalid request, three separate-UID DUMP
denials, two real passive Controller gRPC sessions, Shell input, 24 presentation
cases, 11 Activity widths, owner/recreation lifecycle, and nine read-only cases.
The read-only dual-family check correctly reported absent IPv6 default-route/DNS
findings; this was not an internet-connectivity PASS. No external probe or
connection mutation was performed.

Lifecycle/read-only fixtures initially ran behind the lock screen or sampled
before a queued pre-draw position was applied. They now wait for an actually
visible/focused window and settled layout/scroll work. Assertions were retained;
production Shell behavior was not changed to hide those fixture failures.

Build-local evidence is in `agent/build/qa/latest-widget/`. The native core
fixture's `core-runtime-qa-*` cache store is intentionally removed only after
instrumentation process teardown, since an in-flight callback may outlive
`stop()`. Restore user widgets from the real log context and the normal Clock
Service lifetime after instrumentation. Do not clear app data or alter grants.

This is static inspection plus focused regression evidence, not exhaustive
decoder fuzzing, sustained transport-race/load testing, OEM/Doze certification,
or a fresh live test of every ping/HTTP/DNS/PMTU endpoint and Wi-Fi mutation.
Those require separately approved destinations and dedicated device conditions.
