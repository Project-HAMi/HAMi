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
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
)

// a100MigProfilesWith4g is the A100-40GB allowlist with 4g.20gb added: the same
// memory as 3g.20gb, more compute, and a single legal placement at slot 0.
func a100MigProfilesWith4g() []device.MigProfile {
	return append(a100MigProfiles(), device.MigProfile{
		Name: "4g.20gb", MemoryMB: 20480, Core: 52, SliceCount: 4, InstanceCount: 1,
		Placements: []device.MigPlacement{{Start: 0, Size: 4}},
	})
}

func migPod(preference string) *corev1.Pod {
	annos := map[string]string{AllocateMode: MigMode}
	if preference != "" {
		annos[MigProfilePreference] = preference
	}
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "mig-pod", Annotations: annos}}
}

func migProfileNames(profiles []device.MigProfile) []string {
	names := make([]string, len(profiles))
	for i, profile := range profiles {
		names[i] = profile.Name
	}
	return names
}

func TestParseMigProfilePreference(t *testing.T) {
	cases := []struct {
		raw  string
		want []string
	}{
		{raw: "4g.20gb", want: []string{"4g.20gb"}},
		{raw: " 4g , 3g.20gb ,, 4g ", want: []string{"4g", "3g.20gb"}},
		{raw: "", want: nil},
		{raw: " , ", want: nil},
	}
	for _, tc := range cases {
		if got := parseMigProfilePreference(tc.raw); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("parseMigProfilePreference(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

func TestMigProfileSliceKey(t *testing.T) {
	for profile, want := range map[string]string{"4g.20gb": "4g", "1g.10gb+me": "1g", "4g": "4g", ".5gb": ".5gb"} {
		if got := migProfileSliceKey(profile); got != want {
			t.Errorf("migProfileSliceKey(%q) = %q, want %q", profile, got, want)
		}
	}
}

func TestMigProfilePreferenceReadsTheAnnotation(t *testing.T) {
	if got := migProfilePreference(nil); got != nil {
		t.Fatalf("no annotations should prefer nothing, got %v", got)
	}
	if got := migProfilePreference(map[string]string{AllocateMode: MigMode}); got != nil {
		t.Fatalf("missing annotation should prefer nothing, got %v", got)
	}
	if got := podMigProfilePreference(nil); got != nil {
		t.Fatalf("nil pod should prefer nothing, got %v", got)
	}
	if got := podMigProfilePreference(migPod("4g.20gb,3g")); !reflect.DeepEqual(got, []string{"4g.20gb", "3g"}) {
		t.Fatalf("pod preference = %v, want [4g.20gb 3g]", got)
	}
}

func TestMigProfileCandidatesPutsPreferredFirst(t *testing.T) {
	profiles := a100MigProfilesWith4g()
	cases := []struct {
		name      string
		preferred []string
		want      []string
	}{
		{name: "default is smallest first", preferred: nil, want: []string{"1g.5gb", "2g.10gb", "3g.20gb", "4g.20gb"}},
		{name: "slice class", preferred: []string{"4g"}, want: []string{"4g.20gb", "1g.5gb", "2g.10gb", "3g.20gb"}},
		{name: "exact names keep the listed order", preferred: []string{"3g.20gb", "1g.5gb"}, want: []string{"3g.20gb", "1g.5gb", "2g.10gb", "4g.20gb"}},
		{name: "entries outside the card's allowlist are ignored", preferred: []string{"9g.99gb", "4g.40gb"}, want: []string{"1g.5gb", "2g.10gb", "3g.20gb", "4g.20gb"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := migProfileNames(migProfileCandidates(profiles, tc.preferred)); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("candidates = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSelectMigCandidateHonorsPreference(t *testing.T) {
	profiles := a100MigProfilesWith4g()
	profile, _, ok := selectMigCandidate(profiles, nil, 20000, nil)
	if !ok || profile.Name != "3g.20gb" {
		t.Fatalf("default 20GB profile = %s, want 3g.20gb", profile.Name)
	}
	profile, placement, ok := selectMigCandidate(profiles, nil, 20000, []string{"4g"})
	if !ok || profile.Name != "4g.20gb" || placement != (device.MigPlacement{Start: 0, Size: 4}) {
		t.Fatalf("preferred 20GB profile = %s at %+v, want 4g.20gb at slot 0", profile.Name, placement)
	}
	// A preference cannot shrink the request below its memory.
	profile, _, ok = selectMigCandidate(profiles, nil, 20000, []string{"1g", "2g"})
	if !ok || profile.Name != "3g.20gb" {
		t.Fatalf("undersized preference resolved to %s, want 3g.20gb", profile.Name)
	}
	// The single 4g placement is blocked by a running 2g, so the default profile wins.
	occupied := []device.MigPlacement{{Start: 0, Size: 2}}
	profile, placement, ok = selectMigCandidate(profiles, occupied, 20000, []string{"4g"})
	if !ok || profile.Name != "3g.20gb" || placement != (device.MigPlacement{Start: 4, Size: 4}) {
		t.Fatalf("blocked preference resolved to %s at %+v, want 3g.20gb at slot 4", profile.Name, placement)
	}
}

func TestPlanMigContainerNeverRejectsForAPreference(t *testing.T) {
	profiles := a100MigProfilesWith4g()
	// 20GB + 10GB + 10GB fill an empty A100 as 3g + 2g + 2g, but a 4g at slot 0
	// leaves room for only one 2g. The preference must yield, not reject the card.
	memories := []int32{20000, 10000, 10000}
	chosen, placements, ok := planMigContainer(profiles, nil, memories, []string{"4g"})
	if !ok {
		t.Fatal("a preference that does not fit should fall back to the default layout, not reject the card")
	}
	if got := migProfileNames(chosen); !reflect.DeepEqual(got, []string{"3g.20gb", "2g.10gb", "2g.10gb"}) {
		t.Fatalf("fallback profiles = %v, want [3g.20gb 2g.10gb 2g.10gb]", got)
	}
	if len(placements) != 3 {
		t.Fatalf("placements = %+v, want one per request", placements)
	}
	if _, _, ok := planMigContainer(profiles, nil, []int32{20000, 20000, 20000}, []string{"4g"}); ok {
		t.Fatal("three 20GB slices cannot fit an A100 in any order")
	}
}

func TestPlanMigContainerFallsBackSequentiallyWhenJointFails(t *testing.T) {
	profiles := a100MigProfilesWith4g()
	// Two 20GB slices cannot both be 4g. Placed sequentially the first keeps the
	// preference and the second takes the remaining 3g span.
	chosen, placements, ok := planMigContainer(profiles, nil, []int32{20000, 20000}, []string{"4g"})
	if !ok {
		t.Fatal("4g + 3g should fit an empty A100")
	}
	if got := migProfileNames(chosen); !reflect.DeepEqual(got, []string{"4g.20gb", "3g.20gb"}) {
		t.Fatalf("profiles = %v, want [4g.20gb 3g.20gb]", got)
	}
	want := []device.MigPlacement{{Start: 0, Size: 4}, {Start: 4, Size: 4}}
	if !reflect.DeepEqual(placements, want) {
		t.Fatalf("placements = %+v, want %+v", placements, want)
	}
}

func TestCustomFilterRuleAndRecordMigPlansHonorPreference(t *testing.T) {
	dev := &NvidiaGPUDevices{}
	usage := &device.DeviceUsage{ID: "GPU-a", Mode: MigMode, MigProfiles: a100MigProfilesWith4g()}
	preferred := []string{"4g"}
	if !dev.CustomFilterRule(nil, device.ContainerDeviceRequest{Memreq: 20000}, nil, usage, preferred) {
		t.Fatal("a 20GB request should fit an empty A100")
	}
	tentative := device.ContainerDevices{{UUID: "GPU-a", Usedmem: 20000}}
	recordMigPlans([]*device.DeviceUsage{usage}, tentative, preferred)
	if tentative[0].CustomInfo[MigProfileCustomInfo] != "4g.20gb" || tentative[0].CustomInfo[MigPlacementCustomInfo] != (device.MigPlacement{Start: 0, Size: 4}) {
		t.Fatalf("recorded plan = %+v, want 4g.20gb at slot 0", tentative[0].CustomInfo)
	}
	if err := dev.AddResourceUsage(migPod("4g"), usage, &tentative[0]); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if tentative[0].Usedmem != 20480 || tentative[0].Usedcores != 52 {
		t.Fatalf("committed resources = (%d,%d), want the reported 4g metadata", tentative[0].Usedmem, tentative[0].Usedcores)
	}
	if len(usage.MigAllocationsInUse) != 1 || usage.MigAllocationsInUse[0].Profile != "4g.20gb" {
		t.Fatalf("usage after commit = %+v, want one 4g.20gb allocation", usage.MigAllocationsInUse)
	}
}

func TestAddResourceUsageFallsBackToPreferredProfile(t *testing.T) {
	dev := &NvidiaGPUDevices{}
	usage := &device.DeviceUsage{ID: "GPU-a", Mode: MigMode, MigProfiles: a100MigProfilesWith4g()}
	// No recorded plan: the pod annotation drives the fallback selection.
	ctr := &device.ContainerDevice{UUID: "GPU-a", Usedmem: 20000}
	if err := dev.AddResourceUsage(migPod("4g.20gb"), usage, ctr); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if ctr.CustomInfo[MigProfileCustomInfo] != "4g.20gb" || ctr.Usedcores != 52 {
		t.Fatalf("fallback commit = %+v cores=%d, want 4g.20gb with 52 cores", ctr.CustomInfo, ctr.Usedcores)
	}
	// A nil pod keeps the default profile.
	fresh := &device.DeviceUsage{ID: "GPU-a", Mode: MigMode, MigProfiles: a100MigProfilesWith4g()}
	ctr = &device.ContainerDevice{UUID: "GPU-a", Usedmem: 20000}
	if err := dev.AddResourceUsage(nil, fresh, ctr); err != nil {
		t.Fatalf("commit without pod: %v", err)
	}
	if ctr.CustomInfo[MigProfileCustomInfo] != "3g.20gb" {
		t.Fatalf("default commit = %+v, want 3g.20gb", ctr.CustomInfo)
	}
}

func TestFitHonorsMigProfilePreference(t *testing.T) {
	dev := InitNvidiaDevice(NvidiaConfig{})
	newUsage := func() *device.DeviceUsage {
		return &device.DeviceUsage{
			ID: "GPU-a", Type: NvidiaGPUDevice, Mode: MigMode, Health: true,
			Count: 7, Totalmem: 40960, Totalcore: 100, MigProfiles: a100MigProfilesWith4g(),
		}
	}
	request := device.ContainerDeviceRequest{Nums: 1, Type: NvidiaGPUDevice, Memreq: 20000}
	cases := []struct {
		name string
		pod  *corev1.Pod
		want string
	}{
		{name: "no preference keeps the smallest profile", pod: migPod(""), want: "3g.20gb"},
		{name: "slice class preference", pod: migPod("4g"), want: "4g.20gb"},
		{name: "exact profile preference", pod: migPod("4g.20gb"), want: "4g.20gb"},
		{name: "preference for another model's profile is ignored on this card", pod: migPod("4g.40gb"), want: "3g.20gb"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fit, devs, reason := dev.Fit([]*device.DeviceUsage{newUsage()}, request, tc.pod, &device.NodeInfo{}, &device.PodDevices{})
			if !fit {
				t.Fatalf("20GB request should fit an empty A100: %s", reason)
			}
			got := devs[NvidiaGPUDevice]
			if len(got) != 1 || got[0].CustomInfo[MigProfileCustomInfo] != tc.want {
				t.Fatalf("fit allocated %+v, want %s", got, tc.want)
			}
		})
	}
}

func TestValidateMigProfilePreference(t *testing.T) {
	dev := &NvidiaGPUDevices{config: NvidiaConfig{MigProfileAllowlist: []device.AllowedMigProfiles{
		{Models: []string{"A100-SXM4-40GB"}, Profiles: []string{"1g.5gb", "2g.10gb", "3g.20gb", "4g.20gb", "7g.40gb"}},
		{Models: []string{"H100-SXM5-80GB"}, Profiles: []string{"1g.10gb", "3g.40gb", "4g.40gb", "7g.80gb"}},
	}}}
	cases := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "exact name", value: "4g.20gb"},
		{name: "slice class allowed on some model", value: "4g"},
		{name: "ordered list mixing models", value: " 4g.40gb , 3g "},
		{name: "profile allowed on no model", value: "5g.30gb", wantErr: true},
		{name: "slice class allowed on no model", value: "6g", wantErr: true},
		{name: "empty list", value: " , ", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := dev.validateMigProfilePreference(migPod(tc.value))
			if (err != nil) != tc.wantErr {
				t.Fatalf("validate %q: err=%v, wantErr=%v", tc.value, err, tc.wantErr)
			}
		})
	}
	if err := dev.validateMigProfilePreference(nil); err != nil {
		t.Fatalf("nil pod: %v", err)
	}
	if err := dev.validateMigProfilePreference(migPod("")); err != nil {
		t.Fatalf("pod without the annotation: %v", err)
	}
	if _, err := dev.MutateAdmission(&corev1.Container{}, migPod("5g.30gb")); err == nil {
		t.Fatal("admission should reject a preference that no allowlisted profile satisfies")
	}
	if _, err := dev.MutateAdmission(&corev1.Container{}, migPod("4g")); err != nil {
		t.Fatalf("admission should accept an allowlisted preference: %v", err)
	}
}
