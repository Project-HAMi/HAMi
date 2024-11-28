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

set -x


REPO_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
cd "${REPO_ROOT}"

source "${REPO_ROOT}"/hack/util.sh

function install_govc() {
    local govc_version="v0.37.3"
    local govc_tar_url="https://github.com/vmware/govmomi/releases/download/${govc_version}/govc_Linux_x86_64.tar.gz"

    wget -q $govc_tar_url || { echo "Failed to download govc"; exit 1; }
    tar -zxvf govc_Linux_x86_64.tar.gz
    mv govc /usr/local/bin/
    govc version
}

function govc_poweron_vm() {
    local vm_name=${1:-""}
    local vm_ip=${2:-""}
    if [[ -z "$vm_name" ]]; then
        echo "Error: VM name is required"
        return 1
    fi

    govc vm.power -on "$vm_name"
    echo -e "\033[35m === $vm_name: power turned on === \033[0m"
    until [[ $(govc vm.info "$vm_name" | grep -c poweredOn) -eq 1 ]]; do
        sleep 5
    done

    util::wait_ip_reachable "$vm_ip"
}

function govc_poweroff_vm() {
    local vm_name=${1:-""}
    if [[ -z "$vm_name" ]]; then
        echo "Error: VM name is required"
        return 1
    fi

    if [[ $(govc vm.info "$vm_name" | grep -c poweredOn) -eq 1 ]]; then
        govc vm.power -off -force "$vm_name"
        echo -e "\033[35m === $vm_name has been down === \033[0m"
    fi
}

function govc_restore_vm_snapshot() {
    local vm_name=${1:-""}
    local vm_snapshot_name=${2:-""}

    govc snapshot.revert -vm "$vm_name" "$vm_snapshot_name"
    echo -e "\033[35m === $vm_name reverted to snapshot: $(govc snapshot.tree -vm "$vm_name" -C -D -i -d) === \033[0m"
}

function setup_gpu_test_env() {
    export GOVC_INSECURE=1
    export vm_ip=$VSPHERE_GPU_VM_IP
    export vm_name=$VSPHERE_GPU_VM_NAME
    export vm_snapshot_name=$VSPHERE_GPU_VM_NAME_SNAPSHOT

    echo -n "Preparing: 'govc' existence check - "
    if util::cmd_exist govc; then
        echo "passed"
    else
        echo "installing govc"
        install_govc
    fi

    govc_poweroff_vm "$vm_name"
    govc_restore_vm_snapshot "$vm_name" "$vm_snapshot_name"
    govc_poweron_vm "$vm_name" "$vm_ip"
}


setup_gpu_test_env
