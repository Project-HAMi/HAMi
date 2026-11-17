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

package tecorigin

import (
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
)

func newTeco() *TecoDevices {
	return InitTecoDevice(TecoConfig{ResourceCountName: "tecorigin.com/gpucore"})
}

func tecoNode(registerAnno string) corev1.Node {
	n := corev1.Node{}
	n.Name = "tecorigin-node"
	n.Annotations = map[string]string{RegisterAnnosHami: registerAnno}
	return n
}

func tecoUsage(id string, totalCores, usedMask int64) *device.DeviceUsage {
	return &device.DeviceUsage{
		ID:        id,
		Count:     totalCores,
		Totalcore: maskOf(totalCores),
		Usedcores: usedMask,
		Health:    true,
		Used:      0,
	}
}

func TestParseNodeDevices(t *testing.T) {
	t.Run("HAMi CSV", func(t *testing.T) {
		got, err := parseNodeDevices("SN123432552,4,15296,15,Tecorigin,0,true,0,hami-core:SN123432553,3,8192,7,Tecorigin,0,false,1,hami-core:")
		assert.NilError(t, err)
		assert.Equal(t, len(got), 2)
		assert.DeepEqual(t, *got[0], device.DeviceInfo{
			ID: "SN123432552", Count: 4, Devmem: 15296, Devcore: 0b1111,
			Type: TecoCommonWord, Health: true, Index: 0, Mode: nodeMode,
			DeviceVendor: TecoCommonWord,
		})
		assert.DeepEqual(t, *got[1], device.DeviceInfo{
			ID: "SN123432553", Count: 3, Devmem: 8192, Devcore: 0b111,
			Type: TecoCommonWord, Health: false, Index: 1, Mode: nodeMode,
			DeviceVendor: TecoCommonWord,
		})
	})

	t.Run("missing trailing separator and stray spaces", func(t *testing.T) {
		got, err := parseNodeDevices(" SN123432552 , 4 , 15296 , 15 , Tecorigin , 0 , true , 0 , hami-core")
		assert.NilError(t, err)
		assert.Equal(t, len(got), 1)
		assert.Equal(t, got[0].ID, "SN123432552")
		assert.Equal(t, got[0].Devcore, int64(0b1111))
	})

	t.Run("mask and type are normalized", func(t *testing.T) {
		// A plugin that writes the core count into the mask field, a model name
		// into the type field, or nothing into the mode field must not break
		// placement: the mask is derived from the core count, the type is forced
		// to the vendor common word, and the mode defaults to hami-core.
		got, err := parseNodeDevices("SN123432552,4,15296,4,teco-T100,0,true,0,")
		assert.NilError(t, err)
		assert.Equal(t, got[0].Devcore, int64(0b1111))
		assert.Equal(t, got[0].Type, TecoCommonWord)
		assert.Equal(t, got[0].Mode, nodeMode)
	})

	t.Run("numa and index survive", func(t *testing.T) {
		got, err := parseNodeDevices("SN123432552,3,8192,7,Tecorigin,1,true,3,hami-core:")
		assert.NilError(t, err)
		assert.Equal(t, got[0].Numa, 1)
		assert.Equal(t, got[0].Index, uint(3))
		assert.Equal(t, got[0].Devcore, int64(0b111))
	})

	for _, raw := range []string{
		"",
		"broken",
		"SN123432552,4",
		"SN123432552,4,17179869184,teco-T100,false",                                                 // the old teco annotation is not a HAMi CSV
		"SN123432552,4,15296,15,Tecorigin,0,true,0",                                                 // 7-field HAMi CSV: the index would be lost
		"SN123432552,4,15296,15,Tecorigin,0,true,0,hami-core:SN123432553,3,8192,7,Tecorigin,0,true", // 7-field tail after a valid card
		"SN123432552,4,16039018496,15,Tecorigin,0,true,0,hami-core",                                 // memory in bytes
		"SN123432552,0,15296,15,Tecorigin,0,true,0,hami-core",
		"SN123432552,5,15296,15,Tecorigin,0,true,0,hami-core",
		"SN123432552,4,15296,15,Tecorigin,0,true,0,hami-core,extra", // 10 fields
	} {
		_, err := parseNodeDevices(raw)
		assert.Assert(t, err != nil, "expected %q to be rejected", raw)
	}
}

