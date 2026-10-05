#!/usr/bin/env bash
# Copyright 2026 The HAMi Authors.
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.

set -o errexit
set -o nounset
set -o pipefail

readonly DEFAULT_MAX_COLLECTOR_BYTES=$((4 * 1024 * 1024))
readonly DEFAULT_MAX_BUNDLE_BYTES=$((64 * 1024 * 1024))
readonly MANIFEST_RESERVE_BYTES=$((64 * 1024))
readonly MAX_COMPONENT_PODS=100
readonly KUBECTL_REQUEST_TIMEOUT=30s
readonly VENDOR_TOOL_TIMEOUT=30s

# Print command usage, including the mutually exclusive collection scopes.
usage() {
  echo 'Usage: hami-support-bundle.sh --kubeconfig FILE --context NAME --namespace NAME [--node NAME | --pod NAME | --cluster-wide] [--since DURATION] [--tail-lines N] [--output-dir DIR] [--redact-pattern TEXT] [--dry-run]'
}

# Print a CLI error and stop before collection begins.
die() {
  echo "$*" >&2
  exit 2
}

# Require a non-empty value after an option that takes an argument.
require_value() {
  [[ $# -ge 2 && -n "$2" ]] || die "$1 requires a value"
}

kubeconfig=''; context=''; namespace=''; node=''; pod=''; since='1h'; tail_lines=500
output_dir='./_output/support-bundle'; dry_run=false; cluster_wide=false
declare -a patterns=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --kubeconfig) require_value "$@"; kubeconfig="$2"; shift 2 ;;
    --context) require_value "$@"; context="$2"; shift 2 ;;
    --namespace) require_value "$@"; namespace="$2"; shift 2 ;;
    --node) require_value "$@"; node="$2"; shift 2 ;;
    --pod) require_value "$@"; pod="$2"; shift 2 ;;
    --since) require_value "$@"; since="$2"; shift 2 ;;
    --tail-lines) require_value "$@"; tail_lines="$2"; shift 2 ;;
    --output-dir) require_value "$@"; output_dir="$2"; shift 2 ;;
    --redact-pattern) require_value "$@"; patterns+=("$2"); shift 2 ;;
    --cluster-wide) cluster_wide=true; shift ;;
    --dry-run) dry_run=true; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done

[[ -n "$kubeconfig" && -n "$context" && -n "$namespace" ]] || die '--kubeconfig, --context, and --namespace are required'
[[ "$tail_lines" =~ ^[1-9][0-9]*$ ]] || die '--tail-lines must be positive'
[[ -z "$pod" || -z "$node" ]] || die '--pod and --node cannot be used together'
[[ "$cluster_wide" == false || -z "$pod" ]] || die '--cluster-wide and --pod cannot be used together'
[[ "$cluster_wide" == false || -z "$node" ]] || die '--cluster-wide and --node cannot be used together'
for pattern in "${patterns[@]}"; do
  [[ "$pattern" != *$'\n'* && "$pattern" != *$'\r'* ]] || die '--redact-pattern must not contain newlines'
done
command -v kubectl >/dev/null || die 'kubectl is required'

max_collector_bytes="${HAMI_SUPPORT_MAX_COLLECTOR_BYTES:-$DEFAULT_MAX_COLLECTOR_BYTES}"
max_bundle_bytes="${HAMI_SUPPORT_MAX_BUNDLE_BYTES:-$DEFAULT_MAX_BUNDLE_BYTES}"
[[ "$max_collector_bytes" =~ ^[0-9]+$ && "$max_collector_bytes" -ge 64 && "$max_collector_bytes" -le "$DEFAULT_MAX_COLLECTOR_BYTES" ]] || die 'invalid HAMI_SUPPORT_MAX_COLLECTOR_BYTES'
[[ "$max_bundle_bytes" =~ ^[0-9]+$ && "$max_bundle_bytes" -ge $((2 * MANIFEST_RESERVE_BYTES)) && "$max_bundle_bytes" -le "$DEFAULT_MAX_BUNDLE_BYTES" ]] || die 'invalid HAMI_SUPPORT_MAX_BUNDLE_BYTES'
readonly max_collector_bytes max_bundle_bytes

bundle_name=hami-support-bundle
archive="$output_dir/$bundle_name.tar.gz"
bundle_path="$output_dir/$bundle_name"
[[ ! -e "$bundle_path" && ! -e "$archive" ]] || die 'refusing to overwrite existing bundle output'

