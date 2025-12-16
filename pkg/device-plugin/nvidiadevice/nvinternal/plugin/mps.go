/**
# Copyright 2024 NVIDIA CORPORATION
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
**/

/*
 * Modifications Copyright The HAMi Authors. See
 * GitHub history for details.
 */

package plugin

import (
	"context"
	"os/exec"

	kubeletdevicepluginv1beta1 "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"

	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/rm"
)

// tailer tails the contents of a file.
type tailer struct {
	filename string
	cmd      *exec.Cmd
	cancel   context.CancelFunc
}

type Daemon struct {
	rm rm.ResourceManager
	// root represents the root at which the files and folders controlled by the
	// daemon are created. These include the log and pipe directories.
	root string
	// logTailer tails the MPS control daemon logs.
	logTailer *tailer
}

type mpsOptions struct {
	enabled      bool
	resourceName spec.ResourceName
	daemon       *Daemon
	hostRoot     string
}

// getMPSOptions returns the MPS options specified for the resource manager.
// If MPS is not configured and empty set of options is returned.
func (o *options) getMPSOptions(resourceManager rm.ResourceManager) (mpsOptions, error) {
	return mpsOptions{}, nil
}

func (m *mpsOptions) waitForDaemon() error {
	return nil
}

func (m *mpsOptions) updateReponse(response *kubeletdevicepluginv1beta1.ContainerAllocateResponse) {
	return
}