func TestMaskHelpers(t *testing.T) {
	assert.Equal(t, maskOf(0), int64(0))
	assert.Equal(t, maskOf(3), int64(0b111))
	assert.Equal(t, maskOf(4), int64(0b1111))
	assert.Equal(t, maskOf(5), int64(0))

	assert.Equal(t, popcountMask(0b1011), int64(3))
	assert.Equal(t, popcountMask(0), int64(0))

	// The lowest free cores win, so allocations stay packed at the bottom. They
	// need not be adjacent: 0b1101 has cores 0, 2 and 3 free, so a 2-core request
	// takes 0 and 2.
	got, ok := lowestBits(0b1101, 2)
	assert.Assert(t, ok)
	assert.Equal(t, got, int64(0b0101))

	// Free cores separated by a used one still serve a request of that size.
	got, ok = lowestBits(0b1010, 2)
	assert.Assert(t, ok)
	assert.Equal(t, got, int64(0b1010))

	got, ok = lowestBits(0b1111, 4)
	assert.Assert(t, ok)
	assert.Equal(t, got, int64(0b1111))

	// Fewer free cores than asked, and out-of-range counts, must fail.
	_, ok = lowestBits(0b1000, 2)
	assert.Assert(t, !ok)
	_, ok = lowestBits(0b1111, 0)
	assert.Assert(t, !ok)
	_, ok = lowestBits(0b1111, 5)
	assert.Assert(t, !ok)
	_, ok = lowestBits(0, 1)
	assert.Assert(t, !ok)
}

func TestCoreIDsForTroubleshooting(t *testing.T) {
	node := tecoNode("SN123432552,4,15296,15,Tecorigin,0,true,0,hami-core:")
	node.Annotations[TecoCoreIDsAnnos] = `{"SN123432552":[0,1],"SN123432553":[2]}`
	assert.DeepEqual(t, coreIDsForTroubleshooting(node), map[string][]int64{
		"SN123432552": {0, 1},
		"SN123432553": {2},
	})

	delete(node.Annotations, TecoCoreIDsAnnos)
	assert.Assert(t, coreIDsForTroubleshooting(node) == nil)
}

func TestGetNodeDevices(t *testing.T) {
	dev := newTeco()

	t.Run("HAMi CSV", func(t *testing.T) {
		node := tecoNode("SN123432552,4,15296,15,Tecorigin,0,true,0,hami-core:SN123432553,3,8192,7,Tecorigin,0,false,1,hami-core:")
		node.Annotations[TecoCoreIDsAnnos] = `{"SN123432552":[0,1]}`

		got, err := dev.GetNodeDevices(node)
		assert.NilError(t, err)
		// The unhealthy card is dropped; the surviving one keeps its index (0).
		assert.Equal(t, len(got), 1)

		g := got[0]
		assert.Equal(t, g.ID, "SN123432552")
		assert.Equal(t, g.Index, uint(0))
		assert.Equal(t, g.Count, int64(4))
		assert.Equal(t, g.Devmem, int64(15296))
		assert.Equal(t, g.Devcore, int64(0b1111))
		assert.Equal(t, g.Type, TecoDevice)
		assert.Equal(t, g.DeviceVendor, TecoCommonWord)
		assert.Equal(t, g.Mode, nodeMode)
		assert.Assert(t, g.Health)
		assert.Assert(t, g.CustomInfo["model"] == nil, "the HAMi CSV has no model field")
		assert.DeepEqual(t, g.CustomInfo["coreIDs"], []int64{0, 1})
	})

	t.Run("core id annotation is optional and matched by card id", func(t *testing.T) {
		node := tecoNode("SN123432552,4,15296,15,Tecorigin,0,true,0,hami-core:")

		// Absent annotation: the card must not grow an empty CustomInfo map.
		got, err := dev.GetNodeDevices(node)
		assert.NilError(t, err)
		assert.Assert(t, got[0].CustomInfo == nil)

		// An entry for another card must not attach to this one.
		node.Annotations[TecoCoreIDsAnnos] = `{"SN123432553":[0,1]}`
		got, err = dev.GetNodeDevices(node)
		assert.NilError(t, err)
		assert.Assert(t, got[0].CustomInfo == nil)

		// A matching entry is merged and left available to the caller.
		node.Annotations[TecoCoreIDsAnnos] = `{"SN123432552":[4,5,6,7]}`
		got, err = dev.GetNodeDevices(node)
		assert.NilError(t, err)
		assert.DeepEqual(t, got[0].CustomInfo["coreIDs"], []int64{4, 5, 6, 7})
	})

	t.Run("pre-HAMi annotation is rejected", func(t *testing.T) {
		node := tecoNode("SN123432552,4,17179869184,teco-T100,false")
		_, err := dev.GetNodeDevices(node)
		assert.Assert(t, err != nil)
	})

	missing := corev1.Node{}
	_, err := dev.GetNodeDevices(missing)
	assert.Assert(t, err != nil)

	malformed := tecoNode("broken")
	_, err = dev.GetNodeDevices(malformed)
	assert.Assert(t, err != nil)

	unhealthy := tecoNode("SN123432552,4,15296,15,Tecorigin,0,false,0,hami-core:")
	got, err := dev.GetNodeDevices(unhealthy)
	assert.Assert(t, err != nil)
	assert.Equal(t, len(got), 0)
}

