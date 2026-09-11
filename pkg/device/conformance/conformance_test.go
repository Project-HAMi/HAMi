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

package conformance

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
)

const stubType = "STUB"

// recordingReporter collects what a check reported instead of failing the test,
// so a test can assert that a violation was caught.
type recordingReporter struct {
	messages []string
}

func (r *recordingReporter) Helper() {}

func (r *recordingReporter) Errorf(format string, args ...any) {
	r.messages = append(r.messages, fmt.Sprintf(format, args...))
}

func (r *recordingReporter) failed() bool {
	return len(r.messages) > 0
}

func (r *recordingReporter) joined() string {
	return strings.Join(r.messages, "\n")
}

// stubDevices is a device.Devices whose default behaviour satisfies every
// check. Each test replaces one hook to confirm the matching check reports the
// violation it exists to catch.
type stubDevices struct {
	fit            func(devices []*device.DeviceUsage, request device.ContainerDeviceRequest) (bool, map[string]device.ContainerDevices, string)
	addUsage       func(n *device.DeviceUsage, ctr *device.ContainerDevice) error
	scoreNode      func(policy string) float32
	generate       func(ctr *corev1.Container) device.ContainerDeviceRequest
	getNodeDevices func(n corev1.Node) ([]*device.DeviceInfo, error)
}

func (s *stubDevices) CommonWord() string { return stubType }

func (s *stubDevices) MutateAdmission(ctr *corev1.Container, pod *corev1.Pod) (bool, error) {
	return false, nil
}

func (s *stubDevices) CheckHealth(devType string, n *corev1.Node) (bool, bool) { return true, true }

func (s *stubDevices) NodeCleanUp(nn string) error { return nil }

func (s *stubDevices) GetResourceNames() device.ResourceNames {
	return device.ResourceNames{
		ResourceCountName:  "stub.io/dev",
		ResourceMemoryName: "stub.io/devmem",
		ResourceCoreName:   "stub.io/devcore",
	}
}

func (s *stubDevices) GetNodeDevices(n corev1.Node) ([]*device.DeviceInfo, error) {
	if s.getNodeDevices != nil {
		return s.getNodeDevices(n)
	}
	return nil, errors.New("stub: no register annotation on node")
}

func (s *stubDevices) LockNode(n *corev1.Node, p *corev1.Pod) error { return nil }

func (s *stubDevices) ReleaseNodeLock(n *corev1.Node, p *corev1.Pod) error { return nil }

func (s *stubDevices) GenerateResourceRequests(ctr *corev1.Container) device.ContainerDeviceRequest {
	if s.generate != nil {
		return s.generate(ctr)
	}
	if _, ok := ctr.Resources.Limits[corev1.ResourceName("stub.io/dev")]; !ok {
		return device.ContainerDeviceRequest{}
	}
	return device.ContainerDeviceRequest{Nums: 1, Type: stubType}
}

func (s *stubDevices) PatchAnnotations(pod *corev1.Pod, annoinput *map[string]string, pd device.PodDevices) map[string]string {
	return *annoinput
}

func (s *stubDevices) ScoreNode(node *corev1.Node, podDevices device.PodSingleDevice, previous []*device.DeviceUsage, policy string) float32 {
	if s.scoreNode != nil {
		return s.scoreNode(policy)
	}
	return 0
}

func (s *stubDevices) AddResourceUsage(pod *corev1.Pod, n *device.DeviceUsage, ctr *device.ContainerDevice) error {
	if s.addUsage != nil {
		return s.addUsage(n, ctr)
	}
	n.Used++
	n.Usedmem += ctr.Usedmem
	n.Usedcores += ctr.Usedcores
	return nil
}

func (s *stubDevices) Fit(devices []*device.DeviceUsage, request device.ContainerDeviceRequest, pod *corev1.Pod, nodeInfo *device.NodeInfo, allocated *device.PodDevices) (bool, map[string]device.ContainerDevices, string) {
	if s.fit != nil {
		return s.fit(devices, request)
	}
	reason := map[string]int{}
	picked := device.ContainerDevices{}
	remaining := request.Nums
	for _, d := range devices {
		if remaining == 0 {
			break
		}
		if !d.Health {
			reason[common.CardNotHealth]++
			continue
		}
		if d.Totalmem-d.Usedmem < request.Memreq {
			reason[common.CardInsufficientMemory]++
			continue
		}
		if d.Totalcore-d.Usedcores < request.Coresreq {
			reason[common.CardInsufficientCore]++
			continue
		}
		picked = append(picked, device.ContainerDevice{
			UUID:      d.ID,
			Type:      request.Type,
			Usedmem:   request.Memreq,
			Usedcores: request.Coresreq,
		})
		remaining--
	}
	if remaining > 0 {
		return false, map[string]device.ContainerDevices{}, common.GenReason(reason, len(devices))
	}
	return true, map[string]device.ContainerDevices{request.Type: picked}, ""
}

