# HAMi-core layout notes (for Vulkan vGPU plan)

HAMi-core submodule root: `libvgpu/` (HAMi-core). This note records the real
symbol names and file locations that the Vulkan vGPU plan (Tasks 1.4, 1.6)
will need when extracting a shared throttle utility and a VRAM budget
counter adapter. No source in `libvgpu/` is modified by this task.

## 소스 구조

Top-level build artefacts:
- `libvgpu/CMakeLists.txt` — root CMake, adds `src/` and `test/` subdirs,
  generates `config/static_config.h` from `src/static_config.h.in`.
- `libvgpu/Makefile` — wrapper (`make build` → `./build.sh`,
  `make build-in-docker` runs the build inside an `nvidia/cuda:12.2.0-devel`
  container).
- `libvgpu/build.sh` — invokes cmake with flags
  `-DDLSYM_HOOK_ENABLE=1 -DMULTIPROCESS_LIMIT_ENABLE=1 -DHOOK_MEMINFO_ENABLE=1
  -DHOOK_NVML_ENABLE=1 -DCMAKE_BUILD_TYPE=Debug`, then `make -j$J`.

`libvgpu/src/` (not flat — it is split into feature directories, each with
its own `CMakeLists.txt` that produces an OBJECT library linked together
into `libvgpu.so`):

- `src/libvgpu.c` — top-level hook loader / dlsym dispatch (entrypoints).
- `src/utils.c` — misc helpers (`round_up`, env parsing).
- `src/static_config.h.in` — generated config header.
- `src/allocator/` — **VRAM accounting + oom-check + allocation list** layer.
  - `allocator.c`, `allocator.h` — defines `allocate_raw`, `free_raw`,
    `oom_check`, `add_chunk_only`, `remove_chunk_only`, etc.
- `src/cuda/` — CUDA driver API wrappers:
  - `memory.c` — `cuMemAlloc_v2`, `cuMemAllocManaged`, `cuMemAllocPitch_v2`,
    `cuMemFree_v2`, `cuLaunchKernel`, `cuLaunchKernelEx`,
    `cuLaunchCooperativeKernel`, `cuMemCreate`/`cuMemRelease` (VMM), …
  - `hook.c` — populates the cuda override table with the above symbols.
  - `device.c`, `context.c`, `stream.c`, `event.c`, `graph.c`.
- `src/nvml/` — NVML wrappers (`nvml_entry.c`, `hook.c`).
- `src/multiprocess/` — **shared-memory region (cross-process counters) +
  SM rate limiter**:
  - `multiprocess_memory_limit.c/.h` — `shared_region_t`, per-proc slots,
    `get_current_device_memory_limit`, `get_gpu_memory_usage`,
    `add_gpu_device_memory_usage`, `rm_gpu_device_memory_usage`,
    `pre_launch_kernel`.
  - `multiprocess_utilization_watcher.c/.h` — `rate_limiter`,
    `utilization_watcher` background thread, `init_utilization_watcher`,
    `delta()`/`change_token()` token-bucket logic.
  - `shrreg_tool.c` — standalone CLI for inspecting the shared region.
- `src/include/` — public headers (used by other subdirs via
  `include "include/…"`). Notable:
  - `memory_limit.h` — macros `ENSURE_RUNNING`, `INC_MEMORY_OR_RETURN_ERROR`,
    `DECL_MEMORY_ON_ERROR/_SUCCESS`.
  - `libcuda_hook.h`, `libnvml_hook.h` — override table enum/entries.
  - `nvml-subset.h`, `nvml_override.h`, `nvml_prefix.h`.
  - `log_utils.h` — `LOG_DEBUG/INFO/WARN/ERROR`, `CHECK_DRV_API`,
    `CHECK_NVML_API`, `CHECK_CU_RESULT`.

## VRAM 카운터 API (기존 CUDA 경로에서 사용)

All three primitives live in **allocator + multiprocess** layers. The CUDA
memory wrappers in `src/cuda/memory.c` call them.

### 예약 (reserve / budget check)

- **Signature**: `int oom_check(const int dev, size_t addon);`
- **Defined at**: `libvgpu/src/allocator/allocator.c:36`
- **Declared at**: `libvgpu/src/allocator/allocator.h:155`
- **Semantics**: reads `get_current_device_memory_limit(dev)` and
  `get_gpu_memory_usage(dev)`, returns `1` if `usage + addon > limit`
  (OOM, caller must fail), returns `0` if OK. If `limit == 0` (unlimited)
  always returns `0`. Note: this is a **check-only** primitive, it does
  NOT reserve/increment the counter.
