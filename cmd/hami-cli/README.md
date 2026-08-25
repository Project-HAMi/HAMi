# hami-cli

A small, read-only inspection tool for HAMi vGPU device allocations.

HAMi's scheduler writes device allocation decisions into pod and node
annotations under the `hami.io/` prefix (see
[docs/develop/protocol.md](../../docs/develop/protocol.md)). `hami-cli`
decodes those existing annotations and prints them as a table, so you don't
have to hand-decode `kubectl get pod -o yaml` output to answer "which pod
holds which GPU slice on which node".

It is a pure reader of cluster state: it makes no scheduler-side changes and
requires no GPU hardware to build, test, or run against a cluster that has
HAMi-managed pods.

## Installation

Build from source with the rest of HAMi's binaries:

```bash
make build
# binary is written to bin/hami-cli
```

Or build just this binary directly:

```bash
go build -o bin/hami-cli ./cmd/hami-cli
```

## Usage

`hami-cli` uses the same kubeconfig resolution as `kubectl`: it reads
`$KUBECONFIG` if set, falls back to `~/.kube/config`, and falls back to
in-cluster configuration when running inside a pod.

List every HAMi device allocation in the cluster:

```bash
hami-cli get allocations
```

```text
NODE       NAMESPACE   POD          CONTAINER   DEVICE TYPE   DEVICE UUID                             MEMORY   CORE
node67-4v100  default   train-job    trainer     NVIDIA        GPU-0fc3eda5-e98b-a25b-5b0d-cf5c855d1448   3000    0
```

Filter by node or namespace:

```bash
hami-cli get allocations --node node67-4v100
hami-cli get allocations --namespace ml-team
```

## Vendor coverage

`hami-cli` does not hardcode a list of supported vendors. Every HAMi device
backend writes two annotations per pod with identical values at scheduler
bind time: `hami.io/<slug>-devices-to-allocate` (a pending work queue that
the device plugin's `Allocate()` erases entry-by-entry as each container
starts) and `hami.io/<slug>-allocated` (the stable record, never erased).
`hami-cli` matches the `-allocated` annotation, since it is the one that
still holds a value once a pod is fully running, and decodes it with the
same `device.DecodePodDevices` function the scheduler itself uses. This
means allocations from every currently-supported vendor (NVIDIA, Cambricon,
Ascend, AMD, Hygon, Iluvatar, Kunlun, Metax, Mthreads, Biren, Enflame,
AWS Neuron, Vast.ai) are decoded uniformly, including vendors whose
annotation key is only known at runtime via chart configuration (Ascend,
Iluvatar) and Kunlun, whose key (`hami.io/kunlun-allocated`) omits the
otherwise-common `-devices-` segment.

If a pod's HAMi annotations are malformed, `hami-cli` prints a warning to
stderr and skips that pod rather than failing the whole command.

## Limitations

- Shows what the scheduler *requested/recorded* for each container, not the
  device's live runtime utilization (see `vGPUmonitor`'s Prometheus metrics
  for that).
- Node-side device capacity/registration (`hami.io/node-*-register`) is not
  yet cross-referenced against pod allocations in the table output.
