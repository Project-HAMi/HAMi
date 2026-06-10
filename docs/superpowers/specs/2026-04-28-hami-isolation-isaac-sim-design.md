# HAMi vGPU 격리를 NVIDIA Isaac Sim Kit (Omniverse) 에 적용 — Design

**Date**: 2026-04-28
**Status**: Approved (사용자 design 승인 완료)
**Goal**: HAMi vGPU 격리(NVML + CUDA + Vulkan path) 를 NVIDIA Isaac Sim Kit (Carbonite/OptiX/Vulkan implicit layer chain) 와 호환되게 적용

## 1. Context

PR #1803 (HAMi 메인 fork `xiilab/feat/vulkan-vgpu`) + PR #182 (HAMi-core fork `xiilab/vulkan-layer`) 가 Vulkan vGPU partition 격리를 추가했고, 2026-04-27 새벽에 4개 patch 가 cluster 에 deploy 되어 **노드 wide HAMi 격리** 가 활성화됐다. 그러나 이 시점부터 isaac-launchable namespace 의 **Isaac Sim Kit 6.0.0-rc.22** (`runheadless.sh`, `train.py --livestream 2`) 가 SegFault 로 더 이상 동작하지 않게 됐다.

사용자가 2일 동안 정상 시연했던 baseline 은 **2026-04-27 08:44 이전** 이고, 이 시점 이후의 노드 wide 강제 격리가 NVIDIA Isaac Sim Kit 의 init path 와 호환 충돌한다. race lucky 가 아닌 진짜 regression.

진정한 fix 는 격리 메커니즘을 namespace 단위 opt-in 으로 변경하고 (Step A), HAMi-core 의 hook code 를 Isaac Sim Kit init 시 안전하게 동작하도록 hardening (Step B/C) 한 다음, isaac-launchable namespace 도 opt-in 활성화하여 격리 + 동작 둘 다 만족하는 (Step D) 것이다.

## 2. 4-27 새벽 patch (regression 시점)

| 시각 (UTC) | 변경 |
|---|---|
| 02:02 | `volcano-vgpu-device-plugin:vulkan-v1` Harbor push (`10.61.3.124:30002/library/`) |
| 02:17:50 | `hami-vulkan-manifest-installer` daemonset 생성 (kube-system) — 노드의 `/usr/local/vgpu/vulkan/implicit_layer.d/hami.json` 생성 |
| 03:34:22 | `hami-webhook` MutatingWebhookConfiguration install (helm release `hami-webhook` in `hami-system`) — pod 생성 시 자동 mutation (HAMI_VULKAN_ENABLE env, hami.json mount, NVIDIA_DRIVER_CAPABILITIES patch) |
| 08:44 | `/usr/local/vgpu/ld.so.preload` 만들어짐 — **노드 wide 모든 컨테이너 process 에 `libvgpu.so` 강제 inject** |

마지막 (`ld.so.preload`) 이 결정적 trigger.

## 3. Isaac Sim Kit 와의 호환 충돌 (backtrace 증거)

### 3.1 OptiX denoising init 시 NULL deref
```
000: libc.so.6!__sigaction
001: libvgpu.so!cuMemGetInfo_v2+0x52c (memory.c:513)   ← HAMi-core CUDA hook
002: libnvoptix.so.1!rtGetSymbolTable
004: librtx.optixdenoising.plugin.so!carbOnPluginPreStartup
009: libcarb.scenerenderer-rtx.plugin.so!carbOnPluginPreStartup
010: libomni.hydra.rtx.plugin.so
```
NVIDIA OptiX denoising plugin 이 init 시 `cuMemGetInfo_v2(NULL, NULL)` 호출 → HAMi-core hook 이 NULL pointer dereference 시도.
**Fix 이미 적용됨**: HAMi-core fork commit `03f99d7` — forward to real driver first + NULL guard.

