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

REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
SCRIPT=${REPO_ROOT}/hack/hami-mig-e2e.sh
# shellcheck source=hack/hami-mig-e2e.sh
source "$SCRIPT"

fail_test() {
  echo "FAIL: $*" >&2
  exit 1
}

assert_eq() {
  local want=$1 got=$2 message=$3
  [[ "$got" == "$want" ]] || fail_test "${message}: got=${got} want=${want}"
}

assert_output_line() {
  local output=$1 line=$2
  grep -Fxq -- "$line" <<<"$output" || fail_test "missing output line: ${line}"
}

expect_failure() {
  if ("$@" >/dev/null 2>&1); then
    fail_test "command unexpectedly succeeded: $*"
  fi
}

load_preset a100-40gb
assert_eq a100-40gb "$PRESET_NAME" "default preset name"
assert_eq 1g.5gb "$SMALL_PROFILE" "A100 small profile"
assert_eq 4500 "$SMALL_MEMORY" "A100 small request"
assert_eq 2g.10gb "$MEDIUM_PROFILE" "A100 medium profile"
assert_eq 9500 "$MEDIUM_MEMORY" "A100 medium request"
assert_eq 3g.20gb "$LARGE_PROFILE" "A100 large profile"
assert_eq 19000 "$LARGE_MEMORY" "A100 large request"
assert_eq 7 "$SMALL_CAPACITY" "A100 capacity"

load_preset rtx-pro-6000
assert_eq rtx-pro-6000 "$PRESET_NAME" "RTX preset name"
assert_eq 1g.24gb "$SMALL_PROFILE" "RTX small profile"
assert_eq 8000 "$SMALL_MEMORY" "RTX small request"
assert_eq 2g.48gb "$MEDIUM_PROFILE" "RTX medium profile"
assert_eq 30000 "$MEDIUM_MEMORY" "RTX medium request"
assert_eq 4g.96gb "$LARGE_PROFILE" "RTX full profile"
assert_eq 60000 "$LARGE_MEMORY" "RTX full request"
assert_eq 4 "$SMALL_CAPACITY" "RTX capacity"
expect_failure load_preset unsupported

single_inventory='0, GPU-0000, NVIDIA A100-SXM4-40GB'
resolve_target_gpu "$single_inventory" "" ""
assert_eq 1 "$PHYSICAL_GPU_COUNT" "single-GPU inventory count"
assert_eq 0 "$RESOLVED_TARGET_GPU_INDEX" "automatic single-GPU index"
assert_eq GPU-0000 "$RESOLVED_TARGET_GPU_UUID" "automatic single-GPU UUID"
assert_eq false "$TARGET_GPU_SELECTION_EXPLICIT" "automatic selection marker"

multi_inventory=$(cat <<'EOF'
0, GPU-0000, NVIDIA RTX PRO 6000 Blackwell Server Edition
1, GPU-0001, NVIDIA RTX PRO 6000 Blackwell Server Edition
2, GPU-0002, NVIDIA RTX PRO 6000 Blackwell Server Edition
3, GPU-0003, NVIDIA RTX PRO 6000 Blackwell Server Edition
4, GPU-0004, NVIDIA RTX PRO 6000 Blackwell Server Edition
5, GPU-0005, NVIDIA RTX PRO 6000 Blackwell Server Edition
6, GPU-0006, NVIDIA RTX PRO 6000 Blackwell Server Edition
7, GPU-0007, NVIDIA RTX PRO 6000 Blackwell Server Edition
EOF
)

expect_failure resolve_target_gpu "$multi_inventory" "" ""
resolve_target_gpu "$multi_inventory" "" 4
assert_eq 8 "$PHYSICAL_GPU_COUNT" "multi-GPU inventory count"
assert_eq 4 "$RESOLVED_TARGET_GPU_INDEX" "index-selected GPU index"
assert_eq GPU-0004 "$RESOLVED_TARGET_GPU_UUID" "index-selected GPU UUID"
assert_eq true "$TARGET_GPU_SELECTION_EXPLICIT" "explicit selection marker"

