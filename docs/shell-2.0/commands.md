# Interactive Shell commands and maintenance

The PC and Android interactive Shells use the Junos-inspired hierarchy below, with no compatibility alias layer. The 2.0 operator surfaces are Shell, Android widgets and `dropcheck tui`; the independent one-shot CLI, separate `tui` workflow and Go test DSL have been removed. TUI retains its dashboard/YAML/no-TUI/JSONL paths, not plan-selection or review/rerun entry points. See [the design and migration map](junos-redesign.md), [field audit and QA evidence](qa-junos.md), and the historical [acceptance checklist](tasks.md).

**Validation scope (2026-11-03):** local unit/integration/PTY/race tests and builds only. Earlier Pixel 9 results in [acceptance.md](acceptance.md) predate this cutover and are not acceptance evidence for it. Device installation, live scans/probes/mutations, commits and push were not performed.

## Command hierarchy

```text
show version
show cli
show oui
show permissions
show network [default | id <network-id>]
show wifi status [full | connection | ip | capabilities | eht]
show wifi scan [ssid <ssid> | bssid <bssid>] [band <band>] [mlo] [brief]
show test profiles
show test last
show devices                                             # PC only

debug android wifi info
debug android wifi scan [ssid <ssid> | bssid <bssid>] [band <band>]
debug android network capabilities network-id <id>
debug android network link-properties network-id <id>
debug android network wifi-info network-id <id>
debug adb [wifi | connectivity [networks | requests | diagnostics | traffic]] [timeout <ms>]

request wifi connect <ssid> [passphrase <secret>] [security <mode>] [bssid <bssid>]
                     [band <band>] [mac-randomization <mode>] [timeout <ms>]
request wifi disconnect
request wifi reconnect [timeout <ms>]
request wifi forget ssid <ssid>
request wifi forget config-id <id>
request wifi cycle <ssid> passphrase <secret> [connection/cycle options]
request wifi scan [eht] [ssid <ssid> | bssid <bssid>] [band <band>] [timeout <ms>]
request ping <host>
request traceroute <host>
request pmtu <host>
request ip external
request dns resolve <name>
request http check <url>
request http download <url>
request oui update

test wifi [ssid <ssid>] [bssid <bssid>] [security <mode>] [band <band>]
          [require-ip <true|false>] [require-validated <true|false>] [timeout <ms>]
test link ssid <ssid> [family <ipv4|ipv6|all>]
test lab ssid <ssid> family <ipv4|ipv6> gateway <ip> dns <name> resolver selected
         http <url> expected-status <100..599> timeout <1..30000>
monitor wifi [duration <ms>] [interval <ms>]

set cli default-passphrase <secret>
clear cli default-passphrase
help [<command-path>]
exit                                                     # PC only
```

The comments above identify scope; they are not input syntax. The inventory/help lists every option. Standalone requests retain their prior probe options and `ssid/bssid/network-id` selectors. Scan requests without `eht` also accept `mlo` and `brief`. `show wifi status ip` retains physical Wi-Fi selectors; `eht` accepts mutually exclusive SSID/BSSID evidence filters.

`show oui` and `request oui update` operate locally in both Shells, independently of Android target selection. BSSID vendor fields use offline IEEE longest-prefix lookup; only an explicit update downloads the four public registries. See [database ownership, attribution limits and failure behavior](../oui-vendors.md).

### Scan AP headings and semantic spacing

Full scan AP headings use one fixed-width prefix: `[*] AP 1` for a BSSID
observed connected at collection, `[+] AP 2` for an otherwise unmarked AP with
observed EHT support, and `[ ] AP 3` otherwise. Connected takes precedence;
the body's EHT field retains the evidence for a connected EHT AP. EHT evidence
is Android's `802.11be` standard or successfully decoded, non-truncated EHT
capabilities/operation, not merely a 6GHz band, wide channel or MLD identity.
No mark is not proof of non-support. The existing MLO-view related-MLD mark is
now `~`, not `+`; that view includes its own legend.