### 3.2 Carbonite Vulkan plugin extension list dangling
```
001: libvulkan.so.1!+0x22bc8                       ← Vulkan loader
002: libcarb.graphics-vulkan.plugin.so!std::vector<char const*>::_M_emplace_aux<char const*&>
003: libgpu.foundation.plugin.so!Map_base<string, ulong>::operator[]
009: libgpu.foundation.plugin.so!filesystem::path::~vector()
013: libomni.ui!Image::_loadSourceUrl
039: libomni.kit.renderer.plugin.so!carbOnPluginPreStartup
```
Carbonite Vulkan plugin 이 enabled extension list 만들 때 layer chain 에서 `vkGetInstanceProcAddr(NULL, "vkEnumerate*ExtensionProperties")` 호출 → HAMi Vulkan layer 가 NULL 반환 → loader 가 NULL fn ptr 사용 → SegFault.
**Fix 이미 적용됨**: HAMi-core commit `2b6b875` — `vkEnumerate{Instance,Device}{Extension,Layer}Properties` hooks 추가.

### 3.3 carb.tasking fiber init race
```
014-017: libcarb.tasking.plugin.so!make_fcontext+0x39
```
NVIDIA Kit 의 task scheduler 가 fiber/coroutine context 생성 시 race. Layer chain 활성 시 dispatch 차이로 trigger.
**Fix 미적용** — Step C 영역.

### 3.4 omni.clipboard.service utmp 부재
```
Failed to open [/var/run/utmp]
Active user not found. Using default user [kiosk]
```
`omni.clipboard.service` 가 init 시 logged-in user 식별 실패. 직접 SegFault trigger 는 아니나 race 기여 가능. 우회: utmp record 만들기.

## 4. 검증된 baseline (Step A 직전 상태)

```
ws-node074:
  /usr/local/vgpu/ld.so.preload     = "" (빈 파일, HAMi-core inject 비활성)
  /usr/local/vgpu/libvgpu.so        = HAMi-core fork build (md5 62fedf17)
  /usr/local/vgpu/vulkan/implicit_layer.d/hami.json = 복원
  hami-vulkan-manifest-installer ds = nodeSelector hami.io/disabled=true (비활성)
  isaac-launchable namespace label  = hami.io/webhook=ignore (webhook opt-out)

검증:
  runheadless.sh 5번 → 5/5 exit=124 alive, crash=0, listen 49100/30999 ✅
  nvidia-smi total = 46068 MiB (raw — 격리 비활성)
  외부 http://10.61.3.118 = 5/5 → 200
  isaac-launchable-0/1, usd-composer pod 모두 3/3 Running
```

이 환경이 사용자가 본 2일 동안 동작하던 baseline 과 동등 (격리 0).

## 5. Goal

| 격리 path | 검증 방법 | 기대값 |
|---|---|---|
| **NVML** | `nvidia-smi --query-gpu=memory.total --format=csv,noheader` | `23552 MiB` |
| **CUDA** | `cuMemGetInfo_v2()` returned total / `cuMemAlloc(>23 GiB)` | partition value / `CUDA_ERROR_OUT_OF_MEMORY` |
| **Vulkan** | `vkGetPhysicalDeviceMemoryProperties` heap[0].size / `vkAllocateMemory(>23 GiB)` | `23 GiB` / `VK_ERROR_OUT_OF_DEVICE_MEMORY` |
| **Isaac Sim Kit** | `runheadless.sh` 5번 / `train.py --livestream 2` | 5/5 alive, listen 49100, 화면 표시, 학습 진행 |

**4개 path 동시에 만족** = 성공.

## 6. Architecture (4 Step)

```
Step A (namespace opt-in/out webhook)
   ↓
Step B (HAMi-core CUDA/NVML hook hardening)
   ↓
Step C (HAMi-core Vulkan layer compat hardening)
   ↓
Step D (isaac-launchable opt-in 활성화 + 4-path 검증)
```

각 step 은 independent (앞 step 결과물만 의존). Step A 끝나면 isaac-launchable 즉시 정상 운영. Step B, C 가 완료된 후에만 Step D 의 진짜 검증 가능.

