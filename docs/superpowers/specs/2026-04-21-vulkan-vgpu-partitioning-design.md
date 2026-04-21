# HAMi Vulkan vGPU 분할 — 설계 스펙

- 작성일: 2026-04-21
- 상태: 초안 (구현 전)
- 범위: NVIDIA GPU, Vulkan 컴퓨트 + 그래픽 워크로드
- 영향 레포: `Project-HAMi/HAMi` (Go), `Project-HAMi/HAMi-core` (C, `libvgpu/` submodule)

## 1. 문제 정의

HAMi는 `libvgpu.so`(HAMi-core)에서 CUDA 드라이버 API를 `LD_PRELOAD`로 가로채 NVIDIA GPU를 분할합니다. Vulkan 워크로드(컴퓨트 셰이더, `llama.cpp` Vulkan 백엔드, 렌더링 등)는 Vulkan이 별도 API 계층(`libvulkan.so` → ICD)이기 때문에 이 훅을 그대로 우회합니다. 결과적으로:

- `nvidia.com/gpumem`으로 선언한 VRAM 제한이 Vulkan 할당에는 **적용되지 않음**.
- `nvidia.com/gpucores` SM/코어 throttle이 Vulkan 큐 제출에는 **적용되지 않음**.
- 기본값으로 컨테이너에 **Vulkan 라이브러리 자체가 마운트되지 않음** — HAMi는 `NVIDIA_DRIVER_CAPABILITIES`를 건드리지 않고, NVIDIA Container Toolkit 기본값(`compute,utility`)에는 Vulkan ICD가 포함되지 않음.

이 설계 작성 시점에 레포 전체를 grep한 결과 `vulkan`/`VK_` 언급은 0건.

## 2. 목표

1. 같은 파드 내 Vulkan 메모리 할당에 대해 기존 `nvidia.com/gpumem` 버짓을 **CUDA와 공유**하여 강제한다 (물리 VRAM 한 개 = 버짓 한 개).
2. 기존 `nvidia.com/gpucores` SM throttle을 Vulkan 큐 제출에 강제한다.
3. 요청이 있을 때 Vulkan 라이브러리가 실제로 컨테이너에 도달하게 한다.
4. 완전한 하위 호환성 유지: Vulkan을 요청하지 않은 파드는 동작 변화 없음.

## 비목표 (Non-Goals)

- NVIDIA 외 벤더(AMD, Intel, Moore Threads)의 Vulkan 분할.
- CUDA/Vulkan 별도 VRAM 버짓 (물리 실체는 VRAM 단일 풀).
- `NVIDIA_VISIBLE_DEVICES`가 이미 걸러주는 것 이상의 `vkEnumeratePhysicalDevices` 필터링.
- 그래픽 프레임 페이싱 보장 — SM throttle은 렌더링 워크로드에 지터를 유발할 수 있음(문서화 대상, 해결 대상은 아님).

## 3. 결정 사항

| 항목 | 결정 | 근거 |
|------|------|------|
| 벤더 | NVIDIA 전용 | 기존 HAMi-core CUDA 훅 구조에 부합. |
| 제어 차원 | VRAM + SM | Vulkan으로 LLM 추론하는 수요에서 둘 다 필요. |
| 리소스 API | 기존 `nvidia.com/gpumem`, `nvidia.com/gpucores` 공유 버짓 | 물리 실체와 일치, 사용자 YAML 변경 없음. |
| 활성화 | 파드 annotation `hami.io/vulkan: "true"` opt-in | 모든 CUDA 전용 파드에 수십 MB 그래픽 라이브러리를 붙이지 않기 위해. |
| 후킹 방식 | HAMi-core `libvgpu.so`가 노출하는 Vulkan implicit layer | Vulkan 로더 표준 계약, LD_PRELOAD vs ICD 디스패치 이슈 회피. |
| 버짓 공유 | 프로세스 내 공유 카운터(기존 구조체 재사용) | 같은 `libvgpu.so` 인스턴스가 CUDA/Vulkan 훅을 모두 보유 → 별도 IPC 불필요. |

## 4. 아키텍처

