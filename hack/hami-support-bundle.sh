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
readonly MAX_REDACTION_LINE_BYTES=$((64 * 1024))
readonly KUBECTL_REQUEST_TIMEOUT=30s
readonly VENDOR_TOOL_TIMEOUT=30s

# Print command usage, including the mutually exclusive collection scopes.
usage() {
  cat <<'EOF'
Usage: hami-support-bundle.sh --kubeconfig FILE --context NAME --namespace NAME [--workload-namespace NAME] [--node NAME | --pod NAME | --cluster-wide] [--since DURATION] [--tail-lines N] [--output-dir DIR] [--redact-pattern TEXT] [--dry-run]

--namespace is the namespace where HAMi is installed. --workload-namespace
selects the affected workload namespace and defaults to --namespace.

Requirements: Linux, Bash 4.1 or newer, GNU coreutils and tar, kubectl, and
util-linux flock.
EOF
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

kubeconfig=''; context=''; namespace=''; workload_namespace=''; node=''; pod=''; since='1h'; tail_lines=500
output_dir='./_output/support-bundle'; dry_run=false; cluster_wide=false
declare -a patterns=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --kubeconfig) require_value "$@"; kubeconfig="$2"; shift 2 ;;
    --context) require_value "$@"; context="$2"; shift 2 ;;
    --namespace) require_value "$@"; namespace="$2"; shift 2 ;;
    --workload-namespace) require_value "$@"; workload_namespace="$2"; shift 2 ;;
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
workload_namespace="${workload_namespace:-$namespace}"
[[ "$tail_lines" =~ ^[1-9][0-9]*$ ]] || die '--tail-lines must be positive'
[[ -z "$pod" || -z "$node" ]] || die '--pod and --node cannot be used together'
[[ "$cluster_wide" == false || -z "$pod" ]] || die '--cluster-wide and --pod cannot be used together'
[[ "$cluster_wide" == false || -z "$node" ]] || die '--cluster-wide and --node cannot be used together'
if [[ -n "${patterns[0]+set}" ]]; then
  for pattern in "${patterns[@]}"; do
    [[ "$pattern" != *$'\n'* && "$pattern" != *$'\r'* ]] || die '--redact-pattern must not contain newlines'
  done
fi
command -v kubectl >/dev/null || die 'kubectl is required'
command -v flock >/dev/null || die 'flock is required'

max_collector_bytes="${HAMI_SUPPORT_MAX_COLLECTOR_BYTES:-$DEFAULT_MAX_COLLECTOR_BYTES}"
max_bundle_bytes="${HAMI_SUPPORT_MAX_BUNDLE_BYTES:-$DEFAULT_MAX_BUNDLE_BYTES}"
[[ "$max_collector_bytes" =~ ^[0-9]+$ && "$max_collector_bytes" -ge 64 && "$max_collector_bytes" -le "$DEFAULT_MAX_COLLECTOR_BYTES" ]] || die 'invalid HAMI_SUPPORT_MAX_COLLECTOR_BYTES'
[[ "$max_bundle_bytes" =~ ^[0-9]+$ && "$max_bundle_bytes" -ge $((2 * MANIFEST_RESERVE_BYTES)) && "$max_bundle_bytes" -le "$DEFAULT_MAX_BUNDLE_BYTES" ]] || die 'invalid HAMI_SUPPORT_MAX_BUNDLE_BYTES'
readonly max_collector_bytes max_bundle_bytes

bundle_name=hami-support-bundle
archive="$output_dir/$bundle_name.tar.gz"
bundle_path="$output_dir/$bundle_name"

umask 077
mkdir -p "$output_dir"
staging_dir=''; archive_tmp=''; reservation_acquired=false

# Remove only paths created and recorded by this invocation.
cleanup() {
  if [[ -n "$staging_dir" && -d "$staging_dir" ]]; then
    rm -rf -- "$staging_dir"
  fi
  if [[ -n "$archive_tmp" && -e "$archive_tmp" ]]; then
    rm -f -- "$archive_tmp"
  fi
  if [[ "$reservation_acquired" == true ]]; then
    flock -u "$reservation_fd" || true
    exec {reservation_fd}<&-
    reservation_acquired=false
  fi
}
trap cleanup EXIT
trap 'exit 130' HUP INT TERM

