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

// Package conformance holds the behavioural contract shared by every
// device.Devices implementation.
//
// Each backend calls Run from its own tests. Because the checks live here
// rather than in one vendor's test file, a violation is reported against every
// backend that has it, instead of being found and fixed one vendor at a time
// as happened with #2973, #2811, #2809, #2810 and #2815.
package conformance

import (
	"fmt"
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

// reporter is the subset of *testing.T the checks use. The checks take this
// interface rather than *testing.T so this package's own tests can assert that
// a check reports a violation, which a failing subtest cannot express.
type reporter interface {
	Helper()
	Errorf(format string, args ...any)
}

// policyNeutralScorer mirrors the unexported marker interface in
// pkg/scheduler/policy. A backend whose ScoreNode returns a "higher is better"
// score must implement it so OverrideScore can invert that score under the
// spread policy. Go interfaces are structural, so redeclaring it here lets the
// conformance suite check for it without importing the scheduler.
type policyNeutralScorer interface {
	PolicyNeutralScore()
}

// Fixture carries the vendor-specific inputs the suite cannot build for
// itself. Node and NodeInfo are optional; the rest are required.
type Fixture struct {
	// Devices is a list of healthy devices the backend can allocate from. It
	// needs at least two entries so that order-dependent behaviour is
	// observable.
	Devices []*device.DeviceUsage

	// Request is a device request that Devices can satisfy.
	Request device.ContainerDeviceRequest

	// Pod is passed through to Fit and ScoreNode. Its annotations must not
	// select a scheduling policy that changes whether Request fits, because
	// several checks rely on the fixture being allocatable.
	Pod *corev1.Pod

	// Node is passed to ScoreNode. An empty Node is used when nil.
	Node *corev1.Node

	// NodeInfo is passed to Fit. One derived from Node is used when nil.
	NodeInfo *device.NodeInfo
}

func (f Fixture) validate() error {
	if len(f.Devices) < 2 {
		return fmt.Errorf("fixture needs at least two devices, got %d", len(f.Devices))
	}
	for i, d := range f.Devices {
		if d == nil {
			return fmt.Errorf("fixture device %d is nil", i)
		}
		if !d.Health {
			return fmt.Errorf("fixture device %d (%s) must be healthy", i, d.ID)
		}
	}
	if f.Request.Nums <= 0 {
		return fmt.Errorf("fixture request must ask for at least one device, got %d", f.Request.Nums)
	}
	if f.Pod == nil {
		return fmt.Errorf("fixture pod must not be nil")
	}
	return nil
}

func (f Fixture) node() *corev1.Node {
	if f.Node != nil {
		return f.Node
	}
	return &corev1.Node{}
}

func (f Fixture) nodeInfo() *device.NodeInfo {
	if f.NodeInfo != nil {
		return f.NodeInfo
	}
	n := f.node()
	return &device.NodeInfo{ID: n.Name, Node: n}
}

// copyDevices returns an independent copy of the fixture's devices so that
// usage recorded by one check is not visible to the next.
func (f Fixture) copyDevices() []*device.DeviceUsage {
	out := make([]*device.DeviceUsage, len(f.Devices))
	for i, d := range f.Devices {
		out[i] = d.DeepCopy()
	}
	return out
}

type check struct {
	name string
	fn   func(r reporter, dev device.Devices, f Fixture)
}

var checks = []check{
	{"FitRefusalReasonIsParseable", checkFitRefusalReasonIsParseable},
	{"FitAllocationStaysWithinDeviceBudget", checkFitAllocationStaysWithinDeviceBudget},
	{"FitFeasibilityIsIndependentOfDeviceOrder", checkFitFeasibilityIsIndependentOfDeviceOrder},
	{"ScoreNodeDeclaresPolicyNeutrality", checkScoreNodeDeclaresPolicyNeutrality},
	{"EmptyContainerRequestsNoDevices", checkEmptyContainerRequestsNoDevices},
	{"GetNodeDevicesRejectsMissingAnnotation", checkGetNodeDevicesRejectsMissingAnnotation},
}

// Run executes every conformance check against dev.
//
// Do not call it from a parallel test. Backend constructors register into the
// package-level device.InRequestDevices and device.SupportDevices maps, which
// are not safe for concurrent use.
func Run(t *testing.T, dev device.Devices, f Fixture) {
	t.Helper()
	if err := f.validate(); err != nil {
		t.Fatalf("invalid conformance fixture: %v", err)
	}
	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			c.fn(t, dev, f)
		})
	}
}

