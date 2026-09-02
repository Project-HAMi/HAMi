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

// overwriteEnv decode caches. The webhook calls MutateAdmission once per
// registered Ascend chip (7 on a typical node), so without these caches the
// same pod-level value and container-level JSON would be re-parsed 7× per
// non-Ascend container — and, for invalid inputs, the same klog warning would
// fire 7×.
//
// Keys are the raw annotation content (not a pod identity): identical values
// across pods share one entry, a changed annotation is a different key (no
// stale risk, no TTL needed), and the LRU capacity bounds memory. Entries
// never expire (ttl = math.MaxInt64). Error results (nil) are cached too, so a
// malformed JSON is decoded and logged exactly once per distinct value.
var (
	overwriteEnvPodCache     = cache.NewLRUExpireCache(256)
	overwriteEnvEntriesCache = cache.NewLRUExpireCache(256)
)

const overwriteEnvCacheTTL = time.Duration(math.MaxInt64)

// cachedPodOverwriteEnv parses the pod-level annotation value once and caches
// the result (including Unset for an invalid value) by the raw string, so the
// 7× per-chip calls don't re-parse or re-warn. strconv.ParseBool is cheap, but
// an invalid value would otherwise log the same warning 7×.
func cachedPodOverwriteEnv(podVal string) util.OverwriteEnvMode {
	if podVal == "" {
		return util.OverwriteEnvUnset
	}
	if v, ok := overwriteEnvPodCache.Get(podVal); ok {
		if mode, ok := v.(util.OverwriteEnvMode); ok {
			return mode
		}
		// Wrong type stored: unreachable, but recompute rather than panic.
	}
	mode, _ := util.ParsePodOverwriteEnv(podVal)
	overwriteEnvPodCache.Add(podVal, mode, overwriteEnvCacheTTL)
	return mode
}

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