umask 077
mkdir -p "$output_dir"
staging_dir=''; archive_tmp=''
# Remove only paths created and recorded by this invocation.
cleanup() {
  if [[ -n "$staging_dir" && -d "$staging_dir" ]]; then
    rm -rf -- "$staging_dir"
  fi
  if [[ -n "$archive_tmp" && -e "$archive_tmp" ]]; then
    rm -f -- "$archive_tmp"
  fi
}
trap cleanup EXIT
trap 'exit 130' HUP INT TERM

staging_parent="${TMPDIR:-/tmp}"
staging_dir=$(mktemp -d "$staging_parent/hami-support-bundle.XXXXXX")
chmod 700 "$staging_dir"
bundle_dir="$staging_dir/$bundle_name"
mkdir -p "$bundle_dir/artifacts" "$bundle_dir/errors"
records="$staging_dir/records"
pattern_file="$staging_dir/patterns"
: >"$records"
printf '%s\n' "${patterns[@]}" >"$pattern_file"
declare -a k=(--kubeconfig "$kubeconfig" --context "$context" --request-timeout="$KUBECTL_REQUEST_TIMEOUT")
bundle_bytes=0
bundle_limit_reached=false

# Append one machine-readable collector record in a private staging file.
record() {
  printf '%s\t%s\t%s\t%s\n' "$1" "$2" "$3" "$4" >>"$records"
}

# Join manifest detail fragments without producing leading separators.
append_detail() {
  local current="$1" extra="$2"
  if [[ -n "$current" ]]; then
    printf '%s;%s' "$current" "$extra"
  else
    printf '%s' "$extra"
  fi
}

