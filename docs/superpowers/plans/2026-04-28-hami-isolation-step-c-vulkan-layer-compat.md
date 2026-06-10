# HAMi vGPU 격리 — Step C: HAMi-core Vulkan Layer Compat Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** HAMi-core Vulkan layer (`libvgpu/src/vulkan/`) 가 NVIDIA Isaac Sim Kit (Carbonite/OptiX/Aftermath) 의 Vulkan 초기화 경로에서 NULL deref 없이 dispatch chain 을 끝까지 forwarding 하도록 보강한다. 핵심: **HAMI_VK_TRACE 로그로 실제 호출 패턴 수집 → evidence 있는 break 만 fix**, 추측성 hardening 금지.

**Architecture:** 7개 task. (1) 미완성 WIP foundation commit, (2) GIPA/GDPA cached-fallback, (3) trace 로 evidence 수집, (4) 데이터 기반 hook 추가, (5) dispatch lifetime + chain copy audit (read-only), (6) LD_PRELOAD 강제 후 runheadless 5/5 + partition test, (7) push + draft PR comments.

**Tech Stack:** C, CMake, Vulkan loader spec 1.3 §38, HAMi-core fork (`/Users/xiilab/git/HAMi/libvgpu`, branch `vulkan-layer`), Docker (build-in-docker), kubectl, ws-node074.

**Plan scope:** Step C 만. Step D (isaac-launchable opt-in 활성화 + 4-path 검증) 별도 plan.

---

## File Structure

| 파일 | 변경 종류 | 책임 |
|---|---|---|
| `libvgpu/src/vulkan/layer.c` | Modify | g_first_next_gipa/gdpa cache, GIPA/GDPA fallback, 추가 PhysicalDevice query hook (Task 4 evidence 기반) |
| `libvgpu/src/vulkan/dispatch.c` | Modify | EnumerateDevice* dispatch table entry resolve, hami_instance_first() impl |
| `libvgpu/src/vulkan/dispatch.h` | Modify | dispatch struct EnumerateDevice* fields, hami_instance_first() decl |
| `libvgpu/docs/superpowers/notes/2026-04-28-vk-trace-isaac-sim.md` (Task 3) | Create | Isaac Sim Kit 가 호출하는 GIPA name 목록 + NULL 반환 케이스 |
| `libvgpu/docs/superpowers/notes/2026-04-28-vk-dispatch-lifetime-audit.md` (Task 5) | Create | dispatch lifetime + chain copy audit 결과 |

추가 hook 패턴 (Task 4 evidence 기반):

```c
/* vkGetPhysicalDevice<X> is a thin pass-through. We don't apply HAMi
 * partitioning to read-only queries — only forward through the next
 * layer/ICD via the cached dispatch entry. The reason we hook at all is
 * because the loader pre-resolves these via GIPA(NULL, "vk...") during
 * implicit-layer init: returning NULL there breaks Carbonite. */
static VKAPI_ATTR void VKAPI_CALL
hami_vkGetPhysicalDevice<X>(VkPhysicalDevice phys, ...) {
    hami_instance_dispatch_t *d = hami_instance_first();
    if (!d || !d-><X>) return;
    d-><X>(phys, ...);
}
```

---

## Tasks

### Task 1: WIP foundation commit (Enumerate via dispatch + gipa/gdpa cache + first helper)

**Files:**
- Modify (already in working tree, just need to commit): `libvgpu/src/vulkan/layer.c`, `libvgpu/src/vulkan/dispatch.c`, `libvgpu/src/vulkan/dispatch.h`

이 task 는 **이미 working tree 에 있는 unstaged 변경을 commit 하는 것**. Step B 진행 중 controller 가 의도적으로 staging 안 했다.

- [ ] **Step 1: Verify unstaged diff is consistent with the design**

```bash
cd /Users/xiilab/git/HAMi/libvgpu
git diff --stat src/vulkan/
git diff src/vulkan/dispatch.h
git diff src/vulkan/dispatch.c
git diff src/vulkan/layer.c | head -200
```

