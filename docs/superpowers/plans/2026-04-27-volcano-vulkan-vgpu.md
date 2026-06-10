# Volcano + Vulkan vGPU 통합 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Volcano scheduler 가 운영 중인 클러스터에 HAMi 의 Vulkan vGPU 메모리 partitioning 기능을 통합한다. `xiilab/volcano-vgpu-device-plugin` 의 libvgpu 를 vulkan-layer 가 들어간 HAMi-core 로 교체하고, device-plugin Allocate 에 manifest auto-mount 코드를 추가하며, HAMi 의 mutating webhook 만 별도 helm install 한다.

**Architecture:** HAMi 본가의 commit `0150ea7` (manifest auto-inject) 패턴을 그대로 fork 에 포팅한다. Dockerfile 의 builder stage 가 manifest 파일을 image 에 ship 하고, vgpu-init script 이 host 에 복사하며, device-plugin 의 `Allocate()` 가 host 파일이 존재하면 container 에 bind-mount 한다. webhook 은 HAMi 본가 helm chart 으로 별도 install 하여 annotation 처리만 담당.

**Tech Stack:** Go 1.21+, Kubernetes device-plugin v1beta1, NVIDIA Vulkan Loader, libvgpu (HAMi-core vulkan-layer), helm 3, Volcano scheduler.

**Spec:** `docs/superpowers/specs/2026-04-27-volcano-vulkan-vgpu-design.md`

## File Structure

작업은 두 repo 에 걸친다.

### `xiilab/volcano-vgpu-device-plugin` (PR-1)

| 파일 | 역할 | 변경 |
|---|---|---|
| `libvgpu` (submodule) | HAMi-core (vulkan-layer 포함) | submodule SHA 갱신 |
| `docker/Dockerfile` | image 빌드. builder stage 에 libvulkan-dev 추가, runtime stage 에 hami.json ship | 2 줄 추가 |
| `pkg/.../plugin/server.go` (또는 동등 위치) | device-plugin Allocate 응답 빌더 | 17 줄 추가 (manifest mount) |
| `volcano-vgpu-device-plugin.yml` | standard mode deploy yaml | image tag 갱신 |
| `volcano-vgpu-device-plugin-cdi.yml` | CDI mode deploy yaml | image tag 갱신 |
| `volcano-vgpu-vulkan-manifest.yml` (NEW) | host 측 manifest 파일 사전 배치 (별도 DaemonSet, fallback) | 신규 |
| `examples/vulkan-pod.yaml` (NEW) | E2E 테스트용 sample pod | 신규 |
| `doc/vulkan-vgpu.md` (NEW) | 사용 가이드 | 신규 |

### HAMi 본가 (변경 없음)

helm chart 의 `values.yaml` 으로 webhook only 모드 install. PR 없음.

---

## Task 1: 작업 환경 준비 — repo clone + 브랜치 생성

**Files:**
- Clone: `~/git/volcano-vgpu-device-plugin` (xiilab fork)
- Branch: `feat/vulkan-vgpu-support`

- [ ] **Step 1: Clone xiilab fork**

```bash
cd ~/git
git clone https://github.com/xiilab/volcano-vgpu-device-plugin.git
cd volcano-vgpu-device-plugin
git remote add upstream https://github.com/Project-HAMi/volcano-vgpu-device-plugin.git
git fetch upstream
```

- [ ] **Step 2: 새 브랜치 생성**

```bash
cd ~/git/volcano-vgpu-device-plugin
git checkout -b feat/vulkan-vgpu-support
git submodule update --init --recursive
```

- [ ] **Step 3: 현재 libvgpu submodule SHA 기록**

```bash
cd ~/git/volcano-vgpu-device-plugin
git -C libvgpu rev-parse HEAD
# Expected: 6660c84... (or whatever the current submodule pin is)
```

기록한 SHA 를 노트해 두기 (나중 회귀 비교용).

- [ ] **Step 4: server.go 위치 파악**

```bash
cd ~/git/volcano-vgpu-device-plugin
grep -rln "func.*Allocate.*kubeletdevicepluginv1beta1" pkg/ cmd/ 2>/dev/null
```

찾은 경로를 노트. 이후 task 들에서 이 경로를 사용 (예시 가정: `pkg/plugin/server.go` 또는 `pkg/util/util.go`).

- [ ] **Step 5: 빌드 환경 검증 (변경 없는 상태)**

```bash
cd ~/git/volcano-vgpu-device-plugin
make build 2>&1 | tail -10
```

Expected: 성공. 만약 실패하면 일단 task 진행 멈추고 master 의 build 상태부터 정상화.

- [ ] **Step 6: Commit (브랜치 시작 마커)**

```bash
git commit --allow-empty -m "chore: start feat/vulkan-vgpu-support branch"
```

---

## Task 2: libvgpu submodule 을 vulkan-layer 가 포함된 SHA 로 갱신

**Files:**
- Modify: `libvgpu` submodule pointer
- Modify: `.gitmodules` (이미 vulkan-layer branch 추적 중인지 확인, 필요 시 변경)

- [ ] **Step 1: HAMi 가 사용하는 libvgpu SHA 기록**

```bash
cd ~/git/HAMi
git -C libvgpu rev-parse HEAD
# Expected: 8d4f712... (cuMemFree[Async] untracked-pointer fallback 포함)
```

이 SHA 를 `LIBVGPU_VULKAN_SHA` 로 노트 (이후 step 에서 사용).

- [ ] **Step 2: volcano-vgpu-device-plugin 의 libvgpu remote 가 HAMi-core 의 vulkan-layer branch 를 가리키는지 확인**

```bash
cd ~/git/volcano-vgpu-device-plugin
cat .gitmodules
```

기대값:

```
[submodule "libvgpu"]
	path = libvgpu
	url = https://github.com/Project-HAMi/HAMi-core.git
```

만약 url 이 HAMi 본가 외 fork (e.g., xiilab/HAMi-core) 인 경우, 우리가 사용 중인 vulkan-layer 가 어느 fork 에서 오는지에 맞춰 갱신 필요. HAMi 본가 fork 가 vulkan-layer branch 를 보유하면 그대로 사용. 없으면 xiilab fork 추가:

```bash
git submodule set-url libvgpu https://github.com/xiilab/HAMi-core.git
```

- [ ] **Step 3: submodule 을 LIBVGPU_VULKAN_SHA 로 fast-forward**

```bash
cd ~/git/volcano-vgpu-device-plugin/libvgpu
git fetch origin
git checkout 8d4f712df2941d9314f534bac0038c2f8b7be41f  # LIBVGPU_VULKAN_SHA
cd ..
git add libvgpu
git status
```

기대값: `modified: libvgpu (new commits)` 만 표시.

- [ ] **Step 4: vulkan layer 소스가 들어왔는지 확인**

```bash
cd ~/git/volcano-vgpu-device-plugin
ls libvgpu/src/vulkan/
ls libvgpu/etc/vulkan/implicit_layer.d/
```

기대값: `src/vulkan/` 에 `budget.c`, `loader_intercept.c` 등 존재. `etc/vulkan/implicit_layer.d/hami.json` 존재.

- [ ] **Step 5: Commit submodule 갱신**

```bash
cd ~/git/volcano-vgpu-device-plugin
git commit -m "deps: bump libvgpu to 8d4f712 (vulkan-layer support)"
```

---

## Task 3: Dockerfile builder stage 에 libvulkan-dev 추가

**Files:**
- Modify: `docker/Dockerfile` (빌더 stage 의 apt install 라인)

- [ ] **Step 1: 현재 nvbuild stage 의 apt install 라인 확인**

```bash
cd ~/git/volcano-vgpu-device-plugin
grep -n -E "(FROM .* AS nvbuild|apt|apt-get install)" docker/Dockerfile | head -10
```

이전에 어떤 packages 가 설치되는지 파악. nvbuild stage 가 libvgpu 를 빌드하는 stage.

- [ ] **Step 2: Dockerfile 의 nvbuild stage apt install 에 libvulkan-dev 추가**

다음 형태 (HAMi commit `50b37ff` 와 동일한 수정):

```dockerfile
# nvbuild stage 안의 기존
RUN apt-get update && apt-get install -y \
    cmake \
    make \
    g++ \
    git \
    libvulkan-dev   # ← 신규 라인
```

이미 `libvulkan-dev` 가 설치되어 있으면 skip.

- [ ] **Step 3: 빌드 검증 (Dockerfile syntax)**

```bash
cd ~/git/volcano-vgpu-device-plugin
docker build -f docker/Dockerfile -t volcano-vgpu-device-plugin:vulkan-test . 2>&1 | tail -20
```

기대값: 성공. libvgpu 의 vulkan source 도 함께 컴파일되어야 함. 만약 `vulkan_core.h: No such file` 류의 에러가 나면 libvulkan-dev 가 제대로 install 안 됐거나 PATH 미스.

- [ ] **Step 4: Commit Dockerfile 변경**

```bash
cd ~/git/volcano-vgpu-device-plugin
git add docker/Dockerfile
git commit -m "build: install libvulkan-dev in nvbuild stage for Vulkan layer compile"
```

---

## Task 4: Dockerfile 의 runtime stage 에 hami.json ship

**Files:**
- Modify: `docker/Dockerfile` (runtime stage 의 COPY 라인)

- [ ] **Step 1: 현재 runtime stage 의 libvgpu.so COPY 라인 확인**

```bash
cd ~/git/volcano-vgpu-device-plugin
grep -n "libvgpu.so" docker/Dockerfile
```

기대값: `COPY --from=nvbuild /libvgpu/build/libvgpu.so ...` 같은 라인.

- [ ] **Step 2: 그 라인 직후에 hami.json COPY 추가**

HAMi commit `0150ea7` 와 동일한 한 줄:

```dockerfile
COPY --from=nvbuild /libvgpu/build/libvgpu.so /k8s-vgpu/lib/nvidia/libvgpu.so."$VERSION"
COPY --from=nvbuild /libvgpu/etc/vulkan/implicit_layer.d/hami.json /k8s-vgpu/lib/nvidia/vulkan/implicit_layer.d/hami.json
```

> **Note:** volcano-vgpu-device-plugin 의 path 가 HAMi 의 `/k8s-vgpu/lib/nvidia/` 와 다를 수 있다. Task 1 Step 4 에서 파악한 위치에 맞게 prefix 조정. 일반적으로 같은 prefix.

- [ ] **Step 3: 빌드 검증 + image 안 hami.json 존재 확인**

```bash
cd ~/git/volcano-vgpu-device-plugin
docker build -f docker/Dockerfile -t volcano-vgpu-device-plugin:vulkan-test . 2>&1 | tail -5
docker run --rm --entrypoint /bin/sh volcano-vgpu-device-plugin:vulkan-test \
    -c "ls -la /k8s-vgpu/lib/nvidia/vulkan/implicit_layer.d/hami.json && cat /k8s-vgpu/lib/nvidia/vulkan/implicit_layer.d/hami.json"
```

기대값: 파일 존재 + JSON 내용 출력 (`VK_LAYER_HAMI_vgpu`, `enable_environment: HAMI_VULKAN_ENABLE=1` 등 포함).

- [ ] **Step 4: Commit**

```bash
cd ~/git/volcano-vgpu-device-plugin
git add docker/Dockerfile
git commit -m "feat(image): ship Vulkan implicit layer manifest from libvgpu"
```