Scan results retain a typed connection observation collected after reading
the scan results: physical Wi-Fi BSSIDs, timestamp, completeness and errors.
Matching never uses the SSID, the default Network, another device, a related
MLD or a later rendering-time lookup. Multiple physical Wi-Fi connections can
match separately. Known partial identities can be marked while missing
identities remain explicitly unknown. A complete empty observation means
disconnected; missing, invalid or incomplete data does not. Scan and connection
observations are not claimed to be atomic. Cached scan evidence is still cached.

Paragraphs are separated by exactly one blank line: summary, table, display
metadata/legend, each AP, child information groups, fields and errors. Headings
stay adjacent to their own body; consecutive fields and table rows remain
compact. Android retains section roles rather than guessing from text.
The original brief-table columns and native raw reference text are retained.

- `show wifi status` is the quick everyday summary; its named children select detailed subjects and `full` selects the complete structured diagnostic bundle. Other executable parents, including `show network`, `debug adb` and `debug adb connectivity`, return their whole subject. Group-only input, such as `debug android`, `request` or `test`, displays local help and performs no acquisition. `detail`, `fresh`, root probe aliases, `show adb`, `use`, `check`, `quit`, and `begin` are not accepted interactive spellings.
- Keywords are lowercase and accept unique prefixes. Values retain literal case, spaces and punctuation. Quote/escape whitespace and literal pipes. A keyword-looking SSID remains a value. There is no shell expansion, subprocess pipeline, arbitrary ADB shell, pager, configuration mode, candidate state, or fake commit.
- `?` requests no-Enter contextual help. `help request wifi`, partial-word help, argument/value hints, Tab/cursor-tail completion and Android touch help/history remain local and secret-safe. Space remains a separator, not a completion key. Android retains app navigation instead of pretending to exit the service/process.

### Active request names

The 2026-11-05 naming follow-up uses spaces for subject/action hierarchy rather than inventing compound action names. Wi-Fi `connect/disconnect/reconnect/forget/cycle/scan`, `ping` and `traceroute` keep their names. Technical option keys such as `network-id`, `min-mtu` and `expected-status` are unchanged.

| Previous interactive spelling | Current spelling | Meaning |
| --- | --- | --- |
| `request path-mtu <host>` | `request pmtu <host>` | Discover path MTU; do not change interface MTU. |
| `request global-ip` | `request ip external` | Obtain the source IP seen by the existing external service, not the local address list or a DHCP lease. |
| `request dns <name>` | `request dns resolve <name>` | Resolve A/AAAA through the selected Network's resolver. |
| `request http <url>` | `request http check <url>` | Check the expected HTTP status without following redirects. |
| `request download <url>` | `request http download <url>` | Transfer/count response bytes and measure throughput; follow redirects, discard content and save no file. |

Bare `request ip`, `request dns` and `request http` display local help and perform no acquisition. Unique prefixes, quoted/keyword-looking values, existing defaults/options/Network selectors, finite timeouts, traffic cost, result rendering and typed outcomes are preserved. `ip external` accepts `family ipv4|ipv6|all` (default `all`), not `auto`. These requests remain bound to the selected physical Wi-Fi Network; the new IP subject does not broaden transport selection.

Old executable spellings are rejected rather than retained as aliases. There is no separate one-shot grammar. RPC and typed-operation identifiers remain unchanged. See [request-name regression evidence](qa-request-names.md).

## Acquisition, targets and outcomes

`show wifi status` uses one existing `GetWifiStatus` RPC, including its selected physical Wi-Fi IP snapshot. It shows connection state/SSID/BSSID, configuration/STA identity, band/channel/width/PHY/RSSI, security/PMF, current and explicitly qualified reported maximum rates, decoded AP/HE/EHT observations, MLD/link counts and per-link values, permissions, interface/Network/default identity, Android Internet/Validated capabilities, addresses, routes, DNS, MTU, DHCP, Private DNS, NAT64, proxy, network capability details and collected IPv6 RA fields. Unavailable observations retain their explicit reasons; Android validation is not an active connectivity test. It does not request the full device capability inventory, map every cached AP or collect every Network's details, and the PC performs no ADB supplements for this summary. Connected-channel-width resolution may still read the scan cache; no active scan is started.

