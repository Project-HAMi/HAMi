// Copyright 2021 Cambricon, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package mlu

import pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"

const (
	serverSock               = pluginapi.DevicePluginPath + "cambricon.sock"
	mluLinkPolicyUnsatisfied = "mluLinkPolicyUnsatisfied"
	retries                  = 5

	BestEffort string = "best-effort"
	Restricted string = "restricted"
	Guaranteed string = "guaranteed"

	sriov         string = "sriov"
	envShare      string = "env-share"
	topologyAware string = "topology-aware"
	mluShare      string = "mlu-share"

	mluMonitorDeviceName     = "/dev/cambricon_ctl"
	mluDeviceName            = "/dev/cambricon_dev"
	mluMsgqDeviceName        = "/dev/cambr-msgq"
	mluRPCDeviceName         = "/dev/cambr-rpc"
	mluCmsgDeviceName        = "/dev/cmsg_ctrl"
	mluIpcmDeviceName        = "/dev/cambricon_ipcm"
	mluCommuDeviceName       = "/dev/commu"
	mluUARTConsoleDeviceName = "/dev/ttyMS"
	mluRPMsgDir              = "/dev/cambricon/"
	mluSplitDeviceName       = "/dev/cambricon-split"

	mluMemResourceName       = "cambricon.com/mlumem"
	mluResourceCount         = "cambricon.com/mlunum"
	mluMemResourceAssumeTime = "CAMBRICON_MEM_ASSUME_TIME"
	mluMemResourceAssigned   = "CAMBRICON_MEM_ASSIGHED"
	mluMemSplitLimit         = "CAMBRICON_SPLIT_MEMS"
	mluMemSplitIndex         = "CAMBRICON_SPLIT_VISIBLE_DEVICES"
	mluMemSplitEnable        = "CAMBRICON_SPLIT_ENABLE"
	mluMemLock               = "cambricon.com/mlu-mem.lock"
	mluMemBinaryPath         = "/usr/bin/smlu-containerd"
)