---

## Task 5: vgpu-init.sh (또는 동등 init script) 가 host 에 manifest 복사하는지 확인

**Files:**
- Inspect: `docker/vgpu-init.sh` (또는 동등)

- [ ] **Step 1: vgpu-init.sh 위치 확인**

```bash
cd ~/git/volcano-vgpu-device-plugin
find . -name "vgpu-init.sh" -o -name "init.sh" 2>/dev/null | head
```

- [ ] **Step 2: init script 의 host 복사 로직 확인**

```bash
cat docker/vgpu-init.sh   # 또는 발견된 path
```

기대 패턴: `cp -r /k8s-vgpu/lib/nvidia/* /usr/local/vgpu/` 또는 동등 (recursive copy 로 vulkan/implicit_layer.d/hami.json 도 함께 host 에 복사됨).

- [ ] **Step 3: 만약 init script 이 recursive copy 가 아니면 명시적 라인 추가**

`/k8s-vgpu/lib/nvidia/vulkan/implicit_layer.d/hami.json` 을 `/usr/local/vgpu/vulkan/implicit_layer.d/hami.json` 으로 복사하는 라인 추가 (mkdir -p 포함):

```bash
mkdir -p /usr/local/vgpu/vulkan/implicit_layer.d
cp -f /k8s-vgpu/lib/nvidia/vulkan/implicit_layer.d/hami.json \
      /usr/local/vgpu/vulkan/implicit_layer.d/hami.json
```

이미 recursive copy 로 cover 되면 변경 없음.

- [ ] **Step 4: 변경 있으면 commit**

```bash
cd ~/git/volcano-vgpu-device-plugin
git add docker/vgpu-init.sh
git commit -m "build(init): copy Vulkan manifest to host during vgpu-init"
```

---

## Task 6: device-plugin 의 Allocate 에 manifest mount 코드 추가

**Files:**
- Modify: Task 1 Step 4 에서 발견한 server.go 위치 (가정: `pkg/plugin/server.go`)

- [ ] **Step 1: Allocate 함수 안의 license mount 라인 (앵커) 위치 찾기**

```bash
cd ~/git/volcano-vgpu-device-plugin
grep -n "license" pkg/plugin/server.go   # 또는 발견된 server.go path
```

HAMi 의 `0150ea7` 는 license mount 직전에 vulkan manifest mount 를 추가했다. 같은 앵커 라인 위에 추가.

- [ ] **Step 2: server.go 에 manifest mount 코드 추가**

HAMi 본가 commit `0150ea7` 의 server.go 패치를 그대로 포팅. 정확한 코드:

```go
// Mount Vulkan implicit layer manifest so the HAMi Vulkan layer
// activates for pods that set HAMI_VULKAN_ENABLE=1 (done by the
// webhook when the pod carries hami.io/vulkan="true").
// The manifest file is placed on the host by vgpu-init.sh as part
// of the standard lib distribution; skip the mount if it is
// absent so we do not block pod startup on nodes that have not
// yet been populated.
vulkanManifestHost := hostHookPath + "/vgpu/vulkan/implicit_layer.d/hami.json"
if _, err := os.Stat(vulkanManifestHost); err == nil {
    response.Mounts = append(response.Mounts, &kubeletdevicepluginv1beta1.Mount{
        ContainerPath: "/etc/vulkan/implicit_layer.d/hami.json",
        HostPath:      vulkanManifestHost,
        ReadOnly:      true,
    })
}
```

> **Note:** `hostHookPath` 변수 이름이 volcano-vgpu-device-plugin 에서 다를 수 있다 (`hostMountPath`, `vgpuPath` 등). HAMi 의 정의는 일반적으로 `/usr/local/vgpu` 기본값. fork 의 동등 변수 이름으로 대체.

- [ ] **Step 3: import 확인**

`os.Stat` 사용하므로 `os` import 가 이미 있어야 한다 (다른 mount 코드에서 사용 중일 가능성 큼). 만약 없으면 추가:

```go
import (
    "os"
    // existing imports...
)
```

- [ ] **Step 4: 빌드 검증**

```bash
cd ~/git/volcano-vgpu-device-plugin
go build ./... 2>&1 | head -20
```

기대값: error 0. `hostHookPath` 가 정의되지 않았거나 `kubeletdevicepluginv1beta1` import 누락이면 컴파일 실패 → 변수 이름 또는 import 조정.

- [ ] **Step 5: 단위 테스트 — manifest 파일 존재/부재 시나리오 (TDD)**

server.go 와 같은 패키지에 `server_vulkan_test.go` 생성:

```go
package plugin

import (
    "os"
    "path/filepath"
    "testing"
)

func TestVulkanManifestMount_Present(t *testing.T) {
    tmp := t.TempDir()
    // manifest 파일 사전 배치
    manifestDir := filepath.Join(tmp, "vgpu", "vulkan", "implicit_layer.d")
    if err := os.MkdirAll(manifestDir, 0755); err != nil {
        t.Fatal(err)
    }
    manifestPath := filepath.Join(manifestDir, "hami.json")
    if err := os.WriteFile(manifestPath, []byte("{}"), 0644); err != nil {
        t.Fatal(err)
    }

    // hostHookPath = tmp 라고 가정하고 mount 빌더 호출 (실제 함수 이름은 fork 에 맞춰 조정)
    mounts := buildVulkanManifestMount(tmp)
    if len(mounts) != 1 {
        t.Fatalf("expected 1 mount, got %d", len(mounts))
    }
    if mounts[0].ContainerPath != "/etc/vulkan/implicit_layer.d/hami.json" {
        t.Errorf("unexpected ContainerPath: %s", mounts[0].ContainerPath)
    }
    if mounts[0].HostPath != manifestPath {
        t.Errorf("unexpected HostPath: %s", mounts[0].HostPath)
    }
    if !mounts[0].ReadOnly {
        t.Error("expected ReadOnly=true")
    }
}

func TestVulkanManifestMount_Absent(t *testing.T) {
    tmp := t.TempDir()
    // 파일 없음 — mount 응답에 추가하지 말아야 함
    mounts := buildVulkanManifestMount(tmp)
    if len(mounts) != 0 {
        t.Errorf("expected 0 mounts when manifest absent, got %d", len(mounts))
    }
}
```

