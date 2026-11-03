# Shell 2.0 敵対的QA（2026-11-03）

## 現在の結果: 第一巡11件・第二巡8件を修正済み（2026-11-03）

**第二巡で追加再現した8件も修正した。** 元のGo 12試験、Android 10試験、実readlineの再現は期待値を変えずに通過し、通常実行へ統合した。[第二巡の報告](qa-round2.md)に根本修正と追加の反例を記録した。さらに最新target SDK 37でも指定Pixel 9の[読み取り専用実機確認](acceptance.md)を通過した。全showフィールド・全端末や未測定の権限ダイアログまで完了したという意味ではない。

## 第一巡の修正後の結果（履歴）

**報告した11件は修正済み。** Goの敵対的11試験、Androidの敵対的10試験はすべて通過し、通常の回帰試験へ統合した。Android通常suiteは203試験、失敗・エラー・skipは0。Go全packageの通常試験・race試験、vet・staticcheck、Go 1.26.0 / 1.26.8 / 1.27.2のmodernize検査も通過した。

Android lintは**エラー0件・警告0件**。警告を抑制せず、文字列resource化・入力編集APIの修正・SDKと依存の更新で解消した。今後は`warningsAsErrors=true`で新しい警告も検査を失敗させる。compile SDK 37.2 / target SDK 37への移行と未検証の実機範囲は[SDK移行記録](../android-17-migration.md)を参照。Gradleの外部Kotlin/Protobuf pluginに由来する非推奨API警告は別途残り、ビルド全体が警告なしという意味ではない。

| 対象 | 修正内容 |
| --- | --- |
| QA-01 | ADBのtext表示境界で既存sanitizerを適用。安全JSON・フィルタへ渡る前に資格情報を除去。生の取得bundleと明示的なone-shot raw JSONは変更しない。 |
| QA-02 | 既知のidentity項目以外のFieldを既存の資格情報sanitizerへ渡す。 |
| QA-03 | draftにも通常履歴と同じread-only許可判定を適用。 |
| QA-04 | ADB bundle全体のtyped outcomeから`ok`を決定。フィルタ後も失敗を標準エラーへ表示。 |
| QA-05 | lexerのtoken終端まで置換。PCはreadlineの挿入APIとlistener、AndroidはEditableの範囲置換を使用。引用literalは補完しない。 |
| QA-06 | textフィルタの変換後に`Command FAILED (output filtered)`を保持し、copy・読み上げ元にも残す。 |
| QA-07 | 両Shellのscan detailへoperator・venueを追加。 |
| QA-08 / QA-09 | PSKを空白でなく専用の長さ・文字検証に渡し、checkの`ssid`も共通の一意prefix解決へ統一。 |
| QA-10 | builderのdetail指定をparserで保持。REPLにあったcommand名による補正は削除。 |
| QA-11 | block化時の改行除去を削除し、空行1行と0行を区別。 |

元の再現試験の正しい期待値は維持した。Androidの途中位置補完試験だけは、本番と同じtoken終端の範囲置換を呼ぶように操作部分を更新し、期待する`show wifi status`は変えていない。通常試験の旧「無効なdraftを復元」「失敗blockが空文字」という期待は、安全な新仕様へ変更した。PCにはreadlineの挿入とlistenerの連続動作を確認する試験、ADBには部分失敗・空bundle・timeout・raw JSON不変の試験も追加した。

通常suiteの試験sourceは`controller/internal/{app,command,render}/adversarial_qa_test.go`と`agent/src/test/java/io/dropcheck/agent/AdversarialShellQaTest.kt`。一時overlay・Gradle init script・試験sourceの別コピーは削除した。未使用のAndroid旧status表示・旧Network表示helper・MLO helperも削除し、既存表示試験は本番のpresentation経路へ移した。

今回の修正・SDK移行では端末操作・APK再導入・権限変更・Wi-Fi変更・新規スキャン・外部プローブは行っていない。最新APKのmanifestではmin SDK 31、target SDK 37、`ACCESS_LOCAL_NETWORK`の宣言を確認した。過去の限定実機PASSを最新target SDKの実機PASSとして扱わない。

