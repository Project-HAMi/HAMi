#!/usr/bin/env bash
# Copyright 2026 The HAMi Authors.
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.

set -o errexit
set -o nounset
set -o pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# Stop a focused test with an actionable error.
fail() { echo "FAIL: $*" >&2; exit 1; }
# Require a regular-expression match in a file.
assert_contains() { grep -Eq -- "$2" "$1" || fail "$1 does not contain: $2"; }
# Require a regular expression to be absent from a file.
assert_not_contains() { if grep -Eq -- "$2" "$1"; then fail "$1 unexpectedly contains: $2"; fi; }
# Reject both singular and plural Kubernetes Secret resource requests.
assert_no_secret_requests() {
  ! grep -Eqi '(^|[^[:alnum:]_])get[[:space:]]+secrets?([^[:alnum:]_]|$)' "$1"
}

mkdir -p "$tmp/bin"
cat >"$tmp/bin/kubectl" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$KUBECTL_LOG"
if [[ "${MOCK_MODE:-}" == sleep ]]; then sleep 5; fi
if [[ "${MOCK_MODE:-}" == large-stderr ]]; then
  echo 'collector output survived'
  exec awk 'BEGIN { for (i=0; i<8192; i++) printf "e" > "/dev/stderr" }'
fi
if [[ " $* " == *" rollout status "* ]]; then echo 'Authorization: Basic Zm9vOmJhcg==' >&2; exit 1; fi
if [[ " $* " == *" get pods "* && " $* " == *" -o name "* ]]; then
  if [[ " $* " == *"hami-device-plugin"* ]]; then
    for number in 7 3 1 6 2 5 4; do echo "pod/device-plugin-$number"; done
  else
    echo pod/scheduler-2
    echo pod/scheduler-1
  fi
  exit 0
fi
if [[ " $* " == *"jsonpath="*"restartCount"* ]]; then
  if [[ " $* " == *"scheduler-"* || " $* " == *"device-plugin-1 "* ]]; then echo 0; else echo 1; fi
  exit 0
fi
if [[ " $* " == *"jsonpath={.spec.nodeName}"* ]]; then echo gpu-node-a; exit 0; fi
if [[ " $* " == *" logs "* && " $* " == *" --previous "* ]]; then echo 'previous container diagnostics'; exit 0; fi
if [[ " $* " == *" logs pod/device-plugin-3 "* ]]; then echo 'one Pod log failed' >&2; exit 1; fi
if [[ "${MOCK_MODE:-}" == large ]]; then
  head -c "${MOCK_BYTES:-4096}" /dev/zero | tr '\0' x
  exit 0
fi
if [[ "${MOCK_MODE:-}" == oversized-json && " $* " == *" get pods "* && " $* " == *" -o yaml "* ]]; then
  printf '{"normal":"'
  head -c 1048576 /dev/zero | tr '\0' x
  printf '"}\n'
  exit 0
fi
cat <<'OUT'
image: private.example/team/image:v1
imageID: containerd://registry.internal/team/image@sha256:1234
nvidia.com/gpu: 1
hami.io/vgpu-devices-allocated: GPU-123,1
Authorization: Bearer bearer-value
Authorization: Basic Zm9vOmJhcg==
{"token":"json-token","apiKey":"json-api-key","normal":"visible"}
{"Authorization":"Basic anNvbi1iYXNpYw==","auth":"docker-auth-value"}
password: yaml-password
access_token: access-token-value
password=equals-password
api_key="quoted equals api key"
endpoint: https://url-user:url-pass@service.example/path
command: tool --password space-separated-password --verbose
args: ["--token=json-array-token","--normal=visible"]
separateArgs:
  - --password
  - yaml-list-password
  - --normal
  - visible-setting
inline_json: {"name":"HF_TOKEN","value":"inline-json-secret"}
eventMessage: Failed to pull image private.event.example/team/image:v1 after resolving reference private.reference.example/team/image:v1
session: eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJzZWNyZXQifQ.signaturevalue123
pem: |
  -----BEGIN PRIVATE KEY-----
  pem-private-key-material
  -----END PRIVATE KEY-----
