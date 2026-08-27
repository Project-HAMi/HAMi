# NVIDIA GPU memory oversubscription

HAMi can advertise more schedulable GPU memory than a physical NVIDIA GPU has
by setting `devicePlugin.deviceMemoryScaling` to a value greater than `1`.
For example, a scaling value of `2` makes a 24 GiB GPU advertise approximately
48 GiB to the HAMi scheduler. This setting changes scheduling capacity; it does
not add physical VRAM.

## Behavior when physical VRAM is full

For ordinary CUDA device allocations, HAMi continues to enforce each
container's configured memory limit, but the NVIDIA driver must also satisfy
the allocation from the physical GPU. An allocation can therefore be within
its HAMi limit and still fail with `CUDA_ERROR_OUT_OF_MEMORY` when workloads on
the same GPU have exhausted physical VRAM. CUDA Runtime API calls such as
`cudaMalloc` expose the corresponding `cudaErrorMemoryAllocation` error.

The failed allocation is returned to the process that requested it. Existing
allocations are not automatically evicted by HAMi. Applications must handle
CUDA allocation failures correctly; they may exit, retry, or become unhealthy
depending on their own error handling. Operators should monitor all workloads
sharing the GPU because memory pressure and allocation retry behavior can
still affect latency and availability.

`deviceMemoryScaling` does not enable automatic spillover to host memory.
CUDA Unified Memory is a separate CUDA and hardware capability with different
performance and residency behavior. Do not assume that a failed `cudaMalloc`
will fall back to host memory.

## Recommendations

- Keep the default value `1` when predictable admission is more important than
  utilization.
- Enable scaling only for workloads whose peak memory use is understood and
  which handle CUDA out-of-memory errors safely.
- Leave physical headroom for CUDA contexts, libraries, and workload spikes.
- Avoid combining unrelated latency-sensitive workloads on a heavily
  oversubscribed GPU.
- Validate the exact GPU model, driver, CUDA version, and workload before using
  oversubscription in production.
- Do not treat HAMi's in-container memory control as a security boundary
  against a malicious workload.

## Hardware-backed validation

The opt-in test below creates two Pods on the same GPU. The first Pod allocates
and continuously touches memory. The second attempts an allocation that makes
their combined physical use exceed VRAM. The test requires the second Pod to
receive `CUDA_ERROR_OUT_OF_MEMORY` and then verifies that the first Pod keeps
making CUDA progress. The included probe uses the CUDA Runtime API and checks
for `cudaErrorMemoryAllocation`.

Configure HAMi with sufficient schedulable capacity first, for example:

```yaml
devicePlugin:
  deviceMemoryScaling: 2
```

Run the test on an otherwise idle GPU node:

```bash
TARGET_NODE=gpu-worker-1 \
OVERSUB_ARTIFACT_DIR=/tmp/hami-oversub-results \
bash hack/hami-memory-oversubscription-e2e.sh
```

On a multi-GPU node, select one complete physical UUID:

```bash
TARGET_NODE=gpu-worker-1 \
TARGET_GPU_UUID=GPU-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx \
OVERSUB_ARTIFACT_DIR=/tmp/hami-oversub-results \
bash hack/hami-memory-oversubscription-e2e.sh
```

The artifact directory records the GPU model and memory, driver version,
Kubernetes version, HAMi chart information, scaling value, Pod state,
`nvidia-smi`, workload logs, and device-plugin logs. The default scenario has
each Pod request 60% of physical memory and touch 90% of that request. These
percentages can be adjusted using `OVERSUB_REQUEST_PERCENT` and
`OVERSUB_ALLOCATE_PERCENT`, provided that the combined allocation exceeds
physical VRAM and the configured scaling can schedule both requests.

This test covers ordinary CUDA device memory allocated with `cudaMalloc`. It
does not characterize CUDA Unified Memory, GPU reset behavior, malicious
workloads, or every application's response to an allocation error. Repeat the
test for every production GPU and driver combination that you intend to
support.
