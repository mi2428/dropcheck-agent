# Unfiltered command recall

Date: 2026-11-04. The user reported that PC Up/Down and Ctrl-P did not recall `show wifi status` and explicitly requested all-command recall on both PC and Android.

## Cause and design correction

The history policy conflated two unrelated boundaries: preserving editable user input and sanitizing externally visible results. `HistorySafe`/`historySafe` explicitly rejected ordinary Wi-Fi commands, along with other command families. The previous positive PC PTY fixtures seeded only `show version`, so they did not detect that common Wi-Fi input was unavailable for recall. A new real-readline test failed before the fix; the equivalent Android session-model test also failed for `show wifi status`.

The correction deletes the eligibility functions rather than widening their allowlists. PC uses readline's automatic in-memory history instead of disabling it and manually deciding what to save; Android records every nonblank submitted line without parsing it. Both preserve drafts, including incomplete and credential-bearing input. Entry bounds remain PC 1000 / Android 100, with adjacent duplicate consolidation and no disk history. Android rotation retains the current session, not a process-persistent command history.

Recall is separate from transcript/log/result redaction. Existing redaction remains enabled; recalled input is intentionally unredacted and may contain credentials. Clearing an active default credential does not erase its previously submitted command. Selecting history alone never dispatches anything: Enter/Go still performs normal parsing, target validation and execution. Old tests that required exclusions were replaced with positive all-command, draft and replay assertions; their former policy is no longer a requirement.

## Regression checks

- PC: **48 real-readline PTY cases**, compiled with the race detector. Fourteen command families are replayed using both Up and Ctrl-P, including status/scan/full/MLO, Network selectors, ADB, pipes/JSON, connect/disconnect, a quoted URL, monitor and test. Additional cases cover Up/Down and Ctrl-P/Ctrl-N through multiple entries, credential-command replay, invalid/unfinished-command replay, edited drafts, reverse search and cancellation. Assertions verify exact submitted input and execution counts, not just the presence of key bindings.
- Device commands use a disconnected in-memory Controller server and must fail before dispatch; their failure does not prevent history replay. The cancellation fixture uses an existing fake local process. Installed ADB, real agents, device radio operations and external probes are not used.
- Android: session tests cover all command classes without eligibility checks, unchanged draft restoration, multiple-entry round trips, the 100-entry bound and fresh-session isolation. Native key fixtures now expect status/scan/credential commands to be available through arrows and Ctrl-P/Ctrl-N; the rotation fixture still checks redacted transcript output separately from unfiltered input recall.
- Final gates passed: all Go packages and 48 PTY cases; race checks for app/shell/session/pipeline; Android **245 unit tests / 46 suites, zero failures/errors/skips**; Android lint with no issues; Controller, application APK and instrumentation APK builds; binding drift, formatting, modernize, vet, staticcheck and whitespace checks. Go used 1.26.8 with `GOFLAGS=-p=1`.
- Android native key/IME/rotation instrumentation was built, not installed or executed; real-device UI behavior remains unverified. This fix did not perform a commit, push or live device operation.