apiVersion: v1
kind: Pod
metadata:
  name: multiline-redaction-test
  annotations:
    kubectl.kubernetes.io/last-applied-configuration: |
      {"spec":{"containers":[{"env":[{"name":"HF_TOKEN","value":"last-applied-secret"}]}]}}
spec:
  containers:
    - name: test
      env:
        - name: HF_TOKEN
          value: |
            hf-multiline-secret-first

            hf-multiline-secret-after-blank
            nested_key: hf-multiline-secret-nested
        - name: PASSWORD
          value: >
            password-folded-secret-first
            password-folded-secret-second
        - name: API_KEY
          value: |-
            api-key-block-secret
        - name: ACCESS_KEY
          value: |+
            access-key-block-secret
        - name: CLIENT_SECRET
          value: >2
            client-secret-block-value
        - name: PRIVATE_KEY
          value: |2-
            private-key-block-value
        - name: DATABASE_PASSWORD
          value: database-password
        - name: SECRET_KEY
          value: secret-key-value
        - name: JWT_SIGNING_KEY
          value: jwt-signing-key-value
        - name: NORMAL_MULTILINE
          value: |
            normal-env-multiline-first

            normal-env-multiline-after-blank
        - name: NORMAL_SETTING
          value: keep-this-value
      flowProbe: {name: HF_TOKEN, value: flow-style-secret}
    - env:
      - name: DB_PASS
        value: compact-db-password
      - name: PLAIN_SETTING
        value: compact-plain-private-value
      image: compact.private.example/team/image:v1
      name: retained-container-name
      resources:
        limits:
          nvidia.com/gpu: 1
      tolerations:
        - key: retained-toleration-key
      matchExpressions:
        - key: retained-selector-key
          values:
            - retained-selector-value
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: multiline-redaction-test
spec:
  template:
    spec:
      containers:
        - name: test
          env:
            - name: DEPLOYMENT_API_KEY
              value: |2-
                deployment-api-key-secret
            - name: DEPLOYMENT_VISIBLE
              value: |
                deployment-normal-multiline
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: multiline-redaction-test
data:
  password: |
    configmap-password-secret-first

    configmap-password-secret-after-blank
  normal-config: |
    configmap-normal-multiline
password: |-
  multiline-password-first
  multiline-password-second
normal_note: |
  ordinary multiline text
extra: custom-secret
status:
  images:
    - names:
        - private.node.example/team/image:latest
      sizeBytes: 1234
OUT
EOF
chmod +x "$tmp/bin/kubectl"

cat >"$tmp/bin/npu-smi" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$VENDOR_LOG"
echo 'npu diagnostics'
EOF
chmod +x "$tmp/bin/npu-smi"

export KUBECTL_LOG="$tmp/kubectl.log" VENDOR_LOG="$tmp/vendor.log"
export PATH="$tmp/bin:$PATH"

# Pod scope, structured redaction, per-Pod logs, failures, and local vendor tools.
"$root/hack/hami-support-bundle.sh" --kubeconfig "$tmp/config" --context test --namespace hami --pod target --tail-lines 3 --redact-pattern custom-secret --output-dir "$tmp/pod-out"
tar -tzf "$tmp/pod-out/hami-support-bundle.tar.gz" >"$tmp/pod-archive-members.txt"
assert_contains "$tmp/pod-archive-members.txt" '^hami-support-bundle/manifest\.json$'
assert_contains "$tmp/pod-archive-members.txt" '^hami-support-bundle/artifacts/workloads/pod\.yaml$'
mkdir "$tmp/pod-bundle"
tar -xzf "$tmp/pod-out/hami-support-bundle.tar.gz" -C "$tmp/pod-bundle"
bundle="$tmp/pod-bundle/hami-support-bundle"
test -f "$bundle/manifest.json"
assert_contains "$bundle/manifest.json" '"formatVersion":1'
assert_contains "$bundle/manifest.json" '"status":"failed"'
assert_contains "$bundle/manifest.json" '"collector":"node"'
sensitive_fragments=(
  private.example registry.internal bearer-value Zm9vOmJhcg anNvbi1iYXNpYw
  docker-auth-value json-token json-api-key yaml-password access-token-value
  equals-password 'quoted equals api key' hf-multiline-secret password-folded-secret
  api-key-block-secret access-key-block-secret client-secret-block-value
  private-key-block-value deployment-api-key-secret configmap-password-secret
  database-password multiline-password custom-secret secret-key-value
  jwt-signing-key-value flow-style-secret last-applied-secret url-user url-pass
  space-separated-password json-array-token inline-json-secret private.node.example
  yaml-list-password compact-db-password compact-plain-private-value
  private.event.example private.reference.example pem-private-key-material signaturevalue123
  compact.private.example
)
for fragment in "${sensitive_fragments[@]}"; do
  if grep -R -F -- "$fragment" "$bundle" >/dev/null; then
    fail "sensitive value survived redaction: $fragment"
  fi
