# Junos-inspired interactive Shell redesign

Status: **implemented interactive cutover**, 2026-11-03, from baseline `develop` at `ebe9dbc`. The user authorized implementation after reviewing the hierarchy/filter design and then required new adversarial QA. This document retains the design rationale and complete old-to-new mapping; [commands.md](commands.md) is the current operator/maintenance contract and [qa-junos.md](qa-junos.md) records evidence and untested limits. Device operations, commits and push were not authorized or performed.

**Approved follow-up (2026-11-04):** `show wifi status` is a quick connection/radio/IP/MLO overview with substantially more captured detail than its initial 50–80-line summary, allowing roughly four times that output where useful. Hierarchical indentation, aligned field/value columns and hanging continuations make the expanded view readable without arbitrary truncation. It reuses one `GetWifiStatus` RPC and never waits for PC ADB supplements, full device capability collection, every AP's details or all-Network diagnostics. Named children select detailed subjects; `eht` renders focused EHT/MLO evidence and `full` retains the complete passive bundle including safe raw references and IE dumps. This supersedes the full-by-default statements below, not detailed information access, source/error visibility or privacy requirements. Independent CLI/TUI behavior remains unchanged; see [the current contract](commands.md#acquisition-targets-and-outcomes). The subsequent user authorization covers QA, fixes and local commits; device operations and push remain outside scope. [Follow-up QA](qa-status-followup.md) records the checks and limitations.

## Scope and established requirements

- Redesign the PC and Android **interactive Shells**, including help, completion, history, pipelines and presentation. Preserve common command meaning across both; identify real PC-only capabilities.
- Default `show` output contains all safely obtainable information for its subject. A suffix limits the subject; it does not unlock previously hidden information. In particular, prefer `show wifi status [eht]` and `show adb [wifi|connectivity]` over repeated diagnostic commands.
- Keep passive observation separate from fresh Wi-Fi scans, traffic-generating probes and immediate Wi-Fi changes. Do not expose acquisition tools such as `cmd` and `dumpsys` as competing user commands.
- Preserve target pinning, typed outcomes, permission/Device Owner checks, bounded execution, credential protection and complete safe-value access. Do not substitute another target or cached success after failure.
- The eventual interactive cutover replaces obsolete paths, aliases and dead code, rather than shipping an old/new compatibility mode. This does **not** authorize breaking the independently supported one-shot CLI or RPC contract.
- Implementation changes the two interactive Shells and their shared safety boundaries/fixtures. The independently supported PC one-shot CLI, Controller TUI entry points, harness interface and device configuration remain outside the redesign. Their test fixtures now provide consistent typed success evidence rather than bypassing shared validators.

## Implemented command hierarchy

```text
show
  version                              local Shell application version
  cli                                  local session state; never secret values
  permissions                          Android permissions and Device Owner status
  network                              all Android Networks and their safe properties
    default                            captured Android platform default
    id <network-id>                     one explicit Connectivity Network
  wifi
    status                             full passive Wi-Fi diagnostic bundle
      connection                       only connection/radio observations
      ip                               only selected physical Wi-Fi IP observations
      capabilities                     only device Wi-Fi capabilities
      eht                              only EHT/MLO evidence and its prerequisites
    scan                               full cached AP observations; never starts a scan
  test
    profiles                           test catalogue, prerequisites and traffic cost
    last                               full last test attempt; explicitly historical
  devices                              PC only: connected agents and selection state
  adb                                  PC only: full bounded diagnostic collection
    wifi                               only the Wi-Fi collection
    connectivity                       only the Connectivity collection
      networks                         only Network records
      requests                         only Network request records
      diagnostics                      only Connectivity diagnostic records
      traffic                          only traffic-controller records

request
  wifi
    connect <ssid>                      immediate connection, explicit/session credential
    disconnect                         immediate disconnection
    reconnect                          immediate reconnection
    forget ssid <ssid>                  forget exact saved SSID matches
    forget config-id <id>               forget exactly one saved Wi-Fi configuration
    cycle <ssid>                        bounded connection cycles; optional explicit probes
    scan                               one active scan, returning its full AP observations
      eht                              one active scan plus an EHT/MLO result
  ping <host>
  traceroute <host>
  path-mtu <host>
  global-ip
  dns <name>
  http <url>
  download <url>

test
  wifi                                 evaluate typed Wi-Fi expectations
  link                                 evaluate existing-link provisioning; no probes
  lab                                  run explicitly configured lab probes and assertions

monitor
  wifi                                 finite event window, not an endless stream

set cli default-passphrase <secret>     current Shell memory only
clear cli default-passphrase            clear the current session credential
help [<command-path>]                   local help only
exit                                   PC only: leave the Shell
```

Nodes with both an explanation and children are executable: `show network`, `show wifi status`, `show adb` and `show adb connectivity` do not require another keyword. Root/group-only input such as `request` or `test` shows local contextual help and performs zero device operations. `?` is a help interaction, not another executable alias.

`show` reads, `request` initiates an operation, `test` declares expectations or a test plan, and `monitor` observes over time. These are operator intentions, **not permission classes**: a `request` can fail an assertion, `test lab` sends traffic, and `request wifi scan` scans without changing saved Wi-Fi configurations. The inventory must retain separate effect/permission metadata.

### Arguments and projections