## 7. Step A — Namespace opt-in/out (1일)

### 7.1 변경 대상

| 컴포넌트 | 현재 | 변경 |
|---|---|---|
| `hami-webhook` MutatingWebhookConfiguration `namespaceSelector` | opt-out (`hami.io/webhook NotIn ignore`) | **opt-in (`hami.io/vgpu In enabled`)** |
| `hami-vulkan-manifest-installer` daemonset (노드 wide hami.json install) | 모든 GPU 노드 활성 | **폐기 또는 webhook init container 로 변환** — pod 단위 hami.json mount |
| `/usr/local/vgpu/ld.so.preload` (노드 wide HAMi-core inject) | 모든 컨테이너 강제 inject | **폐기** — webhook 이 enabled namespace pod 에만 `LD_PRELOAD` env 주입 + `libvgpu.so` volume mount |

### 7.2 새 webhook mutation 패턴 (enabled pod 만)

```yaml
# Pod containers[*] 에 추가:
env:
  - name: LD_PRELOAD
    value: /usr/local/vgpu/libvgpu.so
  - name: HAMI_VULKAN_ENABLE
    value: "1"
  - name: NVIDIA_DRIVER_CAPABILITIES
    value: <기존값>,graphics  # 이미 all 이면 noop
volumeMounts:
  - name: hami-libvgpu
    mountPath: /usr/local/vgpu
    readOnly: true
  - name: hami-vulkan-layer
    mountPath: /etc/vulkan/implicit_layer.d/hami.json
    subPath: hami.json
    readOnly: true

# Pod volumes 에 추가:
volumes:
  - name: hami-libvgpu
    hostPath:
      path: /usr/local/vgpu
      type: Directory
  - name: hami-vulkan-layer
    configMap:
      name: hami-vulkan-layer
      items:
        - key: hami.json
          path: hami.json
```

### 7.3 변경 파일

- `charts/hami/values.yaml` — namespaceSelector default mode (`opt-in` 추가)
- `charts/hami/templates/webhook-mutating.yaml` — selector mode 분기
- `charts/hami/templates/manifest-installer-ds.yaml` — 제거 또는 init container 로 이동
- `charts/hami/templates/preload-installer.yaml` (있다면) — 제거 (`/usr/local/vgpu/ld.so.preload` 만들기 daemonset)
- `pkg/scheduler/webhook/*` (mutation 로직 변경)

### 7.4 검증

- isaac-launchable namespace = label 없음 → webhook mutation 0 → 현재 baseline 그대로 (5/5 alive)
- 새 namespace `hami-test` 에 label `hami.io/vgpu=enabled` + simple CUDA pod 배포 → `nvidia-smi 23552 MiB`, `cuMemAlloc(>23GiB)` 거부 검증

## 8. Step B — HAMi-core CUDA/NVML hook hardening (3-5일)

### 8.1 Robustness 패턴

`cuMemGetInfo_v2` 의 fix 패턴 (commit `03f99d7`):

```c
CUresult cuXxx(...) {
    LOG_DEBUG("cuXxx");
    ENSURE_INITIALIZED();

    /* 1. Forward to the real driver FIRST. NULL/missing-context errors
     * surface exactly as without HAMi. We never dereference pointers
     * the driver rejected. */
    CUresult r = REAL_CALL(cuXxx, ...);
    if (r != CUDA_SUCCESS) return r;

    /* 2. NULL/invalid arg guard — return early without enforcement */
    if (...args invalid for HAMi logic...) return r;

    /* 3. Get device + apply HAMi 격리 logic */
    ...
}
```

### 8.2 Audit 대상

