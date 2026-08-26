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
TARGET_GPU_UUID=${TARGET_GPU_UUID:-}
TARGET_GPU_INDEX=${TARGET_GPU_INDEX:-}
MIG_E2E_PRESET=${MIG_E2E_PRESET:-a100-40gb}
MIG_E2E_CONFIRM_NODE_WIDE_RESTART=${MIG_E2E_CONFIRM_NODE_WIDE_RESTART:-false}
DEVICE_PLUGIN_DAEMONSET=${DEVICE_PLUGIN_DAEMONSET:-hami-device-plugin}
DEVICE_PLUGIN_CONTAINER=${DEVICE_PLUGIN_CONTAINER:-device-plugin}
IMAGE=${GPU_TEST_IMAGE:-nvcr.io/nvidia/k8s/cuda-sample:vectoradd-cuda12.5.0-ubuntu22.04}
GPU_PROGRESS_WAIT=${GPU_PROGRESS_WAIT:-5}
CLEANUP_ENABLED=false

log() { printf '\n[%s] %s\n' "$(date -u +%H:%M:%S)" "$*"; }
fail() { echo "FAIL: $*" >&2; exit 1; }

usage() {
  cat <<'EOF'
Usage:
  TARGET_NODE=<node> bash hack/hami-mig-e2e.sh
  bash hack/hami-mig-e2e.sh --print-preset [a100-40gb|rtx-pro-6000]

Environment:
  MIG_E2E_PRESET                     GPU scenario preset (default: a100-40gb)
  TARGET_GPU_UUID                    Exact physical GPU UUID to test
  TARGET_GPU_INDEX                   Physical GPU index to test
  MIG_E2E_CONFIRM_NODE_WIDE_RESTART  Must be true on a multi-GPU node after
                                     the entire node has been drained

A one-GPU node is selected automatically. A multi-GPU node requires an exact
UUID or index; every workload is then pinned to the resolved UUID. Restarting
the HAMi device plugin reconciles every physical GPU, so a multi-GPU run also
requires the explicit node-wide restart confirmation and an idle-node check.
EOF
}

load_preset() {
  local preset=$1

  case "$preset" in
    a100-40gb)
      PRESET_NAME=a100-40gb
      PRESET_GPU_NAME_PATTERN='A100.*40GB'
      SMALL_PROFILE=1g.5gb
      SMALL_MEMORY=4500
      MEDIUM_PROFILE=2g.10gb
      MEDIUM_MEMORY=9500
      LARGE_PROFILE=3g.20gb
      LARGE_MEMORY=19000
      SMALL_CAPACITY=7
      ;;
    rtx-pro-6000)
      PRESET_NAME=rtx-pro-6000
      PRESET_GPU_NAME_PATTERN='RTX PRO 6000 Blackwell Server Edition'
      SMALL_PROFILE=1g.24gb
      SMALL_MEMORY=8000
      MEDIUM_PROFILE=2g.48gb
      MEDIUM_MEMORY=30000
      LARGE_PROFILE=4g.96gb
      LARGE_MEMORY=60000
      SMALL_CAPACITY=4
      ;;
    *)
      fail "unknown MIG_E2E_PRESET ${preset}; expected a100-40gb or rtx-pro-6000"
      ;;
  esac
}

print_preset() {
  cat <<EOF
preset=${PRESET_NAME}
gpu_name_pattern=${PRESET_GPU_NAME_PATTERN}
small_profile=${SMALL_PROFILE}
small_memory_mib=${SMALL_MEMORY}
medium_profile=${MEDIUM_PROFILE}
medium_memory_mib=${MEDIUM_MEMORY}
large_profile=${LARGE_PROFILE}
large_memory_mib=${LARGE_MEMORY}
small_capacity=${SMALL_CAPACITY}
EOF
}