Keywords are lowercase and may use unambiguous prefixes; values remain exact literals unless their specific validator says otherwise. Options use `key value`, not shell flags. Values may be quoted or escaped. The optional subject projection immediately follows its command; subsequent key/value options can appear in any order. At most one projection is accepted.

```text
show network [default | id <network-id>]
show wifi status [connection | ip | capabilities | eht]
show wifi status ip [ssid <ssid>] [bssid <bssid>] [network-id <network-id>]
show wifi status eht [ssid <ssid> | bssid <bssid>]
show wifi scan [ssid <ssid> | bssid <bssid>] [band <band>] [mlo] [brief]

request wifi scan [eht] [ssid <ssid> | bssid <bssid>] [band <band>] [timeout <ms>]
request wifi scan [ssid <ssid> | bssid <bssid>] [band <band>] [timeout <ms>] [mlo] [brief]
show adb [wifi | connectivity [networks | requests | diagnostics | traffic]] [timeout <ms>]

test wifi [ssid <ssid>] [bssid <bssid>] [security <security>] [band <band>]
          [require-ip <true|false>] [require-validated <true|false>] [timeout <ms>]
test link ssid <ssid> [family <ipv4|ipv6|all>]
test lab ssid <ssid> family <ipv4|ipv6> gateway <ip> dns <name> resolver selected
         http <url> expected-status <100..599> timeout <1..30000>
monitor wifi [duration <ms>] [interval <ms>]
```

The two `request wifi scan` forms describe one command: default scan output permits `mlo`/`brief`; its `eht` projection does not. Line breaks above are for documentation, not continuation syntax. `band` defaults to `all`. SSID/BSSID selection on scan commands filters returned AP evidence; it does not direct the radio to scan only that SSID or BSSID.

- `ip` selects a **physical Wi-Fi Network**, retaining the current selector intersection and fail-closed rules. It is not another name for Android's platform default. `eht` SSID/BSSID options filter the evidence view, including matching connection/AP/link records; they do not reconnect or select a probe Network. Preserve affiliated-link BSSID matching.
- Scan SSID and BSSID selectors are explicit and mutually exclusive. A BSSID-shaped SSID is still an SSID when named by `ssid`; remove the current scan-detail target guessing.
- Keep the existing probe options and Network selectors unchanged after their new `request` prefix: ping `count/size/family/timeout`; traceroute `max-hops/via/size/family/timeout`; path MTU `min-mtu/max-mtu/family/timeout`; global IP `family/timeout`; DNS `record/timeout`; HTTP `expected-status/timeout`; download `timeout`. Each also retains `ssid/bssid/network-id`. Only `via` is repeatable, with typed required-hop evaluation retained.
- Retain current value sets and input/operation bounds unless a change is explicitly listed here. In particular, ordinary probe `family` is `ipv4|ipv6|auto`, global IP is `ipv4|ipv6|all`, DNS `record` is `A|AAAA|ALL`, and link defaults to IPv4. `auto` is not dual-family success. Time options remain integer milliseconds; do not silently convert them to Junos-style seconds.
- Retain connection `security/bssid/band/mac-randomization/timeout`. Retain cycle `passphrase/security/bssid/band/mac-randomization/timeout/count/ping/http/forget-after-each/pause`; cycle still requires its explicit credential, with no new session-default or environment-variable behavior.
- Remove the existing `detail` and `fresh` interactive modifiers; do not introduce `extensive`. No fact requires a verbosity switch. Keep scan `brief` as an **explicitly lossy summary** and `mlo` as an explicit AP-subject restriction; remove the artificial requirement that `mlo` needs `brief`. Do not invent new summary modes for other commands during this cutover.

## Default output and acquisition

### Wi-Fi status

`show wifi status` requests the union of safe observations currently reachable through status, diagnostics, capabilities, scan-cache and EHT presentations. Organize it into connection/radio, physical Wi-Fi IP/routes/DNS, capabilities, cached APs, EHT/MLO and contextual Android Network/IP sections. Include existing permission/availability evidence. It must not require a second `detail` or `eht` invocation to reveal fields.

The existing `NetworkRepository.wifiDiagnostics()` is a reusable acquisition starting point, **not proof of complete field presentation**. It collects status, capabilities, scan cache and all Android Networks independently. Preserve source/time distinctions; do not call the bundle atomic, merge different observations because values agree, or repeat the same fact within one subject/observation. A rendered index/reference is not a second measurement. EHT interpretations may reference already displayed facts without printing the entire connection again.

Suffixes request a smaller subject, not deeper output. For example, `show wifi status eht` retains all safe EHT/MLO evidence, availability, device support, current/AP/link relations and relevant Network observations. `show wifi status capabilities` does not need to obtain AP data. Reuse existing RPC payloads first; do not add a universal query protocol merely to avoid a cheap passive read.

PC status preserves the existing IPv6 RA/IP and EHT/MLO **host supplements** where applicable, including them in the full command and in the corresponding `ip`/`eht` projection. They remain separately labelled PC/ADB evidence, with controller receipt times rather than invented Android observation times. Android does not pretend to have those host sources. Missing prerequisites and unsupported supplements are visible availability states; attempted acquisition errors must not disappear as an empty section. Optional host evidence does not redefine the Android operation's typed success. Explicit `show adb` acquisition failures do make that ADB request non-successful.

