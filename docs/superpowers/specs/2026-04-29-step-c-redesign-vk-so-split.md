# Step C 재설계 — Vulkan layer 를 별도 `libvgpu_vk.so` 로 분리

## 배경

2026-04-28 Step C 첫 시도(`docs/superpowers/plans/2026-04-28-hami-isolation-step-c-vulkan-layer-compat.md`)는 ws-node074 production 환경에서 regression 을 만들었다. 검증 데이터는 `libvgpu/docs/superpowers/notes/2026-04-28-vk-trace-isaac-sim.md` 와 같은 폴더의 dispatch lifetime audit 노트에 보존.

핵심 발견:

- pre-Step-C build (md5 `8f889313`) 를 LD_PRELOAD 했을 때 isaac-launchable-0 의 `runheadless.sh` 는 `exit=124 alive`.
- post-Step-C build (md5 `9586feee`, 추가 시도 `1048daaf`) 를 같은 환경에서 LD_PRELOAD 하면 `exit=139` 에서 NVIDIA driver init path crash.
- crash backtrace 는 `libvulkan.so.1` → `libGLX_nvidia.so.0!vk_icdNegotiateLoaderICDInterfaceVersion` → `libEGL_nvidia.so.0!__egl_Main` → `libc.so.6!__sigaction`.
- HAMI_VK_TRACE 카운트는 두 시도 모두 0 — 우리 layer wrapper 는 호출되지 않음.
- HAMI_HOOK 매칭 가설을 falsify 하기 위해 `EnumerateDeviceExtensionProperties` / `EnumerateDeviceLayerProperties` 를 `g_inst_head != NULL` 로 게이트했으나 동일 crash. 즉 regression 은 Vulkan wrapper 코드 path 가 아니라 **`.so` load-time / NVIDIA driver init 시점의 ELF 수준 영향**.

추가 진단 (nm/readelf diff, `7dcb5a4` clean rebuild md5 비교, `LD_DEBUG=symbols,bindings`) 은 sandbox 가 ws-node074 외 build 환경 부재로 차단. 코드 commits `996cb22`, `eea2beb` 는 이번 세션에서 revert (`83fd245`, `f52aada`). 노트 commits 은 보존하되 fork push 보류.

## 목적

regression 의 root cause 진단에 추가 시간을 쓰지 않고, **regression 이 구조적으로 발생할 수 없는 architecture 로 Step C 의 본래 목표 (Carbonite/Kit init 호환되는 Vulkan layer hardening) 를 달성**한다.

본래 목표는 plan 의 Section 1 그대로 — Vulkan layer 가 NVIDIA Isaac Sim Kit (Carbonite/OptiX/Aftermath) 의 Vulkan 초기화 경로에서 NULL deref 없이 dispatch chain 을 끝까지 forwarding.

## 핵심 결정

| # | 결정 | 선택 |
|---|---|---|
| 1 | 접근 | 새 architecture 우선. root cause 진단 spike 생략 |
| 2 | Vulkan layer 활성 trigger | manifest 만 (`/etc/vulkan/implicit_layer.d/hami.json`). LD_PRELOAD path 는 Vulkan 활성 안 함 |
| 3 | 분리 boundary | full Vulkan split — `src/vulkan/*` 전체를 새 `libvgpu_vk.so` 로 |
| 4 | 검증 환경 | local docker (`make build-in-docker`) + ws-node074 integration |
| 5 | 기존 `libvgpu.so` Vulkan 코드 | 완전 제거 — `vulkan_mod` 를 `libvgpu.so` build 에서 제외 |

## 비변경 사항

