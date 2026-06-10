# Step C 재설계 — Vulkan layer 분리 (libvgpu_vk.so) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `src/vulkan/*` 전체를 `libvgpu_vk.so` (신규) 로 분리하고 `libvgpu.so` 에는 HAMi-core 만 남긴다. Vulkan layer 활성은 implicit_layer manifest path 만. 이렇게 해서 2026-04-28 발견된 LD_PRELOAD-only path crash class 가 구조적으로 발생 불가능해진다.

**Architecture:** 5개 HAMi-core 함수 (`oom_check`, `add_gpu_device_memory_usage`, `rm_gpu_device_memory_usage`, `get_current_device_memory_limit`, `rate_limiter`) 를 `hami_core_*` wrapper 로 명시 export → `libvgpu_vk.so` 가 DT_NEEDED 로 link → manifest dlopen 시점에 자동 resolve. Spec: `docs/superpowers/specs/2026-04-29-step-c-redesign-vk-so-split.md`.

**Tech Stack:** C, CMake (libvgpu/), Docker (`make build-in-docker`), kubectl, ws-node074. HAMi-core fork: `/Users/xiilab/git/HAMi/libvgpu`, branch `vulkan-layer` (현재 HEAD `83fd245` — Step C revert 상태).

---

## File Structure

| 파일 | 변경 종류 | 책임 |
|---|---|---|
| `libvgpu/src/include/hami_core_export.h` | Create | 5개 wrapper 함수 declaration. `__attribute__((visibility("default")))` |
| `libvgpu/src/hami_core_export.c` | Create | wrapper 정의 — 내부 HAMi-core 함수를 호출 |
| `libvgpu/src/CMakeLists.txt` | Modify | (a) `hami_core_export.c` 를 libvgpu.so source 에 추가 (b) `vulkan_mod` 를 libvgpu.so 에서 제거 (c) 신규 `libvgpu_vk` target 추가 |
| `libvgpu/src/vulkan/budget.c` | Modify | `extern` 선언 → `#include "hami_core_export.h"` + `hami_core_*` 호출 |
| `libvgpu/src/vulkan/throttle_adapter.c` | Modify | `extern rate_limiter` → `hami_core_throttle()` |
| `libvgpu/share/hami/hami.json` | Create | Vulkan implicit_layer manifest. `library_path` = `/usr/local/vgpu/libvgpu_vk.so` |

추가 산출물 (build):
- `build/libvgpu.so` — HAMi-core 만, `vk*` 미export
- `build/libvgpu_vk.so` — Vulkan layer, DT_NEEDED `libvgpu.so`

---

## Tasks

### Task 1: Add `hami_core_export.{h,c}` — explicit export interface

**Files:**
- Create: `libvgpu/src/include/hami_core_export.h`
- Create: `libvgpu/src/hami_core_export.c`

- [ ] **Step 1: Write the header**

```c
/* libvgpu/src/include/hami_core_export.h */
#ifndef HAMI_CORE_EXPORT_H_
#define HAMI_CORE_EXPORT_H_

#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

/* HAMi-core ↔ libvgpu_vk.so contract.
 * These are the only HAMi-core symbols libvgpu_vk.so depends on.
 * libvgpu.so MUST export them with default visibility; libvgpu_vk.so
 * picks them up via DT_NEEDED link at dlopen() time. */

/* Returns 1 if reserving `addon` bytes on device `dev` would exceed the
 * partition limit, else 0. */
int hami_core_oom_check(int dev, size_t addon);

/* Records `usage` bytes of allocation by (pid, dev). type==2 (DEVICE).
 * Returns 0 on success, non-zero on failure. */
int hami_core_add_memory_usage(int32_t pid, int dev, size_t usage, int type);

/* Releases `usage` bytes by (pid, dev). type==2 (DEVICE). 0 = success. */
int hami_core_rm_memory_usage(int32_t pid, int dev, size_t usage, int type);

/* Returns the partition byte-limit for device `dev`, or 0 = unlimited. */
uint64_t hami_core_get_memory_limit(int dev);

/* Consumes one rate-limiter token (claim size = 1*1). */
void hami_core_throttle(void);

#ifdef __cplusplus
}
#endif

#endif  /* HAMI_CORE_EXPORT_H_ */
```

- [ ] **Step 2: Write the implementation**

```c
/* libvgpu/src/hami_core_export.c */
#include "include/hami_core_export.h"

#include <stdint.h>
#include <stddef.h>

/* Internal HAMi-core symbols. Both libvgpu_vk.so and the wrappers below
 * see the SAME object code linked into libvgpu.so. We make these
 * symbols visible to other .so files only through the wrappers, never
 * directly: that keeps the libvgpu.so→libvgpu_vk.so contract narrow. */
extern int      oom_check(int dev, size_t addon);
extern int      add_gpu_device_memory_usage(int32_t pid, int dev, size_t usage, int type);
extern int      rm_gpu_device_memory_usage(int32_t pid, int dev, size_t usage, int type);
extern uint64_t get_current_device_memory_limit(int dev);
extern void     rate_limiter(int grids, int blocks);

#define HAMI_EXPORT __attribute__((visibility("default")))

HAMI_EXPORT int hami_core_oom_check(int dev, size_t addon) {
    return oom_check(dev, addon);
}

HAMI_EXPORT int hami_core_add_memory_usage(int32_t pid, int dev, size_t usage, int type) {
    return add_gpu_device_memory_usage(pid, dev, usage, type);
}

HAMI_EXPORT int hami_core_rm_memory_usage(int32_t pid, int dev, size_t usage, int type) {
    return rm_gpu_device_memory_usage(pid, dev, usage, type);
}

HAMI_EXPORT uint64_t hami_core_get_memory_limit(int dev) {
    return get_current_device_memory_limit(dev);
}

HAMI_EXPORT void hami_core_throttle(void) {
    rate_limiter(1, 1);
}
```