The user-approved expanded overview has room for roughly four times the initial 50–80-line view, not a forced line count or arbitrary truncation. Existing useful connection and selected-IP details are shown without adding acquisitions. Bulk metadata, whole IE dumps, raw framework strings, AP listings and nested Network WifiInfo copies remain outside the overview. Narrow-terminal wrapping, decoded features or many addresses/routes/links can increase its length; values are not silently clipped. Summary selection happens before text filters, safe JSON, Android Inspect and copy.

Top-level subjects contain two-space-indented groups; field labels are indented two spaces further and share a value column. Only top-level subjects have blank separators. Related short scalars share named components on one row (for example, channel/band/width/frequency or Tx/Rx/link rates); consolidating absent Passpoint fields does not discard populated values. List values and wrapped continuations hang beneath the value column. Narrow layouts put labels and values on separate indented lines rather than clipping them. For example:

```text
Connection
  Identity
    SSID                    : Example Lab
    BSSID                   : 02:00:00:11:22:33
  Radio
    Channel                 : 6GHz; ch=33; width=80MHz; 6115MHz

IP (separate Network observation)
  Link properties
    MTU                     : 1500
```

`show wifi status connection`, `ip` and `capabilities` retain their detailed subjects. `show wifi status eht` uses the focused EHT/MLO renderer for connection/link/AP/device evidence, rather than repeating the general diagnostic bundle. Use `show wifi status full` for the complete structured passive bundle: connection/radio/IP, capabilities, cached APs, contextual Android Networks, EHT/MLO interpretations, and itemized IE records (`id`, `extension`, `bytes`, `hex`). IE/security/capability hex and typed numeric bit representations remain valid structured evidence. Whole-object WifiInfo/ScanResult/MloLink/LinkProperties/NetworkCapabilities string dumps belong only to `debug`, not to ordinary snapshot views or `full`. `full` performs a new passive collection, not a scan refresh or an expansion of a cached previous summary. Neither form permits credential leakage.

### Raw-only debug views

`debug android wifi info` reuses one passive `GetWifiStatus` acquisition and projects only its WifiInfo and associated/affiliated MloLink string snapshots, including a captured disconnected WifiInfo. `debug android wifi scan` reads the framework scan cache through `GetWifiScan`, never `startScan`; literal SSID/BSSID and band restrictions are retained. It projects ScanResult strings and their affiliated MloLink strings, without repeating parsed AP/IE tables.

`debug android network capabilities`, `link-properties` and `wifi-info` require an explicit canonical positive Connectivity `network-id`. They reuse `GetNetwork` for exactly that ID, including non-Wi-Fi Networks where the requested raw object exists. `wifi-info` selects that Network's WifiInfo/MloLink snapshot, not the separate WifiManager capture. A stale, missing or inconsistent identity cannot fall back to the default Network or another connection. Missing raw data is an explicit failure; partial failed captures retain safe raw evidence and acquisition errors without claiming success.

Debug output contains only selected raw strings and minimal source/target/result/latency/error metadata. It does not append the ordinary summary or collect PC host supplements. Credential/control sanitization, bounded native layout, full-safe-value access, pipes, safe presentation JSON and session-only recall still apply. Debug makes its own current passive acquisition; it does not promise to reproduce an earlier `show` capture.

`show adb ...` has moved to Shell `debug adb ...` without an alias or one-shot entry. Shared structured renderers omit whole-object string dumps recursively while preserving typed IE/security hex and numeric bit fields. Protobuf acquisition data and typed evaluation are unchanged; presentation never edits the captured result. See [raw/debug regression evidence](qa-raw-debug.md).

Focused Android EHT output retains literal connected/AP identities in typed records alongside its analysis. Free-form fields and nested credential assignments are sanitized before text/JSON/copy output. See [the follow-up adversarial QA](qa-status-followup.md) for reproduced defects and verification limits.

Related WifiInfo/Network observations are independent captures, not an atomic snapshot. Layout width only reflows the fields selected by each command. Android retains the explicit display/history limits and full-safe-value Inspect/copy path; a limited visible block does not mean the retained result is truncated. Shell and tui preserve typed outcomes; shared structured rendering follows the raw/debug separation above.

