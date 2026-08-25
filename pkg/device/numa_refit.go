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

package device

// NumaRefitRequest asks the scheduler to re-run its normal fit for one
// container of an already-bound Pod, restricted to the physical devices whose
// replicas kubelet's Topology Manager left in the allocation's available set.
// The device plugin sends it when the scheduler-annotated device has no
// replica in that set. See the NUMA alignment design discussion in issue
// Project-HAMi/HAMi#2080.
type NumaRefitRequest struct {
	// PodUID guards against reusing a stale Pod object of the same name.
	PodUID       string `json:"podUID"`
	PodNamespace string `json:"podNamespace"`
	PodName      string `json:"podName"`
	NodeName     string `json:"nodeName"`
	// ContainerIndex is the Pod container position the refit applies to,
	// counting init containers first, matching PodDevices ordering. Senders
	// must derive it from the Pod's current to-allocate annotation state,
	// not from kubelet request positions, which shift as annotation entries
	// are consumed during allocation.
	ContainerIndex int `json:"containerIndex"`
	// ContainerName optionally names the container at ContainerIndex so the
	// scheduler can cross-check the index against the Pod spec and refuse a
	// mismatched request.
	ContainerName string `json:"containerName,omitempty"`
	// DeviceType is the device vendor type the refit applies to, for
	// example "NVIDIA".
	DeviceType string `json:"deviceType"`
	// AllowedDeviceUUIDs lists the physical device IDs kubelet can still
	// satisfy the allocation from. The refit must select among these only.
	AllowedDeviceUUIDs []string `json:"allowedDeviceUUIDs"`
}

// NumaRefitResponse carries the scheduler's decision for a NumaRefitRequest.
// Failures are reported in-band, mirroring the /filter and /bind routes.
type NumaRefitResponse struct {
	Succeeded bool `json:"succeeded"`
	// ContainerDevices holds the reselected devices for the container in
	// EncodeContainerDevices format when Succeeded is true. The format does
	// not carry CustomInfo, so MIG selections are out of scope until MIG
	// support gets its own design, matching the hami-core-first scoping in
	// issue #2080.
	ContainerDevices string `json:"containerDevices,omitempty"`
	// FailureReason explains why the refit was refused, for example no
	// allowed device having enough memory or cores.
	FailureReason string `json:"failureReason,omitempty"`
}
