# Vulkan vGPU 분할 구현 계획

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `hami.io/vulkan: "true"` annotation을 붙인 파드의 Vulkan 메모리 할당과 큐 제출에 기존 `nvidia.com/gpumem` / `nvidia.com/gpucores` 버짓을 강제한다.

**Architecture:** HAMi-core(`libvgpu.so`)에 Vulkan implicit layer를 추가해 `vkAllocateMemory` / `vkFreeMemory` / `vkGetPhysicalDeviceMemoryProperties[2]` / `vkQueueSubmit[2]`를 가로챈다. 기존 CUDA 훅이 사용하는 per-device 메모리 카운터와 SM throttle 유틸을 그대로 재사용한다. HAMi(Go)의 `MutateAdmission`은 annotation을 감지해 `NVIDIA_DRIVER_CAPABILITIES`에 `graphics`를 합치고 `HAMI_VULKAN_ENABLE=1`을 주입한다.

**Tech Stack:** Go 1.22+ (HAMi), C11 + Vulkan 1.3 headers + pthread + NVML (HAMi-core), Docker multi-stage 빌드.

**Reference Spec:** `docs/superpowers/specs/2026-04-21-vulkan-vgpu-partitioning-design.md`

---

## Phase 0 — Submodule 초기화 및 탐색

### Task 0.1: HAMi-core submodule 초기화

**Files:**
- Modify: 없음 (체크아웃만)

- [ ] **Step 1: submodule 상태 확인**

Run:
```bash
git submodule status
```
Expected output contains `libvgpu` 항목. 앞에 `-`가 붙어 있으면 미초기화.

- [ ] **Step 2: submodule 초기화 및 체크아웃**

Run:
```bash
git submodule update --init --recursive libvgpu
```
Expected: `libvgpu/` 아래에 C 소스(`src/`, `Makefile` 등)가 체크아웃됨.

- [ ] **Step 3: 커밋 불필요 확인**

Run:
```bash
git status
```
Expected: working tree clean (submodule 포인터는 이미 `.gitmodules`의 pin과 일치).

---

### Task 0.2: HAMi-core 구조와 기존 카운터 API 탐색

**Files:**
- Create: `docs/superpowers/plans/notes/hami-core-layout.md` (임시 노트, 플랜 종료 후 삭제)

- [ ] **Step 1: 상위 구조 파악**

Run:
```bash
ls libvgpu/
ls libvgpu/src/
find libvgpu/src -maxdepth 2 -name "*.c" -o -name "*.h" | head -40
```
Expected: `libvgpu/src` 하위에 `cuda/`, `memory/` 또는 유사 디렉토리. 공유 헤더(`include/` 또는 `src/*.h`) 확인.

- [ ] **Step 2: VRAM 카운터 API 식별**

Run:
```bash
grep -rn "used_memory\|device_memory\|reserve_memory\|allocate_memory_check" libvgpu/src | head
grep -rn "cuMemAlloc\b" libvgpu/src | head
```
위 검색 결과에서 CUDA allocate 래퍼가 호출하는 "예약" 함수의 시그니처를 확보. 예시 후보: `int32_t oom_check(int, size_t)`, `void add_allocated(int, size_t)` 등.

- [ ] **Step 3: SM throttle 루프 식별**

Run:
```bash
grep -rn "nvmlDeviceGetUtilizationRates\|utilization_watchdog\|usleep\|sm_limit" libvgpu/src | head
```
기존 throttle 폴링 루프가 있는 파일과 함수명 확보.

- [ ] **Step 4: 테스트 프레임워크 식별**

Run:
```bash
ls libvgpu/test 2>/dev/null || ls libvgpu/tests 2>/dev/null
grep -rn "assert(" libvgpu/ 2>/dev/null | head
cat libvgpu/Makefile | head -60
```
테스트 타겟(`make test`, `make check` 등)과 디렉토리 위치 확보. 없으면 "테스트 타겟 없음"을 노트.

- [ ] **Step 5: 노트 기록**

Write `docs/superpowers/plans/notes/hami-core-layout.md` 내용 예시(실제 수치는 Step 2~4 결과로 채움):
```markdown
# HAMi-core layout notes

- src/cuda/memory.c — cuMemAlloc 래퍼. reserve 함수: `int reserve_device_memory(int dev, size_t size)` (L123)
- src/cuda/launch.c — cuLaunchKernel 래퍼. throttle 루프: `static void throttle_wait(int dev)` (L77)
- include/hami_core.h — 공통 헤더. device_memory 구조체 노출.
- test 디렉토리 없음. Makefile `make test` 타겟 없음 → assert.h + 자체 러너 추가 필요.
- Vulkan 헤더: 빌드 미의존. vulkan-headers 패키지 추가 필요.
```

- [ ] **Step 6: 커밋**

```bash
git add docs/superpowers/plans/notes/hami-core-layout.md
git commit -m "docs: HAMi-core layout notes for Vulkan plan"
```

---

## Phase 1 — HAMi-core Vulkan Layer (C)

이 Phase의 모든 작업은 `libvgpu/` 하위에서 진행됩니다. HAMi-core는 submodule이므로, Phase 마지막에 `libvgpu` 레포에 별도 브랜치/PR로 밀고, HAMi 쪽에서 submodule 포인터를 업데이트합니다.

### Task 1.1: 레이어 엔트리포인트 스켈레톤

**Files:**
- Create: `libvgpu/src/vulkan/layer.h`
- Create: `libvgpu/src/vulkan/layer.c`
- Create: `libvgpu/src/vulkan/dispatch.h`
- Create: `libvgpu/src/vulkan/dispatch.c`

- [ ] **Step 1: 실패 테스트 작성 — `vkNegotiateLoaderLayerInterfaceVersion` export 확인**

Create `libvgpu/test/vulkan/test_layer.c`:
```c
#include <assert.h>
#include <dlfcn.h>
#include <stdio.h>
#include <vulkan/vulkan.h>
#include <vulkan/vk_layer.h>

typedef VkResult (VKAPI_PTR *PFN_vkNegotiateLoaderLayerInterfaceVersion)(VkNegotiateLayerInterface*);

int main(void) {
    void *h = dlopen("./libvgpu.so", RTLD_NOW);
    assert(h != NULL);
    PFN_vkNegotiateLoaderLayerInterfaceVersion fn =
        (PFN_vkNegotiateLoaderLayerInterfaceVersion)
        dlsym(h, "vkNegotiateLoaderLayerInterfaceVersion");
    assert(fn != NULL);

    VkNegotiateLayerInterface iface = {0};
    iface.sType = LAYER_NEGOTIATE_INTERFACE_STRUCT;
    iface.loaderLayerInterfaceVersion = 2;
    VkResult r = fn(&iface);
    assert(r == VK_SUCCESS);
    assert(iface.pfnGetInstanceProcAddr != NULL);
    assert(iface.pfnGetDeviceProcAddr != NULL);
    printf("ok: layer entry point negotiates\n");
    return 0;
}
```

- [ ] **Step 2: 테스트가 빌드/실행 실패함 확인**

Run (from `libvgpu/`):
```bash
cc -o /tmp/t test/vulkan/test_layer.c -ldl && /tmp/t
```
Expected: 링크 실패 또는 `dlsym`이 NULL 반환 (심볼 미구현).

- [ ] **Step 3: `layer.h` 최소 헤더 작성**

Create `libvgpu/src/vulkan/layer.h`:
```c
#ifndef HAMI_VULKAN_LAYER_H
#define HAMI_VULKAN_LAYER_H

#include <vulkan/vulkan.h>
#include <vulkan/vk_layer.h>

#ifdef __cplusplus
extern "C" {
#endif

VK_LAYER_EXPORT VkResult VKAPI_CALL
vkNegotiateLoaderLayerInterfaceVersion(VkNegotiateLayerInterface *pVersionStruct);

PFN_vkVoidFunction VKAPI_CALL
hami_vkGetInstanceProcAddr(VkInstance instance, const char *pName);

PFN_vkVoidFunction VKAPI_CALL
hami_vkGetDeviceProcAddr(VkDevice device, const char *pName);

#ifdef __cplusplus
}
#endif

#endif /* HAMI_VULKAN_LAYER_H */
```

- [ ] **Step 4: `dispatch.h` 작성 (next-layer 포인터 테이블)**

Create `libvgpu/src/vulkan/dispatch.h`:
```c
#ifndef HAMI_VULKAN_DISPATCH_H
#define HAMI_VULKAN_DISPATCH_H

#include <vulkan/vulkan.h>
#include <vulkan/vk_layer.h>

typedef struct hami_instance_dispatch {
    VkInstance handle;
    PFN_vkGetInstanceProcAddr next_gipa;
    PFN_vkDestroyInstance DestroyInstance;
    PFN_vkEnumeratePhysicalDevices EnumeratePhysicalDevices;
    PFN_vkGetPhysicalDeviceMemoryProperties GetPhysicalDeviceMemoryProperties;
    PFN_vkGetPhysicalDeviceMemoryProperties2 GetPhysicalDeviceMemoryProperties2;
    struct hami_instance_dispatch *next;
} hami_instance_dispatch_t;

typedef struct hami_device_dispatch {
    VkDevice handle;
    VkPhysicalDevice physical;
    PFN_vkGetDeviceProcAddr next_gdpa;
    PFN_vkDestroyDevice DestroyDevice;
    PFN_vkAllocateMemory AllocateMemory;
    PFN_vkFreeMemory FreeMemory;
    PFN_vkQueueSubmit QueueSubmit;
    PFN_vkQueueSubmit2 QueueSubmit2;
    struct hami_device_dispatch *next;
} hami_device_dispatch_t;

hami_instance_dispatch_t *hami_instance_lookup(VkInstance inst);
hami_instance_dispatch_t *hami_instance_register(VkInstance inst, PFN_vkGetInstanceProcAddr gipa);
void hami_instance_unregister(VkInstance inst);

hami_device_dispatch_t *hami_device_lookup(VkDevice dev);
hami_device_dispatch_t *hami_device_register(VkDevice dev, VkPhysicalDevice phys, PFN_vkGetDeviceProcAddr gdpa);
void hami_device_unregister(VkDevice dev);

#endif /* HAMI_VULKAN_DISPATCH_H */
```

- [ ] **Step 5: `dispatch.c` 작성 (단순 linked list + pthread mutex)**

