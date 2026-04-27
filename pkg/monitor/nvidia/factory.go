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
)

// CacheFactory abstracts version detection and binary casting for shared region cache files.
// Each versioned sub-package (v0, v1, ...) registers its own factory implementations via init().
type CacheFactory interface {
	// Match returns true if this factory can handle the given cache file.
	Match(header *HeaderT, fileSize int64) bool
	// Cast interprets the raw mmap data as the version-specific shared region struct.
	Cast(data []byte) UsageInfo
	// Name returns a human-readable version identifier for logging.
	Name() string
}

var (
	factories   []CacheFactory
	factoriesMu sync.RWMutex
)

// RegisterFactory should be called from a sub-package's init() to register a version factory.
func RegisterFactory(f CacheFactory) {
	factoriesMu.Lock()
	defer factoriesMu.Unlock()
	factories = append(factories, f)
}

// findFactory iterates over registered factories and returns the first one that matches.
func findFactory(header *HeaderT, fileSize int64) CacheFactory {
	factoriesMu.RLock()
	defer factoriesMu.RUnlock()
	for _, f := range factories {
		if f.Match(header, fileSize) {
			return f
		}
	}
	return nil
}
