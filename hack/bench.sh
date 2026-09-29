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

# This script records raw benchmark output. CI runs it for both the PR base and
# head, then uses benchstat to report all deltas and the policy file to gate
# allocation regressions. Shared CI runners remain too noisy for a timing gate.
# Every benchmark still builds its fixtures and runs to completion, which a
# plain "go test" never verifies because it skips benchmark bodies.
#
# BENCHTIME controls work per benchmark invocation. BENCH_COUNT requests
# independent benchmark samples; CI uses both when producing a benchstat
# comparison, while local smoke tests keep the historical single result.
#   BENCHTIME=1s BENCH_COUNT=10 make bench
BENCHTIME="${BENCHTIME:-10x}"
BENCH_COUNT="${BENCH_COUNT:-1}"

output_dir="${BENCH_OUTPUT_DIR:-./_output/bench}"
mkdir -p "${output_dir}"
result_file="${output_dir}/results.txt"

# -run '^$' matches no test, so only benchmarks execute. Unlike hack/unit-test.sh
# this script needs no fake kubeconfig: that setup exists for unit tests that
# build a Kubernetes client, and no benchmark does.
#
# --race is deliberately omitted. The race detector multiplies the runtime and
# distorts the allocation figures the benchmarks exist to report.
go test -run '^$' -bench . -benchmem -benchtime "${BENCHTIME}" -count "${BENCH_COUNT}" \
  $(go list ./pkg/... ./cmd/...) 2>&1 | tee "${result_file}"
