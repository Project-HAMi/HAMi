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

NS=${OVERSUB_E2E_NAMESPACE:-hami-memory-oversubscription-e2e}
HAMI_NS=${HAMI_NAMESPACE:-hami-system}
TARGET_NODE=${TARGET_NODE:-}
TARGET_GPU_UUID=${TARGET_GPU_UUID:-}
DEVICE_PLUGIN_DAEMONSET=${DEVICE_PLUGIN_DAEMONSET:-hami-device-plugin}
DEVICE_PLUGIN_CONTAINER=${DEVICE_PLUGIN_CONTAINER:-device-plugin}
SCHEDULER_DEVICE_CONFIGMAP=${SCHEDULER_DEVICE_CONFIGMAP:-hami-scheduler-device}
IMAGE=${GPU_TEST_IMAGE:-nvidia/cuda:12.5.1-devel-ubuntu22.04}
REQUEST_PERCENT=${OVERSUB_REQUEST_PERCENT:-60}
ALLOCATE_PERCENT=${OVERSUB_ALLOCATE_PERCENT:-90}
ARTIFACT_DIR=${OVERSUB_ARTIFACT_DIR:-}
CLEANUP_ENABLED=false

log() { printf '\n[%s] %s\n' "$(date -u +%H:%M:%S)" "$*"; }
fail() { echo "FAIL: $*" >&2; exit 1; }

