# Host PID broker

The feature is disabled by default.

## Purpose

HAMi-core needs the host PID of each CUDA process because NVML reports processes in the host PID namespace. The current fallback discovers that PID by creating a CUDA primary context while holding the post init lock. That work becomes serial when many processes call `cuInit()` together.

The host PID broker returns the caller's own host PID from Linux `SO_PEERCRED`. It runs inside the NVIDIA device plugin, whose pod already uses the host PID namespace. It does not read host procfs and does not accept a PID supplied by the client.

## Requirements

1. The NVIDIA device plugin must run on Linux as root.

2. `devicePlugin.hostPID` must remain `true`.

3. The workload must use a HAMi-core build that supports protocol version 1 and the `LIBVGPU_HOSTPID_BROKER` gate.

4. The container runtime must support the read-only nested bind mount used for `/tmp/vgpulock/hostpid`.

5. The shared `/tmp/vgpulock` parent must be owned by root and use mode `01777`. The sticky bit allows legacy lock creation while preventing an ordinary workload user from replacing an entry owned by another user.

## Enablement

Set the chart value below:

```yaml
devicePlugin:
  hostPID: true
  hostPIDBroker:
    enabled: true
```

The chart rejects a configuration that enables the broker while disabling the device plugin host PID namespace.

When enabled, the chart does four things:

1. It sets `LIBVGPU_HOSTPID_BROKER=1` in the device plugin.

2. It mounts the host directory `/var/run/hami/hostpid` into the device plugin.

3. The device plugin creates `/var/run/hami/hostpid/broker.sock` and serves protocol version 1.

4. Each allocation that receives HAMi-core also receives `LIBVGPU_HOSTPID_BROKER=1` and a read-only mount from `/var/run/hami/hostpid` to `/tmp/vgpulock/hostpid`.

The device plugin prepares `/tmp/vgpulock` with mode `01777` before returning an allocation. This also applies when the broker is disabled and HAMi-core uses the existing fallback. Allocation fails if the directory cannot be prepared safely.

Preparation opens `/tmp` without following a symlink, verifies its owner and sticky rule, then creates and opens `vgpulock` relative to that descriptor. The first `mkdirat()` requests mode `01777`. A descriptor-based `chmod` restores bits removed by the process umask. A final identity and mode check rejects replacement during preparation.

The allocation response contains one writable parent mount at `/tmp/vgpulock`. When the broker is enabled, it also contains one read-only broker mount at `/tmp/vgpulock/hostpid`. The integration replaces duplicate or path-equivalent entries with these canonical mounts and preserves unrelated mounts. It lists the parent before the nested broker mount so the parent does not hide the broker mount when the runtime applies the response.

Before applying the current gate, the allocation helper clears the reserved broker environment key. When the broker is disabled, it also removes stale broker mounts while preserving the writable parent mount and unrelated mounts.

No value other than the exact string `1` enables the server or client.

## Protocol

The protocol uses one request and one response on a Unix stream connection. Every integer is unsigned and encoded in network byte order.

| Field | Request bytes | Response bytes |
| --- | ---: | ---: |
| Magic `HPID` | 4 | 4 |
| Version | 2 | 2 |
| Command or status | 2 | 2 |
| Host PID | 0 | 4 |

Protocol version 1 supports command 1, which means get the caller's host PID. Status 0 is success. Status 1 means the request was invalid.

The server reads the peer credentials from the connected socket after validating the request. The client never sends a PID.

## Security boundary

1. The server requires effective UID 0 for its default path.

2. The server directory is owned by root with mode `0711`.

3. The shared lock parent is owned by root with mode `01777`. Allocation rejects an unsafe owner, object type, symlink, or final mode.

4. A root-owned `0600` lock file prevents two brokers from replacing each other.

5. The server rejects symlink directories, symlink lock files, regular file collisions, sockets owned by another UID, and active sockets.

6. The server removes only a stale socket owned by the expected UID. During shutdown it removes the path only if its device and inode still match the socket it created.

7. The workload sees the broker directory through a read-only mount. The HAMi-core client checks the directory owner, directory write bits, socket type, socket owner, read-only mount flag, and connected peer UID before trusting a response.

8. A caller can request only its own PID. The kernel supplies that identity through `SO_PEERCRED` in the broker's host PID namespace.

The socket is available only to workloads that receive the allocation mount. A workload can still create connection pressure. The server limits active handlers, applies one transaction deadline, and closes excess connections. A client that cannot complete the transaction uses the existing bounded HAMi-core fallback.

## Failure behavior

| Condition | Server behavior | HAMi-core behavior |
| --- | --- | --- |
| Feature disabled | No socket is created | Existing NVML discovery path |
| Server cannot start | Device plugin startup fails | No new allocation is served by that plugin instance |
| Lock parent cannot be prepared safely | The allocation fails | No unsafe parent mount is returned |
| Socket missing or stale in a workload | No broker response | Existing NVML discovery path |
| Unsafe owner, mode, mount, or peer UID | Request is not trusted | Existing NVML discovery path |
| Malformed protocol reply | Request fails | Existing NVML discovery path |
| Slow or unresponsive broker | Server and client deadlines close the request | Existing NVML discovery path |
| Broker exits after plugin startup | Device plugin exits with the broker error | Kubernetes restarts the device plugin |

The broker never returns a guessed PID. A successful reply contains the PID supplied by the kernel. Every other outcome is a failure that leaves PID discovery to the existing path.

## Rollout

1. Install the server capable HAMi release with `hostPIDBroker.enabled=false`.

2. Install the compatible HAMi-core library. With no broker mount it uses the existing fallback.

3. Enable `hostPIDBroker.enabled` and wait for every NVIDIA device plugin pod to become ready.

4. Restart or recreate selected workloads so their allocation responses include the broker mount and environment gate.

5. Validate the selected workloads before widening the rollout. Check correct host PID assignment, CUDA context accounting, broker use, and fallback behavior.

Existing workloads do not gain a new mount when the device plugin changes. They continue through their existing path until they are recreated.

## Rollback

1. Set `hostPIDBroker.enabled=false`.

2. Wait for the NVIDIA device plugin rollout.

3. Recreate workloads when the broker mount should be removed. Workloads that still have a compatible broker mount can continue until they exit.

4. A newer HAMi-core with no broker available uses the existing fallback, so the library does not need to be rolled back first.

This rollout contract applies to the broker feature. The separate PR 248 lock migration still requires its own mixed binary policy.

## Validation required before release

1. Go race tests for the broker, lifecycle, and allocation integration.

2. The actual C client to Go server contract test.

3. Missing, stale, unsafe, malformed, slow, saturated, restarting, and dying broker cases.

4. Linux and CUDA builds for HAMi-core plus the client and context accounting tests.

5. Kubernetes validation with the feature disabled, enabled, rolled forward, and rolled back.

6. Concurrent `cuInit()` and first primary context benchmarks with raw output, environment details, source revisions, and checksums.

## Known limits

1. The design depends on Linux `SO_PEERCRED` and a device plugin in the host PID namespace.

2. Sandboxed runtimes must be tested in real execution. A mounted Unix socket may be blocked or may not preserve the peer identity needed by this design.

3. Existing workloads need recreation to receive or remove the allocation mount.

4. Additional NVIDIA architectures, driver versions, CRI-O, rootless runtimes, gVisor, and Kata remain separate compatibility cells until each is tested.