Expected diff:
- `dispatch.h` adds `EnumerateDeviceExtensionProperties` + `EnumerateDeviceLayerProperties` PFN fields to `hami_instance_dispatch_t`, adds `hami_instance_first()` decl.
- `dispatch.c` resolves both names in `hami_instance_register`, implements `hami_instance_first()` (returns `g_inst_head` under lock).
- `layer.c` adds `g_first_next_gipa` / `g_first_next_gdpa` static caches set in CreateInstance/CreateDevice, refactors `hami_vkEnumerateDeviceExtensionProperties` / `hami_vkEnumerateDeviceLayerProperties` to forward via `hami_instance_first()->Enumerate*`, expands comments.

If diff has unrelated changes — STOP, ask controller.

- [ ] **Step 2: build-in-docker on ws-node074 to verify the WIP compiles**

```bash
cd /Users/xiilab/git/HAMi/libvgpu
rsync -az --exclude=build --exclude=.git/objects/pack . root@10.61.3.74:/tmp/libvgpu-build/
ssh root@10.61.3.74 'cd /tmp/libvgpu-build && rm -rf .git build && git init -q && git add -A 2>&1 | tail -1 && git -c user.email=x@x -c user.name=x commit -q -m local --no-gpg-sign && make build-in-docker 2>&1 | tail -10'
```

Expected: `Built target vgpu`, no errors.

- [ ] **Step 3: Run all existing unit tests under LD_PRELOAD (regression check for Step B)**

```bash
ssh root@10.61.3.74 'cd /tmp/libvgpu-build/build/test && LD_PRELOAD=/tmp/libvgpu-build/build/libvgpu.so ./test_cuda_null_guards 2>&1; echo EXIT=$?'
```

Expected: 9 `[OK]` lines, EXIT=0. (Vulkan tests have separate build path — Step C plan does not modify them.)

- [ ] **Step 4: Commit**

```bash
cd /Users/xiilab/git/HAMi/libvgpu
git add src/vulkan/layer.c src/vulkan/dispatch.c src/vulkan/dispatch.h
git commit -s -m "fix(vulkan): cache first next-gipa/gdpa + EnumerateDevice* via dispatch table" \
  -m "Foundation for Step C compat hardening:

* dispatch.{h,c}: add EnumerateDeviceExtensionProperties +
  EnumerateDeviceLayerProperties function pointers to the per-instance
  dispatch struct; resolve both during hami_instance_register so the
  layer's own Enumerate* hooks can forward correctly. Add
  hami_instance_first() helper that returns the first registered
  instance dispatch under lock — used by NULL-instance Enumerate
  forwarding when the loader probes before any instance has been
  created.
* layer.c: cache the first next-layer GetInstanceProcAddr /
  GetDeviceProcAddr in static globals during CreateInstance /
  CreateDevice. Expands comments documenting the Vulkan 1.3 §38.3.1
  contract for own-name vs NULL pLayerName Enumerate semantics, and
  why an earlier draft returning LAYER_NOT_PRESENT broke
  vkCreateDevice.

This commit only restructures the existing Enumerate hooks; it does not
yet change GIPA/GDPA fallback behavior (Task 2)."
```

Expected: 1 commit on top of `7dcb5a4`, working tree clean.

---

### Task 2: GIPA / GDPA cached-fallback for unknown instance / device

**Files:**
- Modify: `libvgpu/src/vulkan/layer.c` — `hami_vkGetInstanceProcAddr`, `hami_vkGetDeviceProcAddr`

**Bug:** When NVIDIA driver / Carbonite call our GIPA/GDPA with a `VkInstance`/`VkDevice` handle that we haven't registered (e.g., loader probe before `vkCreateInstance` returns, or upper layer wraps the handle), `hami_instance_lookup(instance)` returns NULL and we return NULL → caller dereferences NULL and SegFaults.

**Fix:** When lookup returns NULL but we have `g_first_next_gipa`/`g_first_next_gdpa` cached from a previous `vkCreateInstance`/`vkCreateDevice`, forward to that cached function. Only when both lookup AND cache are NULL do we return NULL (legitimately uninitialized state — pre-CreateInstance loader bootstrap).

- [ ] **Step 1: Modify `hami_vkGetInstanceProcAddr` (around line 297)**

