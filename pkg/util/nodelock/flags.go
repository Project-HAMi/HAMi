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

package nodelock

import (
	"flag"

	"github.com/spf13/pflag"
)

// InitFlags initializes the node lock flags
func InitFlags(fs *pflag.FlagSet) {
	fs.IntVar(&NodeLockExpireTime, "node-lock-expire-time", DefaultNodeLockExpireTime, "The time in minutes after which a node lock is considered expired")
}

// AddFlags adds the node lock flags to the global flag set
func AddFlags() {
	flag.IntVar(&NodeLockExpireTime, "node-lock-expire-time", DefaultNodeLockExpireTime, "The time in minutes after which a node lock is considered expired")
}