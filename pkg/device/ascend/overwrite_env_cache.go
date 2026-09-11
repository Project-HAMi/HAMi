/*
Copyright 2026 The HAMi Authors.

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

package ascend

import (
	"math"
	"time"

	"github.com/Project-HAMi/HAMi/pkg/util"

	"k8s.io/apimachinery/pkg/util/cache"
)

// overwriteEnvEntriesCache memoizes the decoded container-level OverwriteEnv
// JSON: the webhook calls MutateAdmission once per chip (7 on a typical node),
// so without it the same JSON would be decoded 7× per container. The key is
// the raw JSON content (not a pod identity), so the same key always decodes to
// the same result — expiry cannot improve correctness and the LRU capacity is
// the only bound needed; the TTL is the max duration solely because the API
// requires one. Error results (nil) are cached too, so a malformed JSON is
// decoded and logged exactly once per distinct value. The pod-level annotation
// is not cached: strconv.ParseBool is cheaper than a cache lookup.
var overwriteEnvEntriesCache = cache.NewLRUExpireCache(256)

const overwriteEnvCacheTTL = time.Duration(math.MaxInt64)

// cachedContainerOverwriteEnv resolves ctrName's mode from the container-level
// JSON. The decoded map never leaves the cache layer. listed is false when the
// JSON is empty or malformed, or the container has no entry — the caller falls
// back to the pod-level value.
func cachedContainerOverwriteEnv(rawJSON, ctrName string) (mode util.OverwriteEnvMode, listed bool) {
	if rawJSON == "" {
		return util.OverwriteEnvUnset, false
	}
	v, ok := overwriteEnvEntriesCache.Get(rawJSON)
	if !ok {
		entries, err := util.DecodeContainerOverwriteEnvJSON(rawJSON)
		if err != nil {
			// Cache nil so per-chip calls don't re-decode or re-warn.
			overwriteEnvEntriesCache.Add(rawJSON, map[string]util.OverwriteEnvMode(nil), overwriteEnvCacheTTL)
			return util.OverwriteEnvUnset, false
		}
		overwriteEnvEntriesCache.Add(rawJSON, entries, overwriteEnvCacheTTL)
		v = entries
	}
	entries, _ := v.(map[string]util.OverwriteEnvMode) // nil map is safe to index
	mode, listed = entries[ctrName]
	return mode, listed
}