- **Counter increment** happens later via
  `int add_gpu_device_memory_usage(int32_t pid, int dev, size_t usage, int type);`
  defined at `libvgpu/src/multiprocess/multiprocess_memory_limit.c:336`
  (declared at `…/multiprocess_memory_limit.h:147`).
  - Returns `CUDA_DEVICE_MEMORY_UPDATE_SUCCESS (0)` on success,
    `CUDA_DEVICE_MEMORY_UPDATE_FAILURE (1)` on failure.
- **Full reserve path used in CUDA wrappers**: the allocator wraps this in
  `int allocate_raw(CUdeviceptr *dptr, size_t size)` at
  `libvgpu/src/allocator/allocator.c:205`, which delegates to
  `add_chunk(...)` at `:103` → calls `oom_check` then the real
  `cuMemAlloc_v2`, then `add_gpu_device_memory_usage(getpid(), dev, size, 2)`.
- **Alt path** (for already-allocated buffers, e.g. managed/pitch/VMM):
  `int add_chunk_only(CUdeviceptr address, size_t size);` at
  `libvgpu/src/allocator/allocator.c:133` — same `oom_check` + counter
  increment but without invoking `cuMemAlloc_v2`.

### 해제 (release)

- **Signature**: `int free_raw(CUdeviceptr dptr);`
- **Defined at**: `libvgpu/src/allocator/allocator.c:213`
- **Declared at**: `libvgpu/src/allocator/allocator.h:159`
- **Semantics**: looks up `dptr` in `device_overallocated` list, calls real
  `cuMemFree_v2`, removes the entry, and calls
  `rm_gpu_device_memory_usage(getpid(), dev, t_size, 2)` (defined at
  `libvgpu/src/multiprocess/multiprocess_memory_limit.c:365`).
  Returns `0` on success, `-1` if pointer not found.
- **Alt release-only** (no real `cuMemFree`): `int remove_chunk_only(CUdeviceptr dptr);`
  at `libvgpu/src/allocator/allocator.c:185`.

### 버짓 조회 (budget / limit)

- **Signature**: `uint64_t get_current_device_memory_limit(const int dev);`
- **Defined at**: `libvgpu/src/multiprocess/multiprocess_memory_limit.c:828`
- **Declared at**: `libvgpu/src/multiprocess/multiprocess_memory_limit.h:126`
- **Semantics**: returns `region_info.shared_region->limit[dev]` from the
  cross-process shared region (populated from
  `CUDA_DEVICE_MEMORY_LIMIT_<dev>` env vars). Returns `0` when no limit is
  set (interpreted as "unlimited" by `oom_check`).
- **Companion usage getter**:
  `uint64_t get_current_device_memory_usage(const int dev);` at
  `…/multiprocess_memory_limit.c:846` — sum of `used[dev].total` across
  procs in the shared region; the lower-level
  `size_t get_gpu_memory_usage(const int dev);`
  (`…/multiprocess_memory_limit.c:243`) is what `oom_check` actually reads.

### 실패 시 반환 규약

- `oom_check` → `int`: **`1` = OOM (caller must fail)**, `0` = OK, `limit==0`
  also returns `0` (unlimited). Note: this is the **opposite** of the
  typical "0 = success" Unix convention.
- `allocate_raw` / `add_chunk` / `add_chunk_only` → `int`: `0` on success,
  `CUDA_ERROR_OUT_OF_MEMORY` (= `2`, a `CUresult`) on OOM, `-1` on malloc
  failure. Callers in `cuda/memory.c` compare against `CUDA_SUCCESS` (0).
- `free_raw` → `int`: `0` on success, `-1` if pointer not tracked.
- `add_gpu_device_memory_usage` / `rm_gpu_device_memory_usage` → `int`: `0`
  (`CUDA_DEVICE_MEMORY_UPDATE_SUCCESS`) on success, `1`
  (`CUDA_DEVICE_MEMORY_UPDATE_FAILURE`) on failure.
- `get_current_device_memory_limit` → `uint64_t`: the budget in bytes; `0`
  means "unlimited" (downstream code must treat 0 as a sentinel, not as
  "zero budget").

## SM throttle 루프 (CUDA launch 래퍼)

- **Wrapper file**: `libvgpu/src/cuda/memory.c`
  - `cuLaunchKernel`:        line 545 (calls `pre_launch_kernel()` then
    `rate_limiter(grids, blocks)` when `pidfound==1`).
  - `cuLaunchKernelEx`:       line 556.
  - `cuLaunchCooperativeKernel`: line 567 (only `pre_launch_kernel()`; no
    rate limiter — possible gap).
