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
printf 'benchstat report for %s and %s\n' "$1" "$2"
EOF
chmod +x "${tmp_dir}/benchstat"

cat >"${tmp_dir}/policy.tsv" <<'EOF'
^BenchmarkScoreNode/ 5 0
EOF

cat >"${tmp_dir}/base.txt" <<'EOF'
BenchmarkScoreNode/initContainers=0-8  100  1000 ns/op  200 B/op  4 allocs/op
BenchmarkScoreNode/initContainers=0-8  100  1000 ns/op  400 B/op  4 allocs/op
EOF

cat >"${tmp_dir}/head.txt" <<'EOF'
BenchmarkScoreNode/initContainers=0-8  100  900 ns/op  210 B/op  4 allocs/op
BenchmarkScoreNode/initContainers=0-8  100  900 ns/op  420 B/op  4 allocs/op
EOF

BENCHSTAT_BIN="${tmp_dir}/benchstat" \
	"${repo_root}/hack/bench-compare.sh" "${tmp_dir}/base.txt" "${tmp_dir}/head.txt" "${tmp_dir}/report.txt" "${tmp_dir}/policy.tsv"
grep -q 'benchstat report' "${tmp_dir}/report.txt"

cat >"${tmp_dir}/head.txt" <<'EOF'
BenchmarkScoreNode/initContainers=0-8  100  900 ns/op  211 B/op  4 allocs/op
BenchmarkScoreNode/initContainers=0-8  100  900 ns/op  421 B/op  4 allocs/op
EOF

if BENCHSTAT_BIN="${tmp_dir}/benchstat" \
	"${repo_root}/hack/bench-compare.sh" "${tmp_dir}/base.txt" "${tmp_dir}/head.txt" "${tmp_dir}/report.txt" "${tmp_dir}/policy.tsv"; then
	echo "expected B/op regression to fail" >&2
	exit 1
fi

cat >"${tmp_dir}/head.txt" <<'EOF'
BenchmarkDifferent-8  100  900 ns/op  200 B/op  4 allocs/op
EOF

if BENCHSTAT_BIN="${tmp_dir}/benchstat" \
	"${repo_root}/hack/bench-compare.sh" "${tmp_dir}/base.txt" "${tmp_dir}/head.txt" "${tmp_dir}/report.txt" "${tmp_dir}/policy.tsv"; then
	echo "expected missing benchmark to fail" >&2
	exit 1
fi

echo "bench comparison tests passed"
