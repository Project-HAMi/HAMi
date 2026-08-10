#!/usr/bin/env bash
# Copyright 2026 The HAMi Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -euo pipefail

NS=${MIG_E2E_NAMESPACE:-hami-mig-final-e2e}
HAMI_NS=${HAMI_NAMESPACE:-hami-system}
TARGET_NODE=${TARGET_NODE:-}
DEVICE_PLUGIN_DAEMONSET=${DEVICE_PLUGIN_DAEMONSET:-hami-device-plugin}
DEVICE_PLUGIN_CONTAINER=${DEVICE_PLUGIN_CONTAINER:-device-plugin}
IMAGE=${GPU_TEST_IMAGE:-nvcr.io/nvidia/k8s/cuda-sample:vectoradd-cuda12.5.0-ubuntu22.04}
GPU_PROGRESS_WAIT=${GPU_PROGRESS_WAIT:-5}

log() { printf '\n[%s] %s\n' "$(date -u +%H:%M:%S)" "$*"; }
fail() { echo "FAIL: $*" >&2; exit 1; }

cleanup() {
  kubectl delete namespace "$NS" --wait=false >/dev/null 2>&1 || true
  for _ in $(seq 1 90); do
    kubectl get namespace "$NS" >/dev/null 2>&1 || return 0
    sleep 2
  done
  echo "FAIL: namespace cleanup timed out" >&2
  return 1
}

trap 'status=$?; cleanup || true; exit "$status"' EXIT

device_plugin_pod() {
  kubectl get pods -n "$HAMI_NS" --field-selector "spec.nodeName=${TARGET_NODE}" -o json |
    jq -r --arg daemonset "$DEVICE_PLUGIN_DAEMONSET" '.items[] | select([.metadata.ownerReferences[]? | select(.kind == "DaemonSet" and .name == $daemonset)] | length > 0) | .metadata.name' |
    sed -n '1p'
}

node_nvidia_smi() {
  local pod
  pod=$(device_plugin_pod)
  [[ -n "$pod" ]] || fail "no ${DEVICE_PLUGIN_DAEMONSET} Pod found on ${TARGET_NODE}"
  kubectl exec -n "$HAMI_NS" "$pod" -c "$DEVICE_PLUGIN_CONTAINER" -- nvidia-smi "$@"
}

restart_target_device_plugin() {
  local old_pod new_pod ready
  old_pod=$(device_plugin_pod)
  [[ -n "$old_pod" ]] || fail "no ${DEVICE_PLUGIN_DAEMONSET} Pod found on ${TARGET_NODE}"
  kubectl delete pod -n "$HAMI_NS" "$old_pod" --wait=false
  for _ in $(seq 1 90); do
    new_pod=$(device_plugin_pod || true)
    if [[ -n "$new_pod" && "$new_pod" != "$old_pod" ]]; then
      ready=$(kubectl get pod -n "$HAMI_NS" "$new_pod" -o json 2>/dev/null | jq -r --arg container "$DEVICE_PLUGIN_CONTAINER" '.status.containerStatuses[]? | select(.name == $container) | .ready' || true)
      [[ "$ready" == true ]] && return 0
    fi
    sleep 2
  done
  fail "device plugin on ${TARGET_NODE} did not restart"
}

create_pod() {
  local name=$1 memory=$2
  printf '%s\n' "{\"apiVersion\":\"v1\",\"kind\":\"Pod\",\"metadata\":{\"name\":\"${name}\",\"namespace\":\"${NS}\",\"annotations\":{\"nvidia.com/vgpu-mode\":\"mig\"}},\"spec\":{\"schedulerName\":\"hami-scheduler\",\"nodeSelector\":{\"kubernetes.io/hostname\":\"${TARGET_NODE}\"},\"restartPolicy\":\"Never\",\"containers\":[{\"name\":\"cuda\",\"image\":\"${IMAGE}\",\"imagePullPolicy\":\"IfNotPresent\",\"command\":[\"bash\",\"-lc\",\"set -euo pipefail; nvidia-smi -L; n=0; echo 0 > /tmp/gpu-progress; while true; do if ! /cuda-samples/vectorAdd > /tmp/vectoradd.last 2>&1; then cat /tmp/vectoradd.last >&2; exit 1; fi; n=\$((n + 1)); echo \\\"\$n\\\" > /tmp/gpu-progress.next; mv /tmp/gpu-progress.next /tmp/gpu-progress; done\"],\"resources\":{\"limits\":{\"nvidia.com/gpu\":1,\"nvidia.com/gpumem\":${memory}}}}]}}" | kubectl apply -f -
}

