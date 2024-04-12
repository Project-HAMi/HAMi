/**
# Copyright (c) 2022, NVIDIA CORPORATION.  All rights reserved.
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

package edits

import (
	"fmt"

	"github.com/NVIDIA/nvidia-container-toolkit/internal/discover"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/logger"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/oci"
	ociSpecs "github.com/opencontainers/runtime-spec/specs-go"
	"tags.cncf.io/container-device-interface/pkg/cdi"
	"tags.cncf.io/container-device-interface/specs-go"
)

type edits struct {
	cdi.ContainerEdits
	logger logger.Interface
}

// NewSpecEdits creates a SpecModifier that defines the required OCI spec edits (as CDI ContainerEdits) from the specified
// discoverer.
func NewSpecEdits(logger logger.Interface, d discover.Discover) (oci.SpecModifier, error) {
	c, err := FromDiscoverer(d)
	if err != nil {
		return nil, fmt.Errorf("error constructing container edits: %v", err)
	}
	e := edits{
		ContainerEdits: *c,
		logger:         logger,
	}

	return &e, nil
}

// FromDiscoverer creates CDI container edits for the specified discoverer.
func FromDiscoverer(d discover.Discover) (*cdi.ContainerEdits, error) {
	devices, err := d.Devices()
	if err != nil {
		return nil, fmt.Errorf("failed to discover devices: %v", err)
	}

	mounts, err := d.Mounts()
	if err != nil {
		return nil, fmt.Errorf("failed to discover mounts: %v", err)
	}

	hooks, err := d.Hooks()
	if err != nil {
		return nil, fmt.Errorf("failed to discover hooks: %v", err)
	}

	c := NewContainerEdits()
	for _, d := range devices {
		edits, err := device(d).toEdits()
		if err != nil {
			return nil, fmt.Errorf("failed to created container edits for device: %v", err)
		}
		c.Append(edits)
	}

	for _, m := range mounts {
		c.Append(mount(m).toEdits())
	}

	for _, h := range hooks {
		c.Append(hook(h).toEdits())
	}

	return c, nil
}

// NewContainerEdits is a utility function to create a CDI ContainerEdits struct.
func NewContainerEdits() *cdi.ContainerEdits {
	c := cdi.ContainerEdits{
		ContainerEdits: &specs.ContainerEdits{},
	}
	return &c
}

// Modify applies the defined edits to the incoming OCI spec
func (e *edits) Modify(spec *ociSpecs.Spec) error {
	if e == nil || e.ContainerEdits.ContainerEdits == nil {
		return nil
	}

	e.logger.Info("Mounts:")
	for _, mount := range e.Mounts {
		e.logger.Infof("Mounting %v at %v", mount.HostPath, mount.ContainerPath)
	}
	e.logger.Infof("Devices:")
	for _, device := range e.DeviceNodes {
		e.logger.Infof("Injecting %v", device.Path)
	}
	e.logger.Infof("Hooks:")
	for _, hook := range e.Hooks {
		e.logger.Infof("Injecting %v %v", hook.Path, hook.Args)
	}

	return e.Apply(spec)
}