`show wifi scan` never starts a scan. `request wifi scan [eht]` starts one bounded scan; the EHT composite uses that exact returned AP payload with independently acquired passive prerequisites. Scan selectors filter evidence, not radio scan destinations. Failed/throttled/timed-out refresh data is reference cache, not fresh success. Five unique true metadata values (registration, start, broadcast, update, wait) and no acquisition errors are required for fresh success. Cancellation prevents later acquisition and does not undo completed radio actions.

`show wifi scan brief` keeps its original SSID/BSSID/radio/security/`AGE(s)` table columns, without a vendor column or repeated AP-detail blocks. Seconds retain the exact millisecond fraction (up to three decimal places), using integer arithmetic even at the uint64 boundary. Zero is a known zero age; `?` means the observation timestamp is unavailable. Table columns have a two-space minimum gap; `eap_wpa3_enterprise` is displayed as `wpa3-eap`, while unknown security spellings remain intact. For ordinary brief output, layouts group by SSID when the complete table does not fit, before using narrower records without clipping identities or age values. Collection errors remain visible, `mlo` still includes the requested link evidence, and omitting `brief` retains the complete details and vendor fields, with one blank line between AP blocks. Presentation does not change typed probe/tui results.

PC full/IP/EHT views retain separately labelled existing structured ADB host supplements. Missing serial/interface and attempted acquisition errors are visible. These optional host sources do not redefine Android typed success. Explicit `debug adb` instead requests ten raw sources (five Wi-Fi plus five Connectivity); a leaf requests one. Its whole-collection budget defaults to 90,000 ms and accepts 1..900,000 ms, with the existing 90-second per-process ceiling. Completed sections remain visible; unstarted sources after a deadline/cancel are marked not collected. Any requested-source failure makes the ADB result non-successful. Android/ADB structured-field reconciliation is a separate follow-up, not a text-deduplication side effect of raw separation.

`show network` uses the lightweight all-Network snapshot, not Wi-Fi diagnostics. `default` means the captured Android platform default, including VPN/cellular; `id` requires a canonical positive Connectivity ID. Count, ID/uniqueness/default/time/availability/error consistency are checked. A valid empty all-Network snapshot differs from a missing specifically requested Network. Probe Network selection remains physical Wi-Fi and fails closed on mismatches.

PC targets stay pinned to the startup-resolved identity; Android targets self. Every Wi-Fi mutation and every `test` requires one PC target. Explicit broadcast remains available only for passive reads, finite monitoring, scan requests and standalone probes, with membership frozen at dispatch. There is no in-Shell target switching or fallback to a new agent after transport loss.

Saved Wi-Fi `config-id` is a different namespace from Connectivity `network-id`. Forgetting requires an explicit selector: SSID `123` never means configuration 123; configuration zero is valid. Typed forget requests require `wifi.forget.typed` and are never encoded into the legacy ambiguous `ForgetWifi.target` wire field. Retaining a protocol field does not restore a removed CLI entry. Platform/Device Owner safeguards remain in the shared Android execution path.

`test wifi` evaluates immediately when timeout is omitted; a positive timeout waits finitely for the same conjunctive conditions. Link testing observes IPv4/IPv6/all provisioning without probes. Lab testing requires explicit authorized destinations and stops on failed prerequisites. Monitor defaults remain 10,000 ms/1,000 ms. `show test last` is the historical, session-local latest Wi-Fi/link/lab attempt, including recognized preflight rejections; a rejected abbreviated test cannot leave stale PASS. It is not authorization or current readiness.

## Credentials and history

Connect prefers an explicit credential. PC also permits `passphrase-env <name>` instead of `passphrase`. Only omission permits a session default; empty, invalid, conflicting or unresolved explicit sources fail without fallback. Connect defaults to 45,000 ms; reconnect to 30,000 ms. Cycle still requires its own explicit credential and retains existing bounds/probe/cleanup choices.

