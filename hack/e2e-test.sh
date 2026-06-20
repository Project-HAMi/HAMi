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

E2E_TYPE=${1:-"pullrequest"}
KUBE_CONF=${2:-"${HOME}/.kube/config"}
export KUBE_CONF

REPO_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
cd "${REPO_ROOT}"
source "${REPO_ROOT}"/hack/util.sh

if [ -z "${KUBE_CONF}" ]; then
   echo "Error: KUBE_CONF is not set and no default kubeconfig found."
   exit 1
fi

# Run e2e
if [ "${E2E_TYPE}" == "pullrequest" ] || [ "${E2E_TYPE}" == "release" ]; then
   GINKGO_VERSION=$(go list -m -f '{{.Version}}' github.com/onsi/ginkgo/v2)
   if [ -z "${GINKGO_VERSION}" ]; then
       echo "Error: could not determine ginkgo version from go.mod" >&2
       exit 1
   fi
   go run "github.com/onsi/ginkgo/v2/ginkgo@${GINKGO_VERSION}" \
      run -v -r --fail-fast ./test/e2e/ -- --kubeconfig="${KUBE_CONF}"
else
   echo "Invalid E2E Type: ${E2E_TYPE}"
   exit 1
fi
