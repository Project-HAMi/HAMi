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
cat >"$tmp/kubectl" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$KUBECTL_LOG"
if [[ " $* " == *" rollout status "* ]]; then echo denied >&2; exit 1; fi
cat <<'OUT'
image: private.example/team/image:v1
token: super-token
extra: custom-secret
OUT
EOF
chmod +x "$tmp/kubectl"
export KUBECTL_LOG="$tmp/kubectl.log"
PATH="$tmp:$PATH" "$root/hack/hami-support-bundle.sh" --kubeconfig "$tmp/config" --context test --namespace hami --pod target --tail-lines 3 --redact-pattern custom-secret --output-dir "$tmp/out"
tar -xzf "$tmp/out/hami-support-bundle.tar.gz" -C "$tmp"
test -f "$tmp/hami-support-bundle/manifest.json"
grep -q '"status":"failed"' "$tmp/hami-support-bundle/manifest.json"
! grep -R -E 'private\.example|super-token|custom-secret' "$tmp/hami-support-bundle"
! grep -qi 'secrets' "$KUBECTL_LOG"
: >"$KUBECTL_LOG"
PATH="$tmp:$PATH" "$root/hack/hami-support-bundle.sh" --kubeconfig "$tmp/config" --context test --namespace hami --dry-run --output-dir "$tmp/dry" >/dev/null
test ! -s "$KUBECTL_LOG"
echo 'support bundle tests passed'