- [ ] **Step 3: Add to libvgpu.so build sources in `src/CMakeLists.txt`**

Find the line:
```cmake
add_library(${LIBVGPU} SHARED libvgpu.c utils.c log_utils.c $<TARGET_OBJECTS:nvml_mod> $<TARGET_OBJECTS:cuda_mod> $<TARGET_OBJECTS:allocator_mod> $<TARGET_OBJECTS:multiprocess_mod> $<TARGET_OBJECTS:vulkan_mod>)
```

Replace with (still includes vulkan_mod for now — Task 5 splits it):
```cmake
add_library(${LIBVGPU} SHARED libvgpu.c utils.c log_utils.c hami_core_export.c $<TARGET_OBJECTS:nvml_mod> $<TARGET_OBJECTS:cuda_mod> $<TARGET_OBJECTS:allocator_mod> $<TARGET_OBJECTS:multiprocess_mod> $<TARGET_OBJECTS:vulkan_mod>)
```

- [ ] **Step 4: Verify it compiles (local docker)**

```bash
cd /Users/xiilab/git/HAMi/libvgpu
make build-in-docker 2>&1 | tail -10
```

Expected: `Built target vgpu`, no errors. (Tests/test targets compile too.)

- [ ] **Step 5: Verify the wrappers are exported**

```bash
docker run --rm -v "$PWD:/work" -w /work ubuntu:22.04 bash -c \
  "apt-get -qq update >/dev/null && apt-get -qq install -y binutils >/dev/null && \
   nm -D --defined-only build/libvgpu.so | grep ' T hami_core_'"
```

Expected: 5 lines, one per `hami_core_*` wrapper. (Symbols of type T = exported text.)

- [ ] **Step 6: Commit**

```bash
cd /Users/xiilab/git/HAMi/libvgpu
git add src/include/hami_core_export.h src/hami_core_export.c src/CMakeLists.txt
git commit -s -m "feat(hami-core): explicit hami_core_* export wrappers" \
  -m "Five thin wrappers around the HAMi-core symbols that libvgpu_vk.so
will need after the upcoming Vulkan-layer split: oom_check,
add/rm_gpu_device_memory_usage, get_current_device_memory_limit,
rate_limiter.

All five carry __attribute__((visibility(\"default\"))) so that the
release build (-fvisibility=hidden) keeps the export surface narrow:
libvgpu_vk.so DT_NEEDED-resolves only these names and nothing else from
HAMi-core internals. No call-site changes yet — that follows in the next
commit."
```

---

### Task 2: Update src/vulkan/budget.c + throttle_adapter.c to call wrappers

**Files:**
- Modify: `libvgpu/src/vulkan/budget.c`
- Modify: `libvgpu/src/vulkan/throttle_adapter.c`

- [ ] **Step 1: Replace extern declarations in `src/vulkan/budget.c`**

Find the block (currently around line 22-30):
```c
extern int      oom_check(const int dev, size_t addon);
extern int      add_gpu_device_memory_usage(int32_t pid, int dev,
                                            size_t usage, int type);
extern int      rm_gpu_device_memory_usage(int32_t pid, int dev,
                                            size_t usage, int type);
extern uint64_t get_current_device_memory_limit(const int dev);
```

Replace with:
```c
#include "include/hami_core_export.h"
```

Then update each call site in the same file:
- `oom_check(dev, size)` → `hami_core_oom_check(dev, size)`
- `add_gpu_device_memory_usage(getpid(), dev, size, HAMI_MEM_TYPE_DEVICE)` → `hami_core_add_memory_usage(getpid(), dev, size, HAMI_MEM_TYPE_DEVICE)`
- `rm_gpu_device_memory_usage(getpid(), dev, size, HAMI_MEM_TYPE_DEVICE)` → `hami_core_rm_memory_usage(getpid(), dev, size, HAMI_MEM_TYPE_DEVICE)`
- `get_current_device_memory_limit(dev)` → `hami_core_get_memory_limit(dev)`