Create `libvgpu/src/vulkan/dispatch.c`:
```c
#include "dispatch.h"
#include <pthread.h>
#include <stdlib.h>
#include <string.h>

static hami_instance_dispatch_t *g_inst_head = NULL;
static hami_device_dispatch_t   *g_dev_head  = NULL;
static pthread_mutex_t g_lock = PTHREAD_MUTEX_INITIALIZER;

static void *resolve(PFN_vkGetInstanceProcAddr gipa, VkInstance inst, const char *name) {
    return (void *)gipa(inst, name);
}

hami_instance_dispatch_t *hami_instance_register(VkInstance inst, PFN_vkGetInstanceProcAddr gipa) {
    hami_instance_dispatch_t *d = calloc(1, sizeof(*d));
    d->handle   = inst;
    d->next_gipa = gipa;
    d->DestroyInstance                    = (PFN_vkDestroyInstance)                    resolve(gipa, inst, "vkDestroyInstance");
    d->EnumeratePhysicalDevices           = (PFN_vkEnumeratePhysicalDevices)           resolve(gipa, inst, "vkEnumeratePhysicalDevices");
    d->GetPhysicalDeviceMemoryProperties  = (PFN_vkGetPhysicalDeviceMemoryProperties)  resolve(gipa, inst, "vkGetPhysicalDeviceMemoryProperties");
    d->GetPhysicalDeviceMemoryProperties2 = (PFN_vkGetPhysicalDeviceMemoryProperties2) resolve(gipa, inst, "vkGetPhysicalDeviceMemoryProperties2");

    pthread_mutex_lock(&g_lock);
    d->next = g_inst_head;
    g_inst_head = d;
    pthread_mutex_unlock(&g_lock);
    return d;
}

hami_instance_dispatch_t *hami_instance_lookup(VkInstance inst) {
    pthread_mutex_lock(&g_lock);
    hami_instance_dispatch_t *p = g_inst_head;
    while (p && p->handle != inst) p = p->next;
    pthread_mutex_unlock(&g_lock);
    return p;
}

void hami_instance_unregister(VkInstance inst) {
    pthread_mutex_lock(&g_lock);
    hami_instance_dispatch_t **pp = &g_inst_head;
    while (*pp && (*pp)->handle != inst) pp = &(*pp)->next;
    if (*pp) { hami_instance_dispatch_t *victim = *pp; *pp = victim->next; free(victim); }
    pthread_mutex_unlock(&g_lock);
}

static void *resolve_dev(PFN_vkGetDeviceProcAddr gdpa, VkDevice dev, const char *name) {
    return (void *)gdpa(dev, name);
}

hami_device_dispatch_t *hami_device_register(VkDevice dev, VkPhysicalDevice phys, PFN_vkGetDeviceProcAddr gdpa) {
    hami_device_dispatch_t *d = calloc(1, sizeof(*d));
    d->handle   = dev;
    d->physical = phys;
    d->next_gdpa = gdpa;
    d->DestroyDevice   = (PFN_vkDestroyDevice)   resolve_dev(gdpa, dev, "vkDestroyDevice");
    d->AllocateMemory  = (PFN_vkAllocateMemory)  resolve_dev(gdpa, dev, "vkAllocateMemory");
    d->FreeMemory      = (PFN_vkFreeMemory)      resolve_dev(gdpa, dev, "vkFreeMemory");
    d->QueueSubmit     = (PFN_vkQueueSubmit)     resolve_dev(gdpa, dev, "vkQueueSubmit");
    d->QueueSubmit2    = (PFN_vkQueueSubmit2)    resolve_dev(gdpa, dev, "vkQueueSubmit2");

    pthread_mutex_lock(&g_lock);
    d->next = g_dev_head;
    g_dev_head = d;
    pthread_mutex_unlock(&g_lock);
    return d;
}

hami_device_dispatch_t *hami_device_lookup(VkDevice dev) {
    pthread_mutex_lock(&g_lock);
    hami_device_dispatch_t *p = g_dev_head;
    while (p && p->handle != dev) p = p->next;
    pthread_mutex_unlock(&g_lock);
    return p;
}

void hami_device_unregister(VkDevice dev) {
    pthread_mutex_lock(&g_lock);
    hami_device_dispatch_t **pp = &g_dev_head;
    while (*pp && (*pp)->handle != dev) pp = &(*pp)->next;
    if (*pp) { hami_device_dispatch_t *victim = *pp; *pp = victim->next; free(victim); }
    pthread_mutex_unlock(&g_lock);
}
```

- [ ] **Step 6: `layer.c` 작성 (엔트리포인트 + `vkCreateInstance` / `vkCreateDevice` 훅)**

Create `libvgpu/src/vulkan/layer.c`:
```c
#include "layer.h"
#include "dispatch.h"
#include <string.h>
#include <stdlib.h>

/* forward declarations for hooks implemented in sibling files */
extern void hami_vk_hook_instance(hami_instance_dispatch_t *d);
extern void hami_vk_hook_device(hami_device_dispatch_t *d);

static VkLayerInstanceCreateInfo *find_chain_info(const VkInstanceCreateInfo *pCreateInfo,
                                                  VkLayerFunction func) {
    const VkLayerInstanceCreateInfo *ci = pCreateInfo->pNext;
    while (ci) {
        if (ci->sType == VK_STRUCTURE_TYPE_LOADER_INSTANCE_CREATE_INFO && ci->function == func) {
            return (VkLayerInstanceCreateInfo *)ci;
        }
        ci = (const VkLayerInstanceCreateInfo *)ci->pNext;
    }
    return NULL;
}

static VkLayerDeviceCreateInfo *find_dev_chain_info(const VkDeviceCreateInfo *pCreateInfo,
                                                    VkLayerFunction func) {
    const VkLayerDeviceCreateInfo *ci = pCreateInfo->pNext;
    while (ci) {
        if (ci->sType == VK_STRUCTURE_TYPE_LOADER_DEVICE_CREATE_INFO && ci->function == func) {
            return (VkLayerDeviceCreateInfo *)ci;
        }
        ci = (const VkLayerDeviceCreateInfo *)ci->pNext;
    }
    return NULL;
}

static VKAPI_ATTR VkResult VKAPI_CALL
hami_vkCreateInstance(const VkInstanceCreateInfo *pCreateInfo,
                      const VkAllocationCallbacks *pAllocator,
                      VkInstance *pInstance) {
    VkLayerInstanceCreateInfo *chain = find_chain_info(pCreateInfo, VK_LAYER_LINK_INFO);
    if (!chain || !chain->u.pLayerInfo) return VK_ERROR_INITIALIZATION_FAILED;

    PFN_vkGetInstanceProcAddr next_gipa = chain->u.pLayerInfo->pfnNextGetInstanceProcAddr;
    chain->u.pLayerInfo = chain->u.pLayerInfo->pNext;

    PFN_vkCreateInstance next_create =
        (PFN_vkCreateInstance)next_gipa(VK_NULL_HANDLE, "vkCreateInstance");
    VkResult r = next_create(pCreateInfo, pAllocator, pInstance);
    if (r != VK_SUCCESS) return r;

    hami_instance_dispatch_t *d = hami_instance_register(*pInstance, next_gipa);
    hami_vk_hook_instance(d);
    return VK_SUCCESS;
}

static VKAPI_ATTR void VKAPI_CALL
hami_vkDestroyInstance(VkInstance instance, const VkAllocationCallbacks *pAllocator) {
    hami_instance_dispatch_t *d = hami_instance_lookup(instance);
    if (d) d->DestroyInstance(instance, pAllocator);
    hami_instance_unregister(instance);
}

static VKAPI_ATTR VkResult VKAPI_CALL
hami_vkCreateDevice(VkPhysicalDevice physicalDevice,
                    const VkDeviceCreateInfo *pCreateInfo,
                    const VkAllocationCallbacks *pAllocator,
                    VkDevice *pDevice) {
    VkLayerDeviceCreateInfo *chain = find_dev_chain_info(pCreateInfo, VK_LAYER_LINK_INFO);
    if (!chain || !chain->u.pLayerInfo) return VK_ERROR_INITIALIZATION_FAILED;

    PFN_vkGetInstanceProcAddr next_gipa = chain->u.pLayerInfo->pfnNextGetInstanceProcAddr;
    PFN_vkGetDeviceProcAddr   next_gdpa = chain->u.pLayerInfo->pfnNextGetDeviceProcAddr;
    chain->u.pLayerInfo = chain->u.pLayerInfo->pNext;

    PFN_vkCreateDevice next_create =
        (PFN_vkCreateDevice)next_gipa(VK_NULL_HANDLE, "vkCreateDevice");
    VkResult r = next_create(physicalDevice, pCreateInfo, pAllocator, pDevice);
    if (r != VK_SUCCESS) return r;

    hami_device_dispatch_t *d = hami_device_register(*pDevice, physicalDevice, next_gdpa);
    hami_vk_hook_device(d);
    return VK_SUCCESS;
}

static VKAPI_ATTR void VKAPI_CALL
hami_vkDestroyDevice(VkDevice device, const VkAllocationCallbacks *pAllocator) {
    hami_device_dispatch_t *d = hami_device_lookup(device);
    if (d) d->DestroyDevice(device, pAllocator);
    hami_device_unregister(device);
}

/* GIPA / GDPA: return our wrappers for hooked names, next-layer for the rest. */

/* Hooked functions implemented in other TUs; declarations here. */
VKAPI_ATTR void VKAPI_CALL hami_vkGetPhysicalDeviceMemoryProperties(VkPhysicalDevice, VkPhysicalDeviceMemoryProperties*);
VKAPI_ATTR void VKAPI_CALL hami_vkGetPhysicalDeviceMemoryProperties2(VkPhysicalDevice, VkPhysicalDeviceMemoryProperties2*);
VKAPI_ATTR VkResult VKAPI_CALL hami_vkAllocateMemory(VkDevice, const VkMemoryAllocateInfo*, const VkAllocationCallbacks*, VkDeviceMemory*);
VKAPI_ATTR void     VKAPI_CALL hami_vkFreeMemory(VkDevice, VkDeviceMemory, const VkAllocationCallbacks*);
VKAPI_ATTR VkResult VKAPI_CALL hami_vkQueueSubmit(VkQueue, uint32_t, const VkSubmitInfo*, VkFence);
VKAPI_ATTR VkResult VKAPI_CALL hami_vkQueueSubmit2(VkQueue, uint32_t, const VkSubmitInfo2*, VkFence);

#define HAMI_HOOK(name) do { if (strcmp(pName, "vk" #name) == 0) return (PFN_vkVoidFunction)hami_vk##name; } while (0)

PFN_vkVoidFunction VKAPI_CALL
hami_vkGetInstanceProcAddr(VkInstance instance, const char *pName) {
    HAMI_HOOK(CreateInstance);
    HAMI_HOOK(DestroyInstance);
    HAMI_HOOK(CreateDevice);
    HAMI_HOOK(GetInstanceProcAddr);
    HAMI_HOOK(GetPhysicalDeviceMemoryProperties);
    HAMI_HOOK(GetPhysicalDeviceMemoryProperties2);

    hami_instance_dispatch_t *d = hami_instance_lookup(instance);
    if (!d) return NULL;
    return d->next_gipa(instance, pName);
}

PFN_vkVoidFunction VKAPI_CALL
hami_vkGetDeviceProcAddr(VkDevice device, const char *pName) {
    HAMI_HOOK(DestroyDevice);
    HAMI_HOOK(GetDeviceProcAddr);
    HAMI_HOOK(AllocateMemory);
    HAMI_HOOK(FreeMemory);
    HAMI_HOOK(QueueSubmit);
    HAMI_HOOK(QueueSubmit2);

    hami_device_dispatch_t *d = hami_device_lookup(device);
    if (!d) return NULL;
    return d->next_gdpa(device, pName);
}

VK_LAYER_EXPORT VkResult VKAPI_CALL
vkNegotiateLoaderLayerInterfaceVersion(VkNegotiateLayerInterface *pVersionStruct) {
    if (pVersionStruct->sType != LAYER_NEGOTIATE_INTERFACE_STRUCT)
        return VK_ERROR_INITIALIZATION_FAILED;

    if (pVersionStruct->loaderLayerInterfaceVersion > 2)
        pVersionStruct->loaderLayerInterfaceVersion = 2;

    pVersionStruct->pfnGetInstanceProcAddr = hami_vkGetInstanceProcAddr;
    pVersionStruct->pfnGetDeviceProcAddr   = hami_vkGetDeviceProcAddr;
    pVersionStruct->pfnGetPhysicalDeviceProcAddr = NULL;
    return VK_SUCCESS;
}

/* Placeholders — real bodies live in hooks_memory.c / hooks_submit.c.
   Define weak stubs here so layer.c alone compiles during TDD of Task 1.1. */
#ifndef HAMI_VK_HOOKS_PRESENT
void hami_vk_hook_instance(hami_instance_dispatch_t *d) { (void)d; }
void hami_vk_hook_device(hami_device_dispatch_t *d)     { (void)d; }
VKAPI_ATTR void VKAPI_CALL hami_vkGetPhysicalDeviceMemoryProperties(VkPhysicalDevice p, VkPhysicalDeviceMemoryProperties *o) {
    hami_instance_dispatch_t *d = g_inst_head; (void)d; (void)p; (void)o;
}
VKAPI_ATTR void VKAPI_CALL hami_vkGetPhysicalDeviceMemoryProperties2(VkPhysicalDevice p, VkPhysicalDeviceMemoryProperties2 *o) { (void)p; (void)o; }
VKAPI_ATTR VkResult VKAPI_CALL hami_vkAllocateMemory(VkDevice d, const VkMemoryAllocateInfo *i, const VkAllocationCallbacks *a, VkDeviceMemory *m) { (void)d;(void)i;(void)a;(void)m; return VK_ERROR_OUT_OF_DEVICE_MEMORY; }
VKAPI_ATTR void     VKAPI_CALL hami_vkFreeMemory(VkDevice d, VkDeviceMemory m, const VkAllocationCallbacks *a) { (void)d;(void)m;(void)a; }
VKAPI_ATTR VkResult VKAPI_CALL hami_vkQueueSubmit(VkQueue q, uint32_t n, const VkSubmitInfo *s, VkFence f) { (void)q;(void)n;(void)s;(void)f; return VK_SUCCESS; }
VKAPI_ATTR VkResult VKAPI_CALL hami_vkQueueSubmit2(VkQueue q, uint32_t n, const VkSubmitInfo2 *s, VkFence f) { (void)q;(void)n;(void)s;(void)f; return VK_SUCCESS; }
#endif
```

