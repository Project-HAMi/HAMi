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
KUBE_CONF=${2:-""}
HELM_VER=${3:-"v2.4.1"}
HELM_NAME=${4:-"hami-charts"}
HELM_REPO=${5:-"https://project-hami.github.io/HAMi/"}
TARGET_NS=${6:-"hami-system"}
HAMI_ALIAS="hami"
attempt=0
max_retries=3
retry_interval=10
HELM_SOURCE="${HELM_NAME}"/"${HAMI_ALIAS}"

REPO_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
cd "${REPO_ROOT}"

source "${REPO_ROOT}"/hack/util.sh

# install helm
echo -n "Preparing: 'helm' existence check - "
if util::cmd_exist helm; then
  echo "passed"
else
  echo "installing helm"
  util::install_helm
fi



# Run e2e
if [ "${E2E_TYPE}" == "pullrequest" ] || [ "${E2E_TYPE}" == "release" ]; then
  HELM_SOURCE="*.tgz"
#elif [ "${E2E_TYPE}" == "release" ]; then
#  HELM_SOURCE=""
else
  echo "Invalid E2E Type: ${E2E_TYPE}"
  return 1
fi


while [ "$attempt" -le "$max_retries" ]; do
	# add repo locally
	helm repo add "${HELM_NAME}" "${HELM_REPO}" --force-update --kubeconfig "${KUBE_CONF}"
	helm repo update --kubeconfig "${KUBE_CONF}"

	# install or upgrade
	util::exec_cmd helm --debug upgrade --install --create-namespace --cleanup-on-fail \
							 "${HAMI_ALIAS}"     "${HELM_SOURCE}" -n "${TARGET_NS}"   \
							 --version "${HELM_VER}" --wait --timeout 20m   --kubeconfig "${KUBE_CONF}"

	attempt=$((attempt+1))
	sleep "$retry_interval"
done
