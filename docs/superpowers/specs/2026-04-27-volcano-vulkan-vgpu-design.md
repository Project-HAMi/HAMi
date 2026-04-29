# Volcano + Vulkan vGPU 통합 설계

**작성일**: 2026-04-27
**관련 작업**: HAMi `feat/vulkan-vgpu` 브랜치의 Vulkan vGPU 기능을 `xiilab/volcano-vgpu-device-plugin` 환경에 적용

## 목적

Volcano scheduler 가 이미 운영 중인 클러스터에 HAMi 의 Vulkan vGPU 메모리 partitioning 기능을 추가한다. Volcano scheduler 와 `volcano-vgpu-device-plugin` 은 그대로 유지하면서 **Vulkan workload (Isaac Sim, Kit 등) 도 CUDA workload 와 동일하게 `nvidia.com/gpumem` 제약을 받도록** 한다.

## 비목표 (Non-goals)

- Volcano scheduler 동작/스케줄링 로직 변경 ❌
- 기존 CUDA-only workload 의 동작 회귀 ❌
- HAMi 자체 scheduler extender 또는 device-plugin 도입 ❌
- 새 task scheduler 또는 webhook 체인 변경 ❌

## 현재 상태 (As-is)

### HAMi `feat/vulkan-vgpu` 브랜치 (이미 검증됨)

- `libvgpu` submodule (HAMi-core, vulkan-layer): `vkAllocateMemory` 후킹으로 Vulkan 메모리 enforcement
- `pkg/device/nvidia/device.go:applyVulkanAnnotation`: pod annotation `hami.io/vulkan: "true"` 검사 → `HAMI_VULKAN_ENABLE=1` env + `NVIDIA_DRIVER_CAPABILITIES` 에 `graphics` merge
- `0150ea7` commit: device-plugin 이 Vulkan implicit layer manifest (`hami.json`) 를 container 에 자동 mount
- 2026-04-26 production verification: ws-node074 의 Isaac Sim pod 에서 23 GB partition enforcement 확인

### `xiilab/volcano-vgpu-device-plugin` (현재)

- Project-HAMi/volcano-vgpu-device-plugin 의 fork
- `libvgpu` submodule = `6660c84` (vulkan-layer 미포함)
- HAMi-core 사용은 하지만 CUDA path 만 enforce
- Volcano scheduler 와 ConfigMap (`deviceshare.VGPUEnable: true`) 으로 협업
- standard / CDI 두 가지 deploy yaml 제공

## 설계: 책임 분담

| 레이어 | 담당자 | 변경 |
|---|---|---|
| Pod scheduling | Volcano scheduler | ❌ 변경 없음 |
| GPU 자원 sharing/할당 | volcano-vgpu-device-plugin | ⚠️ submodule + manifest mount |
| Pod spec mutation (env) | HAMi mutating webhook | ✅ 별도 deploy (annotation 처리) |
| Vulkan 메모리 enforcement | libvgpu (HAMi-core vulkan-layer) | ✅ submodule 갱신으로 자동 |

### 핵심 결정

1. **HAMi webhook 만 별도 deploy** — Volcano 우회 아님. mutating admission webhook 은 scheduling 과 별개 단계라 scheduler 그대로 유지.
2. **submodule 단순 교체로는 부족** — Vulkan layer 코드는 들어오지만 manifest 파일 자동 mount + env 주입 두 가지 부수 효과 필요.
3. **manifest 파일은 device-plugin 이 hostPath mount** — HAMi commit `0150ea7` 패턴 그대로 포팅. 호스트 노드에 `/etc/vulkan/implicit_layer.d/hami.json` 사전 배치는 별도 DaemonSet 또는 helm chart init.

## Components

### C1. libvgpu submodule 교체

- **변경 위치**: `xiilab/volcano-vgpu-device-plugin/libvgpu`
- **변경 내용**: `6660c84` → vulkan-layer HEAD (HAMi 가 사용 중인 commit, 현재 `8d4f712`)
- **부수 효과**: vulkan source 추가, `vkQueueSubmit2` / `VkSubmitInfo2` Vulkan 1.3 가드 코드 포함

### C2. Vulkan manifest auto-mount

- **변경 위치**: `xiilab/volcano-vgpu-device-plugin/pkg/.../allocate` (또는 device 응답 빌더)
- **변경 내용**: HAMi commit `0150ea7` 의 `injectVulkanLayerMount()` 함수 포팅
- **동작**: device-plugin 의 `Allocate()` 응답에 다음 mount 추가
  ```
  hostPath:      /etc/vulkan/implicit_layer.d/hami.json
  containerPath: /etc/vulkan/implicit_layer.d/hami.json
  readOnly:      true
  ```
- **CDI 모드**: `volcano-vgpu-device-plugin-cdi.yml` 경로도 동일하게 처리. CDI spec yaml 에 mount 추가하는 형태로.

### C3. 빌드 의존성