Change:
```c
    hami_instance_dispatch_t *d = hami_instance_lookup(instance);
    if (!d) {
        HAMI_TRACE("hami_vkGetInstanceProcAddr: instance %p not registered, returning NULL", (void *)instance);
        return NULL;
    }
    return d->next_gipa(instance, pName);
```

to:
```c
    hami_instance_dispatch_t *d = hami_instance_lookup(instance);
    if (d) return d->next_gipa(instance, pName);
    /* Unknown VkInstance handle: NVIDIA driver and Carbonite occasionally
     * probe through our GIPA with handles we haven't registered (e.g.,
     * during vkCreateInstance before our register call returns, or with
     * an upper-layer-wrapped handle). Returning NULL would SegFault the
     * caller. Forward to the first cached next-layer gipa instead — it
     * was set the first time vkCreateInstance ran and is a valid pointer
     * into the next layer / driver. */
    if (g_first_next_gipa) {
        HAMI_TRACE("hami_vkGetInstanceProcAddr: instance %p not registered, forwarding via cached gipa", (void *)instance);
        return g_first_next_gipa(instance, pName);
    }
    /* Pre-CreateInstance loader bootstrap: the only case where the spec
     * allows us to return NULL for instance entry points (the loader
     * still resolves the global Enumerate* hooks via the same GIPA, but
     * those are matched above by HAMI_HOOK before this fall-through). */
    HAMI_TRACE("hami_vkGetInstanceProcAddr: instance %p not registered AND no cached gipa, returning NULL", (void *)instance);
    return NULL;
```

- [ ] **Step 2: Same pattern for `hami_vkGetDeviceProcAddr` (around line 323)**

Change:
```c
    hami_device_dispatch_t *d = hami_device_lookup(device);
    if (!d) return NULL;
    return d->next_gdpa(device, pName);
```

to:
```c
    hami_device_dispatch_t *d = hami_device_lookup(device);
    if (d) return d->next_gdpa(device, pName);
    if (g_first_next_gdpa) {
        return g_first_next_gdpa(device, pName);
    }
    return NULL;
```

- [ ] **Step 3: Build + run existing unit tests (regression)**

```bash
cd /Users/xiilab/git/HAMi/libvgpu
rsync -az --exclude=build --exclude=.git/objects/pack . root@10.61.3.74:/tmp/libvgpu-build/
ssh root@10.61.3.74 'cd /tmp/libvgpu-build && rm -rf .git build && git init -q && git add -A 2>&1 | tail -1 && git -c user.email=x@x -c user.name=x commit -q -m local --no-gpg-sign && make build-in-docker 2>&1 | tail -10 && cd build/test && LD_PRELOAD=/tmp/libvgpu-build/build/libvgpu.so ./test_cuda_null_guards 2>&1; echo EXIT=$?'
```

Expected: build OK, 9 `[OK]` lines (Step B regression), EXIT=0.

- [ ] **Step 4: Commit**

```bash
cd /Users/xiilab/git/HAMi/libvgpu
git add src/vulkan/layer.c
git commit -s -m "fix(vulkan): GIPA/GDPA fallback to cached next when instance/device unknown" \
  -m "NVIDIA driver and Carbonite probe through our GIPA/GDPA with handles
that may not yet be registered: during vkCreateInstance before our
register completes, or with upper-layer-wrapped handles. Returning
NULL there crashed the caller (SegFault inside libcarb.graphics-vulkan
when assembling the dispatch table).

Now we forward to the first-cached next_gipa/next_gdpa from a previous
CreateInstance/CreateDevice. Only when both per-handle lookup AND the
cache are absent do we return NULL — that's the legitimate
pre-CreateInstance loader bootstrap window where Enumerate* hooks have
already been matched at the top of the function."
```

---

### Task 3: trace which vkGetPhysicalDevice* queries Isaac Sim Kit makes

**Files:**
- Create: `libvgpu/docs/superpowers/notes/2026-04-28-vk-trace-isaac-sim.md`

이 task 는 코드 변경 0 — 실제 trace 수집. Task 4 의 데이터 기반 hook 추가에 입력.