// tecoAllocatableNode is a node as the device plugin leaves it: the annotation
// plus the advertised core count. Registration keys off both, so a CheckHealth
// test without Allocatable would only exercise the "device gone" path.
func tecoAllocatableNode(name, cores, registerAnno string) corev1.Node {
	n := tecoNode(registerAnno)
	n.Name = name
	n.Status.Allocatable = corev1.ResourceList{
		corev1.ResourceName("tecorigin.com/gpucore"): resource.MustParse(cores),
	}
	return n
}

func TestCheckHealth(t *testing.T) {
	dev := newTeco()

	t.Run("no resource on the node", func(t *testing.T) {
		healthy, needUpdate := dev.CheckHealth(TecoDevice, &corev1.Node{})
		assert.Assert(t, !healthy)
		assert.Assert(t, !needUpdate)
	})

	t.Run("first sight asks for an update", func(t *testing.T) {
		node := tecoAllocatableNode("teco-first-sight", "8", "SN123432552,4,15296,15,Tecorigin,0,true,0,hami-core")
		healthy, needUpdate := dev.CheckHealth(TecoDevice, &node)
		assert.Assert(t, healthy)
		assert.Assert(t, needUpdate, "register only calls addNode when needUpdate is true")

		healthy, needUpdate = dev.CheckHealth(TecoDevice, &node)
		assert.Assert(t, healthy)
		assert.Assert(t, !needUpdate, "an unchanged node must not be re-read every tick")
	})

	t.Run("annotation change asks for a re-read", func(t *testing.T) {
		node := tecoAllocatableNode("teco-annotation-change", "8", "SN123432552,4,15296,15,Tecorigin,0,true,0,hami-core")
		_, needUpdate := dev.CheckHealth(TecoDevice, &node)
		assert.Assert(t, needUpdate)

		node.Annotations[RegisterAnnosHami] = "SN123432552,4,15296,15,Tecorigin,0,true,0,hami-core:SN123432553,4,15296,15,Tecorigin,0,true,1,hami-core"
		healthy, needUpdate := dev.CheckHealth(TecoDevice, &node)
		assert.Assert(t, healthy)
		assert.Assert(t, needUpdate)
	})

	t.Run("core id annotation change asks for a re-read", func(t *testing.T) {
		node := tecoAllocatableNode("teco-core-ids", "8",
			"SN123432552,4,15296,15,Tecorigin,0,true,0,hami-core:")
		_, needUpdate := dev.CheckHealth(TecoDevice, &node)
		assert.Assert(t, needUpdate)

		// The device list is untouched, but CustomInfo would go stale without a
		// re-read, so the fingerprint covers the core-id map as well.
		node.Annotations[TecoCoreIDsAnnos] = `{"SN123432552":[4,5,6,7]}`
		healthy, needUpdate := dev.CheckHealth(TecoDevice, &node)
		assert.Assert(t, healthy)
		assert.Assert(t, needUpdate)
	})

	t.Run("mixed health stays registered", func(t *testing.T) {
		node := tecoAllocatableNode("teco-mixed",
			"7", "SN123432552,4,15296,15,Tecorigin,0,true,0,hami-core:SN123432553,3,15296,7,Tecorigin,0,false,1,hami-core:")
		healthy, needUpdate := dev.CheckHealth(TecoDevice, &node)
		assert.Assert(t, healthy)
		assert.Assert(t, needUpdate)
	})

	t.Run("all cards unhealthy", func(t *testing.T) {
		node := tecoAllocatableNode("teco-all-unhealthy", "4", "SN123432552,4,15296,15,Tecorigin,0,false,0,hami-core")
		healthy, _ := dev.CheckHealth(TecoDevice, &node)
		assert.Assert(t, !healthy)
	})

	t.Run("malformed annotation", func(t *testing.T) {
		node := tecoAllocatableNode("teco-malformed", "4", "broken")
		healthy, _ := dev.CheckHealth(TecoDevice, &node)
		assert.Assert(t, !healthy)
	})
}

