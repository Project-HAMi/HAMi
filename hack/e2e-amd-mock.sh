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

# Registers fake AMD GPUs on one worker node and runs the AMD e2e specs that need no
# hardware: the scheduler and webhook decisions, read from the pod allocation annotation.
# Needs a cluster with HAMi installed. Usage: hack/e2e-amd-mock.sh [kubeconfig]

set -o errexit
set -o nounset
set -o pipefail

KUBE_CONF=${1:-"${HOME}/.kube/config"}
REPO_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
cd "${REPO_ROOT}"

NODE=$(kubectl --kubeconfig "${KUBE_CONF}" get nodes -l '!node-role.kubernetes.io/control-plane' -o jsonpath='{.items[0].metadata.name}')
echo "Registering four fake AMD GPUs (two NUMA nodes) on ${NODE}"

# 16 GiB and 32 CUs each, shared by two workloads, like a gfx12 card.
DEVICES=$(jq -nc '[range(4) | {
  id: "GPU-mock-\(.)", index: ., count: 2, devmem: 16304, devcore: 32, type: "AMDGPU",
  numa: (. / 2 | floor), mode: "", health: true, devicevendor: "amd",
  custominfo: {cuPerWGP: 2, pciBDF: "0000:0\(.):00.0"}}]')
kubectl --kubeconfig "${KUBE_CONF}" annotate node "${NODE}" "hami.io/node-amd-register=${DEVICES}" --overwrite
kubectl --kubeconfig "${KUBE_CONF}" patch node "${NODE}" --subresource=status --type=merge \
  -p '{"status":{"capacity":{"amd.com/gpu":"8"},"allocatable":{"amd.com/gpu":"8"}}}'

GINKGO_VERSION=$(go list -m -f '{{.Version}}' github.com/onsi/ginkgo/v2)
AMD_E2E_MOCK=true go run "github.com/onsi/ginkgo/v2/ginkgo@${GINKGO_VERSION}" \
  run -v -r --fail-fast ./test/e2e/amd/ -- --kubeconfig="${KUBE_CONF}"
