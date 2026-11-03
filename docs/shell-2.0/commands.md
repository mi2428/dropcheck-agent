# Interactive Shell command maintenance

This describes the implemented PC/Android interactive boundary. It does not redefine the PC one-shot CLI, the TUI, or the raw protobuf JSON format. The full acceptance checklist remains in [tasks.md](tasks.md); passing the tests below is not a claim of exhaustive field coverage or real-device usability.

**検証状況:** [敵対的QA](qa.md)の第一巡11件と[第二巡](qa-round2.md)の8件を修正し、再現試験を通常実行へ統合した。`make test TARGET=controller`には実readlineのPTY検査も含む。最新target SDK 37でも指定Pixel 9の[読み取り専用実機確認](acceptance.md)は通過したが、全showフィールド・全端末・未測定の権限ダイアログまで保証するものではない。

## One inventory, two typed adapters

`shell/commands.json` owns portable command paths, positional/required arguments, switches, value options, enum choices, effect categories, help descriptions and aliases. `pc_values` labels host-only option extensions such as `passphrase-env`. Run `make generate-shell`; never edit `shell_contract_generated.go` or `AgentShellContract.kt`. `make check-shell` rejects stale bindings, and `make test` runs this check automatically.

The two languages still require typed request builders and platform execution adapters. A generated inventory cannot prove equivalent validation or that an Android API is available. `shell/command_cases.tsv` supplies an executable example for every portable path; both suites reject missing examples. Rejection cases are in `shell/invalid_cases.tsv`; shared pipeline fixtures are in `shell/pipeline_cases.tsv`.

PC-only ADB/device operations stay in `hostGrammar` in `controller/internal/command/grammar.go`. One-shot credential policy is explicit in `grammarTable(false)`; it does not acquire interactive session defaults. Android does not advertise PC host commands as executable stubs.

## Operator contract

- Canonical commands and keyword abbreviations are shared. Quoted/escaped literals retain their case and contents. Only unquoted, unescaped `|` separates stages. In quotes, `?` is data. No shell expansion, substitution, filesystem globbing or subprocess pipeline is performed.
- `?` requests context help without Enter. Partial words and trailing whitespace are distinct; help never dispatches an operation or echoes an already supplied credential. Android exposes `?`, Tab, Prev and Next buttons as well as keyboard help/completion/history. IME composing text is not consumed as help.
- Safe, successfully parsed read-only commands can enter in-memory history. Credential-setting, connection, probe-URL and malformed commands do not. Neither Shell writes history to disk.
- 未送信draftも同じ許可判定に従う。PCではnative履歴の一時編集を移動・検索の前に破棄し、送信後にも安全でない入力を残さない。
- `set default passphrase <value>` and `clear default passphrase` affect only the current Shell session. `use <ssid> [passphrase <value>]` prefers the explicit credential; otherwise a session default is required. It connects immediately and requires one PC target. No candidate configuration or `configure`/`commit` is advertised. Android removes the former plaintext preference when the Activity or service starts; it never reads/imports its old value. Session state survives Activity rotation, not process death.
- `show networks` enumerates Android Networks without obtaining Wi-Fi diagnostics or requesting a scan. `show network default` targets the captured Android default; `show network <id>` requires a canonical positive decimal ID. Missing/default-less/stale IDs fail, never select another Network. Incomplete capability/property reads produce a failed result with explicit reference data. Snapshots are point-in-time collections, not continuous identity guarantees.
- 成功とするNetwork応答は要求ID/default・件数・IDの一意性・内外ID・収集時刻・availability・errorsの整合性を満たすこと。矛盾したSTATUS_OKを成功として表示しない。正しい空の一覧と、単一Network要求への空応答は区別する。
- `show permissions` reports permission states, location enablement and Device Owner status. Read-only observation does not require Device Owner. Wi-Fi mutations retain the existing platform checks.
- `check link ... family all` requires IPv4 **and** IPv6 provisioning and displays separate findings; it does not send probes. `check lab` still requires one explicit IPv4/IPv6 family and authorized operator-supplied destinations.

## Filters and structured presentation

Both Shells accept ordered `begin <pattern>`, `match <pattern>`, `except <pattern>` and `count`. Quote a pattern containing whitespace or `|`. `begin` retains the first matching logical line and everything after it. No match is empty output. `count` includes internal empty logical lines; a terminating newline does not create an extra line. Empty input has zero lines.