- **변경 위치**: `Dockerfile` (volcano-vgpu-device-plugin 의 builder stage)
- **변경 내용**: `libvulkan-dev` apt install (HAMi commit `50b37ff` 와 동일)
- **이유**: vulkan-layer source 컴파일에 Vulkan headers 필요

### C4. HAMi webhook deployment

- **변경 위치**: 새 클러스터에 helm install (코드 변경 없음, deploy 작업)
- **values.yaml**:
  ```yaml
  devicePlugin:
    enabled: false       # volcano-vgpu-device-plugin 이 GPU 자원 등록
  scheduler:
    kubeScheduler:
      enabled: false     # Volcano scheduler 사용
    extender:
      enabled: false     # HAMi extender 사용 안 함
  admissionWebhook:
    enabled: true        # Vulkan annotation 처리만
  ```
- **결과**: HAMi 의 `applyVulkanAnnotation` 코드가 Volcano 환경에서도 동작. annotation 있는 pod 의 container env 자동 주입.

### C5. Host 측 manifest 파일 사전 배치 (`volcano-vgpu-vulkan-manifest.yml`)

- **변경 위치**: `xiilab/volcano-vgpu-device-plugin` 에 신규 raw yaml 추가 (기존 `volcano-vgpu-device-plugin.yml` 와 같은 디렉터리/패턴)
- **구성**: ConfigMap (`hami.json` 본문) + DaemonSet (initContainer 가 ConfigMap 의 `hami.json` 을 host 의 `/etc/vulkan/implicit_layer.d/hami.json` 으로 복사)
- **manifest 내용**: HAMi 의 `0150ea7` commit 에서 사용한 것 그대로 (layer 이름 `VK_LAYER_HAMI_vgpu`, library path `/usr/local/vgpu/libvgpu.so`, enable_environment `HAMI_VULKAN_ENABLE=1`)
- **DaemonSet 위치**: 모든 GPU 노드 (label `nvidia.com/gpu.present=true` 또는 동등 selector). manifest 파일이 ready 된 노드만 device-plugin 의 mount 가 성공할 수 있으므로 device-plugin DaemonSet 보다 먼저 배포하는 게 안전.
- **대안**: 사용자 image 에 manifest 베이크 — 비채택 (사용자 부담 증가)

### C6. E2E 테스트

- **검증 항목**:
  1. annotation 있는 Vulkan pod → Kit boot log 의 `GPU Memory: 23000 MB` (partition enforce)
  2. annotation 없는 Vulkan pod → Kit boot log 의 `GPU Memory: 46068 MB` (full GPU)
  3. annotation 있는 CUDA-only pod → CUDA 정상 + Vulkan layer 안 로드 확인
  4. 기존 volcano-vgpu-device-plugin CUDA sharing 회귀 (HAMi-core dynamic-mig 모드 포함)
- **참고 문서**: HAMi `docs/vulkan-vgpu-e2e-checklist.md` 의 체크리스트 그대로 적용

## Data flow (활성화 케이스)

```
1. kubectl apply  isaac-sim.yaml
     annotations: hami.io/vulkan: "true"
     resources.limits: nvidia.com/gpumem: 23000

2. K8s API server
   ├─ HAMi mutating webhook (별도 deploy 됨)
   │  ├─ env += HAMI_VULKAN_ENABLE=1
   │  └─ env += NVIDIA_DRIVER_CAPABILITIES=compute,utility,graphics
   └─ etcd 저장

3. Volcano scheduler  (변경 없음)
   └─ pod 을 ws-node074 로 schedule

4. kubelet → volcano-vgpu-device-plugin Allocate()
   ├─ GPU UUID 할당 (NVIDIA_VISIBLE_DEVICES)
   ├─ libvgpu.so mount (CUDA + Vulkan 후킹용, 기존 코드)
   └─ /etc/vulkan/implicit_layer.d/hami.json mount (C2 신규)

5. Container 시작
   ├─ ld.so.preload 가 libvgpu.so 로드 (image 측 책임)
   ├─ Vulkan app 시작 → loader 가 hami.json 발견
   ├─ enable_environment 가드 매치 (HAMI_VULKAN_ENABLE=1)
   ├─ Vulkan layer 로드 → vkAllocateMemory 후킹
   └─ CUDA_DEVICE_MEMORY_LIMIT_0=23000m enforce
```

## Error handling / edge cases

| 시나리오 | 동작 | 비고 |
|---|---|---|
| annotation 없는 pod | webhook no-op → env 미주입 → enable_environment 가드 unmatched → layer 안 로드 | 일반 CUDA pod 동작 그대로 |
| 노드에 manifest 파일 없음 | device-plugin Allocate 의 mount 시도 → kubelet mount 실패 → pod ContainerCreating | DaemonSet 의 manifest 배포 readiness 보장 필요 |
| HAMi webhook + Volcano webhook 순서 | mutating webhook chain 순차 실행. capability 추가 → Volcano 가 받는 spec 에 반영 → schedule 시 capability 미사용 | 충돌 없음 |
| CDI 모드 | `volcano-vgpu-device-plugin-cdi.yml` 의 device-plugin 도 동일하게 hami.json mount 추가 필요 | 코드 분기 |
| Vulkan ICD 의존성 부재 | libGLX_nvidia.so 가 vk_icdNegotiateLoaderICDInterfaceVersion -3 반환 → Vulkan init 실패 | 사용자 image 가 libEGL.so.1 + X11 + /dev/dri 포함해야 함 (HAMi 메모리 노트 참고) |