- [ ] **Step 7: 레이어만으로 임시 빌드 및 테스트 통과 확인**

Run (from `libvgpu/`):
```bash
cc -shared -fPIC -o /tmp/libvgpu_stub.so \
   src/vulkan/layer.c src/vulkan/dispatch.c \
   -I/usr/include -lpthread
cc -o /tmp/t test/vulkan/test_layer.c -ldl
cd /tmp && cp /tmp/libvgpu_stub.so ./libvgpu.so && ./t
```
Expected: `ok: layer entry point negotiates`.

- [ ] **Step 8: 커밋 (libvgpu 레포)**

Run (from `libvgpu/`):
```bash
git checkout -b vulkan-layer
git add src/vulkan/layer.h src/vulkan/layer.c src/vulkan/dispatch.h src/vulkan/dispatch.c test/vulkan/test_layer.c
git commit -m "feat(vulkan): add layer entry point and dispatch skeleton"
```

---

### Task 1.2: `vkGetPhysicalDeviceMemoryProperties[2]` 힙 클램프

**Files:**
- Create: `libvgpu/src/vulkan/hooks_memory.c`
- Modify: `libvgpu/src/vulkan/layer.c` (스텁 제거)

- [ ] **Step 1: 실패 테스트 작성**

Create `libvgpu/test/vulkan/test_memprops.c`:
```c
#include <assert.h>
#include <stdio.h>
#include <string.h>
#include <vulkan/vulkan.h>
#include "../../src/vulkan/dispatch.h"

/* pod budget stub used by hooks_memory.c; real implementation in memory module */
size_t hami_pod_memory_budget(int dev_idx) { (void)dev_idx; return 1ull << 30; /* 1 GiB */ }

/* fake next-layer property query reporting 8 GiB device-local heap */
static void VKAPI_CALL fake_next(VkPhysicalDevice p, VkPhysicalDeviceMemoryProperties *out) {
    (void)p;
    memset(out, 0, sizeof(*out));
    out->memoryHeapCount = 1;
    out->memoryHeaps[0].size = 8ull << 30;
    out->memoryHeaps[0].flags = VK_MEMORY_HEAP_DEVICE_LOCAL_BIT;
}

extern VKAPI_ATTR void VKAPI_CALL
hami_vkGetPhysicalDeviceMemoryProperties(VkPhysicalDevice p, VkPhysicalDeviceMemoryProperties *out);

int main(void) {
    VkInstance inst = (VkInstance)0x1;
    hami_instance_dispatch_t *d = hami_instance_register(inst, NULL);
    d->GetPhysicalDeviceMemoryProperties = fake_next;

    VkPhysicalDeviceMemoryProperties props;
    hami_vkGetPhysicalDeviceMemoryProperties((VkPhysicalDevice)0x2, &props);
    assert(props.memoryHeapCount == 1);
    assert(props.memoryHeaps[0].size == (1ull << 30));
    printf("ok: heap clamped to 1 GiB\n");
    return 0;
}
```

- [ ] **Step 2: 테스트 빌드 (기대: stub이 clamp를 안 하므로 실패)**

Run (from `libvgpu/`):
```bash
cc -o /tmp/tm -DHAMI_VK_HOOKS_PRESENT \
   src/vulkan/layer.c src/vulkan/dispatch.c \
   test/vulkan/test_memprops.c -lpthread
/tmp/tm
```
Expected: 링크 에러 (hooks_memory.c 아직 없음) — 또는 `hami_vk_hook_*` 미정의.

- [ ] **Step 3: `hooks_memory.c` 작성 (클램프 + instance hook 설치)**

Create `libvgpu/src/vulkan/hooks_memory.c`:
```c
#include "dispatch.h"
#include <string.h>

/* Provided by the budget module (Phase 2 integrates with existing counter).
   For now declared here, implemented by the unit test or the memory module. */
size_t hami_pod_memory_budget(int dev_idx);

static int physdev_index(VkPhysicalDevice p) {
    /* Simplification: layer sees only devices already filtered by NVIDIA_VISIBLE_DEVICES.
       Use pointer-hash low bits as a stable index within the process. Replace with
       NVML UUID lookup during Task 2.1 integration. */
    return (int)(((uintptr_t)p >> 4) & 0xff);
}

static void clamp_heaps(VkPhysicalDevice p, uint32_t *count, VkMemoryHeap *heaps) {
    size_t budget = hami_pod_memory_budget(physdev_index(p));
    for (uint32_t i = 0; i < *count; ++i) {
        if ((heaps[i].flags & VK_MEMORY_HEAP_DEVICE_LOCAL_BIT) == 0) continue;
        if (heaps[i].size > budget) heaps[i].size = budget;
    }
}

VKAPI_ATTR void VKAPI_CALL
hami_vkGetPhysicalDeviceMemoryProperties(VkPhysicalDevice p,
                                         VkPhysicalDeviceMemoryProperties *out) {
    hami_instance_dispatch_t *d = hami_instance_lookup(VK_NULL_HANDLE); /* caller already registered */
    /* Find the dispatch holding this physical device's instance. For simplicity walk any. */
    extern hami_instance_dispatch_t *g_inst_head;
    (void)d;
    for (hami_instance_dispatch_t *it = g_inst_head; it; it = it->next) {
        if (it->GetPhysicalDeviceMemoryProperties) {
            it->GetPhysicalDeviceMemoryProperties(p, out);
            clamp_heaps(p, &out->memoryHeapCount, out->memoryHeaps);
            return;
        }
    }
}

VKAPI_ATTR void VKAPI_CALL
hami_vkGetPhysicalDeviceMemoryProperties2(VkPhysicalDevice p,
                                          VkPhysicalDeviceMemoryProperties2 *out) {
    extern hami_instance_dispatch_t *g_inst_head;
    for (hami_instance_dispatch_t *it = g_inst_head; it; it = it->next) {
        if (it->GetPhysicalDeviceMemoryProperties2) {
            it->GetPhysicalDeviceMemoryProperties2(p, out);
            clamp_heaps(p, &out->memoryProperties.memoryHeapCount, out->memoryProperties.memoryHeaps);
            return;
        }
    }
}

void hami_vk_hook_instance(hami_instance_dispatch_t *d) {
    /* no per-instance state to install yet */
    (void)d;
}
```

또한 `dispatch.c`의 `g_inst_head`를 non-static로 변경해 다른 TU가 접근 가능하게 한다:

Modify `libvgpu/src/vulkan/dispatch.c:6`:
```c
/* expose to sibling TUs for walk */
hami_instance_dispatch_t *g_inst_head = NULL;
hami_device_dispatch_t   *g_dev_head  = NULL;
```
(기존 `static` 제거)

- [ ] **Step 4: layer.c의 clamp/allocate stub 제거**

Modify `libvgpu/src/vulkan/layer.c` — 파일 끝 `#ifndef HAMI_VK_HOOKS_PRESENT` 블록 중 `hami_vkGetPhysicalDeviceMemoryProperties[2]` stub만 삭제 (할당/제출 stub은 Task 1.3/1.5에서 제거).

- [ ] **Step 5: 테스트 빌드 및 실행 (이번엔 통과해야 함)**

Run:
```bash
cc -o /tmp/tm -DHAMI_VK_HOOKS_PRESENT \
   src/vulkan/layer.c src/vulkan/dispatch.c src/vulkan/hooks_memory.c \
   test/vulkan/test_memprops.c -lpthread
/tmp/tm
```
Expected: `ok: heap clamped to 1 GiB`.

- [ ] **Step 6: 커밋**

```bash
git add src/vulkan/hooks_memory.c src/vulkan/layer.c src/vulkan/dispatch.c test/vulkan/test_memprops.c
git commit -m "feat(vulkan): clamp device-local heap size to pod budget"
```

---

### Task 1.3: `vkAllocateMemory` / `vkFreeMemory` 버짓 강제

**Files:**
- Create: `libvgpu/src/vulkan/hooks_alloc.c`
- Modify: `libvgpu/src/vulkan/layer.c` (해당 stub 제거)

- [ ] **Step 1: 실패 테스트 작성**

