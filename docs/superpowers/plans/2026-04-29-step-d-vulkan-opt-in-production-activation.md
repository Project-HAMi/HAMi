# Step D — Vulkan opt-in production activation + 4-path 검증 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Step C 의 `libvgpu_vk.so` 가 production opt-in path 에서 실제로 chain 진입 + partition enforce 가 NVML / CUDA / Vulkan-memory-query / Vulkan-allocate 4 path 모두에서 작동함을 ws-node074 isaac-launchable-0 에서 검증.

**Architecture:** volcano-vgpu-device-plugin image rebuild → 새 libvgpu.so + libvgpu_vk.so 호스트 install. `hami-vulkan-manifest` ConfigMap 의 `library_path` 를 `libvgpu_vk.so` 로 update + type INSTANCE. manifest installer DaemonSet 재활성. webhook 의 `applyVulkanAnnotation` 코드 그대로 — annotation `hami.io/vulkan: "true"` 가 trigger.

**Tech Stack:** Docker (image rebuild), kubectl (CM/DS apply), ws-node074 (production verification), python (4-path test scripts). Repos: `Project-HAMi/HAMi`, `Project-HAMi/HAMi-core` (libvgpu submodule), `volcano-vgpu-device-plugin` fork at `/Users/xiilab/git/volcano-vgpu-device-plugin/`. Spec: `docs/superpowers/specs/2026-04-29-step-d-vulkan-opt-in-production-activation.md`.

---

## File Structure

| 파일 | 변경 종류 | 책임 |
|---|---|---|
| `/Users/xiilab/git/volcano-vgpu-device-plugin/libvgpu` (submodule) | Modify | submodule SHA bump → `65930f4` (Step C end) |
| `/Users/xiilab/git/volcano-vgpu-device-plugin/docker/Dockerfile.ubuntu20.04` | Inspect / possibly Modify | image build 가 새 `libvgpu_vk.so` 도 `lib/nvidia/` 에 복사하도록 |
| `cluster/runtime/snapshot-2026-04-29-step-d/hami-vulkan-manifest-cm.yaml` | Create (copy from snapshot-2026-04-28) | library_path → libvgpu_vk.so, type → INSTANCE |
| `cluster/runtime/snapshot-2026-04-29-step-d/hami-vulkan-manifest-installer-ds.yaml` | Create (copy from snapshot-2026-04-28) | nodeSelector 복구 |
| `cluster/runtime/snapshot-2026-04-29-step-d/volcano-device-plugin-ds.yaml` | Create (copy from snapshot-2026-04-28) | image tag → vulkan-v2 |
| `cluster/runtime/snapshot-2026-04-29-step-d/4-path-verification.sh` | Create | NVML / CUDA / Vulkan memory / Vulkan allocate 검증 script |
| `cluster/runtime/snapshot-2026-04-29-step-d/vk_partition_test.py` | Create | Vulkan path 검증 python script (vkGetPhysicalDeviceMemoryProperties + vkAllocateMemory) |

---

## Tasks

### Task 1: Inventory current production state + baseline backup

**Files:** none (state capture)

- [ ] **Step 1: Capture current production state**

```bash
ssh root@10.61.3.74 '
echo "=== /usr/local/vgpu/ contents + md5 ==="
ls -la /usr/local/vgpu/ | head
md5sum /usr/local/vgpu/libvgpu*.so 2>/dev/null
echo
echo "=== ConfigMap hami-vulkan-manifest current state ==="
kubectl get cm -n kube-system hami-vulkan-manifest -o yaml | head -30
echo
echo "=== DaemonSet hami-vulkan-manifest-installer status + nodeSelector ==="
kubectl get ds -n kube-system hami-vulkan-manifest-installer -o jsonpath="{.spec.template.spec.nodeSelector}{\"\n\"}{.status}{\"\n\"}"
echo
echo "=== DaemonSet volcano-device-plugin image + status ==="
kubectl get ds -n kube-system volcano-device-plugin -o jsonpath="{.spec.template.spec.containers[*].image}{\"\n\"}{.status}{\"\n\"}"
' > /tmp/step-d-pre-state.txt
cat /tmp/step-d-pre-state.txt
```

Expected output captured to `/tmp/step-d-pre-state.txt`. Verify:
- `libvgpu.so` md5 = `8f889313ece246b2d08ea6291f48b67a` (Step C end baseline)
- `hami-vulkan-manifest-installer` nodeSelector 가 `hami.io/disabled: "true"` (현재 비활성)
- `volcano-device-plugin` image 가 `vulkan-v1`

- [ ] **Step 2: Baseline runheadless on isaac-launchable-0 + isaac-launchable-1**

```bash
for POD in $(kubectl -n isaac-launchable get pods --no-headers | awk '/^isaac-launchable-[0-9]/{print $1}'); do
  echo "=== $POD baseline ==="
  kubectl -n isaac-launchable exec $POD -c vscode -- bash -lc '
    pkill -KILL kit 2>/dev/null; sleep 2
    timeout 45 env ACCEPT_EULA=y /isaac-sim/runheadless.sh > /tmp/baseline.log 2>&1
    EC=$?
    pkill -KILL kit 2>/dev/null
    echo "exit=$EC crash=$(grep -c "Segmentation\|crash has occurred" /tmp/baseline.log) listen=$(ss -tunlp 2>/dev/null | grep -c -E :49100)"
    rm -f /tmp/baseline.log
  '
done
```

Expected: 두 pod 모두 `exit=124 crash=0 listen=1`.

