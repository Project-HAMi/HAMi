#!/usr/bin/env bash
# Copyright 2026 The HAMi Authors.
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.

set -o errexit
set -o nounset
set -o pipefail

usage() {
  echo 'Usage: hami-support-bundle.sh --kubeconfig FILE --context NAME --namespace NAME [--node NAME] [--pod NAME] [--cluster-wide] [--since DURATION] [--tail-lines N] [--output-dir DIR] [--redact-pattern TEXT] [--dry-run]'
}

kubeconfig=''; context=''; namespace=''; node=''; pod=''; since='1h'; tail_lines=500; output_dir='./_output/support-bundle'; dry_run=false; cluster_wide=false
declare -a patterns=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --kubeconfig) kubeconfig="$2"; shift 2 ;;
    --context) context="$2"; shift 2 ;;
    --namespace) namespace="$2"; shift 2 ;;
    --node) node="$2"; shift 2 ;;
    --pod) pod="$2"; shift 2 ;;
    --since) since="$2"; shift 2 ;;
    --tail-lines) tail_lines="$2"; shift 2 ;;
    --output-dir) output_dir="$2"; shift 2 ;;
    --redact-pattern) patterns+=("$2"); shift 2 ;;
    --cluster-wide) cluster_wide=true; shift ;;
    --dry-run) dry_run=true; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done
[[ -n "$kubeconfig" && -n "$context" && -n "$namespace" ]] || { echo '--kubeconfig, --context, and --namespace are required' >&2; exit 2; }
[[ "$tail_lines" =~ ^[1-9][0-9]*$ ]] || { echo '--tail-lines must be positive' >&2; exit 2; }
[[ "$cluster_wide" == false || -z "$node" ]] || { echo '--cluster-wide and --node conflict' >&2; exit 2; }
command -v kubectl >/dev/null || { echo 'kubectl is required' >&2; exit 2; }

bundle_name=hami-support-bundle
bundle_dir="$output_dir/$bundle_name"
archive="$output_dir/$bundle_name.tar.gz"
[[ ! -e "$bundle_dir" && ! -e "$archive" ]] || { echo 'refusing to overwrite existing bundle output' >&2; exit 2; }
mkdir -p "$bundle_dir/artifacts" "$bundle_dir/errors"
records="$bundle_dir/.records"; pattern_file="$bundle_dir/.patterns"
: >"$records"
printf '%s\n' "${patterns[@]}" >"$pattern_file"
declare -a k=(--kubeconfig "$kubeconfig" --context "$context")

record() { printf '%s\t%s\t%s\t%s\n' "$1" "$2" "$3" "$4" >>"$records"; }
redact() {
  awk -v pfile="$pattern_file" '
  function replace_literal(s,p, x){while((x=index(s,p)))s=substr(s,1,x-1)"<redacted>"substr(s,x+length(p));return s}
  BEGIN{while((getline p<pfile)>0)if(p!="")a[++n]=p;close(pfile)}
  {s=$0;gsub(/[Bb]earer[[:space:]]+[[:alnum:]_.~+\/=:-]+/,"Bearer <redacted>",s);gsub(/([Tt]oken|[Pp]assword|[Pp]asswd|[Ss]ecret|[Aa]uthorization)[[:space:]]*[:=][[:space:]]*[^[:space:]]+/,"<redacted-credential>",s);gsub(/([Ii]mage|"image")[[:space:]]*:[[:space:]]*[^[:space:]]+/,"image: <redacted-image>",s);gsub(/[[:alnum:]][[:alnum:].-]*\.[[:alpha:]][[:alnum:].-]*(:[0-9]+)?\/[^[:space:]]+/,"<redacted-image>",s);for(i=1;i<=n;i++)s=replace_literal(s,a[i]);print s}' "$1" >"$2"
}
collect() {
  local id="$1" path="$2"; shift 2
  local out="$bundle_dir/artifacts/$path" err="$bundle_dir/errors/$id.txt" tmp
  mkdir -p "$(dirname "$out")"
  if [[ "$dry_run" == true ]]; then printf '[dry-run] %s: ' "$id"; printf '%q ' "$@"; echo; record skipped "$id" "artifacts/$path" dry-run; return; fi
  tmp=$(mktemp)
  if "$@" >"$tmp" 2>"$err"; then redact "$tmp" "$out"; rm -f "$tmp" "$err"; record success "$id" "artifacts/$path" ''
  else redact "$err" "$err.redacted"; mv "$err.redacted" "$err"; rm -f "$tmp"; record failed "$id" "errors/$id.txt" non-zero; fi
}

