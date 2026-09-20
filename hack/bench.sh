#!/usr/bin/env bash
# Copyright 2026 The HAMi Authors.
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

# Benchmarks run here as a correctness check, not as a timing gate: shared CI
# runners are too noisy to compare durations between runs. The point is that
# every benchmark still builds its fixtures and runs to completion, which a
# plain "go test" never verifies because it skips benchmark bodies.
#
# BENCHTIME keeps a CI run short while allowing a meaningful local measurement:
#   BENCHTIME=1s make bench
BENCHTIME="${BENCHTIME:-10x}"

output_dir="./_output/bench"
mkdir -p "${output_dir}"
result_file="${output_dir}/results.txt"

# -run '^$' matches no test, so only benchmarks execute. Unlike hack/unit-test.sh
# this script needs no fake kubeconfig: that setup exists for unit tests that
# build a Kubernetes client, and no benchmark does.
#
# --race is deliberately omitted. The race detector multiplies the runtime and
# distorts the allocation figures the benchmarks exist to report.
go test -run '^$' -bench . -benchmem -benchtime "${BENCHTIME}" \
  $(go list ./pkg/... ./cmd/...) 2>&1 | tee "${result_file}"
