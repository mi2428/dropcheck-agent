# Interactive Shell commands and maintenance

The PC and Android interactive Shells use the Junos-inspired hierarchy below. This replaces the former interactive grammar; there is no compatibility alias layer. The independent PC one-shot CLI and Controller TUI keep their existing entry points. See [the design and complete migration map](junos-redesign.md), [field audit and QA evidence](qa-junos.md), and the historical [acceptance checklist](tasks.md).

**Validation scope (2026-11-03):** local unit/integration/PTY/race tests and builds only. Earlier Pixel 9 results in [acceptance.md](acceptance.md) predate this cutover and are not acceptance evidence for it. Device installation, live scans/probes/mutations, commits and push were not performed.

## Command hierarchy

```text
show version
show cli
show permissions
show network [default | id <network-id>]
show wifi status [connection | ip | capabilities | eht]
show wifi scan [ssid <ssid> | bssid <bssid>] [band <band>] [mlo] [brief]
show test profiles
show test last
show devices                                             # PC only
show adb [wifi | connectivity [networks | requests | diagnostics | traffic]] [timeout <ms>]

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
request path-mtu <host>
request global-ip
request dns <name>
request http <url>
request download <url>

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

- Executable parents return their whole subject: `show network`, `show wifi status`, `show adb`, and `show adb connectivity`. Group-only input, such as `request` or `test`, displays local help and performs no acquisition. A projection restricts the subject, not the verbosity. `detail`, `fresh`, root probe aliases, `use`, `check`, `quit`, and `begin` are not accepted interactive spellings.
- Keywords are lowercase and accept unique prefixes. Values retain literal case, spaces and punctuation. Quote/escape whitespace and literal pipes. A keyword-looking SSID remains a value. There is no shell expansion, subprocess pipeline, arbitrary ADB shell, pager, configuration mode, candidate state, or fake commit.
- `?` requests no-Enter contextual help. `help request wifi`, partial-word help, argument/value hints, Tab/cursor-tail completion and Android touch help/history remain local and secret-safe. Space remains a separator, not a completion key. Android retains app navigation instead of pretending to exit the service/process.

## Acquisition, targets and outcomes

`show wifi status` collects connection/radio, selected physical Wi-Fi IP, device capabilities, cached APs, EHT/MLO interpretation and contextual Android Networks. It includes safe IE/raw references and explicit availability. Related WifiInfo/Network observations are independent captures, not an atomic snapshot. Layout width cannot select fewer fields. Android retains the explicit display/history limits and full-safe-value Inspect/copy path; a limited visible block does not mean the retained result is truncated.

`show wifi scan` never starts a scan. `request wifi scan [eht]` starts one bounded scan; the EHT composite uses that exact returned AP payload with independently acquired passive prerequisites. Scan selectors filter evidence, not radio scan destinations. Failed/throttled/timed-out refresh data is reference cache, not fresh success. Five unique true metadata values (registration, start, broadcast, update, wait) and no acquisition errors are required for fresh success. Cancellation prevents later acquisition and does not undo completed radio actions.

PC full/IP/EHT views retain separately labelled existing ADB host supplements. Missing serial/interface and attempted acquisition errors are visible. These optional host sources do not redefine Android typed success. Explicit `show adb` instead requests ten sources (five Wi-Fi plus five Connectivity); a leaf requests one. Its whole-collection budget defaults to 90,000 ms and accepts 1..900,000 ms, with the existing 90-second per-process ceiling. Completed sections remain visible; unstarted sources after a deadline/cancel are marked not collected. Any requested-source failure makes the ADB result non-successful.

`show network` uses the lightweight all-Network snapshot, not Wi-Fi diagnostics. `default` means the captured Android platform default, including VPN/cellular; `id` requires a canonical positive Connectivity ID. Count, ID/uniqueness/default/time/availability/error consistency are checked. A valid empty all-Network snapshot differs from a missing specifically requested Network. Probe Network selection remains physical Wi-Fi and fails closed on mismatches.

PC targets stay pinned to the startup-resolved identity; Android targets self. Every Wi-Fi mutation and every `test` requires one PC target. Explicit broadcast remains available only for passive reads, finite monitoring, scan requests and standalone probes, with membership frozen at dispatch. There is no in-Shell target switching or fallback to a new agent after transport loss.

Saved Wi-Fi `config-id` is a different namespace from Connectivity `network-id`. Forgetting requires an explicit selector: SSID `123` never means configuration 123; configuration zero is valid. New typed forget requests require `wifi.forget.typed`, and are never encoded into the legacy ambiguous `ForgetWifi.target`. The latter remains only for independent one-shot/RPC callers. Platform/Device Owner safeguards remain in the shared Android execution path.

`test wifi` evaluates immediately when timeout is omitted; a positive timeout waits finitely for the same conjunctive conditions. Link testing observes IPv4/IPv6/all provisioning without probes. Lab testing requires explicit authorized destinations and stops on failed prerequisites. Monitor defaults remain 10,000 ms/1,000 ms. `show test last` is the historical, session-local latest Wi-Fi/link/lab attempt, including recognized preflight rejections; a rejected abbreviated test cannot leave stale PASS. It is not authorization or current readiness.

## Credentials and history

Connect prefers an explicit credential. PC also permits `passphrase-env <name>` instead of `passphrase`. Only omission permits a session default; empty, invalid, conflicting or unresolved explicit sources fail without fallback. Connect defaults to 45,000 ms; reconnect to 30,000 ms. Cycle still requires its own explicit credential and retains existing bounds/probe/cleanup choices.

Defaults exist only in current-session memory and are cleared on PC Shell exit. Android session state may survive Activity rotation, not process death. The old plaintext preference has deletion-only cleanup and is never imported. `show cli` reports credential present/unset, owner/target/layout/history policy, never the value, and performs no device query.

Recall uses a successfully parsed allowlist, with no pipes: local help; version/cli/permissions; bare `show network`; Wi-Fi connection/IP/capabilities; test profiles/last; PC devices. Full status, EHT/AP/raw/ADB views, explicit/default Network selections, credentials, mutations, probes, invalid drafts and filter literals are excluded. Native history search/Prev/Next apply the same rule. Neither Shell writes history to disk. A transcript is not recall history.

## Filters and structured presentation

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

`ok` is typed, not inferred from visible lines/counts. Each deliberate PC broadcast result is one complete envelope (NDJSON), with identity in safe lines; progress goes to stderr. One-shot `--format json` keeps its independent protobuf-based contract.

## Maintenance and checks

`shell/commands.json` owns 36 portable paths, arguments, switches, enums, effects and descriptions. `pc_values` declares host-only extensions. Run `make generate-shell`, never edit the two generated bindings. `make check-shell` and shared command/rejection/pipeline fixtures detect drift.

PC-only interactive paths live in `command/interactive.go`; the independent CLI declarations live in `one_shot_grammar.go` plus `hostGrammar`. Typed builders, validators, protocol, acquisition and safe presentation are shared where their meaning agrees; there is no old-Shell argv rewrite adapter.

For a real new command: establish effects/targets/permissions/bounds/cancellation and typed success first; reuse an existing RPC if it fits. Otherwise add the smallest protocol/executor/capability extension and regenerate with pinned protoc 35.0. Add inventory and both typed mappings, then result validation, source-aware safe rendering and a failing boundary test. Do not add another help catalogue or generic plugin system. Update the field audit for new payload fields.

Use `make test`, `make build`, configured lints, `make fmt-check`, `make modernize-check`, and whitespace checks. `make test TARGET=controller` includes race-instrumented real-readline PTY privacy and running-cancel checks with a local fake executable, not installed ADB. See [QA evidence and remaining live limits](qa-junos.md).