## 初回QAの結論（修正前の記録）

**敵対的QAは不合格。** Go 11試験中5失敗、Android 10試験中8失敗。両側で重複する問題をまとめて11件を記録した。通常試験や限定実機試験の通過は、この不合格を取り消さない。

`go modernize`相当の検査、vet、staticcheck、整形、生成物の整合性検査は通過した。Android lintも正常終了したが、**エラー0件・警告11件であり、警告なしではない**。機能不具合は未修正。今回適用した修正はGo modernizerの機械的変更と検査入口・CIの追加だけで、QAの期待値を不具合に合わせて変更していない。

### 対象と安全境界

- 未コミットのShell 2.0差分を対象とし、PCの対話入力から実行・表示まで、Androidのparser・セッション・表示block・pipelineを検査した。
- 合成データ、fake ADB、ローカルRPC、単体試験だけを使用した。このQAでは端末操作、APK導入、データ初期化、Wi-Fi変更、新規スキャン、外部プローブを行っていない。Gradleの通常の依存解決は別である。
- 秘密文字列はすべて`qa-synthetic-secret`という合成値。実際の資格情報漏えいを観測したという報告ではない。
- `HEAD`との差分と元コードを比較し、新規回帰、追加経路の欠陥、既存未解決の要件不達を区別した。既存コード全体を過去の環境で実行し直したわけではない。
- 重要度はP1＝秘密情報・成功判定の境界、P2＝操作・表示・共通仕様の不成立、P3＝限定された内部契約の不整合とする。

## 初回に再現した問題（下記は修正前の動作・行番号）

### QA-01 / P1 / PC: ADBの生出力が安全JSONとフィルタへ残る

- 再現: ADBの`Stdout`に`password=qa-synthetic-secret`を持つbundleを表示し、`match qa-synthetic-secret`または`display json`を適用する。どちらにも秘密文字列が残る。
- 根拠: `controller/internal/adbdiag/diag.go:164`は生出力をそのまま書き出す。`controller/internal/app/pipeline_bridge.go:31`は、すでに安全な値を受け取ったものとしてレイアウト・フィルタ・JSON化するだけである。
- 区別: ADBの生出力保持は既存の性質。今回の「安全な表示JSON」へ無条件で接続した経路は新しい。SSID等の正当な文字列まで一律に削るのではなく、ADB取得元の境界で安全性を確定する必要がある。
- 試験: `TestAdversarialQARawADBSecretBoundary`。

### QA-02 / P1 / Android: 汎用metadataのFieldで資格情報を除去しない

- 再現: `AgentBlockPart.Field("fresh_scan_error", "password=qa-synthetic-secret")`を`AgentPresentationBlock.create`へ渡すと、`fullText`に秘密文字列が残る。
- 根拠: `agent/src/main/java/io/dropcheck/agent/AgentBlockPresentation.kt:141`は特定のラベルだけに資格情報の除去を適用する。未知のmetadataラベルは制御文字・URLの処理だけになる。scanの汎用metadataは`AgentWifiScanRenderer.kt:52`から同じField境界に渡る。
- 区別と限界: ラベルに依存する処理は既存の弱点。今回のフィルタ・JSON・copy経路も同じ安全性の前提を使う。試験はField境界への合成入力であり、実機が`fresh_scan_error`というキーや同じ秘密文字列を実際に生成することは確認していない。
- 試験: `typedSafeMetadataCannotLeakDiagnosticCredentials`。

### QA-03 / P1 / Android: 履歴のdraftに資格情報付き未送信入力を保持する

