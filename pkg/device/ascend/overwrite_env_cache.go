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
	"maps"
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

// cachedContainerOverwriteEnv decodes rawJSON once and caches the result by
// the raw string. A shallow copy is returned so callers cannot mutate the
// cached entry.
func cachedContainerOverwriteEnv(rawJSON string) map[string]util.OverwriteEnvMode {
	if rawJSON == "" {
		return nil
	}
	if v, ok := overwriteEnvEntriesCache.Get(rawJSON); ok {
		if entries, ok := v.(map[string]util.OverwriteEnvMode); ok {
			return entries
		}
		// Wrong type stored: unreachable, but recompute rather than panic.
	}
	entries, err := util.DecodeContainerOverwriteEnvJSON(rawJSON)
	if err != nil {
		// DecodeContainerOverwriteEnvJSON already logged the warning. Cache nil
		// so the remaining per-chip calls don't re-decode and re-log.
		overwriteEnvEntriesCache.Add(rawJSON, map[string]util.OverwriteEnvMode(nil), overwriteEnvCacheTTL)
		return nil
	}
	// Return a shallow copy so callers cannot mutate the cached shared map.
	cp := make(map[string]util.OverwriteEnvMode, len(entries))
	maps.Copy(cp, entries)
	overwriteEnvEntriesCache.Add(rawJSON, entries, overwriteEnvCacheTTL)
	return cp
}