```
┌───────────────────────────────────┐
│ Project-HAMi/HAMi    (Go)          │
│  pkg/device/nvidia/device.go       │  ← MutateAdmission 확장
│  (pkg/device/nvidia/device_test.go)│
└────────────┬──────────────────────┘
             │ env: HAMI_VULKAN_ENABLE=1,
             │      NVIDIA_DRIVER_CAPABILITIES⊇graphics
             ▼
┌───────────────────────────────────┐
│ 컨테이너                            │
│  NVIDIA Container Toolkit가         │
│  Vulkan ICD + libGLX_nvidia 마운트  │
│  HAMi device-plugin가               │
│  /usr/local/vgpu/libvgpu.so 마운트  │
└────────────┬──────────────────────┘
             │ Vulkan 로더가 implicit_layer.d 스캔
             ▼
┌───────────────────────────────────┐
│ Project-HAMi/HAMi-core  (C)        │
│  libvgpu.so                        │
│   ├─ 기존 CUDA 훅                  │
│   ├─ 신규 Vulkan 레이어            │
│   │     src/vulkan/*.c             │
│   └─ 공유 VRAM/SM 카운터           │
│  etc/vulkan/implicit_layer.d/      │
│   └─ hami.json (신규)              │
└───────────────────────────────────┘
```

## 5. 컴포넌트

### 5.1 HAMi (Go) — `pkg/device/nvidia/device.go`

신설 상수:
```go
const (
    VulkanEnableAnno       = "hami.io/vulkan"
    VulkanLayerName        = "VK_LAYER_HAMI_vgpu"
    NvidiaDriverCapsEnvVar = "NVIDIA_DRIVER_CAPABILITIES"
    HamiVulkanEnvVar       = "HAMI_VULKAN_ENABLE"
)
```

`MutateAdmission` 확장 (단, `hasResource == true`일 때만):
1. 파드 annotation `hami.io/vulkan`을 읽고 `"true"`일 때만 이후 로직 수행.
2. 신규 `NVIDIA_DRIVER_CAPABILITIES` 값 계산:
   - 컨테이너에 미설정이면: `"compute,utility,graphics"`로 설정.
   - 설정되어 있고 `"all"` 포함이면: 변경 없음.
   - 그 외: 콤마 구분 토큰 파싱 후 `"graphics"`와 합집합, 다시 직렬화.
3. `HAMI_VULKAN_ENABLE=1`이 없으면 추가.
4. `NVIDIA_VISIBLE_DEVICES`, RuntimeClass는 건드리지 않음 (기존 로직 그대로).

스케줄러 익스텐더, 리소스 회계, 디바이스 플러그인 할당 로직은 변경 없음.

### 5.2 HAMi-core (C) — 신규 모듈 `src/vulkan/`

파일 구성:
```
src/vulkan/
  layer.c            # vkNegotiateLoaderLayerInterfaceVersion,
                     # vk_layerGetInstanceProcAddr,
                     # vk_layerGetDeviceProcAddr
  layer.h
  dispatch.c         # VkInstance/VkDevice 별 next-layer 디스패치 테이블
  hooks_memory.c     # vkAllocateMemory, vkFreeMemory,
                     # vkGetPhysicalDeviceMemoryProperties/2
  hooks_buffer.c     # vkCreateBuffer, vkCreateImage,
                     # vkBindBufferMemory/2 (회계상 필요 시)
  hooks_submit.c     # vkQueueSubmit, vkQueueSubmit2
```

후킹 대상 엔트리포인트와 동작:

| 함수 | 동작 |
|------|------|
| `vkGetPhysicalDeviceMemoryProperties` | next-layer 호출 후 device-local 힙의 `size`를 `min(real, pod_budget)`로 클램핑. |
| `vkGetPhysicalDeviceMemoryProperties2` | 동일 로직, `pNext` 체인으로 처리. |
| `vkAllocateMemory` | 공유 카운터 락 획득. `used + allocationSize > budget`이면 언락 후 `VK_ERROR_OUT_OF_DEVICE_MEMORY`. 가능하면 잠정 `used += allocationSize`, 언락, next-layer 호출. next-layer 실패 시 롤백. `VkDeviceMemory → allocationSize` 매핑 저장. |
| `vkFreeMemory` | 매핑에서 size 조회, 락, `used -= size`, 언락, next-layer 호출, 매핑 제거. |
| `vkQueueSubmit` / `vkQueueSubmit2` | CUDA `cuLaunchKernel` 래퍼와 공통화한 throttle 유틸 호출: `nvmlDeviceGetUtilizationRates` 폴링 + `usleep(POLL_INTERVAL)`을 `util < cores_limit` 또는 최대 재시도까지 반복. 이후 next-layer 호출. |