- **Throttle function**: `void rate_limiter(int grids, int blocks);`
  defined at `libvgpu/src/multiprocess/multiprocess_utilization_watcher.c:34`,
  declared at `…/multiprocess_utilization_watcher.h:20`.
- **Background producer**: `void* utilization_watcher();` at
  `…/multiprocess_utilization_watcher.c:178`, started by
  `init_utilization_watcher()` at line 213 (creates a pthread at line 218)
  when `0 < sm_limit <= 100`. Entry point called from `libvgpu.c:888`.
- **Loop structure (this is what Task 1.4 will extract)**:
  1. `rate_limiter` short-circuits if SM limit is `0` or `>=100`
     (unlimited) or if `get_utilization_switch() == 0`.
  2. It does **NOT** itself call `nvmlDeviceGetUtilizationRates` or
     `usleep`. Instead it implements a **token-bucket consumer**:
     ```
     do {
         before = g_cur_cuda_cores;                      // line 52
         if (before < 0) { nanosleep(&g_cycle, NULL); goto CHECK; }  // line 55
         after = before - kernel_size;
     } while (!CAS(&g_cur_cuda_cores, before, after));   // line 59
     ```
     When the shared counter is depleted it `nanosleep`s for
     `g_cycle = 10 ms` (`TIME_TICK * MILLISEC`, from
     `multiprocess_utilization_watcher.h:9`) and retries.
  3. The **actual NVML polling + token refill** runs in the separate
     background thread `utilization_watcher` (lines 178–211):
     ```
     while (1) {
         nanosleep(&g_wait, NULL);              // g_wait = 120 ms (header:14)
         init_gpu_device_utilization();
         get_used_gpu_utilization(userutil, &sysprocnum);
         share = delta(upper_limit, userutil[0], share);
         change_token(share);
     }
     ```
     `get_used_gpu_utilization` (`:121`) calls
     `nvmlDeviceGetComputeRunningProcesses` +
     `nvmlDeviceGetProcessUtilization` (not
     `nvmlDeviceGetUtilizationRates` — per-process sampling is used
     instead). The NVML `nvmlDeviceGetUtilizationRates` symbol **is**
     hooked (`src/nvml/nvml_entry.c:730`) but is a passthrough.
  4. Poll cadence: 120 ms refill loop (`g_wait`), 10 ms consumer backoff
     (`g_cycle`). Max iterations: unbounded (while loop).

**Implication for Task 1.4**: "throttle loop" here is actually a
producer/consumer pair. Extracting a shared utility for Vulkan probably
means extracting (a) a token-bucket consumer equivalent to
`rate_limiter`, and (b) sharing the existing background refill thread —
not extracting a simple `poll-utilization+usleep` helper, because that
pattern does not literally exist in the CUDA path. If Task 1.4 only wants
the passive "sleep-until-budget-available" semantics, the consumer loop
in `rate_limiter` (lines 50–60) is the single place to model on.

## 빌드 / 테스트

### Makefile 타겟
- `build` (default) — runs `./build.sh` locally (needs host CUDA at
  `$CUDA_HOME` or `/usr/local/cuda`).
- `build-in-docker` — bind-mounts the repo into
  `nvidia/cuda:12.2.0-devel-ubuntu20.04` and runs `build.sh` inside.

### CMakeLists 구조
- Root `CMakeLists.txt` (`libvgpu/CMakeLists.txt`) sets
  `LIBRARY_COMPILE_FLAGS = -shared -fPIC -D_GNU_SOURCE -fvisibility=hidden
  -Wall` (Debug adds `-g`, drops `-fvisibility=hidden`), generates
  `config/static_config.h` from the `.h.in` template (git hash/branch
  baked in), then `add_subdirectory(src)` and `add_subdirectory(test)`.
- `src/CMakeLists.txt` adds four subdirs (multiprocess, allocator, cuda,
  nvml), each of which declares an OBJECT library
  (`multiprocess_mod`, `allocator_mod`, `cuda_mod`, `nvml_mod`). The root
  then links them into a single SHARED lib target `vgpu`
  (= `libvgpu.so`), linking against `-lcuda -lnvidia-ml`. On Release a
  `strip_symbol` custom target strips the `.so`.
- `test/CMakeLists.txt` globs every `*.c` / `*.cu` under `test/` and
  builds one executable per file (linking `-lrt -lpthread -lnvidia-ml
  -lcuda -lcudart`). No unit-test framework, no `ctest` registration.

