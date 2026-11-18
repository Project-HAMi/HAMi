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

package googletpu

import (
	"cmp"
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
)

const (
	GoogleTPUDevice     = "GoogleTPU"
	GoogleTPUCommonWord = "GoogleTPU"
	// AcceleratorLabel is the node label GKE sets to the TPU type of a node,
	// for example tpu-v5-lite-podslice.
	AcceleratorLabel = "cloud.google.com/gke-tpu-accelerator"
)

type GoogleTPUConfig struct {
	ResourceCountName string `yaml:"resourceCountName"`
}

// GoogleTPUDevices schedules Google TPU nodes on GKE. GKE advertises the chips
// of a node as an extended resource and attaches them to the containers that
// request them. This backend only decides which node a pod goes to and keeps
// count of the chips in use.
type GoogleTPUDevices struct {
	resourceCountName string
}

func InitGoogleTPUDevice(config GoogleTPUConfig) *GoogleTPUDevices {
	device.SupportDevices[GoogleTPUDevice] = "hami.io/google-tpu-devices-allocated"
	return &GoogleTPUDevices{resourceCountName: config.ResourceCountName}
}

func (dev *GoogleTPUDevices) CommonWord() string {
	return GoogleTPUCommonWord
}

func (dev *GoogleTPUDevices) MutateAdmission(ctr *corev1.Container, p *corev1.Pod) (bool, error) {
	request, err := dev.GenerateResourceRequests(ctr)
	return request.Nums > 0, err
}

// GetNodeDevices lists one device per TPU chip in the node capacity. The device
// IDs are built from the node name and the chip index.
func (dev *GoogleTPUDevices) GetNodeDevices(n corev1.Node) ([]*device.DeviceInfo, error) {
	resourceName := corev1.ResourceName(dev.resourceCountName)
	capacity := n.Status.Capacity[resourceName]
	chips := capacity.Value()
	if chips <= 0 {
		return []*device.DeviceInfo{}, fmt.Errorf("device not found %s", dev.resourceCountName)
	}
	// The kubelet leaves unhealthy chips out of Allocatable without naming
	// them. A TPU pod takes every chip of its node, so a node with a chip
	// missing cannot run one.
	allocatable := n.Status.Allocatable[resourceName]
	healthy := allocatable.Value() >= chips
	devType := GoogleTPUDevice
	if accelerator := n.Labels[AcceleratorLabel]; accelerator != "" {
		devType += "-" + accelerator
	}
	nodedevices := make([]*device.DeviceInfo, 0, chips)
	for i := range chips {
		nodedevices = append(nodedevices, &device.DeviceInfo{
			Index:        uint(i),
			ID:           fmt.Sprintf("%s-%s-%d", n.Name, GoogleTPUDevice, i),
			Count:        1,
			Type:         devType,
			Health:       healthy,
			DeviceVendor: GoogleTPUCommonWord,
		})
	}
	return nodedevices, nil
}

func (dev *GoogleTPUDevices) CheckHealth(devType string, n *corev1.Node) (bool, bool) {
	resourceName := corev1.ResourceName(dev.resourceCountName)
	allocatable := n.Status.Allocatable[resourceName]
	capacity := n.Status.Capacity[resourceName]
	// GetNodeDevices builds the device list from these values, so the list is
	// read again when one of them changes.
	observed := fmt.Sprintf("%d\n%s", allocatable.Value(), n.Labels[AcceleratorLabel])
	return device.AllocatableHealth(devType, n.Name, allocatable.Value(), capacity.Value(), observed)
}

func (dev *GoogleTPUDevices) NodeCleanUp(nn string) error {
	return nil
}

func (dev *GoogleTPUDevices) GetResourceNames() device.ResourceNames {
	return device.ResourceNames{ResourceCountName: dev.resourceCountName}
}

// LockNode does nothing. The node lock is taken at bind and released by the
// HAMi device plugin after it allocates the pod's devices, and no HAMi device
// plugin runs on a TPU node.
func (dev *GoogleTPUDevices) LockNode(n *corev1.Node, p *corev1.Pod) error {
	return nil
}

func (dev *GoogleTPUDevices) ReleaseNodeLock(n *corev1.Node, p *corev1.Pod) error {
	return nil
}

