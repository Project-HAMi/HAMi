#!/usr/bin/env bash
# Copyright 2024 The HAMi Authors.
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

set -o errexit  # Exit immediately if a command exits with a non-zero status
set -o nounset  # Exit if an unset variable is referenced
set -o pipefail # Exit if any command in a pipeline fails
set -x          # Enable debug mode

# Default values
E2E_TYPE=${1:-"pullrequest"}
KUBE_CONF=${2:-"${HOME}/.kube/config"}  # Default to ~/.kube/config
HELM_VER=${3:-"v2.4.1"}
HELM_NAME=${4:-"hami-charts"}
HELM_REPO=${5:-"https://project-hami.github.io/HAMi/"}
TARGET_NS=${6:-"hami-system"}
HAMI_ALIAS="hami"
HELM_SOURCE=""

REPO_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
cd "${REPO_ROOT}"

source "${REPO_ROOT}"/hack/util.sh

# Install Helm if not already installed.
echo -n "Preparing: 'helm' existence check - "
if util::cmd_exist helm; then
  echo "passed"
else
  echo "installing helm"
  util::install_helm
fi

# Set Helm Chart source based on E2E_TYPE.
echo "E2E Type is: ${E2E_TYPE}"

if [ "${E2E_TYPE}" == "pullrequest" ]; then
  # Ensure the charts directory exists and contains a .tgz file
  if [ -d "charts" ] && [ -n "$(ls charts/*.tgz 2>/dev/null)" ]; then
    HELM_SOURCE=$(ls charts/*.tgz | head -n 1)  # Use the first .tgz file found
    echo "Using local chart: ${HELM_SOURCE}"
  else
    echo "Error: No .tgz file found in the charts directory."
    exit 1
  fi
elif [ "${E2E_TYPE}" == "release" ]; then
  HELM_SOURCE="${HELM_NAME}/${HAMI_ALIAS}"
  echo "Using remote chart: ${HELM_SOURCE}"
elif [ "${E2E_TYPE}" == "local" ]; then
  # Package the chart from source and derive image settings from the VERSION file.
  helm dependency update charts/hami
  helm package charts/hami -d charts/
  HELM_SOURCE=$(ls -t charts/hami-*.tgz | head -1)

  echo "Using local chart: ${HELM_SOURCE}"
  echo "Using local image: ${IMG_NAME}:${IMG_TAG}"

  # Load image into the cluster based on the current context.
  KUBE_CONTEXT=$(kubectl --kubeconfig "${KUBE_CONF}" config current-context 2>/dev/null || echo "")
  if echo "${KUBE_CONTEXT}" | grep -q "minikube"; then
    minikube image load "${IMG_NAME}:${IMG_TAG}"
  elif echo "${KUBE_CONTEXT}" | grep -q "^kind-"; then
    kind load docker-image "${IMG_NAME}:${IMG_TAG}" --name "${KUBE_CONTEXT#kind-}"
  else
    echo "Warning: context '${KUBE_CONTEXT}' is not minikube or kind, skipping image load."
  fi

  # Detect the kube-scheduler version from the live cluster.
  KUBE_SCHEDULER_TAG=$(kubectl version -o json 2>/dev/null | jq --raw-output '.serverVersion.gitVersion')
  if [ -z "${KUBE_SCHEDULER_TAG}" ]; then
    echo "Error: Could not detect Kubernetes server version."
    exit 1
  fi
  echo "Detected kube-scheduler tag: ${KUBE_SCHEDULER_TAG}"
else
  echo "Invalid E2E Type: ${E2E_TYPE}"
  exit 1
fi

# Fix kubeconfig file permissions.
chmod 600 "${KUBE_CONF}"

# Add Helm repository.
echo "Adding Helm repository..."
if ! helm repo add "${HELM_NAME}" "${HELM_REPO}" --force-update --kubeconfig "${KUBE_CONF}" --insecure-skip-tls-verify; then
  echo "Error: Failed to add Helm repository. Please check the repository URL and network connectivity."
  exit 1
fi

# Update Helm repositories.
echo "Updating Helm repositories..."
if ! helm repo update --kubeconfig "${KUBE_CONF}"; then
  echo "Error: Failed to update Helm repositories. Please check your Helm configuration."
  exit 1
fi

# Deploy or upgrade Helm Chart.
echo "Deploying/Upgrading HAMI Helm Chart..."
echo "Helm Source: ${HELM_SOURCE}"
echo "Namespace: ${TARGET_NS}"
echo "Kubeconfig: ${KUBE_CONF}"

if [ "${E2E_TYPE}" == "local" ]; then
  if ! helm --debug upgrade --install --create-namespace --cleanup-on-fail \
    "${HAMI_ALIAS}" "${HELM_SOURCE}" -n "${TARGET_NS}" \
    --set global.imageTag="${IMG_TAG}" \
    --set scheduler.extender.image.registry="" \
    --set scheduler.extender.image.repository="${IMG_NAME}" \
    --set devicePlugin.image.registry="" \
    --set devicePlugin.image.repository="${IMG_NAME}" \
    --set devicePlugin.monitor.image.registry="" \
    --set devicePlugin.monitor.image.repository="${IMG_NAME}" \
    --set scheduler.kubeScheduler.image.registry="registry.k8s.io" \
    --set scheduler.kubeScheduler.image.repository="kube-scheduler" \
    --set scheduler.kubeScheduler.image.tag="${KUBE_SCHEDULER_TAG}" \
    --set scheduler.leaderElect=false \
    --wait --timeout 10m --kubeconfig "${KUBE_CONF}"; then
    echo "Error: Failed to deploy/upgrade Helm Chart. Please check the Helm logs above for more details."
    exit 1
  fi
else
  if ! helm --debug upgrade --install --create-namespace --cleanup-on-fail \
    "${HAMI_ALIAS}" "${HELM_SOURCE}" -n "${TARGET_NS}" \
    --set devicePlugin.passDeviceSpecsEnabled=false \
    --version "${HELM_VER}" --set global.imageTag="${HELM_VER}" --wait --timeout 10m --kubeconfig "${KUBE_CONF}"; then
    echo "Error: Failed to deploy/upgrade Helm Chart. Please check the Helm logs above for more details."
    exit 1
  fi
fi

# Check Pod status.
echo "Checking Pod status..."
kubectl --kubeconfig "${KUBE_CONF}" get po -n "${TARGET_NS}"

if ! util::check_pods_status "${KUBE_CONF}" ; then
  echo "Error: Pods are not running correctly."
  exit 1
fi

echo "HAMI Helm Chart deployed successfully."
