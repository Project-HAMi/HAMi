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

#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

set -x

E2E_TYPE=${1:-"pullrequest"}
KUBE_CONF=${2:-""}

REPO_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
source "${REPO_ROOT}"/hack/util.sh

if [ -z "${KUBE_CONF}" ]; then
   echo "Error: KUBE_CONF environment variable is not set."
   exit 1
fi

echo "Using ginkgo version from go.mod:"
go run github.com/onsi/ginkgo/v2/ginkgo version

if [ "${E2E_TYPE}" == "pullrequest" ] || [ "${E2E_TYPE}" == "release" ]; then
   go run github.com/onsi/ginkgo/v2/ginkgo -v -r --fail-fast ./test/e2e/ --kubeconfig="${KUBE_CONF}"
else
   echo "Invalid E2E Type: ${E2E_TYPE}"
   exit 1
fi