The current short supplement commands, bounds and typed/public-data restrictions are a separate scope from the raw `show adb` collection below; the redesign does not authorize expanding them, arbitrary shell execution, or elevating privileges. Avoid acquiring an identical source twice within one invocation when its captured result can actually be reused. Do not cache it across invocations as if it were current.

### Cache versus fresh scan

- `show wifi scan` and the scan section of `show wifi status` only read existing Android scan results. Display source, result age and availability. Reading the cache is not confirmation of freshness.
- `request wifi scan` starts one bounded active scan and returns its results in the **same command**. It is not an acknowledgement requiring another `show`. Display the refresh attempt, callback/update evidence and per-result age; a successful request does not prove every cached AP entry was freshly observed.
- `request wifi scan eht` performs that scan once, then obtains the passive prerequisites for the EHT view. Use the exact returned scan payload, not a later unrelated cache read, for its AP evidence. Preserve independently observed connection/capability/Network times.
- Throttling, scan-start failure or timeout remains non-successful with a stated reason. Any already returned cache is labelled reference data, never substituted as successful fresh evidence. Cancellation stops subsequent collection; do not retry, change target or continue collecting after cancellation.
- Default fresh-scan timeout remains 10,000 ms and bounds the refresh wait, not the entire EHT composite. Passive follow-up retains its own bounded execution and shared cancellation. Keep existing permission checks; do not require Device Owner merely because the command moved under `request`.

### Network and local subjects

`show network` replaces the plural list and uses the existing lightweight Network operation: safe properties for every Android Network, without Wi-Fi diagnostics or scan acquisition. `default` uses the captured platform default, which can be VPN/cellular; `id` uses exactly that canonical positive decimal Network ID. A valid empty all-Network snapshot differs from a missing requested Network. Preserve response ID/count/uniqueness/time/availability/error consistency checks and reject stale or contradictory success payloads.

`show permissions` remains observational, not a permission-grant command. `show cli` is new but entirely local: session owner (`self` or Controller), pinned target/broadcast mode, detected layout state, session-only history policy and default credential **present/unset**. It neither exposes the credential nor queries the device. `show version` retains the local application-version scope and labels its origin. `show devices` remains PC-only.

### ADB collection and cost

At the baseline, 17 ADB command paths selected eight kinds. The independent `KindFull` still runs eight commands but omits the separately available Connectivity `networks` and `requests`. Therefore, mapping the new interactive parent to `KindFull` would **not** satisfy full-by-default.

Define the interactive collection explicitly as the union of the current full bundle and individual kinds: **ten unique invocations**, each run once per target. Keep raw source labels even when their output overlaps. Do not deduplicate text from different captures or trust platform-dependent parsing to replace the original safe evidence.

| Group | Existing allowlisted acquisition, after `adb -s <serial> shell` |
| --- | --- |
| Wi-Fi | `cmd wifi status` |
| Wi-Fi | `dumpsys wifi` |
| Wi-Fi | `dumpsys wifi ipclient` |
| Wi-Fi | `dumpsys wifi WifiScoreReport` |
| Wi-Fi | `dumpsys wifi WifiScoreCard` |
| Connectivity | `dumpsys connectivity` |
| Connectivity | `dumpsys connectivity networks` |
| Connectivity | `dumpsys connectivity requests` |
| Connectivity | `dumpsys connectivity --diag` |
| Connectivity | `dumpsys connectivity trafficcontroller` |

`show adb` requests all ten; `wifi` and `connectivity` request their respective five. A Connectivity suffix requests exactly its one source. `cmd`/`dumpsys`, the original command, target serial, capture times, exit status and elapsed time belong in the result, not alternate command paths. No generic `adb shell`, arbitrary service dump, fresh scan, privilege escalation or credential-bearing dump is added.

**Interactive budget:** `timeout` is a whole-collection budget, default 90,000 ms, accepted range 1..900,000 ms. The existing 90,000 ms per-process ceiling is capped by the collection deadline; sources run sequentially per target. This adopted policy bounds the default full command at 90 seconds per target rather than ten times that; an operator can deliberately increase the budget. The independent one-shot bundle policy is unchanged.

Display progress by source and retain completed safe sections if another source fails. Continue after an individual source error only while the deadline and cancellation state allow the remaining **independent requested sources**; do not retry or mask the error. On deadline/cancellation mark remaining sources not collected. Show section-level failure/timeout/not-collected states and an overall non-success for incomplete requested acquisition. Availability is Android-version dependent; do not silently omit a rejected source or claim every device supports all ten.

## Tests, monitoring and session state

### Assertions and wait

Unify `wifi assert` and `wifi wait connected` as `test wifi`. They already share expectation evaluation; the meaningful difference is the default timeout:

- Omitted `timeout`: immediate evaluation, retaining the current assert default of 0 ms.
- Positive explicit `timeout`: bounded wait for the same conditions. The old no-option wait becomes `test wifi timeout 30000`.
- Keep all existing expectation defaults and conjunctive matching. `require-ip` and `require-validated` remain explicit booleans; a wait does not connect, scan, or send a probe. The existing positive-integer input rule is retained: omit the option for immediate evaluation rather than accepting a new `timeout 0` syntax.

`test link` retains the existing two physical-Wi-Fi identity/IP observations and no active probes. `family all` requires both IPv4 and IPv6 address/default-route/DNS provisioning, with separate findings. Android validated status remains an observation, not a substitute for those requirements. It does not certify uninterrupted association.

