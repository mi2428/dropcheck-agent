# Shell 2.0 敵対的QA・第二巡（2026-11-03）

## 現在の修正状況

**後続の実機確認も実施済み。** 最新target SDK 37の本体・試験APKを指定Pixel 9へ上書き導入し、実Shell入力・表示・Network・LAN権限表示・実PC PTY/gRPCを確認した。ART上のcredential・identity・不正endpoint試験も通過。[acceptance.md](acceptance.md)に測定範囲と未測定事項を分けて記録した。以下の「実機へ未導入」は修正完了時点の履歴である。

**8件を修正し、元のGo 12試験・Android 10試験・実readlineの再現はすべて通過した。** 元の期待値と正常系の対照は維持した。Go/Kotlinの独立QAを通常sourceへ移し、overlayとAndroid用init scriptは削除した。追加の反例・race・全静的検査の結果は次表のとおり。実機の未検証範囲は残る。

| 最終検査 | 結果 |
| --- | --- |
| Go全package | Go 1.26.8で通常・raceとも通過。one-shot CLI・TUIの既存回帰を含む。第二巡12試験に追加2試験を統合。Go 1.26.0でも変更した5packageと実PTYを確認した。 |
| native readline | 実PTY 8通りが通常・race付きで通過。危険なdraftの非復元だけでなく、安全なdraft・履歴の復元も確認。child専用Go試験は非TTYの通常`go test`ではskipし、`make test TARGET=controller`が毎回実PTYで実行する。CIのrace段にも同じdriverを追加した。 |
| Android通常試験 | 216試験、失敗・エラー・skipは0。元の第二巡10試験と追加3試験を通常suiteへ統合。 |
| Go静的検査 | vet・staticcheck v0.7.0はGo 1.26.8で通過。modernizeはGo 1.26.0 / 1.26.8 / 1.27.2で差分なし。 |
| Android lint | エラー0件・警告0件。抑制は追加していない。外部Gradle pluginの非推奨API警告は別に残る。 |
| ビルド・設定 | PC binary、Android本体/instrumentation APKがビルド通過。実boot classpathはSDK 37.2。APKのmin 31 / target 37 / LAN権限宣言を確認。インストール・権限付与はしていない。 |
| 整合性 | 整形、共通契約の生成検査、`git diff --check`、新しいGo sourceの整形、Python構文、文書リンク、不要なQA別コピーの不在を確認。 |

| 対象 | 共通原因への修正 |
| --- | --- |
| R2-01 | 引用付きキー、エスケープされた引用符、隣接するcredentialを両側で除去する。 |
| R2-02 | 表示制御文字・ANSIの正規化をcredential検査より先に行う。typed identityにはcredential検査を適用しない。 |
| R2-03 | 反復`errors`もGoの型付き表示境界で除去。元の測定値はclone前のまま維持する。 |
| R2-04 | IDの形式・一意性、要求ID/default、件数、内外ID、収集時刻、availability、errors、複数defaultを検証する。両Shellの成功応答で同じ制約を適用する。正しい空のNetwork一覧は受理する。 |
| R2-05 | AndroidのField/Table/Scanのidentityを単一の論理行へ正規化する。接続結果などの動的なlabel/valueもTextへ連結せずFieldのまま渡す。`password=Guest`のような正当なSSIDは保持する。 |
| R2-06 | native履歴の最初のscratchを空で作り、危険なdraftを履歴移動・検索前と送信後に破棄する。safeな履歴・draftの復元は維持する。 |
| R2-07 | 共通HTTP入力境界で16-bit portを検証する。HTTP/downloadだけでなくWi-Fi cycleとlab入力にも適用する。 |
| R2-08 | IPv4のoctetとIPv6構文を共通operation境界で検証。ping/traceroute/path-mtu/DNS/cycleとtracerouteのviaを含む。名前解決や通信は行わない。 |

## 修正前の結論（履歴）

**第二巡は不合格。前回の試験で検出しなかった8件を再現した。** 新しいGo試験は12件中5失敗、Androidは10件中2失敗、実readlineのPTY試験は1件中1失敗。試験単位と問題単位は異なり、同じ問題の両言語での再現は重複して数えていない。

一方、既存のGo通常・race試験、Android通常203試験、modernize・vet・staticcheck・Android lintは引き続き通過した。既存試験や静的検査の通過を、今回の不合格の代わりにはしない。

この巡回で変更したのは独立QA用の試験・実行script・文書だけ。本番source、通常試験の期待値、依存、端末の状態は変更していない。新たな8件は未修正である。

## 試験をどう入れ替えたか

「殺虫剤のパラドックス」を避けるため、前回の21試験を再実行するだけでなく、観測点と判定方法を変えた。

