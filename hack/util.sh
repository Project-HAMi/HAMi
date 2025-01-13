#!/usr/bin/env bash
# Copyright 2024 The HAMi Authors.
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

MIN_Go_VERSION=1.21.0

function util::cmd_exist {
  local CMD=$(command -v ${1})
  if [[ ! -x ${CMD} ]]; then
    return 1
  fi
  return 0
}

function util::verify_go_version {
  local go_version
  IFS=" " read -ra go_version <<<"$(GOFLAGS='' go version)"
  if [[ "${MIN_Go_VERSION}" != $(echo -e "${MIN_Go_VERSION}\n${go_version[2]}" | sort -s -t. -k 1,1 -k 2,2n -k 3,3n | head -n1) && "${go_version[2]}" != "devel" ]]; then
    echo "Detected go version: ${go_version[*]}."
    echo "requires ${MIN_Go_VERSION} or greater."
    echo "Please install ${MIN_Go_VERSION} or later."
    exit 1
  fi
}

# util::install_helm will install the helm command
function util::install_helm {
    curl https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash
}

# util::exec_cmd will using eval to parse command
function util::exec_cmd() {
   if [ $# -eq 0 ] ; then
     echo "[Error] no command specified for util::exec_cmd()..."
     exit 2
   fi
   local tmpLog=$(mktemp)
   set +e
   eval "$@" &> $tmpLog
   if [ $? -ne 0 ];then
      echo "[Error] Failed to do $1. detail logs as below:"
      set +x
      echo "$(cat $tmpLog)"
      set -x
      rm -f $tmpLog
      exit 3
   fi
   echo "$1 successful."
   rm -f $tmpLog
   set -e
}

### Wait a node reachable
function util::wait_ip_reachable(){
    local vm_ip=${1:-""}
    local loop_time=${2:-"10"}
    local sleep_time=${2:-"60"}
    echo "Wait vm_ip=$1 reachable ... "
    for ((i=1;i<=$((loop_time));i++)); do
      pingOK=0
      ping -w 2 -c 1 "${vm_ip}"|grep "0%" || pingOK=false
      echo "==> ping ""${vm_ip}" $pingOK
      if [[ ${pingOK} == false ]];then
        sleep "$sleep_time"
      else
        break
      fi
      if [ $i -eq $((loop_time)) ];then
        echo "node not reachable exit!"
        exit 1
      fi
    done
}

# checking pods in namespace works
function util::check_pods_status() {
  local kubeconfig=${1:-""}
  local namespace=${2:-"hami-system"}

  local unhealthy_pods
  unhealthy_pods=$(kubectl get po -n "$namespace" --kubeconfig "$kubeconfig" | grep -Ev "^(NAME|.*Running.*|.*Succeeded.*)")

  if [[ -n "$unhealthy_pods" ]]; then
    echo "Found unhealthy_pods pods in namespace $namespace:"
    echo "$unhealthy_pods"

    for pod in $unhealthy_pods; do
      echo "Describing pod: $pod"
      kubectl describe po "$pod" -n "$namespace" --kubeconfig "$kubeconfig"

      echo "Fetching logs for pod: $pod"
      kubectl logs "$pod" -n "$namespace" --kubeconfig "$kubeconfig"
      echo "---------------------------------------------------"
    done

    return 1
  else

    echo "PASS: All Pods are in Running state."
    return 0
  fi
}