- [ ] **Step 3: No commit (state capture only)**

If any baseline check fails, STOP — production already broken pre-Step-D. Investigate before proceeding.

---

### Task 2: Build & push volcano-vgpu-device-plugin:vulkan-v2 image with new libvgpu.so + libvgpu_vk.so

**Files:**
- Modify (volcano fork): `libvgpu` submodule SHA → `65930f4`
- Inspect/Modify (volcano fork): `docker/Dockerfile.ubuntu20.04`

- [ ] **Step 1: Inspect Dockerfile to confirm libvgpu_vk.so handling**

```bash
cd /Users/xiilab/git/volcano-vgpu-device-plugin
sed -n '30,80p' docker/Dockerfile.ubuntu20.04
```

Verify whether the Dockerfile copies BOTH `libvgpu.so` AND `libvgpu_vk.so` from the libvgpu build dir into `/k8s-vgpu/lib/nvidia/` (or wherever the postStart `cp -rf ... /usr/local/vgpu/` source path is). If only `libvgpu.so` is copied, ADD `libvgpu_vk.so` to the same COPY/cp step.

Expected: Dockerfile already runs `make build-in-docker` or equivalent inside libvgpu and ends up with `libvgpu*.so` in the final image's `/k8s-vgpu/lib/nvidia/`. If not, edit Dockerfile to add the second .so.

- [ ] **Step 2: Bump libvgpu submodule to Step C end**

```bash
cd /Users/xiilab/git/volcano-vgpu-device-plugin/libvgpu
git fetch xiilab vulkan-layer
git checkout 65930f4  # Step C 끝 (feat(vulkan): ship hami.json implicit-layer manifest)
cd ..
git add libvgpu
git status
git -c user.email=je.kim@xiilab.com -c user.name=Jea-Eok-Kim commit -s -m "build: bump libvgpu submodule to Step C end (libvgpu_vk.so split)" -m "Pulls in HAMi-core vulkan-layer 65930f4 — the Step C redesign that
splits Vulkan layer code into a separate libvgpu_vk.so. After this
bump, the device plugin image will ship both libvgpu.so (HAMi-core
only, no vk* exports) and libvgpu_vk.so (Vulkan implicit layer)
into /k8s-vgpu/lib/nvidia/, and the existing postStart cp -rf will
install both onto /usr/local/vgpu/ on each scheduled node.

Spec: HAMi-core docs/superpowers/specs/2026-04-29-step-c-redesign-vk-so-split.md
Step D plan in HAMi parent: docs/superpowers/plans/2026-04-29-step-d-vulkan-opt-in-production-activation.md"
```

- [ ] **Step 3: Build the image**

```bash
cd /Users/xiilab/git/volcano-vgpu-device-plugin
docker build -f docker/Dockerfile.ubuntu20.04 \
  -t 10.61.3.124:30002/library/volcano-vgpu-device-plugin:vulkan-v2 \
  --platform linux/amd64 \
  . 2>&1 | tail -20
```

Expected: `Successfully tagged 10.61.3.124:30002/library/volcano-vgpu-device-plugin:vulkan-v2`. No errors during the libvgpu sub-build.

If local Docker daemon isn't running, push the build to ws-node074:

```bash
rsync -az --exclude=.git/objects/pack . root@10.61.3.74:/tmp/volcano-build/
ssh root@10.61.3.74 'cd /tmp/volcano-build && docker build -f docker/Dockerfile.ubuntu20.04 -t 10.61.3.124:30002/library/volcano-vgpu-device-plugin:vulkan-v2 --platform linux/amd64 . 2>&1 | tail -20'
```

- [ ] **Step 4: Verify the image contains both .so**

```bash
docker run --rm --entrypoint /bin/sh 10.61.3.124:30002/library/volcano-vgpu-device-plugin:vulkan-v2 \
  -c 'ls -la /k8s-vgpu/lib/nvidia/ ; md5sum /k8s-vgpu/lib/nvidia/libvgpu*.so'
```

Expected: 두 .so 모두 존재 + md5 가 우리 build 와 일치 (libvgpu.so `1bd8f078`, libvgpu_vk.so `95b44957` 또는 새로 빌드된 동일한 산출물).

If on ws-node074 (no local docker):

```bash
ssh root@10.61.3.74 'docker run --rm --entrypoint /bin/sh 10.61.3.124:30002/library/volcano-vgpu-device-plugin:vulkan-v2 -c "ls -la /k8s-vgpu/lib/nvidia/ ; md5sum /k8s-vgpu/lib/nvidia/libvgpu*.so"'
```

- [ ] **Step 5: Push to local registry**

```bash
docker push 10.61.3.124:30002/library/volcano-vgpu-device-plugin:vulkan-v2 2>&1 | tail -5
# or via ssh
ssh root@10.61.3.74 'docker push 10.61.3.124:30002/library/volcano-vgpu-device-plugin:vulkan-v2 2>&1 | tail -5'
```

Expected: push 성공.

- [ ] **Step 6: Push volcano fork commit**

```bash
cd /Users/xiilab/git/volcano-vgpu-device-plugin
git remote -v   # confirm xiilab fork
git push xiilab HEAD 2>&1 | tail -3
```

---

### Task 3: Update hami-vulkan-manifest ConfigMap to point to libvgpu_vk.so

**Files:**
- Create: `cluster/runtime/snapshot-2026-04-29-step-d/hami-vulkan-manifest-cm.yaml`

- [ ] **Step 1: Create snapshot directory and copy base ConfigMap**