| 前回の中心 | 第二巡の追加観点 |
| --- | --- |
| `password=value`という単純な文字列 | JSON引用付きキー、ANSIで分割したキー、percent-encoded URLキー、URL user-infoを比較。 |
| 単一Fieldのsanitization | `errors`という反復フィールドの3payload、ADB textから安全JSONまでの連続経路。 |
| Androidの履歴model | PCの本物のreadlineをPTYで駆動。Androidも100件上限の前後、未完了引用、履歴を往復した後の状態を検査。 |
| payloadの型・Network ID入力 | 正しいpayload型なのにID・件数・内側ID・errorsが不整合な応答を投入。実際のShell JSONの`ok`まで検査。 |
| 正常なSSID文字列 | SSIDに改行と偽の`Source:`行を入れ、フィルタが独立したsource行と誤認しないかを比較。 |
| 少数の入力例、例外なしの無作為入力 | 両言語で同じ91組のPSK・数値・BSSID・endpoint境界を検査。受理・拒否まで判定。 |
| 固定したpipeline出力例 | 0～256論理行の257入力で、フィルタの反復不変性、match/exceptの行数保存、Androidのtext/block経路の一致を検査。 |
| 実行中のActivity再接続・cancel | listener不在の間に実行が完了する順序へ変更し、再接続後の一度だけの配送・重複開始の拒否を検査。 |
| 通常サイズの安全な値 | 絵文字・日本語を含む長い値のfile往復と、履歴から追い出した時のprivate file削除。 |

各91組の内訳はPSK 44、数値とoption順序16、BSSID 5、endpoint 26。`set default passphrase ''`は既存のclear動作として扱い、`use ... passphrase ''`は拒否する期待値を両側で確認した。境界や性質の検査は、同じ実装の結果をそのまま正解として保存する方式ではない。

## 修正前に再現した8件

以下の結果・行番号は検出当時の記録であり、現在のsourceが未修正という意味ではない。

### R2-01 / P1 / 両Shell: JSON形式の資格情報が伏字化を抜ける

- 入力: `{"password":"round2-synthetic-secret"}`をADB Stdout、またはAndroidの汎用診断Fieldへ入れる。
- 結果: 安全な表示JSONにも合成秘密文字列が残る。percent-encoded URLキーとURL user-infoの対照例は除去されるため、URL全般が壊れているという主張ではない。
- 原因: Goの`controller/internal/render/safe_text.go:13`とAndroidの`agent/src/main/java/io/dropcheck/agent/AgentBlockPresentation.kt:92`は、引用符付きのキーと区切りを認識しない。
- 区別: 元のsanitizerにもある未解決の弱点。前回の修正が守った「単純なkey=value」と異なる表現で、安全JSONの前提を破る。
- 試験: `TestRound2UntrustedCredentialEncodings/json`、`credentialEncodingsCannotBypassMetadataSanitizer`。

### R2-02 / P1 / 両Shell: ANSI除去の後で資格情報キーが復活する

- 入力: `pass\u001b[31mword=round2-synthetic-secret`。ここで`\u001b`は試験内では実際のESC文字。
- 結果: 最初の資格情報検査ではキーと認識されず、その後のANSI除去で`password=...`が復元され、秘密文字列が表示JSONへ残る。
- 原因: Goの`safePresentationText`から`TextBlock`／`cleanDisplayCell`へ進む順序、Androidの`AgentSafePresentation.text`から`clean`へ進む順序。
- 区別: 既存の正規化順序の不足。前回と同じneedleを使っても、needleが見える時点を変えると検出できる。
- 試験: `TestRound2UntrustedCredentialEncodings/ansi-key`、上記Android試験の`ansi-key`。

### R2-03 / P1 / PC: 反復する診断errorsをsanitizerが見落とす

- 入力: `WifiScan.errors`、`NetworkSnapshot.errors`、`WifiCapabilities.errors`に、それぞれ`password=...`、`authorization=Bearer ...`、`psk=...`の合成文字列を設定。
- 結果: 3payloadとも対話用textに秘密文字列が残る。
- 原因: `controller/internal/render/safe_text.go:84`のフィールド判定は単数の`error`を扱うが、反復フィールドの`errors`を扱わない。`controller/internal/render/render.go:1759`の表示側も除去しない。
- 区別: 元の共通sanitizerにもある不足。新しいNetwork応答にも同じ弱点が及ぶ。前回の「汎用Fieldを守る」修正だけでは別の型の入れ物を守れない。
- 試験: `TestRound2RepeatedErrorsAreSanitized`。

### R2-04 / P1 / PC: 正しい型の誤target応答を成功として受理する