함수 추출이 어렵다면 (인라인 코드라면) 일단 본 테스트는 skip 하고 Step 7 의 통합 검증으로 대체.

- [ ] **Step 6: 테스트 실행 (실행 가능한 경우)**

```bash
cd ~/git/volcano-vgpu-device-plugin
go test ./pkg/plugin/ -run TestVulkanManifestMount -v
```

기대값: 두 testcase 모두 PASS.

만약 `buildVulkanManifestMount` 함수가 없으면 (인라인 코드라면) Step 5 에서 함수 추출 + Step 6 PASS. 함수 추출은 server.go 의 manifest mount 블록을 다음 형태로 분리:

```go
func buildVulkanManifestMount(hostHookPath string) []*kubeletdevicepluginv1beta1.Mount {
    vulkanManifestHost := hostHookPath + "/vgpu/vulkan/implicit_layer.d/hami.json"
    if _, err := os.Stat(vulkanManifestHost); err != nil {
        return nil
    }
    return []*kubeletdevicepluginv1beta1.Mount{{
        ContainerPath: "/etc/vulkan/implicit_layer.d/hami.json",
        HostPath:      vulkanManifestHost,
        ReadOnly:      true,
    }}
}
```

그리고 Allocate 안에서:

```go
response.Mounts = append(response.Mounts, buildVulkanManifestMount(hostHookPath)...)
```

- [ ] **Step 7: Commit**

```bash
cd ~/git/volcano-vgpu-device-plugin
git add pkg/plugin/server.go pkg/plugin/server_vulkan_test.go
git commit -m "feat(plugin): auto-inject Vulkan implicit layer manifest mount"
```

---

## Task 7: 기존 deploy yaml 두 개의 image tag 갱신

**Files:**
- Modify: `volcano-vgpu-device-plugin.yml`
- Modify: `volcano-vgpu-device-plugin-cdi.yml`

- [ ] **Step 1: 현재 image tag 확인**

```bash
cd ~/git/volcano-vgpu-device-plugin
grep -nE "image:.*volcano-vgpu" volcano-vgpu-device-plugin.yml volcano-vgpu-device-plugin-cdi.yml
```

기대값 (예시): `image: projecthami/volcano-vgpu-device-plugin:v1.10.0`

- [ ] **Step 2: 새 tag 결정**

`vulkan-v1` 또는 `v1.10.0-vulkan-v1` 같은 명확한 tag.

- [ ] **Step 3: yaml 두 개의 image 라인 갱신**

```bash
cd ~/git/volcano-vgpu-device-plugin
sed -i.bak 's|image: projecthami/volcano-vgpu-device-plugin:.*|image: 10.61.3.124:30002/library/volcano-vgpu-device-plugin:vulkan-v1|' \
    volcano-vgpu-device-plugin.yml volcano-vgpu-device-plugin-cdi.yml
rm -f *.yml.bak
git diff volcano-vgpu-device-plugin*.yml
```

> **Note:** sed pattern 의 source 부분 (`projecthami/...`) 은 Step 1 에서 본 실제 image 와 일치해야 한다. 달라지면 그에 맞게 조정.

- [ ] **Step 4: Commit**

```bash
cd ~/git/volcano-vgpu-device-plugin
git add volcano-vgpu-device-plugin.yml volcano-vgpu-device-plugin-cdi.yml
git commit -m "chore: bump image to vulkan-v1 in deploy yaml"
```

---

## Task 8: 신규 yaml — host manifest 사전 배치 (fallback DaemonSet)

**Files:**
- Create: `volcano-vgpu-vulkan-manifest.yml`

> **Note:** Task 4-5 의 device-plugin image 가 이미 init script 으로 manifest 를 host 에 배치하므로, **이 DaemonSet 은 fallback** 이다. 노드에 이미 device-plugin DaemonSet 이 떠 있으면 manifest 가 자동 배치되지만, 별도 환경 (e.g., device-plugin 갱신 전, 또는 다른 distribution mechanism 사용 시) 을 위해 standalone 으로 배치 가능.

- [ ] **Step 1: 파일 생성**