// neutralStubDevices declares the policy-neutral scoring contract.
type neutralStubDevices struct {
	*stubDevices
}

func (n *neutralStubDevices) PolicyNeutralScore() {}

func stubFixture() Fixture {
	return Fixture{
		Devices: []*device.DeviceUsage{
			{ID: "stub-0", Index: 0, Count: 10, Totalmem: 1024, Totalcore: 100, Type: stubType, Health: true},
			{ID: "stub-1", Index: 1, Count: 10, Totalmem: 1024, Totalcore: 100, Type: stubType, Health: true},
		},
		Request: device.ContainerDeviceRequest{Nums: 1, Type: stubType, Memreq: 256, Coresreq: 10},
		Pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "stub-pod", Namespace: "default"},
		},
	}
}

func TestRunAcceptsConformantBackend(t *testing.T) {
	Run(t, &stubDevices{}, stubFixture())
}

// TestChecksRejectViolations is the reason this package earns its place: each
// case breaks one part of the contract and asserts the matching check notices.
// Without it the suite could pass everything and prove nothing.
func TestChecksRejectViolations(t *testing.T) {
	tests := []struct {
		name    string
		dev     device.Devices
		check   func(r reporter, dev device.Devices, f Fixture)
		wantMsg string
	}{
		{
			name: "bare refusal reason is not parseable",
			dev: &stubDevices{
				fit: func(_ []*device.DeviceUsage, _ device.ContainerDeviceRequest) (bool, map[string]device.ContainerDevices, string) {
					return false, nil, "core limit out of range"
				},
			},
			check:   checkFitRefusalReasonIsParseable,
			wantMsg: "common.ParseReason cannot read",
		},
		{
			name: "Fit allocating from an unhealthy list",
			dev: &stubDevices{
				fit: func(devices []*device.DeviceUsage, request device.ContainerDeviceRequest) (bool, map[string]device.ContainerDevices, string) {
					return true, map[string]device.ContainerDevices{
						request.Type: {{UUID: devices[0].ID, Type: request.Type}},
					}, ""
				},
			},
			check:   checkFitRefusalReasonIsParseable,
			wantMsg: "every device is unhealthy",
		},
		{
			name: "allocation exceeds device memory",
			dev: &stubDevices{
				fit: func(devices []*device.DeviceUsage, request device.ContainerDeviceRequest) (bool, map[string]device.ContainerDevices, string) {
					return true, map[string]device.ContainerDevices{
						request.Type: {{
							UUID:    devices[0].ID,
							Type:    request.Type,
							Usedmem: devices[0].Totalmem + 1,
						}},
					}, ""
				},
			},
			check:   checkFitAllocationStaysWithinDeviceBudget,
			wantMsg: "exceeds Totalmem",
		},
		{
			name: "allocation exceeds device cores",
			dev: &stubDevices{
				fit: func(devices []*device.DeviceUsage, request device.ContainerDeviceRequest) (bool, map[string]device.ContainerDevices, string) {
					return true, map[string]device.ContainerDevices{
						request.Type: {{
							UUID:      devices[0].ID,
							Type:      request.Type,
							Usedcores: devices[0].Totalcore + 1,
						}},
					}, ""
				},
			},
			check:   checkFitAllocationStaysWithinDeviceBudget,
			wantMsg: "exceeds Totalcore",
		},
		{
			name: "Fit returns a device it was not given",
			dev: &stubDevices{
				fit: func(_ []*device.DeviceUsage, request device.ContainerDeviceRequest) (bool, map[string]device.ContainerDevices, string) {
					return true, map[string]device.ContainerDevices{
						request.Type: {{UUID: "not-on-this-node", Type: request.Type}},
					}, ""
				},
			},
			check:   checkFitAllocationStaysWithinDeviceBudget,
			wantMsg: "was not in the list it was given",
		},
		{
			name: "feasibility depends on device order",
			dev: &stubDevices{
				fit: func(devices []*device.DeviceUsage, request device.ContainerDeviceRequest) (bool, map[string]device.ContainerDevices, string) {
					if devices[0].ID != "stub-0" {
						return false, nil, common.GenReason(map[string]int{common.CardTypeMismatch: 1}, len(devices))
					}
					return true, map[string]device.ContainerDevices{
						request.Type: {{UUID: devices[0].ID, Type: request.Type}},
					}, ""
				},
			},
			check:   checkFitFeasibilityIsIndependentOfDeviceOrder,
			wantMsg: "whether the request fits must not",
		},
		{
			name: "scores without declaring policy neutrality",
			dev: &stubDevices{
				scoreNode: func(_ string) float32 { return 5 },
			},
			check:   checkScoreNodeDeclaresPolicyNeutrality,
			wantMsg: "does not implement PolicyNeutralScore()",
		},
		{
			name: "empty container yields a device request",
			dev: &stubDevices{
				generate: func(_ *corev1.Container) device.ContainerDeviceRequest {
					return device.ContainerDeviceRequest{Nums: 1, Type: stubType}
				},
			},
			check:   checkEmptyContainerRequestsNoDevices,
			wantMsg: "requests no resources",
		},
		{
			name: "GetNodeDevices reports devices for an unregistered node",
			dev: &stubDevices{
				getNodeDevices: func(_ corev1.Node) ([]*device.DeviceInfo, error) {
					return []*device.DeviceInfo{{ID: "ghost"}}, nil
				},
			},
			check:   checkGetNodeDevicesRejectsMissingAnnotation,
			wantMsg: "capacity that does not exist",
		},
		{
			name: "GetNodeDevices panics on an unregistered node",
			dev: &stubDevices{
				getNodeDevices: func(_ corev1.Node) ([]*device.DeviceInfo, error) {
					panic("nil map read")
				},
			},
			check:   checkGetNodeDevicesRejectsMissingAnnotation,
			wantMsg: "panicked",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := &recordingReporter{}
			test.check(r, test.dev, stubFixture())
			if !r.failed() {
				t.Fatalf("check accepted a backend that violates the contract")
			}
			if !strings.Contains(r.joined(), test.wantMsg) {
				t.Errorf("check reported %q, which does not mention %q", r.joined(), test.wantMsg)
			}
		})
	}
}