# Redact credentials and image references only in structured/contextual fields.
# Environment variable names are remembered so the matching YAML value can be
# removed without hiding unrelated environment variables or HAMi annotations.
redact() {
  awk -v pfile="$pattern_file" '
  function trim(v) { sub(/^[[:space:]]+/, "", v); sub(/[[:space:]]+$/, "", v); return v }
  function normalized_key(v) {
    v=trim(v); gsub(/^"|"$/, "", v); v=tolower(v); gsub(/[-.]/, "_", v)
    return v
  }
  function sensitive_key(v, compact) {
    v=normalized_key(v); compact=v; gsub(/_/, "", compact)
    return v ~ /^(auth|authorization)$/ || v ~ /(^|_)(token|password|passwd|secret|api_key|access_key|private_key|client_secret|credential|credentials)$/ || compact ~ /(token|password|passwd|secret|apikey|accesskey|privatekey|clientsecret|credential|credentials)$/
  }
  function sensitive_env_name(v) {
    v=trim(v); gsub(/^['\''"]|['\''"]$/, "", v); v=toupper(v)
    return v ~ /(^|_)(TOKEN|PASSWORD|PASSWD|SECRET|API_KEY|APIKEY|ACCESS_KEY|ACCESSKEY|PRIVATE_KEY|CLIENT_SECRET|CREDENTIAL|CREDENTIALS)$/
  }
  function image_key(v) {
    v=normalized_key(v); gsub(/_/, "", v)
    return v == "image" || v == "imageid" || v == "containerimage" || v == "containerimageid"
  }
  function yaml_key(s, t, c, k) {
    t=s; sub(/^[[:space:]]*-[[:space:]]*/, "", t); c=index(t, ":")
    if (!c) return ""
    k=trim(substr(t, 1, c-1)); gsub(/^['\''"]|['\''"]$/, "", k); return k
  }
  function yaml_value(s, t, c, v) {
    t=s; sub(/^[[:space:]]*-[[:space:]]*/, "", t); c=index(t, ":")
    if (!c) return ""
    v=trim(substr(t, c+1)); sub(/[[:space:]]+#.*$/, "", v); return v
  }
  function replace_yaml_value(s, replacement, c) {
    c=index(s, ":"); return substr(s, 1, c) " " replacement
  }
  function leading_spaces(s, t) { t=s; sub(/[^[:space:]].*$/, "", t); return length(t) }
  function json_string_end(v, i, j, slashes) {
    for (i=1; i<=length(v); i++) if (substr(v,i,1)=="\"") {
      slashes=0; for (j=i-1; j>0 && substr(v,j,1)=="\\"; j--) slashes++
      if (slashes % 2 == 0) return i
    }
    return 0
  }
  function redact_json_fields(s, out, rest, before, header, key, values, ending, replacement) {
    out=""; rest=s
    while (match(rest, /"[^"\\]+"[[:space:]]*:[[:space:]]*"/)) {
      before=substr(rest,1,RSTART-1); header=substr(rest,RSTART,RLENGTH)
      key=header; sub(/^"/,"",key); sub(/".*/,"",key)
      values=substr(rest,RSTART+RLENGTH); ending=json_string_end(values)
      if (!ending) {
        if (sensitive_key(key) || image_key(key)) return out before header (image_key(key) ? "<redacted-image>\"" : "<redacted-credential>\"")
        return out rest
      }
      if (sensitive_key(key)) replacement="<redacted-credential>"
      else if (image_key(key)) replacement="<redacted-image>"
      else replacement=substr(values,1,ending-1)
      out=out before header replacement "\""
      rest=substr(values,ending+1)
    }
    return out rest
  }
  function redact_assignments(s, out, rest, before, header, key, values, quote, ending, replacement) {
    out=""; rest=s
    while (match(rest, /[[:alnum:]_.-]+[[:space:]]*=[[:space:]]*/)) {
      before=substr(rest,1,RSTART-1); header=substr(rest,RSTART,RLENGTH)
      key=header; sub(/[[:space:]]*=.*/,"",key)
      values=substr(rest,RSTART+RLENGTH)
      if (!sensitive_key(key) && !image_key(key)) {
        out=out before header; rest=values; continue
      }
      replacement=(image_key(key) ? "<redacted-image>" : "<redacted-credential>")
      quote=substr(values,1,1)
      if (quote == "\"") { ending=json_string_end(substr(values,2)); if (ending) ending++ }
      else if (quote == sprintf("%c",39)) { ending=index(substr(values,2),quote); if (ending) ending++ }
      else { ending=match(values,/[[:space:],;]/); if (!ending) ending=length(values)+1 }
      if (!ending) return out before header quote replacement quote
      out=out before header (quote == "\"" || quote == sprintf("%c",39) ? quote replacement quote : replacement)
      rest=substr(values,ending + ((quote == "\"" || quote == sprintf("%c",39)) ? 1 : 0))
    }
    return out rest
  }
  function replace_literal(s, p, out, x) {
    out=""
    while ((x=index(s,p)) > 0) {
      out=out substr(s,1,x-1) "<redacted>"
      s=substr(s,x+length(p))
    }
    return out s
  }
  BEGIN { while ((getline p < pfile) > 0) if (p != "") patterns[++n]=p; close(pfile) }
  {
    s=$0
    gsub(/[Aa][Uu][Tt][Hh][Oo][Rr][Ii][Zz][Aa][Tt][Ii][Oo][Nn][[:space:]]*[:=][[:space:]]*[Bb][Aa][Ss][Ii][Cc][[:space:]]+[^[:space:]",}]+/, "Authorization: Basic <redacted>", s)
    gsub(/[Aa][Uu][Tt][Hh][Oo][Rr][Ii][Zz][Aa][Tt][Ii][Oo][Nn][[:space:]]*[:=][[:space:]]*[Bb][Ee][Aa][Rr][Ee][Rr][[:space:]]+[^[:space:]",}]+/, "Authorization: Bearer <redacted>", s)
    s=redact_json_fields(s)
    s=redact_assignments(s)
    key=yaml_key(s); value=yaml_value(s); lower_key=tolower(key)
    if (lower_key == "name") {
      env_sensitive=sensitive_env_name(value); env_indent=leading_spaces(s); env_ttl=6
    } else if (env_sensitive && lower_key == "value" && leading_spaces(s) >= env_indent) {
      s=replace_yaml_value(s, "<redacted-credential>"); env_sensitive=0; env_ttl=0
    } else {
      if (value != "" && sensitive_key(key)) s=replace_yaml_value(s, "<redacted-credential>")
      else if (value != "" && image_key(key)) s=replace_yaml_value(s, "<redacted-image>")
      if (env_sensitive && s !~ /^[[:space:]]*$/) { env_ttl--; if (env_ttl <= 0 || leading_spaces(s) < env_indent) env_sensitive=0 }
    }
    gsub(/[Ii][Mm][Aa][Gg][Ee]([Ii][Dd])?[[:space:]]*[:=][[:space:]]*"?[^[:space:]",}]+/, "image: <redacted-image>", s)
    gsub(/[Ii][Mm][Aa][Gg][Ee][[:space:]]+"[^"]+"/, "image \"<redacted-image>\"", s)
    for (i=1; i<=n; i++) s=replace_literal(s,patterns[i])
    print s
  }' "$1" >"$2"
}

# Copy at most limit bytes from an already bounded staging file.
cap_file() {
  local source="$1" destination="$2" limit="$3"
  head -c "$limit" "$source" >"$destination"
}

# Install one redacted candidate if the collector and bundle budgets allow it.
store_candidate() {
  local status="$1" id="$2" relative_path="$3" detail="$4" candidate="$5"
  local size capped
  size=$(stat -c %s "$candidate")
  if (( size > max_collector_bytes )); then
    capped=$(mktemp "$staging_dir/capped.XXXXXX")
    cap_file "$candidate" "$capped" "$max_collector_bytes"
    candidate="$capped"; size=$max_collector_bytes; status=truncated
    detail=$(append_detail "$detail" "collector-byte-limit=$max_collector_bytes")
  fi
  if (( bundle_bytes + size > max_bundle_bytes - MANIFEST_RESERVE_BYTES )); then
    bundle_limit_reached=true
    record limited "$id" "$relative_path" "$(append_detail "$detail" "bundle-byte-limit=$max_bundle_bytes;artifact-omitted")"
    return
  fi
  mkdir -p "$(dirname "$bundle_dir/$relative_path")"
  cp "$candidate" "$bundle_dir/$relative_path"
  bundle_bytes=$((bundle_bytes + size))
  record "$status" "$id" "$relative_path" "$detail"
}

# Run a collector through bounded FIFOs. The command can never write more than
# max_collector_bytes + 1 bytes to either raw staging file.
collect() {
  local id="$1" path="$2"; shift 2
  local artifact_path="artifacts/$path" run_dir raw_out raw_err candidate
  local out_reader err_reader command_status out_size err_size detail=''
  if [[ "$dry_run" == true ]]; then
    printf '[dry-run] %s: ' "$id"; printf '%q ' "$@"; echo
    record skipped "$id" "$artifact_path" dry-run
    return
  fi
  if [[ "$bundle_limit_reached" == true ]]; then
    record limited "$id" "$artifact_path" "bundle-byte-limit=$max_bundle_bytes;not-run"
    return
  fi
  run_dir=$(mktemp -d "$staging_dir/run.XXXXXX")
  raw_out="$run_dir/stdout"; raw_err="$run_dir/stderr"
  mkfifo "$run_dir/stdout.fifo" "$run_dir/stderr.fifo"
  head -c "$((max_collector_bytes + 1))" <"$run_dir/stdout.fifo" >"$raw_out" & out_reader=$!
  head -c "$((max_collector_bytes + 1))" <"$run_dir/stderr.fifo" >"$raw_err" & err_reader=$!
  set +o errexit
  "$@" >"$run_dir/stdout.fifo" 2>"$run_dir/stderr.fifo"
  command_status=$?
  wait "$out_reader"; wait "$err_reader"
  set -o errexit
  rm -f "$run_dir/stdout.fifo" "$run_dir/stderr.fifo"
  out_size=$(stat -c %s "$raw_out"); err_size=$(stat -c %s "$raw_err")
  if (( out_size > max_collector_bytes )); then
    candidate="$run_dir/stdout.capped"; cap_file "$raw_out" "$candidate" "$max_collector_bytes"; raw_out="$candidate"
    detail="stdout-truncated-at=$max_collector_bytes"
  fi
  if (( err_size > max_collector_bytes )); then
    candidate="$run_dir/stderr.capped"; cap_file "$raw_err" "$candidate" "$max_collector_bytes"; raw_err="$candidate"
    detail=$(append_detail "$detail" "stderr-truncated-at=$max_collector_bytes")
  fi
  candidate="$run_dir/redacted"
  if [[ "$command_status" -eq 0 || ( "$command_status" -eq 141 && "$out_size" -gt "$max_collector_bytes" ) ]]; then
    redact "$raw_out" "$candidate"
    if [[ -n "$detail" ]]; then
      store_candidate truncated "$id" "$artifact_path" "$detail" "$candidate"
    else
      store_candidate success "$id" "$artifact_path" '' "$candidate"
    fi
  else
    redact "$raw_err" "$candidate"
    detail=$(append_detail "exit-code=$command_status" "$detail")
    store_candidate failed "$id" "errors/$id.txt" "$detail" "$candidate"
  fi
  rm -rf -- "$run_dir"
}

# Discover component Pods once, sort them, and collect each Pod's logs serially.
collect_component_logs() {
  local component="$1" selector="app.kubernetes.io/component=hami-$1"
  local list_path="$bundle_dir/artifacts/components/$component-pods.txt" line pod_name count=0
  collect "$component-log-pods" "components/$component-pods.txt" kubectl "${k[@]}" -n "$namespace" get pods -l "$selector" -o name
  [[ "$dry_run" == false && -f "$list_path" ]] || return 0
  LC_ALL=C sort -o "$list_path" "$list_path"
  while IFS= read -r line; do
    [[ "$line" == pod/* ]] || continue
    pod_name=${line#pod/}
    [[ "$pod_name" =~ ^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$ ]] || { record skipped "$component-logs-invalid-pod" "artifacts/logs/$component/invalid.txt" invalid-pod-name; continue; }
    if (( count >= MAX_COMPONENT_PODS )); then
      record limited "$component-logs" "artifacts/logs/$component" "pod-count-limit=$MAX_COMPONENT_PODS"
      break
    fi
    if [[ "$bundle_limit_reached" == true ]]; then
      record limited "$component-logs" "artifacts/logs/$component" "bundle-byte-limit=$max_bundle_bytes;remaining-pods-not-run"
      break
    fi
    collect "$component-logs-$pod_name" "logs/$component/$pod_name.txt" kubectl "${k[@]}" -n "$namespace" logs "pod/$pod_name" --all-containers=true --prefix --tail="$tail_lines" --since="$since"
    count=$((count + 1))
  done <"$list_path"
}

# This script intentionally never runs a Kubernetes Secret API request.
collect client-version versions/client.yaml kubectl "${k[@]}" version --client --output=yaml
collect server-version versions/server.yaml kubectl "${k[@]}" version --output=yaml
collect scheduler-version versions/hami-scheduler.txt kubectl "${k[@]}" -n "$namespace" get deployments -l app.kubernetes.io/component=hami-scheduler -o 'jsonpath={range .items[*]}{.metadata.name}{"\t"}{.metadata.labels.app\.kubernetes\.io/version}{"\n"}{end}'
collect device-plugin-version versions/hami-device-plugin.txt kubectl "${k[@]}" -n "$namespace" get daemonsets -l app.kubernetes.io/component=hami-device-plugin -o 'jsonpath={range .items[*]}{.metadata.name}{"\t"}{.metadata.labels.app\.kubernetes\.io/version}{"\n"}{end}'

if [[ -n "$pod" ]]; then
  collect workload-pod workloads/pod.yaml kubectl "${k[@]}" -n "$namespace" get pod "$pod" -o yaml
  collect events events/events.yaml kubectl "${k[@]}" -n "$namespace" get events --field-selector "involvedObject.kind=Pod,involvedObject.name=$pod" --sort-by=.lastTimestamp -o yaml
  collect pod-node-name scope/pod-node.txt kubectl "${k[@]}" -n "$namespace" get pod "$pod" -o 'jsonpath={.spec.nodeName}{"\n"}'
  if [[ "$dry_run" == false && -s "$bundle_dir/artifacts/scope/pod-node.txt" ]]; then
    IFS= read -r pod_node <"$bundle_dir/artifacts/scope/pod-node.txt"
    if [[ -n "$pod_node" ]]; then
      collect node nodes/node.yaml kubectl "${k[@]}" get node "$pod_node" -o yaml
    else
      record skipped node artifacts/nodes/node.yaml pod-unscheduled
    fi
  elif [[ "$dry_run" == false ]]; then
    record skipped node artifacts/nodes/node.yaml pod-node-unavailable
  fi
elif [[ -n "$node" ]]; then
  collect workloads-on-node workloads/pods-on-node.yaml kubectl "${k[@]}" -A get pods --field-selector "spec.nodeName=$node" -o yaml
  collect events events/events.yaml kubectl "${k[@]}" -A get events --field-selector "involvedObject.kind=Node,involvedObject.name=$node" --sort-by=.lastTimestamp -o yaml
  collect node nodes/node.yaml kubectl "${k[@]}" get node "$node" -o yaml
elif [[ "$cluster_wide" == true ]]; then
  collect workloads workloads/pods.yaml kubectl "${k[@]}" -A get pods -o yaml
  collect events events/events.yaml kubectl "${k[@]}" -A get events --sort-by=.lastTimestamp -o yaml
  collect nodes nodes/nodes.yaml kubectl "${k[@]}" get nodes -o yaml
else
  collect workloads workloads/pods.yaml kubectl "${k[@]}" -n "$namespace" get pods -o yaml
  collect events events/events.yaml kubectl "${k[@]}" -n "$namespace" get events --sort-by=.lastTimestamp -o yaml
  record skipped nodes artifacts/nodes/nodes.yaml namespace-scope
fi

collect scheduler-configmaps config/scheduler.yaml kubectl "${k[@]}" -n "$namespace" get configmaps -l app.kubernetes.io/component=hami-scheduler -o yaml
collect device-plugin-configmaps config/device-plugin.yaml kubectl "${k[@]}" -n "$namespace" get configmaps -l app.kubernetes.io/component=hami-device-plugin -o yaml
collect scheduler-rollout components/scheduler.txt kubectl "${k[@]}" -n "$namespace" rollout status deployment -l app.kubernetes.io/component=hami-scheduler --timeout=30s
collect device-plugin-rollout components/device-plugin.txt kubectl "${k[@]}" -n "$namespace" rollout status daemonset -l app.kubernetes.io/component=hami-device-plugin --timeout=30s
collect_component_logs scheduler
collect_component_logs device-plugin

# Vendor utilities run only on the host executing this script. They are never
# run in Kubernetes workloads or presented as diagnostics from a remote node.
declare -a vendor_tools=(nvidia-smi rocm-smi npu-smi)
declare -a vendor_args=(-q -a info)
for index in "${!vendor_tools[@]}"; do
  tool=${vendor_tools[$index]}
  if command -v "$tool" >/dev/null; then
    if command -v timeout >/dev/null; then
      collect "vendor-$tool" "vendor/$tool.txt" timeout "$VENDOR_TOOL_TIMEOUT" "$tool" "${vendor_args[$index]}"
    else
      record skipped "vendor-$tool" "artifacts/vendor/$tool.txt" timeout-command-unavailable
    fi
  else
    record skipped "vendor-$tool" "artifacts/vendor/$tool.txt" unavailable-on-collector-host
  fi
done

# Escape controlled manifest fields as JSON strings.
json_escape() {
  local value="$1"
  value=${value//\\/\\\\}; value=${value//\"/\\\"}
  value=${value//$'\n'/\\n}; value=${value//$'\r'/\\r}; value=${value//$'\t'/\\t}
  printf '%s' "$value"
}

{
  echo '{"formatVersion":1,"limits":{"maxCollectorBytes":'"$max_collector_bytes"',"maxBundleBytes":'"$max_bundle_bytes"'},"collectors":['
  first=true
  while IFS=$'\t' read -r status id path detail; do
    [[ "$first" == true ]] || echo ','; first=false
    printf '{"status":"%s","collector":"%s","path":"%s"' "$(json_escape "$status")" "$(json_escape "$id")" "$(json_escape "$path")"
    [[ -z "$detail" ]] || printf ',"detail":"%s"' "$(json_escape "$detail")"
    printf '}'
  done <"$records"
  echo ']}'
} >"$bundle_dir/manifest.json"

if [[ "$dry_run" == true ]]; then
  echo 'dry run complete; no bundle was written'
  exit 0
fi

final_bundle_bytes=$(du -sb "$bundle_dir" | awk '{print $1}')
if (( final_bundle_bytes > max_bundle_bytes )); then
  echo "bundle exceeds maximum size after manifest generation ($final_bundle_bytes > $max_bundle_bytes)" >&2
  exit 1
fi
archive_tmp=$(mktemp "$output_dir/.hami-support-bundle.tar.gz.XXXXXX")
tar --sort=name --mtime='UTC 1970-01-01' --owner=0 --group=0 --numeric-owner -C "$staging_dir" -cf - "$bundle_name" | gzip -n >"$archive_tmp"
chmod 600 "$archive_tmp"
mv "$archive_tmp" "$archive"
archive_tmp=''
echo "support bundle written to $archive"