`test lab` retains explicitly supplied, authorized gateway/DNS/HTTP destinations and current URL/family/timeout restrictions. It can perform two identity reads, three gateway ping packets, one selected-resolver DNS query and one HTTP request, stopping on failed prerequisites. HTTP hostname family is not claimed to be pinned merely because the test has a family parameter. No implicit public endpoint, connection setup or permission grant is introduced. Strict BSSID pinning remains unsupported for link/lab; reject it before traffic rather than silently weakening the condition.

`show test profiles` retains the supported/configured/unsupported catalogue and its reasons/costs. `internet` and `eht` remain explicitly unsupported catalogue entries, not executable placeholders or new test implementations. `test` alone does not choose a default. `show test last` includes all retained safe evidence for the last `test wifi`, `test link` or `test lab` attempt, including a bounded credential-free preflight rejection of a recognized test. Wi-Fi assertion attempts are now included; no stale PASS may stand in for a later rejected attempt. Reports are historical, session-local and never authorization or current readiness.

`monitor wifi` preserves the existing finite 10,000 ms window and 1,000 ms interval defaults, event/time provenance and bounds. Do not label it an endless live stream or add unrequested background event retention.

### One connection entry and one credential rule

Replace both `use` and `wifi connect` with `request wifi connect <ssid>`. Explicit `passphrase <secret>` takes priority over an unset option; PC may instead use its existing `passphrase-env <name>` extension. Supplying both is invalid. When neither is supplied, a valid session default is required. An explicitly supplied invalid, empty or unresolved credential fails; it never falls back to the default. No default is sent until the target and complete request have passed validation.

`set cli default-passphrase` and `clear cli default-passphrase` operate immediately on current-session memory only, not candidate/device configuration. Preserve current UTF-8/control-character/PSK validation and secret-safe transcript echo, help, errors, copy and logs. Input recall intentionally includes credentials under the revised policy below. Android rotation may retain the active session; process death does not persist it. Do not reintroduce the removed plaintext preference. Preserve its existing deletion-only cleanup until obsolete persisted data is known to be absent; it is not a legacy credential-reading path.

Connection waits keep 45,000 ms by default; reconnect keeps 30,000 ms. Cycle retains its current explicit credential/probe choices, bounds and cleanup reporting (including the existing count/pause defaults and caps). Cancellation is not rollback: a completed disconnect/forget/connect can already have changed the device. Report completed actions and cleanup failures rather than promising atomic apply/discard. A failed connection attempt must not leave prior readiness or a historical successful test authorizing new work.

### Targets and destructive identifiers

- PC selection stays pinned to the resolved agent identity established at startup, not a label, transient list position or re-resolved prefix. Android targets self. Disconnect/reconnect of the transport must not select a different agent silently. There is no in-Shell target switching.
- Preserve deliberate startup `--target all`, visible prompt/target banners and per-agent result isolation. **Require one target for all Wi-Fi mutations and all `test` commands**, extending the baseline single-target `use`/profile rule to the unified connection and other mutations. This is a deliberate tightening. Passive reads, bounded monitor, explicit scan requests and standalone probe requests retain deliberate broadcast; no implicit broadcast is added. Freeze the target membership at dispatch; newly connected agents do not join a running batch.
- Identify the selected physical Wi-Fi Network/interface before probe dispatch, and retain current selector checks and Network binding cleanup. Request probes remain physical-Wi-Fi probes; general `show network default` must not silently change their routing semantics. SSID/BSSID checks at selection time do not guarantee uninterrupted identity for the full probe.
- Connectivity `network-id` and saved Wi-Fi `config-id` are different namespaces. `request wifi forget` requires **exactly one** explicit `ssid` or `config-id`. An SSID of `123` means the string SSID, never configuration 123. Parse config IDs canonically as non-negative platform configuration IDs; do not apply the Connectivity positive-ID rule blindly. Forget only the matched saved configuration set, report affected IDs, and retain current Device Owner/platform safeguards and already-absent reporting.
- The independent legacy `ForgetWifi.target` string interprets a numeric target as both ID and SSID. New Shell requests instead use the additive typed selector and `wifi.forget.typed` capability/validation. They fail if that capability is absent and never encode the selector back into the ambiguous string. The independent one-shot/RPC contract remains explicit, without a Shell compatibility fallback.

## Interaction, display, history and pipelines

### Help and editing

- Use one interactive inventory for canonical paths, projections, options, effects, descriptions and help/completion. Bare groups show immediate candidates; `help <command-path>` can describe any depth without running it. Mark executable parent nodes with an Enter candidate as well as narrower subjects.
- Retain no-Enter, cursor-aware `?`: empty prompt, partial word (`sho?`), next token (`show ?`), argument/value, and `| ?`. Help must not dispatch, perform device discovery, expose a supplied credential or discard the edit. Quoted/escaped `?` is data. Handle IME composition and Android touch help/Tab/Prev/Next as real input boundaries.
- Retain unambiguous keyword/option prefixes and Tab completion, with candidates on ambiguity. Never abbreviate SSIDs, IDs, URLs, secret values or arbitrary value literals. Resolve exact keywords before prefixes; never complete a secret. Retain cursor-tail replacement and literal spans instead of introducing a second tokenizer.
- Do not implement Junos complete-on-Space in this cutover. Space remains a value/word separator, avoiding new literal/IME behavior and an unnecessary session toggle. Do not add a pager, `screen-length`, `no-more`, configuration-mode prompt, shell escape or fictitious privilege level.
- Keep the selected target and operational `>` context visible; `#` must not imply Device Owner/root. PC `exit`/EOF leaves the Shell and clears session secrets; `quit` is removed as a redundant spelling. Android keeps its existing app navigation rather than accepting a fake process-exit command; navigation alone must not claim to end the retained session or stop the agent service.
- Preserve the existing cancellation controls. During long collection show the target, stage and bounded progress without secret input. Do not display a reusable prompt or start another operation until cancellation and required cleanup are settled; report a delayed cancellation honestly rather than implying Ctrl-C already reversed a device action.

