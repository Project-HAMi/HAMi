/*
Copyright 2024 The HAMi Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package client

import (
	"time"

	"k8s.io/client-go/rest"
)

// Option defines a function type for client configuration options.
type Option func(*rest.Config)

// Now we use the default values of kubernetes client, unless HAMi has specific requirements.
const (
	DefaultQPS     float32 = rest.DefaultQPS
	DefaultBurst   int     = rest.DefaultBurst
	DefaultTimeout int     = 0 // seconds, 0 means no timeout, follow the default behavior of kubernetes client.
)

// WithQPS sets the QPS for the client.
func WithQPS(qps float32) Option {
	return func(c *rest.Config) {
		c.QPS = qps
	}
}

// WithBurst sets the burst for the client.
func WithBurst(burst int) Option {
	return func(c *rest.Config) {
		c.Burst = burst
	}
}

// WithTimeout sets the timeout for the client.
func WithTimeout(timeout int) Option {
	return func(c *rest.Config) {
		c.Timeout = time.Duration(timeout) * time.Second
	}
}

// WithDefaults sets default values for the client configuration.
func WithDefaults() Option {
	return func(c *rest.Config) {
		if c.QPS == 0 {
			c.QPS = DefaultQPS
		}
		if c.Burst == 0 {
			c.Burst = DefaultBurst
		}
		if c.Timeout == 0 {
			c.Timeout = time.Duration(DefaultTimeout) * time.Second
		}
	}
}