```bash
cd /Users/xiilab/git/HAMi
mkdir -p cluster/runtime/snapshot-2026-04-29-step-d
cp cluster/runtime/snapshot-2026-04-28/hami-vulkan-manifest-cm.yaml \
   cluster/runtime/snapshot-2026-04-29-step-d/hami-vulkan-manifest-cm.yaml
```

- [ ] **Step 2: Edit the ConfigMap data — library_path + type**

Use Edit tool to change in `cluster/runtime/snapshot-2026-04-29-step-d/hami-vulkan-manifest-cm.yaml`:

OLD `data.hami.json` value (the inline JSON):
```
"library_path": "/usr/local/vgpu/libvgpu.so"
```
NEW:
```
"library_path": "/usr/local/vgpu/libvgpu_vk.so"
```

OLD:
```
"type": "GLOBAL"
```
NEW:
```
"type": "INSTANCE"
```

Also strip the runtime metadata that doesn't apply to a fresh apply: `creationTimestamp`, `resourceVersion`, `uid`, the `last-applied-configuration` annotation. Keep `name`, `namespace`, `data`.

- [ ] **Step 3: Apply ConfigMap**

```bash
kubectl apply -f cluster/runtime/snapshot-2026-04-29-step-d/hami-vulkan-manifest-cm.yaml
kubectl get cm -n kube-system hami-vulkan-manifest -o jsonpath='{.data.hami\.json}' | python3 -m json.tool
```

Expected: parsed JSON shows `library_path` = `/usr/local/vgpu/libvgpu_vk.so` and `type` = `INSTANCE`.

- [ ] **Step 4: Commit the snapshot**

```bash
cd /Users/xiilab/git/HAMi
git add cluster/runtime/snapshot-2026-04-29-step-d/hami-vulkan-manifest-cm.yaml
git commit -s -m "chore(runtime): Step D — update hami-vulkan-manifest CM to libvgpu_vk.so" \
  -m "library_path = /usr/local/vgpu/libvgpu_vk.so (Step C split target)
type = INSTANCE (per spec; matches single-instance Vulkan layer
contract instead of the deprecated GLOBAL).

enable_environment HAMI_VULKAN_ENABLE=1 unchanged — opt-in trigger
flows through the existing webhook applyVulkanAnnotation."
```

---

### Task 4: Re-enable hami-vulkan-manifest-installer DaemonSet

**Files:**
- Create: `cluster/runtime/snapshot-2026-04-29-step-d/hami-vulkan-manifest-installer-ds.yaml`

- [ ] **Step 1: Copy base + change nodeSelector**

```bash
cp cluster/runtime/snapshot-2026-04-28/hami-vulkan-manifest-installer-ds.yaml \
   cluster/runtime/snapshot-2026-04-29-step-d/hami-vulkan-manifest-installer-ds.yaml
```

Edit `cluster/runtime/snapshot-2026-04-29-step-d/hami-vulkan-manifest-installer-ds.yaml`:

OLD:
```yaml
      nodeSelector:
        hami.io/disabled: "true"
```
NEW:
```yaml
      nodeSelector:
        nvidia.com/gpu.present: "true"
```

Also strip runtime metadata (creationTimestamp, resourceVersion, uid, status, generation, last-applied-configuration annotation).

- [ ] **Step 2: Apply DaemonSet patch**

```bash
kubectl apply -f cluster/runtime/snapshot-2026-04-29-step-d/hami-vulkan-manifest-installer-ds.yaml
```

- [ ] **Step 3: Wait for installer DS to schedule + run on GPU nodes**

```bash
kubectl rollout status ds/hami-vulkan-manifest-installer -n kube-system --timeout=120s
kubectl -n kube-system get pods -l app=hami-vulkan-manifest-installer -o wide
```

Expected: at least 1 pod scheduled (ws-node074 has `nvidia.com/gpu.present=true`).

- [ ] **Step 4: Verify manifest installed on host**

```bash
ssh root@10.61.3.74 'ls -la /usr/local/vgpu/vulkan/implicit_layer.d/ ; cat /usr/local/vgpu/vulkan/implicit_layer.d/hami.json | head -20'
```

Expected: `hami.json` exists with `library_path: /usr/local/vgpu/libvgpu_vk.so`.

- [ ] **Step 5: Post-step alive check (no annotation yet → loader still inert)**

```bash
NEWPOD=$(kubectl -n isaac-launchable get pods --no-headers | awk '/^isaac-launchable-0/{print $1}' | head -1)
kubectl -n isaac-launchable exec $NEWPOD -c vscode -- bash -lc '
pkill -KILL kit 2>/dev/null; sleep 2
timeout 45 env ACCEPT_EULA=y /isaac-sim/runheadless.sh > /tmp/post-task4.log 2>&1
EC=$?
pkill -KILL kit 2>/dev/null
echo "post-task4: exit=$EC crash=$(grep -c "Segmentation\|crash has occurred" /tmp/post-task4.log) listen=$(ss -tunlp 2>/dev/null | grep -c -E :49100)"
rm -f /tmp/post-task4.log
'
```

Expected: `exit=124 crash=0 listen=1`. (Manifest is now installed but `enable_environment` requires `HAMI_VULKAN_ENABLE=1`; without that env, the layer stays inert — should not regress baseline.) If anything else, immediately rollback installer DS to disabled state and STOP.

- [ ] **Step 6: Commit**

