#!/usr/bin/env python3
"""Step D 4-path verification — Vulkan-side partition enforce.

Path 3: vkGetPhysicalDeviceMemoryProperties → device-local heap size
        MUST be the partition limit (~23 GiB), not the raw 46 GiB.
Path 4: vkAllocateMemory(size = 25 GiB) MUST fail with
        VK_ERROR_OUT_OF_DEVICE_MEMORY (partition limit is ~23 GiB).

Run inside isaac-launchable-0 vscode container (annotation
hami.io/vulkan: "true" + webhook-injected manifest + libvgpu_vk.so).
"""
import sys

PARTITION_MIB = 23552
PARTITION_BYTES = PARTITION_MIB * 1024 * 1024
TOLERANCE_MIB = 256
OVER_BUDGET_BYTES = 25 * 1024 * 1024 * 1024

try:
    import vulkan as vk
except ImportError:
    print("ERR: pip install vulkan")
    sys.exit(2)

API_1_3 = (1 << 22) | (3 << 12)


def main():
    app = vk.VkApplicationInfo(
        sType=vk.VK_STRUCTURE_TYPE_APPLICATION_INFO,
        pApplicationName="hami-step-d-probe",
        applicationVersion=1,
        pEngineName="probe",
        engineVersion=1,
        apiVersion=API_1_3,
    )
    ci = vk.VkInstanceCreateInfo(
        sType=vk.VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO,
        pApplicationInfo=app,
    )
    inst = vk.vkCreateInstance(ci, None)
    phys_devs = vk.vkEnumeratePhysicalDevices(inst)
    if not phys_devs:
        print("ERR: no physical devices")
        sys.exit(2)
    dev = phys_devs[0]
    mem_props = vk.vkGetPhysicalDeviceMemoryProperties(dev)

    # Path 3
    device_local_heap_size = 0
    for i in range(mem_props.memoryHeapCount):
        heap = mem_props.memoryHeaps[i]
        if heap.flags & vk.VK_MEMORY_HEAP_DEVICE_LOCAL_BIT:
            if heap.size > device_local_heap_size:
                device_local_heap_size = heap.size
    p3_mib = device_local_heap_size // (1024 * 1024)
    print(f"Path 3: device-local heap = {device_local_heap_size} bytes ({p3_mib} MiB)")
    if abs(p3_mib - PARTITION_MIB) <= TOLERANCE_MIB:
        print(f"Path 3: PASS (within {TOLERANCE_MIB} MiB of {PARTITION_MIB} MiB partition)")
        path3_ok = True
    else:
        print(f"Path 3: FAIL (expected ~{PARTITION_MIB} MiB, got {p3_mib} MiB)")
        path3_ok = False

    # Path 4
    queue_create = vk.VkDeviceQueueCreateInfo(
        sType=vk.VK_STRUCTURE_TYPE_DEVICE_QUEUE_CREATE_INFO,
        queueFamilyIndex=0,
        queueCount=1,
        pQueuePriorities=[1.0],
    )
    device_create = vk.VkDeviceCreateInfo(
        sType=vk.VK_STRUCTURE_TYPE_DEVICE_CREATE_INFO,
        queueCreateInfoCount=1,
        pQueueCreateInfos=[queue_create],
    )
    ldev = vk.vkCreateDevice(dev, device_create, None)
    mem_type_idx = -1
    for i in range(mem_props.memoryTypeCount):
        if mem_props.memoryTypes[i].propertyFlags & vk.VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT:
            mem_type_idx = i
            break
    if mem_type_idx < 0:
        print("Path 4: SKIP (no device-local memory type)")
        path4_ok = False
    else:
        alloc_info = vk.VkMemoryAllocateInfo(
            sType=vk.VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO,
            allocationSize=OVER_BUDGET_BYTES,
            memoryTypeIndex=mem_type_idx,
        )
        path4_ok = False
        try:
            mem = vk.vkAllocateMemory(ldev, alloc_info, None)
            print(f"Path 4: FAIL (expected OOM for {OVER_BUDGET_BYTES // (1024**3)} GiB, got success — partition not enforced)")
            vk.vkFreeMemory(ldev, mem, None)
        except vk.VkErrorOutOfDeviceMemory:
            print(f"Path 4: PASS (VK_ERROR_OUT_OF_DEVICE_MEMORY for {OVER_BUDGET_BYTES // (1024**3)} GiB > {PARTITION_MIB // 1024} GiB partition)")
            path4_ok = True
        except Exception as e:
            print(f"Path 4: FAIL (unexpected error {type(e).__name__}: {e})")
    vk.vkDestroyDevice(ldev, None)
    vk.vkDestroyInstance(inst, None)

    return 0 if (path3_ok and path4_ok) else 1


if __name__ == "__main__":
    sys.exit(main())
