/*
 * Copyright © 2021 peizhaoyou <peizhaoyou@4paradigm.com>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package k8sutil

import (
    corev1 "k8s.io/api/core/v1"
)

func ResourceCounts(pod *corev1.Pod, resourceName corev1.ResourceName) (counts []int) {
    for _, c := range pod.Spec.Containers {
        v, ok := c.Resources.Limits[resourceName]
        if !ok {
            v, ok = c.Resources.Requests[resourceName]
        }
        if ok {
            if n, ok := v.AsInt64(); ok {
                counts = append(counts, int(n))
            }
        }
    }
    return counts
}

func IsPodInTerminatedState(pod *corev1.Pod) bool {
    return pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded
}