- 再現: 安全な履歴`show permissions`を登録し、`wifi connect Lab passphrase qa-synthetic-secret`の編集中にPrev、続いてNextを使うと秘密付き入力が戻る。
- 根拠: `agent/src/main/java/io/dropcheck/agent/AgentShellInput.kt:56`の`recall`は、`remember`の許可判定と異なり、`current`を無条件で`draft`へ保存する。Activityも`MainActivity.kt:850`でこの値を入力欄へ戻す。
- 区別: 今回追加した履歴状態の欠陥。送信後の履歴除外だけでは未送信のdraftを守れない。ディスク保存や実際の外部送信が起きたという意味ではない。
- 試験: `historyDoesNotRetainCredentialDraftAcrossRecall`。

### QA-04 / P1 / PC: ADB取得が全失敗でもJSONが`ok=true`

- 再現: fake ADBが標準エラーへ`permission denied`を出してexit 7を返す状態で、`show adb cmd wifi status | display json`に相当する本番実行関数を呼ぶ。JSONの`ok`がtrueになる。
- 根拠: `controller/internal/adbdiag/diag.go:125`は個別エラーをbundleへ格納し、`Collect`自体はnil errorを返す。`controller/internal/app/repl.go:522`はその後`true`を固定で渡す。
- 区別: 部分失敗を保持する収集方式は既存。そこから新JSONの成功を固定する部分が新しい欠陥。出力行にエラーが含まれることは、誤った`ok`の代わりにはならない。
- 試験: `TestAdversarialQAFailedADBMustNotBeJSONSuccess`。

### QA-05 / P2 / 両Shell: 単語途中のTabで後半を二重挿入する

- 再現: `show wi|fi status`の`|`位置をカーソルとしてTab補完すると、`show wifi fi status`になる。
- 根拠: PCは`controller/internal/app/repl.go:111`でカーソル前だけから追加文字を作る。Androidは`agent/src/main/java/io/dropcheck/agent/MainActivity.kt:835`で前半の置換候補に元の後半をそのまま連結する。いずれも現在の単語の終端を置換範囲に含めていない。
- 区別: PCは元コードでは途中位置の補完を拒否していたため新規回帰。Androidは今回追加した補完の欠陥。再現は編集adapterと同じ連結処理の試験であり、今回実機やPTYを再操作した結果ではない。
- 試験: `TestAdversarialQAMidTokenCompletion`、`midTokenCompletionCannotDuplicateSuffix`。

### QA-06 / P2 / Android: `count`後のcopy・読み上げ元から失敗表示が消える

- 再現: `ok=false`、本文`operation failed`の結果へ`count`を適用すると、`ok=false`は残るが、`block.fullText`は件数表示だけになる。
- 根拠: `agent/src/main/java/io/dropcheck/agent/AgentShellPipeline.kt:26`は失敗情報を本文と一緒に変換する。`MainActivity.kt:118`は空でないblockをそのまま表示し、コピーも`block.fullText`を使う。
- 区別と限界: 今回追加したpipeline経路の欠陥。色とtyped outcomeは保持されるため「成功に変わる」とは主張しない。失敗が色だけに残り、copy・読み上げ元の文字では判別できないことを再現した。実際のTalkBack音声・クリップボード転送は未確認。
- 試験: `filteringCannotReduceFailureToOnlyACountForCopyAndAccessibility`。

### QA-07 / P2 / 両Shell: scan detailから取得済みのoperator・venue情報へ到達できない

- 再現: scan resultの`operator_friendly_name=qa-operator-name`と`venue_name=qa-venue-name`を設定してdetail表示しても両値が現れない。
- 根拠: PCの`controller/internal/render/render.go:1014`、Androidの`AgentWifiScanRenderer.kt:38`と`AgentWifiStatusRenderer.kt:152`で両フィールドを選択していない。取得側の`WifiProtoMapper.kt:154`には両値の収集がある。
- 区別: 元コードにも同じ表示不足がある。今回新しく失った値ではなく、「安全に取得可能なshow情報を網羅する」という要件の未達を具体的に示すもの。これが欠落フィールドの全件一覧という意味ではない。
- 試験: `TestAdversarialQAScanDetailPreservesSafelyCollectedVenue`、`safeVenueFieldsAreReachableInScanDetail`。

### QA-08 / P2 / Android: 8スペースのPSKを共通仕様と異なり拒否する

