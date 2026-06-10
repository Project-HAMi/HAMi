# HAMi vGPU 격리 — Step A: Namespace opt-in/out Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** HAMi 격리 메커니즘 (LD_PRELOAD inject + Vulkan implicit layer manifest mount + webhook env mutation) 을 노드 wide 강제 적용에서 namespace label 기반 opt-in 으로 변경하여, isaac-launchable namespace 의 Isaac Sim Kit 를 정상 동작 baseline 으로 유지하면서 다른 GPU workload namespace 만 격리 enforce 한다.

**Architecture:** webhook 의 `namespaceSelector` 를 opt-out (`hami.io/webhook NotIn ignore`) 에서 opt-in (`hami.io/vgpu In enabled`) 으로 helm values 변경 + 노드 wide `/usr/local/vgpu/ld.so.preload` 와 `hami.json` 자동 install daemonset 을 비활성 + 검증 namespace 에 label 적용 후 격리 enforce 동작 확인. webhook backend 코드 변경 없이 helm chart values 변경 + cluster 측 daemonset patch 로 완료.

**Tech Stack:** Kubernetes 1.34.3 (k0s), Helm chart `hami` (이번 fork `xiilab/feat/vulkan-vgpu`), kubectl, ws-node074 (RTX 6000 Ada x2, NVIDIA driver 580.142).

**Plan scope:** 본 plan 은 design doc 의 4 step 중 **Step A 만** 다룬다. Step B (HAMi-core hook hardening), Step C (Vulkan layer compat), Step D (isaac-launchable opt-in 활성화) 는 별도 plan 으로 작성.

---

## File Structure

| 파일 | 변경 종류 | 책임 |
|---|---|---|
| `charts/hami/values.yaml` | Modify | webhook `namespaceSelector` mode 옵션 (`mode: opt-in \| opt-out`) + 새 default 추가 |
| `charts/hami/templates/scheduler/webhook.yaml` | Modify | `mode` 값 따라 `namespaceSelector.matchExpressions` 분기 (opt-in 시 `hami.io/vgpu In [enabled]`) |
| `cluster/runtime/hami-vulkan-manifest-installer.yaml` (신규) | Create | 현재 cluster 에 install된 ds 의 spec 백업 + 비활성화 패치 (label-based scope 또는 scale 0) — chart 외부 yaml |
| `cluster/runtime/hami-preload-installer.yaml` (신규) | Create | 노드 wide `/usr/local/vgpu/ld.so.preload` 만든 entity 의 spec 백업 + 비활성화 (예: device-plugin daemonset 의 `deviceconfig` ConfigMap 의 `ld.so.preload` key 비우기 또는 mount 제거) |
| `docs/superpowers/specs/2026-04-28-hami-isolation-isaac-sim-design.md` | (no change) | 이미 commit 됨 (`d177471`) — Step A 의 spec 근거 |

**Note:** `hami-vulkan-manifest-installer` ds 와 `hami-preload-installer` ds 의 원본 yaml 은 우리 chart 안에 없다 (cluster 측 별도 설치). 본 plan 은 그 ds 들의 현재 cluster 상태를 dump 하고 namespace label 기반 opt-in 으로 변환된 새 yaml 을 cluster 에 apply 한다.

---

## Tasks

### Task 1: 현재 cluster 의 webhook + installer ds spec 을 yaml 로 dump (백업)

**Files:**
- Create: `cluster/runtime/snapshot-2026-04-28/hami-webhook-mutating.yaml`
- Create: `cluster/runtime/snapshot-2026-04-28/hami-vulkan-manifest-installer-ds.yaml`
- Create: `cluster/runtime/snapshot-2026-04-28/volcano-device-plugin-ds.yaml`
- Create: `cluster/runtime/snapshot-2026-04-28/hami-vulkan-manifest-cm.yaml`

- [ ] **Step 1: snapshot 디렉토리 생성**

```bash
mkdir -p /Users/xiilab/git/HAMi/cluster/runtime/snapshot-2026-04-28
cd /Users/xiilab/git/HAMi
```

