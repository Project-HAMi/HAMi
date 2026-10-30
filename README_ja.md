[English version](README.md) | [中文版](README_cn.md) | 日本語版

<img src="imgs/hami-horizontal-colordark.png" width="600px" alt="HAMi logo">

[![LICENSE](https://img.shields.io/github/license/Project-HAMi/HAMi.svg)](/LICENSE)
[![build status](https://github.com/Project-HAMi/HAMi/actions/workflows/ci.yaml/badge.svg)](https://github.com/Project-HAMi/HAMi/actions/workflows/ci.yaml)
[![Releases](https://img.shields.io/github/v/release/Project-HAMi/HAMi)](https://github.com/Project-HAMi/HAMi/releases/latest)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/9416/badge)](https://www.bestpractices.dev/en/projects/9416)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/Project-HAMi/HAMi/badge)](https://scorecard.dev/viewer/?uri=github.com/Project-HAMi/HAMi)
[![Go Report Card](https://goreportcard.com/badge/github.com/Project-HAMi/HAMi)](https://goreportcard.com/report/github.com/Project-HAMi/HAMi)
[![codecov](https://app.codecov.io/gh/Project-HAMi/HAMi/branch/master/graph/badge.svg?token=ROM8CMPXZ6)](https://app.codecov.io/gh/Project-HAMi/HAMi)
[![FOSSA Status](https://app.fossa.com/api/projects/git%2Bgithub.com%2FProject-HAMi%2FHAMi.svg?type=shield)](https://app.fossa.com/projects/git%2Bgithub.com%2FProject-HAMi%2FHAMi?ref=badge_shield)
[![docker pulls](https://img.shields.io/docker/pulls/projecthami/hami.svg)](https://hub.docker.com/r/projecthami/hami)
[![slack](https://img.shields.io/badge/slack-green?style=for-the-badge&logo=slack)](https://cloud-native.slack.com/archives/C07T10BU4R2)
[![discord](https://img.shields.io/badge/discord-grey?style=for-the-badge&logo=discord)](https://discord.com/invite/Amhy7XmbNq)
[![website](https://img.shields.io/badge/website-blue?style=for-the-badge&logo=readthedocs)](https://project-hami.io)

# HAMi

**AI インフラストラクチャ向け Kubernetes GPU 仮想化およびヘテロジニアスアクセラレータスケジューリング。**

![HAMi Architecture](imgs/hami-architecture.png)

HAMi は **Heterogeneous AI Computing Virtualization Middleware**（異種 AI コンピューティング仮想化ミドルウェア）の略称です。旧称 `k8s-vGPU-scheduler`。HAMi はプラットフォームチームが Kubernetes ワークロード間で高価な GPU やその他の AI アクセラレータを共有し、デバイスメモリとコンピュートを分離し、アプリケーションコードを変更することなくデバイスアウェアなスケジューリングポリシーで Pod をスケジュールできるようにします。

HAMi は [CNCF Incubating](https://www.cncf.io/projects/) および [CNCF Landscape](https://landscape.cncf.io/?item=orchestration-management--scheduling-orchestration--hami) プロジェクトであり、[CNAI Landscape](https://landscape.cncf.io/?group=cnai&item=cnai--general-orchestration--hami) にも掲載されています。

![CNCF logo](imgs/cncf-logo.png)

## ドキュメント

- [アーキテクチャ](https://project-hami.io/docs/next/core-concepts/architecture)
- [GPU 仮想化](https://project-hami.io/docs/next/core-concepts/gpu-virtualization)
- [対応デバイスとベンダー別ガイド](https://project-hami.io/docs/next/userguide/device-supported)
- [前提条件](https://project-hami.io/docs/next/installation/prerequisites)
- [インストールと最初のワークロード](https://project-hami.io/docs/next/get-started/deploy-with-helm)
- [アップグレードと設定の移行](https://project-hami.io/docs/next/installation/upgrade)
- [設定と運用](https://project-hami.io/docs/next/userguide/configure)
- [NVIDIA スケジューリングポリシー](https://project-hami.io/docs/next/userguide/nvidia-device/scheduling-policy)
- [NVIDIA ダイナミック MIG](https://project-hami.io/docs/next/userguide/nvidia-device/dynamic-mig-support)
- [デバイス割り当てメトリクス](https://project-hami.io/docs/next/userguide/monitoring/device-allocation)
- [デバイス使用状況のメトリクス](https://project-hami.io/docs/next/userguide/monitoring/real-time-device-usage)
- [Grafana ダッシュボード](https://project-hami.io/docs/next/userguide/monitoring/grafana-dashboard)
- [WebUI](https://project-hami.io/docs/next/userguide/hami-webui-user-guide)
- [Volcano 連携](https://project-hami.io/docs/next/installation/how-to-use-volcano-vgpu)
- [Kueue 連携](https://project-hami.io/docs/next/userguide/kueue/how-to-use-kueue)
- [ベンチマーク](https://project-hami.io/docs/next/userguide/benchmark)
- [トラブルシューティング](https://project-hami.io/docs/next/troubleshooting)
- [アンインストール](https://project-hami.io/docs/next/installation/uninstall)

## リポジトリのリソース

- [Chart の設定値一覧](charts/hami/README.md)
- [ワークロードの例](examples/)
- [Agent skills](skill/)
- [HAMi-WebUI リポジトリ](https://github.com/Project-HAMi/HAMi-WebUI)

## ロードマップ、ガバナンス、コントリビューション

HAMi は[メンテナー](./MAINTAINERS.md)と[コントリビューター](./AUTHORS.md)によって管理されています。ガバナンスについては、[HAMi コミュニティリポジトリ](https://github.com/Project-HAMi/community/blob/main/governance.md)を参照してください。

コード、ドキュメント、テスト、デバイスバックエンドの改善に貢献するには、[CONTRIBUTING.md](./CONTRIBUTING.md) をお読みください。

## コミュニティ

チャット、メーリングリスト、ミーティングの予定と議事録は [HAMi コミュニティページ（英語）](https://project-hami.io/community)を参照してください。

[講演資料](https://github.com/Project-HAMi/community/tree/main/talks)

## ライセンス

HAMi は Apache License 2.0 の下でライセンスされています。詳細は [LICENSE](LICENSE) を参照してください。

Copyright Contributors to HAMi, established as HAMi a Series of LF Projects, LLC.
