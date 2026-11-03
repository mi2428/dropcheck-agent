# self-hosted CI

`.github/workflows/ci.yml`は`ubuntu-latest-self-hosted`を使う。既存のGitHub-hosted用workflowを移行し、二重実行する別workflowは作らない。

## 実行内容

- Controller: Go 1.26.0 / 1.26.8。protobuf生成の一致、整形、modernize、ビルド、全packageの通常試験、実PTY履歴試験、vet、staticcheck、raceを実行する。
- Android: JDK 17、SDK 37.2 / Build Tools 37.0.0をworkflow内で準備。ユニットテストは`--rerun-tasks --no-build-cache`で実行する。JUnit XMLがない場合、試験数0、失敗・エラー・skipがある場合も失敗とする。件数をjob summaryへ記録する。
- Android lint、本体APKとinstrumentation APKのビルドも実行。試験・lintレポートを成功・失敗に関係なく7日間のartifactとして保存する。実機接続・Wi-Fi変更・外部プローブはCIに含めない。
- 同一refの新しい実行は古い実行をcancelする。Androidのworker数は2に制限する。

## runnerとの対応

2026-11-03に`../homelab/infrastructure/actions-runner/`と実際のARC設定を確認した。`dropcheck-agent`のscale setは既にこのlabelで登録されている。runnerはephemeralで、最大1台、CPU上限3、memory上限6Gi。

共通imageにはMake・Python・Go・gofmt・C compilerなどがある。JDKとAndroid SDKは既存のsetup actionで用意できるため、今回のためのimage拡張や`homelab`変更は不要。Goの版はrunner内の既定値に任せずsetup-goで指定する。追加したupload actionも含め、各actionはcommit SHAで固定する。

runnerはLANへ到達できるため、triggerはリポジトリ内の`push`と`workflow_dispatch`だけに限定する。forkの`pull_request`や`pull_request_target`で未確認のコードを実行しない。書き込み権限を持つ人のpushは信頼する前提であり、ネットワーク隔離の代わりにはならない。`GITHUB_TOKEN`は`contents: read`、checkoutのcredential保存は無効。

## 有効化と確認範囲

確認時点で、このリポジトリのGitHub Actionsは`enabled=false`だった。runner登録とworkflowのcommitだけでは実行されない。有効化はGitHubのSettings → Actions → Generalで行う必要がある。

ローカルのユニットテスト・Lintとworkflow検査の結果は作業報告に記録する。push、Actions設定の変更、GitHub上のjob完走はそれぞれ別の操作であり、ローカル成功をself-hosted実行の成功として扱わない。

### ローカル検証（2026-11-03）

- Androidはworkflowと同じworker数・daemon無効・`--rerun-tasks --no-build-cache`で全216試験を再実行し、失敗・エラー・skipは0。Lint issueも0、本体・instrumentation APKのビルドも通過した。
- Go 1.26.0 / 1.26.8で全packageと実PTY履歴試験を完走。Go 1.26.8のrace、vet、staticcheck、実PTYのraceも通過。modernizeはGo 1.26.0 / 1.26.8 / 1.27.2で差分なし。
- protobuf再生成は差分なし。整形・共通契約検査、actionlintは通過。JUnit完了判定は実際の216試験のXMLを受理し、未生成・0試験・失敗・エラー・skip・属性欠落の合成XMLはすべて拒否した。
- これらはmacOS上の検証である。GitHubのself-hosted jobは、Actionsが無効かつ未pushのため未実行。`homelab`の設定・デプロイは変更していない。
