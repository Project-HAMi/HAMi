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

# This script verifies that every pkg/device/<vendor>/device.go backend
# re-checks namespace ResourceQuota usage in its Fit() implementation, to
# guard against the check silently regressing for a vendor that has it, or
# never landing for a vendor that doesn't.
#
# It runs the quotacheck tool, which uses Go AST analysis to confirm each
# backend's Fit() method reaches a call to the shared
# device.QuotaManager.FitQuota re-check.
#
# ALLOWED_VENDORS lists backends tracked by #2829 that don't call the
# re-check yet; each gets its own fix PR, and removes itself from this list
# when it lands. quotacheck fails if a listed vendor starts passing (a
# stale entry) or an unlisted vendor fails (a new or regressed backend).
ALLOWED_VENDORS="amd,ascend,awsneuron,biren,enflame,hygon,iluvatar,kunlun,metax,mthreads,vastai"

set -o errexit
set -o nounset
set -o pipefail

REPO_ROOT=$(dirname "${BASH_SOURCE[0]}")/..

cd "${REPO_ROOT}"

echo "Running ResourceQuota re-check verification..."
go run ./hack/tools/quotacheck/ -allow "${ALLOWED_VENDORS}"