- HAMi-core (NVML/CUDA hook, allocator, multiprocess) 코드 변경 0. budget IPC 그대로.
- Step B 의 commits (`88143ab`, `275ba3d`, `01a58f1`, `7dcb5a4`) — CUDA NULL guards 보존.
- Step B 의 Vulkan 관련 commits (`2b6b875`, `91ca00c`, `93dd103`) — Carbonite SegFault 1차 수정, NVML hook 활성, deviceUUID zero fallback 모두 보존. `libvgpu_vk.so` 의 시작점은 이 Step B end 코드.
- Step A 의 webhook namespaceSelector (HAMi parent `master` 기반) 변경 0.
- Step D scope (isaac-launchable opt-in 활성화 + 4-path 검증) 그대로 — 이번 spec 은 .so / manifest 산출물만, 활성화 path 는 Step D 가 책임.
- Plan 첫 시도의 commits `996cb22`, `eea2beb` 는 revert 상태. Tasks 1+2 의 의도 (cache first next-gipa, GIPA/GDPA fallback) 는 새 architecture 검증 통과 후 별도 phase 에서 재도입 후보.

## Architecture

```
process (LD_PRELOAD'd or manifest-activated):

  ┌──────────────────────────────────────────────┐
  │ libvgpu.so   ← LD_PRELOAD by ld.so.preload   │
  │   - NVML hooks (nvmlDeviceGetMemoryInfo …)   │
  │   - CUDA hooks (cuMemAlloc …)                │
  │   - allocator + multiprocess (budget IPC)    │
  │   - exports: hami_core_budget_*, hami_core_  │
  │     get_partition_uuid(), …                  │
  │   - NO Vulkan symbols (vk* 미export)         │
  └──────────────────────────────────────────────┘
                       ▲
                       │ DT_NEEDED  (link-time dependency)
                       │ resolved at dlopen
  ┌──────────────────────────────────────────────┐
  │ libvgpu_vk.so   ← Vulkan loader dlopen via   │
  │                    /etc/vulkan/implicit_     │
  │                    layer.d/hami.json         │
  │   - layer.c, dispatch.c (entry points)       │
  │   - hooks_alloc/memory/submit                │
  │   - physdev_index, budget bridge, throttle   │
  │   - exports: vkGetInstanceProcAddr,          │
  │     vkGetDeviceProcAddr,                     │
  │     vkNegotiateLoaderLayerInterfaceVersion   │
  └──────────────────────────────────────────────┘
```

격리 속성:

- Vulkan 코드는 `libvgpu_vk.so` 에 단 1개 copy.
- `libvgpu.so` LD_PRELOAD 단독 시 Vulkan symbol 0 → loader/ICD 가 우리 export 와 collision 가능 surface 0. 4-28 trace 에서 발견된 LD_PRELOAD-only crash class 가 구조적으로 불가능.
- Vulkan layer 활성은 manifest dlopen path 만. Vulkan loader 가 chain 을 정상적으로 build 한 후 우리 layer 에 진입 → `g_inst_head` 가 항상 set 된 상태에서만 wrapper 동작.
- `libvgpu_vk.so` 의 DT_NEEDED 가 `libvgpu.so` 를 가리켜, manifest 활성 시점에 LD_PRELOAD 된 `libvgpu.so` 의 export 자동 resolve. `libvgpu.so` 가 process 에 없으면 dlopen 실패 → loader 가 layer 자동 skip → Isaac Sim alive (no HAMi enforcement). webhook 실수 시 fail-safe.

## Components

| 단위 | 위치 | 책임 | 의존성 |
|---|---|---|---|
| `libvgpu.so` (수정) | `src/CMakeLists.txt` | HAMi-core. `vulkan_mod` OBJECT lib 제거. budget/UUID 조회 함수 export | -lcuda, -lnvidia-ml |
| `libvgpu_vk.so` (신규) | `src/vulkan/CMakeLists.txt` | Vulkan layer entry + dispatch + hooks | DT_NEEDED libvgpu.so, -lpthread |
| budget bridge | `src/vulkan/budget.c` 확장 | `libvgpu.so` 의 `hami_core_*` 함수를 layer hooks 가 호출하는 thin wrapper. 기존 budget.c 가 이미 HAMi-core 와 layer 사이 bridge 역할이므로 별도 파일 신규 없음 | libvgpu.so export |
| `hami.json` manifest | install path 결정 (`/usr/local/vgpu/hami.json` + symlink `/etc/vulkan/implicit_layer.d/hami.json`) | Vulkan implicit layer 정의. `library_path` = `/usr/local/vgpu/libvgpu_vk.so` | (정적 file) |
| 기존 `tests/vulkan/` | 그대로 유지 | layer/dispatch unit tests | libvgpu_vk.so |

