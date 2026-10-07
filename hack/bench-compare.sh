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
# human-readable report; the awk check below gates statistically significant
# allocation-metric changes against the configured limits. Requiring both is
# important because concurrent benchmarks can vary by a handful of allocations
# even when the measured code is identical.

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
stats_dir="$(mktemp -d)"
trap 'rm -rf "${stats_dir}"' EXIT

# Cold-cache output can contain download messages or compiler warnings with a
# colon. benchstat treats any such line as benchmark configuration, which can
# split base and head into separate tables. Preserve the raw artifacts for the
# policy checks below, but give benchstat only canonical Go benchmark records.
normalize_results() {
	awk '/^(goos|goarch|pkg|cpu): / || /^Benchmark/' "$1" >"$2"
}

base_stats="${stats_dir}/base.txt"
head_stats="${stats_dir}/head.txt"
stats_file="${stats_dir}/stats.csv"
normalize_results "${base_results}" "${base_stats}"
normalize_results "${head_results}" "${head_stats}"

"${benchstat_bin}" "base=${base_stats}" "head=${head_stats}" | tee "${report}"
"${benchstat_bin}" -format csv "base=${base_stats}" "head=${head_stats}" >"${stats_file}"

awk -F '\t' -v base="${base_results}" -v head="${head_results}" -v policy="${policy}" -v stats="${stats_file}" '
function metric_value(line, metric,    fields, count, i) {
	count = split(line, fields, /[[:space:]]+/)
	for (i = 1; i <= count; i++) {
		if (fields[i] == metric && i > 1) {
			return fields[i - 1]
		}
	}
	return ""
}

function load_result(file, sums, counts,    line, fields, count, package, name, bytes, allocs) {
	package = "unknown package"
	while ((getline line < file) > 0) {
		if (line ~ /^pkg: /) {
			package = substr(line, 6)
			continue
		}
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
		sums[package, name, "B/op"] += bytes
		sums[package, name, "allocs/op"] += allocs
		counts[package, name, "B/op"]++
		counts[package, name, "allocs/op"]++
	}
	close(file)
}

function load_stats(file, medians, significant,    line, fields, count, package, metric, name) {
	package = "unknown package"
	while ((getline line < file) > 0) {
		if (line ~ /^pkg: /) {
			package = substr(line, 6)
			metric = ""
			continue
		}
		if (line ~ /^,B\/op,/) {
			metric = "B/op"
			continue
		}
		if (line ~ /^,allocs\/op,/) {
			metric = "allocs/op"
			continue
		}
		if (line ~ /^,/ || line ~ /^geomean,/ || metric == "") {
			continue
		}
		count = split(line, fields, /,/)
		if (count < 7 || fields[2] == "" || fields[4] == "") {
			continue
		}
		name = "Benchmark" fields[1]
		sub(/-[0-9]+$/, "", name)
		medians[package, name, metric, "base"] = fields[2]
		medians[package, name, metric, "head"] = fields[4]
		significant[package, name, metric] = fields[6] != "~" && fields[6] != ""
	}
	close(file)
}

BEGIN {
	load_result(base, base_sums, base_counts)
	load_result(head, head_sums, head_counts)
	load_stats(stats, medians, significant)
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
			package = parts[1]
			name = parts[2]
			metric = parts[3]
			if (metric != "B/op" || name !~ patterns[i]) {
				continue
			}
			matched++
			for (m = 1; m <= 2; m++) {
				metric = m == 1 ? "B/op" : "allocs/op"
				result_key = package SUBSEP name SUBSEP metric
				if (!(result_key in head_sums)) {
					printf "head benchmark is missing %s metric for %s (%s)\n", metric, name, package > "/dev/stderr"
					errors++
					continue
				}
				if (!((result_key SUBSEP "base") in medians) || !((result_key SUBSEP "head") in medians)) {
					printf "benchstat did not report %s for %s (%s)\n", metric, name, package > "/dev/stderr"
					errors++
					continue
				}
				old = medians[package, name, metric, "base"]
				new = medians[package, name, metric, "head"]
				limit = metric == "B/op" ? byte_limits[i] : alloc_limits[i]
				if (significant[package, name, metric] && new > old * (1 + limit / 100)) {
					printf "%s regression for %s (%s): base median=%s head median=%s (limit +%s%%)\n", metric, name, package, old, new, limit > "/dev/stderr"
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