## Risks

1. **CDI 모드와 standard 모드 분기 누락**: 두 deploy yaml 이 서로 다른 device-plugin binary 를 사용한다면 manifest mount 코드도 두 곳에 들어가야 함. 점검 필요.
2. **DaemonSet 으로 host 노드에 manifest 배포 안 되어있는 경우**: pod 이 ContainerCreating 으로 stuck. helm chart 또는 별도 manifest 로 readinessGate 처리 필요.
3. **NVIDIA driver container 의존**: Volcano 환경이 NVIDIA gpu-operator 사용한다면 driver container 가 X11/EGL 라이브러리를 마운트해야 Vulkan 동작. HAMi 환경에서 검증한 것과 동일한 image 셋업 가정.
4. **upstream Project-HAMi/volcano-vgpu-device-plugin 과 divergence**: xiilab fork 가 별도 vulkan 코드 포함하는 동안 upstream 과 sync 가 어려워질 수 있음. 가능하면 upstream 에 PR 도 보내 divergence 최소화 권장.

## Testing

1. **Unit test**: 기존 volcano-vgpu-device-plugin 의 device allocate test 에 manifest mount 검증 추가
2. **회귀 test**: CUDA-only workload 가 기존과 동일하게 동작
3. **Integration**: kind/minikube 에서 Volcano + HAMi webhook + 새 device-plugin → 표준 CUDA pod 정상 동작 확인
4. **E2E manual** (ws-node074 또는 별도 Volcano cluster):
   - 4-1. Vulkan pod + annotation: 23 GB partition 확인
   - 4-2. Vulkan pod no-annotation: full GPU 확인
   - 4-3. CUDA pod + annotation: 영향 없음
   - 4-4. dynamic-mig 모드 회귀 (Ampere+ GPU 가용 시)

## Deployment artifact (raw yaml 패턴)

`xiilab/volcano-vgpu-device-plugin` 의 기존 패턴 (helm chart 없음, 평탄한 raw yaml) 을 그대로 따른다.

```
xiilab/volcano-vgpu-device-plugin/
├── volcano-vgpu-device-plugin.yml             # 기존 standard mode (image tag 갱신)
├── volcano-vgpu-device-plugin-cdi.yml         # 기존 CDI mode (image tag 갱신)
└── volcano-vgpu-vulkan-manifest.yml           # ★ 신규 — ConfigMap + DaemonSet
```

HAMi webhook 은 별도 yaml 작성하지 않고 **HAMi 본가 helm chart 재사용** (C4 의 values.yaml).

## Deployment 순서

1. **PR-1: xiilab/volcano-vgpu-device-plugin**
   - submodule 갱신 (C1: `6660c84` → vulkan-layer HEAD)
   - device-plugin 코드에 manifest mount 추가 (C2)
   - Dockerfile 빌드 의존성 (C3: `libvulkan-dev`)
   - 기존 두 yaml 의 image tag 를 새 빌드 (`vulkan-v1`) 으로 갱신
   - 신규 `volcano-vgpu-vulkan-manifest.yml` 추가 (C5)
   - image 빌드 + harbor push

2. **클러스터 deploy**
   - 2-1. `kubectl apply -f volcano-vgpu-vulkan-manifest.yml` (host 에 hami.json 배치)
   - 2-2. `kubectl apply -f volcano-vgpu-device-plugin.yml` (또는 CDI 버전, 새 image rolling)
   - 2-3. `helm install hami-webhook hami/hami` (C4 values 로 webhook only)

3. **E2E 검증** (C6)

## 관련 자료

- HAMi `feat/vulkan-vgpu` 브랜치 (현재)
  - `pkg/device/nvidia/device.go:applyVulkanAnnotation` (webhook 코드)
  - commit `0150ea7` (manifest auto-inject)
  - commit `50b37ff` (libvulkan-dev 빌드 의존성)
  - `docs/vulkan-vgpu-support.md`, `docs/vulkan-vgpu-e2e-checklist.md`
- xiilab/volcano-vgpu-device-plugin
  - `https://github.com/xiilab/volcano-vgpu-device-plugin`
  - 현재 libvgpu submodule: `6660c84`
- HAMi 메모리 노트
  - `project_hami_vulkan_verification.md` (production activation 검증)
- Volcano scheduler
  - `https://github.com/volcano-sh/volcano`
  - vGPU 활성화: `deviceshare.VGPUEnable: true` ConfigMap 설정