```bash
git add cluster/runtime/snapshot-2026-04-29-step-d/hami-vulkan-manifest-installer-ds.yaml
git commit -s -m "chore(runtime): Step D — re-enable hami-vulkan-manifest-installer DS" \
  -m "nodeSelector hami.io/disabled: true → nvidia.com/gpu.present: true.
Was disabled during the 4-27 night-patch rollback; re-enabling it here
because the Step C redesign (libvgpu_vk.so split + manifest INSTANCE
type + enable_environment gate) makes activation safe even when the
manifest is host-installed: layer stays inert until HAMI_VULKAN_ENABLE=1
flows through the webhook on a per-pod basis."
```

---

### Task 5: Bump volcano-device-plugin DaemonSet image to vulkan-v2

**Files:**
- Create: `cluster/runtime/snapshot-2026-04-29-step-d/volcano-device-plugin-ds.yaml`

- [ ] **Step 1: Copy base + bump image tag**

```bash
cp cluster/runtime/snapshot-2026-04-28/volcano-device-plugin-ds.yaml \
   cluster/runtime/snapshot-2026-04-29-step-d/volcano-device-plugin-ds.yaml
```

Edit the file: replace ALL occurrences of `volcano-vgpu-device-plugin:vulkan-v1` with `volcano-vgpu-device-plugin:vulkan-v2`. There are 2 (init container + main container) per the prior snapshot. Also strip runtime metadata.

- [ ] **Step 2: Apply DaemonSet bump**

```bash
kubectl apply -f cluster/runtime/snapshot-2026-04-29-step-d/volcano-device-plugin-ds.yaml
kubectl rollout status ds/volcano-device-plugin -n kube-system --timeout=300s
```

Expected: pods rolling, eventually `numberReady` matches `desiredNumberScheduled`.

- [ ] **Step 3: Verify host install — both .so present with new md5**

```bash
ssh root@10.61.3.74 '
md5sum /usr/local/vgpu/libvgpu.so /usr/local/vgpu/libvgpu_vk.so
ls -la /usr/local/vgpu/libvgpu*.so 2>&1
'
```

Expected: both .so present. md5 of `libvgpu.so` = `1bd8f078...` (or whatever the Step C end build produced; compare against `/tmp/libvgpu-build/build/libvgpu.so` if still around). md5 of `libvgpu_vk.so` = `95b44957...`.

- [ ] **Step 4: Post-step alive check on isaac-launchable-0 (still no annotation)**

```bash
NEWPOD=$(kubectl -n isaac-launchable get pods --no-headers | awk '/^isaac-launchable-0/{print $1}' | head -1)
kubectl -n isaac-launchable exec $NEWPOD -c vscode -- bash -lc '
pkill -KILL kit 2>/dev/null; sleep 2
timeout 45 env ACCEPT_EULA=y /isaac-sim/runheadless.sh > /tmp/post-task5.log 2>&1
EC=$?
pkill -KILL kit 2>/dev/null
echo "post-task5: exit=$EC crash=$(grep -c "Segmentation\|crash has occurred" /tmp/post-task5.log) listen=$(ss -tunlp 2>/dev/null | grep -c -E :49100)"
rm -f /tmp/post-task5.log
'
```

Expected: `exit=124 crash=0 listen=1`. (Without HAMI_VULKAN_ENABLE the layer is still inert.) If regression, immediate rollback to vulkan-v1 image.

- [ ] **Step 5: Commit**

```bash
git add cluster/runtime/snapshot-2026-04-29-step-d/volcano-device-plugin-ds.yaml
git commit -s -m "chore(runtime): Step D — bump volcano-device-plugin to vulkan-v2" \
  -m "Image vulkan-v1 → vulkan-v2. The new image ships libvgpu.so
(Step C end build, HAMi-core only) and libvgpu_vk.so (Vulkan layer)
in /k8s-vgpu/lib/nvidia/, so the existing postStart cp -rf ...
/usr/local/vgpu/ installs both onto every GPU node."
```

---

### Task 6: Annotate isaac-launchable-0 + restart + initial activation verify

**Files:** none (state changes only)

- [ ] **Step 1: Check current annotation**

```bash
NEWPOD=$(kubectl -n isaac-launchable get pods --no-headers | awk '/^isaac-launchable-0/{print $1}' | head -1)
kubectl -n isaac-launchable get pod $NEWPOD -o jsonpath='{.metadata.annotations}' | python3 -m json.tool 2>/dev/null | grep -i hami
```

If `hami.io/vulkan: "true"` already present, the deployment likely had it from prior testing; skip step 2 and go to step 3 (just delete pod to re-apply webhook).

- [ ] **Step 2: Annotate the deployment / statefulset**

```bash
# isaac-launchable-0 is likely managed by a Deployment/StatefulSet — patch the workload, not the pod
kubectl -n isaac-launchable get $(kubectl -n isaac-launchable get all -o name | grep -E "isaac-launchable-0$" | head -1) -o yaml > /tmp/isaac-0-pre.yaml
# Add hami.io/vulkan: "true" to spec.template.metadata.annotations
WORKLOAD=$(kubectl -n isaac-launchable get all -o name | grep -E "isaac-launchable-0$" | head -1)
echo "Workload: $WORKLOAD"
kubectl -n isaac-launchable patch $WORKLOAD --type=merge -p '{"spec":{"template":{"metadata":{"annotations":{"hami.io/vulkan":"true"}}}}}'
```

- [ ] **Step 3: Wait for new pod to come up**