Create `libvgpu/test/vulkan/test_alloc.c`:
```c
#include <assert.h>
#include <stdio.h>
#include <vulkan/vulkan.h>
#include "../../src/vulkan/dispatch.h"

size_t hami_pod_memory_budget(int dev_idx) { (void)dev_idx; return 1ull << 30; /* 1 GiB */ }

/* Simple in-memory accounting stub shared with the layer. */
static size_t g_used = 0;

int  hami_reserve_device_memory(int dev, size_t size) {
    (void)dev;
    if (g_used + size > hami_pod_memory_budget(dev)) return 0;
    g_used += size;
    return 1;
}
void hami_release_device_memory(int dev, size_t size) { (void)dev; g_used -= size; }

static VkResult VKAPI_CALL fake_alloc(VkDevice d, const VkMemoryAllocateInfo *i,
                                      const VkAllocationCallbacks *a, VkDeviceMemory *m) {
    (void)d;(void)a; *m = (VkDeviceMemory)(uintptr_t)(i->allocationSize);
    return VK_SUCCESS;
}
static void VKAPI_CALL fake_free(VkDevice d, VkDeviceMemory m, const VkAllocationCallbacks *a) { (void)d;(void)m;(void)a; }

extern VKAPI_ATTR VkResult VKAPI_CALL
hami_vkAllocateMemory(VkDevice, const VkMemoryAllocateInfo*, const VkAllocationCallbacks*, VkDeviceMemory*);
extern VKAPI_ATTR void VKAPI_CALL
hami_vkFreeMemory(VkDevice, VkDeviceMemory, const VkAllocationCallbacks*);

int main(void) {
    VkDevice dev = (VkDevice)0x1;
    hami_device_dispatch_t *d = hami_device_register(dev, (VkPhysicalDevice)0x2, NULL);
    d->AllocateMemory = fake_alloc;
    d->FreeMemory     = fake_free;

    VkMemoryAllocateInfo info = { .sType=VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO, .allocationSize=(512ull<<20) };
    VkDeviceMemory m1, m2, m3;

    assert(hami_vkAllocateMemory(dev, &info, NULL, &m1) == VK_SUCCESS);
    assert(hami_vkAllocateMemory(dev, &info, NULL, &m2) == VK_SUCCESS);
    assert(hami_vkAllocateMemory(dev, &info, NULL, &m3) == VK_ERROR_OUT_OF_DEVICE_MEMORY);

    hami_vkFreeMemory(dev, m1, NULL);
    assert(hami_vkAllocateMemory(dev, &info, NULL, &m3) == VK_SUCCESS);
    printf("ok: allocate/free budget enforced\n");
    return 0;
}
```

- [ ] **Step 2: 테스트 실패 확인 (함수 미구현 → 링크 에러 또는 stub이 모두 실패 반환)**

Run:
```bash
cc -o /tmp/ta -DHAMI_VK_HOOKS_PRESENT \
   src/vulkan/layer.c src/vulkan/dispatch.c src/vulkan/hooks_memory.c \
   test/vulkan/test_alloc.c -lpthread
/tmp/ta
```
Expected: `VK_ERROR_OUT_OF_DEVICE_MEMORY` assertion 위반(stub이 항상 OOM 반환).

- [ ] **Step 3: `hooks_alloc.c` 작성**

Create `libvgpu/src/vulkan/hooks_alloc.c`:
```c
#include "dispatch.h"
#include <pthread.h>
#include <stdlib.h>
#include <string.h>

int  hami_reserve_device_memory(int dev_idx, size_t size);
void hami_release_device_memory(int dev_idx, size_t size);

typedef struct mem_entry {
    VkDeviceMemory handle;
    size_t size;
    int dev_idx;
    struct mem_entry *next;
} mem_entry_t;

static mem_entry_t *g_mem_head = NULL;
static pthread_mutex_t g_mem_lock = PTHREAD_MUTEX_INITIALIZER;

static int device_to_index(VkDevice d) {
    return (int)(((uintptr_t)d >> 4) & 0xff);
}

VKAPI_ATTR VkResult VKAPI_CALL
hami_vkAllocateMemory(VkDevice device, const VkMemoryAllocateInfo *pInfo,
                      const VkAllocationCallbacks *pAlloc, VkDeviceMemory *pMem) {
    hami_device_dispatch_t *d = hami_device_lookup(device);
    if (!d || !d->AllocateMemory) return VK_ERROR_INITIALIZATION_FAILED;

    int idx = device_to_index(device);
    if (!hami_reserve_device_memory(idx, pInfo->allocationSize))
        return VK_ERROR_OUT_OF_DEVICE_MEMORY;

    VkResult r = d->AllocateMemory(device, pInfo, pAlloc, pMem);
    if (r != VK_SUCCESS) {
        hami_release_device_memory(idx, pInfo->allocationSize);
        return r;
    }

    mem_entry_t *e = calloc(1, sizeof(*e));
    e->handle = *pMem;
    e->size   = pInfo->allocationSize;
    e->dev_idx = idx;

    pthread_mutex_lock(&g_mem_lock);
    e->next = g_mem_head;
    g_mem_head = e;
    pthread_mutex_unlock(&g_mem_lock);
    return VK_SUCCESS;
}

VKAPI_ATTR void VKAPI_CALL
hami_vkFreeMemory(VkDevice device, VkDeviceMemory mem, const VkAllocationCallbacks *pAlloc) {
    hami_device_dispatch_t *d = hami_device_lookup(device);
    if (d && d->FreeMemory) d->FreeMemory(device, mem, pAlloc);

    pthread_mutex_lock(&g_mem_lock);
    mem_entry_t **pp = &g_mem_head;
    while (*pp && (*pp)->handle != mem) pp = &(*pp)->next;
    if (*pp) {
        mem_entry_t *victim = *pp;
        *pp = victim->next;
        pthread_mutex_unlock(&g_mem_lock);
        hami_release_device_memory(victim->dev_idx, victim->size);
        free(victim);
        return;
    }
    pthread_mutex_unlock(&g_mem_lock);
}

void hami_vk_hook_device(hami_device_dispatch_t *d) { (void)d; }
```

- [ ] **Step 4: layer.c의 allocate/free stub 제거**

Modify `libvgpu/src/vulkan/layer.c` — 파일 끝 `#ifndef` 블록에서 `hami_vkAllocateMemory`, `hami_vkFreeMemory`, `hami_vk_hook_device` stub 삭제 (QueueSubmit stub은 Task 1.5까지 유지).

- [ ] **Step 5: 테스트 통과 확인**

Run:
```bash
cc -o /tmp/ta -DHAMI_VK_HOOKS_PRESENT \
   src/vulkan/layer.c src/vulkan/dispatch.c src/vulkan/hooks_memory.c src/vulkan/hooks_alloc.c \
   test/vulkan/test_alloc.c -lpthread
/tmp/ta
```
Expected: `ok: allocate/free budget enforced`.

- [ ] **Step 6: 커밋**

```bash
git add src/vulkan/hooks_alloc.c src/vulkan/layer.c test/vulkan/test_alloc.c
git commit -m "feat(vulkan): enforce pod memory budget on vkAllocateMemory/vkFreeMemory"
```

---

### Task 1.4: 공통 SM throttle 유틸 추출

**Files:**
- Create: `libvgpu/src/common/throttle.h`
- Create: `libvgpu/src/common/throttle.c`
- Modify: 기존 CUDA launch 래퍼 (Task 0.2 노트에서 확보한 파일/함수)

이 Task는 **기존 CUDA 경로에서 쓰이는 throttle 폴링 루프를 함수로 추출하여 공통화**하는 리팩토링이다. Vulkan과 CUDA 양쪽에서 호출한다.

- [ ] **Step 1: 기존 throttle 루프 복사 원본 확인**

`docs/superpowers/plans/notes/hami-core-layout.md`의 Step 3 결과에서 throttle 함수 위치를 열람.

Run (예시, 실제 경로는 노트 기반):
```bash
sed -n '60,110p' libvgpu/src/cuda/launch.c
```
Expected: 폴링 + `usleep` 루프가 보임.

- [ ] **Step 2: 실패 테스트 작성 (호출 가능성만 검증)**

Create `libvgpu/test/common/test_throttle.c`:
```c
#include <assert.h>
#include <stdio.h>
#include <time.h>
#include "../../src/common/throttle.h"

int main(void) {
    /* dev_idx = 0, util_limit = 100 (사실상 폴링 즉시 종료) */
    struct timespec a, b;
    clock_gettime(CLOCK_MONOTONIC, &a);
    hami_throttle_wait(0, 100);
    clock_gettime(CLOCK_MONOTONIC, &b);
    double dur = (b.tv_sec - a.tv_sec) + (b.tv_nsec - a.tv_nsec)/1e9;
    assert(dur < 0.5); /* util_limit=100 → 즉시 통과 */
    printf("ok: throttle returns quickly at util_limit=100\n");
    return 0;
}
```

- [ ] **Step 3: 테스트 실패 확인**

Run:
```bash
cc -o /tmp/tt test/common/test_throttle.c
```
Expected: `throttle.h` 없음 → 컴파일 실패.

- [ ] **Step 4: `throttle.h` / `throttle.c` 작성**

Create `libvgpu/src/common/throttle.h`:
```c
#ifndef HAMI_COMMON_THROTTLE_H
#define HAMI_COMMON_THROTTLE_H

/* Block until NVML reports GPU utilization <= util_limit (percent) or
 * HAMI_THROTTLE_MAX_ITER polls are exhausted. Fail-open on NVML errors. */
void hami_throttle_wait(int dev_idx, int util_limit);

#endif
```

Create `libvgpu/src/common/throttle.c`:
```c
#include "throttle.h"
#include <unistd.h>

/* Forward-declare NVML bits the existing CUDA path already links against.
 * Keep this file header-independent to avoid ordering constraints with the
 * rest of the codebase. Real NVML header names/symbols should be reused
 * from the include path established for the CUDA wrappers. */
typedef struct { unsigned int gpu; unsigned int memory; } nvmlUtilization_t;
typedef void *nvmlDevice_t;
extern int  nvmlDeviceGetHandleByIndex_v2(unsigned int idx, nvmlDevice_t *out);
extern int  nvmlDeviceGetUtilizationRates(nvmlDevice_t dev, nvmlUtilization_t *u);

#define HAMI_THROTTLE_POLL_US 500
#define HAMI_THROTTLE_MAX_ITER 2000

void hami_throttle_wait(int dev_idx, int util_limit) {
    if (util_limit >= 100) return;
    nvmlDevice_t h = NULL;
    if (nvmlDeviceGetHandleByIndex_v2((unsigned)dev_idx, &h) != 0) return;
    for (int i = 0; i < HAMI_THROTTLE_MAX_ITER; ++i) {
        nvmlUtilization_t u = {0};
        if (nvmlDeviceGetUtilizationRates(h, &u) != 0) return;
        if ((int)u.gpu < util_limit) return;
        usleep(HAMI_THROTTLE_POLL_US);
    }
}
```

- [ ] **Step 5: 기존 CUDA throttle 루프를 `hami_throttle_wait` 호출로 치환**

Modify the CUDA launch file identified in Step 1 (예: `libvgpu/src/cuda/launch.c`). 기존 블록:
```c
/* old inline polling loop replaced */
for (...) { ...nvmlDeviceGetUtilizationRates...; usleep(...); }
```
을 다음으로 교체:
```c
#include "../common/throttle.h"
...
hami_throttle_wait(dev_idx, sm_limit_percent);
```
정확한 변수명(`dev_idx`, `sm_limit_percent`)은 기존 파일의 로컬 네이밍에 맞춘다.

