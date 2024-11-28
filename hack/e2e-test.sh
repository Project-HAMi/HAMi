#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail
set -x

E2E_TYPE=${1:-"Default"}

REPO_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
source "${REPO_ROOT}"/hack/util.sh

if util::cmd_exist ginkgo; then
  echo "Using ginkgo version:"
  ginkgo version
else
  go install github.com/onsi/ginkgo/v2/ginkgo
  go get github.com/onsi/gomega/...
  ginkgo version
fi

# Run e2e
if [ "${E2E_TYPE}" == "Default" ];then
	ginkgo -v -r --fail-fast  ./test/e2e/ --kubeconfig="$KUBE_CONF"

  else
    echo "Invalid E2E Type: ${E2E_TYPE}"
    return 1
fi