done
assert_contains "$bundle/artifacts/workloads/pod.yaml" 'nvidia\.com/gpu: 1'
assert_contains "$bundle/artifacts/workloads/pod.yaml" 'hami\.io/vgpu-devices-allocated: GPU-123,1'
assert_contains "$bundle/artifacts/workloads/pod.yaml" 'env: <redacted-environment>'
assert_not_contains "$bundle/artifacts/workloads/pod.yaml" 'keep-this-value'
assert_not_contains "$bundle/artifacts/workloads/pod.yaml" 'normal-env-multiline-first'
assert_not_contains "$bundle/artifacts/workloads/pod.yaml" 'deployment-normal-multiline'
assert_contains "$bundle/artifacts/workloads/pod.yaml" 'image: <redacted-image>'
assert_contains "$bundle/artifacts/workloads/pod.yaml" 'imageID: <redacted-image>'
assert_contains "$bundle/artifacts/workloads/pod.yaml" 'kubectl\.kubernetes\.io/last-applied-configuration: <redacted-credential>'
assert_contains "$bundle/artifacts/workloads/pod.yaml" 'flowProbe: \{name: HF_TOKEN, value: <redacted-credential>\}'
assert_contains "$bundle/artifacts/workloads/pod.yaml" 'endpoint: https://<redacted>@service\.example/path'
assert_contains "$bundle/artifacts/workloads/pod.yaml" 'command: tool --password <redacted-credential> --verbose'
assert_contains "$bundle/artifacts/workloads/pod.yaml" 'args: \["--token=<redacted-credential>","--normal=visible"\]'
assert_contains "$bundle/artifacts/workloads/pod.yaml" 'inline_json: \{"name":"HF_TOKEN","value":"<redacted-credential>"\}'
assert_contains "$bundle/artifacts/workloads/pod.yaml" '- <redacted-credential>'
assert_contains "$bundle/artifacts/workloads/pod.yaml" 'eventMessage: Failed to pull image <redacted-image> after resolving reference <redacted-image>'
assert_contains "$bundle/artifacts/workloads/pod.yaml" 'session: <redacted-jwt>'
assert_contains "$bundle/artifacts/workloads/pod.yaml" '^[[:space:]]*<redacted-private-key>$'
assert_contains "$bundle/artifacts/workloads/pod.yaml" 'name: retained-container-name'
assert_contains "$bundle/artifacts/workloads/pod.yaml" 'resources:'
assert_contains "$bundle/artifacts/workloads/pod.yaml" 'key: retained-toleration-key'
assert_contains "$bundle/artifacts/workloads/pod.yaml" 'key: retained-selector-key'
assert_contains "$bundle/artifacts/workloads/pod.yaml" 'retained-selector-value'
assert_contains "$bundle/artifacts/workloads/pod.yaml" 'images: <redacted-image-inventory>'
assert_contains "$bundle/artifacts/workloads/pod.yaml" 'ordinary multiline text'
assert_contains "$bundle/artifacts/config/scheduler.yaml" 'password: <redacted-credential>'
assert_contains "$bundle/artifacts/config/scheduler.yaml" 'configmap-normal-multiline'
assert_contains "$bundle/artifacts/vendor/npu-smi.txt" 'npu diagnostics'
assert_contains "$VENDOR_LOG" '^info$'
assert_contains "$KUBECTL_LOG" ' get deployments -l app.kubernetes.io/component=hami-scheduler '
assert_contains "$KUBECTL_LOG" ' get daemonsets -l app.kubernetes.io/component=hami-device-plugin '
test "$(grep 'logs pod/device-plugin-' "$KUBECTL_LOG" | grep -vc -- '--previous')" -eq 7 || fail 'did not collect all seven current device-plugin Pod logs'
test "$(grep 'logs pod/device-plugin-' "$KUBECTL_LOG" | grep -c -- '--previous')" -eq 6 || fail 'did not limit previous logs to restarted device-plugin Pods'
device_plugin_log_order=$(grep -v -- '--previous' "$KUBECTL_LOG" | sed -n 's/.*logs pod\/device-plugin-\([0-9][0-9]*\).*/\1/p' | paste -sd ' ' -)
test "$device_plugin_log_order" = '1 2 3 4 5 6 7' || fail "device-plugin logs were not collected deterministically: $device_plugin_log_order"
assert_not_contains "$KUBECTL_LOG" 'logs -l '
assert_not_contains "$KUBECTL_LOG" ' get nodes '
assert_contains "$KUBECTL_LOG" ' get node gpu-node-a -o yaml'
assert_contains "$KUBECTL_LOG" 'logs pod/device-plugin-1 .*--tail=3 --since=1h'
assert_not_contains "$KUBECTL_LOG" 'logs pod/device-plugin-1 --previous'
assert_contains "$KUBECTL_LOG" 'logs pod/device-plugin-2 --previous .*--tail=3 --since=1h'
assert_contains "$bundle/manifest.json" '"status":"failed","collector":"device-plugin-logs-device-plugin-3"'
assert_contains "$bundle/manifest.json" '"status":"skipped","collector":"device-plugin-previous-logs-device-plugin-1".*no-restarted-containers'
assert_not_contains "$bundle/manifest.json" '"status":"failed","collector":"device-plugin-previous-logs-device-plugin-1"'
test -f "$bundle/artifacts/logs/device-plugin/device-plugin-4.txt" || fail 'a Pod log failure stopped later Pod collection'
test -f "$bundle/artifacts/logs/device-plugin/device-plugin-4.previous.txt" || fail 'previous container logs were not collected'
assert_no_secret_requests "$KUBECTL_LOG" || fail 'collector requested a Secret resource'
if grep -Eqi '(^|[[:space:]])(exec|debug|apply|create|delete|patch|replace|scale|set|label|annotate|cordon|drain|taint)([[:space:]]|$)' "$KUBECTL_LOG"; then
  fail 'collector issued a mutating or remote-execution kubectl command'