- [ ] **Step 6: 테스트 및 기존 CUDA 유닛 테스트 통과 확인**

Run:
```bash
cc -o /tmp/tt src/common/throttle.c test/common/test_throttle.c
/tmp/tt
make -C libvgpu test 2>/dev/null || cc -o /tmp/existing libvgpu/test/cuda/*.c libvgpu/src/cuda/*.c libvgpu/src/common/throttle.c -ldl -lpthread
```
Expected: 새 테스트 PASS. 기존 테스트가 있으면 여전히 PASS.

- [ ] **Step 7: 커밋**

```bash
git add src/common/throttle.h src/common/throttle.c src/cuda/launch.c test/common/test_throttle.c
git commit -m "refactor(common): extract SM throttle polling into shared util"
```

---

### Task 1.5: `vkQueueSubmit[2]` throttle 훅

**Files:**
- Create: `libvgpu/src/vulkan/hooks_submit.c`
- Modify: `libvgpu/src/vulkan/layer.c` (나머지 stub 제거)

- [ ] **Step 1: 실패 테스트 작성**

Create `libvgpu/test/vulkan/test_submit.c`:
```c
#include <assert.h>
#include <stdio.h>
#include <vulkan/vulkan.h>
#include "../../src/vulkan/dispatch.h"

static int g_submit_called = 0;
static VkResult VKAPI_CALL fake_submit(VkQueue q, uint32_t n, const VkSubmitInfo *s, VkFence f) {
    (void)q;(void)n;(void)s;(void)f; g_submit_called++; return VK_SUCCESS;
}

/* throttle stub — check it's called */
static int g_throttle_called = 0;
void hami_throttle_wait(int dev, int util) { (void)dev;(void)util; g_throttle_called++; }

/* device→queue not needed; hook keys on device */
extern VKAPI_ATTR VkResult VKAPI_CALL hami_vkQueueSubmit(VkQueue, uint32_t, const VkSubmitInfo*, VkFence);

int main(void) {
    /* Map queue back to device via a small test helper. */
    extern void hami_vk_test_register_queue(VkQueue q, VkDevice d);
    VkDevice dev = (VkDevice)0x11;
    VkQueue  q   = (VkQueue)0x22;
    hami_device_dispatch_t *d = hami_device_register(dev, (VkPhysicalDevice)0, NULL);
    d->QueueSubmit = fake_submit;
    hami_vk_test_register_queue(q, dev);

    VkResult r = hami_vkQueueSubmit(q, 0, NULL, VK_NULL_HANDLE);
    assert(r == VK_SUCCESS);
    assert(g_throttle_called == 1);
    assert(g_submit_called   == 1);
    printf("ok: submit hook throttles then forwards\n");
    return 0;
}
```

- [ ] **Step 2: 테스트 빌드 실패 확인**

Run:
```bash
cc -o /tmp/ts -DHAMI_VK_HOOKS_PRESENT \
   src/vulkan/layer.c src/vulkan/dispatch.c src/vulkan/hooks_memory.c src/vulkan/hooks_alloc.c \
   test/vulkan/test_submit.c -lpthread
```
Expected: `hami_vk_test_register_queue` 미정의 + submit stub이 throttle 호출 안 함.

- [ ] **Step 3: `hooks_submit.c` 작성**

Create `libvgpu/src/vulkan/hooks_submit.c`:
```c
#include "dispatch.h"
#include "../common/throttle.h"
#include <pthread.h>
#include <stdlib.h>

/* Queue → Device registry.
 * Vulkan apps get queues via vkGetDeviceQueue. We don't hook that (yet);
 * instead we resolve the owning device by walking dispatch table entries
 * whose QueueSubmit returns VK_SUCCESS on a dry-run is unreliable. For v1
 * we use a simple map populated by vkGetDeviceQueue hook. */

typedef struct q_entry { VkQueue q; VkDevice d; struct q_entry *next; } q_entry_t;
static q_entry_t *g_q_head = NULL;
static pthread_mutex_t g_q_lock = PTHREAD_MUTEX_INITIALIZER;

void hami_vk_test_register_queue(VkQueue q, VkDevice d);
void hami_vk_test_register_queue(VkQueue q, VkDevice d) {
    q_entry_t *e = calloc(1, sizeof(*e));
    e->q = q; e->d = d;
    pthread_mutex_lock(&g_q_lock);
    e->next = g_q_head; g_q_head = e;
    pthread_mutex_unlock(&g_q_lock);
}

static VkDevice device_for_queue(VkQueue q) {
    pthread_mutex_lock(&g_q_lock);
    q_entry_t *p = g_q_head;
    while (p && p->q != q) p = p->next;
    VkDevice d = p ? p->d : VK_NULL_HANDLE;
    pthread_mutex_unlock(&g_q_lock);
    return d;
}

/* Supplied by the Go webhook via NVIDIA_CORE_LIMIT_SWITCH env (existing path).
 * For now read HAMI_VULKAN_CORES env (0-100). Missing → 100 (no throttle). */
static int queue_util_limit(void) {
    const char *v = getenv("HAMI_VULKAN_CORES");
    if (!v) return 100;
    int n = atoi(v);
    return (n <= 0 || n > 100) ? 100 : n;
}

static int device_to_index(VkDevice d) { return (int)(((uintptr_t)d >> 4) & 0xff); }

VKAPI_ATTR VkResult VKAPI_CALL
hami_vkQueueSubmit(VkQueue queue, uint32_t n, const VkSubmitInfo *p, VkFence f) {
    VkDevice d = device_for_queue(queue);
    hami_device_dispatch_t *dd = hami_device_lookup(d);
    if (!dd || !dd->QueueSubmit) return VK_ERROR_INITIALIZATION_FAILED;
    hami_throttle_wait(device_to_index(d), queue_util_limit());
    return dd->QueueSubmit(queue, n, p, f);
}

VKAPI_ATTR VkResult VKAPI_CALL
hami_vkQueueSubmit2(VkQueue queue, uint32_t n, const VkSubmitInfo2 *p, VkFence f) {
    VkDevice d = device_for_queue(queue);
    hami_device_dispatch_t *dd = hami_device_lookup(d);
    if (!dd || !dd->QueueSubmit2) return VK_ERROR_INITIALIZATION_FAILED;
    hami_throttle_wait(device_to_index(d), queue_util_limit());
    return dd->QueueSubmit2(queue, n, p, f);
}
```

- [ ] **Step 4: layer.c의 남은 stub (QueueSubmit/QueueSubmit2) 제거**

`#ifndef HAMI_VK_HOOKS_PRESENT` 블록 전체 삭제.

- [ ] **Step 5: 테스트 통과 확인**

Run:
```bash
cc -o /tmp/ts -DHAMI_VK_HOOKS_PRESENT \
   src/vulkan/layer.c src/vulkan/dispatch.c \
   src/vulkan/hooks_memory.c src/vulkan/hooks_alloc.c src/vulkan/hooks_submit.c \
   test/vulkan/test_submit.c -lpthread
/tmp/ts
```
Expected: `ok: submit hook throttles then forwards`.

- [ ] **Step 6: 커밋**

```bash
git add src/vulkan/hooks_submit.c src/vulkan/layer.c test/vulkan/test_submit.c
git commit -m "feat(vulkan): throttle vkQueueSubmit[2] using shared SM util loop"
```

---

### Task 1.6: 버짓 카운터를 HAMi-core 본체와 통합

**Files:**
- Modify: `libvgpu/src/vulkan/hooks_alloc.c`, `hooks_memory.c` (stub `hami_reserve_device_memory` / `hami_pod_memory_budget` 제거)
- Modify: 기존 메모리 모듈 (Task 0.2 노트 경로)

- [ ] **Step 1: 기존 CUDA 예약 함수 시그니처 확인**

노트 파일을 읽고, CUDA 경로가 사용하는 예약/반환/버짓 조회 함수의 실제 이름을 적는다. 예: `int oom_check(int dev, size_t)`, `void add_allocated(int, size_t)`, `size_t get_limit(int)`.

- [ ] **Step 2: 어댑터 작성 (공통 API로 노출)**

Modify `libvgpu/src/common/budget.h` (create if not exists):
```c
#ifndef HAMI_COMMON_BUDGET_H
#define HAMI_COMMON_BUDGET_H
#include <stddef.h>

/* Reserve size bytes on dev. Returns 1 on success, 0 if over budget. */
int    hami_reserve_device_memory(int dev, size_t size);
void   hami_release_device_memory(int dev, size_t size);
size_t hami_pod_memory_budget(int dev);

#endif
```

Create `libvgpu/src/common/budget.c`:
```c
#include "budget.h"

/* Adapt to the existing CUDA memory module's names (replace with actual
   symbols documented in notes/hami-core-layout.md). */
extern int    oom_check(int dev, size_t size);
extern void   add_allocated(int dev, size_t size);
extern void   sub_allocated(int dev, size_t size);
extern size_t get_memory_limit(int dev);

int hami_reserve_device_memory(int dev, size_t size) {
    if (!oom_check(dev, size)) return 0;
    add_allocated(dev, size);
    return 1;
}

void hami_release_device_memory(int dev, size_t size) {
    sub_allocated(dev, size);
}

size_t hami_pod_memory_budget(int dev) {
    return get_memory_limit(dev);
}
```

**주의**: `oom_check` / `add_allocated` / `get_memory_limit` 이름은 Task 0.2에서 확보한 실제 이름으로 교체. 다른 이름이면 `budget.c`만 수정하여 어댑터 역할을 유지.

- [ ] **Step 3: Vulkan TU에서 로컬 stub 선언 제거, 공통 헤더 include**

Modify `libvgpu/src/vulkan/hooks_alloc.c:4-5`:
```c
#include "../common/budget.h"
```
그리고 파일 상단의 `extern int hami_reserve_device_memory(...); extern void hami_release_device_memory(...);` 선언 2줄 제거.

Modify `libvgpu/src/vulkan/hooks_memory.c` 상단:
```c
#include "../common/budget.h"
```
그리고 `size_t hami_pod_memory_budget(int);` 전방 선언 제거.

- [ ] **Step 4: 단위 테스트는 어댑터와 충돌하지 않도록 조정**

기존 `test/vulkan/test_alloc.c`, `test_memprops.c`에는 테스트용 `hami_pod_memory_budget` / `hami_reserve_device_memory` 가 정의되어 있다. 테스트 실행 시 `budget.c`를 **제외**하고 빌드한다(중복 심볼 방지). 테스트 빌드 커맨드 확인:
```bash
cc -o /tmp/ta -DHAMI_VK_HOOKS_PRESENT \
   src/vulkan/layer.c src/vulkan/dispatch.c \
   src/vulkan/hooks_memory.c src/vulkan/hooks_alloc.c \
   test/vulkan/test_alloc.c -lpthread
```
위 명령에 `budget.c`가 없어야 함. 실 라이브러리(.so) 빌드는 `budget.c`를 포함.

- [ ] **Step 5: 기존 + 신규 테스트 모두 PASS 확인**

```bash
/tmp/tm && /tmp/ta && /tmp/ts
```
Expected: 3개 모두 `ok:`.