# Lock the destination directory itself. The kernel releases this lock even if
# the collector is killed, and no owner-file publication window is involved.
exec {reservation_fd}<"$output_dir"
flock -n "$reservation_fd" || die 'support bundle output is already reserved by another collector'
reservation_acquired=true
[[ ! -e "$bundle_path" && ! -e "$archive" ]] || die 'refusing to overwrite existing bundle output'

staging_parent="${TMPDIR:-/tmp}"
staging_dir=$(mktemp -d "$staging_parent/hami-support-bundle.XXXXXX")
chmod 700 "$staging_dir"
bundle_dir="$staging_dir/$bundle_name"
mkdir -p "$bundle_dir/artifacts" "$bundle_dir/errors"
records="$staging_dir/records"
pattern_file="$staging_dir/patterns"
: >"$records"
: >"$pattern_file"
if [[ -n "${patterns[0]+set}" ]]; then
  printf '%s\n' "${patterns[@]}" >"$pattern_file"
fi
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

# Redact credentials and image references in structured/contextual fields.
# Environment subtrees are suppressed because even innocently named variables
# can contain private tenant data; unrelated fields and HAMi annotations remain.
redact() {
  awk -v pfile="$pattern_file" -v max_line_bytes="$MAX_REDACTION_LINE_BYTES" '
  function trim(v) { sub(/^[[:space:]]+/, "", v); sub(/[[:space:]]+$/, "", v); return v }
  function normalized_key(v) {
    v=trim(v); gsub(/^"|"$/, "", v); v=tolower(v); gsub(/[-.]/, "_", v)
    return v
  }
  function sensitive_key(v, compact) {
    v=normalized_key(v); compact=v; gsub(/_/, "", compact)
    return v ~ /^(auth|authorization)$/ || v ~ /(^|_)(auth|token|password|passwd|pass|secret|credential|credentials|cookie|sig|signature)(_|$)/ || v ~ /(^|_)(api|access|private|secret|signing)_key(_|$)/ || compact ~ /(token|password|passwd|pass|secret|apikey|accesskey|privatekey|secretkey|signingkey|clientsecret|credential|credentials|cookie|sig|signature)$/
  }
  function sensitive_env_name(v) {
    v=trim(v); gsub(/^['\''"]|['\''"]$/, "", v); v=toupper(v)
    return v ~ /(^|_)(AUTH|TOKEN|PASSWORD|PASSWD|PASS|SECRET|KEY|CREDENTIAL|CREDENTIALS|COOKIE|SIG|SIGNATURE)(_|$)/
  }
  function yaml_block_scalar(v) {
    return v ~ /^[|>]([+-][1-9]?|[1-9][+-]?)?$/
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
  function yaml_key_indent(s, indent, t) {
    indent=leading_spaces(s); t=substr(s,indent+1)
    if (match(t,/^-[[:space:]]+/)) indent+=RLENGTH
    return indent
  }
  function inside_yaml_tree(s, indent, current, t) {
    if (s ~ /^[[:space:]]*$/) return 1
    current=leading_spaces(s)
    if (current > indent) return 1
    t=substr(s,current+1)
    return current == indent && t ~ /^-[[:space:]]+/
  }
  function json_string_end(v, i, j, slashes) {
    for (i=1; i<=length(v); i++) if (substr(v,i,1)=="\"") {
      slashes=0; for (j=i-1; j>0 && substr(v,j,1)=="\\"; j--) slashes++
      if (slashes % 2 == 0) return i
    }
    return 0
  }
  function unquoted_end(v, i, c) {
    for (i=1; i<=length(v); i++) {
      c=substr(v,i,1)
      if (c ~ /[[:space:]]/ || c == "," || c == ";" || c == "]" || c == "}" || c == "\"" || c == sprintf("%c",39)) return i
    }
    return length(v)+1
  }
  function inline_sensitive_env(s, rest, header, values, quote, ending, name) {
    rest=s
    while (match(rest, /(^|[,{][[:space:]]*)"?name"?[[:space:]]*:[[:space:]]*/)) {
      header=substr(rest,RSTART,RLENGTH); values=substr(rest,RSTART+RLENGTH); quote=substr(values,1,1)
      if (quote == "\"") {
        ending=json_string_end(substr(values,2)); if (!ending) return 0
        name=substr(values,2,ending-1); rest=substr(values,ending+2)
      } else if (quote == sprintf("%c",39)) {
        ending=index(substr(values,2),quote); if (!ending) return 0
        name=substr(values,2,ending-1); rest=substr(values,ending+2)
      } else {
        ending=unquoted_end(values); name=substr(values,1,ending-1); rest=substr(values,ending)
      }
      if (sensitive_env_name(name)) return 1
    }
    return 0
  }
  function redact_inline_values(s, out, rest, before, header, values, quote, ending) {
    out=""; rest=s
    while (match(rest, /(^|[,{][[:space:]]*)"?value"?[[:space:]]*:[[:space:]]*/)) {
      before=substr(rest,1,RSTART-1); header=substr(rest,RSTART,RLENGTH); values=substr(rest,RSTART+RLENGTH); quote=substr(values,1,1)
      if (quote == "\"") { ending=json_string_end(substr(values,2)); if (ending) ending++ }
      else if (quote == sprintf("%c",39)) { ending=index(substr(values,2),quote); if (ending) ending++ }
      else ending=unquoted_end(values)
      if (!ending) return out before header quote "<redacted-credential>" quote
      out=out before header (quote == "\"" || quote == sprintf("%c",39) ? quote "<redacted-credential>" quote : "<redacted-credential>")
      rest=substr(values,ending + ((quote == "\"" || quote == sprintf("%c",39)) ? 1 : 0))
    }
    return out rest
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
      else ending=unquoted_end(values)
      if (!ending) return out before header quote replacement quote
      out=out before header (quote == "\"" || quote == sprintf("%c",39) ? quote replacement quote : replacement)
      rest=substr(values,ending + ((quote == "\"" || quote == sprintf("%c",39)) ? 1 : 0))
    }
    return out rest
  }
  function redact_space_args(s, out, rest, before, header, key, values, quote, ending) {
    out=""; rest=s
    while (match(rest, /(--[[:alnum:]_.-]+|-p)[[:space:]]+/)) {
      before=substr(rest,1,RSTART-1); header=substr(rest,RSTART,RLENGTH)
      key=header; sub(/[[:space:]].*/,"",key)
      values=substr(rest,RSTART+RLENGTH)
      if (key != "-p") sub(/^--/,"",key)
      if (key != "-p" && !sensitive_key(key)) { out=out before header; rest=values; continue }
      quote=substr(values,1,1)
      if (quote == "\"") { ending=json_string_end(substr(values,2)); if (ending) ending++ }
      else if (quote == sprintf("%c",39)) { ending=index(substr(values,2),quote); if (ending) ending++ }
      else ending=unquoted_end(values)
      if (!ending) return out before header quote "<redacted-credential>" quote
      out=out before header (quote == "\"" || quote == sprintf("%c",39) ? quote "<redacted-credential>" quote : "<redacted-credential>")
      rest=substr(values,ending + ((quote == "\"" || quote == sprintf("%c",39)) ? 1 : 0))
    }
    return out rest
  }
  function redact_image_mentions(s, out, rest, before, header, values, quote, ending) {
    out=""; rest=s
    while (match(tolower(rest),/(^|[^[:alnum:]_])(image|reference)[[:space:]]*[:=]?[[:space:]]+/)) {
      before=substr(rest,1,RSTART-1); header=substr(rest,RSTART,RLENGTH); values=substr(rest,RSTART+RLENGTH); quote=substr(values,1,1)
      if (quote == "\"") { ending=json_string_end(substr(values,2)); if (ending) ending++ }
      else if (quote == sprintf("%c",39)) { ending=index(substr(values,2),quote); if (ending) ending++ }
      else ending=unquoted_end(values)
      if (!ending) return out before header quote "<redacted-image>" quote
      out=out before header (quote == "\"" || quote == sprintf("%c",39) ? quote "<redacted-image>" quote : "<redacted-image>")
      rest=substr(values,ending + ((quote == "\"" || quote == sprintf("%c",39)) ? 1 : 0))
    }
    return out rest
  }
  function redact_dns_lookups(s, out, rest, before, matched) {
    out=""; rest=s
    while (match(tolower(rest), /lookup[[:space:]]+[^[:space:]:]+/)) {
      before=substr(rest,1,RSTART-1); matched=substr(rest,RSTART,RLENGTH)
      sub(/[^[:space:]]+$/, "<redacted-host>", matched)
      out=out before matched; rest=substr(rest,RSTART+RLENGTH)
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
    if (pem_private_key) {
      if ($0 ~ /-----END [A-Z0-9 ]*PRIVATE KEY-----/) pem_private_key=0
      next
    }
    if ($0 ~ /-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----/) {
      pem_indent=leading_spaces($0)
      print substr($0,1,pem_indent) "<redacted-private-key>"; pem_private_key=1; next
    }
    if (length($0) > max_line_bytes) {
      print "<redacted-oversized-line>"; next
    }
    if (sensitive_yaml_tree) {
      if (inside_yaml_tree($0,sensitive_yaml_tree_indent)) next
      sensitive_yaml_tree=0
    }
    if (sensitive_yaml_block) {
      if ($0 ~ /^[[:space:]]*$/ || leading_spaces($0) > sensitive_yaml_block_indent) next
      sensitive_yaml_block=0
    }
    s=$0
    if (pending_yaml_arg) {
      if (s ~ /^[[:space:]]*$/) next
      if (leading_spaces(s) == pending_yaml_arg_indent && s ~ /^[[:space:]]*-[[:space:]]+/) {
        sub(/-[[:space:]]+.*/,"- <redacted-credential>",s); pending_yaml_arg=0
      } else pending_yaml_arg=0
    }
    gsub(/[Aa][Uu][Tt][Hh][Oo][Rr][Ii][Zz][Aa][Tt][Ii][Oo][Nn][[:space:]]*[:=][[:space:]]*[Bb][Aa][Ss][Ii][Cc][[:space:]]+[^[:space:]",}]+/, "Authorization: Basic <redacted>", s)
    gsub(/[Aa][Uu][Tt][Hh][Oo][Rr][Ii][Zz][Aa][Tt][Ii][Oo][Nn][[:space:]]*[:=][[:space:]]*[Bb][Ee][Aa][Rr][Ee][Rr][[:space:]]+[^[:space:]",}]+/, "Authorization: Bearer <redacted>", s)
    s=redact_json_fields(s)
    if (inline_sensitive_env(s)) s=redact_inline_values(s)
    s=redact_assignments(s)
    s=redact_space_args(s)
    gsub(/[[:alnum:]_.%+~-]+:[^[:space:]@\/]+@/, "<redacted>@", s)
    gsub(/[[:alnum:]_-]{8,}\.[[:alnum:]_-]{8,}\.[[:alnum:]_-]{8,}/, "<redacted-jwt>", s)
    gsub(/gh[pousr]_[[:alnum:]]{20,}/, "<redacted-token>", s)
    gsub(/glpat-[[:alnum:]_-]{20,}/, "<redacted-token>", s)
    gsub(/(AKIA|ASIA)[0-9A-Z]{16}/, "<redacted-access-key>", s)
    s=redact_dns_lookups(s)
    key=yaml_key(s); value=yaml_value(s); lower_key=tolower(key)
    if (lower_key == "env" || lower_key == "kubectl.kubernetes.io/last-applied-configuration" || lower_key == "images") {
      replacement=(lower_key == "images" ? "<redacted-image-inventory>" : (lower_key == "env" ? "<redacted-environment>" : "<redacted-credential>"))
      if (value == "" || yaml_block_scalar(value)) { sensitive_yaml_tree=1; sensitive_yaml_tree_indent=yaml_key_indent(s) }
      s=replace_yaml_value(s, replacement); env_sensitive=0; env_ttl=0
    } else if (lower_key == "name") {
      env_sensitive=sensitive_env_name(value); env_indent=leading_spaces(s); env_ttl=6
    } else if (env_sensitive && lower_key == "value" && leading_spaces(s) >= env_indent) {
      if (yaml_block_scalar(value)) { sensitive_yaml_block=1; sensitive_yaml_block_indent=leading_spaces(s) }
      s=replace_yaml_value(s, "<redacted-credential>"); env_sensitive=0; env_ttl=0
    } else {
      if (value != "" && sensitive_key(key)) {
        if (yaml_block_scalar(value)) { sensitive_yaml_block=1; sensitive_yaml_block_indent=leading_spaces(s) }
        s=replace_yaml_value(s, "<redacted-credential>")
      }
      else if (value != "" && image_key(key)) s=replace_yaml_value(s, "<redacted-image>")
      if (env_sensitive && s !~ /^[[:space:]]*$/) { env_ttl--; if (env_ttl <= 0 || leading_spaces(s) < env_indent) env_sensitive=0 }
    }
    list_value=s; sub(/^[[:space:]]*-[[:space:]]+/,"",list_value)
    if (s ~ /^[[:space:]]*-[[:space:]]+(--[[:alnum:]_.-]+|-p)[[:space:]]*$/) {
      list_key=trim(list_value)
      if (list_key == "-p" || sensitive_key(substr(list_key,3))) { pending_yaml_arg=1; pending_yaml_arg_indent=leading_spaces(s) }
    }
    s=redact_image_mentions(s)
    for (i=1; i<=n; i++) s=replace_literal(s,patterns[i])
    print s
  }' "$1" >"$2"
}

# Copy at most limit bytes from an already bounded staging file.
cap_file() {
  local source="$1" destination="$2" limit="$3"
  head -c "$limit" "$source" >"$destination"
}

# Capture a bounded prefix from a FIFO, then drain the rest without storing it.
capture_and_drain() {
  local fifo="$1" destination="$2" limit="$3"
  { head -c "$limit" >"$destination"; cat >/dev/null; } <"$fifo"
}

# Install one redacted candidate if the collector and bundle budgets allow it.
store_candidate() {
  local status="$1" id="$2" relative_path="$3" detail="$4" candidate="$5"
  local size capped
  size=$(stat -c %s "$candidate")
  if (( size > max_collector_bytes )); then
    capped=$(mktemp "$staging_dir/capped.XXXXXX")
    cap_file "$candidate" "$capped" "$max_collector_bytes"
    candidate="$capped"; size=$max_collector_bytes
    [[ "$status" != success ]] || status=truncated
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
  local artifact_path="artifacts/$path" run_dir raw_out raw_err candidate redacted_out redacted_err
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
  capture_and_drain "$run_dir/stdout.fifo" "$raw_out" "$((max_collector_bytes + 1))" {reservation_fd}<&- & out_reader=$!
  capture_and_drain "$run_dir/stderr.fifo" "$raw_err" "$((max_collector_bytes + 1))" {reservation_fd}<&- & err_reader=$!
  if "$@" >"$run_dir/stdout.fifo" 2>"$run_dir/stderr.fifo" {reservation_fd}<&-; then
    command_status=0
  else
    command_status=$?
  fi
  wait "$out_reader"; wait "$err_reader"
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
  if [[ "$command_status" -eq 0 ]]; then
    redact "$raw_out" "$candidate"
    if [[ -n "$detail" ]]; then
      store_candidate truncated "$id" "$artifact_path" "$detail" "$candidate"
    else
      store_candidate success "$id" "$artifact_path" '' "$candidate"
    fi
  else
    redacted_out="$run_dir/stdout.redacted"; redacted_err="$run_dir/stderr.redacted"
    redact "$raw_out" "$redacted_out"
    redact "$raw_err" "$redacted_err"
    {
      if [[ -s "$redacted_out" ]]; then printf 'stdout:\n'; cat "$redacted_out"; fi
      if [[ -s "$redacted_err" ]]; then printf 'stderr:\n'; cat "$redacted_err"; fi
    } >"$candidate"
    detail=$(append_detail "$detail" "exit-code=$command_status")
    store_candidate failed "$id" "errors/$id.txt" "$detail" "$candidate"
  fi
  rm -rf -- "$run_dir"
}

# Collect a bounded operational view without the environment and volume detail
# present in full Pod manifests.
collect_component_pod_metadata() {
  local component="$1" selector="app.kubernetes.io/component=hami-$1"
  collect "$component-pod-metadata" "components/$component-pod-metadata.txt" kubectl "${k[@]}" -n "$namespace" get pods -l "$selector" -o wide
}

# Discover component Pods once, sort them, and collect each Pod's logs serially.
# optional_empty_detail makes an empty optional component explicit in the
# manifest; required components retain their original empty-list behaviour.
collect_component_logs() {
  local component="$1" optional_empty_detail="${2:-}" selector="app.kubernetes.io/component=hami-$1"
  local list_path="$bundle_dir/artifacts/components/$component-pods.txt" line pod_name count=0
  local restart_statuses previous_id container_name restart_count restarted
  collect "$component-log-pods" "components/$component-pods.txt" kubectl "${k[@]}" -n "$namespace" get pods -l "$selector" -o name
  [[ "$dry_run" == false ]] || return 0
  if [[ ! -f "$list_path" ]]; then
    if [[ -n "$optional_empty_detail" ]]; then
      if [[ "$bundle_limit_reached" == true ]]; then
        record limited "$component-logs" "artifacts/logs/$component" "bundle-byte-limit=$max_bundle_bytes;not-run"
      else
        record skipped "$component-logs" "artifacts/logs/$component" pod-discovery-unavailable
      fi
    fi
    return 0
  fi
  if [[ ! -s "$list_path" ]]; then
    [[ -z "$optional_empty_detail" ]] || record skipped "$component-logs" "artifacts/logs/$component" "$optional_empty_detail"
    return 0
  fi
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
    if [[ "$bundle_limit_reached" == false ]]; then
      previous_id="$component-previous-logs-$pod_name"
      restarted=false
      if restart_statuses=$(kubectl "${k[@]}" -n "$namespace" get pod "$pod_name" -o 'jsonpath={range .status.initContainerStatuses[*]}{.name}{"\t"}{.restartCount}{"\n"}{end}{range .status.containerStatuses[*]}{.name}{"\t"}{.restartCount}{"\n"}{end}{range .status.ephemeralContainerStatuses[*]}{.name}{"\t"}{.restartCount}{"\n"}{end}' {reservation_fd}<&-); then
        while IFS=$'\t' read -r container_name restart_count; do
          [[ -n "$container_name" && "$restart_count" =~ ^[0-9]+$ ]] || continue
          (( restart_count > 0 )) || continue
          restarted=true
          collect "$previous_id-$container_name" "logs/$component/$pod_name.$container_name.previous.txt" kubectl "${k[@]}" -n "$namespace" logs "pod/$pod_name" -c "$container_name" --previous --prefix --tail="$tail_lines" --since="$since"
        done <<<"$restart_statuses"
        [[ "$restarted" == true ]] || record skipped "$previous_id" "artifacts/logs/$component/$pod_name.previous.txt" no-restarted-containers
      else
        record skipped "$previous_id" "artifacts/logs/$component/$pod_name.previous.txt" pod-status-unavailable
      fi
    fi
    count=$((count + 1))
  done <"$list_path"
}

# Collect one optional DaemonSet-backed component. Workload discovery decides
# whether rollout status is meaningful, while Pod discovery independently
# controls log collection so a DaemonSet with no Pods remains diagnosable.
collect_optional_daemonset_component() {
  local component="$1" selector="app.kubernetes.io/component=hami-$1"
  local workloads_path="$bundle_dir/artifacts/components/$component-daemonsets.txt"
  local empty_logs_detail=component-not-installed

  collect_component_pod_metadata "$component"
  collect "$component-daemonsets" "components/$component-daemonsets.txt" kubectl "${k[@]}" -n "$namespace" get daemonsets -l "$selector" -o name
  if [[ "$dry_run" == true ]]; then
    collect "$component-rollout" "components/$component-rollout.txt" kubectl "${k[@]}" -n "$namespace" rollout status daemonset -l "$selector" --timeout=30s
  elif [[ -s "$workloads_path" ]]; then
    empty_logs_detail=no-matching-pods
    collect "$component-rollout" "components/$component-rollout.txt" kubectl "${k[@]}" -n "$namespace" rollout status daemonset -l "$selector" --timeout=30s
  elif [[ -f "$workloads_path" ]]; then
    record skipped "$component-rollout" "artifacts/components/$component-rollout.txt" component-not-installed
  elif [[ "$bundle_limit_reached" == true ]]; then
    record limited "$component-rollout" "artifacts/components/$component-rollout.txt" "bundle-byte-limit=$max_bundle_bytes;not-run"
  else
    empty_logs_detail=workload-discovery-unavailable
    record skipped "$component-rollout" "artifacts/components/$component-rollout.txt" workload-discovery-unavailable
  fi
  collect_component_logs "$component" "$empty_logs_detail"
}

# Session relays are scheduler-created Pods and have no rollout workload.
collect_optional_pod_component() {
  local component="$1"
  collect_component_pod_metadata "$component"
  collect_component_logs "$component" component-not-installed
}

# This script intentionally never runs a Kubernetes Secret API request.
collect client-version versions/client.yaml kubectl "${k[@]}" version --client --output=yaml
collect server-version versions/server.yaml kubectl "${k[@]}" version --output=yaml
collect scheduler-version versions/hami-scheduler.txt kubectl "${k[@]}" -n "$namespace" get deployments -l app.kubernetes.io/component=hami-scheduler -o 'jsonpath={range .items[*]}{.metadata.name}{"\t"}{.metadata.labels.app\.kubernetes\.io/version}{"\n"}{end}'
collect device-plugin-version versions/hami-device-plugin.txt kubectl "${k[@]}" -n "$namespace" get daemonsets -l app.kubernetes.io/component=hami-device-plugin -o 'jsonpath={range .items[*]}{.metadata.name}{"\t"}{.metadata.labels.app\.kubernetes\.io/version}{"\n"}{end}'

if [[ -n "$pod" ]]; then
  collect workload-pod workloads/pod.yaml kubectl "${k[@]}" -n "$workload_namespace" get pod "$pod" -o yaml
  collect events events/events.yaml kubectl "${k[@]}" -n "$workload_namespace" get events --field-selector "involvedObject.kind=Pod,involvedObject.name=$pod" --sort-by=.lastTimestamp -o yaml
  collect pod-node-name scope/pod-node.txt kubectl "${k[@]}" -n "$workload_namespace" get pod "$pod" -o 'jsonpath={.spec.nodeName}{"\n"}'
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
  collect workloads-on-node workloads/pods-on-node.yaml kubectl "${k[@]}" -n "$workload_namespace" get pods --field-selector "spec.nodeName=$node" -o yaml
  collect events events/events.yaml kubectl "${k[@]}" -n "$workload_namespace" get events --field-selector "involvedObject.kind=Node,involvedObject.name=$node" --sort-by=.lastTimestamp -o yaml
  collect node nodes/node.yaml kubectl "${k[@]}" get node "$node" -o yaml
elif [[ "$cluster_wide" == true ]]; then
  collect workloads workloads/pods.yaml kubectl "${k[@]}" -A get pods -o yaml
  collect events events/events.yaml kubectl "${k[@]}" -A get events --sort-by=.lastTimestamp -o yaml
  collect nodes nodes/nodes.yaml kubectl "${k[@]}" get nodes -o yaml
else
  collect workloads workloads/pods.yaml kubectl "${k[@]}" -n "$workload_namespace" get pods -o yaml
  collect events events/events.yaml kubectl "${k[@]}" -n "$workload_namespace" get events --sort-by=.lastTimestamp -o yaml
  record skipped nodes artifacts/nodes/nodes.yaml namespace-scope
fi

collect scheduler-configmaps config/scheduler.yaml kubectl "${k[@]}" -n "$namespace" get configmaps -l app.kubernetes.io/component=hami-scheduler -o yaml
collect device-plugin-configmaps config/device-plugin.yaml kubectl "${k[@]}" -n "$namespace" get configmaps -l app.kubernetes.io/component=hami-device-plugin -o yaml
collect scheduler-rollout components/scheduler.txt kubectl "${k[@]}" -n "$namespace" rollout status deployment -l app.kubernetes.io/component=hami-scheduler --timeout=30s
collect device-plugin-rollout components/device-plugin.txt kubectl "${k[@]}" -n "$namespace" rollout status daemonset -l app.kubernetes.io/component=hami-device-plugin --timeout=30s
collect_component_logs scheduler
collect_component_logs device-plugin
collect_optional_daemonset_component mock-device-plugin
collect_optional_daemonset_component lupine-server
collect_optional_pod_component lupine-session

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