`libvgpu.so` 의 신규 export (HAMi-core 측 인터페이스):

- `hami_core_get_device_uuid_count()` — NVML idx 매핑
- `hami_core_get_device_memory_limit(int nvml_idx)` — partition 값
- `hami_core_budget_charge(int nvml_idx, size_t bytes)` — 할당 시 budget 차감
- `hami_core_budget_release(int nvml_idx, size_t bytes)` — 해제 시 복귀
- `hami_core_budget_remaining(int nvml_idx)` — 남은 한도 조회

prefix `hami_core_*` 통일. 기존 internal 이름 (`get_used_memory_for_uuid` 등) 은 그대로 두고, 외부 인터페이스는 별도 파일 (`src/hami_core_export.c` 또는 기존 `libvgpu.c` 끝에 export 블록 추가) 의 thin wrapper 로 명시 export. CMake `-fvisibility=hidden` default 적용 + 외부 인터페이스 함수에만 `__attribute__((visibility("default")))` 부착해서 export surface 를 의도된 5개로 좁힘.

## Data flow (production happy path)

```
1. Pod 시작
   → ld.so.preload 가 libvgpu.so LD_PRELOAD
   → NVML/CUDA hook 활성, partition 값 ready

2. Isaac Sim Kit 시작
   → Vulkan loader 가 implicit_layer.d/ scan
   → hami.json 발견 → libvgpu_vk.so dlopen
   → DT_NEEDED libvgpu.so 자동 resolve (이미 process 에 있음)
   → vkNegotiateLoaderLayerInterfaceVersion 호출

3. 앱이 vkCreateInstance
   → loader chain 거쳐 hami_vkCreateInstance
   → hami_instance_register, hook table 구성

4. 앱이 vkAllocateMemory
   → hami_vkAllocateMemory wrapper
   → hami_core_budget_remaining(idx) 조회 (libvgpu.so call)
   → 가능하면 next_alloc 호출 + hami_core_budget_charge
   → 한도 초과 시 VK_ERROR_OUT_OF_DEVICE_MEMORY

5. 앱이 vkGetPhysicalDeviceMemoryProperties
   → hooks_memory.c
   → hami_core_get_device_memory_limit 으로 raw 값 clamp
```

## Error handling

| 시나리오 | 동작 |
|---|---|
| `libvgpu.so` 부재 + manifest 활성 | `libvgpu_vk.so` dlopen 시 DT_NEEDED 해결 실패 → loader 가 layer 자동 skip → Isaac Sim alive (no HAMi enforcement) |
| manifest 부재 + `libvgpu.so` LD_PRELOAD | Vulkan loader 가 layer 발견 0 → libvgpu_vk.so 미load → NVML/CUDA hook 만 동작. Vulkan 호출은 raw — partition 안 됨, 운영자 책임 |
| `hami_vkCreateInstance` 안에서 chain 실패 | 기존과 동일: `VK_ERROR_INITIALIZATION_FAILED` 반환 |
| budget 차감 시 `libvgpu.so` 함수 NULL (불가하지만 방어) | `hami_vkAllocateMemory` 가 next_alloc 그대로 forward (no enforcement). 로깅만 |
| `physdev_index` UUID 매핑 실패 | 기존과 동일: NVML idx=0 fallback (single-GPU). `93dd103` 패치 그대로 |
| Vulkan wrapper 진입 후 NULL deref 가능 path | Step B end 의 NULL guards (`2b6b875`) 그대로 보존 |

Race / lifetime 분석은 기존 audit (`6fc7f9a` `2026-04-28-vk-dispatch-lifetime-audit.md`) 그대로 유효. 별도 .so 라도 같은 process · 같은 dispatch table — race surface 변경 없음.

## Testing