```bash
cd ~/git/volcano-vgpu-device-plugin
cat > volcano-vgpu-vulkan-manifest.yml <<'EOF'
# HAMi Vulkan implicit layer manifest 를 host 노드의
# /usr/local/vgpu/vulkan/implicit_layer.d/hami.json 으로 배치하는 DaemonSet.
# device-plugin image 의 vgpu-init.sh 가 이미 같은 작업을 하므로 일반적으로 불필요.
# device-plugin 갱신 전 또는 별도 init 시나리오용 fallback.
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: hami-vulkan-manifest
  namespace: kube-system
data:
  hami.json: |
    {
        "file_format_version": "1.0.0",
        "layer": {
            "name": "VK_LAYER_HAMI_vgpu",
            "type": "GLOBAL",
            "library_path": "/usr/local/vgpu/libvgpu.so",
            "api_version": "1.3.0",
            "implementation_version": "1",
            "description": "HAMi Vulkan vGPU memory partitioning layer",
            "enable_environment": {
                "HAMI_VULKAN_ENABLE": "1"
            }
        }
    }
---
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: hami-vulkan-manifest-installer
  namespace: kube-system
  labels:
    app: hami-vulkan-manifest-installer
spec:
  selector:
    matchLabels:
      app: hami-vulkan-manifest-installer
  template:
    metadata:
      labels:
        app: hami-vulkan-manifest-installer
    spec:
      tolerations:
      - operator: Exists
      nodeSelector:
        nvidia.com/gpu.present: "true"
      hostPID: false
      restartPolicy: Always
      containers:
      - name: installer
        image: busybox:1.36
        command:
        - /bin/sh
        - -c
        - |
          set -eu
          mkdir -p /host/usr/local/vgpu/vulkan/implicit_layer.d
          cp -f /manifest/hami.json \
                /host/usr/local/vgpu/vulkan/implicit_layer.d/hami.json
          echo "[hami-vulkan-manifest] installed at /usr/local/vgpu/vulkan/implicit_layer.d/hami.json"
          # 종료하지 않고 sleep — DaemonSet 이라 restart 루프 회피
          sleep infinity
        volumeMounts:
        - name: manifest
          mountPath: /manifest
          readOnly: true
        - name: host-vgpu
          mountPath: /host/usr/local/vgpu
        securityContext:
          runAsUser: 0
      volumes:
      - name: manifest
        configMap:
          name: hami-vulkan-manifest
      - name: host-vgpu
        hostPath:
          path: /usr/local/vgpu
          type: DirectoryOrCreate
EOF
```

- [ ] **Step 2: yaml syntax 검증**

```bash
cd ~/git/volcano-vgpu-device-plugin
kubectl apply --dry-run=client -f volcano-vgpu-vulkan-manifest.yml
```

기대값:

```
configmap/hami-vulkan-manifest created (dry run)
daemonset.apps/hami-vulkan-manifest-installer created (dry run)
```

- [ ] **Step 3: Commit**

```bash
cd ~/git/volcano-vgpu-device-plugin
git add volcano-vgpu-vulkan-manifest.yml
git commit -m "feat(deploy): add fallback DaemonSet for Vulkan manifest placement"
```

---

## Task 9: 사용 예시 yaml + 사용 가이드 문서

**Files:**
- Create: `examples/vulkan-pod.yaml`
- Create: `doc/vulkan-vgpu.md`

- [ ] **Step 1: examples/vulkan-pod.yaml 생성**

```bash
cd ~/git/volcano-vgpu-device-plugin
mkdir -p examples
cat > examples/vulkan-pod.yaml <<'EOF'
# HAMi Vulkan vGPU 분할 활성화 예시 pod.
# - annotation `hami.io/vulkan: "true"` 가 HAMi mutating webhook 을 통해
#   `HAMI_VULKAN_ENABLE=1` 와 NVIDIA_DRIVER_CAPABILITIES 의 graphics 캡을 주입.
# - device-plugin 이 hami.json 을 자동 mount 하여 Vulkan loader 가 layer 인식.
# - libvgpu (HAMi-core) 의 vkAllocateMemory 후킹이 nvidia.com/gpumem 한계 enforce.
apiVersion: v1
kind: Pod
metadata:
  name: vulkan-vgpu-demo
  annotations:
    hami.io/vulkan: "true"
spec:
  schedulerName: volcano
  containers:
  - name: vulkan-app
    image: nvidia/cuda:12.2.0-runtime-ubuntu22.04
    command: ["sleep", "infinity"]
    resources:
      limits:
        nvidia.com/gpu: 1
        nvidia.com/gpumem: 4000  # MiB
        nvidia.com/gpucores: 50  # %
EOF
```

- [ ] **Step 2: doc/vulkan-vgpu.md 생성**

```bash
cd ~/git/volcano-vgpu-device-plugin
cat > doc/vulkan-vgpu.md <<'EOF'
# Vulkan vGPU 지원

이 device-plugin 은 CUDA workload 와 동일하게 **Vulkan workload** 도 메모리 partitioning 을 enforce 한다. Volcano scheduler 와 함께 사용한다.

## 작동 원리

1. **libvgpu (HAMi-core) vulkan-layer**: `vkAllocateMemory` 를 후킹하여 `CUDA_DEVICE_MEMORY_LIMIT_0` 를 enforce.
2. **device-plugin Allocate**: 호스트의 `/usr/local/vgpu/vulkan/implicit_layer.d/hami.json` 이 존재하면 container 의 `/etc/vulkan/implicit_layer.d/hami.json` 으로 bind-mount.
3. **HAMi mutating webhook (별도 install)**: pod annotation `hami.io/vulkan: "true"` 검사 → `HAMI_VULKAN_ENABLE=1` env + `NVIDIA_DRIVER_CAPABILITIES` 에 `graphics` 추가.
4. **enable_environment 가드**: manifest 의 `enable_environment: HAMI_VULKAN_ENABLE=1` 매치 시에만 layer 로드. annotation 없는 pod 은 영향 없음.

## 설치 (한 번만)

### 1. device-plugin 갱신 (이미 새 image)

```bash
kubectl apply -f volcano-vgpu-device-plugin.yml
# 또는 CDI 모드:
# kubectl apply -f volcano-vgpu-device-plugin-cdi.yml
```

### 2. HAMi mutating webhook 별도 install (helm)

```bash
helm repo add hami https://project-hami.github.io/HAMi
helm install hami-webhook hami/hami \
    --namespace kube-system \
    --set devicePlugin.enabled=false \
    --set scheduler.kubeScheduler.enabled=false \
    --set scheduler.extender.enabled=false \
    --set admissionWebhook.enabled=true