레이어 ↔ 로더 계약:
- `vk_layer.h` 시그니처대로 `vkNegotiateLoaderLayerInterfaceVersion` export.
- 반환 구조체에 `vk_layerGetInstanceProcAddr` / `vk_layerGetDeviceProcAddr` 포인터 채움.
- `VkLayerInstanceCreateInfo` 체인에서 next-layer 포인터를 획득해 `VkInstance` 핸들 키의 디스패치 테이블에 저장.
- 훅 대상이 아닌 이름은 next-layer 포인터를 그대로 반환(pass-through).

### 5.3 공유 VRAM / SM 카운터

HAMi-core는 이미 CUDA 래퍼가 참조하는 per-device `device_memory` 구조체를 갖고 있음. Vulkan 래퍼는 **같은** API를 호출:
```c
// 의사코드
if (!reserve_device_memory(dev_idx, size)) return VK_ERROR_OUT_OF_DEVICE_MEMORY;
```
`reserve_device_memory` 내부 뮤텍스가 CUDA/Vulkan 경로를 직렬화. 신규 IPC, 신규 공유메모리 세그먼트 없음.

SM throttle 폴링 루프는 공통 유틸(`util_throttle(dev_idx)`)로 추출하여 `cuLaunchKernel` 래퍼(기존)와 `vkQueueSubmit` 래퍼(신규)가 공유.

### 5.4 Vulkan 레이어 매니페스트

파일: `etc/vulkan/implicit_layer.d/hami.json`. HAMi-core Dockerfile이 이미지의 `/etc/vulkan/implicit_layer.d/hami.json` 경로에 설치.

```json
{
  "file_format_version": "1.2.0",
  "layer": {
    "name": "VK_LAYER_HAMI_vgpu",
    "type": "GLOBAL",
    "library_path": "/usr/local/vgpu/libvgpu.so",
    "api_version": "1.3.0",
    "implementation_version": "1",
    "description": "HAMi Vulkan vGPU limiter",
    "enable_environment":  { "HAMI_VULKAN_ENABLE": "1" },
    "disable_environment": { "HAMI_VULKAN_DISABLE": "1" }
  }
}
```

`enable_environment`로 Go 웹훅이 주입한 env가 있을 때만 활성화되므로, 매니페스트가 존재하는 CUDA 전용 파드에서도 레이어는 비활성 상태.

### 5.5 빌드

- HAMi-core `Makefile`: `src/vulkan/*.c` 소스 추가, CFLAGS에 `-I$(VULKAN_SDK_INCLUDE)` 추가, 런타임 링크 없음(`libvulkan.so`는 로더가 dlopen).
- HAMi-core Dockerfile: `apt-get install vulkan-headers`(또는 동등 패키지), `etc/vulkan/implicit_layer.d/hami.json`을 이미지의 `/etc/vulkan/implicit_layer.d/`로 복사.

## 6. 데이터 흐름

### 6.1 Admission
1. 사용자가 `nvidia.com/gpumem: 3000`, `nvidia.com/gpucores: 30`, annotation `hami.io/vulkan: "true"`로 파드 생성.
2. HAMi 웹훅 `MutateAdmission` 기존 경로 — `NVIDIA_VISIBLE_DEVICES`, RuntimeClass 설정.
3. 신규 경로(annotation 존재 + `hasResource`): `NVIDIA_DRIVER_CAPABILITIES`에 `graphics` 합집합 병합, `HAMI_VULKAN_ENABLE=1` 추가.
4. 스케줄러/디바이스 플러그인 흐름은 변경 없음.