wait_ready() { kubectl wait -n "$NS" --for=condition=Ready "pod/$1" --timeout=180s; }
mig_count() { node_nvidia_smi -L | grep -c 'MIG ' || true; }
profile_count() { node_nvidia_smi -L | grep -c "MIG $1" || true; }
pod_uuid() { kubectl exec -n "$NS" "$1" -- nvidia-smi -L | sed -n 's/.*UUID: \(MIG-[^)]*\)).*/\1/p' | head -1; }
gpu_progress() { kubectl exec -n "$NS" "$1" -- cat /tmp/gpu-progress; }

assert_gpu_progress() {
  local pod=$1 before after phase restarts
  phase=$(kubectl get pod -n "$NS" "$pod" -o jsonpath='{.status.phase}')
  restarts=$(kubectl get pod -n "$NS" "$pod" -o jsonpath='{.status.containerStatuses[0].restartCount}')
  [[ "$phase" == Running && "$restarts" == 0 ]] || fail "$pod workload unhealthy: phase=${phase} restarts=${restarts}"
  before=$(gpu_progress "$pod")
  sleep "$GPU_PROGRESS_WAIT"
  after=$(gpu_progress "$pod")
  [[ "$before" =~ ^[0-9]+$ && "$after" =~ ^[0-9]+$ && "$after" -gt "$before" ]] || {
    kubectl exec -n "$NS" "$pod" -- cat /tmp/vectoradd.last >&2 || true
    fail "$pod CUDA progress stalled: before=${before} after=${after}"
  }
  echo "GPU_PROGRESS pod=${pod} before=${before} after=${after}"
}

snapshot_gpu_progress() {
  local pod
  for pod in "$@"; do
    printf '%s=%s\n' "$pod" "$(gpu_progress "$pod")"
  done
}

assert_gpu_progress_since() {
  local snapshot=$1 pod before after phase restarts
  shift
  for pod in "$@"; do
    before=$(sed -n "s/^${pod}=//p" <<<"$snapshot")
    after=$(gpu_progress "$pod")
    phase=$(kubectl get pod -n "$NS" "$pod" -o jsonpath='{.status.phase}')
    restarts=$(kubectl get pod -n "$NS" "$pod" -o jsonpath='{.status.containerStatuses[0].restartCount}')
    [[ "$phase" == Running && "$restarts" == 0 && "$before" =~ ^[0-9]+$ && "$after" =~ ^[0-9]+$ && "$after" -gt "$before" ]] || {
      kubectl exec -n "$NS" "$pod" -- cat /tmp/vectoradd.last >&2 || true
      fail "$pod CUDA workload did not survive operation: before=${before} after=${after} phase=${phase} restarts=${restarts}"
    }
    echo "GPU_PROGRESS_ACROSS_OPERATION pod=${pod} before=${before} after=${after}"
  done
}

wait_count() {
  local want=$1 timeout=${2:-120} start count
  start=$(date +%s)
  while true; do
    count=$(mig_count)
    [[ "$count" == "$want" ]] && { echo "MIG_COUNT=${want}"; return; }
    (( $(date +%s) - start < timeout )) || { node_nvidia_smi -L; fail "MIG count=${count}, want=${want}"; }
    sleep 2
  done
}

assert_uuid() {
  local pod=$1 want=$2 got
  got=$(pod_uuid "$pod")
  [[ -n "$got" && "$got" == "$want" ]] || fail "$pod UUID changed: got=${got} want=${want}"
}

assert_runtime_annotation() {
  local pod=$1 raw
  raw=$(kubectl get pod -n "$NS" "$pod" -o json | jq -r '.metadata.annotations["hami.io/vgpu-mig-allocations"]')
  jq -e 'length > 0 and all(.[]; (.migUUID | startswith("MIG-")) and (.profile | length > 0) and (.placement.size > 0) and (.gpuInstanceID | type == "number") and (.computeInstanceID | type == "number"))' <<<"$raw" >/dev/null || fail "$pod lacks concrete MIG runtime identity annotation"
}

assert_pending_unbound() {
  local pod=$1 phase node
  sleep 20
  phase=$(kubectl get pod -n "$NS" "$pod" -o jsonpath='{.status.phase}')
  node=$(kubectl get pod -n "$NS" "$pod" -o jsonpath='{.spec.nodeName}')
  [[ "$phase" == Pending && -z "$node" ]] || fail "$pod expected Pending/unbound, got phase=${phase} node=${node}"
}

[[ -n "$TARGET_NODE" ]] || fail "set TARGET_NODE to the Kubernetes node under test"
kubectl get node "$TARGET_NODE" >/dev/null || fail "target node ${TARGET_NODE} does not exist"
node_nvidia_smi -L >/dev/null
physical_gpu_count=$(node_nvidia_smi --query-gpu=index --format=csv,noheader | wc -l | tr -d ' ')
[[ "$physical_gpu_count" == 1 ]] || fail "target node ${TARGET_NODE} must expose exactly one physical GPU, found ${physical_gpu_count}"