func TestGenerateResourceRequests(t *testing.T) {
	dev := newTeco()

	// No resource at all: not a device request.
	req, err := dev.GenerateResourceRequests(&corev1.Container{Name: "c"})
	assert.NilError(t, err)
	assert.DeepEqual(t, req, device.ContainerDeviceRequest{})

	// An explicit zero is "no device", not an invalid request.
	zero := &corev1.Container{
		Name: "c",
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{"tecorigin.com/gpucore": resource.MustParse("0")},
		},
	}
	req, err = dev.GenerateResourceRequests(zero)
	assert.NilError(t, err)
	assert.DeepEqual(t, req, device.ContainerDeviceRequest{})

	// A positive integer is a core count carried in TotalCoresreq.
	positive := &corev1.Container{
		Name: "c",
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{"tecorigin.com/gpucore": resource.MustParse("3")},
		},
	}
	req, err = dev.GenerateResourceRequests(positive)
	assert.NilError(t, err)
	assert.Equal(t, req.Nums, int64(1))
	assert.Equal(t, req.Type, TecoDevice)
	assert.Equal(t, req.TotalCoresreq, int64(3))
	assert.Equal(t, req.MemPercentagereq, int64(101))

	// Requests are read when limits are absent.
	fromRequests := &corev1.Container{
		Name: "c",
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{"tecorigin.com/gpucore": resource.MustParse("2")},
		},
	}
	req, err = dev.GenerateResourceRequests(fromRequests)
	assert.NilError(t, err)
	assert.Equal(t, req.TotalCoresreq, int64(2))

	// Limits win over requests.
	both := &corev1.Container{
		Name: "c",
		Resources: corev1.ResourceRequirements{
			Limits:   corev1.ResourceList{"tecorigin.com/gpucore": resource.MustParse("4")},
			Requests: corev1.ResourceList{"tecorigin.com/gpucore": resource.MustParse("2")},
		},
	}
	req, err = dev.GenerateResourceRequests(both)
	assert.NilError(t, err)
	assert.Equal(t, req.TotalCoresreq, int64(4))

	// Fractional core counts are invalid and must fail closed.
	fractional := &corev1.Container{
		Name: "c",
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{"tecorigin.com/gpucore": resource.MustParse("1.5")},
		},
	}
	_, err = dev.GenerateResourceRequests(fractional)
	assert.Assert(t, err != nil)

	// An unconfigured backend advertises no resource name.
	unconfigured := InitTecoDevice(TecoConfig{})
	req, err = unconfigured.GenerateResourceRequests(positive)
	assert.NilError(t, err)
	assert.DeepEqual(t, req, device.ContainerDeviceRequest{})
}