```

### 3. (선택) Fallback manifest DaemonSet

device-plugin 이 init 으로 manifest 를 host 에 자동 배치하지 못하는 환경에서:

```bash
kubectl apply -f volcano-vgpu-vulkan-manifest.yml
```

## 사용

pod 에 annotation `hami.io/vulkan: "true"` + `nvidia.com/gpumem` resource limit 추가:

```yaml
apiVersion: v1
kind: Pod
metadata:
  annotations:
    hami.io/vulkan: "true"
spec:
  containers:
  - name: vulkan-app
    image: <Vulkan 사용 image>
    resources:
      limits:
        nvidia.com/gpu: 1
        nvidia.com/gpumem: 4000
```

전체 예시: `examples/vulkan-pod.yaml`

## 검증

container 안에서:

```bash
# 1. env 주입 확인
env | grep -E '(HAMI_VULKAN|DRIVER_CAPABILITIES)'
# 기대: HAMI_VULKAN_ENABLE=1, NVIDIA_DRIVER_CAPABILITIES=...,graphics

# 2. manifest 파일 mount 확인
ls /etc/vulkan/implicit_layer.d/hami.json

# 3. Vulkan tool 로 GPU memory limit 확인 (Vulkan app 실행 시)
# 예: Isaac Sim Kit boot log 의 'GPU Memory: <limit> MB'
```
EOF
```

- [ ] **Step 3: yaml syntax 검증**

```bash
cd ~/git/volcano-vgpu-device-plugin
kubectl apply --dry-run=client -f examples/vulkan-pod.yaml
```

기대값: `pod/vulkan-vgpu-demo created (dry run)`.

- [ ] **Step 4: Commit**

```bash
cd ~/git/volcano-vgpu-device-plugin
git add examples/vulkan-pod.yaml doc/vulkan-vgpu.md
git commit -m "docs(vulkan): usage guide + sample pod"
```

---

## Task 10: image 빌드 + harbor push

**Files:**
- (없음 — 운영 작업)

- [ ] **Step 1: 빌더 머신 (ws-node074 = 10.61.3.74) 으로 코드 sync**

```bash
# 로컬 (mac) 에서
cd ~/git/volcano-vgpu-device-plugin
git push origin feat/vulkan-vgpu-support
```

빌더 머신 측:

```bash
ssh root@10.61.3.74 'cd /root && \
  git clone https://github.com/xiilab/volcano-vgpu-device-plugin.git volcano-vgpu-device-plugin-vulkan 2>/dev/null || true; \
  cd /root/volcano-vgpu-device-plugin-vulkan && \
  git fetch origin && git checkout feat/vulkan-vgpu-support && git submodule update --init --recursive'
```

- [ ] **Step 2: 빌더 머신에서 image 빌드 + push**

```bash
ssh root@10.61.3.74 'cd /root/volcano-vgpu-device-plugin-vulkan && \
  docker build -f docker/Dockerfile \
    -t 10.61.3.124:30002/library/volcano-vgpu-device-plugin:vulkan-v1 . && \
  docker push 10.61.3.124:30002/library/volcano-vgpu-device-plugin:vulkan-v1'
```

기대값: 마지막에 `digest: sha256:... size: ...` 출력.

- [ ] **Step 3: image 정상 push 검증**

```bash
ssh root@10.61.3.74 'docker pull 10.61.3.124:30002/library/volcano-vgpu-device-plugin:vulkan-v1 && \
  docker run --rm --entrypoint /bin/sh \
    10.61.3.124:30002/library/volcano-vgpu-device-plugin:vulkan-v1 \
    -c "ls /k8s-vgpu/lib/nvidia/vulkan/implicit_layer.d/hami.json"'
```

기대값: `/k8s-vgpu/lib/nvidia/vulkan/implicit_layer.d/hami.json` 출력 (파일 존재 확인).

---

## Task 11: 클러스터 deploy

**Files:**
- (없음 — 운영 작업)

- [ ] **Step 1: 신규 manifest DaemonSet apply (fallback, 권장)**

```bash
kubectl --context=<volcano-cluster> apply -f volcano-vgpu-vulkan-manifest.yml
```

기대값:
```
configmap/hami-vulkan-manifest created
daemonset.apps/hami-vulkan-manifest-installer created
```

- [ ] **Step 2: DaemonSet pod 들 Ready 대기 + manifest 파일 host 에 배치 확인**

```bash
until kubectl --context=<volcano-cluster> -n kube-system get ds hami-vulkan-manifest-installer \
    -o jsonpath='{.status.numberReady}/{.status.desiredNumberScheduled}{"\n"}' 2>/dev/null \
    | grep -q "^[1-9].*/[1-9]"; do sleep 3; done

# host 에 파일 있는지 (DaemonSet pod 안에서)
kubectl --context=<volcano-cluster> -n kube-system get pod -l app=hami-vulkan-manifest-installer \
    -o name | head -1 | xargs -I{} kubectl --context=<volcano-cluster> -n kube-system exec {} -- \
    ls -la /host/usr/local/vgpu/vulkan/implicit_layer.d/hami.json
```

기대값: 파일 존재.

- [ ] **Step 3: device-plugin DaemonSet 갱신 (rolling update)**

```bash
kubectl --context=<volcano-cluster> apply -f volcano-vgpu-device-plugin.yml
# 또는 CDI:
# kubectl --context=<volcano-cluster> apply -f volcano-vgpu-device-plugin-cdi.yml
```

- [ ] **Step 4: device-plugin pod ready 대기 + new image 사용 확인**

```bash
until kubectl --context=<volcano-cluster> -n kube-system get ds volcano-vgpu-device-plugin \
    -o jsonpath='{.status.numberReady}/{.status.desiredNumberScheduled}{"\n"}' 2>/dev/null \
    | grep -q "^[1-9].*/[1-9]"; do sleep 3; done

kubectl --context=<volcano-cluster> -n kube-system get pod -l app=volcano-vgpu-device-plugin \
    -o jsonpath='{.items[*].spec.containers[*].image}{"\n"}'
```