- 再現: `wifi connect Lab passphrase '        '`をPCは受理し、Androidは拒否する。引用内はASCIIスペース8個であり、空文字ではない。
- 根拠: `agent/src/main/java/io/dropcheck/agent/AgentInteractiveShell.kt:278`で、PSK長の専用検証より先に`isNotBlank()`を要求する。元コードのconnect/cycle専用処理はPSKを直接`validPassphrase`へ渡していた。
- 区別: Android parserの新規回帰。parser境界の差であり、実機接続の成功を確認した試験ではない。
- 試験: `requiredWhitespacePskMatchesPcBoundary`、PCの基準は`TestAdversarialQALiteralPSKAndPrefixBaseline`。

### QA-09 / P2 / Android: `check link`の一意な`ssid`省略形を拒否する

- 再現: `check link s Lab`をPCは受理し、Androidは拒否する。
- 根拠: `agent/src/main/java/io/dropcheck/agent/AgentInteractiveShell.kt:212`が最初の`ssid`だけを完全一致で要求する。後続optionや通常の共通構文と異なる。
- 区別: この完全一致判定は元コードにもある。新規回帰ではなく、共通契約へ置き換えた後も残る既存の不一致。
- 試験: `uniqueCheckKeywordPrefixMatchesPcBoundary`、PCの基準は`TestAdversarialQALiteralPSKAndPrefixBaseline`。

### QA-10 / P3 / PC: parserがNetwork builderのdetail指定を消す

- 再現: `show network default`をparseすると、`NetworkOperation`が指定した`Options.Detail=true`がfalseになる。
- 根拠: `controller/internal/command/network_operations.go:22`に対し、`controller/internal/command/grammar.go:509`が存在しない`detail`スイッチの値で上書きする。
- 区別と限界: 新Network operationと既存の一律上書きの組み合わせによる内部契約の不整合。対話REPLは`op.Name == "network.show"`でdetailを別途選ぶため、今回の試験は実機のNetwork detail欠落を証明しない。builderの意図がparserで保持されない点を検出した。
- 試験: `TestAdversarialQARenderDetailIntent`。

### QA-11 / P3 / Android: 空行だけの結果をblockへ保持すると論理行が失われる

- 再現: `a\n\nb\n`へ`match '^$'`を適用すると文字列版は`\n`を返す。結果blockを経由するとその改行が除去され、続けて別pipelineでcountした値が1ではなく0になる。
- 根拠: `agent/src/main/java/io/dropcheck/agent/AgentShellPipeline.kt:30`が終端改行を除去し、空行1行と0行を同じ空文字にする。
- 区別と限界: 新pipeline/block間の境界の欠陥。**一度blockへ保持してから別pipelineへ渡す合成ケース**であり、通常の単一コマンド内の`match ... | count`全般が壊れるという主張ではない。
- 試験: `finalBlankLogicalLineSurvivesFilterComposition`。

## 追加観点で問題を再現しなかった範囲

- Go・Androidそれぞれseed `20261103`で2000入力。parser、補完、context helpにpanic・例外なし。両言語の乱数系列は同一ではなく、同じ入力集合を比較した試験ではない。無作為入力は有効・無効を問わず処理し、受理結果の正しさまでは判定しない。
- PCのagent見出し付き安全JSONはagent identityを保持した。ただし合成接続1台の見出し付き出力であり、複数実機同時broadcastを再検証したものではない。
- PCの空の必須literal拒否、`check link`後のSSID補完、bare `check lab`、Androidの資格情報付き引数helpは今回の追加試験を通過した。
- 通常のGo全packageとrace試験は通過。one-shot CLI、TUI、harnessの既存回帰試験も含むが、未試験の動作まで無回帰と断定しない。

## 初回の静的検査・通常回帰の結果（履歴）

