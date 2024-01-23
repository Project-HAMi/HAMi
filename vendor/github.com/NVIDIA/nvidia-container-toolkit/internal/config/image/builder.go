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

package image

import (
	"fmt"
	"strings"

	"github.com/opencontainers/runtime-spec/specs-go"
)

type builder struct {
	env            map[string]string
	mounts         []specs.Mount
	disableRequire bool
}

// New creates a new CUDA image from the input options.
func New(opt ...Option) (CUDA, error) {
	b := &builder{}
	for _, o := range opt {
		if err := o(b); err != nil {
			return CUDA{}, err
		}
	}
	if b.env == nil {
		b.env = make(map[string]string)
	}

	return b.build()
}

// build creates a CUDA image from the builder.
func (b builder) build() (CUDA, error) {
	if b.disableRequire {
		b.env[envNVDisableRequire] = "true"
	}

	c := CUDA{
		env:    b.env,
		mounts: b.mounts,
	}
	return c, nil
}

// Option is a functional option for creating a CUDA image.
type Option func(*builder) error

// WithDisableRequire sets the disable require option.
func WithDisableRequire(disableRequire bool) Option {
	return func(b *builder) error {
		b.disableRequire = disableRequire
		return nil
	}
}

// WithEnv sets the environment variables to use when creating the CUDA image.
// Note that this also overwrites the values set with WithEnvMap.
func WithEnv(env []string) Option {
	return func(b *builder) error {
		envmap := make(map[string]string)
		for _, e := range env {
			parts := strings.SplitN(e, "=", 2)
			if len(parts) != 2 {
				return fmt.Errorf("invalid environment variable: %v", e)
			}
			envmap[parts[0]] = parts[1]
		}
		return WithEnvMap(envmap)(b)
	}
}

// WithEnvMap sets the environment variable map to use when creating the CUDA image.
// Note that this also overwrites the values set with WithEnv.
func WithEnvMap(env map[string]string) Option {
	return func(b *builder) error {
		b.env = env
		return nil
	}
}

// WithMounts sets the mounts associated with the CUDA image.
func WithMounts(mounts []specs.Mount) Option {
	return func(b *builder) error {
		b.mounts = mounts
		return nil
	}
}