- [ ] **Step 2: webhook 현재 spec dump**

```bash
kubectl get mutatingwebhookconfiguration hami-webhook-webhook -o yaml \
  > cluster/runtime/snapshot-2026-04-28/hami-webhook-mutating.yaml
ls -la cluster/runtime/snapshot-2026-04-28/hami-webhook-mutating.yaml
```

Expected: 파일 존재, size > 0, `namespaceSelector:` 안에 `key: hami.io/webhook` 와 `operator: NotIn` 보임.

- [ ] **Step 3: 두 daemonset + ConfigMap dump**

```bash
kubectl -n kube-system get ds hami-vulkan-manifest-installer -o yaml \
  > cluster/runtime/snapshot-2026-04-28/hami-vulkan-manifest-installer-ds.yaml
kubectl -n kube-system get ds volcano-device-plugin -o yaml \
  > cluster/runtime/snapshot-2026-04-28/volcano-device-plugin-ds.yaml
kubectl -n kube-system get cm hami-vulkan-manifest -o yaml \
  > cluster/runtime/snapshot-2026-04-28/hami-vulkan-manifest-cm.yaml
ls -la cluster/runtime/snapshot-2026-04-28/
```

Expected: 4 yaml 파일 존재.

- [ ] **Step 4: snapshot commit**

```bash
git add cluster/runtime/snapshot-2026-04-28/
git commit -s -m "chore(cluster): snapshot 4-27 새벽 패치 시점의 webhook + installer ds + cm"
```

Expected: commit 생성, `git log --oneline -1` 에 commit 보임.

---

### Task 2: helm chart values.yaml 에 namespaceSelector mode 옵션 추가

**Files:**
- Modify: `charts/hami/values.yaml:178-185` (현 namespaceSelector block)

- [ ] **Step 1: 현재 values.yaml 의 namespaceSelector block 확인**

```bash
sed -n '170,200p' charts/hami/values.yaml
```

Expected output (4-line `matchExpressions` 가 opt-out 인 상태):
```yaml
    namespaceSelector:
      matchLabels: {}
      matchExpressions: []
      ## opt-out: hami.io/webhook=ignore label 가진 namespace 는 webhook 적용 안 함
      ## (template 에 hard-coded matchExpressions 존재)
```

- [ ] **Step 2: values.yaml 수정 — mode 옵션 추가**

`charts/hami/values.yaml` 의 `scheduler.admissionWebhook.namespaceSelector` block 을 다음으로 교체:

```yaml
    # namespaceSelector controls which namespaces the webhook will apply to.
    # mode:
    #   "opt-out" (legacy default): apply to all namespaces except those labeled
    #              hami.io/webhook=ignore. Suitable when most workloads need vGPU
    #              isolation and a small number opt out.
    #   "opt-in"  (recommended for clusters with NVIDIA Omniverse / Isaac Sim
    #              workloads that conflict with HAMi-core hooks): apply ONLY to
    #              namespaces labeled hami.io/vgpu=enabled. Other namespaces see
    #              no mutation, no LD_PRELOAD inject, no implicit Vulkan layer.
    namespaceSelector:
      mode: opt-in
      matchLabels: {}
      matchExpressions: []
```

- [ ] **Step 3: helm lint 로 syntax 검증**

```bash
cd /Users/xiilab/git/HAMi
helm lint charts/hami 2>&1 | tail -5
```

Expected: `1 chart(s) linted, 0 chart(s) failed`.

- [ ] **Step 4: commit**

```bash
git add charts/hami/values.yaml
git commit -s -m "feat(chart): add namespaceSelector.mode (opt-in|opt-out) for webhook" \
  -m "Adds an explicit mode toggle. opt-in matches namespaces labeled hami.io/vgpu=enabled (recommended for clusters running NVIDIA Omniverse / Isaac Sim workloads that conflict with HAMi-core hooks). opt-out keeps the legacy hami.io/webhook=ignore exclusion behavior. Default switches to opt-in to fail safe — clusters with vGPU workloads must explicitly enable per-namespace."
```

