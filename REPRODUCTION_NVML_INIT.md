# Reproduction: NVML Init Panic

## Issue
The device plugin crashes with `panic(0)` when NVML initialization fails.

## Location
`pkg/device-plugin/nvidiadevice/nvinternal/plugin/register.go:97`

```go
if nvret := nvmlInit(); nvret != nvml.SUCCESS {
    klog.Errorln("nvml Init err: ", nvret)
    panic(0)  // ← CRASH HERE
}
```

## How to Reproduce

### Method 1: Unload NVIDIA Driver
```bash
# On the node where device plugin runs
sudo rmmod nvidia_uvm
sudo rmmod nvidia

# Device plugin will crash on next registration cycle
kubectl logs -n kube-system nvidia-device-plugin-xxx
# Expected: panic(0) crash
```

### Method 2: Permission Issues
```bash
# Restrict /dev/nvidiactl permissions
sudo chmod 000 /dev/nvidiactl

# Device plugin will fail NVML init
kubectl logs -n kube-system nvidia-device-plugin-xxx
# Expected: panic(0) crash
```

### Method 3: Simulated Failure (Test)
```go
// In test file
func TestGetAPIDevices_NVMLInitFailure(t *testing.T) {
    oldInit := nvmlInit
    defer func() { nvmlInit = oldInit }()
    
    // Simulate NVML init failure
    nvmlInit = func() nvml.Return {
        return nvml.ERROR_UNKNOWN
    }
    
    plugin := &NvidiaDevicePlugin{...}
    
    // This currently panics - should return empty list instead
    devices := plugin.getAPIDevices()
    assert.NotNil(t, devices)
    assert.Empty(t, *devices)
}
```

## Impact

**Before Fix:**
- Single NVML failure → Entire device plugin crashes
- ALL GPUs on node become unavailable
- Requires manual pod restart
- MTTR: 5-30 minutes

**After Fix:**
- NVML failure → Returns empty device list
- Plugin stays running
- Retries on next cycle (30s)
- Self-healing

## Current Behavior
```
E0807 12:00:00.123456   12345 register.go:97] nvml Init err:  6
panic: 0

goroutine 1 [running]:
github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/plugin.(*NvidiaDevicePlugin).getAPIDevices(...)
    /workspace/pkg/device-plugin/nvidiadevice/nvinternal/plugin/register.go:97
...
```

## Expected Behavior After Fix
```
E0807 12:00:00.123456   12345 register.go:95] Failed to initialize NVML, returning empty device list for graceful degradation error="nvml init failed: Initialization Failed" returnCode=6
I0807 12:00:00.789012   12345 register.go:210] Registered 0 device(s)
I0807 12:00:35.123456   12345 register.go:95] Retrying device registration...
```

## Related Issues
- #982 - Added error handling for nvml.Init but didn't remove panic(0)
- #2043 - Fixed CheckHealth nil deref panic (different function)
- #1964 - Fixed CheckHealth timestamp panic (different function)

## Why This Wasn't Fixed Before
PR #982 added *additional* error handling around NVML operations but kept the 
panic(0) calls as a "fail-fast" approach. However, in production, graceful 
degradation is preferred over total failure.
