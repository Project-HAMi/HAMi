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

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

cat >"${tmp_dir}/benchstat" <<'EOF'
#!/usr/bin/env bash
if [[ "${1:-}" == "-format" && "${2:-}" == "csv" ]]; then
	shift 2
	format=csv

else
	format=text
fi
if [[ "${1:-}" != base=* || "${2:-}" != head=* ]]; then
	echo "expected labeled base and head benchmark inputs" >&2
	exit 2
fi
base_file="${1#base=}"
head_file="${2#head=}"
for file in "${base_file}" "${head_file}"; do
	if grep -Eq '^(cgo-gcc-prolog|go): ' "${file}"; then
		echo "non-benchmark build output was not normalized" >&2
		exit 2
	fi
	grep -q '^pkg: ' "${file}"
	grep -q '^Benchmark' "${file}"
done
if [[ "${format}" == "csv" ]]; then
	cat <<CSV
pkg: github.com/Project-HAMi/HAMi/pkg/scheduler
,base,,head,,,
,B/op,CI,B/op,CI,vs base,P
ScoreNode/initContainers=0-8,200,0%,${BENCHSTAT_HEAD_BYTES:-210},0%,${BENCHSTAT_BYTES_DELTA:-+5.00%},p=0.000 n=10

,base,,head,,,
,allocs/op,CI,allocs/op,CI,vs base,P
ScoreNode/initContainers=0-8,4,0%,${BENCHSTAT_HEAD_ALLOCS:-4},0%,${BENCHSTAT_ALLOCS_DELTA:-~},${BENCHSTAT_ALLOCS_P:-p=1.000 n=10}
CSV
	exit
fi
printf 'benchstat report for %s and %s\n' "$1" "$2"
EOF
chmod +x "${tmp_dir}/benchstat"

cat >"${tmp_dir}/policy.tsv" <<'EOF'
^BenchmarkScoreNode/ 5 0
EOF

cat >"${tmp_dir}/base.txt" <<'EOF'
go: downloading example.com/cold-cache-only v1.0.0
cgo-gcc-prolog: In function 'compiler warning present only in the cold-cache baseline'
pkg: github.com/Project-HAMi/HAMi/pkg/scheduler
EOF
for _ in {1..10}; do
	printf '%s\n' 'BenchmarkScoreNode/initContainers=0-8  100  1000 ns/op  200 B/op  4 allocs/op' >>"${tmp_dir}/base.txt"
done

printf '%s\n' 'pkg: github.com/Project-HAMi/HAMi/pkg/scheduler' >"${tmp_dir}/head.txt"
for _ in {1..10}; do
	# Timing is intentionally much worse and must remain informational.
	printf '%s\n' 'BenchmarkScoreNode/initContainers=0-8  100  9000 ns/op  210 B/op  4 allocs/op' >>"${tmp_dir}/head.txt"
done

BENCHSTAT_BIN="${tmp_dir}/benchstat" \
	"${repo_root}/hack/bench-compare.sh" "${tmp_dir}/base.txt" "${tmp_dir}/head.txt" "${tmp_dir}/reports/report.txt" "${tmp_dir}/policy.tsv"
grep -q 'benchstat report' "${tmp_dir}/reports/report.txt"

printf '%s\n' 'pkg: github.com/Project-HAMi/HAMi/pkg/scheduler' >"${tmp_dir}/head-regression.txt"
for _ in {1..10}; do
	printf '%s\n' 'BenchmarkScoreNode/initContainers=0-8  100  900 ns/op  211 B/op  4 allocs/op' >>"${tmp_dir}/head-regression.txt"
done

if BENCHSTAT_BIN="${tmp_dir}/benchstat" BENCHSTAT_HEAD_BYTES=211 BENCHSTAT_BYTES_DELTA='+5.50%' \
	"${repo_root}/hack/bench-compare.sh" "${tmp_dir}/base.txt" "${tmp_dir}/head-regression.txt" "${tmp_dir}/report.txt" "${tmp_dir}/policy.tsv"; then
	echo "expected B/op regression to fail" >&2
	exit 1
fi

printf '%s\n' 'pkg: github.com/Project-HAMi/HAMi/pkg/scheduler' >"${tmp_dir}/head-alloc-regression.txt"
for _ in {1..10}; do
	printf '%s\n' 'BenchmarkScoreNode/initContainers=0-8  100  900 ns/op  210 B/op  5 allocs/op' >>"${tmp_dir}/head-alloc-regression.txt"
done

if BENCHSTAT_BIN="${tmp_dir}/benchstat" BENCHSTAT_HEAD_ALLOCS=5 BENCHSTAT_ALLOCS_DELTA='+25.00%' BENCHSTAT_ALLOCS_P='p=0.000 n=10' \
	"${repo_root}/hack/bench-compare.sh" "${tmp_dir}/base.txt" "${tmp_dir}/head-alloc-regression.txt" "${tmp_dir}/report.txt" "${tmp_dir}/policy.tsv"; then
	echo "expected allocs/op regression to fail" >&2
	exit 1
fi

# A changed median without statistical significance is benchmark noise, not a
# demonstrated regression. This is the shape that previously made CI flaky.
BENCHSTAT_BIN="${tmp_dir}/benchstat" BENCHSTAT_HEAD_ALLOCS=5 BENCHSTAT_ALLOCS_DELTA='~' \
	"${repo_root}/hack/bench-compare.sh" "${tmp_dir}/base.txt" "${tmp_dir}/head-alloc-regression.txt" "${tmp_dir}/report.txt" "${tmp_dir}/policy.tsv"

cat >"${tmp_dir}/head.txt" <<'EOF'
pkg: github.com/Project-HAMi/HAMi/pkg/scheduler
BenchmarkDifferent-8  100  900 ns/op  200 B/op  4 allocs/op
EOF

if BENCHSTAT_BIN="${tmp_dir}/benchstat" \
	"${repo_root}/hack/bench-compare.sh" "${tmp_dir}/base.txt" "${tmp_dir}/head.txt" "${tmp_dir}/report.txt" "${tmp_dir}/policy.tsv"; then
	echo "expected missing benchmark to fail" >&2
	exit 1
fi

cat >"${tmp_dir}/failing-benchstat" <<'EOF'
#!/usr/bin/env bash
exit 1
EOF
chmod +x "${tmp_dir}/failing-benchstat"

if BENCHSTAT_BIN="${tmp_dir}/failing-benchstat" \
	"${repo_root}/hack/bench-compare.sh" "${tmp_dir}/base.txt" "${tmp_dir}/head.txt" "${tmp_dir}/report.txt" "${tmp_dir}/policy.tsv"; then
	echo "expected benchstat failure to propagate" >&2
	exit 1
fi

echo "bench comparison tests passed"