---

### Task 3: helm chart webhook template 의 namespaceSelector 분기

**Files:**
- Modify: `charts/hami/templates/scheduler/webhook.yaml` (namespaceSelector block)

- [ ] **Step 1: 현재 webhook template 의 namespaceSelector block 확인**

```bash
grep -n -A 15 "namespaceSelector:" charts/hami/templates/scheduler/webhook.yaml
```

Expected: opt-out hard-code (`key: hami.io/webhook, operator: NotIn, values: [ignore]`).

- [ ] **Step 2: namespaceSelector block 을 mode 분기로 교체**

```yaml
    namespaceSelector:
      {{- if .Values.scheduler.admissionWebhook.namespaceSelector.matchLabels }}
      matchLabels:
        {{- toYaml .Values.scheduler.admissionWebhook.namespaceSelector.matchLabels | nindent 8 }}
      {{- end }}
      matchExpressions:
      {{- if eq (.Values.scheduler.admissionWebhook.namespaceSelector.mode | default "opt-out") "opt-in" }}
      - key: hami.io/vgpu
        operator: In
        values:
        - enabled
      {{- else }}
      - key: hami.io/webhook
        operator: NotIn
        values:
        - ignore
      {{- end }}
      {{- if .Values.scheduler.admissionWebhook.whitelistNamespaces }}
      - key: kubernetes.io/metadata.name
        operator: NotIn
        values:
        {{- toYaml .Values.scheduler.admissionWebhook.whitelistNamespaces | nindent 10 }}
      {{- end }}
      {{- if .Values.scheduler.admissionWebhook.namespaceSelector.matchExpressions }}
      {{- toYaml .Values.scheduler.admissionWebhook.namespaceSelector.matchExpressions | nindent 6 }}
      {{- end }}
```

- [ ] **Step 3: helm template render — opt-in mode 일 때 generated YAML 검증**

```bash
helm template my-hami charts/hami --show-only templates/scheduler/webhook.yaml \
  --set scheduler.admissionWebhook.namespaceSelector.mode=opt-in 2>&1 \
  | grep -A 6 namespaceSelector
```

Expected output 안에:
```
matchExpressions:
- key: hami.io/vgpu
  operator: In
  values:
  - enabled
```

- [ ] **Step 4: helm template render — opt-out mode 일 때 generated YAML 검증**

```bash
helm template my-hami charts/hami --show-only templates/scheduler/webhook.yaml \
  --set scheduler.admissionWebhook.namespaceSelector.mode=opt-out 2>&1 \
  | grep -A 6 namespaceSelector
```

Expected output 안에:
```
matchExpressions:
- key: hami.io/webhook
  operator: NotIn
  values:
  - ignore
```

- [ ] **Step 5: commit**

```bash
git add charts/hami/templates/scheduler/webhook.yaml
git commit -s -m "feat(chart): webhook namespaceSelector branches on mode (opt-in|opt-out)" \
  -m "Renders the matching matchExpressions block based on the new namespaceSelector.mode value (Task 2). opt-in produces 'hami.io/vgpu In [enabled]'; opt-out keeps 'hami.io/webhook NotIn [ignore]'. Whitelist and user-supplied matchExpressions are still appended after the mode-specific entry."
```

---

### Task 4: cluster 의 webhook MutatingWebhookConfiguration 을 opt-in 으로 직접 patch (helm 재배포 없이)

**Files:**
- Modify (cluster only): `MutatingWebhookConfiguration/hami-webhook-webhook`

이 task 는 helm release 재실행이 아니라 **cluster 의 webhook spec 만 직접 patch** 한다 (다른 helm-managed 자원 영향 안 줌). 후속 helm upgrade 시 chart 변경분 (Task 2/3) 과 일치.

- [ ] **Step 1: 현재 webhook namespaceSelector 확인**

```bash
kubectl get mutatingwebhookconfiguration hami-webhook-webhook \
  -o jsonpath='{.webhooks[0].namespaceSelector}{"\n"}'
```

