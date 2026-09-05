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

package ascend

import (
	"maps"
	"math"
	"time"

	"github.com/Project-HAMi/HAMi/pkg/util"

	"k8s.io/apimachinery/pkg/util/cache"
)

// overwriteEnvEntriesCache memoizes the decoded container-level OverwriteEnv
// JSON (hami.io/overwrite-env-containers). The webhook calls MutateAdmission
// once per registered Ascend chip (7 on a typical node), so without the cache
// the same JSON would be decoded 7× per container — and, for a malformed
// value, its warning logged 7×. The pod-level annotation is deliberately not
// cached: strconv.ParseBool is cheaper than a cache lookup, so an invalid
// pod-level value may log its warning per chip.
//
// The key is the raw JSON content (not a pod identity): identical values
// across pods share one entry, and a changed annotation is a different key —
// the same key always decodes to the same result, so expiry cannot improve
// correctness and the LRU capacity is the only bound needed. The TTL is the
// maximum duration solely because LRUExpireCache's API requires one. Error
// results (nil) are cached too, so a malformed JSON is decoded and logged
// exactly once per distinct value.
var overwriteEnvEntriesCache = cache.NewLRUExpireCache(256)

const overwriteEnvCacheTTL = time.Duration(math.MaxInt64)

// cachedContainerOverwriteEnv decodes the container-level JSON once and caches
// the result (including nil for a malformed JSON) by the raw string, so the 7×
// per-chip calls don't re-decode or re-warn. An empty rawJSON returns nil
// without touching the cache (no entry for "no annotation"). A shallow copy of
// the decoded map is returned so callers cannot mutate the cached entry.
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