fi
if awk '!/--request-timeout=30s/ { exit 1 }' "$KUBECTL_LOG"; then :; else fail 'a kubectl command lacks --request-timeout'; fi
if awk -v args="--kubeconfig $tmp/config --context test" 'index($0,args) == 0 { exit 1 }' "$KUBECTL_LOG"; then :; else fail 'a kubectl command lacks the explicit kubeconfig or context'; fi
test "$(stat -c %a "$tmp/pod-out/hami-support-bundle.tar.gz")" = 600 || fail 'archive permissions are not private'

# Prove the Secret API guard recognizes both Kubernetes resource spellings.
printf '%s\n' 'get secret example' >"$tmp/forbidden.log"
if assert_no_secret_requests "$tmp/forbidden.log"; then fail 'singular Secret request was not detected'; fi
printf '%s\n' 'get secrets' >"$tmp/forbidden.log"
if assert_no_secret_requests "$tmp/forbidden.log"; then fail 'plural Secret request was not detected'; fi
if grep -Eqi '^[[:space:]]*collect .*kubectl .*get[[:space:]]+secrets?([[:space:]]|$)' "$root/hack/hami-support-bundle.sh"; then
  fail 'collector source contains a Kubernetes Secret API request'
fi

# A literal contained in "<redacted>" must finish rather than re-scan replacement text.
: >"$KUBECTL_LOG"
timeout 15s "$root/hack/hami-support-bundle.sh" --kubeconfig "$tmp/config" --context test --namespace hami --redact-pattern e --output-dir "$tmp/literal-out" >/dev/null
test -f "$tmp/literal-out/hami-support-bundle.tar.gz"