// checkFitRefusalReasonIsParseable asserts that a refusal from Fit is written
// in the form common.ParseReason reads. The scheduler turns that reason into
// the Pod's scheduling event, so a bare string leaves the user with no
// explanation of why a node was rejected (#2881).
func checkFitRefusalReasonIsParseable(r reporter, dev device.Devices, f Fixture) {
	r.Helper()

	// Marking every device unhealthy is the one refusal that can be provoked
	// without knowing a backend's vendor-specific limits.
	devices := f.copyDevices()
	for _, d := range devices {
		d.Health = false
	}

	allocated := device.PodDevices{}
	fit, _, reason := dev.Fit(devices, f.Request, f.Pod, f.nodeInfo(), &allocated)
	if fit {
		r.Errorf("Fit allocated from a device list in which every device is unhealthy")
		return
	}
	if len(common.ParseReason(reason)) == 0 {
		r.Errorf("Fit refused with reason %q, which common.ParseReason cannot read; "+
			"report refusals through common.GenReason so the Pod gets an event naming the cause", reason)
	}
}

// checkFitAllocationStaysWithinDeviceBudget asserts that applying what Fit
// returned leaves every device inside its own totals. No backend bounds
// AddResourceUsage itself, so the budget is only respected if Fit hands back an
// allocation that fits (#2809, #2810).
func checkFitAllocationStaysWithinDeviceBudget(r reporter, dev device.Devices, f Fixture) {
	r.Helper()

	devices := f.copyDevices()
	allocated := device.PodDevices{}
	fit, containerDevices, reason := dev.Fit(devices, f.Request, f.Pod, f.nodeInfo(), &allocated)
	if !fit {
		r.Errorf("Fit refused the fixture's own request (%q); the fixture must describe an allocatable request", reason)
		return
	}

	byID := make(map[string]*device.DeviceUsage, len(devices))
	for _, d := range devices {
		byID[d.ID] = d
	}

	for _, containerDevice := range containerDevices {
		for i := range containerDevice {
			target, ok := byID[containerDevice[i].UUID]
			if !ok {
				r.Errorf("Fit returned device %q, which was not in the list it was given", containerDevice[i].UUID)
				continue
			}
			if err := dev.AddResourceUsage(f.Pod, target, &containerDevice[i]); err != nil {
				r.Errorf("AddResourceUsage failed for device %q returned by Fit: %v", containerDevice[i].UUID, err)
			}
		}
	}

	for _, d := range devices {
		if d.Count > 0 && d.Used > d.Count {
			r.Errorf("device %q: Used %d exceeds Count %d after applying what Fit returned", d.ID, d.Used, d.Count)
		}
		if d.Totalmem > 0 && d.Usedmem > d.Totalmem {
			r.Errorf("device %q: Usedmem %d exceeds Totalmem %d after applying what Fit returned", d.ID, d.Usedmem, d.Totalmem)
		}
		if d.Totalcore > 0 && d.Usedcores > d.Totalcore {
			r.Errorf("device %q: Usedcores %d exceeds Totalcore %d after applying what Fit returned", d.ID, d.Usedcores, d.Totalcore)
		}
	}
}

