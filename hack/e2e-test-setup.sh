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

REPO_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
cd "${REPO_ROOT}"

source "${REPO_ROOT}"/hack/util.sh

function setup_e2e_env() {
    echo "=== Setting up e2e environment on the local node ==="

    echo -n "Checking kubectl connectivity... "
    local kubeconf="${KUBE_CONF:-${HOME}/.kube/config}"
    if ! kubectl --kubeconfig "${kubeconf}" get nodes &>/dev/null; then
        echo "Error: cannot reach Kubernetes API server via ${kubeconf}"
        exit 1
    fi
    echo "OK"

    echo -n "Checking for GPU node... "
    local gpu_nodes
    gpu_nodes=$(kubectl --kubeconfig "${kubeconf}" get nodes \
        -l 'nvidia.com/gpu.present=true' --no-headers 2>/dev/null | wc -l)
    if [[ "${gpu_nodes}" -eq 0 ]]; then
        echo "Warning: no nodes with label nvidia.com/gpu.present=true found"
    else
        echo "found ${gpu_nodes} GPU node(s)"
    fi

    echo "=== e2e environment ready ==="
}

setup_e2e_env
