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

REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "${REPO_ROOT}"

source "${REPO_ROOT}/hack/util.sh"

echo "=== Verifying mock device plugin Helm chart templates ==="

# 1. When mockDevicePlugin is disabled (default), no mock daemonset should be rendered
out_disabled=$(helm template hami ./charts/hami --show-only templates/device-plugin/daemonsetmock.yaml 2>/dev/null || true)
if echo "${out_disabled}" | grep -q "kind: DaemonSet"; then
  echo "FAIL: mock device plugin DaemonSet should not be rendered when disabled"
  exit 1
fi
echo "PASS: mock device plugin DaemonSet omitted when disabled"

# 2. When mockDevicePlugin is enabled without pull secrets, imagePullSecrets should not be rendered
out_no_secrets=$(helm template hami ./charts/hami \
  --set mockDevicePlugin.enabled=true \
  --show-only templates/device-plugin/daemonsetmock.yaml)
if echo "${out_no_secrets}" | grep -q "imagePullSecrets:"; then
  echo "FAIL: imagePullSecrets should not be rendered when no pull secrets are configured"
  exit 1
fi
echo "PASS: imagePullSecrets omitted when no secrets configured"

# 3. When mockDevicePlugin.image.pullSecrets is configured
out_image_secrets=$(helm template hami ./charts/hami \
  --set mockDevicePlugin.enabled=true \
  --set mockDevicePlugin.image.pullSecrets[0]=mock-secret \
  --show-only templates/device-plugin/daemonsetmock.yaml)
if ! echo "${out_image_secrets}" | grep -q "mock-secret"; then
  echo "FAIL: mock-secret not found in rendered manifest"
  exit 1
fi
echo "PASS: mockDevicePlugin.image.pullSecrets rendered correctly"

# 4. When global.imagePullSecrets is configured
out_global_secrets=$(helm template hami ./charts/hami \
  --set mockDevicePlugin.enabled=true \
  --set global.imagePullSecrets[0]=global-secret \
  --show-only templates/device-plugin/daemonsetmock.yaml)
if ! echo "${out_global_secrets}" | grep -q "global-secret"; then
  echo "FAIL: global-secret not found in rendered manifest"
  exit 1
fi
echo "PASS: global.imagePullSecrets rendered correctly"

# 5. When both global.imagePullSecrets and mockDevicePlugin.image.pullSecrets are configured
out_both_secrets=$(helm template hami ./charts/hami \
  --set mockDevicePlugin.enabled=true \
  --set global.imagePullSecrets[0]=global-secret \
  --set mockDevicePlugin.image.pullSecrets[0]=mock-secret \
  --show-only templates/device-plugin/daemonsetmock.yaml)
if ! echo "${out_both_secrets}" | grep -q "global-secret" || ! echo "${out_both_secrets}" | grep -q "mock-secret"; then
  echo "FAIL: both global-secret and mock-secret should be rendered"
  exit 1
fi
echo "PASS: both global and mock image pull secrets rendered correctly"

# 6. When mockDevicePlugin.imagePullSecrets is configured
out_top_secrets=$(helm template hami ./charts/hami \
  --set mockDevicePlugin.enabled=true \
  --set mockDevicePlugin.imagePullSecrets[0]=top-level-mock-secret \
  --show-only templates/device-plugin/daemonsetmock.yaml)
if ! echo "${out_top_secrets}" | grep -q "top-level-mock-secret"; then
  echo "FAIL: top-level-mock-secret not found in rendered manifest"
  exit 1
fi
echo "PASS: mockDevicePlugin.imagePullSecrets rendered correctly"

echo "=== All mock device plugin Helm template tests passed ==="