### Safe output and recall

All-subject output is sectioned, lossless safe data, not a requirement to print credentials or duplicate raw/decoded facts. Unsupported, absent, false, stale, redacted, failed and not-observed stay distinct. Use actual PC columns/Android content pixels for reflow, not field selection. Retain current display/history resource bounds and full-safe-value Inspect/copy access. If Android cannot display the whole result inline, explicitly expose that the view is limited and how to open/copy the complete retained result; never claim every value is on screen. Failure to retain the full safe result is a visible failure, not silent truncation.

**Revised after the history regression report (2026-11-04):** recall is an input-editing facility, not an output-redaction boundary. Remove command eligibility policies entirely: retain every submitted nonblank command, including Wi-Fi diagnostics, mutations, probes/URLs, credentials, pipes and invalid input. Preserve drafts unchanged through Prev/Next and native search. Keep the existing session-only entry budgets, adjacent-duplicate handling and no disk history. Re-execution still requires explicit submission and normal target/validation checks; recording an input never dispatches it. Transcript/log/result redaction remains independent and must not rewrite or discard the recall buffer. The previous allowlist wrongly excluded ordinary repeated Wi-Fi commands and is superseded by [the history follow-up](qa-history.md).

### Pipes

**User-approved filter direction (2026-11-03), now implemented:** replace `begin` with `find`, retain `except` and `match`, and add `last` and `trim` on both Shells. This supersedes the earlier `begin` requirement. Retain `count` and exclusive `display json`, with no `begin` alias. The detailed numeric/Unicode rules below are the portable contract.

```text
| find <pattern>
| except <pattern>
| last <lines>
| match <pattern>
| trim <start-column>
| count
| display json
```