| Hook | 현재 상태 | 액션 |
|---|---|---|
| `cuMemGetInfo`, `cuMemGetInfo_v2` | ✅ Fixed (`03f99d7`) | unit test 추가 |
| `cuMemAlloc`, `cuMemAlloc_v2` | audit 필요 | NULL devptr / `bytesize == 0` guard |
| `cuMemAllocAsync`, `cuMemAllocPitch` | audit 필요 | 동일 패턴 |
| `cuMemFree`, `cuMemFree_v2`, `cuMemFreeAsync`, `cuMemFreeHost` | audit 필요 | untracked pointer fallback (이미 일부 fix `3bebc8a`) |
| `cuCtxGetDevice` | audit 필요 | NULL ctx 시 driver error pass-through |
| `cuMemCreate` | ✅ Fixed (`833c62c`) | 검증 |
| `nvmlDeviceGetMemoryInfo`, `_v2` | ✅ Robust | 검증 |

### 8.3 단위 검증

각 hook 별로:
- normal happy path (정상 인자, 정상 반환)
- NULL pointer arg (driver 가 거부하면 그대로 반환)
- partition limit 도달 (OOM 반환)
- partition limit 0 (unlimited fallback)

`vk_partition_test.py` 와 비슷한 단순 test 추가 (`cuda_partition_test.py`).

### 8.4 Isaac Sim 통합 검증 (Step B 완료 시점)

- `LD_PRELOAD=/usr/local/vgpu/libvgpu.so /isaac-sim/python.sh -c "from isaacsim import SimulationApp; SimulationApp({'headless': True}).close()"` — graceful exit (no SegFault)
- `runheadless.sh` 단독 실행 — Vulkan path 문제 잔존하므로 Step C 후 검증

## 9. Step C — HAMi-core Vulkan layer compat (5-7일)

### 9.1 이미 적용된 fix (commits)

- `93dd103`: deviceUUID zero → idx=0 fallback (single-GPU container 호환)
- `91ca00c`: HOOK_NVML_ENABLE build flag — NVML hook activate
- `2b6b875`: `vkEnumerate{Instance,Device}{Extension,Layer}Properties` hooks — GIPA NULL deref 방지

### 9.2 추가 hardening

`hami_vkGetInstanceProcAddr` audit:
- 모든 instance-level entry point 호출 시 invalid handle pass-through 패턴 (단, NVIDIA driver 에 unknown handle 절대 forward 금지 — 정의되지 않은 동작)
- 현재 hook 안 한 함수들 (`vkGetPhysicalDeviceFormatProperties{,2}`, `vkGetPhysicalDeviceImageFormatProperties{,2}`, `vkGetPhysicalDeviceQueueFamilyProperties{,2}`, `vkGetPhysicalDeviceFeatures{,2}`, `vkGetPhysicalDeviceProperties{,2}`, `vkGetPhysicalDeviceSparseImageFormatProperties{,2}`) — instance dispatch 통해 next layer forward 가 표준 패턴이며 instance 등록 시 cache

`hami_vkCreateInstance` / `hami_vkCreateDevice` audit:
- chain 변경의 in-place 수정 (`chain->u.pLayerInfo = chain->u.pLayerInfo->pNext`) 이 spec 표준 — caller 가 createInfo 재사용 안 한다고 가정. 그러나 NVIDIA OptiX 가 재사용 가능성 있음 → caller-safe deep copy 검토.

dispatch lifetime audit:
- `hami_instance_unregister` / `hami_device_unregister` 가 caller-side에서 적절한 시점에 호출되는지
- multi-instance 환경 (Carbonite 가 두 번째 instance 만드는 케이스) 에서 first instance 의 cached gipa 가 stale 안 되도록

OptiX/Aftermath 호환:
- `aftermath_status=auto-enabled` 환경에서 vkCreateDevice extensions 처리 검증
- `librtx.optixdenoising.plugin.so` init path 추적 (Step B 의 cuMemGetInfo 이후 stage)

### 9.3 검증

- `runheadless.sh` 5번 — 5/5 alive + listen 49100/30999 (현재 ld.so.preload 비활성에서 5/5 → layer 활성에서도 5/5 목표)
- `vk_partition_test.py` — Vulkan partition enforce 유지 (이미 통과)
- `train.py --livestream 2` — 학습 진행 + WebRTC 화면 표시
- OptiX denoising 활성 시 Kit init 통과