- [ ] **Step 1: Verify the new build (with Tasks 1-2 commits) is on ws-node074 + swap into /usr/local/vgpu/**

```bash
ssh root@10.61.3.74 '
md5sum /tmp/libvgpu-build/build/libvgpu.so
cp -av /usr/local/vgpu/libvgpu.so /usr/local/vgpu/libvgpu.so.bak-pre-step-c 2>&1 | tail -2
cp -f /tmp/libvgpu-build/build/libvgpu.so /usr/local/vgpu/libvgpu.so
md5sum /usr/local/vgpu/libvgpu.so
'
```

- [ ] **Step 2: runheadless.sh under HAMI_VK_TRACE=1 + LD_PRELOAD inside isaac-launchable pod**

```bash
NEWPOD=$(kubectl -n isaac-launchable get pod -o jsonpath='{.items[0].metadata.name}')
echo "Pod: $NEWPOD"

kubectl -n isaac-launchable exec $NEWPOD -c vscode -- bash -lc '
mkdir -p /tmp/vk-trace
pkill -KILL kit 2>/dev/null; sleep 2
timeout 50 env \
  ACCEPT_EULA=y \
  HAMI_VK_TRACE=1 \
  LD_PRELOAD=/usr/local/vgpu/libvgpu.so \
  /isaac-sim/runheadless.sh > /tmp/vk-trace/run.log 2>&1
EC=$?
pkill -KILL kit 2>/dev/null
echo "exit=$EC"
echo "=== HAMI_VK_TRACE lines ==="
grep -c "HAMI_VK_TRACE" /tmp/vk-trace/run.log
echo "=== unique GIPA names (sorted by count) ==="
grep "hami_vkGetInstanceProcAddr.*name=" /tmp/vk-trace/run.log | sed -e "s/.*name=//" -e "s/ .*//" | sort | uniq -c | sort -rn | head -50
echo "=== GDPA names ==="
grep "hami_vkGetDeviceProcAddr.*name=" /tmp/vk-trace/run.log 2>/dev/null | sed -e "s/.*name=//" | sort | uniq -c | sort -rn | head -30
echo "=== unregistered fallback hits ==="
grep -c "not registered" /tmp/vk-trace/run.log
echo "=== SegFault / Segmentation ==="
grep -E "Segmentation|crash has occurred" /tmp/vk-trace/run.log | head -10
'
```

Expected output structure:
- `exit=124` (timeout = alive) OR `exit=139` (crash — Step C still failing for this scenario)
- Top-N GIPA names: many `vkCreateInstance`, `vkGetPhysicalDeviceMemoryProperties`, `vkAllocateMemory`, etc.
- Names returning NULL: those that fall through (`not registered` lines) tell us which entry points needed cached-gipa fallback.

- [ ] **Step 3: Save trace evidence to notes file**

```bash
cd /Users/xiilab/git/HAMi/libvgpu
mkdir -p docs/superpowers/notes
cat > docs/superpowers/notes/2026-04-28-vk-trace-isaac-sim.md <<EOF
# Vulkan layer trace — Isaac Sim Kit init under LD_PRELOAD (2026-04-28)

Build base: HAMi-core \`vulkan-layer\` after Step C Tasks 1-2.

## Methodology

\`\`\`
HAMI_VK_TRACE=1 LD_PRELOAD=/usr/local/vgpu/libvgpu.so /isaac-sim/runheadless.sh
\`\`\`

(timeout 50s; pod isaac-launchable-0 / vscode container)

## Findings (paste from Step 2 output)

### Exit code

(fill: 124 = alive, 139 = SegFault)

### Top-N vkGetInstanceProcAddr names

(paste sorted-by-count list)

### vkGetDeviceProcAddr names

(paste)

### "not registered" fall-through count

(paste count)

### vkGetPhysicalDevice* names that need explicit hooks

(decision: list which names appeared in the trace AND returned NULL —
those are the ones Task 4 should hook. Skip names that already forward
via cached-gipa fallback (Task 2 fix).)
EOF
# Edit/fill the placeholders with the actual Step 2 output
\$EDITOR docs/superpowers/notes/2026-04-28-vk-trace-isaac-sim.md
```

