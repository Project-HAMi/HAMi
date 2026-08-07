# Reproduction: NVML Init Panic Fix

## Issue (Before This Fix)
The device plugin crashed with `panic(0)` when NVML initialization failed.

## Solution (After This Fix)
The device plugin now returns an empty device list and retries on the next cycle.

## Location
`pkg/device-plugin/nvidiadevice/nvinternal/plugin/register.go:96-100`

**Before:**
```go
if nvret := nvmlInit(); nvret != nvml.SUCCESS {
    klog.Errorln("nvml Init err: ", nvret)
    panic(0)  // ← CRASH
}
```

**After:**
```go
if nvret := nvmlInit(); nvret != nvml.SUCCESS {
    klog.ErrorS(fmt.Errorf("nvml init failed: %s", nvml.ErrorString(nvret)), 
        "Failed to initialize NVML, returning empty device list for graceful degradation", 
        "returnCode", nvret)
    emptyRes := make([]*device.DeviceInfo, 0)
    return &emptyRes  // ← GRACEFUL DEGRADATION
}
```

## How to Reproduce the Original Issue

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

### Behavior Before Fix
- Single NVML failure → Entire device plugin crashes
- ALL GPUs on node become unavailable
- Requires manual pod restart
- MTTR: 5-30 minutes

### Behavior After Fix
- NVML failure → Returns empty device list
- Plugin stays running
- Retries on next registration cycle:
  - Success: 30 seconds wait
  - Annotation patch failure: 5 seconds wait
- Self-healing when driver is fixed

## Current Behavior (After This Fix)
```text
# When NVML init fails
E0807 12:00:00.123456   12345 register.go:96] Failed to initialize NVML, returning empty device list for graceful degradation error="nvml init failed: Initialization Failed" returnCode=6
I0807 12:00:00.789012   12345 register.go:217] Discovered 0 device(s) for registration
I0807 12:00:00.789123   12345 register.go:254] Updating node annotations with 0 device(s)

# On successful registration (even with 0 devices), waits 30 seconds
I0807 12:00:30.123456   12345 register.go:284] Successfully updated node annotation. Next check in 30s...

# If annotation patch fails, retries after 5 seconds
E0807 12:00:00.999999   12345 register.go:281] Failed to register annotation: connection refused. Retrying in 5s...
```

## Expected Behavior (Self-Healing)
```text
# Cycle 1: NVML init fails, annotation succeeds, wait 30s
E0807 12:00:00.123456   12345 register.go:96] Failed to initialize NVML, returning empty device list for graceful degradation error="nvml init failed: Driver Not Loaded" returnCode=29
I0807 12:00:00.789012   12345 register.go:217] Discovered 0 device(s) for registration
I0807 12:00:00.999999   12345 register.go:284] Successfully updated node annotation. Next check in 30s...

# Cycle 2 (after 30s): Driver loaded, NVML succeeds
I0807 12:00:30.123456   12345 register.go:217] Discovered 4 device(s) for registration
I0807 12:00:30.456789   12345 register.go:254] Updating node annotations with 4 device(s)
I0807 12:00:30.789012   12345 register.go:284] Successfully updated node annotation. Next check in 30s...
```

## Related Issues
- #982 - Added error handling for nvml.Init but didn't remove panic(0)
- #2043 - Fixed CheckHealth nil deref panic (different function)
- #1964 - Fixed CheckHealth timestamp panic (different function)

## Why This Wasn't Fixed Before
PR #982 added *additional* error handling around NVML operations but kept the 
panic(0) calls as a "fail-fast" approach. However, in production, graceful 
degradation is preferred over total failure.