```bash
kubectl -n isaac-launchable rollout status $WORKLOAD --timeout=300s
NEWPOD=$(kubectl -n isaac-launchable get pods --no-headers | awk '/^isaac-launchable-0/{print $1}' | head -1)
echo "New pod: $NEWPOD"
kubectl -n isaac-launchable get pod $NEWPOD -o jsonpath='{range .spec.containers[*]}{.name}: {.env[?(@.name=="HAMI_VULKAN_ENABLE")].value}{"\n"}{end}'
kubectl -n isaac-launchable get pod $NEWPOD -o jsonpath='{range .spec.containers[*]}{.name}: {.env[?(@.name=="NVIDIA_DRIVER_CAPABILITIES")].value}{"\n"}{end}'
```

Expected: `vscode: 1` for HAMI_VULKAN_ENABLE, NVIDIA_DRIVER_CAPABILITIES contains `graphics`.

- [ ] **Step 4: Verify pod healthy + alive runheadless**

```bash
kubectl -n isaac-launchable get pod $NEWPOD
kubectl -n isaac-launchable exec $NEWPOD -c vscode -- bash -lc '
pkill -KILL kit 2>/dev/null; sleep 2
timeout 50 env ACCEPT_EULA=y /isaac-sim/runheadless.sh > /tmp/active.log 2>&1
EC=$?
pkill -KILL kit 2>/dev/null
echo "active: exit=$EC crash=$(grep -c "Segmentation\|crash has occurred" /tmp/active.log) listen=$(ss -tunlp 2>/dev/null | grep -c -E :49100)"
rm -f /tmp/active.log
'
```

Expected: `exit=124 crash=0 listen=1`. If regression → rollback annotation, then if still bad rollback DS bumps too.

- [ ] **Step 5: No commit**

---

### Task 7: 4-path partition-enforcement verification

**Files:**
- Create: `cluster/runtime/snapshot-2026-04-29-step-d/4-path-verification.sh`
- Create: `cluster/runtime/snapshot-2026-04-29-step-d/vk_partition_test.py`

This task confirms partition enforce works in NVML, CUDA, Vulkan-memory-query, Vulkan-allocate.

- [ ] **Step 1: Write the python Vulkan probe**

Create `cluster/runtime/snapshot-2026-04-29-step-d/vk_partition_test.py`:

```python
#!/usr/bin/env python3
"""Step D 4-path verification — Vulkan-side partition enforce.

Path 3: vkGetPhysicalDeviceMemoryProperties → device-local heap size MUST
        be the partition limit (23552 MiB), not the raw 46068 MiB.
Path 4: vkAllocateMemory(size = 25 GiB) MUST fail with
        VK_ERROR_OUT_OF_DEVICE_MEMORY (partition limit is 23 GiB).

Requires: python3-vulkan or vulkan binding (pip install vulkan).
Run inside isaac-launchable-0 vscode container with HAMI_VULKAN_ENABLE=1
already in env.
"""
import sys
import ctypes

try:
    import vulkan as vk
except ImportError:
    print("ERR: pip install vulkan (or python3-vulkan)")
    sys.exit(2)

PARTITION_MIB = 23552  # Step C/D production limit
PARTITION_BYTES = PARTITION_MIB * 1024 * 1024
OVER_BUDGET_BYTES = 25 * 1024 * 1024 * 1024  # 25 GiB > 23 GiB

# Path 3: query memory properties
app_info = vk.VkApplicationInfo(
    sType=vk.VK_STRUCTURE_TYPE_APPLICATION_INFO,
    pApplicationName="hami-step-d-probe",
    applicationVersion=1,
    pEngineName="probe",
    engineVersion=1,
    apiVersion=vk.VK_API_VERSION_1_3,
)
inst_info = vk.VkInstanceCreateInfo(sType=vk.VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO, pApplicationInfo=app_info)
inst = vk.vkCreateInstance(inst_info, None)
phys_devs = vk.vkEnumeratePhysicalDevices(inst)
if not phys_devs:
    print("ERR: no physical devices")
    sys.exit(2)
dev = phys_devs[0]
mem_props = vk.vkGetPhysicalDeviceMemoryProperties(dev)

device_local_heap_size = 0
for i in range(mem_props.memoryHeapCount):
    heap = mem_props.memoryHeaps[i]
    if heap.flags & vk.VK_MEMORY_HEAP_DEVICE_LOCAL_BIT:
        device_local_heap_size = max(device_local_heap_size, heap.size)
print(f"Path 3: device-local heap size = {device_local_heap_size} bytes ({device_local_heap_size // (1024*1024)} MiB)")
if abs(device_local_heap_size - PARTITION_BYTES) < (256 * 1024 * 1024):  # 256 MiB tolerance
    print(f"Path 3: PASS (within 256 MiB of {PARTITION_MIB} MiB partition)")
else:
    print(f"Path 3: FAIL (expected ~{PARTITION_MIB} MiB, got {device_local_heap_size // (1024*1024)} MiB)")

# Path 4: try to allocate over-budget
device_create_info = vk.VkDeviceCreateInfo(
    sType=vk.VK_STRUCTURE_TYPE_DEVICE_CREATE_INFO,
    queueCreateInfoCount=1,
    pQueueCreateInfos=[vk.VkDeviceQueueCreateInfo(
        sType=vk.VK_STRUCTURE_TYPE_DEVICE_QUEUE_CREATE_INFO,
        queueFamilyIndex=0,
        queueCount=1,
        pQueuePriorities=[1.0],
    )],
)
ldev = vk.vkCreateDevice(dev, device_create_info, None)
mem_type_idx = -1
for i in range(mem_props.memoryTypeCount):
    if mem_props.memoryTypes[i].propertyFlags & vk.VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT:
        mem_type_idx = i
        break
alloc_info = vk.VkMemoryAllocateInfo(
    sType=vk.VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO,
    allocationSize=OVER_BUDGET_BYTES,
    memoryTypeIndex=mem_type_idx,
)
try:
    mem = vk.vkAllocateMemory(ldev, alloc_info, None)
    print(f"Path 4: FAIL (expected VK_ERROR_OUT_OF_DEVICE_MEMORY for {OVER_BUDGET_BYTES} bytes, got success — partition not enforced)")
    vk.vkFreeMemory(ldev, mem, None)
except vk.VkErrorOutOfDeviceMemory:
    print(f"Path 4: PASS (VK_ERROR_OUT_OF_DEVICE_MEMORY for {OVER_BUDGET_BYTES // (1024*1024*1024)} GiB > {PARTITION_MIB // 1024} GiB partition)")
except Exception as e:
    print(f"Path 4: FAIL (unexpected error {type(e).__name__}: {e})")

vk.vkDestroyDevice(ldev, None)
vk.vkDestroyInstance(inst, None)
```