기대값: 모든 pod 의 image 가 `10.61.3.124:30002/library/volcano-vgpu-device-plugin:vulkan-v1`.

---

## Task 12: HAMi webhook 별도 install (helm)

**Files:**
- (없음 — 운영 작업)

- [ ] **Step 1: HAMi helm repo 추가**

```bash
helm repo add hami https://project-hami.github.io/HAMi
helm repo update
```

- [ ] **Step 2: webhook only values 로 install**

```bash
helm install hami-webhook hami/hami \
    --kube-context <volcano-cluster> \
    --namespace kube-system \
    --set devicePlugin.enabled=false \
    --set scheduler.kubeScheduler.enabled=false \
    --set scheduler.extender.enabled=false \
    --set admissionWebhook.enabled=true
```

- [ ] **Step 3: webhook pod ready 대기**

```bash
until kubectl --context=<volcano-cluster> -n kube-system get deployment \
    hami-webhook 2>/dev/null \
    -o jsonpath='{.status.readyReplicas}/{.status.replicas}{"\n"}' \
    | grep -q "^[1-9].*/[1-9]"; do sleep 3; done
```

> **Note:** 실제 deployment 이름은 helm chart values 에 따라 다를 수 있다. `kubectl get deploy -n kube-system | grep hami` 로 확인.

- [ ] **Step 4: MutatingWebhookConfiguration 등록 확인**

```bash
kubectl --context=<volcano-cluster> get mutatingwebhookconfigurations | grep hami
```

기대값: `hami-webhook` 또는 동등 객체 존재.

---

## Task 13: E2E 검증 — 4 케이스

**Files:**
- Use: `examples/vulkan-pod.yaml`

- [ ] **Step 1: Case 1 — annotation 있는 Vulkan pod 의 partition enforce**

```bash
kubectl --context=<volcano-cluster> apply -f examples/vulkan-pod.yaml
kubectl --context=<volcano-cluster> wait --for=condition=Ready pod/vulkan-vgpu-demo --timeout=60s

# env 주입 확인
kubectl --context=<volcano-cluster> exec vulkan-vgpu-demo -- env | grep -E "(HAMI_VULKAN|DRIVER_CAPABILITIES)"
# 기대: HAMI_VULKAN_ENABLE=1, NVIDIA_DRIVER_CAPABILITIES=...,graphics

# manifest mount 확인
kubectl --context=<volcano-cluster> exec vulkan-vgpu-demo -- ls /etc/vulkan/implicit_layer.d/hami.json
# 기대: 파일 존재

# CUDA_DEVICE_MEMORY_LIMIT 확인 (HAMi-core 환경)
kubectl --context=<volcano-cluster> exec vulkan-vgpu-demo -- env | grep CUDA_DEVICE_MEMORY_LIMIT
# 기대: CUDA_DEVICE_MEMORY_LIMIT_0=4000m
```

- [ ] **Step 2: Case 1 — Vulkan app 실제 메모리 enforce 확인 (Isaac Sim 또는 vulkaninfo)**

Isaac Sim 같은 Vulkan workload pod 에서:

```bash
# Kit boot log 의 GPU Memory 라인 확인
kubectl --context=<volcano-cluster> logs <isaac-sim-pod> | grep "GPU Memory"
# 기대: | 0 | NVIDIA RTX 6000 Ada Generation | Yes: 0 | | 4000 MB | ...
#       (전체 GPU 가 아닌 partition 한계로 표시되어야 함)
```

또는 vulkan tool 로 device memory 조회:

```bash
kubectl --context=<volcano-cluster> exec vulkan-vgpu-demo -- vulkaninfo --summary 2>&1 | grep -i memory
```

- [ ] **Step 3: Case 2 — annotation 없는 Vulkan pod 은 full GPU**

```bash
cat > /tmp/vulkan-noanno.yaml <<'EOF'
apiVersion: v1
kind: Pod
metadata:
  name: vulkan-noanno
spec:
  schedulerName: volcano
  containers:
  - name: vulkan-app
    image: nvidia/cuda:12.2.0-runtime-ubuntu22.04
    command: ["sleep", "infinity"]
    resources:
      limits:
        nvidia.com/gpu: 1
        nvidia.com/gpumem: 4000
EOF
kubectl --context=<volcano-cluster> apply -f /tmp/vulkan-noanno.yaml
kubectl --context=<volcano-cluster> wait --for=condition=Ready pod/vulkan-noanno --timeout=60s

# HAMI_VULKAN_ENABLE 가 없어야 함
kubectl --context=<volcano-cluster> exec vulkan-noanno -- env | grep HAMI_VULKAN_ENABLE || \
    echo "[OK] HAMI_VULKAN_ENABLE not injected"
```

기대값: `[OK] HAMI_VULKAN_ENABLE not injected`. CUDA_DEVICE_MEMORY_LIMIT 는 여전히 4000m (annotation 무관, device-plugin 이 enforce). Vulkan layer 만 안 로드.

- [ ] **Step 4: Case 3 — annotation 있는 CUDA-only pod 동작 정상**

```bash
cat > /tmp/cuda-anno.yaml <<'EOF'
apiVersion: v1
kind: Pod
metadata:
  name: cuda-anno
  annotations:
    hami.io/vulkan: "true"
spec:
  schedulerName: volcano
  containers:
  - name: cuda-app
    image: nvidia/cuda:12.2.0-base-ubuntu22.04
    command: ["nvidia-smi"]
    resources:
      limits:
        nvidia.com/gpu: 1
        nvidia.com/gpumem: 4000
EOF
kubectl --context=<volcano-cluster> apply -f /tmp/cuda-anno.yaml
kubectl --context=<volcano-cluster> wait --for=condition=Ready pod/cuda-anno --timeout=60s 2>/dev/null
sleep 5  # Job-style 종료 기다리기
kubectl --context=<volcano-cluster> logs cuda-anno | head -20
```