func TestScoreNodeDeclaresPolicyNeutrality(t *testing.T) {
	tests := []struct {
		name       string
		dev        device.Devices
		wantFailed bool
	}{
		{
			name:       "a backend that does not score is neutral by construction",
			dev:        &stubDevices{},
			wantFailed: false,
		},
		{
			name: "a scoring backend that declares neutrality is accepted",
			dev: &neutralStubDevices{
				stubDevices: &stubDevices{scoreNode: func(_ string) float32 { return 5 }},
			},
			wantFailed: false,
		},
		{
			name: "a scoring backend that does not declare neutrality is rejected",
			dev: &stubDevices{
				scoreNode: func(_ string) float32 { return 5 },
			},
			wantFailed: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := &recordingReporter{}
			checkScoreNodeDeclaresPolicyNeutrality(r, test.dev, stubFixture())
			if r.failed() != test.wantFailed {
				t.Errorf("failed = %t, want %t (%s)", r.failed(), test.wantFailed, r.joined())
			}
		})
	}
}

func TestFixtureValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(f *Fixture)
		wantErr string
	}{
		{
			name:    "a valid fixture is accepted",
			mutate:  func(_ *Fixture) {},
			wantErr: "",
		},
		{
			name:    "a single device cannot expose order dependence",
			mutate:  func(f *Fixture) { f.Devices = f.Devices[:1] },
			wantErr: "at least two devices",
		},
		{
			name:    "a nil device is rejected",
			mutate:  func(f *Fixture) { f.Devices[0] = nil },
			wantErr: "is nil",
		},
		{
			name:    "an unhealthy device is rejected",
			mutate:  func(f *Fixture) { f.Devices[0].Health = false },
			wantErr: "must be healthy",
		},
		{
			name:    "a request for no devices is rejected",
			mutate:  func(f *Fixture) { f.Request.Nums = 0 },
			wantErr: "at least one device",
		},
		{
			name:    "a nil pod is rejected",
			mutate:  func(f *Fixture) { f.Pod = nil },
			wantErr: "pod must not be nil",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := stubFixture()
			test.mutate(&f)
			err := f.validate()
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validate() = nil, want an error mentioning %q", test.wantErr)
			}
			if !strings.Contains(err.Error(), test.wantErr) {
				t.Errorf("validate() = %q, which does not mention %q", err, test.wantErr)
			}
		})
	}
}

func TestSafeGetNodeDevicesCapturesPanic(t *testing.T) {
	dev := &stubDevices{
		getNodeDevices: func(_ corev1.Node) ([]*device.DeviceInfo, error) {
			panic("boom")
		},
	}
	result := safeGetNodeDevices(dev, corev1.Node{})
	if result.panicValue == nil {
		t.Fatalf("safeGetNodeDevices did not capture the panic")
	}
}