# Oversized single-line structured output is suppressed without quadratic parsing.
: >"$KUBECTL_LOG"
timeout 15s env MOCK_MODE=oversized-json "$root/hack/hami-support-bundle.sh" --kubeconfig "$tmp/config" --context test --namespace hami --output-dir "$tmp/oversized-line-out" >/dev/null
mkdir "$tmp/oversized-line-bundle"
tar -xzf "$tmp/oversized-line-out/hami-support-bundle.tar.gz" -C "$tmp/oversized-line-bundle"
assert_contains "$tmp/oversized-line-bundle/hami-support-bundle/artifacts/workloads/pods.yaml" '^<redacted-oversized-line>$'

# Namespace and cluster-wide modes must issue different workload/event commands.
kubectl_calls_before_dry_run=$(wc -l <"$KUBECTL_LOG")
"$root/hack/hami-support-bundle.sh" --kubeconfig "$tmp/config" --context test --namespace hami --dry-run --output-dir "$tmp/ns-dry" >"$tmp/ns-dry.txt"
"$root/hack/hami-support-bundle.sh" --kubeconfig "$tmp/config" --context test --namespace hami --cluster-wide --dry-run --output-dir "$tmp/cluster-dry" >"$tmp/cluster-dry.txt"
assert_contains "$tmp/ns-dry.txt" '-n hami get pods -o yaml'
assert_contains "$tmp/ns-dry.txt" '-n hami get events'
assert_contains "$tmp/cluster-dry.txt" '-A get pods -o yaml'
assert_contains "$tmp/cluster-dry.txt" '-A get events'
assert_contains "$tmp/cluster-dry.txt" ' get nodes -o yaml'
assert_contains "$tmp/cluster-dry.txt" '-n hami get configmaps'

# Node mode limits tenant workloads/events to the requested namespace.
"$root/hack/hami-support-bundle.sh" --kubeconfig "$tmp/config" --context test --namespace hami --node gpu-node-a --dry-run --output-dir "$tmp/node-dry" >"$tmp/node-dry.txt"
assert_contains "$tmp/node-dry.txt" '-n hami get pods --field-selector spec.nodeName=gpu-node-a'
assert_contains "$tmp/node-dry.txt" '-n hami get events --field-selector involvedObject.kind=Node'
assert_not_contains "$tmp/node-dry.txt" '-A get pods'
assert_contains "$tmp/node-dry.txt" ' get node gpu-node-a -o yaml'
assert_not_contains "$tmp/node-dry.txt" ' get nodes '
test "$(wc -l <"$KUBECTL_LOG")" -eq "$kubectl_calls_before_dry_run" || fail 'dry-run executed kubectl'

# Scope flags are mutually exclusive.
for conflict in '--pod p --node n' '--pod p --cluster-wide' '--node n --cluster-wide'; do
  read -r -a conflict_args <<<"$conflict"
  if "$root/hack/hami-support-bundle.sh" --kubeconfig "$tmp/config" --context test --namespace hami "${conflict_args[@]}" --output-dir "$tmp/conflict-${conflict// /-}" >"$tmp/conflict.out" 2>&1; then
    fail "accepted conflicting scope: $conflict"
  fi
  assert_contains "$tmp/conflict.out" 'cannot be used together'
done

# Per-collector output is capped and reported as truncated.
: >"$KUBECTL_LOG"
MOCK_MODE=large HAMI_SUPPORT_MAX_COLLECTOR_BYTES=256 "$root/hack/hami-support-bundle.sh" --kubeconfig "$tmp/config" --context test --namespace hami --output-dir "$tmp/limited-out" >/dev/null
mkdir "$tmp/limited-bundle"
tar -xzf "$tmp/limited-out/hami-support-bundle.tar.gz" -C "$tmp/limited-bundle"
limited="$tmp/limited-bundle/hami-support-bundle"
assert_contains "$limited/manifest.json" '"status":"truncated"'
while IFS= read -r artifact; do
  test "$(stat -c %s "$artifact")" -le 256 || fail "collector artifact exceeds limit: $artifact"
done < <(find "$limited/artifacts" "$limited/errors" -type f)