Expected (opt-out):
```
{"matchExpressions":[{"key":"hami.io/webhook","operator":"NotIn","values":["ignore"]}]}
```

- [ ] **Step 2: opt-in 으로 patch**

```bash
kubectl patch mutatingwebhookconfiguration hami-webhook-webhook --type=json \
  --patch='[{"op":"replace","path":"/webhooks/0/namespaceSelector","value":{"matchExpressions":[{"key":"hami.io/vgpu","operator":"In","values":["enabled"]}]}}]'
```

Expected: `mutatingwebhookconfiguration.admissionregistration.k8s.io/hami-webhook-webhook patched`

- [ ] **Step 3: 검증 — opt-in 으로 변경됨**

```bash
kubectl get mutatingwebhookconfiguration hami-webhook-webhook \
  -o jsonpath='{.webhooks[0].namespaceSelector}{"\n"}'
```

Expected:
```
{"matchExpressions":[{"key":"hami.io/vgpu","operator":"In","values":["enabled"]}]}
```

- [ ] **Step 4: isaac-launchable namespace 의 기존 label `hami.io/webhook=ignore` 제거 (이제 불필요)**

```bash
kubectl label namespace isaac-launchable hami.io/webhook-
```

Expected: `namespace/isaac-launchable unlabeled`.

- [ ] **Step 5: isaac-launchable pod 재생성 — webhook mutation 0 건 검증**

```bash
kubectl -n isaac-launchable delete pod -l app=isaac-launchable --wait=false
sleep 80
NEWPOD=$(kubectl -n isaac-launchable get pod -l app=isaac-launchable,instance=pod-1 -o jsonpath='{.items[0].metadata.name}')
echo "POD=$NEWPOD"
kubectl -n isaac-launchable exec $NEWPOD -c vscode -- bash -lc \
  'env | grep -E "^(HAMI|LD_PRELOAD|NVIDIA_DRIVER_CAP)" ; ls /etc/vulkan/implicit_layer.d/'
```

Expected:
- env 에 `HAMI_VULKAN_ENABLE` 없음 (또는 기존 deployment yaml 에 박힌 것만)
- env 에 `LD_PRELOAD` 없음
- `/etc/vulkan/implicit_layer.d/` 에 `hami.json` 없음 (단, ld.so.preload 가 컨테이너 안에 있을 수 있음 — 별도 task 처리)

---

### Task 5: 노드 wide `/usr/local/vgpu/ld.so.preload` 와 hami.json install daemonset 비활성화

**Files:**
- Modify (cluster only): node `ws-node074:/usr/local/vgpu/ld.so.preload` (이미 비어있는 상태 유지)
- Modify (cluster only): `DaemonSet/hami-vulkan-manifest-installer` (비활성)

- [ ] **Step 1: 노드 ld.so.preload 가 빈 파일 또는 미존재 확인**

```bash
ssh root@10.61.3.74 'ls -la /usr/local/vgpu/ld.so.preload; cat /usr/local/vgpu/ld.so.preload | wc -c'
```

Expected: 파일 size 0 또는 1 (빈/newline). 만약 size > 1 이면 비우기:

```bash
ssh root@10.61.3.74 ': > /usr/local/vgpu/ld.so.preload'
```

- [ ] **Step 2: hami-vulkan-manifest-installer ds 가 비활성 (nodeSelector hami.io/disabled=true) 확인**

```bash
kubectl -n kube-system get ds hami-vulkan-manifest-installer \
  -o jsonpath='{.spec.template.spec.nodeSelector}{"\n"}'
```

Expected:
```
{"hami.io/disabled":"true"}
```

만약 다른 selector 면 patch:

```bash
kubectl -n kube-system patch daemonset hami-vulkan-manifest-installer --type='json' \
  -p='[{"op":"replace","path":"/spec/template/spec/nodeSelector","value":{"hami.io/disabled":"true"}}]'
```

