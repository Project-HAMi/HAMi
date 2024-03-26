#!/usr/bin/env bash

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
go test $(go list ./pkg/... | grep -v ./pkg/device-plugin/...) -short --race -count=1 -covermode=atomic  -coverprofile=${cov_file}
cat $cov_file | grep -v mode: | grep -v pkg/version | grep -v fake | grep -v main.go  >> ${mergeF}
#merge them
echo "mode: atomic" > coverage.out
cat ${mergeF} >> coverage.out
go tool cover -func=coverage.out
rm -rf coverage.out ${tmpDir}  ${mergeF}