### 6.2 컨테이너 시작
1. NVIDIA Container Toolkit prestart 훅이 `NVIDIA_DRIVER_CAPABILITIES=compute,utility,graphics`를 감지해 Vulkan ICD JSON + `libGLX_nvidia.so.0` + `libnvidia-glvkspirv.so` 등을 마운트.
2. HAMi-core 이미지가 `libvgpu.so`와 `/etc/vulkan/implicit_layer.d/hami.json`을 이미 배치함.
3. Vulkan 로더가 `implicit_layer.d`를 스캔하고 `HAMI_VULKAN_ENABLE=1`을 확인한 뒤 `libvgpu.so`에서 `VK_LAYER_HAMI_vgpu` 로드.

### 6.3 런타임
- `vkAllocateMemory(size)` → 레이어 → 카운터 예약 → next-layer 또는 `VK_ERROR_OUT_OF_DEVICE_MEMORY`.
- `vkFreeMemory(mem)` → 레이어 → 카운터 반환 → next-layer.
- `vkGetPhysicalDeviceMemoryProperties` → next-layer → 힙 size 클램프 → 반환.
- `vkQueueSubmit` → 레이어 throttle 폴링 → next-layer.

### 6.4 공유 버짓 (CUDA + Vulkan 동시 사용)
두 경로 모두 하나의 뮤텍스로 보호되는 `reserve_device_memory(dev, size)`에 진입. API를 가로질러 합산된 활성 할당량은 파드 버짓을 초과하지 않음.

## 7. 에러 처리

| 상황 | 동작 |
|------|------|
| `HAMI_VULKAN_ENABLE` 미설정 | `enable_environment` 게이트 불통과 → 레이어 미활성화, Vulkan은 훅 없이 실행. |
| 런타임에 매니페스트 파일 누락 | 로더가 레이어를 발견 못 함 → Vulkan은 훅 없이 실행, HAMi-core 시작 프로브에서 경고 로그(추후). |
| 빌드 타임에 `vulkan-headers` 없음 | 컴파일 에러. 런타임 무관. |
| NVML 유틸리티 조회 실패 | throttle 스킵 (fail-open), errno 로그. |
| next-layer 체인 재진입 | 디스패치 테이블에서 저장된 next 포인터로 라우팅, 레이어 코드 비재진입 설계로 재귀 차단. |
| 멀티 physical device 컨테이너 | PCI 버스 ID / NVML 디바이스 핸들 기반 per-device 카운터. `NVIDIA_VISIBLE_DEVICES`가 이미 세트를 제한. |
| 예약 후 next-layer `vkAllocateMemory` 실패 | 카운터 롤백, 에러 그대로 반환. |
| 앱이 `VkDeviceMemory`를 leak (`vkFreeMemory` 호출 안 함) | 프로세스 동안 카운터 drift, 프로세스 종료 시 라이브러리 언로드로 해소. |
| non-NVIDIA 파드에 `hami.io/vulkan: true` annotation | NVIDIA 디바이스에서 `hasResource == false` → 조용히 no-op. |
| 사용자가 `NVIDIA_DRIVER_CAPABILITIES=all` 선설정 | 변경 없음 (`all` ⊇ `graphics`). |
| 사용자가 `NVIDIA_DRIVER_CAPABILITIES=compute` 선설정 | `compute,graphics`로 교체(합집합). |
| 사용자가 `NVIDIA_DRIVER_CAPABILITIES=compute,graphics` 선설정 | 변경 없음 (이미 `graphics` 포함). |

## 8. 테스트 전략

### 8.1 Go 단위 테스트 — `pkg/device/nvidia/device_test.go`
- `TestMutateAdmission_VulkanAnno_AddsGraphicsCap` — annotation + HAMi 리소스 → env에 `graphics`, `HAMI_VULKAN_ENABLE=1` 포함.
- `TestMutateAdmission_VulkanAnno_MergesExistingCaps` — 기존 `compute` 있음 → `compute,graphics`로 병합.
- `TestMutateAdmission_VulkanAnno_AllCaps_NoChange` — 기존 `all` 있음 → 변경 없음.
- `TestMutateAdmission_NoVulkanAnno_NoChange` — annotation 없음 → env 주입 없음.
- `TestMutateAdmission_VulkanAnno_NoGPUResource` — annotation만 있고 HAMi 리소스 없음 → no-op.
- `TestMutateAdmission_VulkanAnno_IdempotentHamiEnable` — 웹훅 재적용 시 `HAMI_VULKAN_ENABLE` 중복 추가되지 않음.

