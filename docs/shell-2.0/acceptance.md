# Shell 2.0 live acceptance

## 最新target SDK 37での実機再確認（2026-11-03）

**指定済みPixel 9 / Android 17（SDK 37）で、最新APKの読み取り専用Shell確認はPASS。** 下の旧target SDK 36の結果を流用せず、第二巡の修正後APKを上書き導入して実行した。確認終了は2026-11-03 08:54 UTC。本番sourceの追加修正は不要だった。

### 導入と確認した環境

- compile SDK 37.2 / target SDK 37 / min SDK 31。OSのsecurity patchは2026-10-05。
- 本体と試験APKの両方を`install -r -t`で更新。`-g`、uninstall、データ初期化、権限の許可・取消は行っていない。両packageのtarget SDK 37を端末から取得し、導入前後のdata-directory inodeが同じであることを確認した。
- 同じversionNameのdirty buildを区別するため、端末のinstalled APKとPCの成果物のSHA-256を照合した。本体: `b0dd5b6f764aa738fc32d458a59daa69516839c6f2376b79432e3ac80a740552`。最終試験APK: `3526ad45c850b6a55c06a3dd0ac7fa3cebe22f38ecb508bbb1e02f8baa8f9a01`。
- 実機の表示は1080x2424、density override 356、font scale 0.85。FINE_LOCATION、NEARBY_WIFI_DEVICES、ACCESS_LOCAL_NETWORK、BACKGROUND_LOCATIONは既存の許可を使用した。権限状態は終了後も同じだった。

### 実測結果

| 確認 | 結果 |
| --- | --- |
| `ShellInputInstrumentation` | PASS。Enter不要help、引用literal、IME composing、cursor suffix、hardware Tab、途中位置の補完。取得commandは0。 |
| `PresentationInstrumentation` | PASS。実Android TextView/Paintの24条件、長い安全値file。さらにART上でJSON・ANSI・10,000回のescapeを含むcredentialの除去3条件、SSID行境界1条件、不正IP/port拒否5条件を確認した。取得commandは0。 |
| `ActualActivityPresentationInstrumentation`、IME非表示 | PASS、11条件。幅・入力サイズ・focus・selection・scroll anchor・copy元を検査。AccessibilityNodeは10条件で確認し、残り1条件はrootがなく未測定。 |
| 同上、IME表示 | PASS、11条件。実際にIMEが表示されたことを計測。AccessibilityNodeは11条件で確認。 |
| `LinkProfileLiveInstrumentation` | PASS。既存Wi-FiのIPv4読み取り2段。wrong-SSIDは`MISSING,SKIP`、別Networkへのfallbackなし。GET_IP_STATUSのみ。 |
| `ShellReadOnlyInstrumentation` | PASS、9群。実InputConnection/IME Go、非同期結果、Network一覧/default/明示ID/stale ID、JSON・パイプ、失敗表示、session credential・履歴、横画面再生成と再生成後の取得。`target_sdk=37 runtime_lan=granted`を実測し、LAN権限表示をplatform状態と照合。Networkは2件。 |
| 実PC PTY + USB経由gRPC | PASS、7群。Enter不要help/Tab、24/40/80/120列、権限JSON、Network一覧/default/明示ID/stale ID。フィルタ後の論理行数は全幅で2。未送信の合成credential draftも上/下で復元されず、設定commandは実行されなかった。 |

instrumentationは初回6回と追加2回の計8回がすべて明示的なPASSと`INSTRUMENTATION_CODE: -1`で終了した。新しいART境界試験は`PresentationInstrumentation.kt`へ、LAN表示の照合は`ShellReadOnlyInstrumentation.kt`へ残した。PC controllerも正常終了した。

### 不足の検出と状態保持

- 現在のWi-Fiの`family all`は`NOT_PASS_WITH_EXPLICIT_FINDINGS`。不足は`ipv6.default_routes`と`ipv6.dns_servers`で、Shellが偽の成功を返さないことを確認した。Wi-FiやDNSを変更してPASSへ変えてはいない。
- 本体・試験packageのデータ初期化はなし。合成PSKはsession内でclearし、実PSKは入力していない。旧平文credential preferenceは最終状態でも不在。
- density、font scale、自動回転1／user rotation 0、端末権限は導入前と一致。横画面指定は試験Activityだけで行い解除した。ADB forward/reverseは開始前・終了後とも空で、無関係なルールの削除やADB server再起動はしていない。
- Wi-Fi変更、新規スキャン、外部プローブは0。広い`make e2e`は実行せず、読み取り専用instrumentationと明示serial指定のPC Shellだけを使用した。
- 追加試験APKのビルド・Android lintは通過し、lint issueは0。単体216試験の既存成功結果も維持した（単体taskはUP-TO-DATE）。