(Or just inline-write via a heredoc if you have the trace output handy — the point is to capture the evidence.)

- [ ] **Step 4: Commit notes**

```bash
git add docs/superpowers/notes/2026-04-28-vk-trace-isaac-sim.md
git commit -s -m "docs(notes): vk trace for Isaac Sim Kit init under LD_PRELOAD"
```

---

### Task 4: add explicit hooks for vkGetPhysicalDevice* names that broke (evidence-driven)

**Files:**
- Modify: `libvgpu/src/vulkan/layer.c` (HAMI_HOOK entries + thin wrappers)
- Modify: `libvgpu/src/vulkan/dispatch.c`, `dispatch.h` (add PFN fields + resolve)

**Decision rule:** Task 3 trace 의 결론에 따라.
- **If trace 결과 모든 vkGetPhysicalDevice* 가 cached-gipa 로 정상 forward 됨 (exit=124, no crash, no "not registered" 다수)** → Task 4 는 코드 변경 0, just document "no additional hooks needed" 로 commit 하고 끝.
- **If 특정 vkGetPhysicalDeviceX 에서 fall-through 또는 crash** → 해당 name 만 hook.

#### IF additional hooks needed (예시: vkGetPhysicalDeviceFormatProperties2)

- [ ] **Step 1: dispatch.h 에 PFN field 추가**

```c
typedef struct hami_instance_dispatch {
    /* ... existing fields ... */
    PFN_vkGetPhysicalDeviceFormatProperties2 GetPhysicalDeviceFormatProperties2;
    /* ... */
} hami_instance_dispatch_t;
```

- [ ] **Step 2: dispatch.c 의 hami_instance_register 에 resolve 추가**

```c
d->GetPhysicalDeviceFormatProperties2 =
    (PFN_vkGetPhysicalDeviceFormatProperties2)resolve(gipa, inst, "vkGetPhysicalDeviceFormatProperties2");
```

- [ ] **Step 3: layer.c 에 thin wrapper 추가**

```c
static VKAPI_ATTR void VKAPI_CALL
hami_vkGetPhysicalDeviceFormatProperties2(VkPhysicalDevice phys,
                                           VkFormat format,
                                           VkFormatProperties2 *pProperties) {
    hami_instance_dispatch_t *d = hami_instance_first();
    if (!d || !d->GetPhysicalDeviceFormatProperties2) return;
    d->GetPhysicalDeviceFormatProperties2(phys, format, pProperties);
}
```

- [ ] **Step 4: HAMI_HOOK 추가 (in hami_vkGetInstanceProcAddr)**

```c
HAMI_HOOK(GetPhysicalDeviceFormatProperties2);
```

(Repeat for each name from Task 3 evidence.)

- [ ] **Step 5: build + verify the trace path no longer hits "not registered" for the new names**

```bash
# (rebuild + swap .so + re-run trace from Task 3 Step 2)
# Expected: "not registered" count drops to ~0 for the names just hooked.
```

- [ ] **Step 6: Commit (one commit even if multiple names)**

```bash
git add src/vulkan/{layer,dispatch}.{c,h}
git commit -s -m "fix(vulkan): hook vkGetPhysicalDevice* entry points missing in trace" \
  -m "Trace under HAMI_VK_TRACE=1 + LD_PRELOAD on Isaac Sim Kit init showed
the following names returned NULL through GIPA(VK_NULL_HANDLE, ...)
during loader implicit-layer probing: <LIST>. Each is now hooked with
a thin pass-through wrapper that forwards to the next layer/ICD via
hami_instance_first()->Get*. The layer does not apply HAMi
partitioning to these read-only queries.

See docs/superpowers/notes/2026-04-28-vk-trace-isaac-sim.md for the
trace evidence."
```

---

### Task 5: dispatch lifetime + chain deep-copy audit (review-only)

**Files:**
- Read: `libvgpu/src/vulkan/dispatch.c`, `libvgpu/src/vulkan/layer.c`
- Create: `libvgpu/docs/superpowers/notes/2026-04-28-vk-dispatch-lifetime-audit.md`

이 task 는 **read-only audit** — 코드 변경은 evidence 가 있을 때만.

