# Step D — Vulkan layer opt-in production activation + 4-path 검증

## 배경

Step C 재설계 (`docs/superpowers/specs/2026-04-29-step-c-redesign-vk-so-split.md`, plan `2026-04-29-step-c-vk-so-split.md`) 가 완료. 산출물:

- `libvgpu.so`: HAMi-core 만 (vk* 미export, 5개 `hami_core_*` export). 검증된 build md5 `1bd8f078a15b20e86b78626ddb938141`.
- `libvgpu_vk.so` (신규): Vulkan implicit-layer code. DT_NEEDED → `libvgpu.so`. Build md5 `95b44957ca3546fb72f8b5d7d699a4aa`.
- `hami.json` manifest (`libvgpu/share/hami/hami.json`): `library_path = /usr/local/vgpu/libvgpu_vk.so`, `type = INSTANCE`, api 1.3.0.
- ws-node074 검증: LD_PRELOAD `libvgpu.so` (manifest 미설치) × 5 → 5/5 alive (regression class 사라짐).

다만 RT9 의 manifest 활성 검증에서 `HAMI_VK_TRACE > 0` 은 확인되지 않음 — Kit 의 embedded Conan vulkan-loader 가 우리 GIPA 를 traverse 하지 않음. **Step D 의 4-path 검증이 이 부분의 closure**.

기존 production state (4-27 새벽 패치 이후 baseline):

- `volcano-device-plugin` DaemonSet (image `10.61.3.124:30002/library/volcano-vgpu-device-plugin:vulkan-v1`) 이 `postStart` lifecycle hook 으로 `cp -rf /k8s-vgpu/lib/nvidia/. /usr/local/vgpu/` 실행 → 호스트 `/usr/local/vgpu/libvgpu.so` 가 image 의 .so 로 매번 reset.
- `hami-vulkan-manifest` ConfigMap (`kube-system/`) 에 `hami.json` 정의. 현재 `library_path: /usr/local/vgpu/libvgpu.so`, `type: GLOBAL`, `enable_environment: HAMI_VULKAN_ENABLE=1`.
- `hami-vulkan-manifest-installer` DaemonSet 이 ConfigMap 의 `hami.json` 을 host `/usr/local/vgpu/vulkan/implicit_layer.d/hami.json` 으로 install. 현재 `nodeSelector: hami.io/disabled: "true"` 로 비활성 (4-27 새벽 패치 호환 충돌 후 baseline 보존).
- HAMi webhook (`pkg/device/nvidia/device.go::applyVulkanAnnotation`) 가 pod annotation `hami.io/vulkan: "true"` 인식해서 container 에 `HAMI_VULKAN_ENABLE=1` + `NVIDIA_DRIVER_CAPABILITIES` 에 `graphics` 추가. 이 코드는 이미 master 에 있음.

## 목적

Step C 의 `libvgpu_vk.so` 가 **production opt-in 활성 path 에서 실제로 동작**함을 검증하고, partition enforce 가 4 path 모두에서 작동함을 입증한다. 검증은 ws-node074 isaac-launchable namespace 의 isaac-launchable-0 pod 에서 수행.

## 핵심 결정

| # | 결정 | 선택 |
|---|---|---|
| 1 | `libvgpu.so` + `libvgpu_vk.so` 호스트 install 방식 | volcano-device-plugin image 에 두 파일 모두 ship (image rebuild). 기존 `cp -rf /k8s-vgpu/lib/nvidia/.` lifecycle 가 둘 다 install. 별도 DaemonSet 추가 안 함 |
| 2 | manifest CM 변경 | 기존 `hami-vulkan-manifest` ConfigMap update — `library_path` → `/usr/local/vgpu/libvgpu_vk.so`, `type` → `INSTANCE`, `enable_environment` 유지 (`HAMI_VULKAN_ENABLE: "1"`) |
| 3 | manifest installer DaemonSet 재활성 | `nodeSelector` 를 `hami.io/disabled: "true"` → `nvidia.com/gpu.present: "true"` 로 복귀. install path 그대로 (`/usr/local/vgpu/vulkan/implicit_layer.d/hami.json`) |
| 4 | opt-in trigger | 기존 `hami.io/vulkan: "true"` annotation + webhook injection 그대로. 추가 코드 변경 0 |
| 5 | 4-path 검증 method | isaac-launchable-0 vscode container 에서 ad-hoc shell + python script 실행. 별도 test pod 만들지 않음 (existing pod 활용) |
| 6 | rollback 안전장치 | swap 전 backup md5 기록, 각 단계 후 baseline runheadless 확인, 실패 시 즉시 backup restore |

