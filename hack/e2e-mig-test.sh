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

# Wrapper that validates kubeconfig and delegates to hami-mig-e2e.sh.
# All test logic and preflight checks live in hami-mig-e2e.sh; this
# script only ensures KUBECONFIG is set so bare kubectl calls work.

set -o errexit
set -o nounset
set -o pipefail

set -x

KUBE_CONF=${1:-"${HOME}/.kube/config"}
export KUBE_CONF
export KUBECONFIG="${KUBE_CONF}"

REPO_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
cd "${REPO_ROOT}"

if [ ! -f "${KUBE_CONF}" ]; then
   echo "Error: kubeconfig file not found at ${KUBE_CONF}"
   exit 1
fi

source "${REPO_ROOT}"/hack/hami-mig-e2e.sh
main