- [ ] **Step 1: dispatch lifetime audit**

Question: `hami_instance_unregister` / `hami_device_unregister` 호출 시점에 (a) 다른 thread 에서 lookup 중이면 race, (b) Carbonite 가 아직 valid handle 로 알고 있으면 use-after-free.

Investigate:
- `hami_vkDestroyInstance` (layer.c:101) 의 lookup → forward → unregister 순서
- 멀티 instance 환경에서 first instance unregister 후 `hami_instance_first()` 가 두 번째 instance 반환하는지

Document findings.

- [ ] **Step 2: chain pLayerInfo in-place 수정 audit**

`hami_vkCreateInstance` (layer.c:76):
```c
chain->u.pLayerInfo = chain->u.pLayerInfo->pNext;
```

Question: NVIDIA driver 가 createInfo 를 재사용해서 `chain->u.pLayerInfo` 가 이미 advance 된 상태로 본다면 두 번째 layer 가 chain 을 못 따라간다.

Investigate:
- Vulkan loader spec 1.3 §38.4 의 chain 처리 표준 요구사항
- 기존 NVIDIA layer 들 (e.g., nvidia_layers.json) 이 어떻게 처리하는지 (gpgpu/khronos vulkan-loader 소스 참조)

Document findings.

- [ ] **Step 3: notes 파일 작성 + commit**

```bash
cd /Users/xiilab/git/HAMi/libvgpu
cat > docs/superpowers/notes/2026-04-28-vk-dispatch-lifetime-audit.md <<EOF
# Vulkan dispatch lifetime + chain copy audit (2026-04-28)

## Dispatch lifetime

(findings — race risk? use-after-free risk? evidence?)

### Decision

(no change / fix needed: <describe>)

## Chain pLayerInfo in-place advance

(findings — is in-place advance spec-standard? do real layers do this?)

### Decision

(no change / fix needed: <describe deep-copy approach>)
EOF
git add docs/superpowers/notes/2026-04-28-vk-dispatch-lifetime-audit.md
git commit -s -m "docs(notes): vk dispatch lifetime + chain copy audit"
```

(If audit reveals a real bug → STOP and ask controller for guidance on whether to add a Task 5b code-change task.)

---

### Task 6: ws-node074 integration verify (runheadless 5/5 + partition test under LD_PRELOAD)

**Files:**
- (no code change)
- Verify: ws-node074 isaac-launchable pod baseline under forced LD_PRELOAD

이 task 는 진짜 integration test — Step B 의 Task 8 가 못 한 "LD_PRELOAD 강제 후 Isaac Sim 동작" 검증.

- [ ] **Step 1: 새 .so 가 swap 되어 있는지 확인**

```bash
ssh root@10.61.3.74 '
md5sum /usr/local/vgpu/libvgpu.so
md5sum /tmp/libvgpu-build/build/libvgpu.so
'
```

만약 두 md5 다르면 → swap 다시:
```bash
ssh root@10.61.3.74 'cp -f /tmp/libvgpu-build/build/libvgpu.so /usr/local/vgpu/libvgpu.so'
```

- [ ] **Step 2: runheadless.sh 5번 with LD_PRELOAD forced**

```bash
NEWPOD=$(kubectl -n isaac-launchable get pod -o jsonpath='{.items[0].metadata.name}')
kubectl -n isaac-launchable exec $NEWPOD -c vscode -- bash -lc '
mkdir -p /tmp/v
for i in 1 2 3 4 5; do
  pkill -KILL kit 2>/dev/null; sleep 3
  timeout 50 env \
    ACCEPT_EULA=y \
    LD_PRELOAD=/usr/local/vgpu/libvgpu.so \
    /isaac-sim/runheadless.sh >/tmp/v/c$i.log 2>&1
  EC=$?
  CRASH=$(grep -cE "Segmentation fault|crash has occurred" /tmp/v/c$i.log)
  LISTEN=$(ss -tunlp 2>/dev/null | grep -c -E ":49100|:30999")
  echo "run $i (LD_PRELOAD): exit=$EC crash=$CRASH listen=$LISTEN"
done
pkill -KILL kit 2>/dev/null
'
```