func TestFit(t *testing.T) {
	tests := []struct {
		name        string
		devices     []*device.DeviceUsage
		want        int64
		annotations map[string]string
		fit         bool
		wantCores   map[string]int64
		reasonKey   string
	}{
		{
			name:      "exact single card",
			devices:   []*device.DeviceUsage{tecoUsage("g0", 4, 0)},
			want:      4,
			fit:       true,
			wantCores: map[string]int64{"g0": 0b1111},
		},
		{
			name:      "partial single card takes the lowest cores",
			devices:   []*device.DeviceUsage{tecoUsage("g0", 4, 0)},
			want:      2,
			fit:       true,
			wantCores: map[string]int64{"g0": 0b0011},
		},
		{
			name:      "allocation starts above a used core",
			devices:   []*device.DeviceUsage{tecoUsage("g0", 4, 0b0001)},
			want:      2,
			fit:       true,
			wantCores: map[string]int64{"g0": 0b0110},
		},
		{
			name:      "never spreads when one card fits",
			devices:   []*device.DeviceUsage{tecoUsage("g0", 4, 0), tecoUsage("g1", 4, 0)},
			want:      3,
			fit:       true,
			wantCores: map[string]int64{"g0": 0b0111},
		},
		{
			name:      "uses as few cards as possible",
			devices:   []*device.DeviceUsage{tecoUsage("g0", 4, 0), tecoUsage("g1", 4, 0)},
			want:      5,
			fit:       true,
			wantCores: map[string]int64{"g0": 0b1111, "g1": 0b0001},
		},
		{
			name:      "fills a partly used card before a free one",
			devices:   []*device.DeviceUsage{tecoUsage("g0", 4, 0), tecoUsage("g1", 4, 0b0011)},
			want:      2,
			fit:       true,
			wantCores: map[string]int64{"g1": 0b1100},
		},
		{
			// The runtime accepts an arbitrary core mask, so two free cores that
			// are not adjacent are as good as a contiguous run.
			name:      "non-contiguous free cores are usable",
			devices:   []*device.DeviceUsage{tecoUsage("g0", 4, 0b1010)},
			want:      2,
			fit:       true,
			wantCores: map[string]int64{"g0": 0b0101},
		},
		{
			// Taking the lowest free cores can straddle a used card core; that is
			// exactly what 0b1101 free (0, 2, 3) does for a 2-core request.
			name:      "allocation straddles a used core",
			devices:   []*device.DeviceUsage{tecoUsage("g0", 4, 0b0010)},
			want:      2,
			fit:       true,
			wantCores: map[string]int64{"g0": 0b0101},
		},
		{
			// With fragmentation no longer disqualifying a card, the "fill the
			// emptiest card" rule still prefers the card with less free capacity.
			name:      "fragmented card wins over a free one",
			devices:   []*device.DeviceUsage{tecoUsage("g0", 4, 0), tecoUsage("g1", 4, 0b1010)},
			want:      2,
			fit:       true,
			wantCores: map[string]int64{"g1": 0b0101},
		},
		{
			name:      "takes every remaining core even when none are adjacent",
			devices:   []*device.DeviceUsage{tecoUsage("g0", 4, 0b0010)},
			want:      3,
			fit:       true,
			wantCores: map[string]int64{"g0": 0b1101},
		},
		{
			// Two 3-core cards hold the 6-core request exactly, so they win over
			// the two empty cards even though those could also serve it.
			name: "packs the tightest pair on a small node",
			devices: []*device.DeviceUsage{
				tecoUsage("g0", 4, 0), tecoUsage("g1", 4, 0),
				tecoUsage("g2", 4, 0b0001), tecoUsage("g3", 4, 0b0001),
			},
			want:      6,
			fit:       true,
			wantCores: map[string]int64{"g2": 0b1110, "g3": 0b1110},
		},
		{
			// Same shape as above with 11 cards, past the point where the old
			// implementation switched to a greedy path and picked the emptiest
			// cards instead. Packing must not depend on the card count.
			name: "packs the tightest pair on a large node",
			devices: []*device.DeviceUsage{
				tecoUsage("g0", 4, 0), tecoUsage("g1", 4, 0), tecoUsage("g2", 4, 0),
				tecoUsage("g3", 4, 0b0001), tecoUsage("g4", 4, 0b0001),
				tecoUsage("g5", 4, 0b0001), tecoUsage("g6", 4, 0b0001),
				tecoUsage("g7", 4, 0b0001), tecoUsage("g8", 4, 0b0001),
				tecoUsage("g9", 4, 0b0001), tecoUsage("g10", 4, 0b0001),
			},
			want:      6,
			fit:       true,
			wantCores: map[string]int64{"g3": 0b1110, "g4": 0b1110},
		},
		{
			name:      "refuses to spread beyond the physical minimum",
			devices:   []*device.DeviceUsage{tecoUsage("g0", 4, 0b0001), tecoUsage("g1", 3, 0)},
			want:      4,
			fit:       false,
			reasonKey: common.CardInsufficientCore,
		},
		{
			name:      "not enough cores",
			devices:   []*device.DeviceUsage{tecoUsage("g0", 4, 0)},
			want:      5,
			fit:       false,
			reasonKey: common.CardInsufficientCore,
		},
		{
			name:      "exhausted card",
			devices:   []*device.DeviceUsage{tecoUsage("g0", 4, 0b1111)},
			want:      1,
			fit:       false,
			reasonKey: common.CardInsufficientCore,
		},
		{
			name: "unhealthy card is skipped",
			devices: []*device.DeviceUsage{func() *device.DeviceUsage {
				d := tecoUsage("g0", 4, 0)
				d.Health = false
				return d
			}()},
			want:      1,
			fit:       false,
			reasonKey: common.CardNotHealth,
		},
		{
			name:        "use-gpuuuid restricts placement",
			devices:     []*device.DeviceUsage{tecoUsage("g0", 4, 0), tecoUsage("g1", 4, 0)},
			want:        1,
			annotations: map[string]string{TecoUseUUID: "g1"},
			fit:         true,
			wantCores:   map[string]int64{"g1": 0b0001},
		},
		{
			name:        "nouse-gpuuuid excludes placement",
			devices:     []*device.DeviceUsage{tecoUsage("g0", 4, 0), tecoUsage("g1", 4, 0)},
			want:        1,
			annotations: map[string]string{TecoNoUseUUID: "g0"},
			fit:         true,
			wantCores:   map[string]int64{"g1": 0b0001},
		},
		{
			name:      "zero request is rejected",
			devices:   []*device.DeviceUsage{tecoUsage("g0", 4, 0)},
			want:      0,
			fit:       false,
			reasonKey: common.CardInsufficientCore,
		},
	}

	dev := newTeco()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pod := &corev1.Pod{}
			pod.Annotations = test.annotations
			req := device.ContainerDeviceRequest{
				Nums:             1,
				Type:             TecoDevice,
				TotalCoresreq:    test.want,
				MemPercentagereq: 101,
			}

			fit, got, reason := dev.Fit(test.devices, req, pod, nil, &device.PodDevices{})
			assert.Equal(t, fit, test.fit)
			if !test.fit {
				assert.Assert(t, reason != "")
				if test.reasonKey != "" {
					assert.Equal(t, common.ParseReason(reason)[test.reasonKey], 1)
				}
				return
			}
			assert.Equal(t, reason, "")
			assert.Equal(t, len(got[TecoDevice]), len(test.wantCores))
			for _, alloc := range got[TecoDevice] {
				assert.Equal(t, alloc.Usedcores, test.wantCores[alloc.UUID])
			}
		})
	}
}