resolve_target_gpu "$multi_inventory" GPU-0006 ""
assert_eq 6 "$RESOLVED_TARGET_GPU_INDEX" "UUID-selected GPU index"
assert_eq GPU-0006 "$RESOLVED_TARGET_GPU_UUID" "UUID-selected GPU UUID"
resolve_target_gpu "$multi_inventory" GPU-0004 4
assert_eq GPU-0004 "$RESOLVED_TARGET_GPU_UUID" "matching dual selector"
expect_failure resolve_target_gpu "$multi_inventory" GPU-0004 5
expect_failure resolve_target_gpu "$multi_inventory" GPU-dead ""
expect_failure resolve_target_gpu "$multi_inventory" "" 04

full_listing=$(cat <<'EOF'
GPU 0: NVIDIA RTX PRO 6000 Blackwell Server Edition (UUID: GPU-0000)
GPU 4: NVIDIA RTX PRO 6000 Blackwell Server Edition (UUID: GPU-0004)
  MIG 2g.48gb Device 0: (UUID: MIG-aaaa)
  MIG 1g.24gb Device 1: (UUID: MIG-bbbb)
GPU 7: NVIDIA RTX PRO 6000 Blackwell Server Edition (UUID: GPU-0007)
  MIG 1g.24gb Device 0: (UUID: MIG-cccc)
EOF
)
expected_listing=$(cat <<'EOF'
GPU 4: NVIDIA RTX PRO 6000 Blackwell Server Edition (UUID: GPU-0004)
  MIG 2g.48gb Device 0: (UUID: MIG-aaaa)
  MIG 1g.24gb Device 1: (UUID: MIG-bbbb)
EOF
)
selected_listing=$(extract_target_gpu_listing "$full_listing" GPU-0004)
assert_eq "$expected_listing" "$selected_listing" "target nvidia-smi listing"
expect_failure extract_target_gpu_listing "$full_listing" GPU-dead

preset_output=$(bash "$SCRIPT" --print-preset)
assert_output_line "$preset_output" 'preset=a100-40gb'
assert_output_line "$preset_output" 'small_profile=1g.5gb'
assert_output_line "$preset_output" 'small_memory_mib=4500'
assert_output_line "$preset_output" 'small_capacity=7'

preset_output=$(bash "$SCRIPT" --print-preset rtx-pro-6000)
assert_output_line "$preset_output" 'preset=rtx-pro-6000'
assert_output_line "$preset_output" 'small_profile=1g.24gb'
assert_output_line "$preset_output" 'medium_profile=2g.48gb'
assert_output_line "$preset_output" 'large_profile=4g.96gb'
assert_output_line "$preset_output" 'small_capacity=4'
expect_failure bash "$SCRIPT" --print-preset unsupported

kubectl() {
  [[ "$*" == "apply -f -" ]] || fail_test "unexpected dry-run kubectl arguments: $*"
  cat
}
NS=dry-run
TARGET_NODE=gpu-node
TARGET_GPU_UUID=GPU-0004
IMAGE=example.invalid/vectoradd:test
pod_manifest=$(create_pod dry-run-pod 8000)
assert_output_line "$pod_manifest" '    nvidia.com/use-gpuuuid: "GPU-0004"'
assert_output_line "$pod_manifest" '    kubernetes.io/hostname: gpu-node'
assert_output_line "$pod_manifest" "            n=\$((n + 1))"
assert_output_line "$pod_manifest" "            echo \"\$n\" > /tmp/gpu-progress.next"
assert_output_line "$pod_manifest" '          nvidia.com/gpumem: 8000'
unset -f kubectl

kubectl() {
  printf '%s\n' '{"metadata":{"annotations":{"hami.io/vgpu-mig-allocations":"[{\"gpuUUID\":\"GPU-0004\",\"profile\":\"1g.24gb\",\"migUUID\":\"MIG-aaaa\",\"placement\":{\"start\":9,\"size\":3},\"gpuInstanceID\":6,\"computeInstanceID\":0}]"}}}'
}
TARGET_GPU_UUID=GPU-0004
assert_runtime_annotation dry-run-pod 1g.24gb
expect_failure assert_runtime_annotation dry-run-pod 2g.48gb
TARGET_GPU_UUID=GPU-0005
expect_failure assert_runtime_annotation dry-run-pod 1g.24gb
unset -f kubectl

echo "PASS: hami-mig-e2e preset, target-selection, and manifest helpers"