// checkFitFeasibilityIsIndependentOfDeviceOrder asserts that reversing the
// candidate list does not change whether the request fits. The scheduler sorts
// candidates by policy and backends are expected to honour that order when
// choosing, so which device is picked may differ; whether one is found at all
// must not (#2811).
func checkFitFeasibilityIsIndependentOfDeviceOrder(r reporter, dev device.Devices, f Fixture) {
	r.Helper()

	forward := f.copyDevices()
	forwardAllocated := device.PodDevices{}
	forwardFit, _, forwardReason := dev.Fit(forward, f.Request, f.Pod, f.nodeInfo(), &forwardAllocated)

	reversed := f.copyDevices()
	slices.Reverse(reversed)
	reversedAllocated := device.PodDevices{}
	reversedFit, _, reversedReason := dev.Fit(reversed, f.Request, f.Pod, f.nodeInfo(), &reversedAllocated)

	if forwardFit != reversedFit {
		r.Errorf("Fit reported feasibility %t for the candidate list (%q) but %t for its reverse (%q); "+
			"which device is chosen may depend on order, whether the request fits must not",
			forwardFit, forwardReason, reversedFit, reversedReason)
	}
}

// checkScoreNodeDeclaresPolicyNeutrality asserts that a backend returning a
// non-zero node score also implements PolicyNeutralScore. NodeScoreList sorts
// descending and Filter takes the last entry, so under the spread policy the
// lowest score wins. An undeclared "higher is better" score is therefore
// inverted and the worst node is picked (#2973).
func checkScoreNodeDeclaresPolicyNeutrality(r reporter, dev device.Devices, f Fixture) {
	r.Helper()

	previous := f.copyDevices()
	devices := f.copyDevices()
	allocated := device.PodDevices{}
	fit, containerDevices, reason := dev.Fit(devices, f.Request, f.Pod, f.nodeInfo(), &allocated)
	if !fit {
		r.Errorf("Fit refused the fixture's own request (%q); the fixture must describe an allocatable request", reason)
		return
	}

	podDevices := make(device.PodSingleDevice, 0, len(containerDevices))
	for _, containerDevice := range containerDevices {
		podDevices = append(podDevices, containerDevice)
	}

	scores := false
	for _, policy := range []string{
		util.NodeSchedulerPolicyBinpack.String(),
		util.NodeSchedulerPolicySpread.String(),
	} {
		if dev.ScoreNode(f.node(), podDevices, previous, policy) != 0 {
			scores = true
		}
	}
	if !scores {
		return
	}
	if _, ok := dev.(policyNeutralScorer); !ok {
		r.Errorf("ScoreNode returns a non-zero score but the backend does not implement PolicyNeutralScore(); " +
			"the spread policy selects the lowest score, so this score is inverted and the worst node wins")
	}
}

// checkEmptyContainerRequestsNoDevices asserts that a container asking for none
// of a backend's resources produces no device request. Resourcereqs treats a
// positive Nums as "this container wants this vendor's devices", so a non-zero
// result here would draw every Pod into the backend.
func checkEmptyContainerRequestsNoDevices(r reporter, dev device.Devices, f Fixture) {
	r.Helper()

	if request := dev.GenerateResourceRequests(&corev1.Container{}); request.Nums != 0 {
		r.Errorf("GenerateResourceRequests asked for %d devices for a container that requests no resources", request.Nums)
	}
}

// checkGetNodeDevicesRejectsMissingAnnotation asserts that a node carrying no
// register annotation is reported as having no devices, rather than panicking
// in the scheduler's registration loop.
func checkGetNodeDevicesRejectsMissingAnnotation(r reporter, dev device.Devices, f Fixture) {
	r.Helper()

	result := safeGetNodeDevices(dev, corev1.Node{})
	if result.panicValue != nil {
		r.Errorf("GetNodeDevices panicked on a Node with no register annotation: %v", result.panicValue)
		return
	}
	if result.err == nil && len(result.infos) > 0 {
		r.Errorf("GetNodeDevices reported %d devices for a Node with no register annotation; "+
			"return an error or no devices so the scheduler does not cache capacity that does not exist",
			len(result.infos))
	}
}

type nodeDevicesResult struct {
	infos      []*device.DeviceInfo
	err        error
	panicValue any
}

// safeGetNodeDevices calls GetNodeDevices and captures a panic, so that a
// backend which panics fails its own check instead of aborting the test binary.
func safeGetNodeDevices(dev device.Devices, node corev1.Node) (result nodeDevicesResult) {
	defer func() {
		if p := recover(); p != nil {
			result.panicValue = p
		}
	}()
	result.infos, result.err = dev.GetNodeDevices(node)
	return result
}