# This script intentionally never runs a Kubernetes Secret API request.
collect client-version versions/client.yaml kubectl version --client --output=yaml
collect server-version versions/server.yaml kubectl "${k[@]}" version --output=yaml
if [[ -n "$pod" ]]; then
  collect workload-pod "workloads/pod-$pod.yaml" kubectl "${k[@]}" -n "$namespace" get pod "$pod" -o yaml
  collect events events/events.yaml kubectl "${k[@]}" -n "$namespace" get events --field-selector "involvedObject.name=$pod" --sort-by=.lastTimestamp -o yaml
elif [[ -n "$node" ]]; then
  collect workloads-on-node "workloads/pods-on-$node.yaml" kubectl "${k[@]}" -n "$namespace" get pods --field-selector "spec.nodeName=$node" -o yaml
  collect events events/events.yaml kubectl "${k[@]}" -n "$namespace" get events --field-selector "involvedObject.kind=Node,involvedObject.name=$node" --sort-by=.lastTimestamp -o yaml
else
  collect workloads workloads/pods.yaml kubectl "${k[@]}" -n "$namespace" get pods -o yaml
  collect events events/events.yaml kubectl "${k[@]}" -n "$namespace" get events --sort-by=.lastTimestamp -o yaml
fi
collect nodes nodes/nodes.yaml kubectl "${k[@]}" get nodes -o yaml
collect scheduler-configmaps config/scheduler.yaml kubectl "${k[@]}" -n "$namespace" get configmaps -l app.kubernetes.io/component=hami-scheduler -o yaml
collect device-plugin-configmaps config/device-plugin.yaml kubectl "${k[@]}" -n "$namespace" get configmaps -l app.kubernetes.io/component=hami-device-plugin -o yaml
collect scheduler-rollout components/scheduler.txt kubectl "${k[@]}" -n "$namespace" rollout status deployment -l app.kubernetes.io/component=hami-scheduler --timeout=30s
collect device-plugin-rollout components/device-plugin.txt kubectl "${k[@]}" -n "$namespace" rollout status daemonset -l app.kubernetes.io/component=hami-device-plugin --timeout=30s
collect scheduler-logs logs/scheduler.txt kubectl "${k[@]}" -n "$namespace" logs -l app.kubernetes.io/component=hami-scheduler --all-containers=true --prefix --tail="$tail_lines" --since="$since"
collect device-plugin-logs logs/device-plugin.txt kubectl "${k[@]}" -n "$namespace" logs -l app.kubernetes.io/component=hami-device-plugin --all-containers=true --prefix --tail="$tail_lines" --since="$since"
for tool in nvidia-smi rocm-smi ascend-smi; do if command -v "$tool" >/dev/null; then collect "vendor-$tool" "vendor/$tool.txt" "$tool" -a; else record skipped "vendor-$tool" "artifacts/vendor/$tool.txt" unavailable; fi; done

{
  echo '{"formatVersion":1,"collectors":['; first=true
  while IFS=$'\t' read -r status id path detail; do [[ "$first" == true ]] || echo ','; first=false; printf '{"status":"%s","collector":"%s","path":"%s"' "$status" "$id" "$path"; [[ -z "$detail" ]] || printf ',"detail":"%s"' "$detail"; printf '}'; done <"$records"
  echo ']}'
} >"$bundle_dir/manifest.json"
rm -f "$records" "$pattern_file"
if [[ "$dry_run" == true ]]; then rm -rf "$bundle_dir"; echo 'dry run complete; no bundle was written'; exit 0; fi
tar --sort=name --mtime='UTC 1970-01-01' --owner=0 --group=0 --numeric-owner -C "$output_dir" -cf - "$bundle_name" | gzip -n >"$archive"
rm -rf "$bundle_dir"
echo "support bundle written to $archive"