## 비변경 사항

- HAMi parent Go 코드 (`pkg/device/nvidia/device.go`, webhook). `applyVulkanAnnotation` 그대로.
- `libvgpu` (HAMi-core) 코드 — Step C 끝낸 그대로.
- helm chart templates — Step D 는 runtime YAMLs (`cluster/runtime/snapshot-2026-04-28/`) 만 update. chart 통합은 별도 Step.
- volcano-device-plugin (Volcano fork) Go 코드 — 변경 없이 image rebuild 만.

## 호환성 약속

- `hami.io/vulkan: "true"` annotation 미설정 pod: HAMI_VULKAN_ENABLE 미주입 → loader manifest 의 `enable_environment` 매칭 실패 → layer 미활성. 기존 동작 그대로.
- annotation true 설정 pod: webhook 가 env 주입 → layer 활성 → partition enforce.
- `volcano-vgpu-device-plugin:vulkan-v1` image rebuild 는 기존 build pipeline 재사용. tag 만 `vulkan-v2` 로 bump.

## Architecture

```
┌── volcano-device-plugin DS (priv container, image vulkan-v2 신규) ──┐
│   - postStart: cp -rf /k8s-vgpu/lib/nvidia/. /usr/local/vgpu/      │
│       → /usr/local/vgpu/libvgpu.so       (Step C build)            │
│       → /usr/local/vgpu/libvgpu_vk.so    (신규)                    │
│       → /usr/local/vgpu/ld.so.preload    (기존)                    │
└────────────────────────────────────────────────────────────────────┘
                                 ↓
┌── hami-vulkan-manifest ConfigMap (kube-system) ────────────────────┐
│   hami.json:                                                       │
│     "type": "INSTANCE"                                             │
│     "library_path": "/usr/local/vgpu/libvgpu_vk.so"                │
│     "enable_environment": { "HAMI_VULKAN_ENABLE": "1" }            │
└────────────────────────────────────────────────────────────────────┘
                                 ↓
┌── hami-vulkan-manifest-installer DS (재활성, nodeSelector 복구) ───┐
│   - cp /manifest/hami.json → /host/usr/local/vgpu/vulkan/          │
│       implicit_layer.d/hami.json                                   │
└────────────────────────────────────────────────────────────────────┘
                                 ↓
┌── pod (with annotation hami.io/vulkan: "true") ────────────────────┐
│   webhook injects:                                                 │
│     - HAMI_VULKAN_ENABLE=1                                         │
│     - NVIDIA_DRIVER_CAPABILITIES = ...,graphics                    │
│   Vulkan loader 가 manifest 발견 → enable_environment 매칭 →       │
│   libvgpu_vk.so dlopen → DT_NEEDED libvgpu.so → 5 hami_core_*      │
│   resolved → layer chain 진입                                      │
└────────────────────────────────────────────────────────────────────┘
```

## Components

| 단위 | 위치 | 변경 종류 |
|---|---|---|
| `volcano-vgpu-device-plugin` image (vulkan-v2) | external (Volcano fork) | rebuild — image 의 `/k8s-vgpu/lib/nvidia/` 에 새 `libvgpu.so` + `libvgpu_vk.so` 둘 다 포함 |
| `cluster/runtime/snapshot-2026-04-28/hami-vulkan-manifest-cm.yaml` | repo | update — library_path / type / 주석 |
| `cluster/runtime/snapshot-2026-04-28/hami-vulkan-manifest-installer-ds.yaml` | repo | update — nodeSelector 복구 |
| `cluster/runtime/snapshot-2026-04-28/volcano-device-plugin-ds.yaml` | repo | update — image tag → vulkan-v2 |
| `cluster/runtime/snapshot-2026-04-28/4-path-verification.sh` (신규) | repo | NVML / CUDA / Vulkan memory query / Vulkan allocate 검증 script |

(snapshot 디렉토리 명을 `snapshot-2026-04-29-step-d` 로 새로 만들거나 기존 디렉토리 이름 변경할지는 plan 단계 결정.)

## Activation flow

production deploy:

