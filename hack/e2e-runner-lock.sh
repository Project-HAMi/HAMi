#!/usr/bin/env bash
# Copyright 2026 The HAMi Authors.
# Licensed under the Apache License, Version 2.0 (the "License");
# You may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Serialize e2e jobs from hami and hami-commercial on a shared self-hosted runner.
# GitHub Actions steps run in separate shells, so flock file descriptors cannot
# span steps. mkdir is atomic and the lock directory persists across steps.

set -o errexit
set -o nounset
set -o pipefail

LOCK_DIR="${HAMI_E2E_LOCK_DIR:-/tmp/hami-e2e-runner.lock.d}"
WAIT_INTERVAL="${HAMI_E2E_LOCK_WAIT_INTERVAL:-30}"
TIMEOUT="${HAMI_E2E_LOCK_TIMEOUT:-7200}"
STALE_AFTER="${HAMI_E2E_LOCK_STALE_AFTER:-14400}"

lock_info_mtime() {
  if [[ ! -d "${LOCK_DIR}" ]]; then
    echo 0
    return
  fi
  local mtime
  mtime=$(stat -c %Y "${LOCK_DIR}" 2>/dev/null || stat -f %m "${LOCK_DIR}" 2>/dev/null)
  echo "${mtime:-0}"
}

remove_stale_lock() {
  if [[ ! -d "${LOCK_DIR}" ]]; then
    return 1
  fi

  local now lock_mtime
  now="$(date +%s)"
  lock_mtime="$(lock_info_mtime)"
  if (( now - lock_mtime > STALE_AFTER )); then
    echo "Removing stale e2e runner lock (older than ${STALE_AFTER}s): ${LOCK_DIR}"
    rm -rf "${LOCK_DIR}"
    return 0
  fi

  return 1
}

acquire_lock() {
  local start now elapsed
  start="$(date +%s)"

  while true; do
    if mkdir "${LOCK_DIR}" 2>/dev/null; then
      cat > "${LOCK_DIR}/info" <<EOF
repository=${GITHUB_REPOSITORY:-unknown}
run_id=${GITHUB_RUN_ID:-unknown}
run_attempt=${GITHUB_RUN_ATTEMPT:-unknown}
workflow=${GITHUB_WORKFLOW:-unknown}
job=${GITHUB_JOB:-unknown}
actor=${GITHUB_ACTOR:-unknown}
pid=$$
started_at=$(date +"%Y-%m-%dT%H:%M:%S%z")
EOF
      echo "Acquired shared e2e runner lock: ${LOCK_DIR}"
      return 0
    fi

    if remove_stale_lock; then
      continue
    fi

    if [[ -f "${LOCK_DIR}/info" ]]; then
      echo "Shared e2e runner is busy:"
      sed 's/^/  /' "${LOCK_DIR}/info"
    else
      echo "Shared e2e runner lock exists but info file is missing, waiting..."
    fi

    now="$(date +%s)"
    elapsed=$(( now - start ))
    if (( elapsed > TIMEOUT )); then
      echo "Timed out waiting for shared e2e runner lock after ${TIMEOUT}s"
      exit 1
    fi

    echo "Waiting for shared e2e runner lock... (${elapsed}s elapsed)"
    sleep "${WAIT_INTERVAL}"
  done
}

release_lock() {
  if [[ ! -d "${LOCK_DIR}" ]]; then
    echo "Shared e2e runner lock already released: ${LOCK_DIR}"
    return 0
  fi

  if [[ -f "${LOCK_DIR}/info" ]]; then
    local holder_run_id holder_repository
    holder_run_id="$(grep -E '^run_id=' "${LOCK_DIR}/info" | cut -d= -f2- || true)"
    holder_repository="$(grep -E '^repository=' "${LOCK_DIR}/info" | cut -d= -f2- || true)"
    if [[ -n "${GITHUB_RUN_ID:-}" && "${holder_run_id}" != "${GITHUB_RUN_ID}" ]]; then
      echo "Skip releasing lock owned by ${holder_repository} run ${holder_run_id}"
      return 0
    fi
  fi

  rm -rf "${LOCK_DIR}"
  echo "Released shared e2e runner lock: ${LOCK_DIR}"
}

usage() {
  echo "Usage: $0 acquire|release"
  exit 1
}

case "${1:-}" in
  acquire) acquire_lock ;;
  release) release_lock ;;
  *) usage ;;
esac