func (dev *GoogleTPUDevices) GenerateResourceRequests(ctr *corev1.Container) (device.ContainerDeviceRequest, error) {
	resourceName := corev1.ResourceName(dev.resourceCountName)
	quantity, ok := ctr.Resources.Limits[resourceName]
	if !ok {
		quantity, ok = ctr.Resources.Requests[resourceName]
	}
	if !ok {
		return device.ContainerDeviceRequest{}, nil
	}
	chips, isInt := quantity.AsInt64()
	if !isInt || chips < 0 {
		return device.ContainerDeviceRequest{}, &device.ErrInvalidDeviceRequest{
			Container: ctr.Name,
			Device:    "googletpu",
			Reason:    fmt.Sprintf("%s must be a non-negative integer, got %s", dev.resourceCountName, quantity.String()),
		}
	}
	if chips == 0 {
		return device.ContainerDeviceRequest{}, nil
	}
	return device.ContainerDeviceRequest{Nums: chips, Type: GoogleTPUDevice}, nil
}

func (dev *GoogleTPUDevices) PatchAnnotations(pod *corev1.Pod, annoinput *map[string]string, pd device.PodDevices) map[string]string {
	if devlist, ok := pd[GoogleTPUDevice]; ok && len(devlist) > 0 {
		(*annoinput)[device.SupportDevices[GoogleTPUDevice]] = device.EncodePodSingleDevice(devlist)
	}
	return *annoinput
}

func (dev *GoogleTPUDevices) ScoreNode(node *corev1.Node, podDevices device.PodSingleDevice, previous []*device.DeviceUsage, policy string) float32 {
	return 0
}

func (dev *GoogleTPUDevices) AddResourceUsage(pod *corev1.Pod, n *device.DeviceUsage, ctr *device.ContainerDevice) error {
	n.Used++
	return nil
}

// podChips returns the number of chips the containers of a pod request together.
func (dev *GoogleTPUDevices) podChips(pod *corev1.Pod) int64 {
	if pod == nil {
		return 0
	}
	var chips int64
	for i := range pod.Spec.Containers {
		// The scheduler rejects a pod with an invalid request before Fit runs.
		request, _ := dev.GenerateResourceRequests(&pod.Spec.Containers[i])
		chips += request.Nums
	}
	return chips
}

// Fit gives the chips of a node to one pod. GKE cannot serve a pod that takes
// only some of the chips of its node. On Ironwood the containers of a pod can
// divide the chips between them, so the rule is checked for the pod and each
// container gets the chips it asked for.
func (dev *GoogleTPUDevices) Fit(devices []*device.DeviceUsage, request device.ContainerDeviceRequest, pod *corev1.Pod, nodeInfo *device.NodeInfo, allocated *device.PodDevices) (bool, map[string]device.ContainerDevices, string) {
	klog.V(4).InfoS("Allocating device for container request", "pod", klog.KObj(pod), "card request", request)
	tmpDevs := make(map[string]device.ContainerDevices)
	// podChips leaves out init containers, so an init container is checked on
	// its own request.
	wanted := max(request.Nums, dev.podChips(pod))
	if int(wanted) != len(devices) {
		klog.V(5).InfoS(common.NodeRequiresAllDevices, "pod", klog.KObj(pod), "request", wanted, "node chips", len(devices))
		return false, tmpDevs, common.GenReason(map[string]int{common.NodeRequiresAllDevices: int(wanted)}, len(devices))
	}
	// Take the chips by index, so that a container gets the same chips in
	// whatever order the scheduler passes them.
	ordered := slices.Clone(devices)
	slices.SortFunc(ordered, func(a, b *device.DeviceUsage) int {
		return cmp.Compare(a.Index, b.Index)
	})
	reason := make(map[string]int)
	chips := make(device.ContainerDevices, 0, request.Nums)
	for _, d := range ordered {
		switch {
		case !d.Health:
			reason[common.CardNotHealth]++
		case d.Used >= d.Count:
			reason[common.CardTimeSlicingExhausted]++
		case len(chips) < int(request.Nums):
			chips = append(chips, device.ContainerDevice{
				Idx:  int(d.Index),
				UUID: d.ID,
				Type: request.Type,
			})
		}
	}
	if len(chips) < int(request.Nums) {
		klog.V(5).InfoS("TPU node is not free", "pod", klog.KObj(pod), "reason", reason)
		return false, tmpDevs, common.GenReason(reason, len(devices))
	}
	tmpDevs[request.Type] = chips
	klog.V(4).InfoS("device allocate success", "pod", klog.KObj(pod), "allocate device", tmpDevs)
	return true, tmpDevs, ""
}