Defaults exist only in current-session memory and are cleared on PC Shell exit. Android session state may survive Activity rotation, not process death. The old plaintext preference has deletion-only cleanup and is never imported. `show cli` reports credential present/unset, owner/target/layout/history policy, never the value, and performs no device query.

Recall stores every submitted nonblank command, independently of parsing, command kind, credentials, pipes or execution success. This includes all Wi-Fi views, requests, tests, monitors, malformed input and credential-bearing commands, so they can be recalled, edited and resubmitted. History is session-local memory only (PC: 1000 entries; Android: 100); adjacent duplicates may share one entry. Neither Shell writes history to disk. Clearing the active default passphrase does not delete a previously entered command from recall. Transcript/log/output redaction is separate from input recall and remains enabled. A transcript is not recall history.

Use Up or Ctrl-P for the previous command and Down or Ctrl-N for the next command. Moving past the newest entry restores the current draft unchanged, including unfinished or credential-bearing input. Recalling a command does not execute it; Enter/Go submits it again through normal parsing, target selection and execution checks. Android also provides Prev/Next touch buttons; PC key navigation requires an interactive terminal, not redirected input/output. See [the history follow-up](qa-history.md) for the superseding design and regression checks; earlier filtered-history QA records describe the old policy.

## Filters and structured presentation

Structured Shell metadata uses aligned field/value columns, including request results, test reports, and monitor output. PC alignment uses display-cell widths, not rune counts. Android aligns native fields with measured text widths and spans; exact retained field facts keep their typed roles after line filters. Repeated probe, test-stage/metric, cycle and address data use tables where useful, with lossless record layouts when tables do not fit. Raw/native reference text and URLs are not inferred to be field records from their colons, and machine JSON is not passed through human column alignment.

Choose syntax by role, not by command or acquisition source. Headings have no trailing colon. Fields use `Label: value`, indented two spaces relative to their heading, with a shared value column for peer fields. Tables express that relationship through headers and columns, not colons in each cell. Named composite values may retain `key=value` components, such as `Radio: band=6ghz ch=5`; decoded security/roaming/IE lists remain complete multiline values beneath a named field. Native Android applies the same relative field hierarchy with measured layout, while logical/filter/copy records keep their established labels. Usage, navigation lists, logs and compact status summaries retain their separate roles.

ADB supplements follow the same field/table rules as Android evidence. Their `Source` and controller receipt time join the owned body records before final layout; display width does not trigger acquisition again or change that receipt time. The time is not an Android observation timestamp. TUI detailed check inspection reuses the structured report renderer at its available width; selecting one step does not recalculate the core's whole-run outcome or counts.

Generated `Source` and `Target` are independent fields, not two labels inside one value. Android results use `Source: android` and `Target: self`; Controller results identify the acquired agent. Historical results keep that acquisition target even after the current selection changes. Status and failure messages are separate fields too. Android filters preserve an unchanged field's original role, including multiline values and availability/wire-value pairs, without promoting identical raw text into a field or restoring explicitly removed lines. Oversized results retain bounded metadata plus full safe values in Inspect/copy.

The colon belongs immediately after the field label; padding belongs after the colon, never inside the label. Only the values share a column:

```text
a:    1
bc:   2
defg: 3
```

Wrapped values hang beneath that value column. At narrow widths, the label and its attached colon precede an indented value; even a wrapped long label must not leave the colon on its own line. Tables have exactly one blank line before the header and after the final row. Adjacent tables share the separating blank line; empty headers and lossless record layouts retain their outer boundary. Android native separator lines count toward the table display budget. Its logical/copy fields remain `label: value` records rather than imitating native glyph widths with spaces.

Explicit filters retain their defined line behavior: `find` may remove an initial blank, and `except '^$'` removes blanks. Formatting does not guess table roles from plain text or silently reinsert lines removed by a filter. See [the field and table-boundary QA](qa-field-layout.md) for this follow-up's checks and limits.

See [the alignment adversarial QA](qa-alignment.md) for reproduced layout, identity-selection and information-retention defects, checks and unverified native behavior.
See [the cross-surface style QA](qa-style-consistency.md) for the role-based inventory, corrections and verification limits.