### このPASSに含めない範囲

必要権限が既に許可されている端末なので、新しい権限ダイアログの許可・拒否・再要求は未測定。LANへの実通信、TLS/CT/ECH、clipboard payload転送、TalkBack有効状態、人による読みやすさ、Widget、API 31–36端末、全OEM、全showフィールドの網羅も未確認。実際に通した通信はUSB reverseを使う既存のloopback/plaintext gRPCであり、LAN/TLSのPASSとはしない。

## 旧target SDK 36での実機確認（履歴、2026-11-03）

**PASS for the approved, bounded read-only Shell acceptance scope on one Pixel 9.** This is not a release-wide pass, proof of full `show` field coverage, or a claim that the connected Wi-Fi satisfies both IP families. In particular, `family all` correctly returned a non-pass with explicit IPv6 default-route and DNS findings.

**後続の敵対的QAで、第一巡11件・第二巡8件を修正した。** [qa.md](qa.md)と[qa-round2.md](qa-round2.md)に修正前の根拠と修正後の検査を記録した。Android lintは現在エラー・警告とも0件。さらに[compile SDK 37.2 / target SDK 37へ移行](../android-17-migration.md)したが、以下の実機結果は旧target SDK 36で測った範囲であり、最新APKを再導入・実機確認したという意味ではない。第二巡の修正時には実機へ触れていない。

### Observed environment and installation

- Google Pixel 9; Android SDK 37. Hardware/SDK were read after ADB authorization, not inferred from a device identifier.
- Physical display 1080x2424, physical density 420, existing override density 356, system font scale 0.85. Automatic rotation was 1 and user rotation 0.
- Installed main build: `0.9.2-162-g0c78bb1-dirty`, versionCode 1, targetSdk 36. This is the local unreleased implementation, not a tagged 2.0 release.
- ADB 1.0.41, package 37.0.1-15733141, darwin/arm64. Every device command was pinned to the explicitly approved USB handset; its unique identifier is omitted from this report.
- The user approved replacement installation of the main debug APK and test APK and read-only tests. Main APK replacement succeeded with `-r -t`, without `-g`, uninstall or data clear.
- The preexisting `io.dropcheck.agent.test` had a different signature. After separate explicit consent, **only that test package** was uninstalled and replaced. `io.dropcheck.agent` was not uninstalled. Its credential-encrypted data-directory inode was unchanged before/after the test session.

### Executed checks

| Check | Observed result | Scope / limitation |
| --- | --- | --- |
| `ShellInputInstrumentation` | PASS | Five input paths: no-Enter contextual help, quoted literal question mark, IME composing text, cursor suffix preservation and native Tab key dispatch; zero acquisition commands. |
| `PresentationInstrumentation` | PASS, 24 cases | Real Android TextView/Paint/layout fitting at 24/32/40/48/60/80/96/120 nominal columns and synthetic font-scale contexts 1.0/1.3/2.0; full-safe-value file retention. System font settings were not changed. |
| `ActualActivityPresentationInstrumentation`, IME hidden | PASS, 11 cases | Attached portrait Activity at the actual 0.85 font scale; line-width bounds, unchanged input size, scroll anchor, selection and copy **source** retention. Accessibility node order checked in 10 cases; one had no measurable root. |
| Same Activity check, IME visible | PASS, 11 cases | IME visibility was measured as true; same layout/state assertions. Accessibility node order checked in 10 cases; one had no measurable root. |
| `LinkProfileLiveInstrumentation` | PASS | Existing physical Wi-Fi IPv4 check passed both read-only snapshots; wrong-SSID selection produced `MISSING,SKIP`, not fallback. Initial/link/wrong-target reads were 1/2/1. No mutation or external probe. |
| `ShellReadOnlyInstrumentation` | PASS, 9 scenario groups | Actual InputConnection commit + IME Go, asynchronous production Shell execution and retained safe results; Networks/default/explicit/stale IDs, permissions, JSON, ordered begin/match/count, rejected JSON+count, filtered failure visibility, dual-family findings, session credential privacy, recall history, landscape Activity recreation and post-recreation read. Two Networks were observed; private identities were not emitted. |
| Controller Shell over a real host PTY | PASS, 6 groups | Real readline no-Enter `?` and Tab; serial-pinned live RPCs; permissions JSON and Network pipelines at 24/40/80/120 columns; list/default/explicit/stale Network outcomes. The logical filtered count was **2 at all four widths**. |
| Retired credential preference | PASS for final state | The former `agent-shell-use-defaults` store was absent after the session. No raw preferences or real credentials were emitted. This confirms the final state, not that a real stored credential existed before the test. |