(Keep the `cuInit` extern — that's CUDA driver, not HAMi-core.)

- [ ] **Step 2: Update `src/vulkan/throttle_adapter.c`**

Replace the file body:
```c
#include "vulkan/throttle_adapter.h"
#include "include/hami_core_export.h"

void hami_vulkan_throttle(void) {
    /* Consume one token — represents "one queue submission". The
     * underlying rate_limiter interprets (grids*blocks) as the claim
     * size; the wrapper uses (1,1) so Vulkan submits compete fairly
     * with tiny CUDA kernel launches. */
    hami_core_throttle();
}
```

- [ ] **Step 3: Build (still combined libvgpu.so)**

```bash
cd /Users/xiilab/git/HAMi/libvgpu
make build-in-docker 2>&1 | tail -5
```

Expected: `Built target vgpu`, `Built target test_*`. No errors.

- [ ] **Step 4: Step B regression test under LD_PRELOAD**

Local docker run (ws-node074 not yet involved):
```bash
docker run --rm -v "$PWD/build:/build" --gpus none \
  ubuntu:22.04 bash -c \
  "LD_PRELOAD=/build/libvgpu.so /build/test/test_cuda_null_guards 2>&1 | tail -15; echo EXIT=\$?"
```

Expected: 9 `[OK]` lines, `EXIT=0`. (No GPU needed — test is hook-level NULL guards only.)

- [ ] **Step 5: Commit**

```bash
git add src/vulkan/budget.c src/vulkan/throttle_adapter.c
git commit -s -m "refactor(vulkan): use hami_core_* wrappers instead of internal externs" \
  -m "Replace the extern declarations of oom_check / add_/rm_gpu_device_
memory_usage / get_current_device_memory_limit / rate_limiter in
src/vulkan/budget.c and src/vulkan/throttle_adapter.c with calls
through the new include/hami_core_export.h interface.

This is a pure call-site rewrite — same runtime behavior, same .so
boundary (still linked into one libvgpu.so for now). The point is to
remove direct dependence on HAMi-core internal symbol names so the
upcoming libvgpu_vk.so split can keep DT_NEEDED narrow."
```

---

### Task 3: Pre-split sanity build (combined libvgpu.so still healthy)

This task is verification only — confirms Tasks 1+2 didn't break anything before we attempt the split.

**Files:** none (verification)

- [ ] **Step 1: Build clean**

```bash
cd /Users/xiilab/git/HAMi/libvgpu
rm -rf build
make build-in-docker 2>&1 | tail -8
```

Expected: `Built target vgpu`, `Built target strip_symbol`, no warnings about undefined references.

- [ ] **Step 2: Verify exports include `hami_core_*` and `vk*` (still combined)**

```bash
docker run --rm -v "$PWD/build:/build" ubuntu:22.04 bash -c \
  "apt-get -qq update >/dev/null && apt-get -qq install -y binutils >/dev/null && \
   echo '=== hami_core_* ==='; nm -D --defined-only /build/libvgpu.so | grep ' T hami_core_'; \
   echo '=== vk* ==='; nm -D --defined-only /build/libvgpu.so | grep ' T vk'"
```

Expected: 5 `hami_core_*` lines + 3 `vk*` lines (`vkGetInstanceProcAddr`, `vkGetDeviceProcAddr`, `vkNegotiateLoaderLayerInterfaceVersion`). Combined .so still has Vulkan exports because `vulkan_mod` is still linked in.

- [ ] **Step 3: Run all unit tests**

```bash
docker run --rm -v "$PWD/build:/build" ubuntu:22.04 bash -c \
  "cd /build/test && for t in test_cuda_null_guards test_layer test_memprops test_alloc; do \
     [ -x ./\$t ] && (echo '---' \$t '---'; ./\$t 2>&1 | tail -8; echo EXIT=\$?); \
   done"
```

Expected: each test prints `[OK]` lines and exits 0.

- [ ] **Step 4: No commit (verification only)**

If any check fails, STOP and ask controller — don't proceed to split.

---

### Task 4: Split CMake — create libvgpu_vk.so target, remove vulkan_mod from libvgpu.so

**Files:**
- Modify: `libvgpu/src/CMakeLists.txt`

- [ ] **Step 1: Edit `src/CMakeLists.txt`**

Find:
```cmake
add_library(${LIBVGPU} SHARED libvgpu.c utils.c log_utils.c hami_core_export.c $<TARGET_OBJECTS:nvml_mod> $<TARGET_OBJECTS:cuda_mod> $<TARGET_OBJECTS:allocator_mod> $<TARGET_OBJECTS:multiprocess_mod> $<TARGET_OBJECTS:vulkan_mod>)
target_compile_options(${LIBVGPU} PUBLIC ${LIBRARY_COMPILE_FLAGS})
target_compile_definitions(${LIBVGPU} PUBLIC HOOK_NVML_ENABLE)
target_link_libraries(${LIBVGPU} PUBLIC -lcuda -lnvidia-ml)

if (NOT CMAKE_BUILD_TYPE STREQUAL "Debug")
add_custom_target(strip_symbol ALL
    COMMAND strip -x ${CMAKE_BINARY_DIR}/lib${LIBVGPU}.so
    DEPENDS ${LIBVGPU})
endif()
```

Replace with:
```cmake
# libvgpu.so: HAMi-core only. Vulkan layer code now lives in libvgpu_vk.so.
add_library(${LIBVGPU} SHARED libvgpu.c utils.c log_utils.c hami_core_export.c $<TARGET_OBJECTS:nvml_mod> $<TARGET_OBJECTS:cuda_mod> $<TARGET_OBJECTS:allocator_mod> $<TARGET_OBJECTS:multiprocess_mod>)
target_compile_options(${LIBVGPU} PUBLIC ${LIBRARY_COMPILE_FLAGS})
target_compile_definitions(${LIBVGPU} PUBLIC HOOK_NVML_ENABLE)
target_link_libraries(${LIBVGPU} PUBLIC -lcuda -lnvidia-ml)

# libvgpu_vk.so: Vulkan implicit-layer code. Activated via
# /etc/vulkan/implicit_layer.d/hami.json (see share/hami/hami.json).
# DT_NEEDED links libvgpu.so so the loader resolves the hami_core_*
# wrappers when the Vulkan loader dlopen()s us.
set(LIBVGPU_VK vgpu_vk)
add_library(${LIBVGPU_VK} SHARED $<TARGET_OBJECTS:vulkan_mod>)
target_compile_options(${LIBVGPU_VK} PUBLIC ${LIBRARY_COMPILE_FLAGS})
target_link_libraries(${LIBVGPU_VK} PUBLIC ${LIBVGPU} -lpthread)

if (NOT CMAKE_BUILD_TYPE STREQUAL "Debug")
add_custom_target(strip_symbol ALL
    COMMAND strip -x ${CMAKE_BINARY_DIR}/lib${LIBVGPU}.so
    COMMAND strip -x ${CMAKE_BINARY_DIR}/lib${LIBVGPU_VK}.so
    DEPENDS ${LIBVGPU} ${LIBVGPU_VK})
endif()
```

Notes:
- `target_link_libraries(${LIBVGPU_VK} PUBLIC ${LIBVGPU} ...)` makes CMake emit `-lvgpu` on the linker command line; ld.so records this as DT_NEEDED `libvgpu.so` in the resulting `libvgpu_vk.so`.
- `vulkan_mod` 의 OBJECT lib 는 그대로 유지 — 두 target 중 하나에만 link됨.

- [ ] **Step 2: Build clean**

```bash
cd /Users/xiilab/git/HAMi/libvgpu
rm -rf build
make build-in-docker 2>&1 | tail -10
```

Expected: `Built target vgpu`, `Built target vgpu_vk`, both without warnings about undefined references.

- [ ] **Step 3: Verify both .so produced**

```bash
ls -la build/libvgpu.so build/libvgpu_vk.so
```

Expected: both files present, executable.

- [ ] **Step 4: Commit**

```bash
git add src/CMakeLists.txt
git commit -s -m "build: split Vulkan layer into separate libvgpu_vk.so" \
  -m "libvgpu.so loses vulkan_mod and now contains only HAMi-core
(NVML/CUDA hooks + allocator + multiprocess). libvgpu_vk.so is a new
shared target that holds all of src/vulkan/* and links libvgpu.so as
DT_NEEDED so the hami_core_* wrappers resolve when the Vulkan loader
dlopen()s the new .so via the implicit-layer manifest.

After this commit:
* nm -D libvgpu.so MUST NOT show vk*
* nm -D libvgpu_vk.so MUST show vkGetInstanceProcAddr,
  vkGetDeviceProcAddr, vkNegotiateLoaderLayerInterfaceVersion (and only
  those as exports thanks to -fvisibility=hidden + HAMI_LAYER_EXPORT).
* readelf -d libvgpu_vk.so MUST list libvgpu.so as NEEDED.

Step C plan: docs/superpowers/plans/2026-04-29-step-c-vk-so-split.md
Spec: docs/superpowers/specs/2026-04-29-step-c-redesign-vk-so-split.md"
```

---

### Task 5: ELF / symbol diff verification (the structural-isolation proof)

**Files:** none (verification only — but commit a script to docs/notes for future runs)

- [ ] **Step 1: Run the symbol-isolation check**

```bash
cd /Users/xiilab/git/HAMi/libvgpu
docker run --rm -v "$PWD/build:/build" ubuntu:22.04 bash -c '
apt-get -qq update >/dev/null
apt-get -qq install -y binutils >/dev/null
echo "=== libvgpu.so: must have hami_core_* but NO vk* ==="
echo "--- hami_core_* (expect 5) ---"
nm -D --defined-only /build/libvgpu.so | grep " T hami_core_" | wc -l
echo "--- vk* (expect 0) ---"
nm -D --defined-only /build/libvgpu.so | grep -E " T vk[A-Z]" | wc -l
echo
echo "=== libvgpu_vk.so: must have only the 3 layer entry points ==="
nm -D --defined-only /build/libvgpu_vk.so | grep " T " | grep -E "^[^[:space:]]+ T (vk[A-Z]|hami_)" | sort
echo
echo "=== libvgpu_vk.so: DT_NEEDED must include libvgpu.so ==="
readelf -d /build/libvgpu_vk.so | grep NEEDED
echo
echo "=== libvgpu_vk.so: undefined hami_core_* symbols (expect 5) ==="
nm -D --undefined-only /build/libvgpu_vk.so | grep "hami_core_" | wc -l
'
```

Expected:
- libvgpu.so hami_core_* count: `5`
- libvgpu.so vk* count: `0`
- libvgpu_vk.so exports: `vkGetDeviceProcAddr`, `vkGetInstanceProcAddr`, `vkNegotiateLoaderLayerInterfaceVersion` (3 lines, no `hami_*`)
- DT_NEEDED includes `libvgpu.so` and `libpthread.so.0`
- libvgpu_vk.so undefined hami_core_* count: `5`

If any check fails — STOP. The structural-isolation property is the whole point of Step C.

- [ ] **Step 2: No commit (verification only)**

---

### Task 6: Unit tests against the split build

**Files:** none (verification only)

- [ ] **Step 1: Step B regression — `test_cuda_null_guards` under LD_PRELOAD libvgpu.so**

```bash
docker run --rm -v "$PWD/build:/build" ubuntu:22.04 bash -c \
  "LD_PRELOAD=/build/libvgpu.so /build/test/test_cuda_null_guards 2>&1; echo EXIT=\$?"
```

Expected: 9 `[OK]` lines, `EXIT=0`. CUDA hook code unchanged across the split, so this MUST pass identically to Task 3 step 3.

- [ ] **Step 2: Vulkan unit tests against libvgpu_vk.so**

```bash
docker run --rm -v "$PWD/build:/build" ubuntu:22.04 bash -c '
for t in test_layer test_memprops test_alloc; do
  [ -x /build/test/$t ] || { echo "SKIP $t (not built)"; continue; }
  echo "--- $t ---"
  LD_LIBRARY_PATH=/build LD_PRELOAD=/build/libvgpu.so:/build/libvgpu_vk.so /build/test/$t 2>&1 | tail -10
  echo "EXIT=$?"
done'
```

Expected: each test exits 0 with its expected `[OK]` lines.

(Why both .so in LD_PRELOAD: the Vulkan unit tests fake the next-layer GIPA and don't go through Vulkan loader manifest activation, so we have to hand-load libvgpu_vk.so. This only matters for unit tests; production uses manifest dlopen.)

- [ ] **Step 3: No commit (verification only)**

---

### Task 7: Add Vulkan implicit-layer manifest file

**Files:**
- Create: `libvgpu/share/hami/hami.json`

- [ ] **Step 1: Write the manifest**

```json
{
  "file_format_version": "1.0.0",
  "layer": {
    "name": "VK_LAYER_HAMI_vgpu",
    "type": "INSTANCE",
    "library_path": "/usr/local/vgpu/libvgpu_vk.so",
    "api_version": "1.3.0",
    "implementation_version": "1",
    "description": "HAMi vGPU partition layer — clamps device-memory queries and tracks Vulkan allocations against the per-pod budget.",
    "instance_extensions": [],
    "device_extensions": []
  }
}
```

Save to `libvgpu/share/hami/hami.json`.

(Production install path: `/etc/vulkan/implicit_layer.d/hami.json`, typically a symlink to `/usr/local/vgpu/hami.json`. The webhook + DaemonSet that drops this file are Step D scope, not this plan.)

- [ ] **Step 2: Validate the JSON**

```bash
python3 -c "import json; json.load(open('share/hami/hami.json')); print('OK')"
```

Expected: `OK`.

- [ ] **Step 3: Commit**

```bash
git add share/hami/hami.json
git commit -s -m "feat(vulkan): ship hami.json implicit-layer manifest" \
  -m "Static manifest that the Step D webhook + DaemonSet will install
into /etc/vulkan/implicit_layer.d/ to activate libvgpu_vk.so via the
Vulkan loader. file_format_version 1.0.0, type INSTANCE, api 1.3.0.

library_path is the production install path /usr/local/vgpu/libvgpu_vk.so;
no extensions claimed (the layer only intercepts existing entry points)."
```

---

### Task 8: ws-node074 LD_PRELOAD-only smoke (the regression-killed proof)

**Files:** none (production-side verification)

This task verifies the structural-isolation property on the actual hardware that exhibited the 2026-04-28 regression. The expected outcome is that LD_PRELOAD `libvgpu.so` (Vulkan layer NOT activated, manifest absent) leaves Isaac Sim Kit unaffected — because `libvgpu.so` no longer exports any `vk*` symbols.

- [ ] **Step 1: Sync sources to ws-node074 and rebuild**

```bash
cd /Users/xiilab/git/HAMi/libvgpu
rsync -az --exclude=build --exclude=.git/objects/pack . root@10.61.3.74:/tmp/libvgpu-build/
ssh root@10.61.3.74 'cd /tmp/libvgpu-build && rm -rf .git build && git init -q && git add -A 2>&1 | tail -1 && git -c user.email=x@x -c user.name=x commit -q -m local --no-gpg-sign && make build-in-docker 2>&1 | tail -8'
```

Expected: Both `Built target vgpu` and `Built target vgpu_vk` lines.

- [ ] **Step 2: Verify backups + swap libvgpu.so only (NOT installing manifest yet)**

```bash
ssh root@10.61.3.74 '
md5sum /usr/local/vgpu/libvgpu.so /usr/local/vgpu/libvgpu.so.bak-pre-step-c
cp -av /usr/local/vgpu/libvgpu.so /usr/local/vgpu/libvgpu.so.bak-pre-stepC2 2>&1 | tail -1
cp -f /tmp/libvgpu-build/build/libvgpu.so /usr/local/vgpu/libvgpu.so
md5sum /tmp/libvgpu-build/build/libvgpu.so /usr/local/vgpu/libvgpu.so
ls -la /etc/vulkan/implicit_layer.d/   # confirm hami.json absent
'
```

Expected: pre-stepC2 backup created, swap completes, two md5 match (new file in place), `/etc/vulkan/implicit_layer.d/` shows only `nvidia_layers.json` (no `hami.json`).

- [ ] **Step 3: Baseline runheadless under no LD_PRELOAD (confirm swap doesn't break steady state)**

```bash
NEWPOD=$(kubectl -n isaac-launchable get pods --no-headers | grep '^isaac-launchable-0' | awk '{print $1}' | head -1)
echo "Pod: $NEWPOD"
kubectl -n isaac-launchable exec $NEWPOD -c vscode -- bash -lc '
pkill -KILL kit 2>/dev/null; sleep 2
timeout 45 env ACCEPT_EULA=y /isaac-sim/runheadless.sh > /tmp/c-baseline.log 2>&1
EC=$?
pkill -KILL kit 2>/dev/null
echo "exit=$EC crash=$(grep -c "Segmentation\|crash has occurred" /tmp/c-baseline.log) listen=$(ss -tunlp 2>/dev/null | grep -c -E :49100)"
'
```

Expected: `exit=124 crash=0 listen=1`. If anything else, STOP and restore from `.bak-pre-stepC2`.

- [ ] **Step 4: LD_PRELOAD-forced runheadless × 5 (the regression check)**

```bash
kubectl -n isaac-launchable exec $NEWPOD -c vscode -- bash -lc '
mkdir -p /tmp/v
PASS=0
for i in 1 2 3 4 5; do
  pkill -KILL kit 2>/dev/null; sleep 3
  timeout 50 env \
    ACCEPT_EULA=y \
    LD_PRELOAD=/usr/local/vgpu/libvgpu.so \
    /isaac-sim/runheadless.sh > /tmp/v/r$i.log 2>&1
  EC=$?
  CRASH=$(grep -cE "Segmentation fault|crash has occurred" /tmp/v/r$i.log)
  LISTEN=$(ss -tunlp 2>/dev/null | grep -c -E ":49100")
  echo "run $i: exit=$EC crash=$CRASH listen=$LISTEN"
  [ "$EC" = "124" ] && [ "$CRASH" = "0" ] && PASS=$((PASS+1))
  pkill -KILL kit 2>/dev/null
done
echo "PASS=$PASS / 5"
'
```

Expected: `PASS=5 / 5` with each run reporting `exit=124 crash=0 listen=1`.

If `PASS < 5`, the regression is NOT only-Vulkan-code — it lives in HAMi-core too. STOP. Restore `/usr/local/vgpu/libvgpu.so` from `.bak-pre-stepC2`. Open separate analysis (likely needs a full bisect on production hardware).

- [ ] **Step 5: HAMi-core init verification (NVML hook should still work)**

```bash
kubectl -n isaac-launchable exec $NEWPOD -c vscode -- bash -lc '
LD_PRELOAD=/usr/local/vgpu/libvgpu.so nvidia-smi --query-gpu=memory.total --format=csv,noheader
'
```

Expected: `23552 MiB` (clamped) — confirms NVML hook is active. If raw `46068 MiB`, partition env not picked up; investigate but NOT a Step C regression.

- [ ] **Step 6: No commit. Record outcome locally**

```bash
echo "Task 8 PASS=5/5: $(date -u +%Y-%m-%dT%H:%M:%SZ)" >> /tmp/step-c-task8-result.txt
```

(The commit comes in Task 10 with the submodule bump.)

---

### Task 9: ws-node074 manifest-activated smoke (Vulkan layer actually doing its job)

**Files:** none (production-side verification)

This task confirms the new architecture's happy path: `libvgpu.so` LD_PRELOAD'd + `libvgpu_vk.so` installed at `/usr/local/vgpu/libvgpu_vk.so` + `hami.json` at `/etc/vulkan/implicit_layer.d/hami.json` → Isaac Sim Kit alive AND partition enforced.

- [ ] **Step 1: Install libvgpu_vk.so + manifest on host**

```bash
ssh root@10.61.3.74 '
cp -av /tmp/libvgpu-build/build/libvgpu_vk.so /usr/local/vgpu/libvgpu_vk.so 2>&1 | tail -1
md5sum /usr/local/vgpu/libvgpu_vk.so
cp -av /tmp/libvgpu-build/share/hami/hami.json /etc/vulkan/implicit_layer.d/hami.json 2>&1 | tail -1
ls -la /etc/vulkan/implicit_layer.d/
'
```

Expected: both files in place. Manifest path now lists `hami.json` alongside `nvidia_layers.json`.

- [ ] **Step 2: Manifest-activated runheadless × 5 with HAMI_VK_TRACE on the first run only**

```bash
kubectl -n isaac-launchable exec $NEWPOD -c vscode -- bash -lc '
mkdir -p /tmp/v2
PASS=0
for i in 1 2 3 4 5; do
  pkill -KILL kit 2>/dev/null; sleep 3
  TRACE_ARG=""
  [ "$i" = "1" ] && TRACE_ARG="HAMI_VK_TRACE=1"
  timeout 50 env \
    ACCEPT_EULA=y \
    $TRACE_ARG \
    LD_PRELOAD=/usr/local/vgpu/libvgpu.so \
    /isaac-sim/runheadless.sh > /tmp/v2/r$i.log 2>&1
  EC=$?
  CRASH=$(grep -cE "Segmentation fault|crash has occurred" /tmp/v2/r$i.log)
  LISTEN=$(ss -tunlp 2>/dev/null | grep -c -E ":49100")
  echo "run $i: exit=$EC crash=$CRASH listen=$LISTEN"
  [ "$EC" = "124" ] && [ "$CRASH" = "0" ] && PASS=$((PASS+1))
  pkill -KILL kit 2>/dev/null
done
echo "PASS=$PASS / 5"
echo "=== run 1 trace lines ==="
grep -c HAMI_VK_TRACE /tmp/v2/r1.log
echo "=== run 1 top GIPA names ==="
grep "hami_vkGetInstanceProcAddr.*name=" /tmp/v2/r1.log | sed -e "s/.*name=//" -e "s/ .*//" | sort | uniq -c | sort -rn | head -20
'
```

Expected:
- `PASS=5 / 5`
- run 1 trace lines > 100 (layer is now actually being invoked through the chain)
- top GIPA names: `vkCreateInstance`, `vkGetPhysicalDeviceMemoryProperties*`, `vkAllocateMemory`, etc.

If `PASS < 5` even with manifest active, the layer code itself has a real bug. STOP, capture trace evidence, surface to controller.

If trace lines = 0 with manifest active, the loader didn't pick up our manifest. Inspect: `nvidia_layers.json` content vs ours, JSON syntax, file permissions on `/etc/vulkan/implicit_layer.d/hami.json`.

- [ ] **Step 3: Partition clamp verification under manifest-active path**

```bash
kubectl -n isaac-launchable exec $NEWPOD -c vscode -- bash -lc '
echo "=== nvidia-smi clamp via NVML hook ==="
LD_PRELOAD=/usr/local/vgpu/libvgpu.so nvidia-smi --query-gpu=memory.total --format=csv,noheader
echo "=== Vulkan vkGetPhysicalDeviceMemoryProperties via vk_partition_test (if present) ==="
if [ -f vk_partition_test.py ]; then
  LD_PRELOAD=/usr/local/vgpu/libvgpu.so /isaac-sim/python.sh vk_partition_test.py 2>&1 | head -30
  echo "EXIT=$?"
else
  echo "vk_partition_test.py 부재 — skip (Step D scope에서 작성)"
fi
'
```

Expected: nvidia-smi shows `23552 MiB`. If `vk_partition_test.py` exists, Vulkan-side memory query also clamped to `23552 MiB`.

- [ ] **Step 4: No commit (verification only)**

If the verification fails, STOP. Restore: `cp /usr/local/vgpu/libvgpu.so.bak-pre-stepC2 /usr/local/vgpu/libvgpu.so; rm /etc/vulkan/implicit_layer.d/hami.json`.

---

### Task 10: Push HAMi-core fork + bump parent submodule + draft PR comments

**Files:**
- Modify (parent repo): `libvgpu` submodule SHA bump
- Create: `/tmp/step-c-vk-split-pr-drafts/{pr182,pr1803}.md`

- [ ] **Step 1: Push libvgpu fork**

```bash
cd /Users/xiilab/git/HAMi/libvgpu
git log --oneline -10
git push xiilab vulkan-layer 2>&1 | tail -10
```

Expected: 4 new commits push successfully (the docs-only commits from the prior session + the Tasks 1-2-4-7 code commits).

- [ ] **Step 2: Bump parent HAMi submodule**

```bash
cd /Users/xiilab/git/HAMi
NEW_SHA=$(cd libvgpu && git rev-parse HEAD)
echo "new HAMi-core SHA: $NEW_SHA"
git add libvgpu
git commit -s -m "chore(libvgpu): bump HAMi-core for Step C — Vulkan layer split" \
  -m "Pulls in the Step C redesign: Vulkan layer code is now a separate
libvgpu_vk.so, activated by /etc/vulkan/implicit_layer.d/hami.json.
libvgpu.so retains only HAMi-core (NVML/CUDA hooks + allocator +
multiprocess) and loses all vk* exports.

Verified on ws-node074:
* LD_PRELOAD libvgpu.so without manifest → 5/5 runheadless exit=124
  alive (the 2026-04-28 regression class is gone).
* LD_PRELOAD libvgpu.so + hami.json manifest → 5/5 alive,
  HAMI_VK_TRACE > 100 lines, partition clamp 44 GiB → 23 GiB.

Spec: docs/superpowers/specs/2026-04-29-step-c-redesign-vk-so-split.md
Plan: docs/superpowers/plans/2026-04-29-step-c-vk-so-split.md"
git push xiilab feat/vulkan-vgpu 2>&1 | tail -5
```

- [ ] **Step 3: Draft PR comments — DO NOT POST**

```bash
mkdir -p /tmp/step-c-vk-split-pr-drafts

cat > /tmp/step-c-vk-split-pr-drafts/pr182.md <<'EOF'
## Step C redesigned — Vulkan layer split into libvgpu_vk.so

The 2026-04-28 attempt (commits since reverted) regressed `runheadless.sh`
under LD_PRELOAD on ws-node074 — see notes/2026-04-28-vk-trace-isaac-sim.md.
Trace evidence proved our layer wrappers were never called; the
regression lived at the .so-load boundary. Rather than spending more
diagnostic cycles on production hardware, this redesign makes that
class of regression structurally impossible.

| Commit | Change |
|---|---|
| (sha) | feat(hami-core): explicit hami_core_* export wrappers |
| (sha) | refactor(vulkan): use hami_core_* wrappers instead of internal externs |
| (sha) | build: split Vulkan layer into separate libvgpu_vk.so |
| (sha) | feat(vulkan): ship hami.json implicit-layer manifest |

### What changed
- `libvgpu.so` keeps NVML/CUDA hooks + allocator + multiprocess. Loses
  all `vk*` exports.
- New `libvgpu_vk.so` carries the entire `src/vulkan/*` and exports
  only `vkGetInstanceProcAddr`, `vkGetDeviceProcAddr`,
  `vkNegotiateLoaderLayerInterfaceVersion`. DT_NEEDED includes
  `libvgpu.so`, so the linker resolves the 5 `hami_core_*` wrappers at
  Vulkan-loader dlopen time.
- `share/hami/hami.json` is the implicit-layer manifest the Step D
  webhook drops into `/etc/vulkan/implicit_layer.d/`.

### Verification on ws-node074
- ELF: `nm -D libvgpu.so | grep 'T vk'` → 0 lines. `nm -D libvgpu_vk.so`
  → exactly 3 `vk*` exports. `readelf -d libvgpu_vk.so` lists
  `libvgpu.so` as NEEDED.
- Step B regression `test_cuda_null_guards`: 9/9 [OK] (CUDA hooks
  unchanged across the split).
- LD_PRELOAD `libvgpu.so` without manifest, `runheadless.sh` × 5: 5/5
  `exit=124 crash=0 listen=1`. **The 2026-04-28 regression class is
  gone.**
- LD_PRELOAD `libvgpu.so` + manifest, `runheadless.sh` × 5: 5/5 alive,
  `HAMI_VK_TRACE` > 100 lines (layer in chain), partition clamp
  44 GiB → 23 GiB.

### Out of scope
- The original Step C tasks (cache first next-gipa, GIPA/GDPA fallback,
  `EnumerateDevice*` hooks) were reverted and stay deferred until this
  architecture is verified in production. They will return as a follow-up
  PR after the split is in.

EOF

cat > /tmp/step-c-vk-split-pr-drafts/pr1803.md <<'EOF'
## Step C — Vulkan layer split (libvgpu_vk.so)

HAMi-core PR #182 redesigned Step C: `libvgpu.so` is now HAMi-core only,
and a new `libvgpu_vk.so` holds the Vulkan implicit layer. Activation
moves entirely to the manifest path, removing the LD_PRELOAD/Vulkan-
loader collision surface that bit us on 2026-04-28.

The `libvgpu` submodule pointer is bumped to `<NEW_HAMI_BUMP_SHA>`.

### Verification (ws-node074, isaac-launchable-0)
- LD_PRELOAD `libvgpu.so` without manifest: 5/5 `runheadless.sh` alive
  (regression class structurally gone).
- LD_PRELOAD `libvgpu.so` + `hami.json`: 5/5 alive, layer in chain
  (`HAMI_VK_TRACE > 0`), partition clamp 44 GiB → 23 GiB.

Spec: `docs/superpowers/specs/2026-04-29-step-c-redesign-vk-so-split.md`
Plan: `docs/superpowers/plans/2026-04-29-step-c-vk-so-split.md`
EOF

HAMI_BUMP_SHA=$(cd /Users/xiilab/git/HAMi && git rev-parse HEAD)
sed -i.bak "s/<NEW_HAMI_BUMP_SHA>/$HAMI_BUMP_SHA/g" /tmp/step-c-vk-split-pr-drafts/pr1803.md
rm /tmp/step-c-vk-split-pr-drafts/pr1803.md.bak

ls -la /tmp/step-c-vk-split-pr-drafts/
```

(SHA placeholders in pr182.md will be filled by the controller from `git log` output.)

- [ ] **Step 4: Report — DO NOT post comments. Wait for explicit user approval.**

---

## Self-Review

**1. Spec coverage:**
- Spec §"Architecture" (split, DT_NEEDED, manifest-only activation) → Tasks 1-4, 7
- Spec §"Components" (libvgpu.so loses vulkan_mod, libvgpu_vk.so, budget bridge update, hami.json) → Tasks 1-4, 7
- Spec §"Data flow" (production happy path) → Tasks 8-9 verify
- Spec §"Error handling" (libvgpu.so absent, manifest absent, etc.) → Task 8 covers `libvgpu.so` absent indirectly (we only test the present case here; absent case is "loader skips layer" which is library-loader behavior we trust); manifest-absent case is exactly Task 8's main test.
- Spec §"Testing" (unit + ELF + LD_PRELOAD-only smoke + manifest smoke + HAMI_VK_TRACE) → Tasks 3, 5, 6, 8, 9
- Spec §"Production safety gate" (backup before swap, baseline-after-swap check, md5 logging) → Task 8 step 2-3, plus restore guidance in step 4.
- Spec §"Out of scope" (Tasks 1+2 deferred, root-cause diagnostic skipped, webhook in Step A/D) → reflected in Task 10 PR draft language. ✅

**2. Placeholder scan:** Tasks 8 and 9 contain expected outputs and concrete kubectl/ssh commands. Task 10 PR drafts have one explicit `<NEW_HAMI_BUMP_SHA>` placeholder that's substituted in step 3 and a `(sha)` placeholder in pr182.md noted as "filled by the controller". No `TODO`/`TBD`/`figure out`/`add appropriate ...` patterns. ✅

**3. Type consistency:** `hami_core_oom_check` / `hami_core_add_memory_usage` / `hami_core_rm_memory_usage` / `hami_core_get_memory_limit` / `hami_core_throttle` — same names in header, .c, call sites, and verification grep. `LIBVGPU_VK = vgpu_vk` → `lib${LIBVGPU_VK}.so` = `libvgpu_vk.so` consistent across CMake + ELF checks + manifest `library_path`. ✅

**4. Scope check:** Single .so split + manifest. Plan-able as one implementation. Step D (manifest install via webhook + opt-in label activation) is the next plan, not this one. ✅

**5. Production safety:** Task 8 verifies before installing the manifest (LD_PRELOAD-only) precisely so we get the regression-killed proof first. Task 9 only proceeds if Task 8 passes. Both have explicit restore commands at failure. ✅

---

## Estimated time

| Task | 예상 |
|---|---|
| 1 hami_core_export wrappers | 25분 |
| 2 vulkan call-site rewrite | 15분 |
| 3 pre-split sanity build | 10분 |
| 4 CMake split | 20분 |
| 5 ELF / symbol diff verify | 10분 |
| 6 unit tests | 15분 |
| 7 manifest file | 10분 |
| 8 ws-node074 LD_PRELOAD-only smoke | 30분 |
| 9 ws-node074 manifest smoke | 30분 |
| 10 push + bump + PR drafts | 20분 |
| **총** | **약 3시간** |
