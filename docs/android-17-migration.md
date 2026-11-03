# Android 17 / SDK 37.2への移行（2026-11-03）

## 採用した安定版

ユーザーの2.0向け移行指示に従い、previewや警告抑制による回避ではなく、最新安定SDKへ移行した。確認日は2026-11-03。

| 対象 | 採用値 | 一次資料 |
| --- | --- | --- |
| compile SDK | 37.2、revision 2 | [Google SDK repository](https://dl.google.com/android/repository/repository2-3.xml)の`platforms;android-37.2`。channel-0、codename空、previewなしを確認。beta・canaryの名前だけを見て安定版と判定していない。 |
| target / min SDK | 37 / 31 | [Android 17のtarget変更](https://developer.android.com/about/versions/17/behavior-changes-17)を確認。min SDKは無断で切り上げない。 |
| AGP | 9.5.0 | [Google Maven metadata](https://dl.google.com/dl/android/maven2/com/android/tools/build/gradle/maven-metadata.xml)、[互換性表](https://developer.android.com/build/releases/agp-9-5-0-release-notes)。metadata更新2026-10-28、Gradle最低9.6.0、JDK 17。 |
| Gradle | 9.8.2 | [公式current情報](https://services.gradle.org/versions/current)、配布zipの公式SHA-256。wrapperのproperties・jar・scriptsも正規taskで更新。 |
| Build Tools | 37.0.0 | Google SDK repositoryの安定版一覧。 |
| gRPC Java | 1.85.1 | [Maven Central metadata](https://repo.maven.apache.org/maven2/io/grpc/grpc-okhttp/maven-metadata.xml)、更新2026-10-20。[release記録](https://github.com/grpc/grpc-java/releases/tag/v1.85.1)とPOMも確認。 |
| Protobuf Java / compiler | 4.36.2 | [Maven Central metadata](https://repo.maven.apache.org/maven2/com/google/protobuf/protobuf-javalite/maven-metadata.xml)、更新2026-10-15。4.37.0-RC1は採用しない。Go側の固定generatorは無関係に変更しない。 |

JDK 17、built-in Kotlin、既存のProtobuf Gradle plugin 0.10.0を使用する。独自のgeneratorや互換shimは追加しない。Gradle 9.8.2の`Configuration.setVisible`に対し、外部Kotlin/Protobuf pluginから非推奨警告が出ることを`--warning-mode=all -Dorg.gradle.deprecation.trace=true`で確認した。これはアプリのlint警告ではないが、ビルド全体の警告ゼロは未達である。警告を無効化したり、依存cacheを書き換えて消したりしていない。

## API・権限の移行

- [Local network permission](https://developer.android.com/privacy-and-security/local-network-permission)に従い、`ACCESS_LOCAL_NETWORK`をmanifestへ宣言。SDK 37以降だけruntime要求へ含める。ActivityとNetworkRepositoryの権限表示は同じ一覧を使用し、31/32/33/36/37の境界を単体試験で確認する。名前の一覧はJVMで試験可能な純粋な文字列データであり、新API呼び出しやlint抑制を含まない。
- 権限拒否時の連続した自動要求を止め、要求済み状態をActivity再生成でも保持する。観測・診断は引き続き利用可能であり、OSが拒否する通信を別Networkや暗黙の許可で迂回しない。読み取り専用の`show permissions`で拒否状態を確認できる。
- Android 17のCT・ECH等のTLS変更はplatformの既定に従う。安全性を下げるopt-out、独自TrustManager、証明書検証の無効化は追加しない。既存のADB reverse用gRPC loopback/plaintext構成は変更していない。後続の指定端末での再試験は通過したが、実LAN/TLS接続の検証ではない。
- Widgetの新しいbitmap予算について、両Widgetの`RemoteViews`は文字列・既存resourceだけを設定し、実行時bitmapを転送していないことを確認。`MessageQueue`内部反射、static final書き換え、native動的ロード、旧BAL定数の利用も検索範囲では存在しない。
- 非推奨の`SHOW_IMPLICIT`を現在の入力APIのflag 0へ変更。旧`FLAG_FULLSCREEN`はInsetsControllerと重複するため削除。`setDecorFitsSystemWindows`はedge-to-edgeが強制されないAPI 31–34にだけ残す。
- UIラベルを文字列resourceへ移し、補完は表示文の連結ではなくEditableのtoken範囲置換へ変更した。Android lintは`warningsAsErrors=true`で検査する。

## 削除した旧経路と残す実稼働処理

- 本番から呼ばれず、旧試験だけが維持していた`AgentWifiStatusRenderer.render`と旧Network表示helperを削除。試験は現在のpresentation経路を検査するように更新した。inactiveなAndroid placeholderを表示しない判定は既存の共通helperを再利用した。
- 未使用のMLO `briefNodeLabel` / `briefMarker`を削除。Network detailをcommand名で補正するREPL側の処理も、parserの根本修正後に削除した。
- QA試験を通常sourceへ移し、overlay・一時Gradle source設定・別コピーを削除した。
- 古い`android.useAndroidX=false`のopt-outを削除した。
- 新gRPC generatorの出力と全sourceに`javax.annotation`参照がないことを確認し、不要になった`javax.annotation-api`依存を削除した。
- API 31–36でも使うWi-Fi/receiver/mapperの分岐はデッドコードではないため維持する。実際に参照されるHE/EHT decoder・IP内のWifiInfo表示も削除しない。
- `DropDeviceAdminReceiver`は既存端末のDevice Owner componentとして永続参照され得る。削除すると既存管理状態を壊すため維持する。旧平文credentialの削除処理も安全な更新のために維持するが、旧値の読み込み・平文保存・互換実行経路は復活させない。

## 検証と未検証範囲

- Go全packageの通常・race試験、vet、staticcheck、Go 1.26.0 / 1.26.8 / 1.27.2のmodernize検査は通過。
- Androidは第二巡QA統合後216単体試験、失敗・エラー・skip 0。Android lint 9.5.0のXMLはissue 0。実機試験の追加後もlintを確認した。
- 最新SDKでdebug本体とinstrumentation APKをビルド。APKのmanifestからmin SDK 31、target SDK 37、LAN権限宣言を確認した。
- CIのSDK provisionも`platforms;android-37.2`へ更新した。未pushのためGitHub-hosted CI自体は未実行。
- 最新APKを指定Pixel 9 / Android 17へ上書き導入し、target SDK 37とinstalled APKのhash一致を確認。IME・画面配置・再生成・権限表示・Network取得・実PC PTY/gRPCは[実機再確認](shell-2.0/acceptance.md)で通過した。旧target SDK 36の結果を流用していない。
- 権限ダイアログの許可・拒否、LAN通信、TLS/CT/ECH、API 31–36端末、Widget、clipboard転送・TalkBackは未測定。必要権限は既存の許可を使い、許可・取消を行っていない。
- 本体・試験packageのデータ領域、表示設定・端末権限は保持した。Wi-Fi変更・新規スキャン・外部プローブも0。機能修正の根拠は[QA報告](shell-2.0/qa.md)を参照。
