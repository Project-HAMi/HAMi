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

package nvidia

import (
	"sync"

	"github.com/Project-HAMi/HAMi/pkg/monitor/nvidia/api"
	nvidiav0 "github.com/Project-HAMi/HAMi/pkg/monitor/nvidia/v0"
	nvidiav1 "github.com/Project-HAMi/HAMi/pkg/monitor/nvidia/v1"
)

var registerBuiltinsOnce sync.Once

func ensureBuiltinsRegistered() {
	registerBuiltinsOnce.Do(func() {
		nvidiav0.Register()
		nvidiav1.Register()
	})
}

func findFactory(header *HeaderT, fileSize int64) api.CacheFactory {
	ensureBuiltinsRegistered()
	return api.FindFactory(header, fileSize)
}