trim_whitespace() {
  local value=$1
  value=${value#"${value%%[![:space:]]*}"}
  value=${value%"${value##*[![:space:]]}"}
  printf '%s' "$value"
}

# Resolve a CSV inventory produced by:
# nvidia-smi --query-gpu=index,uuid,name --format=csv,noheader,nounits
# The result is returned in RESOLVED_TARGET_GPU_* globals for easy shell use.
resolve_target_gpu() {
  local inventory=$1 requested_uuid=$2 requested_index=$3
  local line index uuid name i selected=-1
  local -a indices=() uuids=() names=()

  if [[ -n "$requested_index" && ! "$requested_index" =~ ^(0|[1-9][0-9]*)$ ]]; then
    fail "TARGET_GPU_INDEX must be a non-negative integer, got ${requested_index}"
  fi
  if [[ -n "$requested_uuid" && ! "$requested_uuid" =~ ^GPU-[0-9A-Fa-f-]+$ ]]; then
    fail "TARGET_GPU_UUID must be a complete GPU-* UUID, got ${requested_uuid}"
  fi

  while IFS= read -r line || [[ -n "$line" ]]; do
    [[ -n "$line" ]] || continue
    IFS=',' read -r index uuid name <<<"$line"
    index=$(trim_whitespace "$index")
    uuid=$(trim_whitespace "$uuid")
    name=$(trim_whitespace "$name")
    [[ "$index" =~ ^(0|[1-9][0-9]*)$ ]] || fail "invalid GPU index in nvidia-smi inventory: ${line}"
    [[ "$uuid" =~ ^GPU-[0-9A-Fa-f-]+$ ]] || fail "invalid GPU UUID in nvidia-smi inventory: ${line}"
    [[ -n "$name" ]] || fail "missing GPU name in nvidia-smi inventory: ${line}"

    for ((i = 0; i < ${#indices[@]}; i++)); do
      [[ "${indices[$i]}" != "$index" ]] || fail "duplicate GPU index ${index} in nvidia-smi inventory"
      [[ "${uuids[$i]}" != "$uuid" ]] || fail "duplicate GPU UUID ${uuid} in nvidia-smi inventory"
    done
    indices+=("$index")
    uuids+=("$uuid")
    names+=("$name")
  done <<<"$inventory"

  PHYSICAL_GPU_COUNT=${#indices[@]}
  ((PHYSICAL_GPU_COUNT > 0)) || fail "nvidia-smi reported no physical GPUs"

  TARGET_GPU_SELECTION_EXPLICIT=false
  if [[ -n "$requested_uuid" || -n "$requested_index" ]]; then
    TARGET_GPU_SELECTION_EXPLICIT=true
  elif ((PHYSICAL_GPU_COUNT != 1)); then
    printf '%s\n' "$inventory" >&2
    fail "${PHYSICAL_GPU_COUNT} physical GPUs found; set exactly one TARGET_GPU_UUID or TARGET_GPU_INDEX"
  fi

  for ((i = 0; i < PHYSICAL_GPU_COUNT; i++)); do
    [[ -z "$requested_uuid" || "${uuids[$i]}" == "$requested_uuid" ]] || continue
    [[ -z "$requested_index" || "${indices[$i]}" == "$requested_index" ]] || continue
    ((selected == -1)) || fail "GPU selector matched more than one inventory row"
    selected=$i
  done
  ((selected >= 0)) || fail "GPU selector did not match exactly one physical GPU"

  RESOLVED_TARGET_GPU_INDEX=${indices[$selected]}
  RESOLVED_TARGET_GPU_UUID=${uuids[$selected]}
  RESOLVED_TARGET_GPU_NAME=${names[$selected]}
}

extract_target_gpu_listing() {
  local listing=$1 target_uuid=$2
  awk -v target_uuid="$target_uuid" '
    /^GPU [0-9]+:/ {
      if (printing) {
        exit
      }
      printing = index($0, "(UUID: " target_uuid ")") > 0
      if (printing) {
        found = 1
      }
    }
    printing { print }
    END {
      if (!found) {
        exit 1
      }
    }
  ' <<<"$listing"
}

cleanup() {
  kubectl delete namespace "$NS" --wait=false >/dev/null 2>&1 || true
  for _ in $(seq 1 90); do
    kubectl get namespace "$NS" >/dev/null 2>&1 || return 0
    sleep 2
  done
  echo "FAIL: namespace cleanup timed out" >&2
  return 1
}

on_exit() {
  local status=$?
  trap - EXIT
  if [[ "$CLEANUP_ENABLED" == true ]]; then
    cleanup || true
  fi
  exit "$status"
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

target_gpu_listing() {
  local listing selected
  listing=$(node_nvidia_smi -L)
  if ! selected=$(extract_target_gpu_listing "$listing" "$TARGET_GPU_UUID"); then
    printf '%s\n' "$listing" >&2
    fail "target GPU ${TARGET_GPU_UUID} was not present in nvidia-smi -L output"
  fi
  printf '%s\n' "$selected"
}

validate_registered_target() {
  local registration target count registered_index registered_mode profile
  if ! registration=$(kubectl get node "$TARGET_NODE" -o json | jq -er '.metadata.annotations["hami.io/node-nvidia-register"] | fromjson'); then
    fail "node ${TARGET_NODE} lacks a valid hami.io/node-nvidia-register annotation"
  fi

  count=$(jq -r --arg uuid "$TARGET_GPU_UUID" '[.[] | select(.id == $uuid)] | length' <<<"$registration")
  [[ "$count" == 1 ]] || fail "target GPU ${TARGET_GPU_UUID} must appear exactly once in HAMi registration, found ${count}"
  target=$(jq -c --arg uuid "$TARGET_GPU_UUID" '.[] | select(.id == $uuid)' <<<"$registration")
  registered_index=$(jq -r '.index | tostring' <<<"$target")
  registered_mode=$(jq -r '.mode // ""' <<<"$target")
  [[ "$registered_index" == "$TARGET_GPU_INDEX" ]] || fail "target GPU index mismatch: nvidia-smi=${TARGET_GPU_INDEX} HAMi=${registered_index}"
  [[ "$registered_mode" == mig ]] || fail "target GPU ${TARGET_GPU_UUID} is registered in ${registered_mode:-unknown} mode, not mig"

  for profile in "$SMALL_PROFILE" "$MEDIUM_PROFILE" "$LARGE_PROFILE"; do
    jq -e --arg profile "$profile" 'any(.migProfiles[]?; .name == $profile and (.placements | length > 0))' <<<"$target" >/dev/null ||
      fail "target GPU registration lacks legal placements for preset profile ${profile}"
  done
}

assert_no_foreign_gpu_pods() {
  local foreign
  foreign=$(kubectl get pods -A --field-selector "spec.nodeName=${TARGET_NODE}" -o json | jq -r --arg namespace "$NS" '
    .items[]
    | select(.metadata.namespace != $namespace)
    | select(.status.phase != "Succeeded" and .status.phase != "Failed")
    | select(
        ((.metadata.annotations["hami.io/vgpu-devices-allocated"] // "") | length > 0)
        or any(
          (.spec.initContainers[]?, .spec.containers[]?);
          (((.resources.limits // {}) + (.resources.requests // {})) | keys | any(startswith("nvidia.com/")))
        )
      )
    | "\(.metadata.namespace)/\(.metadata.name)"
  ')
  [[ -z "$foreign" ]] || fail "node-wide plugin restart is unsafe; foreign GPU Pods remain on ${TARGET_NODE}: ${foreign//$'\n'/, }"
}

mig_lines_count() {
  grep -c 'MIG ' || true
}

assert_no_foreign_mig_instances() {
  local all_count target_count listing
  listing=$(node_nvidia_smi -L)
  all_count=$(mig_lines_count <<<"$listing")
  target_count=$(target_gpu_listing | mig_lines_count)
  if [[ "$all_count" != "$target_count" ]]; then
    printf '%s\n' "$listing" >&2
    fail "node-wide plugin restart is unsafe; MIG instances exist outside target GPU ${TARGET_GPU_UUID}"
  fi
}

compute_apps() {
  node_nvidia_smi --query-compute-apps=gpu_uuid,pid,process_name --format=csv,noheader,nounits
}

assert_no_gpu_compute_processes() {
  local apps
  apps=$(compute_apps)
  [[ -z "$apps" ]] || fail "GPU compute processes remain before the test baseline: ${apps//$'\n'/; }"
}

assert_compute_apps_only_on_target() {
  local apps target_mig_uuids line app_uuid
  apps=$(compute_apps)
  [[ -n "$apps" ]] || return 0
  target_mig_uuids=$(target_gpu_listing | sed -n 's/.*UUID: \(MIG-[^)]*\)).*/\1/p')

  while IFS= read -r line || [[ -n "$line" ]]; do
    [[ -n "$line" ]] || continue
    IFS=',' read -r app_uuid _ <<<"$line"
    app_uuid=$(trim_whitespace "$app_uuid")
    if [[ "$app_uuid" == "$TARGET_GPU_UUID" ]] || grep -Fxq -- "$app_uuid" <<<"$target_mig_uuids"; then
      continue
    fi
    fail "node-wide plugin restart is unsafe; compute process is active outside target GPU: ${line}"
  done <<<"$apps"
}

assert_multi_gpu_preflight_safe() {
  ((PHYSICAL_GPU_COUNT > 1)) || return 0
  [[ "$TARGET_GPU_SELECTION_EXPLICIT" == true ]] || fail "multi-GPU test requires an explicit target GPU"
  [[ "$MIG_E2E_CONFIRM_NODE_WIDE_RESTART" == true ]] ||
    fail "device-plugin restart reconciles all ${PHYSICAL_GPU_COUNT} GPUs; drain the node, then set MIG_E2E_CONFIRM_NODE_WIDE_RESTART=true"
  assert_no_foreign_gpu_pods
  assert_no_foreign_mig_instances
  assert_compute_apps_only_on_target
}

assert_multi_gpu_empty_baseline() {
  ((PHYSICAL_GPU_COUNT > 1)) || return 0
  assert_no_foreign_gpu_pods
  assert_no_foreign_mig_instances
  assert_no_gpu_compute_processes
}

assert_restart_safe() {
  ((PHYSICAL_GPU_COUNT > 1)) || return 0
  assert_no_foreign_gpu_pods
  assert_no_foreign_mig_instances
  assert_compute_apps_only_on_target
}

restart_target_device_plugin() {
  local old_pod new_pod ready
  assert_restart_safe
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
  cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: ${name}
  namespace: ${NS}
  annotations:
    nvidia.com/vgpu-mode: "mig"
    hami.io/gpu-scheduler-policy: "binpack"
    nvidia.com/use-gpuuuid: "${TARGET_GPU_UUID}"
spec:
  schedulerName: hami-scheduler
  nodeSelector:
    kubernetes.io/hostname: ${TARGET_NODE}
  restartPolicy: Never
  containers:
    - name: cuda
      image: ${IMAGE}
      imagePullPolicy: IfNotPresent
      command:
        - bash
        - -lc
        - |
          set -euo pipefail
          nvidia-smi -L
          n=0
          echo 0 > /tmp/gpu-progress
          while true; do
            if ! /cuda-samples/vectorAdd > /tmp/vectoradd.last 2>&1; then
              cat /tmp/vectoradd.last >&2
              exit 1
            fi
            n=\$((n + 1))
            echo "\$n" > /tmp/gpu-progress.next
            mv /tmp/gpu-progress.next /tmp/gpu-progress
          done
      resources:
        limits:
          nvidia.com/gpu: 1
          nvidia.com/gpumem: ${memory}
EOF
}

wait_ready() { kubectl wait -n "$NS" --for=condition=Ready "pod/$1" --timeout=180s; }

wait_ready_many() {
  local pod pid status=0
  local -a pids=()
  for pod in "$@"; do
    wait_ready "$pod" &
    pids+=("$!")
  done
  for pid in "${pids[@]}"; do
    wait "$pid" || status=1
  done
  ((status == 0)) || fail "one or more Pods did not become Ready"
}

mig_count() { target_gpu_listing | mig_lines_count; }
profile_count() { target_gpu_listing | grep -c "MIG $1" || true; }
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
    (( $(date +%s) - start < timeout )) || { target_gpu_listing; fail "MIG count=${count}, want=${want} on ${TARGET_GPU_UUID}"; }
    sleep 2
  done
}

wait_uuid_absent() {
  local uuid=$1 timeout=${2:-120} start
  start=$(date +%s)
  while true; do
    if ! target_gpu_listing | grep -Fq "UUID: ${uuid})"; then
      echo "MIG_UUID_RECLAIMED=${uuid}"
      return 0
    fi
    (( $(date +%s) - start < timeout )) || { target_gpu_listing; fail "MIG UUID ${uuid} was not reclaimed"; }
    sleep 2
  done
}

assert_uuid() {
  local pod=$1 want=$2 got
  got=$(pod_uuid "$pod")
  [[ -n "$got" && "$got" == "$want" ]] || fail "$pod UUID changed: got=${got} want=${want}"
}

assert_runtime_annotation() {
  local pod=$1 expected_profile=$2 raw
  raw=$(kubectl get pod -n "$NS" "$pod" -o json | jq -r '.metadata.annotations["hami.io/vgpu-mig-allocations"]')
  jq -e --arg target "$TARGET_GPU_UUID" --arg profile "$expected_profile" '
    type == "array" and length == 1 and all(.[];
      (.gpuUUID == $target)
      and (.profile == $profile)
      and (.migUUID | startswith("MIG-"))
      and (.placement.size > 0)
      and (.gpuInstanceID | type == "number")
      and (.computeInstanceID | type == "number")
    )
  ' <<<"$raw" >/dev/null || fail "$pod lacks the expected concrete MIG runtime identity on ${TARGET_GPU_UUID}"
}

assert_pending_unbound() {
  local pod=$1 phase node
  sleep 20
  phase=$(kubectl get pod -n "$NS" "$pod" -o jsonpath='{.status.phase}')
  node=$(kubectl get pod -n "$NS" "$pod" -o jsonpath='{.spec.nodeName}')
  [[ "$phase" == Pending && -z "$node" ]] || fail "$pod expected Pending/unbound, got phase=${phase} node=${node}"
}

assert_restart_log_target() {
  local logs
  logs=$(kubectl logs -n "$HAMI_NS" "$(device_plugin_pod)" -c "$DEVICE_PLUGIN_CONTAINER" --since=3m)
  if ! grep -Fq "inUseGPUs=[${TARGET_GPU_INDEX}]" <<<"$logs" &&
    ! grep -Fq "inUseGPUs=\"[${TARGET_GPU_INDEX}]\"" <<<"$logs"; then
    fail "device-plugin startup did not preserve target GPU index ${TARGET_GPU_INDEX} as the sole busy GPU"
  fi
}

delete_pods_and_wait() {
  local pod
  kubectl delete pod -n "$NS" "$@" --wait=false
  for pod in "$@"; do
    kubectl wait -n "$NS" --for=delete "pod/$pod" --timeout=120s || true
  done
}

run_a100_40gb_cases() {
  local p1 p2 p3 p4 i pod refill_index reclaim_count survivor_count
  local uuid_one_a uuid_one_b uuid_two_a uuid_two_b uuid_two_c uuid_overflow
  local uuid_three_a uuid_two_d uuid_one_d
  local progress_before_restart progress_before_reclaim progress_before_mixed_reclaim progress_before_burst_reclaim
  local -a burst_pods=() reclaimed_pods=() survivor_pods=() survivor_uuids=()
  local -a refill_pods=() active_pods=()

  log "CASE 1: two concurrent 1g plus a mixed 2g"
  create_pod one-a "$SMALL_MEMORY"
  create_pod one-b "$SMALL_MEMORY"
  wait_ready one-a & p1=$!
  wait_ready one-b & p2=$!
  wait "$p1"; wait "$p2"
  wait_count 2
  [[ "$(profile_count "$SMALL_PROFILE")" == 2 ]] || fail "expected two ${SMALL_PROFILE}"
  uuid_one_a=$(pod_uuid one-a)
  uuid_one_b=$(pod_uuid one-b)
  create_pod two-a "$MEDIUM_MEMORY"
  wait_ready two-a
  wait_count 3
  [[ "$(profile_count "$SMALL_PROFILE")" == 2 && "$(profile_count "$MEDIUM_PROFILE")" == 1 ]] || fail "1g/2g mixed layout missing"
  uuid_two_a=$(pod_uuid two-a)
  assert_runtime_annotation one-a "$SMALL_PROFILE"
  assert_runtime_annotation one-b "$SMALL_PROFILE"
  assert_runtime_annotation two-a "$MEDIUM_PROFILE"
  assert_gpu_progress one-a
  assert_gpu_progress one-b
  assert_gpu_progress two-a
  echo "PASS CASE 1 one-a=${uuid_one_a} one-b=${uuid_one_b} two-a=${uuid_two_a}"

  log "CASE 2: reach exact 1x1g + 3x2g capacity and reject overflow"
  kubectl delete pod -n "$NS" one-b --wait=true
  wait_count 2
  assert_uuid one-a "$uuid_one_a"
  assert_uuid two-a "$uuid_two_a"
  create_pod two-b "$MEDIUM_MEMORY"
  create_pod two-c "$MEDIUM_MEMORY"
  wait_ready two-b & p1=$!
  wait_ready two-c & p2=$!
  wait "$p1"; wait "$p2"
  wait_count 4
  [[ "$(profile_count "$SMALL_PROFILE")" == 1 && "$(profile_count "$MEDIUM_PROFILE")" == 3 ]] || fail "expected 1x1g + 3x2g"
  uuid_two_b=$(pod_uuid two-b)
  uuid_two_c=$(pod_uuid two-c)
  assert_gpu_progress one-a
  assert_gpu_progress two-a
  assert_gpu_progress two-b
  assert_gpu_progress two-c
  create_pod overflow-one "$SMALL_MEMORY"
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
  [[ "$(profile_count "$SMALL_PROFILE")" == 1 && "$(profile_count "$MEDIUM_PROFILE")" == 3 ]] || fail "layout changed during restart"
  assert_restart_log_target
  echo "PASS CASE 3 all UUIDs preserved"

  log "CASE 4: immediate delete/replacement after restart"
  progress_before_reclaim=$(snapshot_gpu_progress one-a two-a two-c)
  kubectl delete pod -n "$NS" two-b --wait=true
  wait_ready overflow-one
  assert_gpu_progress_since "$progress_before_reclaim" one-a two-a two-c
  assert_gpu_progress overflow-one
  wait_count 4
  [[ "$(profile_count "$SMALL_PROFILE")" == 2 && "$(profile_count "$MEDIUM_PROFILE")" == 2 ]] || fail "replacement layout incorrect"
  assert_uuid one-a "$uuid_one_a"
  assert_uuid two-a "$uuid_two_a"
  assert_uuid two-c "$uuid_two_c"
  uuid_overflow=$(pod_uuid overflow-one)
  [[ -n "$uuid_overflow" ]] || fail "replacement 1g has no MIG UUID"
  echo "PASS CASE 4 replacement=${uuid_overflow}"

  log "reset before 3g topology"
  delete_pods_and_wait one-a two-a two-c overflow-one
  wait_count 0

  log "CASE 5: scheduler rejects topology-infeasible 1g beside 2x3g"
  create_pod three-a "$LARGE_MEMORY"
  create_pod three-b "$LARGE_MEMORY"
  wait_ready three-a & p1=$!
  wait_ready three-b & p2=$!
  wait "$p1"; wait "$p2"
  wait_count 2
  [[ "$(profile_count "$LARGE_PROFILE")" == 2 ]] || fail "expected two ${LARGE_PROFILE} instances"
  assert_gpu_progress three-a
  assert_gpu_progress three-b
  create_pod blocked-one "$SMALL_MEMORY"
  assert_pending_unbound blocked-one
  [[ "$(kubectl get pod -n "$NS" blocked-one -o jsonpath='{.status.reason}')" != UnexpectedAdmissionError ]] || fail "topology-infeasible Pod reached admission"
  echo "PASS CASE 5 topology rejected before Bind"

  log "reset before balanced topology"
  delete_pods_and_wait three-a three-b blocked-one
  wait_count 0

  log "CASE 6: 1x3g + 1x2g + 2x1g exact capacity, overflow, and immediate replacement"
  create_pod three-a "$LARGE_MEMORY"
  create_pod two-d "$MEDIUM_MEMORY"
  create_pod one-c "$SMALL_MEMORY"
  create_pod one-d "$SMALL_MEMORY"
  wait_ready three-a & p1=$!
  wait_ready two-d & p2=$!
  wait_ready one-c & p3=$!
  wait_ready one-d & p4=$!
  wait "$p1"; wait "$p2"; wait "$p3"; wait "$p4"
  wait_count 4
  [[ "$(profile_count "$LARGE_PROFILE")" == 1 && "$(profile_count "$MEDIUM_PROFILE")" == 1 && "$(profile_count "$SMALL_PROFILE")" == 2 ]] || fail "expected 1x3g + 1x2g + 2x1g"
  uuid_three_a=$(pod_uuid three-a)
  uuid_two_d=$(pod_uuid two-d)
  uuid_one_d=$(pod_uuid one-d)
  assert_gpu_progress three-a
  assert_gpu_progress two-d
  assert_gpu_progress one-c
  assert_gpu_progress one-d
  create_pod overflow-two "$SMALL_MEMORY"
  assert_pending_unbound overflow-two
  progress_before_mixed_reclaim=$(snapshot_gpu_progress three-a two-d one-d)
  kubectl delete pod -n "$NS" one-c --wait=true
  wait_ready overflow-two
  assert_gpu_progress_since "$progress_before_mixed_reclaim" three-a two-d one-d
  assert_gpu_progress overflow-two
  wait_count 4
  [[ "$(profile_count "$LARGE_PROFILE")" == 1 && "$(profile_count "$MEDIUM_PROFILE")" == 1 && "$(profile_count "$SMALL_PROFILE")" == 2 ]] || fail "mixed replacement layout incorrect"
  assert_uuid three-a "$uuid_three_a"
  assert_uuid two-d "$uuid_two_d"
  assert_uuid one-d "$uuid_one_d"
  echo "PASS CASE 6"

  log "reset before full-capacity burst"
  delete_pods_and_wait three-a two-d one-d overflow-two
  wait_count 0

  log "CASE 7: ${SMALL_CAPACITY} concurrent 1g instances, partial reclaim and refill"
  for i in $(seq 1 "$SMALL_CAPACITY"); do
    pod="burst-${i}"
    burst_pods+=("$pod")
    create_pod "$pod" "$SMALL_MEMORY"
  done
  wait_ready_many "${burst_pods[@]}"
  wait_count "$SMALL_CAPACITY" 180
  [[ "$(profile_count "$SMALL_PROFILE")" == "$SMALL_CAPACITY" ]] || fail "expected ${SMALL_CAPACITY} ${SMALL_PROFILE} instances"
  for pod in "${burst_pods[@]}"; do assert_gpu_progress "$pod"; done

  reclaim_count=$((SMALL_CAPACITY / 2))
  survivor_count=$((SMALL_CAPACITY - reclaim_count))
  ((reclaim_count > 0)) || fail "SMALL_CAPACITY must allow at least one reclaim"
  for i in $(seq 1 "$SMALL_CAPACITY"); do
    pod="burst-${i}"
    if ((i % 2 == 1 && ${#reclaimed_pods[@]} < reclaim_count)); then
      reclaimed_pods+=("$pod")
    else
      survivor_pods+=("$pod")
      survivor_uuids+=("$(pod_uuid "$pod")")
    fi
  done

  progress_before_burst_reclaim=$(snapshot_gpu_progress "${survivor_pods[@]}")
  delete_pods_and_wait "${reclaimed_pods[@]}"
  wait_count "$survivor_count"
  assert_gpu_progress_since "$progress_before_burst_reclaim" "${survivor_pods[@]}"
  for i in "${!survivor_pods[@]}"; do
    assert_uuid "${survivor_pods[$i]}" "${survivor_uuids[$i]}"
  done

  for i in $(seq 1 "$reclaim_count"); do
    refill_index=$((SMALL_CAPACITY + i))
    pod="burst-${refill_index}"
    refill_pods+=("$pod")
    create_pod "$pod" "$SMALL_MEMORY"
  done
  wait_ready_many "${refill_pods[@]}"
  wait_count "$SMALL_CAPACITY" 180
  [[ "$(profile_count "$SMALL_PROFILE")" == "$SMALL_CAPACITY" ]] || fail "refill did not restore ${SMALL_CAPACITY} ${SMALL_PROFILE} instances"
  active_pods=("${survivor_pods[@]}" "${refill_pods[@]}")
  for pod in "${active_pods[@]}"; do assert_gpu_progress "$pod"; done
  echo "PASS CASE 7"
}

run_rtx_pro_6000_cases() {
  local uuid_small uuid_medium uuid_replacement uuid_b2 uuid_b3 uuid_b4 uuid_full
  local progress_before_reclaim progress_before_recreate progress_before_restart progress_before_refill

  log "CASE 1: mixed ${SMALL_PROFILE} and ${MEDIUM_PROFILE} on the selected RTX PRO 6000"
  create_pod mixed-small "$SMALL_MEMORY"
  create_pod mixed-large "$MEDIUM_MEMORY"
  wait_ready_many mixed-small mixed-large
  wait_count 2
  [[ "$(profile_count "$SMALL_PROFILE")" == 1 && "$(profile_count "$MEDIUM_PROFILE")" == 1 ]] || fail "RTX mixed-profile layout missing"
  uuid_small=$(pod_uuid mixed-small)
  uuid_medium=$(pod_uuid mixed-large)
  [[ -n "$uuid_small" && -n "$uuid_medium" ]] || fail "RTX mixed workloads lack MIG UUIDs"
  assert_runtime_annotation mixed-small "$SMALL_PROFILE"
  assert_runtime_annotation mixed-large "$MEDIUM_PROFILE"
  assert_gpu_progress mixed-small
  assert_gpu_progress mixed-large
  echo "PASS CASE 1 mixed-small=${uuid_small} mixed-large=${uuid_medium}"

  log "CASE 2: reclaim only ${SMALL_PROFILE}, preserve its neighbor, and reuse the placement"
  progress_before_reclaim=$(snapshot_gpu_progress mixed-large)
  kubectl delete pod -n "$NS" mixed-small --wait=true
  wait_count 1
  wait_uuid_absent "$uuid_small"
  [[ "$(profile_count "$SMALL_PROFILE")" == 0 && "$(profile_count "$MEDIUM_PROFILE")" == 1 ]] || fail "small RTX MIG instance was not reclaimed exactly"
  assert_uuid mixed-large "$uuid_medium"
  assert_gpu_progress_since "$progress_before_reclaim" mixed-large
  progress_before_recreate=$(snapshot_gpu_progress mixed-large)
  create_pod mixed-small-replacement "$SMALL_MEMORY"
  wait_ready mixed-small-replacement
  wait_count 2
  [[ "$(profile_count "$SMALL_PROFILE")" == 1 && "$(profile_count "$MEDIUM_PROFILE")" == 1 ]] || fail "RTX mixed placement was not reusable"
  assert_uuid mixed-large "$uuid_medium"
  assert_gpu_progress_since "$progress_before_recreate" mixed-large
  assert_gpu_progress mixed-small-replacement
  uuid_replacement=$(pod_uuid mixed-small-replacement)
  assert_runtime_annotation mixed-small-replacement "$SMALL_PROFILE"
  echo "PASS CASE 2 replacement=${uuid_replacement}"

  log "CASE 3: busy device-plugin restart preserves both RTX MIG UUIDs and CUDA progress"
  progress_before_restart=$(snapshot_gpu_progress mixed-large mixed-small-replacement)
  restart_target_device_plugin
  sleep 15
  assert_gpu_progress_since "$progress_before_restart" mixed-large mixed-small-replacement
  assert_uuid mixed-large "$uuid_medium"
  assert_uuid mixed-small-replacement "$uuid_replacement"
  wait_count 2
  [[ "$(profile_count "$SMALL_PROFILE")" == 1 && "$(profile_count "$MEDIUM_PROFILE")" == 1 ]] || fail "RTX mixed layout changed during restart"
  assert_restart_log_target
  echo "PASS CASE 3 both RTX MIG UUIDs preserved"

  log "reset before four-way RTX saturation"
  delete_pods_and_wait mixed-large mixed-small-replacement
  wait_count 0

  log "CASE 4: four ${SMALL_PROFILE} instances saturate one RTX GPU and reject overflow"
  for i in $(seq 1 "$SMALL_CAPACITY"); do create_pod "burst-${i}" "$SMALL_MEMORY"; done
  wait_ready_many burst-1 burst-2 burst-3 burst-4
  wait_count "$SMALL_CAPACITY" 180
  [[ "$(profile_count "$SMALL_PROFILE")" == "$SMALL_CAPACITY" ]] || fail "expected ${SMALL_CAPACITY} ${SMALL_PROFILE} instances"
  for i in $(seq 1 "$SMALL_CAPACITY"); do
    assert_runtime_annotation "burst-${i}" "$SMALL_PROFILE"
    assert_gpu_progress "burst-${i}"
  done
  create_pod overflow-small "$SMALL_MEMORY"
  assert_pending_unbound overflow-small
  wait_count "$SMALL_CAPACITY"
  echo "PASS CASE 4 RTX capacity enforced"

  log "CASE 5: saturated RTX placement reclaims and refills without disturbing neighbors"
  uuid_b2=$(pod_uuid burst-2)
  uuid_b3=$(pod_uuid burst-3)
  uuid_b4=$(pod_uuid burst-4)
  progress_before_refill=$(snapshot_gpu_progress burst-2 burst-3 burst-4)
  kubectl delete pod -n "$NS" burst-1 --wait=true
  wait_ready overflow-small
  wait_count "$SMALL_CAPACITY"
  [[ "$(profile_count "$SMALL_PROFILE")" == "$SMALL_CAPACITY" ]] || fail "RTX refill did not restore exact capacity"
  assert_uuid burst-2 "$uuid_b2"
  assert_uuid burst-3 "$uuid_b3"
  assert_uuid burst-4 "$uuid_b4"
  assert_gpu_progress_since "$progress_before_refill" burst-2 burst-3 burst-4
  assert_runtime_annotation overflow-small "$SMALL_PROFILE"
  assert_gpu_progress overflow-small
  echo "PASS CASE 5 RTX placement reclaimed and refilled"

  log "reset before full-GPU RTX profile"
  delete_pods_and_wait burst-2 burst-3 burst-4 overflow-small
  wait_count 0

  log "CASE 6: ${LARGE_PROFILE} consumes the RTX MIG topology and rejects another slice"
  create_pod full-a "$LARGE_MEMORY"
  wait_ready full-a
  wait_count 1
  [[ "$(profile_count "$LARGE_PROFILE")" == 1 ]] || fail "expected one ${LARGE_PROFILE} instance"
  uuid_full=$(pod_uuid full-a)
  assert_runtime_annotation full-a "$LARGE_PROFILE"
  assert_gpu_progress full-a
  create_pod blocked-small "$SMALL_MEMORY"
  assert_pending_unbound blocked-small
  wait_count 1
  assert_uuid full-a "$uuid_full"
  assert_gpu_progress full-a
  echo "PASS CASE 6 RTX full profile capacity enforced"
}

run_hardware_e2e() {
  local inventory

  trap on_exit EXIT
  [[ -n "$TARGET_NODE" ]] || fail "set TARGET_NODE to the Kubernetes node under test"
  kubectl get node "$TARGET_NODE" >/dev/null || fail "target node ${TARGET_NODE} does not exist"
  node_nvidia_smi -L >/dev/null
  inventory=$(node_nvidia_smi --query-gpu=index,uuid,name --format=csv,noheader,nounits)
  resolve_target_gpu "$inventory" "$TARGET_GPU_UUID" "$TARGET_GPU_INDEX"
  TARGET_GPU_INDEX=$RESOLVED_TARGET_GPU_INDEX
  TARGET_GPU_UUID=$RESOLVED_TARGET_GPU_UUID
  TARGET_GPU_NAME=$RESOLVED_TARGET_GPU_NAME

  [[ "$TARGET_GPU_NAME" =~ $PRESET_GPU_NAME_PATTERN ]] ||
    fail "preset ${PRESET_NAME} does not match target GPU model ${TARGET_GPU_NAME}"
  validate_registered_target
  assert_multi_gpu_preflight_safe

  log "preset=${PRESET_NAME} target=${TARGET_NODE} GPU ${TARGET_GPU_INDEX} ${TARGET_GPU_UUID} (${TARGET_GPU_NAME})"
  CLEANUP_ENABLED=true
  log "baseline cleanup on ${TARGET_NODE} GPU ${TARGET_GPU_INDEX}"
  cleanup
  wait_count 0
  assert_no_gpu_compute_processes
  assert_multi_gpu_empty_baseline
  kubectl create namespace "$NS"

  case "$PRESET_NAME" in
    a100-40gb) run_a100_40gb_cases ;;
    rtx-pro-6000) run_rtx_pro_6000_cases ;;
    *) fail "no scenario set for preset ${PRESET_NAME}" ;;
  esac

  log "final cleanup and health"
  cleanup
  wait_count 0
  assert_no_gpu_compute_processes
  assert_multi_gpu_empty_baseline
  kubectl get nodes
  kubectl get pods -n "$HAMI_NS"
  node_nvidia_smi -i "$TARGET_GPU_UUID" -q | grep -A3 'MIG Mode'
  CLEANUP_ENABLED=false
  echo "ALL_FIXED_MIG_E2E_TESTS_PASSED preset=${PRESET_NAME} gpu=${TARGET_GPU_UUID}"
}

main() {
  case "${1:-}" in
    --help|-h)
      usage
      return 0
      ;;
    --print-preset)
      [[ $# -le 2 ]] || fail "--print-preset accepts at most one preset name"
      load_preset "${2:-$MIG_E2E_PRESET}"
      print_preset
      return 0
      ;;
    "")
      load_preset "$MIG_E2E_PRESET"
      run_hardware_e2e
      ;;
    *)
      usage >&2
      fail "unknown argument $1"
      ;;
  esac
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