- [ ] **Step 2: Write the orchestrator script**

Create `cluster/runtime/snapshot-2026-04-29-step-d/4-path-verification.sh`:

```bash
#!/bin/bash
# Step D 4-path verification orchestrator.
# Run from controller host; orchestrates 4-path checks inside isaac-launchable-0.
set -u

NS=isaac-launchable
POD=$(kubectl -n $NS get pods --no-headers | awk '/^isaac-launchable-0/{print $1}' | head -1)
if [ -z "$POD" ]; then
    echo "ERR: isaac-launchable-0 pod not found"; exit 1
fi
echo "Pod: $POD"

# Copy the python probe into the pod
kubectl -n $NS cp "$(dirname "$0")/vk_partition_test.py" $POD:/tmp/vk_partition_test.py -c vscode

PASS=0
FAIL=0

echo
echo "=== Path 1: NVML hook (nvidia-smi clamp) ==="
RAW=$(kubectl -n $NS exec $POD -c vscode -- bash -lc 'env -u LD_PRELOAD nvidia-smi --query-gpu=memory.total --format=csv,noheader' 2>&1 | head -1)
HOOKED=$(kubectl -n $NS exec $POD -c vscode -- bash -lc 'nvidia-smi --query-gpu=memory.total --format=csv,noheader' 2>&1 | grep -E "MiB" | head -1)
echo "  raw  = $RAW"
echo "  hook = $HOOKED"
if echo "$HOOKED" | grep -qE "23552 MiB"; then
    echo "  Path 1: PASS"; PASS=$((PASS+1))
else
    echo "  Path 1: FAIL"; FAIL=$((FAIL+1))
fi

echo
echo "=== Path 2: CUDA driver hook (cuMemGetInfo clamp) ==="
P2=$(kubectl -n $NS exec $POD -c vscode -- bash -lc '
python3 -c "
import sys
try:
    import pycuda.driver as cuda
    cuda.init()
    ctx = cuda.Device(0).make_context()
    free, total = cuda.mem_get_info()
    print(f\"free={free} total={total}\")
    ctx.pop()
except ImportError:
    sys.exit(2)
except Exception as e:
    print(f\"err: {e}\")
" 2>&1' || echo "ERR")
echo "  $P2"
TOTAL_MIB=$(echo "$P2" | sed -nE "s/.*total=([0-9]+).*/\1/p" | awk "{print int(\$1/(1024*1024))}")
if [ "$TOTAL_MIB" = "23552" ] || [ "$TOTAL_MIB" -ge "23000" -a "$TOTAL_MIB" -le "24000" ]; then
    echo "  Path 2: PASS (~$TOTAL_MIB MiB)"; PASS=$((PASS+1))
else
    echo "  Path 2: SKIP_OR_FAIL (no pycuda or unexpected total=$TOTAL_MIB)"; FAIL=$((FAIL+1))
fi

echo
echo "=== Paths 3 & 4: Vulkan memory query + allocate ==="
P34=$(kubectl -n $NS exec $POD -c vscode -- bash -lc '
if ! python3 -c "import vulkan" 2>/dev/null; then
    /isaac-sim/python.sh -m pip install vulkan 2>&1 | tail -3
fi
/isaac-sim/python.sh /tmp/vk_partition_test.py 2>&1
')
echo "$P34"
echo "$P34" | grep -q "Path 3: PASS" && PASS=$((PASS+1)) || FAIL=$((FAIL+1))
echo "$P34" | grep -q "Path 4: PASS" && PASS=$((PASS+1)) || FAIL=$((FAIL+1))

echo
echo "=== Summary ==="
echo "PASS=$PASS FAIL=$FAIL of 4 paths"
[ "$FAIL" = "0" ] && exit 0 || exit 1
```

- [ ] **Step 3: chmod + run**

```bash
chmod +x cluster/runtime/snapshot-2026-04-29-step-d/4-path-verification.sh
./cluster/runtime/snapshot-2026-04-29-step-d/4-path-verification.sh
```

Expected: `PASS=4 FAIL=0 of 4 paths`. If any path fails, capture the output and STOP for analysis. Do not roll back automatically — the underlying issue may be a code bug, not a deployment issue.

- [ ] **Step 4: Commit verification scripts**