Both Shells support `find <pattern>`, `except <pattern>`, `last <lines>`, `match <pattern>`, `trim <start-column>`, `count`, and exclusive `display json`. The whole pipeline is validated before dispatch, with at most 16 stages. `begin`, `display set`, `no-more`, `save`, `tee` and pipe operations are rejected.

- `find` keeps the first matching logical line and everything after it; no match yields empty output. `match`/`except` keep matching/nonmatching lines in order.
- `last` keeps at most the last N logical lines in their original order, without allocating N entries. `trim` keeps each line from a 1-based Unicode scalar position: `trim 11` removes the first ten positions; `trim 2` on `A😀B` yields `😀B`. Short lines become empty lines rather than disappearing. Both numbers are mandatory decimal integers in 1..2147483647.
- Count blank logical lines, but do not invent a line after a terminating newline. Empty input has zero lines. Stages run in order on sanitized complete logical records before wrapping; neither terminal columns nor Android pixels change matches/counts.
- Patterns are limited to 512 UTF-8 bytes. Go `regexp` and RE2/J share literals, classes, alternation, groups, anchors, quantifiers and separate inline flags `(?i)`, `(?m)`, `(?s)` / their negative forms. Backreferences, lookaround, named/scoped groups, Unicode properties and engine-specific escapes are rejected. Portable escapes are punctuation and `\d \D \s \S \w \W \n \r \t \f`; double a backslash through the command lexer, including inside quotes.
- Control-sequence normalization and credential sanitization precede filtering/copy/inspection/speech. Literal typed SSIDs such as `password=Guest` are preserved, but identity newlines cannot forge independent source records. Filtered failure stays visible on PC stderr or the Android unfilterable failure indicator.

Interactive `| display json` is a safe presentation envelope, not a protobuf dump:

```json
{"ok":false,"lines":["Command FAILED","Reason: permission denied"]}
```

`ok` is typed, not inferred from visible lines/counts. Each deliberate PC broadcast result is one complete envelope (NDJSON), with identity in safe lines; progress goes to stderr. The retired one-shot `--format json` is not a Shell flag. The basic Go API returns typed results rather than a presentation envelope.

## Maintenance and checks

`shell/commands.json` owns the portable paths, arguments, switches, enums, effects and descriptions. `pc_values` declares host-only extensions. Run `make generate-shell`, never edit the two generated bindings. `make check-shell` and shared command/rejection/pipeline fixtures detect drift.

PC-only paths live in `controller/internal/command/interactive.go`; `ParseShellTokens` consumes that extension and the generated portable inventory. Shell dispatch consumes `shell.Command` directly in `internal/app/repl.go`. `internal/acquire` owns the single atomic/compound acquisition and payload-validation implementation shared by Shell, tui and the public facade. YAML compilation lives in `internal/tuiconfig`; private `internal/tuirun` adds policies, scope controls, ordered events and owned cleanup through a small acquisition adapter. `internal/tui` renders only the tui dashboard. The sole public Go package is `pkg/dropcheck` for basic operations, not scenario authoring. `one_shot_grammar.go`, linuxcli, workflow TUI and the testing.T/fluent DSL have been deleted; no old-path import alias or argv rewrite adapter remains.

For a real new command: establish effects/targets/permissions/bounds/cancellation and typed success first; reuse an existing RPC if it fits. Otherwise add the smallest protocol/executor/capability extension and regenerate with pinned protoc 35.0. Add inventory and both typed mappings, then result validation, source-aware safe rendering and a failing boundary test. Do not add another help catalogue or generic plugin system. Update the field audit for new payload fields.

Use `make test`, `make build`, configured lints, `make fmt-check`, `make modernize-check`, and whitespace checks. `make test TARGET=controller` includes a fake-backed external Go consumer, versioned module consumption through an offline file proxy without `replace`, and race-instrumented real-readline PTY recall/replay and running-cancel checks with a local fake executable, not installed ADB. The public dependency check excludes tui/UI/render packages. See [remediation and checks](../qa-post-removal.md) and [remaining live limits](qa-junos.md).