Patterns are limited to 512 UTF-8 bytes, pipelines to 16 stages. Go uses its linear-time `regexp`; Android uses RE2/J 1.8. Common literals, character classes, alternation, groups, anchors, quantifiers and the separate inline switches `(?i)`, `(?m)`, `(?s)` / `(?-i)`, `(?-m)`, `(?-s)` are accepted. Backreferences, lookaround, named/scoped groups, Unicode-property escapes and engine-specific escapes are rejected. Portable escapes are punctuation escapes and `\d \D \s \S \w \W \n \r \t \f`; the command lexer consumes backslashes, so double a backslash when entering one (including inside quotes).

Filters see sanitized logical records **before** terminal wrapping or Android pixel layout. With no filter, adaptive tables remain available. Width changes cannot alter which logical lines match or how many are counted. The typed outcome is retained even when filtering hides all text.

ANSI・表示制御文字の正規化はcredential検査より先に行う。引用付きキー・反復errorsも対象とするが、typedなSSIDの`password=Guest`は除去しない。Androidのidentity Field/Table/Scanは改行を空白へ正規化し、データから独立したsource行を作らない。

Androidのtextフィルタでは、失敗結果にフィルタ対象外の`Command FAILED (output filtered)`行を付ける。件数・選択行は元の論理行だけから計算するが、copy・読み上げ元から失敗表示が消えることはない。JSONはtypedな`ok`を保持する。PCの失敗通知は標準エラーへ出力する。

`| display json` is exclusive of text stages. Both interactive Shells emit a safe presentation envelope:

```json
{"ok":false,"lines":["Command FAILED","Reason: permission denied"]}
```

`ok` comes from the operation outcome, never the line content/count. `lines` contains complete safe logical lines; it is not a raw protobuf dump or a promise of identical independently acquired timestamps. PC one-shot `--format json` retains its existing protobuf-based contract. `display set` and `no-more` are rejected rather than accepted as no-ops.

## Adding a real command

For a future `tcping` (an example, **not** an implemented command):

1. Define the target, side effects, permission/capability requirements, input bounds, timeout/cancellation and typed result. Reuse an existing operation only when its semantics actually fit.
2. Add protocol fields in `proto/dropcheck/v1/control.proto` only when necessary. Append messages to avoid unrelated descriptor-index churn. Run `make generate-go PROTOC=/path/to/protoc-35.0`; Android regenerates its lite bindings during the Gradle build.
3. Implement Android execution through `CommandExecutor` and advertise its capability in `AgentCommandRegistry`. Update exhaustive `ProtoLogFields` cases without logging credentials. Add a pure execution-policy/boundary test; never infer success by scraping displayed strings.
4. Add the portable declaration to `shell/commands.json` and regenerate. Add the Go operation builder/`ValidateOperation` and typed mapping in `grammar.go`; add the Kotlin typed mapping in `AgentInteractiveShell.kt`. Do **not** create another handwritten help/completion inventory.
5. Add Go `harness.payloadMatches`, PC rendering and Android `AgentShellAdapter` payload checks/parts. Fail closed for a missing or wrong payload, include provenance and availability, and sanitize before filtering/history/copy. Keep platform acquisition separate from layout.
6. Add a row to `shell/command_cases.tsv`, plus the smallest execution/result/width regression. Run `make test`, `make build`, configured linters and `make fmt-check`. Compile synthetic instrumentation; run it only on an authorized device. Update this guide if a previously explicit boundary changes.

The mechanical path intentionally touches protocol, device execution and presentation separately. There is no runtime plugin loader, reflective command discovery or one-implementation factory.

## Display acceptance boundaries

- Status detail adds incremental connection/capability/link evidence rather than repeating overview SSID/BSSID/rates. Scan overview and supplemental detail use AP row numbers instead of repeating BSSID. `brief` suppresses deeper scan evidence; it does not mean “narrow screen”. A new detail command is a new observation, not guaranteed continuation of the original scan.
- Android keeps the existing explicit display/history budgets and Inspect/copy-full-safe-values path. A result exceeding the visible budget is labelled; the visible display is not claimed to contain every retained value.
- Raw framework strings and decoded interpretations may overlap. The existing full EHT/diagnostic renderers still need an exhaustive, provenance-aware field/duplicate audit against every protocol field. The shared inventory/fixtures check command and pipeline contracts, not complete show-field coverage.
- The bounded Pixel 9 live input/layout/recreation/Network acceptance is recorded in [acceptance.md](acceptance.md). Compilation alone is not a live-device pass; that measured run does not certify other hardware, all observation fields, clipboard transfer, enabled TalkBack or human readability.