- [ ] **Step 6: 커밋**

```bash
git add src/common/budget.h src/common/budget.c src/vulkan/hooks_alloc.c src/vulkan/hooks_memory.c
git commit -m "feat(vulkan): integrate shared VRAM budget with existing CUDA counter"
```

---

### Task 1.7: 레이어 매니페스트 JSON

**Files:**
- Create: `libvgpu/etc/vulkan/implicit_layer.d/hami.json`

- [ ] **Step 1: 매니페스트 파일 작성**

Create `libvgpu/etc/vulkan/implicit_layer.d/hami.json`:
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

- [ ] **Step 2: JSON 문법 검증**

Run:
```bash
python3 -m json.tool libvgpu/etc/vulkan/implicit_layer.d/hami.json > /dev/null && echo ok
```
Expected: `ok`.

- [ ] **Step 3: 커밋**

```bash
git add etc/vulkan/implicit_layer.d/hami.json
git commit -m "feat(vulkan): ship implicit layer manifest gated by HAMI_VULKAN_ENABLE"
```

---

### Task 1.8: Makefile / Dockerfile 통합

**Files:**
- Modify: `libvgpu/Makefile`
- Modify: `libvgpu/Dockerfile` (존재하면) 또는 이미지 빌드 스크립트

- [ ] **Step 1: 현재 Makefile 소스 리스트 확인**

Run:
```bash
grep -n "SRCS\|OBJECTS\|\.c\b" libvgpu/Makefile | head -30
```

- [ ] **Step 2: Vulkan 소스 추가**

Modify `libvgpu/Makefile` — 기존 `SRCS := ...` (또는 등가) 라인에 다음을 추가:
```makefile
VULKAN_SRCS := \
    src/vulkan/layer.c \
    src/vulkan/dispatch.c \
    src/vulkan/hooks_memory.c \
    src/vulkan/hooks_alloc.c \
    src/vulkan/hooks_submit.c \
    src/common/throttle.c \
    src/common/budget.c

SRCS += $(VULKAN_SRCS)
CFLAGS += -I$(VULKAN_SDK_INCLUDE) -I./src
```

`VULKAN_SDK_INCLUDE`는 기본값을 지정한다:
```makefile
VULKAN_SDK_INCLUDE ?= /usr/include
```

- [ ] **Step 3: 테스트 타겟 추가**

Modify `libvgpu/Makefile` 하단에 test 타겟이 없다면 추가:
```makefile
.PHONY: test-vulkan
test-vulkan:
	cc -o /tmp/t_mem  -DHAMI_VK_HOOKS_PRESENT $(VULKAN_SRCS) test/vulkan/test_memprops.c -lpthread -I./src -I$(VULKAN_SDK_INCLUDE) && /tmp/t_mem
	cc -o /tmp/t_alloc -DHAMI_VK_HOOKS_PRESENT $(filter-out src/common/budget.c,$(VULKAN_SRCS)) test/vulkan/test_alloc.c -lpthread -I./src -I$(VULKAN_SDK_INCLUDE) && /tmp/t_alloc
	cc -o /tmp/t_sub  -DHAMI_VK_HOOKS_PRESENT $(filter-out src/common/throttle.c src/common/budget.c,$(VULKAN_SRCS)) test/vulkan/test_submit.c -lpthread -I./src -I$(VULKAN_SDK_INCLUDE) && /tmp/t_sub
	cc -o /tmp/t_thr  src/common/throttle.c test/common/test_throttle.c && /tmp/t_thr

test: test-vulkan
```

- [ ] **Step 4: Dockerfile 수정 (매니페스트 복사 + 헤더 설치)**

Modify `libvgpu/Dockerfile` (없으면 이 단계는 "빌드 이미지 Dockerfile에 동일 내용 추가"로 대체):
```dockerfile
RUN apt-get update && apt-get install -y --no-install-recommends \
        libvulkan-dev \
    && rm -rf /var/lib/apt/lists/*

COPY etc/vulkan/implicit_layer.d/hami.json \
     /etc/vulkan/implicit_layer.d/hami.json
```

- [ ] **Step 5: 빌드 & 테스트**

Run:
```bash
cd libvgpu && make clean && make && make test-vulkan
```
Expected: `libvgpu.so` 생성, 4개 테스트 모두 PASS.

- [ ] **Step 6: 커밋**

```bash
git add Makefile Dockerfile
git commit -m "build(vulkan): integrate Vulkan layer sources and manifest into image"
```

---

### Task 1.9: HAMi-core PR 푸시 및 릴리스 태그

**Files:** (메타 작업)

- [ ] **Step 1: 브랜치 푸시**

Run (from `libvgpu/`):
```bash
git push -u origin vulkan-layer
```

- [ ] **Step 2: PR 생성**

```bash
gh pr create --title "feat(vulkan): vGPU partitioning for Vulkan workloads" \
             --body "$(cat <<'EOF'
## Summary
- Vulkan implicit layer VK_LAYER_HAMI_vgpu (activated by HAMI_VULKAN_ENABLE=1)
- vkAllocateMemory/vkFreeMemory share the existing CUDA VRAM counter
- vkGetPhysicalDeviceMemoryProperties[2] clamps device-local heap to pod budget
- vkQueueSubmit[2] routes through the shared SM utilization throttle
- Manifest ships to /etc/vulkan/implicit_layer.d/hami.json

Design: Project-HAMi/HAMi docs/superpowers/specs/2026-04-21-vulkan-vgpu-partitioning-design.md

## Test plan
- [x] unit: test_layer, test_memprops, test_alloc, test_submit, test_throttle
- [ ] integration: vulkaninfo in HAMi-scheduled pod
- [ ] regression: existing CUDA hooks unaffected
EOF
)"
```

- [ ] **Step 3: PR URL 기록**

PR URL을 `docs/superpowers/plans/notes/hami-core-pr.md`에 적는다 (HAMi 쪽 Task 2.6에서 참조).

- [ ] **Step 4: 릴리스 태그 준비 (머지 후 별도)**

PR 머지 후, HAMi-core 메인테이너가 릴리스 태그(예: `v1.7.0`)를 잘라 이미지(`projecthami/hami-vgpu:v1.7.0`)를 푸시. 이 Task 안에서는 릴리스 태그 이름만 `docs/superpowers/plans/notes/hami-core-pr.md`에 기록.

---

## Phase 2 — HAMi (Go) 웹훅

### Task 2.1: Vulkan annotation 상수 및 실패 테스트

**Files:**
- Modify: `pkg/device/nvidia/device.go:39-57` (const 블록)
- Modify: `pkg/device/nvidia/device_test.go` (뒤에 신규 테스트 추가)

- [ ] **Step 1: 상수 추가**

Modify `pkg/device/nvidia/device.go:39`, 기존 const 블록 끝에 추가:
```go
const (
    HandshakeAnnos       = "hami.io/node-handshake"
    // ... 기존 상수 ...
    MpsMode      = "mps"

    // Vulkan vGPU partitioning (added 2026-04-21)
    VulkanEnableAnno       = "hami.io/vulkan"
    VulkanLayerName        = "VK_LAYER_HAMI_vgpu"
    NvidiaDriverCapsEnvVar = "NVIDIA_DRIVER_CAPABILITIES"
    HamiVulkanEnvVar       = "HAMI_VULKAN_ENABLE"
)
```

(Go의 const 선언은 한 블록에 합치지 말고, 기존 블록에 뒤에 붙이거나 별도 블록으로 추가. 프로젝트 컨벤션상 별도 블록이 더 깔끔.)

- [ ] **Step 2: 실패 단위 테스트 작성**

Append to `pkg/device/nvidia/device_test.go`:
```go
func TestMutateAdmission_VulkanAnno_AddsGraphicsCap(t *testing.T) {
    dev := &NvidiaGPUDevices{
        config: NvidiaConfig{
            ResourceCountName:            "nvidia.com/gpu",
            ResourceMemoryName:           "nvidia.com/gpumem",
            ResourceCoreName:             "nvidia.com/gpucores",
            ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
        },
    }
    ctr := &corev1.Container{
        Resources: corev1.ResourceRequirements{
            Limits: corev1.ResourceList{
                "nvidia.com/gpu": *resource.NewQuantity(1, resource.BinarySI),
            },
        },
    }
    pod := &corev1.Pod{
        ObjectMeta: metav1.ObjectMeta{
            Annotations: map[string]string{VulkanEnableAnno: "true"},
        },
    }
    _, err := dev.MutateAdmission(ctr, pod)
    assert.NilError(t, err)

    var caps, enable string
    for _, e := range ctr.Env {
        if e.Name == NvidiaDriverCapsEnvVar {
            caps = e.Value
        }
        if e.Name == HamiVulkanEnvVar {
            enable = e.Value
        }
    }
    assert.Assert(t, strings.Contains(caps, "graphics"), "expected graphics in caps, got %q", caps)
    assert.Equal(t, enable, "1")
}
```

`metav1` import 추가: `pkg/device/nvidia/device_test.go` 상단 import 블록에 `metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"` 이미 있는지 확인; 없으면 추가. `strings` 동일.

- [ ] **Step 3: 테스트 실패 확인**

Run:
```bash
go test ./pkg/device/nvidia/ -run TestMutateAdmission_VulkanAnno_AddsGraphicsCap -v
```
Expected: FAIL (아직 로직 미구현).

- [ ] **Step 4: 커밋**

```bash
git add pkg/device/nvidia/device.go pkg/device/nvidia/device_test.go
git commit -m "test(nvidia): failing test for Vulkan annotation env injection"
```

---

### Task 2.2: `MutateAdmission`에 Vulkan 로직 추가

**Files:**
- Modify: `pkg/device/nvidia/device.go:342-378` (MutateAdmission)

- [ ] **Step 1: 헬퍼 함수 추가**

Modify `pkg/device/nvidia/device.go` — `MutateAdmission` 함수 아래(또는 파일 끝)에 추가:
```go
// mergeGraphicsCap returns the union of existing NVIDIA_DRIVER_CAPABILITIES
// tokens with "graphics". If existing contains "all", it is returned unchanged.
// An empty existing value becomes "compute,utility,graphics" (baseline needed
// for Vulkan ICD plus existing HAMi CUDA path).
func mergeGraphicsCap(existing string) string {
    if existing == "" {
        return "compute,utility,graphics"
    }
    tokens := strings.Split(existing, ",")
    seen := make(map[string]struct{}, len(tokens))
    for _, t := range tokens {
        t = strings.TrimSpace(t)
        if t == "" {
            continue
        }
        if t == "all" {
            return existing
        }
        seen[t] = struct{}{}
    }
    if _, ok := seen["graphics"]; ok {
        return existing
    }
    tokens = append(tokens, "graphics")
    // normalize: trim spaces, drop empties
    cleaned := make([]string, 0, len(tokens))
    for _, t := range tokens {
        t = strings.TrimSpace(t)
        if t != "" {
            cleaned = append(cleaned, t)
        }
    }
    return strings.Join(cleaned, ",")
}

// applyVulkanAnnotation mutates the container env when the pod opts into
// Vulkan partitioning. No-op otherwise.
func applyVulkanAnnotation(ctr *corev1.Container, pod *corev1.Pod) {
    if pod == nil || pod.Annotations[VulkanEnableAnno] != "true" {
        return
    }

    capsIdx := -1
    for i, e := range ctr.Env {
        if e.Name == NvidiaDriverCapsEnvVar {
            capsIdx = i
            break
        }
    }
    merged := mergeGraphicsCap("")
    if capsIdx >= 0 {
        merged = mergeGraphicsCap(ctr.Env[capsIdx].Value)
    }
    if capsIdx >= 0 {
        ctr.Env[capsIdx].Value = merged
    } else {
        ctr.Env = append(ctr.Env, corev1.EnvVar{Name: NvidiaDriverCapsEnvVar, Value: merged})
    }

    hasEnable := false
    for _, e := range ctr.Env {
        if e.Name == HamiVulkanEnvVar {
            hasEnable = true
            break
        }
    }
    if !hasEnable {
        ctr.Env = append(ctr.Env, corev1.EnvVar{Name: HamiVulkanEnvVar, Value: "1"})
    }
}
```