log "baseline cleanup on ${TARGET_NODE}"
cleanup
kubectl create namespace "$NS"
wait_count 0

log "CASE 1: two concurrent 1g plus a mixed 2g"
create_pod one-a 4500
create_pod one-b 4500
wait_ready one-a & p1=$!
wait_ready one-b & p2=$!
wait "$p1"; wait "$p2"
wait_count 2
[[ "$(profile_count 1g.5gb)" == 2 ]] || fail "expected two 1g.5gb"
uuid_one_a=$(pod_uuid one-a)
uuid_one_b=$(pod_uuid one-b)
create_pod two-a 9500
wait_ready two-a
wait_count 3
[[ "$(profile_count 1g.5gb)" == 2 && "$(profile_count 2g.10gb)" == 1 ]] || fail "1g/2g mixed layout missing"
uuid_two_a=$(pod_uuid two-a)
assert_runtime_annotation one-a
assert_runtime_annotation one-b
assert_runtime_annotation two-a
assert_gpu_progress one-a
assert_gpu_progress one-b
assert_gpu_progress two-a
echo "PASS CASE 1 one-a=${uuid_one_a} one-b=${uuid_one_b} two-a=${uuid_two_a}"

log "CASE 2: reach exact 1x1g + 3x2g capacity and reject overflow"
kubectl delete pod -n "$NS" one-b --wait=true
wait_count 2
assert_uuid one-a "$uuid_one_a"
assert_uuid two-a "$uuid_two_a"
create_pod two-b 9500
create_pod two-c 9500
wait_ready two-b & p1=$!
wait_ready two-c & p2=$!
wait "$p1"; wait "$p2"
wait_count 4
[[ "$(profile_count 1g.5gb)" == 1 && "$(profile_count 2g.10gb)" == 3 ]] || fail "expected 1x1g + 3x2g"
uuid_two_b=$(pod_uuid two-b)
uuid_two_c=$(pod_uuid two-c)
assert_gpu_progress one-a
assert_gpu_progress two-a
assert_gpu_progress two-b
assert_gpu_progress two-c
create_pod overflow-one 4500
assert_pending_unbound overflow-one
wait_count 4
echo "PASS CASE 2 capacity enforced"

log "CASE 3: busy device-plugin restart preserves every MIG UUID"
progress_before_restart=$(snapshot_gpu_progress one-a two-a two-b two-c)
restart_target_device_plugin
sleep 15
assert_gpu_progress_since "$progress_before_restart" one-a two-a two-b two-c
assert_uuid one-a "$uuid_one_a"
assert_uuid two-a "$uuid_two_a"
assert_uuid two-b "$uuid_two_b"
assert_uuid two-c "$uuid_two_c"
wait_count 4
[[ "$(profile_count 1g.5gb)" == 1 && "$(profile_count 2g.10gb)" == 3 ]] || fail "layout changed during restart"
kubectl logs -n "$HAMI_NS" "$(device_plugin_pod)" -c "$DEVICE_PLUGIN_CONTAINER" --since=3m | grep 'inUseGPUs=\[0\]' >/dev/null || fail "startup did not detect busy GPU"
echo "PASS CASE 3 all UUIDs preserved"

log "CASE 4: immediate delete/replacement after restart"
progress_before_reclaim=$(snapshot_gpu_progress one-a two-a two-c)
kubectl delete pod -n "$NS" two-b --wait=true
wait_ready overflow-one
assert_gpu_progress_since "$progress_before_reclaim" one-a two-a two-c
assert_gpu_progress overflow-one
wait_count 4
[[ "$(profile_count 1g.5gb)" == 2 && "$(profile_count 2g.10gb)" == 2 ]] || fail "replacement layout incorrect"
assert_uuid one-a "$uuid_one_a"
assert_uuid two-a "$uuid_two_a"
assert_uuid two-c "$uuid_two_c"
uuid_overflow=$(pod_uuid overflow-one)
[[ -n "$uuid_overflow" ]] || fail "replacement 1g has no MIG UUID"
echo "PASS CASE 4 replacement=${uuid_overflow}"

log "reset before 3g topology"
kubectl delete pod -n "$NS" one-a two-a two-c overflow-one --wait=false
for pod in one-a two-a two-c overflow-one; do kubectl wait -n "$NS" --for=delete "pod/$pod" --timeout=120s || true; done
wait_count 0