- 入力: Network 42を要求したcommand IDに、Network 43の`NetworkSnapshot`を返すfake RPC応答。
- 結果: 本番の`runCommandForAgent`を通した表示JSONが`ok=true`になる。共通executorでも、空snapshot、外側IDと内側IPのID不一致、`errors`付きなのにSTATUS_OKという4状態を受理する。整合したNetwork 42の応答は正しく受理される対照試験もある。
- 原因: `controller/internal/harness/operation.go:122`の`payloadMatches`はGetNetworkとGetNetworksを同じpayload型だけで判定する。requestのID、返却件数、内外のsubject、partial failureとの整合性を確認しない。
- 区別: 新しいNetwork機能と既存の型だけの検査の組み合わせによる契約不足。実機が実際にNetworkを切り替えたという報告ではなく、矛盾した応答に対してfail-closedにならないことを再現した。
- 試験: `TestRound2NetworkResponseIdentityAndConsistency`、`TestRound2WrongNetworkIsNotSuccessfulShellJSON`。

### R2-05 / P2 / Android: SSID内の改行が独立したsource行に化ける

- 入力: `AgentBlockPart.Field("SSID", "Lab\nSource: forged")`へ`match '^Source: forged$'`を適用。
- 結果: `Source: forged\n`が残る。Go側の対応するSSID表示は、この独立行を作らず試験を通過する。
- 原因: `AgentBlockPresentation.kt:28`のfullText化と、同ファイル`:131`の改行保持を組み合わせると、単一identityの値とlogical recordの境界が区別できなくなる。
- 区別と限界: typedな`ok`が変わったり、コードが実行されたりするわけではない。copy・論理行フィルタ上でデータからsource表示を偽装できる。単体の表示境界への合成入力であり、実APから同じSSIDを受信する実機試験は行っていない。
- 試験: `identityCannotForgeLogicalSourceRecord`。Goの対照は`TestRound2IdentityCannotForgeLogicalSource`。

### R2-06 / P1 / PC: 本物のreadlineが未送信の資格情報draftを復元する

- 手順: 本番`replLineEditor`をPTYで開始し、`show version`を一度送信。次に`set default passphrase round2-synthetic-secret`を送信せず入力し、上・下の履歴操作後にEnterを押す。
- 結果: 未送信draftが復元され、セッションのdefault passphraseが設定される。本番の状態を確認するGo assertionも失敗する。
- 原因: `controller/internal/app/repl.go:38`の`DisableAutoSaveHistory`と送信後の`HistorySafe`は、readline v1.5.1が編集中の内容を`history.Update`に保持する処理を止めない。
- 区別と限界: 前回修正したAndroid sessionとは別の入力backend。PCで安全な送信済み履歴を追加したことと、既存libraryのdraft保持の組み合わせを見落としていた。ディスク保存や外部流出ではなく、履歴による資格情報の再提示・保持を再現した。
- 試験: `TestPTYRound2NativeHistory`と、現在は`tools/test_shell_history.py`へ移したPTY driver。本番line editorを直接使い、ADB・agent・端末への呼び出しは0。最初の「fake ADBで端末なし起動」は起動前提不足で停止し、製品の失敗件数には含めていない。

### R2-07 / P2 / PC: HTTP URLのport上限を検証しない

- 入力: `http https://example.test:65536/`、`download https://example.test:2147483648/`など。
- 結果: PCは受理し、Androidは拒否する。443・65535は両側が受理する対照例。
- 原因: `controller/internal/command/validate.go:84`のendpoint検証はscheme・hostname・user-infoを調べるが、portの数値範囲を調べない。
- 区別と限界: 既存Go validationの不足で、最新SDK移行による新規回帰とは断定しない。今回、接続・外部プローブは実行していない。
- 試験: 両言語の`ProbeEndpointTrustBoundaries`に対応する試験。

### R2-08 / P2 / PC: 不正なIP literalの受理がAndroidと異なる

- 入力: `999.0.0.1`、`256.1.1.1`、`2001:db8:::1`をping・traceroute・path-mtuへ渡す9組。
- 結果: PCはすべて受理し、Androidは拒否する。通常のIPv4・IPv6・hostnameは両側とも受理する対照例。
- 原因: `controller/internal/command/validate.go:28`は空文字・先頭ハイフン・一部の空白等だけを検証し、不正IP literalを排除しない。
- 区別と限界: 元のGo validationから残る共通契約の不一致。入力をOS shell経由で実行したという話ではなく、コマンド注入や実際の通信は主張しない。
- 試験: 同上。拒否の差をparser境界だけで比較した。

## 今回通過した新しい検査