- [ ] **Step 3: 노드 hami.json manifest 가 컨테이너로 mount 안 되는지 검증**

```bash
NEWPOD=$(kubectl -n isaac-launchable get pod -l app=isaac-launchable,instance=pod-1 -o jsonpath='{.items[0].metadata.name}')
kubectl -n isaac-launchable exec $NEWPOD -c vscode -- bash -lc 'ls /etc/vulkan/implicit_layer.d/'
```

Expected: 출력에 `nvidia_layers.json` 만 있고 `hami.json` 없음. **만약 hami.json 있으면**: 노드 `/usr/local/vgpu/vulkan/implicit_layer.d/hami.json` 도 삭제 필요:

```bash
ssh root@10.61.3.74 'rm -f /usr/local/vgpu/vulkan/implicit_layer.d/hami.json; ls /usr/local/vgpu/vulkan/implicit_layer.d/'
```

그 후 pod 재생성 후 재검증.

- [ ] **Step 4: isaac-launchable runheadless.sh 5번 baseline 검증 — 5/5 alive 유지**

```bash
NEWPOD=$(kubectl -n isaac-launchable get pod -l app=isaac-launchable,instance=pod-1 -o jsonpath='{.items[0].metadata.name}')
kubectl -n isaac-launchable exec $NEWPOD -c vscode -- bash -lc '
mkdir -p /tmp/v
for i in 1 2 3 4 5; do
  pkill -KILL kit 2>/dev/null; sleep 3
  timeout 50 env ACCEPT_EULA=y /isaac-sim/runheadless.sh >/tmp/v/r$i.log 2>&1
  EC=$?
  CRASH=$(grep -cE "Segmentation fault|crash has occurred" /tmp/v/r$i.log)
  LISTEN=$(ss -tunlp 2>/dev/null | grep -c -E ":49100|:30999")
  echo "run $i: exit=$EC crash=$CRASH listen=$LISTEN"
done
pkill -KILL kit 2>/dev/null
'
```

Expected: 5번 모두 `exit=124 crash=0 listen>=1` (alive + signaling listen).

---

### Task 6: 새 검증 namespace `hami-test` 에 격리 enforce 동작 검증

**Files:**
- Create (cluster only): `Namespace/hami-test` (label `hami.io/vgpu=enabled`)
- Create: `cluster/runtime/test/cuda-partition-test-pod.yaml`

- [ ] **Step 1: 검증 namespace 만들고 label 적용**

```bash
kubectl create namespace hami-test --dry-run=client -o yaml | kubectl apply -f -
kubectl label namespace hami-test hami.io/vgpu=enabled --overwrite
kubectl get namespace hami-test --show-labels
```

Expected: label 출력에 `hami.io/vgpu=enabled` 포함.

- [ ] **Step 2: 단순 CUDA test pod manifest 작성**

`cluster/runtime/test/cuda-partition-test-pod.yaml`:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: cuda-partition-test
  namespace: hami-test
spec:
  restartPolicy: Never
  nodeSelector:
    kubernetes.io/hostname: ws-node074
  containers:
  - name: cuda
    image: 10.61.3.124:30002/library/isaac-launchable-vscode:6.0.0-fix5364
    command: ["/bin/bash", "-c"]
    args:
    - |
      set -e
      echo "=== nvidia-smi ==="
      nvidia-smi --query-gpu=memory.total --format=csv,noheader
      echo "=== env ==="
      env | grep -E "^(HAMI|LD_PRELOAD|NVIDIA_DRIVER_CAP)" | sort
      echo "=== ls /etc/vulkan/implicit_layer.d ==="
      ls /etc/vulkan/implicit_layer.d/
      echo "=== ld.so.preload ==="
      [ -f /etc/ld.so.preload ] && cat /etc/ld.so.preload || echo "(no ld.so.preload)"
      echo "=== sleep 60 ==="
      sleep 60
    resources:
      limits:
        volcano.sh/vgpu-number: "1"
        volcano.sh/vgpu-memory: "23"
        volcano.sh/vgpu-cores: "50"
      requests:
        volcano.sh/vgpu-number: "1"
        volcano.sh/vgpu-memory: "23"
        volcano.sh/vgpu-cores: "50"