- [ ] **Step 2: `MutateAdmission`에서 호출**

Modify `pkg/device/nvidia/device.go:365-370` (기존 `if hasResource` 블록 바로 뒤에 추가):
```go
    if hasResource {
        // Set runtime class name if it is not set by user and the runtime class name is configured
        if p.Spec.RuntimeClassName == nil && dev.config.RuntimeClassName != "" {
            p.Spec.RuntimeClassName = &dev.config.RuntimeClassName
        }
        applyVulkanAnnotation(ctr, p)
    }
```

- [ ] **Step 3: 테스트 통과 확인**

Run:
```bash
go test ./pkg/device/nvidia/ -run TestMutateAdmission_VulkanAnno_AddsGraphicsCap -v
```
Expected: PASS.

- [ ] **Step 4: 커밋**

```bash
git add pkg/device/nvidia/device.go
git commit -m "feat(nvidia): inject Vulkan env when pod carries hami.io/vulkan annotation"
```

---

### Task 2.3: Caps 병합 엣지 케이스 테스트

**Files:**
- Modify: `pkg/device/nvidia/device_test.go`

- [ ] **Step 1: 추가 테스트들 작성**

Append to `pkg/device/nvidia/device_test.go`:
```go
func TestMutateAdmission_VulkanAnno_MergesExistingCaps(t *testing.T) {
    dev := &NvidiaGPUDevices{
        config: NvidiaConfig{
            ResourceCountName:            "nvidia.com/gpu",
            ResourceMemoryName:           "nvidia.com/gpumem",
            ResourceCoreName:             "nvidia.com/gpucores",
            ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
        },
    }
    ctr := &corev1.Container{
        Env: []corev1.EnvVar{{Name: NvidiaDriverCapsEnvVar, Value: "compute,utility"}},
        Resources: corev1.ResourceRequirements{
            Limits: corev1.ResourceList{
                "nvidia.com/gpu": *resource.NewQuantity(1, resource.BinarySI),
            },
        },
    }
    pod := &corev1.Pod{
        ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{VulkanEnableAnno: "true"}},
    }
    _, _ = dev.MutateAdmission(ctr, pod)

    var caps string
    for _, e := range ctr.Env {
        if e.Name == NvidiaDriverCapsEnvVar {
            caps = e.Value
        }
    }
    assert.Assert(t, strings.Contains(caps, "compute"))
    assert.Assert(t, strings.Contains(caps, "utility"))
    assert.Assert(t, strings.Contains(caps, "graphics"))
}

func TestMutateAdmission_VulkanAnno_AllCaps_NoChange(t *testing.T) {
    dev := &NvidiaGPUDevices{
        config: NvidiaConfig{
            ResourceCountName: "nvidia.com/gpu",
        },
    }
    ctr := &corev1.Container{
        Env: []corev1.EnvVar{{Name: NvidiaDriverCapsEnvVar, Value: "all"}},
        Resources: corev1.ResourceRequirements{
            Limits: corev1.ResourceList{
                "nvidia.com/gpu": *resource.NewQuantity(1, resource.BinarySI),
            },
        },
    }
    pod := &corev1.Pod{
        ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{VulkanEnableAnno: "true"}},
    }
    _, _ = dev.MutateAdmission(ctr, pod)

    for _, e := range ctr.Env {
        if e.Name == NvidiaDriverCapsEnvVar {
            assert.Equal(t, e.Value, "all")
        }
    }
}

func TestMutateAdmission_NoVulkanAnno_NoChange(t *testing.T) {
    dev := &NvidiaGPUDevices{
        config: NvidiaConfig{ResourceCountName: "nvidia.com/gpu"},
    }
    ctr := &corev1.Container{
        Resources: corev1.ResourceRequirements{
            Limits: corev1.ResourceList{
                "nvidia.com/gpu": *resource.NewQuantity(1, resource.BinarySI),
            },
        },
    }
    pod := &corev1.Pod{}
    _, _ = dev.MutateAdmission(ctr, pod)
    for _, e := range ctr.Env {
        assert.Assert(t, e.Name != NvidiaDriverCapsEnvVar, "unexpected caps env")
        assert.Assert(t, e.Name != HamiVulkanEnvVar, "unexpected enable env")
    }
}

func TestMutateAdmission_VulkanAnno_NoGPUResource(t *testing.T) {
    dev := &NvidiaGPUDevices{
        config: NvidiaConfig{
            ResourceCountName:            "nvidia.com/gpu",
            ResourceMemoryName:           "nvidia.com/gpumem",
            ResourceCoreName:             "nvidia.com/gpucores",
            ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
        },
    }
    ctr := &corev1.Container{Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{}}}
    pod := &corev1.Pod{
        ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{VulkanEnableAnno: "true"}},
    }
    _, _ = dev.MutateAdmission(ctr, pod)
    for _, e := range ctr.Env {
        assert.Assert(t, e.Name != HamiVulkanEnvVar, "no Vulkan env on non-GPU pod")
    }
}

func TestMutateAdmission_VulkanAnno_IdempotentHamiEnable(t *testing.T) {
    dev := &NvidiaGPUDevices{
        config: NvidiaConfig{ResourceCountName: "nvidia.com/gpu"},
    }
    ctr := &corev1.Container{
        Env: []corev1.EnvVar{{Name: HamiVulkanEnvVar, Value: "1"}},
        Resources: corev1.ResourceRequirements{
            Limits: corev1.ResourceList{
                "nvidia.com/gpu": *resource.NewQuantity(1, resource.BinarySI),
            },
        },
    }
    pod := &corev1.Pod{
        ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{VulkanEnableAnno: "true"}},
    }
    _, _ = dev.MutateAdmission(ctr, pod)
    count := 0
    for _, e := range ctr.Env {
        if e.Name == HamiVulkanEnvVar {
            count++
        }
    }
    assert.Equal(t, count, 1)
}
```

- [ ] **Step 2: 모두 PASS 확인**

Run:
```bash
go test ./pkg/device/nvidia/ -run TestMutateAdmission_VulkanAnno -v
```
Expected: 5 tests PASS.

- [ ] **Step 3: 기존 전체 테스트 회귀 없음 확인**

Run:
```bash
go test ./pkg/device/nvidia/...
```
Expected: PASS 전체.

- [ ] **Step 4: 커밋**

```bash
git add pkg/device/nvidia/device_test.go
git commit -m "test(nvidia): cover Vulkan annotation edge cases"
```

---

### Task 2.4: HAMi-core submodule 포인터 업데이트

**Files:**
- Modify: `libvgpu` submodule reference

- [ ] **Step 1: Phase 1에서 머지된 HAMi-core 커밋 확인**

Task 1.9의 PR이 머지된 후, `libvgpu` 레포 main의 최신 커밋 SHA를 확보.

- [ ] **Step 2: submodule 업데이트**

Run:
```bash
cd libvgpu
git fetch origin main
git checkout main
git pull
cd ..
git diff --submodule libvgpu
```
Expected: `libvgpu <old>..<new>` 한 줄.

- [ ] **Step 3: submodule 포인터 커밋**

Run:
```bash
git add libvgpu
git commit -m "deps: bump libvgpu to include Vulkan vGPU layer"
```

---

## Phase 3 — 예제 및 문서

### Task 3.1: Vulkan 예제 파드

**Files:**
- Create: `examples/nvidia/vulkan_example.yaml`

- [ ] **Step 1: 예제 YAML 작성**

Create `examples/nvidia/vulkan_example.yaml`:
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: hami-vulkan-example
  annotations:
    hami.io/vulkan: "true"
spec:
  restartPolicy: Never
  containers:
    - name: vulkaninfo
      # any image with vulkaninfo + libvulkan1
      image: khronosgroup/vulkan-samples:latest
      command: ["vulkaninfo"]
      resources:
        limits:
          nvidia.com/gpu: "1"
          nvidia.com/gpumem: "1024"   # 1 GiB VRAM budget
          nvidia.com/gpucores: "30"   # 30% SM throttle
```

- [ ] **Step 2: 커밋**

```bash
git add examples/nvidia/vulkan_example.yaml
git commit -m "example: Vulkan vGPU partitioned pod"
```

---

### Task 3.2: 지원 문서 (영문)

**Files:**
- Create: `docs/vulkan-vgpu-support.md`

- [ ] **Step 1: 문서 작성**

Create `docs/vulkan-vgpu-support.md`:
```markdown
# Vulkan vGPU Support

HAMi partitions NVIDIA GPUs for Vulkan workloads by injecting a Vulkan implicit
layer (`VK_LAYER_HAMI_vgpu`) that shares the same VRAM and SM budgets used by
the existing CUDA hooks.

## Enabling Vulkan partitioning

Add the `hami.io/vulkan: "true"` annotation to any pod that uses HAMi NVIDIA
resources. The webhook will:

- Union `graphics` into `NVIDIA_DRIVER_CAPABILITIES` so the NVIDIA Container
  Toolkit mounts the Vulkan ICD and graphics libraries.
- Set `HAMI_VULKAN_ENABLE=1` which activates the HAMi Vulkan layer via its
  `enable_environment` clause in the implicit layer manifest.

Example: `examples/nvidia/vulkan_example.yaml`.

## What gets limited

- `nvidia.com/gpumem` enforces VRAM allocation across **both** CUDA and Vulkan
  in the container, sharing a single budget.
- `nvidia.com/gpucores` throttles Vulkan `vkQueueSubmit[2]` using the same
  NVML-based polling loop as `cuLaunchKernel`.
- `vkGetPhysicalDeviceMemoryProperties[2]` clamps the device-local heap size
  to the pod budget so apps that size allocations from this value self-limit.

## What is not limited (yet)

- Vulkan Video (`VK_KHR_video_queue`) submissions.
- Frame-pacing jitter introduced by throttling on graphics queues (documented
  behavior; strict/cooperative modes are a future option).

## Troubleshooting

