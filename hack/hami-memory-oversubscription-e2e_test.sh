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
SCRIPT=${REPO_ROOT}/hack/hami-memory-oversubscription-e2e.sh

fail_test() { echo "FAIL: $*" >&2; exit 1; }
assert_line() { grep -Fqx -- "$2" <<<"$1" || fail_test "missing line: $2"; }
expect_failure() { if "$@" >/dev/null 2>&1; then fail_test "command unexpectedly succeeded: $*"; fi; }

output=$(bash "$SCRIPT" --print-manifests gpu-node GPU-0000 8192)
assert_line "$output" '    nvidia.com/use-gpuuuid: "GPU-0000"'
assert_line "$output" '    kubernetes.io/hostname: gpu-node'
assert_line "$output" '        nvidia.com/gpumem: 4915'
assert_line "$output" '      exec /tmp/vram-probe holder 4423'
assert_line "$output" '      exec /tmp/vram-probe expect-oom 4423'

expect_failure env OVERSUB_REQUEST_PERCENT=0 bash "$SCRIPT" --print-manifests gpu-node GPU-0000 8192
expect_failure env OVERSUB_REQUEST_PERCENT=101 bash "$SCRIPT" --print-manifests gpu-node GPU-0000 8192
expect_failure env OVERSUB_REQUEST_PERCENT=50 OVERSUB_ALLOCATE_PERCENT=100 bash "$SCRIPT" --print-manifests gpu-node GPU-0000 8192
expect_failure bash "$SCRIPT" --print-manifests gpu-node GPU-0000

echo "PASS: memory oversubscription E2E validation and manifests"