- `find` retains the first matching logical line and all following lines; no match produces empty output. Move the current `begin` implementation and its fixtures to this canonical spelling, without an old-name rewrite path.
- `match` retains matching logical lines; `except` retains nonmatching logical lines. Both keep the existing portable regular-expression semantics and input order.
- `last <lines>` retains at most the final requested number of logical lines from the preceding stage, in their original order. Count blank logical lines exactly as `count` does; empty input remains empty. It is a finite-result filter, not tail-follow or a new collection. Require an explicit positive decimal count in `1..2147483647`; do not invent a screen-length-dependent default or allocate a buffer of that requested size when fewer lines exist.
- `trim <start-column>` keeps each logical line from the specified **1-based starting position** onward: `trim 1` leaves it unchanged; `trim 11` removes its first ten positions. This follows the Juniper guide's starting-column example, not a conflicting interpretation of the short help as a removal count. Require an explicit positive decimal value in `1..2147483647`. A shorter line becomes an empty logical line rather than disappearing, so trim alone does not change the line count.
- For portable `trim`, define a logical position as one Unicode scalar value in the sanitized line, not a UTF-8 byte, UTF-16 code unit, terminal cell or Android pixel. Do not split a Unicode scalar or normalize the text. This deliberately makes PC/Android filtering independent of layout; it does not claim Junos-equivalent Unicode cell or grapheme behavior. For example, trimming `A😀B` from position 2 yields `😀B` on both platforms. Reuse standard string iteration rather than introducing a display-width dependency into filtering.
- Parse and validate the entire pipeline before dispatch. Preserve ordered stages, literal/quoted pipes, the portable regex subset, 512 UTF-8 byte patterns, at most 16 stages and blank-inclusive logical-line counting. See [the implemented pattern and escaping contract](commands.md#filters-and-structured-presentation).
- Sanitize control sequences and credentials before filtering, layout, copy, inspection and speech. Filters operate on complete logical lines before wrapping; width cannot change their matches or counts. No subprocess pipeline or arbitrary shell expansion is added.
- `display json` stays exclusive of text filters and retains the safe presentation envelope `{"ok":false,"lines":[...]}`, not raw protobuf/ADB JSON. Full logical lines are retained in JSON; it is not a truncated screen dump. Deliberate PC broadcast emits one complete envelope per agent as newline-delimited JSON, with target identity in its safe lines, not one invalid concatenated JSON document. Progress belongs on PC stderr or a separate Android UI block, never inside the JSON stream. One-shot `--format json` stays unchanged.
- Determine typed outcomes before text filtering. Zero visible lines, `count`, or a projection applied to an already failed result must not turn failure into success. Requested-subject plans may omit unrelated collection, but must not hide errors in sources they did request. Preserve PC stderr/Android unfilterable failure indicators and per-agent outcome isolation.
- Continue rejecting `display set`, `no-more`, pipe `request`, `save`, `tee` and other unimplemented Junos stages rather than adding decorative no-ops.

For example, `show wifi status | find EHT | except unavailable | last 20` applies those three transforms in order. `trim` also precedes later patterns/counts and final wrapping; it never reveals text removed by credential sanitization. Numeric argument omissions, zero/negative/overflow values, extra arguments and obsolete `begin` are rejected before any acquisition.

## Complete old-to-new mapping

These tables cover the **33 portable paths** in `shell/commands.json` and **18 PC host paths** in `hostGrammar` at the baseline. Unlisted options are retained under the rules above. A wider replacement result is intentional; this is not a byte-for-byte output compatibility promise.

### Portable paths

| Current path | Proposed replacement | Important change |
| --- | --- | --- |
| `show version` | `show version` | Keep local version scope. |
| `show networks` | `show network` | All-Network parent is executable; no scan. |
| `show network` | `show network default` or `show network id <id>` | Old target becomes an explicit subject; new bare parent lists all. |
| `show permissions` | `show permissions` | No privilege mutation. |
| `show wifi status` | `show wifi status` | Full safe bundle, not just the former overview. |
| `show wifi diagnostics` | `show wifi status` | Remove overlapping entry; preserve contextual Network evidence. |
| `show wifi capabilities` | `show wifi status capabilities` | Restrict the subject; all capability fields by default. |
| `show wifi scan` | `show wifi scan` or `request wifi scan` | Move `fresh` to the request; keep explicit `brief`/`mlo` with no coupling. |
| `show wifi scan detail` | `show wifi scan ssid <ssid>` or `show wifi scan bssid <bssid>` | Full records by default; explicit target type. |
| `show wifi eht` | `show wifi status eht` or `request wifi scan eht` | Fresh variant returns EHT evidence in one command. |
| `show ip status` | `show wifi status ip` | Preserve physical Wi-Fi selection and all existing selector keys. |
| `wifi connect` | `request wifi connect <ssid>` | Unify with `use`; allow explicit or session credential; one target. |
| `wifi disconnect` | `request wifi disconnect` | Immediate mutation; single-target restriction. |
| `wifi forget` | `request wifi forget ssid <ssid>` or `request wifi forget config-id <id>` | Typed namespace separation, not a cosmetic rename. |
| `wifi wait connected` | `test wifi timeout 30000` | Carry an explicitly supplied old timeout instead; no-options wait remains bounded. |
| `wifi assert` | `test wifi` | Immediate by default; positive timeout retains bounded polling. |
| `wifi monitor` | `monitor wifi` | Retain finite-window defaults, not background streaming. |
| `wifi reconnect` | `request wifi reconnect` | Retain timeout; single-target restriction. |
| `wifi cycle` | `request wifi cycle <ssid>` | Explicit credential and optional probe/cleanup choices retained; one target. |
| `ping` | `request ping <host>` | Keep selectors, bounds and typed result. |
| `traceroute` | `request traceroute <host>` | Keep typed `via` expectations. |
| `path-mtu` | `request path-mtu <host>` | MTU bounds remain IP-packet bytes. |
| `global-ip` | `request global-ip` | Active service lookup, not passive `show`. |
| `dns` | `request dns <name>` | Keep record choices and selected-Network resolution. |
| `http` | `request http <url>` | Preserve URL validation, expected status and timeouts. |
| `download` | `request download <url>` | Preserve traffic cost, URL validation and timeouts. |
| `help` | `help [<command-path>]` | Extend to arbitrary depth, not only a root topic. |
| `set default passphrase` | `set cli default-passphrase <secret>` | Session memory only. |
| `clear default passphrase` | `clear cli default-passphrase` | Immediate local clear. |
| `use` | `request wifi connect <ssid>` | No separate shorthand execution path. |
| `check` | `test link ...` or `test lab ...` | Bare `test` shows choices; no default/public fallback. |
| `show checks` | `show test profiles` | Preserve catalogue and unsupported reasons/costs. |
| `show check last` | `show test last` | Full historical report; no `detail` unlock; include Wi-Fi test attempts. |

### PC-only host paths

| Current path | Proposed replacement | Collection scope |
| --- | --- | --- |
| `show devices` | `show devices` | Local connected-agent/selection list. |
| `show adb cmd wifi status` | `show adb wifi` | Five Wi-Fi sources, including this source. |
| `show adb wifi status` | `show adb wifi` | Same grouped result, not another alias. |
| `show adb wifi dumpsys` | `show adb wifi` | Same grouped result. |
| `show adb dumpsys wifi` | `show adb wifi` | Same grouped result. |
| `show adb connectivity` | `show adb connectivity` | Now all five Connectivity sources. |
| `show adb connectivity dumpsys` | `show adb connectivity` | Full Connectivity group, source labels retained. |
| `show adb connectivity networks` | `show adb connectivity networks` | Only the corresponding source. |
| `show adb connectivity requests` | `show adb connectivity requests` | Only the corresponding source. |
| `show adb connectivity diagnostics` | `show adb connectivity diagnostics` | Only the corresponding source. |
| `show adb connectivity trafficcontroller` | `show adb connectivity traffic` | Source tool name remains in output. |
| `show adb connectivity --diag` | `show adb connectivity diagnostics` | No interactive acquisition flag alias. |
| `show adb dumpsys connectivity` | `show adb connectivity` | Full Connectivity group. |
| `show adb dumpsys connectivity networks` | `show adb connectivity networks` | Only the corresponding source. |
| `show adb dumpsys connectivity requests` | `show adb connectivity requests` | Only the corresponding source. |
| `show adb dumpsys connectivity diagnostics` | `show adb connectivity diagnostics` | Only the corresponding source. |
| `show adb dumpsys connectivity trafficcontroller` | `show adb connectivity traffic` | Only the corresponding source. |
| `show adb diagnostics full` | `show adb` | Ten-source union, not the incomplete eight-source bundle. |

### Aliases and local behavior outside the inventory

| Existing input | Proposal |
| --- | --- |
| `p`, `tr`, `pm`, `gip` | Remove explicit root aliases; use `request` with context-specific unique prefixes. |
| `h` | Remove the alias; unique prefix resolution still permits it only while `help` is the unique matching root. |
| `?` | Keep no-Enter contextual help and bare-help behavior, not an alias that can consume command arguments. |
| `exit`, `quit` | Keep PC `exit`/EOF; remove redundant `quit`. Do not advertise either as Android process control. |
| `detail` on status/diagnostics/capabilities/IP/EHT/last report | Remove; the relevant safe evidence becomes default output. |
| `fresh` on scan/EHT | Move to `request wifi scan [eht]`, including any explicit timeout. |
| `begin` | Replace with user-approved `find`; remove `begin` from interactive parsing, help and completion with no alias. |

An old spelling that happens to be a valid unambiguous prefix of the new grammar is not a maintained alias. Do not add special rewrite rules to keep it alive. New ambiguous prefixes must report candidates rather than choosing a command by old habit.

## Implementation boundary and acceptance

The implementation followed this direct-cutover sequence. Acceptance evidence and remaining live limits are recorded separately in [qa-junos.md](qa-junos.md):

1. **Separate surfaces before renaming.** Today both `GrammarTable` and `ShellGrammarTable` derive from the same generated portable rows in `grammar.go`; `interactive` mainly adjusts credentials. Give the unchanged one-shot surface its explicit existing declaration before making `shell/commands.json` interactive-only. Reuse typed builders, validators, protocol and execution where semantics match; do not send one-shot input through a new-Shell legacy adapter. Keep one common interactive inventory for PC/Android and an explicit PC extension, not two handwritten Shell catalogues or a universal parser framework.
2. **Fix the real semantic boundaries.** Add the typed forget selector/capability, unify connection credential resolution, define collection/deadline/outcome rules, and keep cancellation/target pinning at the shared execution boundary. Do not hide the numeric-SSID defect with command-text prefixes or a string shim.
3. **Update both typed Shell adapters and views.** Generate bindings rather than editing them. Teach parent-executable/projection parsing, help and completion from the same metadata, replacing the current scan-only parsing exception with one explicit rule. Positionals and key values must remain literals, even if they look like child keywords. Reuse existing acquisition and presentation primitives; no reflective plugin loader, factory hierarchy, new persistence layer or dependency is justified by this design.
4. **Audit fields before claiming full output.** Map each safe existing protocol/diagnostic field to subject, source/time, availability, unit, redaction and default/projection placement. Include raw framework fields, unknown enums, EHT/AP/link relations, host supplements and historical results. Every safe field must be rendered or have a specific documented exclusion; fixtures alone are not proof of an exhaustive field audit. Remove duplicate facts only within the same subject/observation. No existing full-field acceptance item becomes complete just because commands were renamed.
5. **Replace fixtures/help/docs atomically.** Update portable command/rejection/pipeline fixtures, PC/Android adapter cases and the operator guide; delete obsolete interactive paths, alias handling, `use` branches and dead verbosity render paths. Retain independently used one-shot/RPC/harness code, with boundary regression tests, not as an old Shell mode.
6. **Verify proportionately.** Run `make check-shell`, `make test`, `make build`, both configured lints, `make fmt-check`, `make modernize-check` and whitespace checks with supported pinned tools. Include real-readline PTY/history checks. Device installation, actual scans/probes/mutations and live acceptance need separate explicit authorization; neither this document nor a unit-test pass grants it.

Required regression evidence:

- Every new portable path has equivalent PC/Android parsing, typed intent, help, completion and effect classification; every superseded interactive path is rejected unless independently valid as a new prefix. Android help contains no executable PC-only stubs. One-shot commands/output/credentials and TUI/harness contracts remain unchanged.
- Full Wi-Fi output contains the safe union of former views; `eht`, `ip` and capabilities restrict without hiding prerequisite/error provenance. Scan cache reads cause zero scan starts; each fresh form starts exactly once and returns its requested view without another command.
- ADB full/group/leaf plans contain exactly ten/five/one unique invocations, with both Connectivity-only sources covered. Deadline, process error, missing serial, unsupported subcommand and cancellation retain explicit incomplete/non-success results. Safe JSON/text/copy contain no credentials.
- Numeric and BSSID-shaped SSIDs, config ID zero, stale Connectivity IDs, empty/default Network snapshots, malformed success payloads and ID namespace collisions fail or select exactly as declared. An agent without typed-forget capability is not sent an ambiguous mutation.
- Explicit/session/environment credential precedence, invalid-explicit no-fallback behavior, old plaintext cleanup, activity rotation/process loss and redacted transcript echo remain protected. Unfiltered draft/search recall is verified independently. Single-target and deliberate broadcast behavior is tested at dispatch, including membership/identity changes.
- Immediate versus waited Wi-Fi evaluation, IPv4/IPv6/all link provisioning, configured lab traffic, unsupported profiles, last rejected attempt, finite monitoring and post-cancel cleanup have typed checks, not output-string success heuristics.
- Shared pipeline fixtures cover `find` first/no match, `match`/`except`, `last` with fewer/more/blank/zero-input lines, `trim` at position 1/past the end/on blank and Unicode lines, both transform orders, counts after trimming, invalid numeric arguments, `begin` rejection, JSON exclusivity and failure preservation. Both implementations and the selected layout widths are checked by the automated cutover suites; live layout acceptance remains separate.
- PC 24/40/80/120-column and non-TTY output, Android measured narrow/wide/rotation/IME, full-value access, logical filtering/counting and safe JSON preserve the complete requested safe evidence and unfilterable failure indication. Live human readability and exhaustive field coverage remain separate claims requiring their own evidence.

## Adopted decision record

1. Adopt this hierarchy and exact spelling: `request` for Wi-Fi actions/standalone probes; `test` for assertions/profiles; `monitor wifi`; session-only `set/clear cli`; no new configuration mode.
2. Adopt full-by-default Wi-Fi/ADB output, the listed projections and old-path/alias removal; retain scan `brief` as the only explicit summary. Adopt the approved filters with the numeric/Unicode rules above.
3. Merge `use` into connect with the stated explicit/session credential rule; unify wait/assert as immediate or timeout-bounded `test wifi`; include all such test attempts in the historical last report.
4. Adopt explicit typed SSID/config-ID forgetting and the single-target restriction for all Wi-Fi mutations/tests. Keep only deliberate broadcast for the other listed classes.
5. Adopt the ten-source ADB union and total 90-second default/15-minute maximum collection budget. Synthetic plan/deadline/cancel checks validate the implementation; actual supported-source behavior remains device-dependent and unverified live.

The user subsequently authorized implementation of this design. That authorization did not include operating devices, committing or pushing. Items above describe the adopted hierarchy, credential, typed-selector, target and budget decisions, not remaining permission requests. The limited automated verification must not be confused with live acceptance.

## Junos comparison and primary references

Juniper sources below were consulted on **2026-11-03**; no publication/revision date is inferred from the access date. They establish vendor behavior, not Android availability or Dropcheck implementation.

| Official source | Junos behavior | Deliberate Dropcheck choice |
| --- | --- | --- |
| [Operational mode overview](https://www.juniper.net/documentation/us/en/software/junos/cli/topics/topic-map/junos-cli-operational-overview.html) | `show`, `monitor`, `test`, `request` are operational categories; `request` is system-level operations, while `ping`/`traceroute` are top-level. `brief/detail/extensive/terse` vary output. | Group standalone probes under `request` by product choice, not a claim of exact Junos syntax; default `show` is full, not a hidden extensive view. Test names are Dropcheck-specific. |
| [Online help](https://www.juniper.net/documentation/us/en/software/junos/cli/topics/topic-map/online-help.html) | Contextual `?`, partial-name help and Tab/Space completion for unique matches. | Retain no-Enter help, unique prefixes and Tab/touch completion; do not add complete-on-Space now. |
| [Operational output filters](https://www.juniper.net/documentation/us/en/software/junos/cli/topics/topic-map/filtering-operational-command.html) | `find`, `match`, `except`, `last`, `trim`, `count`, `display json` and real pagination controls are documented. The detailed trim example uses a 1-based starting column (`trim 11` removes ten columns); last can depend on screen length. | Adopt the approved filter names, replacing `begin`; keep bounded logical-line/JSON behavior. Require explicit last/trim numbers, define portable Unicode trim positions and omit pager-dependent counts or fake pager/file/pipe-operation support. |
| [CLI environment settings](https://www.juniper.net/documentation/us/en/software/junos/cli/topics/topic-map/cli-environment-settings.html) | Operational `set cli` and `show cli` manage/display CLI environment, not a candidate device configuration. | Use that session distinction, but the default-passphrase option is Dropcheck-specific and memory-only. Do not copy unused screen/prompt/terminal controls. |
| [Configuration mode](https://www.juniper.net/documentation/us/en/software/junos/cli/topics/topic-map/cli-configuration.html) | Configuration statements are committed to activate device configuration. | No `configure`/`commit` without a real candidate/validate/apply/discard owner and lifecycle. Immediate radio actions cannot be retroactively staged. |

Code evidence: [portable inventory](../../shell/commands.json), [interactive parser](../../controller/internal/command/interactive.go), [ADB collection](../../controller/internal/adbdiag/interactive.go), [host supplements](../../controller/internal/app/diagnostic_supplement.go), [Android collection](../../agent/src/main/java/io/dropcheck/agent/NetworkRepository.kt), [Wi-Fi defaults](../../agent/src/main/java/io/dropcheck/agent/WifiCommandPolicy.kt), [forget matching](../../agent/src/main/java/io/dropcheck/agent/WifiConnectorPolicy.kt), [wire contract](../../proto/dropcheck/v1/control.proto). The old-to-new tables above describe the baseline; the linked code and current operator contract describe the replacement.