### 테스트 프레임워크
- **없음.** The `test/` directory contains bare CUDA sample programs
  (one-off allocation/launch harnesses) that are compiled into
  stand-alone binaries. There is no GoogleTest, no `ctest`, no
  assertion framework, no CI `make test`. Verification is manual
  (run a binary under `LD_PRELOAD=libvgpu.so`, inspect logs).
- `test/python/` holds four manual PyTorch/TF/MXNet smoke scripts
  (`limit_pytorch.py`, `limit_tensorflow.py`, `limit_tensorflow2.py`,
  `limit_mxnet.py`) copied into the build dir via a `python_test`
  custom target.

### test/ 디렉토리 파일 목록
```
test/CMakeLists.txt
test/test_alloc.c
test/test_alloc_hold.c
test/test_alloc_host.c
test/test_alloc_managed.c
test/test_alloc_pitch.c
test/test_create_3d_array.c
test/test_create_array.c
test/test_host_alloc.c
test/test_host_register.c
test/test_runtime_alloc.c
test/test_runtime_alloc_host.c
test/test_runtime_alloc_managed.c
test/test_runtime_host_alloc.c
test/test_runtime_host_register.c
test/test_runtime_launch.cu
test/test_utils.h
test/python/limit_mxnet.py
test/python/limit_pytorch.py
test/python/limit_tensorflow.py
test/python/limit_tensorflow2.py
```

## 기타 관찰

### Vulkan 헤더 의존성
- **현재 없음.** `grep -ri "vulkan\|VULKAN\|vk_" libvgpu/` returns zero
  files. The build links only `-lcuda -lnvidia-ml`; `CMakeLists.txt`
  references only `CUDA_HOME`. Any Vulkan layer work will have to add a
  new `src/vulkan/` subdir and new dependency on vulkan-headers /
  libvulkan.

### 후속 Task에 영향 주는 주의사항
1. **`oom_check` is check-only, not reserve+commit.** The CUDA path is:
   `oom_check` → real `cuMemAlloc` → `add_gpu_device_memory_usage` (or the
   combined `allocate_raw` / `add_chunk`). There is a TOCTOU window. For
   the Vulkan adapter (Task 1.6) we must replicate this two-step pattern
   (or add a new atomic `reserve(dev, size)` helper) and must commit the
   counter with `add_gpu_device_memory_usage(..., type=2)` after the
   Vulkan allocation succeeds.
2. **Sentinel value `limit == 0` means unlimited**, not "zero budget".
   Downstream Vulkan code must preserve this.
3. **Per-process accounting key is `getpid()`** (plus a shared-region
   `hostpid` fixed up by `update_host_pid()`). Vulkan allocations made
   from the same process should reuse the existing shared region slot,
   not allocate a new one.
4. **`rate_limiter` silently no-ops** when SM limit is `0`, `>=100`, or
   `get_utilization_switch()==0`. A Vulkan consumer that reuses this
   primitive inherits that behaviour — the Vulkan wrapper will need its
   own switch/env var if we want independent SM partitioning.
5. **`cuLaunchCooperativeKernel` at `src/cuda/memory.c:567` is missing
   the `rate_limiter` call** (only `pre_launch_kernel` runs). Not our
   bug to fix, but worth knowing when auditing throttle coverage.
6. **No unit-test framework.** If Task 1.4/1.6 want unit tests around
   the extracted utility, we will have to introduce one (GoogleTest or
   equivalent) inside `libvgpu/`, which is a submodule change. A less
   invasive option is to put unit tests on the HAMi (Go) side that
   exercise the C symbols via cgo, or write new stand-alone C binaries
   under `test/` following the current convention.
7. **Visibility is `-fvisibility=hidden` in Release builds.** Any new
   symbols that Vulkan wrappers need to export from `libvgpu.so` must be
   annotated (`__attribute__((visibility("default")))` or similar) or
   they will not be dlsym-resolvable.

## 시도한 검색 (참고)

```
grep -rn "oom_check" libvgpu/src/
  → allocator/allocator.h:155 decl, allocator.c:36 defn
grep -rn "allocate_raw\|free_raw\|add_chunk_only" libvgpu/src/
  → allocator/allocator.c:205 / :213 / :133
grep -rn "get_current_device_memory_limit\|get_gpu_memory_usage" libvgpu/src/
  → multiprocess/multiprocess_memory_limit.c:828 / :243
grep -rn "rate_limiter\|utilization_watcher\|nvmlDeviceGetUtilizationRates" libvgpu/src/
  → multiprocess/multiprocess_utilization_watcher.c:34 / :178
  → nvml/nvml_entry.c:730 (passthrough hook, not the throttle path)
grep -rin "vulkan\|VULKAN\|vk_" libvgpu/
  → (no matches)
```