All listed instrumentation runs returned `INSTRUMENTATION_CODE: -1` with their explicit pass report. A process exit status alone was not treated as an instrumentation pass. `ShellReadOnlyInstrumentation.kt` and its test-manifest registration are retained as the live regression test; no production-code fix was needed for the measured scope.

The initial attempt to automate ASCII entry with `adb shell input text` was **not** counted as a product test pass or failure: the active IME transformed the key events into composed characters before submission. The deterministic test instead used the actual input widget's InputConnection and IME editor action, without switching the system IME. IME composition was independently covered by `ShellInputInstrumentation`.

### Expected non-pass: current Wi-Fi IPv6 provisioning

The live Android `check link ... family all` emitted separate findings for all six IPv4/IPv6 address, default-route and DNS metrics. Its result was `NOT_PASS_WITH_EXPLICIT_FINDINGS`; the reported unmet metrics were:

- `ipv6.default_routes`
- `ipv6.dns_servers`

The acceptance assertion passed because these unmet requirements were visible and the Shell did **not** return `ok=true`. No attempt was made to change the Wi-Fi, routes, DNS or permissions to turn this into a provisioning pass. These are observations of the selected physical Wi-Fi at test time, not claims about every Network on the handset or continuous connectivity.

### Preservation and validation

- Main app data was not cleared; only the separately approved conflicting test package was removed. The synthetic session PSK was cleared in the test and was not retained in display/recall history. No real PSK was supplied.
- Final display density 356, system font scale 0.85, automatic rotation 1 and user rotation 0 matched the initial readings. Landscape was requested only for the test Activity and released in cleanup.
- ADB forward/reverse lists were empty before and after the session. The owned PC controller exited normally; no unrelated rules were removed and the ADB server was not restarted.
- Network mutations, fresh scan requests and external probe invocations: **zero**. The broad `make e2e` live runner was intentionally not used because its setup/cleanup can change Wi-Fi.
- Main/test APK builds, the 192 Android unit tests (up-to-date rerun), updated instrumentation compilation and Android lint passed. `git diff --check` passed. Production Go/Kotlin sources were not changed during this acceptance session.

### Not accepted by these tests

Clipboard payload transfer, an enabled TalkBack service, human readability, all OEMs/SDKs, Wi-Fi mutation/active probes, continuous roam/connection guarantees and an exhaustive source-aware audit of **every** `show` field remain outside this measured pass. Native fitting/copy-source checks do not establish those properties.

### Reproduction

Build/install the current main and test APKs on an explicitly authorized handset without data clear or permission grants, and run each named instrumentation with:

```sh
adb -s <approved-serial> shell am instrument -w -r \
  io.dropcheck.agent.test/io.dropcheck.agent.ShellReadOnlyInstrumentation
```

Use the corresponding class names above for the other cases; pass `-e ime false` / `-e ime true` before the component for the attached-Activity matrix. Read-only link/identity checks require an existing physical Wi-Fi and adequate observation permissions; missing prerequisites must not be reported as a pass. Request separate consent before removing a signature-conflicting test APK; never remove the main app to bypass the conflict.

## Earlier preflight history (same date)

The first attempt stopped at `unauthorized`, before installation or live checks. After the user approved USB debugging, a fresh serial-pinned check returned `device`, and the bounded tests above proceeded. The earlier blocked attempt is superseded by the measured results above, not silently counted as a pass.
