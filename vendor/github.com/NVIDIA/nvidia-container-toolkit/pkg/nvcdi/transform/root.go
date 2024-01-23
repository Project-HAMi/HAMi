/**
# Copyright (c) NVIDIA CORPORATION.  All rights reserved.
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

package transform

import (
	"fmt"
	"path/filepath"
	"strings"

	"tags.cncf.io/container-device-interface/specs-go"
)

type rootTransformer struct {
	root       string
	targetRoot string
}

var _ Transformer = (*rootTransformer)(nil)

// NewRootTransformer creates a new transformer for modifying
// the root for paths in a CDI spec. If both roots are identical,
// this tranformer is a no-op.
func NewRootTransformer(root string, targetRoot string) Transformer {
	if root == targetRoot {
		return NewNoopTransformer()
	}

	t := rootTransformer{
		root:       root,
		targetRoot: targetRoot,
	}
	return t
}

// Transform replaces the root in a spec with a new root.
// It walks the spec and replaces all host paths that start with root with the target root.
func (t rootTransformer) Transform(spec *specs.Spec) error {
	if spec == nil {
		return nil
	}

	for _, d := range spec.Devices {
		d := d
		if err := t.applyToEdits(&d.ContainerEdits); err != nil {
			return fmt.Errorf("failed to apply root transform to device %s: %w", d.Name, err)
		}
	}

	if err := t.applyToEdits(&spec.ContainerEdits); err != nil {
		return fmt.Errorf("failed to apply root transform to spec: %w", err)
	}
	return nil
}

func (t rootTransformer) applyToEdits(edits *specs.ContainerEdits) error {
	for i, dn := range edits.DeviceNodes {
		dn.HostPath = t.transformPath(dn.HostPath)
		edits.DeviceNodes[i] = dn
	}

	for i, hook := range edits.Hooks {
		hook.Path = t.transformPath(hook.Path)

		var args []string
		for _, arg := range hook.Args {
			if !strings.Contains(arg, "::") {
				args = append(args, t.transformPath(arg))
				continue
			}

			// For the 'create-symlinks' hook, special care is taken for the
			// '--link' flag argument which takes the form <target>::<link>.
			// Both paths, the target and link paths, are transformed.
			split := strings.Split(arg, "::")
			if len(split) != 2 {
				return fmt.Errorf("unexpected number of '::' separators in hook argument")
			}
			split[0] = t.transformPath(split[0])
			split[1] = t.transformPath(split[1])
			args = append(args, strings.Join(split, "::"))
		}
		hook.Args = args
		edits.Hooks[i] = hook
	}

	for i, mount := range edits.Mounts {
		mount.HostPath = t.transformPath(mount.HostPath)
		edits.Mounts[i] = mount
	}

	return nil
}

func (t rootTransformer) transformPath(path string) string {
	if !strings.HasPrefix(path, t.root) {
		return path
	}

	return filepath.Join(t.targetRoot, strings.TrimPrefix(path, t.root))
}