| 층 | 어디 실행 | 무엇 검증 |
|---|---|---|
| Unit (`test/vulkan/`) | local docker | 기존 `test_layer`, `test_memprops`, `test_alloc` 등이 새 `libvgpu_vk.so` 로 빌드/통과 |
| ELF / symbol diff | local | `nm -D libvgpu.so | grep '^.* T vk'` 결과 0줄. `nm -D libvgpu_vk.so` 에 `vkGetInstanceProcAddr`, `vkGetDeviceProcAddr`, `vkNegotiateLoaderLayerInterfaceVersion` 만 외부 export. `readelf -d libvgpu_vk.so | grep NEEDED` 에 libvgpu.so 포함 |
| Step B regression | local docker (LD_PRELOAD libvgpu.so) | `test_cuda_null_guards` 9/9 [OK] |
| LD_PRELOAD-only smoke | ws-node074 isaac-launchable-0 | LD_PRELOAD `libvgpu.so` (manifest 미설치) + runheadless.sh × 5 → 5/5 exit=124 alive crash=0. **regression class 가 사라졌다는 핵심 검증** |
| Manifest 활성 smoke (Step D 와 합치) | ws-node074 isaac-launchable-0 | LD_PRELOAD `libvgpu.so` + manifest hami.json + runheadless.sh × 5 → 5/5 alive + Vulkan partition enforce (44 GiB → 23 GiB clamp) |
| HAMI_VK_TRACE 수집 | ws-node074 manifest 활성 path | trace lines > 0 — layer 가 실제로 chain 에 진입했음 검증 |

## Production safety gate

이번 세션의 사고 재발 방지:

1. ws-node074 의 `/usr/local/vgpu/libvgpu.so` swap 전 항상 `.bak-pre-stepC2` 백업.
2. Swap 직후 baseline runheadless 1회 (no LD_PRELOAD) → alive 확인. 실패 시 즉시 restore.
3. Baseline 통과 시에만 LD_PRELOAD-forced 검증 진행.
4. 모든 swap 단계는 `md5sum` before/after 로 기록.
5. isaac-launchable-0 / isaac-launchable-1 의 3/3 Running steady state 가 swap 후에도 유지되는지 monitor.

## Compatibility / 호환성 약속

- 기존 manifest 사용자 (4-27 새벽 패치 시점에 manifest installer 가 활성된 환경) 는 manifest 의 `library_path` 만 update 하면 동작 — Vulkan layer 의 ABI / behavior 는 유지.
- Step D 의 활성화 webhook 은 manifest installer + LD_PRELOAD config 가 분리됨을 인지해야 함 (별도 .so 두 개 install).
- `libvgpu.so` 의 신규 export (`hami_core_*`) 는 추가일 뿐. 기존 internal 함수 변경 없음.

## Out of scope (이번 spec 에서 다루지 않음)

- Tasks 1+2 의 cache + GIPA fallback 재도입 — 새 architecture 검증 통과 후 별도 phase.
- root cause 진단 spike (ELF/symbol diff, LD_DEBUG) — `libvgpu_vk.so` 분리만으로 영향이 사라지는지 보고 결정.
- HAMi parent 의 webhook / namespaceSelector / opt-in label — Step A / Step D scope.
- `hami.json` manifest 의 자동 install/uninstall (DaemonSet 또는 webhook 주입) — Step D scope. 이번 spec 은 manifest 파일 자체와 그것이 가리킬 .so 만.

## PR

`Project-HAMi/HAMi-core` (libvgpu) 의 `vulkan-layer` branch 에 새 commits. 별도 PR 또는 PR #182 의 후속 commits. parent repo `HAMi` 의 submodule SHA bump 는 기존 PR #1803 또는 새 PR.

## Test plan (high level)

1. local docker `make build-in-docker` → `libvgpu.so` + `libvgpu_vk.so` 두 산출물 생성 검증.
2. local `nm -D` / `readelf -d` 로 export / NEEDED 검증.
3. local docker 에서 `test_cuda_null_guards` 9/9 + `test_layer`/`test_memprops`/`test_alloc` 통과.
4. ws-node074 swap → baseline runheadless alive → LD_PRELOAD-only × 5 alive (no manifest).
5. ws-node074 manifest 활성 (Step D 와 통합) → 5/5 alive + partition clamp + HAMI_VK_TRACE > 0.