```

- [ ] **Step 3: pod 배포 + webhook mutation 적용 검증**

```bash
kubectl apply -f cluster/runtime/test/cuda-partition-test-pod.yaml
sleep 30
kubectl -n hami-test get pod cuda-partition-test -o wide
kubectl -n hami-test logs cuda-partition-test
```

Expected logs:
- `nvidia-smi memory.total` = `23552 MiB` (NVML hook 적용됨)
- env 에 `HAMI_VULKAN_ENABLE=1` 또는 `LD_PRELOAD=/usr/local/vgpu/libvgpu.so` 둘 중 하나 이상 webhook mutation 으로 주입됨
- `ls /etc/vulkan/implicit_layer.d/` 에 `hami.json` 또는 (`hami.json` 없으면 다음 plan 의 webhook mutation 보완 필요)
- `ld.so.preload` 에 `/usr/local/vgpu/libvgpu.so` 포함

**중요:** 만약 webhook mutation 이 LD_PRELOAD env 와 hami.json mount 를 자동 주입하지 않으면 (현재 webhook 코드는 HAMI_VULKAN_ENABLE env 와 NVIDIA_DRIVER_CAPABILITIES patch 만 한다고 추정) — 본 Step A 는 격리 enforce 까지 도달 안 함. **Step A 의 진정한 완료는 webhook 이 LD_PRELOAD + libvgpu.so + hami.json mount 까지 자동 주입하도록 확장**. 이는 webhook backend Go 코드 변경 — 본 plan 의 Task 7 로 추가.

- [ ] **Step 4: pod 정리**

```bash
kubectl -n hami-test delete pod cuda-partition-test
```

- [ ] **Step 5: test manifest commit**

```bash
git add cluster/runtime/test/cuda-partition-test-pod.yaml
git commit -s -m "test(cluster): add cuda-partition-test pod for namespace opt-in 격리 검증"
```

---

### Task 7: webhook mutation 확장 — LD_PRELOAD env + libvgpu.so volume mount + hami.json 자동 주입

**Files:**
- Modify: `pkg/scheduler/webhook/*.go` (mutation 로직)
- Create (chart): `charts/hami/templates/scheduler/hami-vulkan-layer-cm.yaml` (hami.json content for mounting)

이 task 는 webhook backend Go 코드 변경. 본 plan 에서는 **인터페이스 정의 + 단위 test 작성** 까지만, 실제 Go 코드 수정은 Step A 의 후반부 또는 별도 plan 으로 분리.

- [ ] **Step 1: webhook backend 코드 위치 식별**

```bash
cd /Users/xiilab/git/HAMi
find pkg cmd -type f -name "*.go" | xargs grep -lE "MutatingWebhook|admission\\.AdmissionReview|patchOps" 2>/dev/null | head -10
```

Expected: 1개 이상의 .go 파일 출력. 그 파일이 mutation 로직 entry point.

- [ ] **Step 2: 현재 mutation 로직이 무엇을 patch 하는지 확인**

```bash
WEBHOOK_FILE=$(find pkg cmd -type f -name "*.go" | xargs grep -lE "MutatingWebhook|admission\\.AdmissionReview" 2>/dev/null | head -1)
echo "WEBHOOK_FILE=$WEBHOOK_FILE"
grep -n "HAMI_VULKAN_ENABLE\|NVIDIA_DRIVER_CAPABILITIES\|LD_PRELOAD\|libvgpu\|hami\\.json" "$WEBHOOK_FILE"
```

이 단계는 실제 코드 베이스 조사. 결과에 따라 다음 step 의 plan 분리 여부 결정.

- [ ] **Step 3: 결정 게이트**

만약 grep 결과:
- A. webhook 이 **이미** LD_PRELOAD + libvgpu.so mount + hami.json mount 를 주입한다 → Step A 의 Task 6 검증으로 통과 가능. 다음 step (Task 8) 의 진짜 격리 검증 진행.
- B. webhook 이 HAMI_VULKAN_ENABLE env 만 주입하고 LD_PRELOAD/mount 는 안 한다 → **Step A 를 두 sub-plan 으로 분할**:
   - A.1 (본 plan Task 1-6 까지): namespaceSelector opt-in + isaac-launchable baseline 보호
   - A.2 (별도 plan): webhook backend Go 코드에 LD_PRELOAD env + libvgpu.so mount + hami.json mount 주입 추가 — design doc 의 7.2 절 참조

본 plan 에서는 결정만 하고, B 면 별도 plan 으로 분기. A 면 Task 8 으로 진행.

---

### Task 8: 통합 검증 — isaac-launchable baseline 유지 + hami-test namespace 격리 enforce 확인

(Task 7 결정 게이트가 A 인 경우만 실행, B 면 별도 plan)

- [ ] **Step 1: isaac-launchable namespace baseline 재확인**

```bash
NEWPOD=$(kubectl -n isaac-launchable get pod -l app=isaac-launchable,instance=pod-1 -o jsonpath='{.items[0].metadata.name}')
kubectl -n isaac-launchable exec $NEWPOD -c vscode -- bash -lc \
  'nvidia-smi --query-gpu=memory.total --format=csv,noheader; env | grep -E "^(HAMI|LD_PRELOAD|NVIDIA_DRIVER_CAP)"; ls /etc/vulkan/implicit_layer.d/'
```

Expected:
- `46068 MiB` (HAMi 격리 0, raw)
- env 에 `HAMI_VULKAN_ENABLE` 또는 `LD_PRELOAD` 없음
- `/etc/vulkan/implicit_layer.d/` 에 `hami.json` 없음

- [ ] **Step 2: isaac-launchable runheadless.sh 5번 alive 검증**

```bash
NEWPOD=$(kubectl -n isaac-launchable get pod -l app=isaac-launchable,instance=pod-1 -o jsonpath='{.items[0].metadata.name}')
kubectl -n isaac-launchable exec $NEWPOD -c vscode -- bash -lc '
mkdir -p /tmp/baseline
for i in 1 2 3 4 5; do
  pkill -KILL kit 2>/dev/null; sleep 3
  timeout 50 env ACCEPT_EULA=y /isaac-sim/runheadless.sh >/tmp/baseline/r$i.log 2>&1
  EC=$?
  CRASH=$(grep -cE "Segmentation fault|crash has occurred" /tmp/baseline/r$i.log)
  LISTEN=$(ss -tunlp 2>/dev/null | grep -c -E ":49100|:30999")
  echo "run $i: exit=$EC crash=$CRASH listen=$LISTEN"
done
pkill -KILL kit 2>/dev/null
'
```

Expected: 5/5 `exit=124 crash=0 listen>=1`.

- [ ] **Step 3: hami-test namespace 의 cuda-partition-test pod 격리 검증**

(Task 6 의 pod manifest 재배포)

```bash
kubectl apply -f cluster/runtime/test/cuda-partition-test-pod.yaml
sleep 30
kubectl -n hami-test logs cuda-partition-test | grep -E "memory.total|HAMI|LD_PRELOAD|hami.json"
kubectl -n hami-test delete pod cuda-partition-test
```

Expected logs:
- `23552 MiB` (NVML 격리 적용)
- env 에 `LD_PRELOAD=/usr/local/vgpu/libvgpu.so` 또는 `HAMI_VULKAN_ENABLE=1`
- `hami.json` mount 또는 ld.so.preload 활성

- [ ] **Step 4: PR commit/push**

```bash
git add docs/superpowers/specs/2026-04-28-hami-isolation-isaac-sim-design.md \
        docs/superpowers/plans/2026-04-28-hami-isolation-step-a-namespace-opt-in.md \
        charts/hami/values.yaml \
        charts/hami/templates/scheduler/webhook.yaml \
        cluster/runtime/snapshot-2026-04-28/ \
        cluster/runtime/test/cuda-partition-test-pod.yaml
git status --short
git log --oneline -10
git push xiilab feat/vulkan-vgpu
```

Expected: push 성공.

- [ ] **Step 5: PR #1803 follow-up 코멘트 등록 — Step A 완료 보고**

```bash
cat > /tmp/pr1803_step_a_done.md <<'EOF'
## Step A complete — Namespace opt-in/out for HAMi mutating webhook

Switches the webhook namespaceSelector from opt-out (`hami.io/webhook NotIn ignore`) to opt-in (`hami.io/vgpu In enabled`). Clusters that mix HAMi vGPU isolation with NVIDIA Omniverse / Isaac Sim Kit workloads can now keep Isaac Sim namespaces unmutated (no LD_PRELOAD inject, no implicit Vulkan layer manifest) while other namespaces explicitly opt in for full isolation.

### Verification

isaac-launchable namespace (no `hami.io/vgpu` label):
- `nvidia-smi memory.total` = 46068 MiB (HAMi inject 0)
- `runheadless.sh` 5/5 alive + listen 49100/30999
- baseline restored to the working state from before the 4-27 dawn patch

hami-test namespace (`hami.io/vgpu=enabled`):
- Webhook mutation applied
- `nvidia-smi memory.total` = 23552 MiB (NVML hook active)
- LD_PRELOAD / hami.json mount injected (when Task 7 decision gate is A)

### Spec / plan

- spec: `docs/superpowers/specs/2026-04-28-hami-isolation-isaac-sim-design.md`
- plan: `docs/superpowers/plans/2026-04-28-hami-isolation-step-a-namespace-opt-in.md`

Step B (HAMi-core hook hardening) and Step C (Vulkan layer compat) follow in separate plans so that isaac-launchable can eventually opt-in for full isolation (Step D) once the hook code is hardened to coexist with Carbonite/OptiX/Vulkan layer chain.
EOF
gh api repos/Project-HAMi/HAMi/issues/1803/comments -X POST -f body="$(cat /tmp/pr1803_step_a_done.md)" --jq '.html_url'
```

Expected: PR comment URL 출력.

---

## Self-Review

**1. Spec coverage 점검:**
- Spec §7 (Step A) 의 webhook namespaceSelector 변경 → Tasks 2, 3, 4 ✅
- Spec §7 의 노드 wide ld.so.preload 폐기 → Task 5 ✅
- Spec §7 의 hami-vulkan-manifest-installer 폐기 → Task 5 ✅
- Spec §7 의 LD_PRELOAD env / volume mount webhook 자동 주입 → Task 7 (결정 게이트, 별도 plan 가능성)
- Spec §7.4 의 검증 (isaac-launchable baseline + 새 namespace 격리) → Tasks 6, 8 ✅
- Spec §11 의 위험 (helm release 영향) → Task 4 가 cluster 직접 patch 로 우회 ✅

**2. Placeholder scan:** "TBD"/"TODO"/"implement later" 검색 — 본 plan 에 없음 ✅. 단 Task 7 의 결정 게이트가 webhook 코드 조사 후 분기 — 이는 placeholder 가 아니라 명시적 decision point.

**3. Type consistency:** `hami.io/vgpu=enabled` label key/value 가 Tasks 2, 3, 4, 6, 8 에서 일관 사용 ✅. `hami.io/webhook=ignore` 는 legacy 로 명시적 표시 ✅.

**4. Scope check:** Step A 만 다룸. Step B/C/D 는 별도 plan 명시 ✅. 단 Task 7 이 webhook backend Go 코드 변경 가능성 → 별도 plan 분기 명시.

---

## Open question (실행 시 결정)

**Task 7 의 결정 게이트** 가 A (현재 webhook 이 이미 LD_PRELOAD + mount 주입) 인지 B (env 만 주입, 코드 확장 필요) 인지 — Task 7 Step 1-2 실행 후 결정. B 면 본 plan 의 Task 8 은 별도 sub-plan A.2 로 분리.