```bash
git add cluster/runtime/snapshot-2026-04-29-step-d/4-path-verification.sh \
        cluster/runtime/snapshot-2026-04-29-step-d/vk_partition_test.py
git commit -s -m "test(runtime): Step D — 4-path partition enforce verification scripts" \
  -m "Run on ws-node074 against isaac-launchable-0 with hami.io/vulkan
annotation active. Verifies:

Path 1: NVML hook nvidia-smi → 23552 MiB clamp
Path 2: CUDA driver hook cuMemGetInfo → ~23 GiB total
Path 3: Vulkan vkGetPhysicalDeviceMemoryProperties → device-local heap
        ~23 GiB
Path 4: Vulkan vkAllocateMemory(25 GiB) → VK_ERROR_OUT_OF_DEVICE_MEMORY

Skip path 2 if pycuda unavailable in pod (informational FAIL — not
blocker, NVML+CUDA hooks already validated by Step B unit tests)."
```

---

### Task 8: HAMI_VK_TRACE host-loader verification + sanity check other Vulkan pods

**Files:** none (verification only)

- [ ] **Step 1: HAMI_VK_TRACE host-loader probe**

Run a small Vulkan probe via host system Vulkan loader (NOT Kit's Conan-bundled loader) to confirm our layer is in chain:

```bash
NEWPOD=$(kubectl -n isaac-launchable get pods --no-headers | awk '/^isaac-launchable-0/{print $1}' | head -1)
kubectl -n isaac-launchable exec $NEWPOD -c vscode -- bash -lc '
which vulkaninfo || apt list --installed 2>/dev/null | grep -i vulkan-tools
HAMI_VK_TRACE=1 vulkaninfo --summary 2>&1 | head -20 || echo "vulkaninfo unavailable"
echo
echo "=== HAMI_VK_TRACE lines via /isaac-sim python ==="
HAMI_VK_TRACE=1 /isaac-sim/python.sh -c "
import vulkan as vk
app = vk.VkApplicationInfo(sType=vk.VK_STRUCTURE_TYPE_APPLICATION_INFO, apiVersion=vk.VK_API_VERSION_1_3)
ci = vk.VkInstanceCreateInfo(sType=vk.VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO, pApplicationInfo=app)
inst = vk.vkCreateInstance(ci, None)
print(\"created instance\")
vk.vkDestroyInstance(inst, None)
" 2>&1 | grep -E "HAMI_VK_TRACE|created" | head -20
'
```

Expected: HAMI_VK_TRACE lines > 0 — at least the `vkGetInstanceProcAddr` lookups for each entry point during `vkCreateInstance`. This proves the layer is in the chain when activation conditions are met (manifest installed + HAMI_VULKAN_ENABLE=1 + python uses host's libvulkan, not Kit's Conan-bundled one).

If trace=0 even here, capture full log and surface to controller — manifest activation is broken at the loader level.

- [ ] **Step 2: Sanity check other Vulkan-using pods**

```bash
echo "=== isaac-launchable-1 ==="
POD1=$(kubectl -n isaac-launchable get pods --no-headers | awk '/^isaac-launchable-1/{print $1}' | head -1)
kubectl -n isaac-launchable exec $POD1 -c vscode -- bash -lc '
pkill -KILL kit 2>/dev/null; sleep 2
timeout 45 env ACCEPT_EULA=y /isaac-sim/runheadless.sh > /tmp/p1.log 2>&1
EC=$?; pkill -KILL kit 2>/dev/null
echo "isaac-launchable-1: exit=$EC crash=$(grep -c "Segmentation\|crash has occurred" /tmp/p1.log) listen=$(ss -tunlp 2>/dev/null | grep -c -E :49100)"
rm -f /tmp/p1.log
'
echo
echo "=== usd-composer ==="
POD2=$(kubectl -n isaac-launchable get pods --no-headers | awk '/^usd-composer/{print $1}' | head -1)
[ -n "$POD2" ] && kubectl -n isaac-launchable get pod $POD2
echo
echo "=== other isaac-launchable namespace pods status ==="
kubectl -n isaac-launchable get pods
```

Expected:
- isaac-launchable-1: `exit=124 crash=0 listen=1` (no annotation → still inert; should be unaffected by Step D changes).
- usd-composer: `3/3 Running`, no crash loop.
- All other pods steady.

If isaac-launchable-1 regresses despite NOT having the annotation, that means the manifest is being activated globally somehow — the `enable_environment` gate is broken or the webhook is leaking annotation cross-pod. Investigate.

- [ ] **Step 3: No commit**

---

### Task 9: Push snapshot YAMLs + draft PR comments (DO NOT post)

**Files:**
- Create: `/tmp/step-d-pr-drafts/{pr-hami,pr-volcano-fork}.md`

- [ ] **Step 1: Push parent HAMi commits**

```bash
cd /Users/xiilab/git/HAMi
git log --oneline xiilab/feat/vulkan-vgpu..HEAD 2>&1 | head -5
git push xiilab feat/vulkan-vgpu 2>&1 | tail -3
```

- [ ] **Step 2: Draft PR comments**

```bash
mkdir -p /tmp/step-d-pr-drafts

cat > /tmp/step-d-pr-drafts/pr-hami.md <<'EOF'
## Step D — Vulkan opt-in production activation + 4-path 검증

Step C 의 `libvgpu_vk.so` 분리 산출물을 production opt-in path 에서 활성화하고, partition enforce 가 4 path 모두에서 작동함을 ws-node074 에서 검증.

### Commits

- `chore(runtime): Step D — update hami-vulkan-manifest CM to libvgpu_vk.so`
- `chore(runtime): Step D — re-enable hami-vulkan-manifest-installer DS`
- `chore(runtime): Step D — bump volcano-device-plugin to vulkan-v2`
- `test(runtime): Step D — 4-path partition enforce verification scripts`

### Verification on ws-node074, isaac-launchable-0 (with `hami.io/vulkan: "true"` annotation)

| Path | Expected | Actual |
|---|---|---|
| 1. NVML `nvidia-smi` | 23552 MiB | (fill from script run) |
| 2. CUDA `cuMemGetInfo` | ~23 GiB | (fill) |
| 3. Vulkan `vkGetPhysicalDeviceMemoryProperties` device-local heap | ~23 GiB | (fill) |
| 4. Vulkan `vkAllocateMemory(25 GiB)` | `VK_ERROR_OUT_OF_DEVICE_MEMORY` | (fill) |

`HAMI_VK_TRACE > 0` confirmed via host vulkan-loader path on python3-vulkan probe.

### Companion changes
- volcano-vgpu-device-plugin fork: libvgpu submodule bumped to HAMi-core `65930f4` (Step C end). Image rebuilt and pushed as `vulkan-v2` to local registry.

### Rollback path (if needed)
- DaemonSet `hami-vulkan-manifest-installer`: nodeSelector → `hami.io/disabled: "true"` (kubectl patch).
- DaemonSet `volcano-device-plugin`: image → `vulkan-v1`.
- Annotation `hami.io/vulkan` → remove from workload.

Spec: `docs/superpowers/specs/2026-04-29-step-d-vulkan-opt-in-production-activation.md`
Plan: `docs/superpowers/plans/2026-04-29-step-d-vulkan-opt-in-production-activation.md`
EOF

cat > /tmp/step-d-pr-drafts/pr-volcano-fork.md <<'EOF'
## bump libvgpu submodule to HAMi-core Step C end (libvgpu_vk.so split)

Pulls in HAMi-core `vulkan-layer` `65930f4` — the Step C redesign that splits Vulkan layer code into a separate `libvgpu_vk.so`. After this bump:

- `libvgpu.so` (HAMi-core only, no `vk*` exports) and `libvgpu_vk.so` (Vulkan implicit layer) are both shipped in `/k8s-vgpu/lib/nvidia/`.
- The existing postStart `cp -rf /k8s-vgpu/lib/nvidia/. /usr/local/vgpu/` installs both onto every GPU node.
- Image tag bump: `vulkan-v1` → `vulkan-v2`.

Verification done in HAMi parent Step D plan; partition enforce confirmed across NVML, CUDA, Vulkan-memory-query, Vulkan-allocate paths on ws-node074 isaac-launchable-0.

Submodule SHA: `65930f4` (commit "feat(vulkan): ship hami.json implicit-layer manifest").
EOF

ls -la /tmp/step-d-pr-drafts/
```

- [ ] **Step 3: Report — DO NOT post comments. Wait for explicit user approval.**

---

## Self-Review

**1. Spec coverage:**
- Spec §"핵심 결정 1" (image rebuild) → Task 2
- Spec §"핵심 결정 2" (CM update) → Task 3
- Spec §"핵심 결정 3" (installer DS 재활성) → Task 4
- Spec §"핵심 결정 4" (annotation/webhook) → Task 6
- Spec §"핵심 결정 5" (4-path verification) → Task 7
- Spec §"핵심 결정 6" (rollback) → 각 Task post-step alive 체크 + restore 가이드
- Spec §"Activation flow" → Tasks 3-6 순서대로
- Spec §"4-path verification" → Task 7
- Spec §"Production safety gate" → Tasks 1, 4-5 의 post-step 검증 + Task 8 의 sanity ✅

**2. Placeholder scan:** Task 7 의 (fill from script run) 자리는 PR draft 의 verification table 이고, 실행 후 채워질 자리이지 plan 자체의 결함이 아님. 그 외 placeholder 없음. ✅

**3. Type consistency:** `hami.io/vulkan` annotation 이름 / `HAMI_VULKAN_ENABLE` env 이름 / `library_path` JSON key — 모든 task 에서 일관 사용. PARTITION_MIB=23552 / OVER_BUDGET_BYTES=25 GiB — vk_partition_test.py 와 4-path-verification.sh 가 동일 값 사용. ✅

**4. Scope check:** 단일 production deploy + 검증. helm chart 통합 / Tasks 1+2 재도입 / multi-GPU 는 out of scope (spec 명시). 단일 plan 으로 실행 가능. ✅

**5. External-repo dependency**: Task 2 가 `volcano-vgpu-device-plugin` fork 작업 (HAMi parent repo 외). Plan 에 명시적으로 working dir 구분, git push 도 fork 만. 이 task 는 controller 가 외부 repo permissions / SSH 가 보장되는 환경에서 실행해야 함. 안 되면 BLOCKED 보고. ✅

---

## Estimated time

| Task | 예상 |
|---|---|
| 1 inventory + baseline | 10분 |
| 2 image build + push (외부 repo, libvgpu submodule bump 포함) | 60분 |
| 3 CM update + apply | 15분 |
| 4 installer DS 재활성 | 15분 |
| 5 device plugin DS bump | 20분 |
| 6 annotation + restart + verify | 20분 |
| 7 4-path verification scripts + run | 45분 |
| 8 trace host-loader + sanity | 20분 |
| 9 push + PR drafts | 15분 |
| **총** | **약 3.5시간** |

(Task 2 가 가장 변동성 큼 — image build 인프라/네트워크 의존도 높음.)