Expected: 5/5 `exit=124 crash=0 listen=1`. **이게 진짜 Step C 성공 기준**.

- [ ] **Step 3: vk_partition_test.py — Vulkan partition enforce 유지 확인**

```bash
kubectl -n isaac-launchable exec $NEWPOD -c vscode -- bash -lc '
if [ -f vk_partition_test.py ]; then
  LD_PRELOAD=/usr/local/vgpu/libvgpu.so /isaac-sim/python.sh vk_partition_test.py 2>&1 | head -30
  echo "EXIT=$?"
else
  echo "vk_partition_test.py 부재 — Step D 에서 작성"
fi
'
```

Expected: vk_partition_test.py 가 있으면 partition enforce (44 GiB → 23 GiB clamp) 결과 출력. 없으면 Step D 스킵 가능.

- [ ] **Step 4: nvidia-smi raw 값 확인 (LD_PRELOAD 비활성 vs 활성)**

```bash
kubectl -n isaac-launchable exec $NEWPOD -c vscode -- bash -lc '
echo "=== without LD_PRELOAD (raw) ==="
nvidia-smi --query-gpu=memory.total --format=csv,noheader
echo "=== with LD_PRELOAD (clamped) ==="
LD_PRELOAD=/usr/local/vgpu/libvgpu.so nvidia-smi --query-gpu=memory.total --format=csv,noheader
'
```

Expected: 
- raw: ~46068 MiB
- clamped: 23552 MiB (HAMI_VULKAN_ENABLE + partition annotation 이 있으면; 없으면 raw)

만약 isaac-launchable 에 아직 hami.io/vgpu=enabled label 없으면 clamp 안 됨 — Step D 에서 활성화. Step C 의 의무는 단지 "LD_PRELOAD forced 후 crash 안 함".

---

### Task 7: push HAMi-core fork + bump submodule + draft PR comments (don't post)

**Files:**
- Modify (parent repo): `libvgpu` submodule SHA bump
- Create: `/tmp/step-c-pr-drafts/{pr182,pr1803}.md`

- [ ] **Step 1: Push libvgpu fork**

```bash
cd /Users/xiilab/git/HAMi/libvgpu
git log --oneline -8
git push xiilab vulkan-layer 2>&1 | tail
```

Expected: Step C commits (Task 1, 2, 3, 4-if-any, 5) push 성공.

- [ ] **Step 2: Bump HAMi parent submodule SHA**

```bash
cd /Users/xiilab/git/HAMi
NEW_SHA=$(cd libvgpu && git rev-parse HEAD)
echo "new HAMi-core SHA: $NEW_SHA"
git add libvgpu
git commit -s -m "chore(libvgpu): bump HAMi-core for Step C vulkan layer compat" \
  -m "Pulls in Step C commits hardening the Vulkan layer for Isaac Sim Kit
init paths. See docs/superpowers/specs/2026-04-28-hami-isolation-isaac-sim-design.md
section 9 and the plan at docs/superpowers/plans/2026-04-28-hami-isolation-step-c-vulkan-layer-compat.md.

Verified on ws-node074: 5/5 runheadless.sh exit=124 alive under
LD_PRELOAD=/usr/local/vgpu/libvgpu.so (Isaac Sim Kit 6.0.0-rc.22)."
git push xiilab feat/vulkan-vgpu 2>&1 | tail
```

- [ ] **Step 3: Draft PR comments (DO NOT post)**