# Capped readers must drain oversized streams so the producer exits naturally.
: >"$KUBECTL_LOG"
MOCK_MODE=large-stderr HAMI_SUPPORT_MAX_COLLECTOR_BYTES=256 "$root/hack/hami-support-bundle.sh" --kubeconfig "$tmp/config" --context test --namespace hami --output-dir "$tmp/drained-out" >/dev/null
mkdir "$tmp/drained-bundle"
tar -xzf "$tmp/drained-out/hami-support-bundle.tar.gz" -C "$tmp/drained-bundle"
drained="$tmp/drained-bundle/hami-support-bundle"
assert_not_contains "$drained/manifest.json" '"status":"failed"'
assert_contains "$drained/manifest.json" '"status":"truncated"'
assert_contains "$drained/manifest.json" 'stderr-truncated-at=256'
assert_contains "$drained/artifacts/workloads/pods.yaml" '^collector output survived$'

# The overall limit stops later collectors and remains machine-readable.
: >"$KUBECTL_LOG"
MOCK_MODE=large MOCK_BYTES=20000 HAMI_SUPPORT_MAX_COLLECTOR_BYTES=16384 HAMI_SUPPORT_MAX_BUNDLE_BYTES=131072 "$root/hack/hami-support-bundle.sh" --kubeconfig "$tmp/config" --context test --namespace hami --cluster-wide --output-dir "$tmp/overall-out" >/dev/null
mkdir "$tmp/overall-bundle"
tar -xzf "$tmp/overall-out/hami-support-bundle.tar.gz" -C "$tmp/overall-bundle"
overall="$tmp/overall-bundle/hami-support-bundle"
assert_contains "$overall/manifest.json" '"status":"limited"'
test "$(du -sb "$overall" | awk '{print $1}')" -le 131072 || fail 'uncompressed bundle exceeds overall limit'

# Private staging exists with mode 700 and is removed after interruption.
mkdir "$tmp/private-staging"
: >"$KUBECTL_LOG"
TMPDIR="$tmp/private-staging" MOCK_MODE=sleep "$root/hack/hami-support-bundle.sh" --kubeconfig "$tmp/config" --context test --namespace hami --output-dir "$tmp/interrupted-out" >/dev/null 2>&1 &
collector_pid=$!
for _ in {1..50}; do
  staging_path=$(find "$tmp/private-staging" -mindepth 1 -maxdepth 1 -type d -name 'hami-support-bundle.*' -print -quit)
  [[ -n "$staging_path" ]] && break
  sleep 0.1
done
[[ -n "${staging_path:-}" ]] || fail 'private staging directory was not created'
test "$(stat -c %a "$staging_path")" = 700 || fail 'staging directory permissions are not private'
if TMPDIR="$tmp/private-staging" "$root/hack/hami-support-bundle.sh" --kubeconfig "$tmp/config" --context test --namespace hami --output-dir "$tmp/interrupted-out" >"$tmp/concurrent.out" 2>&1; then
  fail 'concurrent collector unexpectedly reused the same output destination'
fi
assert_contains "$tmp/concurrent.out" 'already reserved by another collector'
kill -TERM "$collector_pid"
if wait "$collector_pid"; then fail 'interrupted collector unexpectedly succeeded'; fi
test -z "$(find "$tmp/private-staging" -mindepth 1 -maxdepth 1 -name 'hami-support-bundle.*' -print -quit)" || fail 'staging data remained after interruption'

# An untrappable termination still releases the kernel-managed destination lock.
mkdir "$tmp/kill9-staging"
TMPDIR="$tmp/kill9-staging" MOCK_MODE=sleep "$root/hack/hami-support-bundle.sh" --kubeconfig "$tmp/config" --context test --namespace hami --output-dir "$tmp/kill9-out" >/dev/null 2>&1 &
collector_pid=$!
for _ in {1..50}; do
  staging_path=$(find "$tmp/kill9-staging" -mindepth 1 -maxdepth 1 -type d -name 'hami-support-bundle.*' -print -quit)
  [[ -n "$staging_path" ]] && break
  sleep 0.1
done
[[ -n "${staging_path:-}" ]] || fail 'kill -9 collector did not start'
kill -KILL "$collector_pid"
if wait "$collector_pid" 2>/dev/null; then fail 'kill -9 collector unexpectedly succeeded'; fi
"$root/hack/hami-support-bundle.sh" --kubeconfig "$tmp/config" --context test --namespace hami --output-dir "$tmp/kill9-out" >/dev/null
test -f "$tmp/kill9-out/hami-support-bundle.tar.gz" || fail 'kernel-managed lock was not released after kill -9'

echo 'support bundle tests passed'