| Symptom | Check |
|---------|-------|
| Container has no `vulkan` CLI / libs | Annotation absent or `NVIDIA_DRIVER_CAPABILITIES` already frozen to `compute` by image. |
| `vkAllocateMemory` always succeeds | Layer did not activate — ensure `HAMI_VULKAN_ENABLE=1` set and `/etc/vulkan/implicit_layer.d/hami.json` exists. |
| `vulkaninfo` still shows full VRAM heap | Layer manifest not loaded; run `VK_LOADER_DEBUG=all vulkaninfo` to see layer scan. |
```

- [ ] **Step 2: 커밋**

```bash
git add docs/vulkan-vgpu-support.md
git commit -m "docs: Vulkan vGPU support guide"
```

---

### Task 3.3: 중국어 번역

**Files:**
- Create: `docs/vulkan-vgpu-support_cn.md`

- [ ] **Step 1: 영문 문서를 중국어로 번역해서 작성**

Create `docs/vulkan-vgpu-support_cn.md`:
```markdown
# Vulkan vGPU 支持

HAMi 通过注入 Vulkan 隐式层（`VK_LAYER_HAMI_vgpu`）对 NVIDIA GPU 进行 Vulkan 工作负载的切分。该层与已有的 CUDA 钩子共享同一套 VRAM 与 SM 预算。

## 启用方式

在使用 HAMi NVIDIA 资源的 Pod 上添加 annotation `hami.io/vulkan: "true"`。Webhook 会：

- 将 `graphics` 合并进 `NVIDIA_DRIVER_CAPABILITIES`，以便 NVIDIA Container Toolkit 挂载 Vulkan ICD 与图形库。
- 设置 `HAMI_VULKAN_ENABLE=1`，通过隐式层 manifest 的 `enable_environment` 激活 HAMi Vulkan 层。

示例：`examples/nvidia/vulkan_example.yaml`。

## 生效范围

- `nvidia.com/gpumem` 对容器内 CUDA 与 Vulkan 的 VRAM 分配**共享同一预算**。
- `nvidia.com/gpucores` 通过与 `cuLaunchKernel` 相同的 NVML 轮询机制对 `vkQueueSubmit[2]` 进行限速。
- `vkGetPhysicalDeviceMemoryProperties[2]` 将 device-local 堆大小裁剪为 Pod 预算。

## 未涵盖项（未来工作）

- Vulkan Video（`VK_KHR_video_queue`）提交。
- 图形队列限速导致的帧抖动（已记录，未来提供 strict/cooperative 模式）。

## 故障排查

| 现象 | 检查 |
|------|------|
| 容器没有 Vulkan 库 | annotation 缺失，或镜像已冻结 `NVIDIA_DRIVER_CAPABILITIES=compute`。 |
| `vkAllocateMemory` 总是成功 | 层未激活 — 确认 `HAMI_VULKAN_ENABLE=1` 与 `/etc/vulkan/implicit_layer.d/hami.json` 存在。 |
| `vulkaninfo` 仍报告全量 VRAM | Manifest 未加载；可 `VK_LOADER_DEBUG=all vulkaninfo` 查看扫描日志。 |
```

- [ ] **Step 2: 커밋**

```bash
git add docs/vulkan-vgpu-support_cn.md
git commit -m "docs: 中文版 Vulkan vGPU 支持说明"
```

---

## Phase 4 — 통합 검증

### Task 4.1: 수동 E2E — 힙 클램프 확인

**Files:** (런타임 실행)

- [ ] **Step 1: HAMi-core 이미지 빌드**

Run:
```bash
cd libvgpu && docker build -t projecthami/hami-vgpu:dev . && cd ..
```

- [ ] **Step 2: HAMi 이미지에 submodule 반영 빌드**

Run:
```bash
make docker-build
```
(없으면 기존 CI 명령 사용)

- [ ] **Step 3: 테스트 클러스터에 배포**

Run:
```bash
helm upgrade --install hami charts/hami \
    --set scheduler.image.repository=projecthami/hami-scheduler \
    --set scheduler.image.tag=dev \
    --set devicePlugin.image.repository=projecthami/hami-device-plugin \
    --set devicePlugin.image.tag=dev \
    --set vgpu.image.repository=projecthami/hami-vgpu \
    --set vgpu.image.tag=dev
kubectl apply -f examples/nvidia/vulkan_example.yaml
```

- [ ] **Step 4: 힙 클램프 확인**

Run:
```bash
kubectl logs hami-vulkan-example | grep -iE "heap|device local"
```
Expected: device-local 힙 size가 ≤ 1 GiB (1024 MiB, pod 버짓).

- [ ] **Step 5: 결과 기록**

`docs/superpowers/plans/notes/e2e-vulkaninfo.md`에 로그 요약을 적는다.

- [ ] **Step 6: 커밋**

```bash
git add docs/superpowers/plans/notes/e2e-vulkaninfo.md
git commit -m "test(e2e): vulkaninfo heap clamp verified in HAMi-scheduled pod"
```

---

### Task 4.2: 수동 E2E — 할당 초과 시 OOM 반환

**Files:** (런타임 실행)

- [ ] **Step 1: 할당 초과 테스트 스크립트 작성**

Create `examples/nvidia/vulkan_oom_test.yaml`:
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: hami-vulkan-oom-test
  annotations:
    hami.io/vulkan: "true"
spec:
  restartPolicy: Never
  containers:
    - name: oom
      image: ghcr.io/example/vulkan-alloc-test:latest  # 2 GiB를 반복 할당하는 테스트 바이너리
      resources:
        limits:
          nvidia.com/gpu: "1"
          nvidia.com/gpumem: "1024"
```
(이미지가 없으면, 간단한 C 프로그램 `vkAllocateMemory(2GiB)` 루프를 작성해 별도 이미지로 빌드.)

- [ ] **Step 2: 실행 및 OOM 확인**

Run:
```bash
kubectl apply -f examples/nvidia/vulkan_oom_test.yaml
kubectl logs hami-vulkan-oom-test
```
Expected: 로그에 `VK_ERROR_OUT_OF_DEVICE_MEMORY` 또는 등가 메시지.

- [ ] **Step 3: 결과 기록 및 커밋**

`docs/superpowers/plans/notes/e2e-vulkaninfo.md`에 추가 기록.
```bash
git add examples/nvidia/vulkan_oom_test.yaml docs/superpowers/plans/notes/e2e-vulkaninfo.md
git commit -m "test(e2e): vulkan OOM returns VK_ERROR_OUT_OF_DEVICE_MEMORY"
```

---

### Task 4.3: 혼합 워크로드 — CUDA + Vulkan 공유 버짓

**Files:** (런타임 실행)

- [ ] **Step 1: 혼합 컨테이너 파드 작성**

Create `examples/nvidia/vulkan_cuda_mixed.yaml`:
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: hami-vulkan-cuda-mixed
  annotations:
    hami.io/vulkan: "true"
spec:
  restartPolicy: Never
  containers:
    - name: app
      image: ghcr.io/example/cuda-vulkan-mixed:latest  # CUDA 512 MiB + Vulkan 512 MiB
      resources:
        limits:
          nvidia.com/gpu: "1"
          nvidia.com/gpumem: "1024"
```

- [ ] **Step 2: 실행 및 합산 버짓 준수 확인**

Run:
```bash
kubectl apply -f examples/nvidia/vulkan_cuda_mixed.yaml
kubectl logs hami-vulkan-cuda-mixed
```
Expected: 양쪽 할당 성공, 추가 할당 시 OOM.

- [ ] **Step 3: 커밋**

```bash
git add examples/nvidia/vulkan_cuda_mixed.yaml
git commit -m "test(e2e): CUDA+Vulkan mixed workload shares single VRAM budget"
```

---

### Task 4.4: 플랜 아티팩트 정리 및 최종 PR

**Files:**
- Delete: `docs/superpowers/plans/notes/` (임시 노트)

- [ ] **Step 1: 노트 디렉토리 제거**

Run:
```bash
git rm -r docs/superpowers/plans/notes/
git commit -m "chore: drop temporary planning notes"
```

- [ ] **Step 2: HAMi 브랜치 푸시 및 PR**

Run:
```bash
git push -u origin vulkan-vgpu-partitioning
gh pr create --title "feat(nvidia): Vulkan vGPU partitioning" \
             --body "$(cat <<'EOF'
## Summary
- Webhook injects graphics cap + HAMI_VULKAN_ENABLE=1 when `hami.io/vulkan: "true"` annotation is present
- libvgpu submodule bumped to include Vulkan implicit layer (VK_LAYER_HAMI_vgpu)
- CUDA and Vulkan share the existing `nvidia.com/gpumem` and `nvidia.com/gpucores` budgets
- Docs + example added

Design: docs/superpowers/specs/2026-04-21-vulkan-vgpu-partitioning-design.md
HAMi-core PR: (link from notes/hami-core-pr.md before deletion)

## Test plan
- [x] Go unit tests (5 new)
- [x] HAMi-core unit tests (layer / memprops / alloc / submit / throttle)
- [x] E2E: vulkaninfo heap clamp
- [x] E2E: vkAllocateMemory OOM at budget
- [x] E2E: CUDA + Vulkan mixed workload shares budget
EOF
)"
```

---

## 자가 점검

### 스펙 커버리지

| 스펙 요구사항 | 해당 Task |
|---------------|-----------|
| §3 Activation via annotation | Task 2.2, 2.3 |
| §5.1 Go 상수/로직 | Task 2.1, 2.2 |
| §5.2 C 레이어 엔트리포인트 | Task 1.1 |
| §5.2 메모리 속성 clamp | Task 1.2 |
| §5.2 vkAllocateMemory/vkFreeMemory | Task 1.3 |
| §5.2 vkQueueSubmit throttle | Task 1.4 + 1.5 |
| §5.3 공유 카운터 통합 | Task 1.6 |
| §5.4 Manifest JSON | Task 1.7 |
| §5.5 Build 통합 | Task 1.8 |
| §6 데이터 흐름 (admission + runtime) | Task 2.2 (admission), 1.1~1.5 (runtime) |
| §7 에러 처리 (merge 규칙) | Task 2.3 (edge cases) |
| §8.1 Go 단위 테스트 | Task 2.1, 2.3 |
| §8.2 C 단위 테스트 | Task 1.1~1.5 |
| §8.3 E2E | Task 4.1, 4.2, 4.3 |
| §9 Delivery 순서 | Phase 1 → 2 → 3 → 4 |

### 타입 일관성

- Go: `VulkanEnableAnno`, `NvidiaDriverCapsEnvVar`, `HamiVulkanEnvVar`를 Task 2.1, 2.2, 2.3에서 동일하게 사용.
- C: `hami_reserve_device_memory(int, size_t)` / `hami_release_device_memory(int, size_t)` / `hami_pod_memory_budget(int)`을 Task 1.3, 1.6에서 동일 시그니처 유지.
- C: `hami_throttle_wait(int dev_idx, int util_limit)` Task 1.4, 1.5에서 동일.

### Placeholder 없음 확인

- 모든 "Step"이 실제 커맨드/코드/기대 출력 포함.
- HAMi-core 기존 카운터 함수 이름은 Task 0.2 탐색 노트를 근거로 Task 1.6 어댑터에서 실제 이름으로 교체하도록 지시함 (노트 자체가 아티팩트).
- 테스트 코드는 매 Task마다 full source 포함.