### 8.2 HAMi-core C 단위 테스트
- `vk_layerGetInstanceProcAddr` — 훅 대상 이름은 래퍼 반환, 그 외는 next-layer 포인터 반환.
- `vkAllocateMemory`:
  - 버짓 이내 → next-layer 호출, 카운터 증가.
  - 버짓 초과 → `VK_ERROR_OUT_OF_DEVICE_MEMORY`, next-layer 미호출, 카운터 불변.
  - next-layer 에러 반환 → 카운터 롤백.
- pthread 경쟁 스트레스: CUDA `cuMemAlloc` + Vulkan `vkAllocateMemory` 동시 실행 시 `used_memory ≤ budget` 불변식, 성공 합산이 버짓 초과 없음.
- `vkGetPhysicalDeviceMemoryProperties` 클램프: 반환된 구조체의 힙 size가 `min(real, budget)`.

### 8.3 통합 / E2E
- 신규 예제 `examples/nvidia/vulkan_example.yaml` — `hami.io/vulkan: "true"`, `nvidia.com/gpumem: 1024`, `vulkaninfo` 이미지. 검증(수동 또는 스크립트):
  - `vulkaninfo | grep heapSize`가 device-local 힙에서 ≤ 1024 MiB.
  - `vkAllocateMemory` 테스트 바이너리(또는 `vkcube --size-mb 2048`)가 `OUT_OF_DEVICE_MEMORY`로 실패.
- (수동, CI 미포함) Vulkan 백엔드 llama.cpp 파드에 `gpumem: 4096` + 7B 모델 — 버짓 초과 시 할당 실패 로그 확인. `docs/vulkan-vgpu-support.md`에 기록.

### 8.4 수동 검증 체크리스트 (문서)
- `vulkaninfo` 힙 size 클램프.
- `vkAllocateMemory` 버짓 초과 시 기대한 에러 반환.
- 큐 제출 집중 워크로드에서 `nvidia-smi` compute 사용률이 설정된 `gpucores` 근방에서 throttle.
- 한 파드에서 CUDA + Vulkan 혼합 워크로드가 합산 버짓을 준수.

## 9. 딜리버리 계획

두 레포에 걸친 변경, 순서:

1. **HAMi-core PR** (C): Vulkan 레이어 모듈, 매니페스트 JSON, Dockerfile 업데이트, Makefile 업데이트, C 단위 테스트. 신규 릴리스 태그(`vX.Y.0`).
2. **HAMi PR** (Go, 이 레포):
   - `pkg/device/nvidia/device.go` — annotation → env 주입.
   - `pkg/device/nvidia/device_test.go` — 단위 테스트.
   - `libvgpu` submodule 포인터를 신규 HAMi-core 릴리스로 갱신.
   - `examples/nvidia/vulkan_example.yaml`.
   - `docs/vulkan-vgpu-support.md` (영문 + `_cn.md`).

롤아웃: 기본 OFF (annotation 게이트). 기존 배포에 대한 마이그레이션/파괴적 변경 없음.

## 10. 미해결 / 후속 과제

- SM throttle 하 그래픽 워크로드의 프레임 페이싱 — `vkQueueSubmit` 지터 측정 후 후속 릴리스에서 throttle 모드 설정(`strict` vs `cooperative`) 옵션 필요할 수 있음.
- Vulkan Video 확장(`VK_KHR_video_queue`) — v1에서는 후킹 대상 아님.
- Vulkan 할당 거부에 대한 Prometheus 메트릭 — 후속.
- MPS 모드와의 상호작용 — MPS는 Vulkan을 노출하지 않음. annotation + MPS 모드 조합은 에러 또는 `hami-core` 모드로 폴백 + 경고. 구현 단계에서 최종 결정.