기대값: nvidia-smi 출력. `Total memory` 가 4000 MiB (HAMi-core 가 가짜 한계 표시) 또는 정상 GPU 정보. Vulkan 영향 없이 CUDA 동작.

- [ ] **Step 5: Case 4 — 기존 standard CUDA workload 회귀 (annotation 없음, gpumem 만)**

```bash
cat > /tmp/cuda-standard.yaml <<'EOF'
apiVersion: v1
kind: Pod
metadata:
  name: cuda-standard
spec:
  schedulerName: volcano
  containers:
  - name: cuda-app
    image: nvidia/cuda:12.2.0-base-ubuntu22.04
    command: ["nvidia-smi"]
    resources:
      limits:
        nvidia.com/gpu: 1
        nvidia.com/gpumem: 8000
EOF
kubectl --context=<volcano-cluster> apply -f /tmp/cuda-standard.yaml
kubectl --context=<volcano-cluster> wait --for=condition=Ready pod/cuda-standard --timeout=60s 2>/dev/null
sleep 5
kubectl --context=<volcano-cluster> logs cuda-standard | head -20
```

기대값: nvidia-smi 정상 출력. CUDA_DEVICE_MEMORY_LIMIT_0=8000m. HAMi-core CUDA enforce 정상.

- [ ] **Step 6: 4 케이스 모두 PASS 면 정리**

```bash
kubectl --context=<volcano-cluster> delete pod vulkan-vgpu-demo vulkan-noanno cuda-anno cuda-standard --ignore-not-found
rm -f /tmp/vulkan-noanno.yaml /tmp/cuda-anno.yaml /tmp/cuda-standard.yaml
```

---

## Task 14: PR 작성 + merge

**Files:**
- (없음 — git 작업)

- [ ] **Step 1: 모든 commit 확인**

```bash
cd ~/git/volcano-vgpu-device-plugin
git log --oneline feat/vulkan-vgpu-support ^main 2>&1
```

기대 commits (Task 1-9 의 commit):

```
feat(plugin): auto-inject Vulkan implicit layer manifest mount
feat(image): ship Vulkan implicit layer manifest from libvgpu
build: install libvulkan-dev in nvbuild stage for Vulkan layer compile
deps: bump libvgpu to 8d4f712 (vulkan-layer support)
chore: bump image to vulkan-v1 in deploy yaml
feat(deploy): add fallback DaemonSet for Vulkan manifest placement
docs(vulkan): usage guide + sample pod
build(init): copy Vulkan manifest to host during vgpu-init   (있는 경우)
chore: start feat/vulkan-vgpu-support branch
```

- [ ] **Step 2: PR 작성**

```bash
cd ~/git/volcano-vgpu-device-plugin
gh pr create \
    --base main \
    --head feat/vulkan-vgpu-support \
    --title "feat: Vulkan vGPU memory partitioning support" \
    --body "$(cat <<'EOF'
## Summary

- libvgpu submodule 을 vulkan-layer 가 포함된 SHA 로 갱신
- device-plugin Allocate 가 host 의 hami.json 을 container 에 자동 mount
- Dockerfile builder stage 에 libvulkan-dev 추가 + runtime stage 에 hami.json ship
- 신규 yaml 추가: fallback manifest DaemonSet, 사용 예시
- 사용 가이드 문서 추가

## 동작 원리

HAMi 본가의 Vulkan vGPU 지원 (commit 0150ea7) 패턴을 그대로 포팅. annotation `hami.io/vulkan: "true"` 가 붙은 pod 만 HAMi mutating webhook 이 HAMI_VULKAN_ENABLE=1 env 를 주입 → manifest 의 enable_environment 가드 매치 → Vulkan layer 로드 → vkAllocateMemory 후킹으로 메모리 enforce.

## 운영 deploy

1. `kubectl apply -f volcano-vgpu-vulkan-manifest.yml`  (선택)
2. `kubectl apply -f volcano-vgpu-device-plugin.yml`  (또는 CDI)
3. `helm install hami-webhook hami/hami --set ...`  (webhook only)

## Test plan

- [ ] Case 1: annotation 있는 Vulkan pod → memory enforce
- [ ] Case 2: annotation 없는 Vulkan pod → full GPU memory
- [ ] Case 3: annotation 있는 CUDA-only pod → CUDA 정상
- [ ] Case 4: 기존 CUDA workload 회귀 → gpumem enforce 정상
EOF
)"
```

- [ ] **Step 3: PR review 후 merge**

reviewer 의 피드백 적용. merge 시 squash 또는 rebase 정책은 fork 의 기존 관행 따른다.

---

## 참고 자료

- HAMi 본가 commit `0150ea7`: device-plugin Vulkan manifest auto-inject
- HAMi 본가 commit `50b37ff`: Dockerfile libvulkan-dev 추가
- HAMi spec `docs/superpowers/specs/2026-04-21-vulkan-vgpu-partitioning-design.md`
- HAMi plan `docs/superpowers/plans/2026-04-21-vulkan-vgpu-partitioning.md`
- HAMi 사용 가이드 `docs/vulkan-vgpu-support.md`
- HAMi E2E 체크리스트 `docs/vulkan-vgpu-e2e-checklist.md`
- 메모리 노트 `project_hami_vulkan_verification.md`
- 본 plan 의 spec `docs/superpowers/specs/2026-04-27-volcano-vulkan-vgpu-design.md`
