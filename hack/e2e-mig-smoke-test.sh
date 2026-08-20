#!/usr/bin/env bash
# Copyright 2024 The HAMi Authors.
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

# Lightweight MIG smoke test for PR gates
# This script runs a minimal MIG test to verify basic MIG functionality
# without running the full MIG matrix (which is reserved for nightly/release).

set -euo pipefail

KUBE_CONF=${1:-"${HOME}/.kube/config"}
export KUBECONFIG="${KUBE_CONF}"

NS=${MIG_E2E_NAMESPACE:-hami-mig-smoke-e2e}
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

create_pod() {
  local name=$1 memory=$2
  printf '%s\n' "{\"apiVersion\":\"v1\",\"kind\":\"Pod\",\"metadata\":{\"name\":\"${name}\",\"namespace\":\"${NS}\",\"annotations\":{\"nvidia.com/vgpu-mode\":\"mig\"}},\"spec\":{\"schedulerName\":\"hami-scheduler\",\"nodeSelector\":{\"kubernetes.io/hostname\":\"${TARGET_NODE}\"},\"restartPolicy\":\"Never\",\"containers\":[{\"name\":\"cuda\",\"image\":\"${IMAGE}\",\"imagePullPolicy\":\"IfNotPresent\",\"command\":[\"bash\",\"-lc\",\"set -euo pipefail; nvidia-smi -L; n=0; echo 0 > /tmp/gpu-progress; while true; do if ! /cuda-samples/vectorAdd > /tmp/vectoradd.last 2>&1; then cat /tmp/vectoradd.last >&2; exit 1; fi; n=\$((n + 1)); echo \\\"\$n\\\" > /tmp/gpu-progress.next; mv /tmp/gpu-progress.next /tmp/gpu-progress; done\"],\"resources\":{\"limits\":{\"nvidia.com/gpu\":1,\"nvidia.com/gpumem\":${memory}}}}]}}" | kubectl apply -f -
}

wait_ready() { kubectl wait -n "$NS" --for=condition=Ready "pod/$1" --timeout=180s; }
mig_count() { node_nvidia_smi -L | grep -c 'MIG ' || true; }
profile_count() { node_nvidia_smi -L | grep -c "MIG $1" || true; }
pod_uuid() { kubectl exec -n "$NS" "$1" -- nvidia-smi -L | sed -n 's/.*UUID: \(MIG-[^)]*\)).*/\1/p' | head -1; }
gpu_progress() { kubectl exec -n "$NS" "$1" -- cat /tmp/gpu-progress; }

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

log "SMOKE CASE: basic MIG allocation and GPU progress validation"
create_pod smoke-one 4500
wait_ready smoke-one
wait_count 1
[[ "$(profile_count 1g.5gb)" == 1 ]] || fail "expected one 1g.5gb"
uuid_smoke_one=$(pod_uuid smoke-one)
assert_runtime_annotation smoke-one
assert_gpu_progress smoke-one

log "SMOKE CASE: overflow rejection"
create_pod smoke-overflow 4500
assert_pending_unbound smoke-overflow
wait_count 1
echo "PASS SMOKE CASE: overflow rejected"

log "final cleanup and health"
cleanup
wait_count 0
kubectl get nodes
kubectl get pods -n "$HAMI_NS"
node_nvidia_smi -q | grep -A3 'MIG Mode'
echo "ALL_MIG_SMOKE_TESTS_PASSED"
