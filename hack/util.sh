#!/usr/bin/env bash
# Copyright 2024 The HAMi Authors.
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

set -o errexit  # Exit immediately if a command exits with a non-zero status
set -o nounset  # Exit if an unset variable is referenced
set -o pipefail # Exit if any command in a pipeline fails

MIN_Go_VERSION=1.21.0

# Check if a command exists.
function util::cmd_exist {
  command -v "${1}" >/dev/null 2>&1
}

# Verify Go version.
function util::verify_go_version {
  local go_version
  IFS=" " read -ra go_version <<<"$(GOFLAGS='' go version)"
  if [[ "${go_version[2]}" == "devel" ]]; then
    return 0
  fi
  util::vercomp "${go_version[2]#go}" "${MIN_Go_VERSION}"
  if [[ $? -eq 2 ]]; then
    echo "Detected go version: ${go_version[*]}."
    echo "Requires ${MIN_Go_VERSION} or greater."
    echo "Please install ${MIN_Go_VERSION} or later."
    exit 1
  fi
}

# Version comparison function.
function util::vercomp {
  if [[ $1 == $2 ]]; then
    return 0
  fi
  local IFS=.
  local i ver1=($1) ver2=($2)
  for ((i=${#ver1[@]}; i<${#ver2[@]}; i++)); do
    ver1[i]=0
  done
  for ((i=0; i<${#ver1[@]}; i++)); do
    if [[ -z ${ver2[i]} ]]; then
      ver2[i]=0
    fi
    if ((10#${ver1[i]} > 10#${ver2[i]})); then
      return 1
    fi
    if ((10#${ver1[i]} < 10#${ver2[i]})); then
      return 2
    fi
  done
  return 0
}

# Install Helm.
function util::install_helm {
  if util::cmd_exist helm; then
    echo "Helm is already installed."
    return 0
  fi
  echo "Installing Helm..."
  curl https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash
}

# Execute a command and capture output.
function util::exec_cmd {
  if [ $# -eq 0 ]; then
    echo "[Error] No command specified for util::exec_cmd()..."
    exit 2
  fi
  local tmpLog=$(mktemp)
  set +e
  "$@" &> "$tmpLog"
  local ret=$?
  if [ $ret -ne 0 ]; then
    echo "[Error] Failed to execute command: $*"
    echo "Detail logs:"
    cat "$tmpLog"
    rm -f "$tmpLog"
    exit $ret
  fi
  echo "Command executed successfully: $*"
  rm -f "$tmpLog"
  set -e
}

# Check if an IP is reachable.
function util::wait_ip_reachable {
  local vm_ip=${1:-""}
  local loop_time=${2:-10}
  local sleep_time=${3:-60}
  echo "Waiting for IP $vm_ip to be reachable..."
  for ((i=1; i<=loop_time; i++)); do
    if ping -c 1 -W 2 "$vm_ip" &>/dev/null; then
      echo "IP $vm_ip is reachable."
      return 0
    fi
    echo "Attempt $i/$loop_time: IP $vm_ip not reachable. Retrying in $sleep_time seconds..."
    sleep "$sleep_time"
  done
  echo "Error: IP $vm_ip not reachable after $loop_time attempts."
  exit 1
}

# Check Pod status in a namespace.
function util::check_pods_status {
  local kubeconfig=${1:-""}
  local namespace=${2:-""}
  local retries=${3:-10}
  local interval=${4:-30}

  local attempt=0
  local unhealthy_pods

  while (( attempt < retries )); do
    echo "Checking Pod status (Attempt $(( attempt + 1 ))/$retries)..."

    # Checking unhealthy pods in namespaces，ignore the  Running & Succeeded status
    if [[ -z "$namespace" ]]; then
      unhealthy_pods=$(kubectl get po -A --kubeconfig "$kubeconfig" --no-headers --ignore-not-found | awk '!/Running|Succeeded|Completed/ {print $2}')
    else
      unhealthy_pods=$(kubectl get po -n "$namespace" --kubeconfig "$kubeconfig" --no-headers --ignore-not-found | awk '!/Running|Succeeded|Completed/ {print $1}')
    fi

    if [[ -z "$unhealthy_pods" ]]; then
      echo "PASS: All Pods are in Running or Succeeded state."
      return 0
    fi

    echo "Found unhealthy pods:"
    echo "$unhealthy_pods"

    if (( attempt < retries - 1 )); then
      echo "Retrying pod check in ${interval}s..."
      sleep "$interval"
    fi

    (( attempt++ ))
  done

  if [[ -n "$unhealthy_pods" ]]; then
    echo "Found unhealthy pods in namespace $namespace:"
    echo "$unhealthy_pods"

    for pod in $unhealthy_pods; do
      echo "Describing pod: $pod"
      kubectl describe po "$pod" -n "$namespace" --kubeconfig "$kubeconfig"

      echo "Fetching logs for pod: $pod"
      kubectl logs "$pod" -n "$namespace" --kubeconfig "$kubeconfig"
      echo "---------------------------------------------------"
    done

    return 1
  fi
}