usage() {
  cat <<'EOF'
Usage:
  TARGET_NODE=<node> bash hack/hami-memory-oversubscription-e2e.sh
  bash hack/hami-memory-oversubscription-e2e.sh --print-manifests <node> <gpu-uuid> <physical-mib>

The target HAMi installation must use deviceMemoryScaling > 1.2. The test
creates two Pods on one physical GPU. Each requests 60% of physical VRAM and
touches 90% of its request. The holder must remain healthy while the challenger
observes CUDA's runtime out-of-memory error (`cudaErrorMemoryAllocation`).

Environment:
  TARGET_GPU_UUID             Exact physical GPU UUID; required on multi-GPU nodes
  SCHEDULER_DEVICE_CONFIGMAP  ConfigMap containing device-config.yaml
  GPU_TEST_IMAGE              CUDA devel image containing nvcc
  OVERSUB_ARTIFACT_DIR        Directory for metadata and diagnostic logs
EOF
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

is_positive_integer() {
  [[ "$1" =~ ^[1-9][0-9]*$ ]]
}

validate_percentages() {
  is_positive_integer "$REQUEST_PERCENT" || fail "OVERSUB_REQUEST_PERCENT must be a positive integer"
  is_positive_integer "$ALLOCATE_PERCENT" || fail "OVERSUB_ALLOCATE_PERCENT must be a positive integer"
  ((REQUEST_PERCENT <= 100)) || fail "OVERSUB_REQUEST_PERCENT must not exceed 100"
  ((ALLOCATE_PERCENT <= 100)) || fail "OVERSUB_ALLOCATE_PERCENT must not exceed 100"
  ((2 * REQUEST_PERCENT * ALLOCATE_PERCENT > 10000)) ||
    fail "the two allocations must exceed 100% of physical VRAM"
}

validate_memory_scaling() {
  local scaling=$1 required
  required=$((2 * REQUEST_PERCENT))
  awk -v scaling="$scaling" -v required="$required" '
    BEGIN { exit !(scaling ~ /^[0-9]+([.][0-9]+)?$/ && scaling * 100 >= required) }
  ' || fail "deviceMemoryScaling=${scaling} cannot schedule two ${REQUEST_PERCENT}% requests; require at least $(awk -v r="$required" 'BEGIN { printf "%.2f", r / 100 }')"
}

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

resolve_target_gpu() {
  local inventory=$1 line index uuid name count=0 selected_index selected_uuid selected_name
  while IFS=',' read -r index uuid name; do
    index=${index//[[:space:]]/}
    uuid=${uuid//[[:space:]]/}
    name=${name# }
    [[ -n "$index" ]] || continue
    count=$((count + 1))
    if [[ -n "$TARGET_GPU_UUID" && "$uuid" != "$TARGET_GPU_UUID" ]]; then
      continue
    fi
    selected_index=$index
    selected_uuid=$uuid
    selected_name=$name
  done <<<"$inventory"

  ((count > 0)) || fail "nvidia-smi reported no physical GPUs"
  if ((count > 1)) && [[ -z "$TARGET_GPU_UUID" ]]; then
    fail "${count} GPUs found on ${TARGET_NODE}; set TARGET_GPU_UUID"
  fi
  [[ -n "${selected_uuid:-}" ]] || fail "TARGET_GPU_UUID did not match a GPU on ${TARGET_NODE}"
  RESOLVED_GPU_INDEX=$selected_index
  RESOLVED_GPU_UUID=$selected_uuid
  RESOLVED_GPU_NAME=$selected_name
}

probe_source() {
  cat <<'EOF'
#include <cuda_runtime.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

int main(int argc, char **argv) {
    if (argc != 3) {
        fprintf(stderr, "usage: %s holder|expect-oom allocation-mib\n", argv[0]);
        return 2;
    }
    size_t mib = strtoull(argv[2], NULL, 10);
    size_t bytes = mib * 1024ULL * 1024ULL;
    void *ptr = NULL;
    cudaError_t result = cudaMalloc(&ptr, bytes);
    if (strcmp(argv[1], "expect-oom") == 0) {
        if (result == cudaErrorMemoryAllocation) {
            printf("EXPECTED_OOM code=%d name=%s allocation_mib=%zu\n", result,
                   cudaGetErrorName(result), mib);
            return 0;
        }
        if (result == cudaSuccess) {
            cudaFree(ptr);
            fprintf(stderr, "UNEXPECTED_SUCCESS allocation_mib=%zu\n", mib);
            return 1;
        }
        fprintf(stderr, "UNEXPECTED_CUDA_ERROR code=%d name=%s\n", result,
                cudaGetErrorName(result));
        return 1;
    }
    if (result != cudaSuccess) {
        fprintf(stderr, "HOLDER_ALLOCATION_FAILED code=%d name=%s\n", result,
                cudaGetErrorName(result));
        return 1;
    }
    result = cudaMemset(ptr, 0x5a, bytes);
    if (result != cudaSuccess || cudaDeviceSynchronize() != cudaSuccess) {
        fprintf(stderr, "HOLDER_INITIAL_TOUCH_FAILED\n");
        return 1;
    }
    printf("HOLDER_READY allocation_mib=%zu\n", mib);
    fflush(stdout);
    for (unsigned long heartbeat = 1;; heartbeat++) {
        result = cudaMemset(ptr, (int)(heartbeat & 0xff), bytes);
        if (result != cudaSuccess || cudaDeviceSynchronize() != cudaSuccess) {
            fprintf(stderr, "HOLDER_TOUCH_FAILED heartbeat=%lu error=%s\n",
                    heartbeat, cudaGetErrorName(result));
            return 1;
        }
        printf("HEARTBEAT %lu\n", heartbeat);
        fflush(stdout);
        sleep(2);
    }
}
EOF
}

create_probe_configmap() {
  probe_source | kubectl create configmap vram-probe -n "$NS" --from-file=vram-probe.cu=/dev/stdin --dry-run=client -o yaml | kubectl apply -f -
}

pod_manifest() {
  local name=$1 mode=$2 request_mib=$3 allocation_mib=$4
  cat <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: ${name}
  namespace: ${NS}
  annotations:
    nvidia.com/use-gpuuuid: "${RESOLVED_GPU_UUID}"
spec:
  restartPolicy: Never
  nodeSelector:
    kubernetes.io/hostname: ${TARGET_NODE}
  containers:
  - name: probe
    image: ${IMAGE}
    command: ["/bin/bash", "-ceu"]
    args:
    - |
      nvcc -O2 /probe/vram-probe.cu -o /tmp/vram-probe
      exec /tmp/vram-probe ${mode} ${allocation_mib}
    resources:
      limits:
        nvidia.com/gpu: 1
        nvidia.com/gpumem: ${request_mib}
        nvidia.com/gpucores: 50
    volumeMounts:
    - name: probe-source
      mountPath: /probe
      readOnly: true
  volumes:
  - name: probe-source
    configMap:
      name: vram-probe
EOF
}

wait_for_log() {
  local pod=$1 pattern=$2 attempts=${3:-180}
  for _ in $(seq 1 "$attempts"); do
    kubectl logs -n "$NS" "$pod" 2>/dev/null | grep -q "$pattern" && return 0
    sleep 2
  done
  return 1
}

last_heartbeat() {
  kubectl logs -n "$NS" "$1" | awk '/^HEARTBEAT [0-9]+$/ { value=$2 } END { print value+0 }'
}

capture_artifacts() {
  [[ -n "$ARTIFACT_DIR" ]] || return 0
  mkdir -p "$ARTIFACT_DIR"
  {
    echo "timestamp=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    echo "node=${TARGET_NODE}"
    echo "gpu_index=${RESOLVED_GPU_INDEX}"
    echo "gpu_uuid=${RESOLVED_GPU_UUID}"
    echo "gpu_name=${RESOLVED_GPU_NAME}"
    echo "physical_memory_mib=${PHYSICAL_MEMORY_MIB}"
    echo "driver_version=${DRIVER_VERSION}"
    echo "device_memory_scaling=${MEMORY_SCALING}"
    echo "request_mib=${REQUEST_MIB}"
    echo "allocation_mib=${ALLOCATION_MIB}"
    kubectl version -o json | jq -r '"kubernetes_server_version=" + .serverVersion.gitVersion'
    helm list -n "$HAMI_NS" -o json 2>/dev/null | jq -r '.[] | "helm_chart=" + .chart + " app_version=" + .app_version' || true
  } >"$ARTIFACT_DIR/metadata.txt"
  node_nvidia_smi >"$ARTIFACT_DIR/nvidia-smi.txt" 2>&1 || true
  kubectl get pods -n "$NS" -o wide >"$ARTIFACT_DIR/pods.txt" 2>&1 || true
  kubectl logs -n "$NS" vram-holder >"$ARTIFACT_DIR/holder.log" 2>&1 || true
  kubectl logs -n "$NS" vram-challenger >"$ARTIFACT_DIR/challenger.log" 2>&1 || true
  kubectl logs -n "$HAMI_NS" "$(device_plugin_pod)" -c "$DEVICE_PLUGIN_CONTAINER" >"$ARTIFACT_DIR/device-plugin.log" 2>&1 || true
}

cleanup() {
  kubectl delete namespace "$NS" --wait=false >/dev/null 2>&1 || true
}

on_exit() {
  local status=$?
  trap - EXIT
  capture_artifacts || true
  [[ "$CLEANUP_ENABLED" == true ]] && cleanup
  exit "$status"
}

print_manifests() {
  TARGET_NODE=$1
  RESOLVED_GPU_UUID=$2
  PHYSICAL_MEMORY_MIB=$3
  REQUEST_MIB=$((PHYSICAL_MEMORY_MIB * REQUEST_PERCENT / 100))
  ALLOCATION_MIB=$((REQUEST_MIB * ALLOCATE_PERCENT / 100))
  pod_manifest vram-holder holder "$REQUEST_MIB" "$ALLOCATION_MIB"
  echo '---'
  pod_manifest vram-challenger expect-oom "$REQUEST_MIB" "$ALLOCATION_MIB"
}

run_hardware_e2e() {
  require_command kubectl
  require_command jq
  require_command awk
  validate_percentages
  [[ -n "$TARGET_NODE" ]] || fail "TARGET_NODE is required"
  kubectl get node "$TARGET_NODE" >/dev/null

  local inventory config
  inventory=$(node_nvidia_smi --query-gpu=index,uuid,name --format=csv,noheader,nounits)
  resolve_target_gpu "$inventory"
  PHYSICAL_MEMORY_MIB=$(node_nvidia_smi --query-gpu=memory.total --format=csv,noheader,nounits -i "$RESOLVED_GPU_INDEX" | tr -d '[:space:]')
  DRIVER_VERSION=$(node_nvidia_smi --query-gpu=driver_version --format=csv,noheader -i "$RESOLVED_GPU_INDEX" | tr -d '[:space:]')
  is_positive_integer "$PHYSICAL_MEMORY_MIB" || fail "invalid physical memory reported by nvidia-smi"

  config=$(kubectl get configmap "$SCHEDULER_DEVICE_CONFIGMAP" -n "$HAMI_NS" -o jsonpath='{.data.device-config\.yaml}')
  MEMORY_SCALING=$(awk '$1 == "deviceMemoryScaling:" { print $2; exit }' <<<"$config")
  [[ -n "$MEMORY_SCALING" ]] || fail "deviceMemoryScaling not found in ${SCHEDULER_DEVICE_CONFIGMAP}"
  validate_memory_scaling "$MEMORY_SCALING"

  REQUEST_MIB=$((PHYSICAL_MEMORY_MIB * REQUEST_PERCENT / 100))
  ALLOCATION_MIB=$((REQUEST_MIB * ALLOCATE_PERCENT / 100))
  log "GPU=${RESOLVED_GPU_NAME} physical=${PHYSICAL_MEMORY_MIB}MiB scaling=${MEMORY_SCALING} request=${REQUEST_MIB}MiB allocation=${ALLOCATION_MIB}MiB"

  trap on_exit EXIT
  kubectl create namespace "$NS"
  CLEANUP_ENABLED=true
  create_probe_configmap
  pod_manifest vram-holder holder "$REQUEST_MIB" "$ALLOCATION_MIB" | kubectl apply -f -
  wait_for_log vram-holder HOLDER_READY || fail "holder did not allocate and touch VRAM"
  local before after phase
  before=$(last_heartbeat vram-holder)

  pod_manifest vram-challenger expect-oom "$REQUEST_MIB" "$ALLOCATION_MIB" | kubectl apply -f -
  wait_for_log vram-challenger EXPECTED_OOM || fail "challenger did not report CUDA_ERROR_OUT_OF_MEMORY"
  kubectl wait -n "$NS" --for=jsonpath='{.status.phase}'=Succeeded pod/vram-challenger --timeout=120s
  sleep 5
  after=$(last_heartbeat vram-holder)
  phase=$(kubectl get pod vram-holder -n "$NS" -o jsonpath='{.status.phase}')
  [[ "$phase" == Running && "$after" -gt "$before" ]] ||
    fail "holder did not remain healthy after challenger OOM: phase=${phase} before=${before} after=${after}"

  log "PASS: challenger received expected CUDA OOM; holder progressed from heartbeat ${before} to ${after}"
}

case "${1:-}" in
  --help|-h)
    usage
    ;;
  --print-manifests)
    [[ $# -eq 4 ]] || fail "--print-manifests requires node, GPU UUID, and physical MiB"
    validate_percentages
    print_manifests "$2" "$3" "$4"
    ;;
  "")
    run_hardware_e2e
    ;;
  *)
    usage >&2
    exit 2
    ;;
esac
