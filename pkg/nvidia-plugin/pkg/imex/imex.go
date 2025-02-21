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

package imex

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/klog/v2"

	spec "github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/api/config/v1"
)

// Channels represents a set of IMEX channels.
type Channels []*Channel

// Channel represents an IMEX channel.
type Channel struct {
	ID       string
	Path     string
	HostPath string
}

// GetChannels returns the set of channels for the given config.
// If the selection of the default IMEX channel is disabled no channels are returned.
func GetChannels(config *spec.Config, devRoot string) (Channels, error) {
	var channels Channels
	for _, channelID := range config.Imex.ChannelIDs {
		id := fmt.Sprintf("%d", channelID)
		channelName := "channel" + id
		path := filepath.Join("/dev/nvidia-caps-imex-channels", channelName)
		channel := Channel{
			ID:       id,
			Path:     path,
			HostPath: filepath.Join(devRoot, path),
		}
		if exists, err := channel.exists(); !exists {
			if config.Imex.Required {
				return nil, errors.Join(err, fmt.Errorf("requested IMEX channel %v does not exist", channelName))
			}
			klog.Warningf("Ignoring requested IMEX channel %v (%v)", channelName, err)
			continue
		}
		klog.Infof("Selecting IMEX channel %v", channelName)
		channels = append(channels, &channel)
	}
	return channels, nil
}

// exists checks whether the IMEX channel exists.
// We check both the Path and HostPath since the location of the device node
// associated with the channel in the container is dependent on how it is
// injected.
// For example, if the host driver root is mounted at /driver-root the channel
// device node would be available at /driver-root/dev even if it was not
// injected into the container through any other mechanism.
// For the case of management containers using CDI to inject device nodes, these
// device nodes would exist at /dev in the container instead.
func (c Channel) exists() (bool, error) {
	paths := []string{c.HostPath}
	if c.HostPath != c.Path {
		paths = append(paths, c.Path)
	}
	var errs error
	for _, path := range paths {
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			errs = errors.Join(errs, err)
			continue
		}

		if info.Mode()&os.ModeCharDevice == 0 {
			errs = errors.Join(errs, fmt.Errorf("%v is not a character device", path))
			continue
		}
		return true, nil
	}
	return false, errs
}