- 両言語のPSK byte数・制御文字44組、数値とoption順序16組、canonical BSSID 5組。疑ったMAC形式の差は、Goの後段validationも含めると再現せず棄却した。
- pipelineの16/17段、512/514 UTF-8 bytesの境界。257の異なる論理行入力で、4種類のフィルタの反復不変性とmatch/exceptの行数保存。Androidはblock版とtext版の一致も確認した。
- Android履歴の130件登録、100件上限を越えた往復、資格情報付き・未完了のdraftを復元しないこと。
- Androidのlistener不在中に完了した結果を、新listenerへ一度だけ渡すこと。配送待ちの重複開始も拒否した。これは合成post queueの順序試験であり、実機の回転・UI threadの全競合を保証しない。
- 長い絵文字・日本語の安全値file往復と、履歴eviction時のprivate file削除。clipboardへの実転送ではない。
- 無効なpipelineではsession credentialが変わらず、複数targetの`use`は状態変更・実行より前に拒否されること。

## 修正前の通常回帰・静的検査・SDK確認（履歴）

| 検査 | 結果 |
| --- | --- |
| Go通常全package / race全package | 両方通過。前回の敵対的11試験、one-shot CLI・TUI・harnessの既存試験を含む。第二巡の独立overlayは含めない。 |
| Go modernize | Go 1.26.0 / 1.26.8 / 1.27.2で差分なし。 |
| vet / staticcheck | CIに合わせたGo 1.26.8、staticcheck v0.7.0で通過。 |
| Android通常試験 | 203試験を再実行して通過。前回の敵対的10試験を含み、第二巡classは含めない。 |
| Android lint | 正常終了。最終XMLのissueは0件、エラー・警告とも0。外部Gradle pluginの非推奨警告は引き続き別件。 |
| 整形・共通契約・文書 | `make fmt-check`、`make check-shell`、`git diff --check`、QA scriptの構文、文書リンクと8件のfinding IDを確認して通過。 |
| Android本体 / instrumentation APK | ビルド通過。実機へ再導入していない。 |
| SDK・権限 | lint modelでboot classpathとcompileTargetが`android-37.2`であることを確認。APKのmin SDK 31、target SDK 37、LAN権限宣言も確認。31/32/33/36/37の既存の権限一覧試験は通常suiteで通過。 |

権限要求ダイアログの拒否・復帰・rotation、SDK 37端末の実LAN通信・TLS・clipboard・TalkBackは今回も未実行。sourceやmanifestの確認だけで実機PASSにはしていない。本体データ・Wi-Fi設定・端末権限の変更、新規スキャン、外部プローブは0。

## 現在の回帰試験の実行方法

リポジトリのルートから実行する。元の失敗を検出した試験を通常実行へ統合した。

```sh
GOTOOLCHAIN=go1.26.8 make test TARGET=controller
make test TARGET=agent
GOTOOLCHAIN=go1.26.8 GOFLAGS='-race -p=1' python3 tools/test_shell_history.py
```

- Goは各`controller/internal/{app,command,harness,pipeline,render}/pesticide_round2_test.go`、Androidは`agent/src/test/java/io/dropcheck/agent/PesticideRotationQaTest.kt`に統合した。別コピーやoverlay経路は残さない。
- PTY driverは`tools/test_shell_history.py`。`make test TARGET=controller`から毎回実行し、CIも同じ入口を使う。通常の`go test`だけではTTYがないためchild専用試験をskipし、このdriverが実PTYで実行する。
- PTYは出力文字列だけでなく、セッション状態のassertionとGo試験の完了も確認する。ADB・agent・実機は使わず、端末なし起動用のstubを本番へ追加しない。

追加の反例は引用符のエスケープ、10,000回のエスケープを含む長いcredential、ANSIのOSC形式、正規化したField名、重複ID・複数default・欠落IP、Field/Table/Scanのidentityなど。Androidのcredential検査にも既存のRE2/Jを再利用し、長い入力でJava正規表現の再帰を増やさない。入力拒否は共通fixtureへも追加し、cycle・lab・DNS・viaという別の呼び出し元を検査した。

履歴は矢印だけでなくCtrl-P/N、履歴編集、送信済みcredential、検索、割り込み、安全なdraft/履歴も実PTYで検査した。途中でrace付き実行の検索caseが不安定になったが、driverが次のraw-mode promptより前にキーを送っていたことを記録出力で確認した。結果だけを見て製品不具合とせず、次のpromptを待つようdriverを直し、同じ期待値で再実行した。Go側の状態・出力回数・race警告の不在・child processの正常終了まで確認する。この集合も完全性の証明ではなく、次の巡回では別の競合順序・データ表現・反例へ入れ替える。
