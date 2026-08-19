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

# This script verifies that production Go code does not use Kubernetes API
# verbs that are not granted by the RBAC roles in the helm chart.
#
# It runs the rbaccheck tool which reads the ClusterRole/Role templates from
# the chart and uses Go AST analysis to check Go source files. Each directory
# is checked against the role of the binary that runs its code; shared code
# (pkg/util/) is checked against the union of all roles.

set -o errexit
set -o nounset
set -o pipefail

REPO_ROOT=$(dirname "${BASH_SOURCE[0]}")/..

cd "${REPO_ROOT}"

echo "Running RBAC permission check..."
go run ./hack/tools/rbaccheck/ ./pkg/ ./cmd/