log "CASE 5: scheduler rejects topology-infeasible 1g beside 2x3g"
create_pod three-a 19000
create_pod three-b 19000
wait_ready three-a & p1=$!
wait_ready three-b & p2=$!
wait "$p1"; wait "$p2"
wait_count 2
[[ "$(profile_count 3g.20gb)" == 2 ]] || fail "expected two 3g instances"
assert_gpu_progress three-a
assert_gpu_progress three-b
create_pod blocked-one 4500
assert_pending_unbound blocked-one
[[ "$(kubectl get pod -n "$NS" blocked-one -o jsonpath='{.status.reason}')" != UnexpectedAdmissionError ]] || fail "topology-infeasible Pod reached admission"
echo "PASS CASE 5 topology rejected before Bind"

log "reset before balanced topology"
kubectl delete pod -n "$NS" three-a three-b blocked-one --wait=false
for pod in three-a three-b blocked-one; do kubectl wait -n "$NS" --for=delete "pod/$pod" --timeout=120s || true; done
wait_count 0

log "CASE 6: 1x3g + 1x2g + 2x1g exact capacity, overflow, and immediate replacement"
create_pod three-a 19000
create_pod two-d 9500
create_pod one-c 4500
create_pod one-d 4500
wait_ready three-a & p1=$!
wait_ready two-d & p2=$!
wait_ready one-c & p3=$!
wait_ready one-d & p4=$!
wait "$p1"; wait "$p2"; wait "$p3"; wait "$p4"
wait_count 4
[[ "$(profile_count 3g.20gb)" == 1 && "$(profile_count 2g.10gb)" == 1 && "$(profile_count 1g.5gb)" == 2 ]] || fail "expected 1x3g + 1x2g + 2x1g"
uuid_three_a=$(pod_uuid three-a)
uuid_two_d=$(pod_uuid two-d)
uuid_one_d=$(pod_uuid one-d)
assert_gpu_progress three-a
assert_gpu_progress two-d
assert_gpu_progress one-c
assert_gpu_progress one-d
create_pod overflow-two 4500
assert_pending_unbound overflow-two
progress_before_mixed_reclaim=$(snapshot_gpu_progress three-a two-d one-d)
kubectl delete pod -n "$NS" one-c --wait=true
wait_ready overflow-two
assert_gpu_progress_since "$progress_before_mixed_reclaim" three-a two-d one-d
assert_gpu_progress overflow-two
wait_count 4
[[ "$(profile_count 3g.20gb)" == 1 && "$(profile_count 2g.10gb)" == 1 && "$(profile_count 1g.5gb)" == 2 ]] || fail "mixed replacement layout incorrect"
assert_uuid three-a "$uuid_three_a"
assert_uuid two-d "$uuid_two_d"
assert_uuid one-d "$uuid_one_d"
echo "PASS CASE 6"

log "reset before seven-way burst"
kubectl delete pod -n "$NS" three-a two-d one-d overflow-two --wait=false
for pod in three-a two-d one-d overflow-two; do kubectl wait -n "$NS" --for=delete "pod/$pod" --timeout=120s || true; done
wait_count 0

log "CASE 7: seven concurrent 1g instances, partial reclaim and refill"
for i in $(seq 1 7); do create_pod "burst-${i}" 4500; done
for i in $(seq 1 7); do wait_ready "burst-${i}" & done
wait
wait_count 7 180
[[ "$(profile_count 1g.5gb)" == 7 ]] || fail "expected seven 1g instances"
for i in $(seq 1 7); do assert_gpu_progress "burst-${i}"; done
uuid_b2=$(pod_uuid burst-2); uuid_b4=$(pod_uuid burst-4); uuid_b6=$(pod_uuid burst-6); uuid_b7=$(pod_uuid burst-7)
progress_before_burst_reclaim=$(snapshot_gpu_progress burst-2 burst-4 burst-6 burst-7)
kubectl delete pod -n "$NS" burst-1 burst-3 burst-5 --wait=false
for i in 1 3 5; do kubectl wait -n "$NS" --for=delete "pod/burst-${i}" --timeout=120s || true; done
wait_count 4
assert_gpu_progress_since "$progress_before_burst_reclaim" burst-2 burst-4 burst-6 burst-7
assert_uuid burst-2 "$uuid_b2"; assert_uuid burst-4 "$uuid_b4"; assert_uuid burst-6 "$uuid_b6"; assert_uuid burst-7 "$uuid_b7"
for i in 8 9 10; do create_pod "burst-${i}" 4500; done
for i in 8 9 10; do wait_ready "burst-${i}" & done
wait
wait_count 7 180
[[ "$(profile_count 1g.5gb)" == 7 ]] || fail "refill did not restore seven 1g instances"
for i in 2 4 6 7 8 9 10; do assert_gpu_progress "burst-${i}"; done
echo "PASS CASE 7"

log "final cleanup and health"
cleanup
wait_count 0
kubectl get nodes
kubectl get pods -n "$HAMI_NS"
node_nvidia_smi -q | grep -A3 'MIG Mode'
echo "ALL_FIXED_MIG_E2E_TESTS_PASSED"