| 検査 | 結果と範囲 |
| --- | --- |
| `GOTOOLCHAIN=go1.26.0 GOFLAGS=-p=1 make modernize-check` | 通過、差分なし。CIの下限toolchainも確認。 |
| 同上、Go 1.26.8 / 1.27.2 | 両方通過、差分なし。 |
| `GOTOOLCHAIN=go1.26.8 GOFLAGS=-p=1 make lint TARGET=controller STATICCHECK=<v0.7.0-binary>` | vet・staticcheckとも指摘なし。ローカルGo 1.27のexport形式とはtoolchainを分けた。 |
| `make fmt-check` | 通過。未追跡ファイルを除外する既存targetの範囲を補うため、生成Go以外の全195ファイルへ直接`gofmt -l`も実行し、出力なし。 |
| `make check-shell` | Go/Kotlinの共通契約生成物に差分なし。 |
| protobuf生成物 | pinned protoc 35.0・既存の固定版Go pluginで別の一時フォルダへ生成し、`control.pb.go`・`control_grpc.pb.go`のbyte完全一致を確認。作業中ファイルを上書きしていない。 |
| `git diff --check` | 通過。 |
| `make lint TARGET=agent` | 正常終了。ただしlint XMLはエラー0件・警告11件。内訳は下記。 |
| `GOTOOLCHAIN=go1.26.8 GOFLAGS=-p=1 make test TARGET=controller` | 全package通過。最終Go変更後のrace版も全package通過。 |
| `GOTOOLCHAIN=go1.26.8 GOFLAGS=-p=1 go test -race -count=1 ./...`（`controller/`） | 全package通過。 |
| `make test TARGET=agent` | XML合計192試験、失敗・エラー・skipすべて0。敵対的QAの一時source追加なしの通常suite。 |
| native build / instrumentation build | 両方の通常buildと`:agent:assembleDebugAndroidTest`は通過済み。今回のQAで端末へ再導入していない。 |

Android lintの警告11件は、`OldTargetApi` 1件、`AndroidGradlePluginVersion` 1件、`NewerVersionAvailable` 4件、`SetTextI18n` 5件。前10件の指摘元は`HEAD`にも同じコードが存在する。新規の1件は`MainActivity.kt:842`の補完入力連結に対する`SetTextI18n`である。更新案内が指す各版の採用可否は検討しておらず、無関係な依存更新や警告の抑制で結果をきれいに見せていない。したがって、既存設定の静的検査コマンドは通過したが、**警告を含めた完全な指摘ゼロは未達**である。Gradleには別途`android.useAndroidX=false`の非推奨警告も残る。

modernizerは[Go 1.26の公式変更](https://go.dev/doc/go1.26#go-command)とローカルの`go help fix`で確認した`go fix -diff ./...`を使用した。`Makefile`の`modernize-check`と`.github/workflows/ci.yml`の両Go matrixへ検査を追加した。CI自体は未pushのため実行しておらず、Ubuntu上での成功を主張しない。変更した`SplitSeq`、`FieldsSeq`、`CutPrefix`、`new(expr)`、`t.Context`、`WaitGroup.Go`等は機械的なmodernize対応である。長いmetric名一覧も元コードと完全一致を検査した。

## 再現方法

リポジトリのルートから実行する。現在は両コマンドとも成功終了する。通常の`make test`にも同じ試験が含まれる。

```sh
GOTOOLCHAIN=go1.26.8 python3 docs/shell-2.0/qa/reproduce.py go
python3 docs/shell-2.0/qa/reproduce.py android
```

- Goは3packageの保持したQA試験だけを実行する。11試験すべて通過。
- Androidは通常source setのQA classを指定する。10試験すべて通過。通常のJUnit XMLをこの選択実行の結果で置き換えるため、全suiteの結果を取り直す場合は続けて`make test TARGET=agent`を実行する。
- [qa/reproduce.py](qa/reproduce.py)は対象試験を選ぶだけで、source setや試験の期待値を書き換えない。

限定実機試験の内容と未検証範囲は[acceptance.md](acceptance.md)を参照。実機で測った通過範囲は有効だが、Shell 2.0全体の受け入れを完了したものとして扱わない。