## 10. Step D — isaac-launchable opt-in 활성화 + 검증 (1-2일)

### 10.1 시나리오

1. isaac-launchable namespace label 변경: `hami.io/webhook=ignore` 제거 → `hami.io/vgpu=enabled` 추가
2. isaac-launchable-* / usd-composer pod 재생성
3. webhook 이 enabled mutation 적용 (LD_PRELOAD env, libvgpu.so mount, hami.json mount)
4. 4-path 동시 검증

### 10.2 검증 매트릭스

| Path | Command | Expected |
|---|---|---|
| NVML | `kubectl exec ... nvidia-smi --query-gpu=memory.total --format=csv,noheader` | `23552 MiB` |
| CUDA | `LD_PRELOAD=/usr/local/vgpu/libvgpu.so python -c "import cupy; cupy.cuda.runtime.malloc(25*1024**3)"` | `cudaErrorMemoryAllocation` |
| Vulkan | `kubectl exec ... /isaac-sim/python.sh vk_partition_test.py` | heap[0]=23 GiB, 25/30 GiB OOM |
| Isaac Sim | `kubectl exec ... ACCEPT_EULA=y /isaac-sim/runheadless.sh` 5회 | 5/5 alive, listen 49100/30999 |
| Isaac Sim 학습 | `kubectl exec ... ./isaaclab.sh -p train.py --livestream 2 --max_iterations 5` | `Iteration 0..4` reward 출력 + 화면 표시 |

5/5 통과 = Step D 성공 = 전체 design goal 달성.

## 11. 위험 및 대응

| 위험 | 영향 | 대응 |
|---|---|---|
| Step B/C 가 며칠 걸리는데 isaac-launchable 즉시 운영 필요 | 높음 | Step A 만으로 isaac-launchable 즉시 baseline 동작 (현 상태) |
| Step C 후에도 race 잔존 (NVIDIA Kit 자체 bug) | 중 | NVIDIA bug report, Isaac Sim GA / 다른 RC build 시도 |
| `namespaceSelector` opt-in 변경이 기존 사용자 영향 (label 없는 namespace 격리 0) | 중 | helm chart values 의 default mode 분기 — 기존 사용자는 명시적 enable, 새 사용자만 opt-in default |
| `ld.so.preload` 폐기로 cluster wide 격리 일시적 0 | 낮음-중 | Step A 후 즉시 namespace label 추가로 enabled namespace 격리 회복 |
| Webhook 의 volume mount 추가가 기존 pod spec 과 충돌 | 낮음 | mountPath 검증 (`/etc/vulkan/implicit_layer.d/hami.json` subPath) — 기존 nvidia_layers.json 과 공존 가능 |

## 12. 일정

| Step | 일정 | 결과물 |
|---|---|---|
| A | 1일 | helm chart commit + push, webhook config 변경, isaac-launchable baseline 안정 |
| B | 3-5일 | HAMi-core PR #182 추가 commits (cuda/nvml hook hardening) + unit test |
| C | 5-7일 | HAMi-core PR #182 추가 commits (Vulkan layer compat) + Isaac Sim init 통과 |
| D | 1-2일 | isaac-launchable opt-in label + 4-path 검증, 운영 회복 + 격리 동시 만족 |

**총 약 10-15일**.

## 13. 산출물

- HAMi 메인 (`xiilab/feat/vulkan-vgpu` PR #1803): helm chart 변경 commit 들
- HAMi-core (`xiilab/vulkan-layer` PR #182): hook hardening + Vulkan layer compat commits
- volcano-vgpu-device-plugin (`xiilab/pr/vulkan-upstream` PR #118): 변경 없음 (libvgpu.so hostPath mount 패턴 유지)
- 본 spec 문서 + 후속 implementation plans (`writing-plans` skill 출력)

## 14. 다음 단계

이 spec 검토 후 `writing-plans` skill 으로 Step A 부터 step-by-step implementation plan 생성 → step별 commit/PR push → 검증 → 다음 step.
