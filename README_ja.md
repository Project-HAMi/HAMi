[English version](README.md) | [中文版](README_cn.md) | 日本語版

<img src="imgs/hami-horizontal-colordark.png" width="600px">

[![LICENSE](https://img.shields.io/github/license/Project-HAMi/HAMi.svg)](/LICENSE)
[![build status](https://github.com/Project-HAMi/HAMi/actions/workflows/ci.yaml/badge.svg)](https://github.com/Project-HAMi/HAMi/actions/workflows/ci.yaml)
[![Releases](https://img.shields.io/github/v/release/Project-HAMi/HAMi)](https://github.com/Project-HAMi/HAMi/releases/latest)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/9416/badge)](https://www.bestpractices.dev/en/projects/9416)
[![Go Report Card](https://goreportcard.com/badge/github.com/Project-HAMi/HAMi)](https://goreportcard.com/report/github.com/Project-HAMi/HAMi)
[![codecov](https://codecov.io/gh/Project-HAMi/HAMi/branch/master/graph/badge.svg?token=ROM8CMPXZ6)](https://codecov.io/gh/Project-HAMi/HAMi)
[![FOSSA Status](https://app.fossa.com/api/projects/git%2Bgithub.com%2FProject-HAMi%2FHAMi.svg?type=shield)](https://app.fossa.com/projects/git%2Bgithub.com%2FProject-HAMi%2FHAMi?ref=badge_shield)
[![docker pulls](https://img.shields.io/docker/pulls/projecthami/hami.svg)](https://hub.docker.com/r/projecthami/hami)
[![Contact Me](https://img.shields.io/badge/Contact%20Me-blue)](https://github.com/Project-HAMi/HAMi#meeting--contact)
[![slack](https://img.shields.io/badge/slack-green?style=for-the-badge&logo=googlechat)](https://cloud-native.slack.com/archives/C07T10BU4R2)
[![discord](https://img.shields.io/badge/discord-5865F2?style=for-the-badge&logo=discord)](https://discord.gg/Amhy7XmbNq)
[![website](https://img.shields.io/badge/website-green?style=for-the-badge&logo=readthedocs)](http://project-hami.io)

## Project-HAMi: 異種AIコンピューティング仮想化ミドルウェア

## はじめに

HAMi(旧称「k8s-vGPU-scheduler」)は、Kubernetes用のヘテロジニアスデバイス管理ミドルウェアです。GPU、NPUなどの異なるタイプのヘテロジニアスデバイスを管理し、Pod間でヘテロジニアスデバイスを共有し、デバイスのトポロジーとスケジューリングポリシーに基づいてより良いスケジューリング決定を行うことができます。

HAMiは、異なるヘテロジニアスデバイス間のギャップを埋め、ユーザーがアプリケーションを変更することなく統一されたインターフェースで管理できるようにすることを目指しています。2024年12月現在、HAMiはインターネット、パブリッククラウド、プライベートクラウドだけでなく、金融、証券、エネルギー、通信、教育、製造などのさまざまな垂直産業でも広く採用されています。50以上の企業や機関がエンドユーザーであるだけでなく、アクティブな貢献者でもあります。

![cncf_logo](imgs/cncf-logo.png)

HAMiは[Cloud Native Computing Foundation](https://cncf.io/)(CNCF)のサンドボックス及び[landscape](https://landscape.cncf.io/?item=orchestration-management--scheduling-orchestration--hami)プロジェクトであり、
[CNAI Landscapeプロジェクト](https://landscape.cncf.io/?group=cnai&item=cnai--general-orchestration--hami)でもあります。


## デバイス仮想化

HAMiは、デバイス共有とデバイスリソース分離をサポートすることにより、GPUを含むいくつかのヘテロジニアスデバイスにデバイス仮想化を提供します。デバイス仮想化をサポートするデバイスのリストについては、[サポートされているデバイス](#サポートされているデバイス)を参照してください。

### デバイス共有

- デバイスコアの使用量を指定することにより、部分的なデバイス割り当てが可能です。
- デバイスメモリを指定することにより、部分的なデバイス割り当てが可能です。
- ストリーミングマルチプロセッサに厳格な制限を課します。
- 既存のプログラムへの変更は不要です。
- [dynamic-mig](docs/dynamic-mig-support.md)機能をサポートします。[例](examples/nvidia/dynamic_mig_example.yaml)

<img src="./imgs/example.png" width = "500" />

### デバイスリソース分離

デバイス分離の簡単なデモンストレーション:
次のリソースを持つタスクは、コンテナ内で3000MのデバイスメモリーとGPUを認識します:

```yaml
      resources:
        limits:
          nvidia.com/gpu: 1 # Podが必要とする物理GPUの数を宣言
          nvidia.com/gpumem: 3000 # 各物理GPUがPodに割り当てる3GのGPUメモリを識別
```

![img](./imgs/hard_limit.jpg)

> 注意:
1. **HAMiをインストールした後、ノードに登録される`nvidia.com/gpu`の値はデフォルトでvGPUの数になります。**
2. **Pod内でリソースをリクエストする場合、`nvidia.com/gpu`は現在のPodが必要とする物理GPUの数を指します。**

### サポートされているデバイス

[NVIDIA GPU](https://github.com/Project-HAMi/HAMi#preparing-your-gpu-nodes)   
[Cambricon MLU](docs/cambricon-mlu-support.md)   
[HYGON DCU](docs/hygon-dcu-support.md)   
[Iluvatar CoreX GPU](docs/iluvatar-gpu-support.md)   
[Moore Threads GPU](docs/mthreads-support.md)   
[HUAWEI Ascend NPU](https://github.com/Project-HAMi/ascend-device-plugin/blob/main/README.md)   
[MetaX GPU](docs/metax-support.md)   

## アーキテクチャ

<img src="./imgs/hami-arch.png" width = "600" />

HAMiは、統一されたmutatingwebhook、統一されたスケジューラーエクステンダー、異なるデバイスプラグイン、および各ヘテロジニアスAIデバイスのための異なるコンテナ内仮想化技術を含む、いくつかのコンポーネントで構成されています。

## クイックスタート

### オーケストレーターを選択

[![kube-scheduler](https://img.shields.io/badge/kube-scheduler-blue)](#前提条件)
[![volcano-scheduler](https://img.shields.io/badge/volcano-scheduler-orange)](docs/how-to-use-volcano-vgpu.md)

### 前提条件

NVIDIAデバイスプラグインを実行するための前提条件のリストは以下の通りです:

- NVIDIA drivers >= 440
- nvidia-docker version > 2.0
- containerd/docker/cri-oコンテナランタイムのデフォルトランタイムとしてnvidiaが設定されていること
- Kubernetes version >= 1.18
- glibc >= 2.17 & glibc < 2.30
- kernel version >= 3.10
- helm > 3.0

### インストール

まず、「gpu=on」ラベルを追加して、HAMiでスケジューリングするためにGPUノードにラベルを付けます。このラベルがないと、ノードはスケジューラーで管理できません。

```
kubectl label nodes {nodeid} gpu=on
```

helmにリポジトリを追加します

```
helm repo add hami-charts https://project-hami.github.io/HAMi/
```

次のコマンドでデプロイします:

```
helm install hami hami-charts/hami -n kube-system
```

[設定](docs/config.md)を調整してインストールをカスタマイズします。

次のコマンドでインストールを確認します:

```
kubectl get pods -n kube-system
```

`hami-device-plugin`(旧称`vgpu-device-plugin`)と`hami-scheduler`(旧称`vgpu-scheduler`)の両方のPodが*Running*状態であれば、インストールは成功です。[こちら](examples/nvidia/default_use.yaml)の例を試すことができます。

### WebUI

[HAMi-WebUI](https://github.com/Project-HAMi/HAMi-WebUI)はHAMi v2.4以降で利用可能です。

インストールガイドについては、[こちら](https://github.com/Project-HAMi/HAMi-WebUI/blob/main/docs/installation/helm/index.md)をクリックしてください。

### モニタリング

モニタリングはインストール後に自動的に有効になります。次のURLにアクセスしてクラスター情報の概要を取得します:

```
http://{scheduler ip}:{monitorPort}/metrics
```

デフォルトのmonitorPortは31993です。他の値はインストール時に`--set devicePlugin.service.httpPort`を使用して設定できます。

Grafanaダッシュボードの[例](docs/dashboard.md)

> **注意** タスクを送信する前にノードのステータスは収集されません

## 注意事項

- NVIDIAイメージでデバイスプラグインを使用する際にvGPUをリクエストしない場合、マシン上のすべてのGPUがコンテナ内に公開される可能性があります
- 現在、A100 MIGは「none」および「mixed」モードでのみサポートされています。
- 「nodeName」フィールドを持つタスクは現時点ではスケジュールできません。代わりに「nodeSelector」を使用してください。

## ロードマップ、ガバナンス、コントリビューション

このプロジェクトは[メンテナー](./MAINTAINERS.md)と[コントリビューター](./AUTHORS.md)のグループによって管理されています。彼らがどのように選ばれ、管理されているかは、[ガバナンスドキュメント](https://github.com/Project-HAMi/community/blob/main/governance.md)に概説されています。

コントリビューターになり、HAMiコードの開発に関わることに興味がある場合は、パッチの送信とコントリビューションワークフローの詳細について[CONTRIBUTING](CONTRIBUTING.md)を参照してください。

興味のあることについては[ロードマップ](docs/develop/roadmap.md)を参照してください。

## ミーティングと連絡先

HAMiコミュニティは、オープンで歓迎的な環境を育むことに取り組んでおり、他のユーザーや開発者と関わるための複数の方法があります。

ご質問がある場合は、以下のチャネルからお気軽にお問い合わせください:

- 定期コミュニティミーティング: 毎週金曜日 16:00 UTC+8 (中国語)。[タイムゾーンに変換](https://www.thetimezoneconverter.com/?t=14%3A30&tz=GMT%2B8&)。
  - [ミーティングノートとアジェンダ](https://docs.google.com/document/d/1YC6hco03_oXbF9IOUPJ29VWEddmITIKIfSmBX8JtGBw/edit#heading=h.g61sgp7w0d0c)
  - [ミーティングリンク](https://meeting.tencent.com/dm/Ntiwq1BICD1P)
- Email: すべてのメンテナーのメールアドレスは[MAINTAINERS.md](MAINTAINERS.md)を参照してください。問題を報告したり質問したりする場合は、メールでお気軽にご連絡ください。
- [メーリングリスト](https://groups.google.com/forum/#!forum/hami-project)

## 講演と参考資料

|                  | リンク                                                                                                                    |
|------------------|-------------------------------------------------------------------------------------------------------------------------|
| CHINA CLOUD COMPUTING INFRASTRUCTURE DEVELOPER CONFERENCE (Beijing 2024) | [Unlocking heterogeneous AI infrastructure on k8s clusters](https://live.csdn.net/room/csdnnews/3zwDP09S) 03:06:15から開始 |
| KubeDay(Japan 2024) | [Unlocking Heterogeneous AI Infrastructure K8s Cluster:Leveraging the Power of HAMi](https://www.youtube.com/watch?v=owoaSb4nZwg) |
| KubeCon & AI_dev Open Source GenAI & ML Summit(China 2024) | [Is Your GPU Really Working Efficiently in the Data Center?N Ways to Improve GPU Usage](https://www.youtube.com/watch?v=ApkyK3zLF5Q) |
| KubeCon & AI_dev Open Source GenAI & ML Summit(China 2024) | [Unlocking Heterogeneous AI Infrastructure K8s Cluster](https://www.youtube.com/watch?v=kcGXnp_QShs)                                     |
| KubeCon(EU 2024)| [Cloud Native Batch Computing with Volcano: Updates and Future](https://youtu.be/fVYKk6xSOsw) |

## ライセンス

HAMiはApache 2.0ライセンスの下にあります。詳細については[LICENSE](LICENSE)ファイルを参照してください。

## スター履歴

[![Star History Chart](https://api.star-history.com/svg?repos=Project-HAMi/HAMi&type=Date)](https://star-history.com/#Project-HAMi/HAMi&Date)
