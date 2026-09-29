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

# Compare Go benchmark output from a PR base and head. benchstat produces the
# human-readable report; the awk check below only gates allocation metrics,
# which are deterministic enough to compare on a shared runner.

set -o errexit
set -o nounset
set -o pipefail

if [[ "$#" -ne 4 ]]; then
	echo "usage: $0 <base-results> <head-results> <report> <policy>" >&2
	exit 2
fi

base_results="$1"
head_results="$2"
report="$3"
policy="$4"
benchstat_bin="${BENCHSTAT_BIN:-benchstat}"

for file in "${base_results}" "${head_results}" "${policy}"; do
	if [[ ! -f "${file}" ]]; then
		echo "required file does not exist: ${file}" >&2
		exit 2
	fi
done

mkdir -p "$(dirname "${report}")"
"${benchstat_bin}" "${base_results}" "${head_results}" | tee "${report}"

awk -F '\t' -v base="${base_results}" -v head="${head_results}" -v policy="${policy}" '
function metric_value(line, metric,    fields, count, i) {
	count = split(line, fields, /[[:space:]]+/)
	for (i = 1; i <= count; i++) {
		if (fields[i] == metric && i > 1) {
			return fields[i - 1]
		}
	}
	return ""
}

function load_result(file, sums, counts,    line, fields, count, name, bytes, allocs) {
	while ((getline line < file) > 0) {
		if (line !~ /^Benchmark/) {
			continue
		}
		count = split(line, fields, /[[:space:]]+/)
		name = fields[1]
		sub(/-[0-9]+$/, "", name)
		bytes = metric_value(line, "B/op")
		allocs = metric_value(line, "allocs/op")
		if (bytes == "" || allocs == "") {
			printf "%s has no B/op or allocs/op metrics: %s\n", file, line > "/dev/stderr"
			errors++
			continue
		}
		sums[name, "B/op"] += bytes
		sums[name, "allocs/op"] += allocs
		counts[name, "B/op"]++
		counts[name, "allocs/op"]++
	}
	close(file)
}

BEGIN {
	load_result(base, base_sums, base_counts)
	load_result(head, head_sums, head_counts)
	while ((getline line < policy) > 0) {
		if (line == "" || line ~ /^#/) {
			continue
		}
		count = split(line, fields, /[[:space:]]+/)
		if (count != 3 || fields[2] !~ /^[0-9]+([.][0-9]+)?$/ || fields[3] !~ /^[0-9]+([.][0-9]+)?$/) {
			printf "invalid benchmark policy line: %s\n", line > "/dev/stderr"
			errors++
			continue
		}
		patterns[++policy_count] = fields[1]
		byte_limits[policy_count] = fields[2]
		alloc_limits[policy_count] = fields[3]
	}
	close(policy)

	for (i = 1; i <= policy_count; i++) {
		matched = 0
		for (key in base_sums) {
			split(key, parts, SUBSEP)
			name = parts[1]
			metric = parts[2]
			if (metric != "B/op" || name !~ patterns[i]) {
				continue
			}
			matched++
			for (m = 1; m <= 2; m++) {
				metric = m == 1 ? "B/op" : "allocs/op"
				if (!((name SUBSEP metric) in head_sums)) {
					printf "head benchmark is missing %s metric for %s\n", metric, name > "/dev/stderr"
					errors++
					continue
				}
				old = base_sums[name, metric] / base_counts[name, metric]
				new = head_sums[name, metric] / head_counts[name, metric]
				limit = metric == "B/op" ? byte_limits[i] : alloc_limits[i]
				if (new > old * (1 + limit / 100)) {
					printf "%s regression for %s: base=%s head=%s (limit +%s%%)\n", metric, name, old, new, limit > "/dev/stderr"
					errors++
				}
			}
		}
		if (matched == 0) {
			printf "benchmark policy did not match base results: %s\n", patterns[i] > "/dev/stderr"
			errors++
		}
	}
	exit errors != 0
}
'