1. volcano-device-plugin image rebuild + push (`vulkan-v2` tag).
2. ConfigMap `hami-vulkan-manifest` apply (library_path 변경).
3. DaemonSet `hami-vulkan-manifest-installer` patch (nodeSelector 복구) → DS pod schedule → manifest install 실행.
4. DaemonSet `volcano-device-plugin` image bump → pod rollout → postStart lifecycle 가 새 .so 두 개 install.
5. isaac-launchable-0 pod 의 annotation 에 `hami.io/vulkan: "true"` 추가 (이미 있을 수도). pod 재시작 → webhook 가 env 주입.
6. 4-path verification 실행.

## 4-path verification

4 path 모두 `hami.io/vulkan: "true"` annotation 활성된 isaac-launchable-0 의 vscode container 에서 실행:

| Path | 명령 | 기대 |
|---|---|---|
| 1. NVML hook | `nvidia-smi --query-gpu=memory.total --format=csv,noheader` | `23552 MiB` (clamp). 이미 검증 — 그대로. |
| 2. CUDA driver hook | python: `import pycuda.driver as cuda; cuda.init(); ctx = cuda.Device(0).make_context(); free, total = cuda.mem_get_info(); print(total)` | `23552 * 1024 * 1024` ≈ 24696061952 bytes (clamp) |
| 3. Vulkan memory query | python: `vkGetPhysicalDeviceMemoryProperties` 의 `memoryHeaps[device-local].size` | `23552 * 1024 * 1024` (clamp) |
| 4. Vulkan allocate | python: `vkAllocateMemory(VkMemoryAllocateInfo{ size = 25 * 1024 * 1024 * 1024 })` (25 GiB > 23 GiB partition) | `VK_ERROR_OUT_OF_DEVICE_MEMORY` |

추가:

- Manifest 가 active layer 로 enumerated 되는지 (`VK_LOADER_DEBUG=layer` 출력) 확인.
- HAMI_VK_TRACE > 0 (layer 가 호출됨을 입증) — Kit 의 embedded Conan loader 우회를 위해 host system Vulkan loader 쓰는 python 스크립트로 검증.

`vk_partition_test.py` 같은 script 를 신규 작성 (또는 기존 isaac-sim/ 디렉토리에서 재사용). 위치: `cluster/runtime/snapshot-2026-04-28/4-path-verification.sh` 또는 isaac-launchable-0 의 home dir.

## Production safety gate

각 단계마다:

1. **Pre-step 백업**: 현재 production state 의 md5sum + ConfigMap export + DaemonSet status 기록.
2. **Apply**: kubectl apply / patch.
3. **Post-step verify**: isaac-launchable-0 / -1 baseline runheadless 1회 — `exit=124 crash=0 listen=1` 확인. 실패 시 즉시 rollback (backup 적용).
4. **Roll forward only on green**.

## 비검증 항목

- helm chart 통합 (현재 chart values 에 vulkan toggle 없음. 추가는 별도 Step).
- usd-composer / 다른 Vulkan 사용 pod 검증 (Step D 는 isaac-launchable-0 만).
- multi-GPU 케이스 (현재 ws-node074 single-GPU 로만 검증).

## Test plan (high level)

1. volcano-vgpu-device-plugin image rebuild + push.
2. ConfigMap update + patch DS — DS pod 가 manifest install 한 후 isaac-launchable-0 baseline runheadless 1회 확인.
3. volcano-device-plugin DS image bump → pod rollout — `/usr/local/vgpu/libvgpu_vk.so` 존재 + md5 = 새 build md5 확인.
4. isaac-launchable-0 annotation 확인 (이미 `hami.io/vulkan: "true"` 인지) + pod 재시작.
5. 4-path 검증 실행. 4/4 expected 결과.
6. HAMI_VK_TRACE > 0 확인 (host system loader path 통한 python script).
7. usd-composer 등 다른 Vulkan 사용 pod 영향 0 확인 (steady state Running 유지).

## Out of scope (이번 spec 에서 다루지 않음)

- Tasks 1+2 재도입 (`996cb22` cache + Enumerate hooks, `eea2beb` GIPA fallback). 별도 follow-up plan.
- helm chart 의 vulkan toggle / values 추가.
- `enable_environment` 외 alternative trigger (예: env-var-prefix manifest, side-channel labels). 현재 path 가 standard 이므로 그대로 유지.
- volcano-device-plugin Go 코드 변경 (image rebuild 만).
- Multi-GPU partition enforce 검증.
