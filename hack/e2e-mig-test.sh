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

set -o errexit
set -o nounset
set -o pipefail

set -x

KUBE_CONF=${1:-"${HOME}/.kube/config"}
export KUBE_CONF
export KUBECONFIG="${KUBE_CONF}"

REPO_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
cd "${REPO_ROOT}"
source "${REPO_ROOT}"/hack/util.sh

if [ -z "${KUBE_CONF}" ]; then
   echo "Error: KUBE_CONF is not set and no default kubeconfig found."
   exit 1
fi

# Validate kubeconfig file exists
if [ ! -f "${KUBE_CONF}" ]; then
   echo "Error: kubeconfig file not found at ${KUBE_CONF}"
   exit 1
fi

# Check kubectl connectivity
if ! kubectl --kubeconfig "${KUBE_CONF}" get nodes &>/dev/null; then
   echo "Error: cannot reach Kubernetes API server via ${KUBE_CONF}"
   exit 1
fi

# Check if TARGET_NODE is set
if [ -z "${TARGET_NODE:-}" ]; then
   echo "Error: TARGET_NODE environment variable is not set."
   echo "Please set TARGET_NODE to the Kubernetes node name that has MIG-capable GPU."
   echo "Example: export TARGET_NODE=mig-node-0"
   exit 1
fi

# Validate target node exists
if ! kubectl --kubeconfig "${KUBE_CONF}" get node "${TARGET_NODE}" &>/dev/null; then
   echo "Error: target node ${TARGET_NODE} does not exist in the cluster."
   exit 1
fi

# Check if the target node has GPU resources
node_gpu_capacity=$(kubectl --kubeconfig "${KUBE_CONF}" get node "${TARGET_NODE}" -o jsonpath='{.status.capacity.nvidia\.com/gpu}' 2>/dev/null || echo "")
if [ -z "${node_gpu_capacity}" ] || [ "${node_gpu_capacity}" = "0" ]; then
   echo "Error: target node ${TARGET_NODE} does not have nvidia.com/gpu capacity."
   echo "MIG E2E test requires a node with MIG-capable GPU."
   exit 1
fi

echo "=== MIG E2E Test Environment ==="
echo "Kubeconfig: ${KUBE_CONF}"
echo "Target Node: ${TARGET_NODE}"
echo "GPU Capacity: ${node_gpu_capacity}"
echo "================================"

# Run the MIG E2E test
"${REPO_ROOT}"/hack/hami-mig-e2e.sh
