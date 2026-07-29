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

REPO_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
GOLANGCI_LINT_VER="v2.12.2"

cd "${REPO_ROOT}"
source "hack/util.sh"

if util::cmd_exist golangci-lint; then
  echo "Using golangci-lint version:"
  golangci-lint version
else
  echo "Installing golangci-lint ${GOLANGCI_LINT_VER}"
  # https://golangci-lint.run/usage/install/#other-ci
  GOLANGCI_INSTALL_REF="6b2ddf9224768e2097b028b7ac7f6efb97a5f9f6"
  GOLANGCI_INSTALL_SHA256="d32d3534af96cfd59546a084d22b213e8a47541cada5013aa8a84c4fa2589905"
  GOLANGCI_INSTALL_SCRIPT=$(mktemp)
  trap 'rm -f "${GOLANGCI_INSTALL_SCRIPT}"' EXIT
  curl -sSfL "https://raw.githubusercontent.com/golangci/golangci-lint/${GOLANGCI_INSTALL_REF}/install.sh" -o "${GOLANGCI_INSTALL_SCRIPT}"
  echo "${GOLANGCI_INSTALL_SHA256}  ${GOLANGCI_INSTALL_SCRIPT}" | sha256sum -c -
  sh "${GOLANGCI_INSTALL_SCRIPT}" -b "$(go env GOPATH)/bin" "${GOLANGCI_LINT_VER}"
fi

if golangci-lint run -v; then
  echo 'Congratulations!  All Go source files have passed staticcheck.'
else
  echo # print one empty line, separate from warning messages.
  echo 'Please review the above warnings.'
  echo 'If the above warnings do not make sense, feel free to file an issue.'
  exit 1
fi
