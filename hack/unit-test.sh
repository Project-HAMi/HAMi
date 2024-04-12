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

# init kubeconfig env
kubeconfig_path="${HOME}/.kube"
kubeconfig_file="${kubeconfig_path}/config"
kubeconfig_demo="./hack/kubeconfig-demo.yaml"

echo "kubeconfig: ${kubeconfig_file}"

if [ ! -f "$kubeconfig_file" ]; then
  echo "Generate fake kubeconfig"
  if [ ! -d "${kubeconfig_path}" ]; then
    trap 'rm -rf "$kubeconfig_path"' EXIT
    mkdir -p "${kubeconfig_path}"
    cp ${kubeconfig_demo} "${kubeconfig_file}"
  else
    trap 'rm -f "$kubeconfig_file"' EXIT
    cp ${kubeconfig_demo} "${kubeconfig_file}"
  fi
else
  echo "Use local kubeconfig"
fi

tmpDir=$(mktemp -d)
mergeF="${tmpDir}/merge.out"
rm -f ${mergeF}
ls $tmpDir
cov_file="${tmpDir}/c.cover"
go test $(go list ./pkg/... | grep -v ./pkg/device-plugin/...) -short --race -count=1 -covermode=atomic -coverprofile=${cov_file}
cat $cov_file | grep -v mode: | grep -v pkg/version | grep -v fake | grep -v main.go >>${mergeF}
#merge them
echo "mode: atomic" >coverage.out
cat ${mergeF} >>coverage.out
go tool cover -func=coverage.out
rm -rf coverage.out ${tmpDir} ${mergeF}