```bash
mkdir -p /tmp/step-c-pr-drafts

cat > /tmp/step-c-pr-drafts/pr182.md <<'EOF'
## Step C complete — Vulkan layer compat hardening (Isaac Sim Kit)

Builds on Step B (CUDA hook NULL guards). Adds Vulkan layer changes:

| Commit | Change |
|---|---|
| (sha) | dispatch table: EnumerateDevice* PFNs + hami_instance_first() helper |
| (sha) | layer.c: cache first next-gipa/gdpa, refactor Enumerate hooks |
| (sha) | GIPA/GDPA fallback to cached gipa for unknown handles |
| (sha) | (if Task 4) hook vkGetPhysicalDevice<X> entry points found NULL in trace |
| (sha) | docs/notes: trace evidence + dispatch lifetime audit |

### Verification

- 9/9 unit tests (Step B) regression pass
- ws-node074 isaac-launchable pod under `LD_PRELOAD=/usr/local/vgpu/libvgpu.so` + Isaac Sim Kit 6.0.0-rc.22:
  - 5/5 `runheadless.sh` exit=124 alive, no SegFault, listen :49100
  - HAMI_VK_TRACE evidence: <count> GIPA lookups, 0 unhandled "not registered" fall-throughs
- Step D (isaac-launchable opt-in label activation) follows in a separate plan.
EOF

cat > /tmp/step-c-pr-drafts/pr1803.md <<'EOF'
## Step C (Vulkan layer compat) complete

HAMi-core PR #182 added Vulkan layer hardening for Isaac Sim Kit init:

- dispatch table EnumerateDevice* + hami_instance_first() helper
- cached first next-gipa/gdpa
- GIPA/GDPA cached-fallback for unknown handles
- (if Task 4 added hooks) explicit hooks for vkGetPhysicalDevice<X> names that returned NULL through GIPA(NULL, ...)

The `libvgpu` submodule pointer is bumped to <NEW_HAMI_BUMP_SHA>.

### Verification

ws-node074 isaac-launchable pod under `LD_PRELOAD=/usr/local/vgpu/libvgpu.so` runs Isaac Sim Kit (`runheadless.sh`) 5/5 alive (exit=124, listen :49100), no SegFault. Step D (opt-in activation + 4-path enforce verification) follows.

Spec: `docs/superpowers/specs/2026-04-28-hami-isolation-isaac-sim-design.md`
Plan: `docs/superpowers/plans/2026-04-28-hami-isolation-step-c-vulkan-layer-compat.md`
EOF

# Substitute real SHAs
HAMI_BUMP_SHA=$(cd /Users/xiilab/git/HAMi && git rev-parse HEAD)
sed -i.bak "s/<NEW_HAMI_BUMP_SHA>/$HAMI_BUMP_SHA/g" /tmp/step-c-pr-drafts/pr1803.md
rm /tmp/step-c-pr-drafts/pr1803.md.bak

ls -la /tmp/step-c-pr-drafts/
```

(SHA placeholders in pr182.md will be filled by the controller — too many to script.)

- [ ] **Step 4: Report — DO NOT post comments. Wait for explicit user approval.**

---

## Self-Review

**1. Spec coverage:** spec §9.1 (foundation) → Task 1; §9.2 GIPA fallback → Task 2; §9.2 추가 hook → Tasks 3+4 (evidence-driven); §9.2 dispatch lifetime + chain copy → Task 5; §9.3 검증 → Task 6. ✅

**2. Placeholder scan:** Task 4 의 코드 예시는 evidence-driven 결과에 따라 실제 다를 수 있음을 명시 — placeholder 가 아니라 "case 별 구체적 패턴". 이외 placeholder 없음. ✅

**3. Type consistency:** `hami_instance_dispatch_t` / `PFN_vkGet*` / `g_first_next_gipa` 모든 task 에서 일관 사용. ✅

**4. Scope check:** Step C 만. Step D 별도 plan. Step B 는 이미 완료. ✅

**5. Evidence-driven 원칙:** Task 4 가 가장 큰 잠재 scope creep — 명시적으로 "Task 3 trace 결과로만 결정, 추측 hook 추가 금지" 박아둠. ✅

---

## 일정 추정

| Task | 예상 시간 |
|---|---|
| 1 WIP foundation commit | 20분 |
| 2 GIPA/GDPA cached-fallback | 30분 |
| 3 trace + notes | 45분 |
| 4 evidence-driven hooks (range: 0 ~ 6 names × 10min) | 0~60분 |
| 5 lifetime + chain audit (review-only) | 45분 |
| 6 ws-node074 integration verify | 30분 |
| 7 push + draft PR comments | 20분 |
| **총** | **약 3~4시간** |

(Task 4 의 scope 가 trace 결과에 따라 0 ~ 60분으로 큰 편차. 최악의 경우에도 4시간 내.)
