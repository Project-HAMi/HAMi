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

package nvidia

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
)

type cardCheckCtx struct {
	request     *device.ContainerDeviceRequest
	pod         *corev1.Pod
	allocated   *device.PodDevices
	tmpDevsMap  map[string]device.ContainerDevices
	deviceType  string
	commonWord  string
	nv          *NvidiaGPUDevices
	memreq      int32
	deviceIndex int
	isMutex     bool
}

type cardCheck func(dev *device.DeviceUsage, ctx *cardCheckCtx) string

var cardCheckPipeline = []cardCheck{
	checkCardUUID,
	checkCardTimeSlicing,
	checkCardMutex,
	checkCardQuota,
	checkCardMemory,
	checkCardCore,
	checkCardExclusive,
	checkCardComputeExhausted,
	checkCardCustomRule,
}

func runCardChecks(dev *device.DeviceUsage, ctx *cardCheckCtx) string {
	for _, check := range cardCheckPipeline {
		if reason := check(dev, ctx); reason != "" {
			return reason
		}
	}
	return ""
}

func computeMemreq(req device.ContainerDeviceRequest, dev *device.DeviceUsage) int32 {
	if req.Memreq > 0 {
		return req.Memreq
	}
	if req.MemPercentagereq != 101 && req.Memreq == 0 {
		return dev.Totalmem * req.MemPercentagereq / 100
	}
	return 0
}

func normalizeCoresreq(req *device.ContainerDeviceRequest, pod *corev1.Pod, dev *device.DeviceUsage) {
	if req.Coresreq > 100 {
		klog.ErrorS(nil, "core limit can't exceed 100", "pod", klog.KObj(pod), "device", dev.ID)
		req.Coresreq = 100
	}
}

func checkCardHealth(dev *device.DeviceUsage, pod *corev1.Pod) string {
	if !dev.Health {
		klog.V(5).InfoS(common.CardNotHealth, "pod", klog.KObj(pod), "device", dev.ID, "health", dev.Health)
		return common.CardNotHealth
	}
	return ""
}

func checkCardUUID(dev *device.DeviceUsage, ctx *cardCheckCtx) string {
	if !device.CheckUUID(ctx.pod.GetAnnotations(), dev.ID, GPUUseUUID, GPUNoUseUUID, ctx.commonWord) {
		klog.V(5).InfoS(common.CardUUIDMismatch, "pod", klog.KObj(ctx.pod), "device", dev.ID, "device index", ctx.deviceIndex, "current device info is:", *dev)
		return common.CardUUIDMismatch
	}
	return ""
}

func checkCardTimeSlicing(dev *device.DeviceUsage, ctx *cardCheckCtx) string {
	if dev.Count <= dev.Used {
		klog.V(5).InfoS(common.CardTimeSlicingExhausted, "pod", klog.KObj(ctx.pod), "device", dev.ID, "count", dev.Count, "used", dev.Used)
		return common.CardTimeSlicingExhausted
	}
	return ""
}

func checkCardMutex(dev *device.DeviceUsage, ctx *cardCheckCtx) string {
	if ctx.isMutex && dev.Used > 0 {
		klog.V(5).InfoS(common.ExclusiveDeviceAllocateConflict, "pod", klog.KObj(ctx.pod), "device", dev.ID, "device index", ctx.deviceIndex, "used", dev.Used)
		return common.ExclusiveDeviceAllocateConflict
	}
	return ""
}

func checkCardQuota(dev *device.DeviceUsage, ctx *cardCheckCtx) string {
	if !fitQuota(ctx.tmpDevsMap, ctx.allocated, ctx.pod.Namespace, int64(ctx.memreq), int64(ctx.request.Coresreq)) {
		klog.V(3).InfoS(common.ResourceQuotaNotFit, "pod", ctx.pod.Name, "memreq", ctx.memreq, "coresreq", ctx.request.Coresreq)
		return common.ResourceQuotaNotFit
	}
	return ""
}

func checkCardMemory(dev *device.DeviceUsage, ctx *cardCheckCtx) string {
	if dev.Totalmem-dev.Usedmem < ctx.memreq {
		klog.V(5).InfoS(common.CardInsufficientMemory, "pod", klog.KObj(ctx.pod), "device", dev.ID, "device index", ctx.deviceIndex, "device total memory", dev.Totalmem, "device used memory", dev.Usedmem, "request memory", ctx.memreq)
		return common.CardInsufficientMemory
	}
	return ""
}

func checkCardCore(dev *device.DeviceUsage, ctx *cardCheckCtx) string {
	if dev.Totalcore-dev.Usedcores < ctx.request.Coresreq {
		klog.V(5).InfoS(common.CardInsufficientCore, "pod", klog.KObj(ctx.pod), "device", dev.ID, "device index", ctx.deviceIndex, "device total core", dev.Totalcore, "device used core", dev.Usedcores, "request cores", ctx.request.Coresreq)
		return common.CardInsufficientCore
	}
	return ""
}

func checkCardExclusive(dev *device.DeviceUsage, ctx *cardCheckCtx) string {
	if dev.Totalcore == 100 && ctx.request.Coresreq == 100 && dev.Used > 0 {
		klog.V(5).InfoS(common.ExclusiveDeviceAllocateConflict, "pod", klog.KObj(ctx.pod), "device", dev.ID, "device index", ctx.deviceIndex, "used", dev.Used)
		return common.ExclusiveDeviceAllocateConflict
	}
	return ""
}

func checkCardComputeExhausted(dev *device.DeviceUsage, ctx *cardCheckCtx) string {
	if dev.Totalcore != 0 && dev.Usedcores == dev.Totalcore && ctx.request.Coresreq == 0 {
		klog.V(5).InfoS(common.CardComputeUnitsExhausted, "pod", klog.KObj(ctx.pod), "device", dev.ID, "device index", ctx.deviceIndex)
		return common.CardComputeUnitsExhausted
	}
	return ""
}

func checkCardCustomRule(dev *device.DeviceUsage, ctx *cardCheckCtx) string {
	if !ctx.nv.CustomFilterRule(ctx.allocated, *ctx.request, ctx.tmpDevsMap[ctx.deviceType], dev) {
		klog.V(5).InfoS(common.CardNotFoundCustomFilterRule, "pod", klog.KObj(ctx.pod), "device", dev.ID, "device index", ctx.deviceIndex)
		return common.CardNotFoundCustomFilterRule
	}
	return ""
}