func TestFitNilPod(t *testing.T) {
	// Fit never receives a nil pod from the scheduler, but it must not panic on
	// one: a nil pod carries no pin/unpin annotations.
	dev := newTeco()
	req := device.ContainerDeviceRequest{
		Nums: 1, Type: TecoDevice, TotalCoresreq: 2, MemPercentagereq: 101,
	}
	fit, got, reason := dev.Fit(
		[]*device.DeviceUsage{tecoUsage("g0", 4, 0)}, req, nil, nil, &device.PodDevices{})
	assert.Equal(t, fit, true)
	assert.Equal(t, reason, "")
	assert.Equal(t, len(got[TecoDevice]), 1)
	assert.Equal(t, got[TecoDevice][0].Usedcores, int64(0b0011))
}

func TestPatchAnnotations(t *testing.T) {
	dev := newTeco()
	pd := device.PodDevices{TecoDevice: device.PodSingleDevice{
		device.ContainerDevices{{UUID: "g0", Type: TecoDevice, Usedcores: 0b0011}},
		device.ContainerDevices{{UUID: "g0", Type: TecoDevice, Usedcores: 0b0100}},
	}}

	anno := map[string]string{}
	out := dev.PatchAnnotations(&corev1.Pod{}, &anno, pd)
	assert.Assert(t, out[device.InRequestDevices[TecoDevice]] != "")
	assert.Equal(t, out[device.SupportDevices[TecoDevice]], out[device.InRequestDevices[TecoDevice]])

	empty := map[string]string{}
	out = dev.PatchAnnotations(&corev1.Pod{}, &empty, device.PodDevices{})
	assert.Equal(t, len(out), 0)
}

func TestCoreAccounting(t *testing.T) {
	dev := newTeco()

	assert.Equal(t, dev.AccumulateCores(0b0011, 0b0100), int64(0b0111))
	assert.Equal(t, dev.CountAllocatedCores(0b1011), int64(3))

	usage := &device.DeviceUsage{Usedcores: 0b0001, Usedmem: 10}
	ctr := &device.ContainerDevice{Usedcores: 0b0010, Usedmem: 5}
	assert.NilError(t, dev.AddResourceUsage(nil, usage, ctr))
	assert.Equal(t, usage.Used, int64(1))
	assert.Equal(t, usage.Usedcores, int64(0b0011))
	assert.Equal(t, usage.Usedmem, int64(15))
}
