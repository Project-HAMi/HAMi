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

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fitSignature mirrors device.Devices.Fit so fixtures are selected by
// findFitMethod the same way a real backend's method is.
const fitSignature = "func (d *Devices) Fit(devices []*device.DeviceUsage, request device.ContainerDeviceRequest, " +
	"pod *corev1.Pod, nodeInfo *device.NodeInfo, allocated *device.PodDevices) " +
	"(bool, map[string]device.ContainerDevices, string) {\n"

// fitFile returns a parseable fixture file whose Fit() method has the given
// body, followed by any extra package-level declarations.
func fitFile(body string, decls ...string) string {
	return "package fixture\n\ntype Devices struct{}\n\n" + fitSignature + body + "}\n" +
		strings.Join(decls, "\n")
}

// gatedOn wraps a quota expression in the rejection branch real backends use.
func gatedOn(expr string) string {
	return "\tif !" + expr + " {\n\t\treturn false, nil, \"quota\"\n\t}\n\treturn true, nil, \"\"\n"
}

// wrapper is a local fitQuota() helper that reaches the re-check, like the one
// nvidia and cambricon use.
const wrapper = `
func fitQuota(ns string, memreq, coresreq int64) bool {
	return device.GetLocalCache().FitQuota(ns, memreq, 1, coresreq, "dev")
}
`

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

// checkFixture writes content as device.go in a fresh temp dir and checks it.
func checkFixture(t *testing.T, content string) []string {
	t.Helper()
	path := writeFile(t, t.TempDir(), "device.go", content)
	violations, err := checkDeviceFile(path)
	if err != nil {
		t.Fatalf("checkDeviceFile: %v", err)
	}
	return violations
}

// assertViolation fails unless there is exactly one violation containing want.
func assertViolation(t *testing.T, violations []string, want string) {
	t.Helper()
	if len(violations) != 1 {
		t.Fatalf("expected exactly one violation, got %v", violations)
	}
	if !strings.Contains(violations[0], want) {
		t.Errorf("violation = %q, want it to mention %q", violations[0], want)
	}
}

func TestCheckDeviceFile_DirectCall(t *testing.T) {
	got := checkFixture(t, fitFile(gatedOn(`device.GetLocalCache().FitQuota("ns", 0, 1, 0, "dev")`)))
	if len(got) != 0 {
		t.Errorf("expected no violations for a gated direct FitQuota call, got %v", got)
	}
}

func TestCheckDeviceFile_LocalWrapper(t *testing.T) {
	got := checkFixture(t, fitFile(gatedOn(`fitQuota("ns", 0, 0)`), wrapper))
	if len(got) != 0 {
		t.Errorf("expected no violations when Fit() reaches FitQuota via a local wrapper, got %v", got)
	}
}

func TestCheckDeviceFile_WrapperInOtherFile(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "device.go", fitFile(gatedOn(`fitQuota("ns", 0, 0)`)))
	writeFile(t, dir, "quota.go", "package fixture\n"+wrapper)

	violations, err := checkDeviceFile(path)
	if err != nil {
		t.Fatalf("checkDeviceFile: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("expected no violations when the wrapper lives in another file of the same package, got %v", violations)
	}
}

// TestCheckDeviceFile_FitInOtherFile guards against the checker only looking
// at device.go: a backend that moves Fit() into another file of the same
// package is still compliant and must not be reported as missing Fit().
func TestCheckDeviceFile_FitInOtherFile(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "device.go", "package fixture\n\ntype Devices struct{}\n"+wrapper)
	writeFile(t, dir, "fit.go", "package fixture\n\n"+fitSignature+gatedOn(`fitQuota("ns", 0, 0)`)+"}\n")

	violations, err := checkDeviceFile(path)
	if err != nil {
		t.Fatalf("checkDeviceFile: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("expected no violations when Fit() is declared in another file of the package, got %v", violations)
	}
}

func TestCheckDeviceFile_MissingCheck(t *testing.T) {
	got := checkFixture(t, fitFile("\treturn true, nil, \"\"\n"))
	assertViolation(t, got, "does not call")
}

func TestCheckDeviceFile_UnrelatedCallsDoNotCount(t *testing.T) {
	got := checkFixture(t, fitFile(gatedOn("otherHelper()"), "\nfunc otherHelper() bool {\n\treturn true\n}\n"))
	assertViolation(t, got, "does not call")
}

// TestCheckDeviceFile_DiscardedResult is the case a pure reachability check
// misses: FitQuota runs, but nothing acts on what it returns, so the candidate
// device is admitted regardless and the race stays open.
func TestCheckDeviceFile_DiscardedResult(t *testing.T) {
	cases := map[string]string{
		"assigned to the blank identifier": "\t_ = fitQuota(\"ns\", 0, 0)\n\treturn true, nil, \"\"\n",
		"called as a bare statement":       "\tfitQuota(\"ns\", 0, 0)\n\treturn true, nil, \"\"\n",
		"passed only to a logger":          "\tklog.V(3).InfoS(\"quota\", \"fits\", fitQuota(\"ns\", 0, 0))\n\treturn true, nil, \"\"\n",
	}

	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			assertViolation(t, checkFixture(t, fitFile(body, wrapper)), "discards its result")
		})
	}
}

func TestCheckDeviceFile_ResultCheckedViaVariable(t *testing.T) {
	cases := map[string]string{
		"assigned then branched on": "\tok := fitQuota(\"ns\", 0, 0)\n\tif !ok {\n\t\treturn false, nil, \"quota\"\n\t}\n\treturn true, nil, \"\"\n",
		"bound in the if init":      "\tif ok := fitQuota(\"ns\", 0, 0); !ok {\n\t\treturn false, nil, \"quota\"\n\t}\n\treturn true, nil, \"\"\n",
		"returned directly":         "\treturn fitQuota(\"ns\", 0, 0), nil, \"\"\n",
		"declared with var":         "\tvar ok = fitQuota(\"ns\", 0, 0)\n\tif !ok {\n\t\treturn false, nil, \"quota\"\n\t}\n\treturn true, nil, \"\"\n",
	}

	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if got := checkFixture(t, fitFile(body, wrapper)); len(got) != 0 {
				t.Errorf("expected no violations when the result gates admission, got %v", got)
			}
		})
	}
}

func TestCheckDeviceFile_UninvokedClosureDoesNotCount(t *testing.T) {
	body := "\tcheck := func() bool {\n\t\treturn device.GetLocalCache().FitQuota(\"ns\", 0, 1, 0, \"dev\")\n\t}\n\t_ = check\n\treturn true, nil, \"\"\n"
	assertViolation(t, checkFixture(t, fitFile(body)), "does not call")
}

func TestCheckDeviceFile_InvokedClosureCounts(t *testing.T) {
	body := gatedOn("func() bool {\n\t\treturn device.GetLocalCache().FitQuota(\"ns\", 0, 1, 0, \"dev\")\n\t}()")
	if got := checkFixture(t, fitFile(body)); len(got) != 0 {
		t.Errorf("expected no violations when Fit() invokes a closure that calls FitQuota, got %v", got)
	}
}

func TestCheckDeviceFile_AsyncCallDoesNotCount(t *testing.T) {
	cases := map[string]string{
		"go statement calling FitQuota directly": "\tgo device.GetLocalCache().FitQuota(\"ns\", 0, 1, 0, \"dev\")\n\treturn true, nil, \"\"\n",
		"go statement launching a closure":       "\tgo func() {\n\t\tdevice.GetLocalCache().FitQuota(\"ns\", 0, 1, 0, \"dev\")\n\t}()\n\treturn true, nil, \"\"\n",
	}

	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			assertViolation(t, checkFixture(t, fitFile(body)), "does not call")
		})
	}
}

// TestCheckDeviceFile_AsyncCallArgsAreSeen documents the two rules meeting:
// a `go` statement's arguments are evaluated synchronously, so the call is
// found, but handing its result to a logger still doesn't gate admission.
func TestCheckDeviceFile_AsyncCallArgsAreSeen(t *testing.T) {
	body := "\tgo record(\"checked\", fitQuota(\"ns\", 0, 0))\n\treturn true, nil, \"\"\n"
	decls := wrapper + "\nfunc record(msg string, ok bool) {}\n"
	assertViolation(t, checkFixture(t, fitFile(body, decls)), "discards its result")
}

// TestCheckDeviceFile_ShadowingFitIgnored guards findFitMethod's signature
// match: an unrelated method named Fit must not stand in for the interface
// implementation, or a backend could pass the gate on a helper's check while
// its real Fit() admits pods unguarded.
func TestCheckDeviceFile_ShadowingFitIgnored(t *testing.T) {
	shadow := "\ntype cache struct{}\n\nfunc (c *cache) Fit() bool {\n\treturn device.GetLocalCache().FitQuota(\"ns\", 0, 1, 0, \"dev\")\n}\n"
	// The shadowing helper is declared first, so a first-match lookup would
	// pick it and wrongly report the package as compliant.
	content := "package fixture\n\ntype Devices struct{}\n" + shadow + "\n" + fitSignature + "\treturn true, nil, \"\"\n}\n"
	assertViolation(t, checkFixture(t, content), "does not call")
}

func TestCheckDeviceFile_FitSignatureChanged(t *testing.T) {
	content := "package fixture\n\ntype Devices struct{}\n\nfunc (d *Devices) Fit() bool {\n\treturn true\n}\n"
	assertViolation(t, checkFixture(t, content), "update fitParamCount")
}

// TestCheckDeviceFile_SameNamedMethodsOnDifferentReceivers guards call-graph
// resolution: indexing helpers by name alone lets one receiver's method
// overwrite another's, which can drop the real wrapper and report a compliant
// backend as broken.
func TestCheckDeviceFile_SameNamedMethodsOnDifferentReceivers(t *testing.T) {
	decls := `
type other struct{}

func (o *other) check() bool {
	return true
}

func (d *Devices) check() bool {
	return device.GetLocalCache().FitQuota("ns", 0, 1, 0, "dev")
}
`
	if got := checkFixture(t, fitFile(gatedOn("d.check()"), decls)); len(got) != 0 {
		t.Errorf("expected no violations when the real wrapper shares its name with another receiver's method, got %v", got)
	}
}

// TestCheckDeviceFile_RecursiveHelperTerminates guards the visited set against
// a mutually recursive call chain hanging the check.
func TestCheckDeviceFile_RecursiveHelperTerminates(t *testing.T) {
	decls := `
func ping() bool {
	return pong()
}

func pong() bool {
	return ping()
}
`
	assertViolation(t, checkFixture(t, fitFile(gatedOn("ping()"), decls)), "does not call")
}

func TestCheckDeviceFile_NoFitMethod(t *testing.T) {
	content := "package fixture\n\nfunc NotFit() bool {\n\treturn true\n}\n"
	assertViolation(t, checkFixture(t, content), "no Fit() method found")
}

func TestCheckDeviceFile_UnparseableFileErrors(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "device.go", "package fixture\n\nfunc broken( {\n")

	if _, err := checkDeviceFile(path); err == nil {
		t.Fatal("expected an error for an unparseable file, got nil")
	}
}

func TestDefaultDeviceFiles(t *testing.T) {
	root := t.TempDir()
	deviceDir := filepath.Join(root, "pkg", "device")

	mustMkdirAll(t, filepath.Join(deviceDir, "vendora"))
	writeFile(t, filepath.Join(deviceDir, "vendora"), "device.go", "package vendora\n")

	mustMkdirAll(t, filepath.Join(deviceDir, "vendorb"))
	writeFile(t, filepath.Join(deviceDir, "vendorb"), "device.go", "package vendorb\n")

	// common has no device.go and must be skipped explicitly.
	mustMkdirAll(t, filepath.Join(deviceDir, "common"))
	writeFile(t, filepath.Join(deviceDir, "common"), "device.go", "package common\n")

	// vendorc has no device.go and must be skipped by absence.
	mustMkdirAll(t, filepath.Join(deviceDir, "vendorc"))

	paths, err := defaultDeviceFiles(root)
	if err != nil {
		t.Fatalf("defaultDeviceFiles: %v", err)
	}

	want := map[string]bool{
		filepath.Join(deviceDir, "vendora", "device.go"): true,
		filepath.Join(deviceDir, "vendorb", "device.go"): true,
	}
	if len(paths) != len(want) {
		t.Fatalf("defaultDeviceFiles returned %v, want keys of %v", paths, want)
	}
	for _, p := range paths {
		if !want[p] {
			t.Errorf("unexpected path %s", p)
		}
	}
}

func mustMkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
}

// TestRealBackends runs quotacheck against the actual pkg/device backends in
// this repository. nvidia and cambricon already re-check ResourceQuota in
// Fit() (see #2536) and must pass; the backends tracked by #2829 as still
// missing the check must be flagged, mirroring this issue's acceptance
// criteria.
//
// It iterates the backends quotacheck discovers rather than a fixed list, so
// a newly added backend fails here until it is classified, instead of being
// silently unverified while hack/verify-quota.sh fails in CI.
func TestRealBackends(t *testing.T) {
	root, err := findRepoRoot()
	if err != nil {
		t.Fatalf("findRepoRoot: %v", err)
	}

	compliant := map[string]bool{
		"cambricon": true,
		"nvidia":    true,
	}
	// Keep in sync with ALLOWED_VENDORS in hack/verify-quota.sh.
	nonCompliant := map[string]bool{
		"amd":       true,
		"ascend":    true,
		"awsneuron": true,
		"biren":     true,
		"enflame":   true,
		"hygon":     true,
		"iluvatar":  true,
		"kunlun":    true,
		"metax":     true,
		"mthreads":  true,
		"vastai":    true,
	}

	paths, err := defaultDeviceFiles(root)
	if err != nil {
		t.Fatalf("defaultDeviceFiles: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("no device backends discovered under pkg/device")
	}

	seen := map[string]bool{}
	for _, path := range paths {
		vendor := filepath.Base(filepath.Dir(path))
		seen[vendor] = true

		violations, err := checkDeviceFile(path)
		if err != nil {
			t.Fatalf("checkDeviceFile(%s): %v", vendor, err)
		}

		switch {
		case compliant[vendor]:
			if len(violations) != 0 {
				t.Errorf("%s: expected no violations, got %v", vendor, violations)
			}
		case nonCompliant[vendor]:
			if len(violations) == 0 {
				t.Errorf("%s: now re-checks ResourceQuota; move it to the compliant set here and drop it from ALLOWED_VENDORS in hack/verify-quota.sh", vendor)
			}
		default:
			t.Errorf("%s: backend is classified in neither the compliant nor the non-compliant set; add it here, and to ALLOWED_VENDORS in hack/verify-quota.sh if its Fit() does not re-check ResourceQuota yet", vendor)
		}
	}

	for _, set := range []map[string]bool{compliant, nonCompliant} {
		for vendor := range set {
			if !seen[vendor] {
				t.Errorf("%s: listed here but no longer discovered under pkg/device; remove its stale entry", vendor)
			}
		}
	}
}

func TestParseAllowList(t *testing.T) {
	got := parseAllowList(" foo, bar ,,baz")
	want := map[string]bool{"foo": true, "bar": true, "baz": true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseAllowList(...) = %v, want %v", got, want)
	}

	if got := parseAllowList(""); len(got) != 0 {
		t.Errorf("parseAllowList(\"\") = %v, want empty", got)
	}
}

// newRunFixture writes a device.go for a "vendora" backend under a fresh
// temp root and returns its path.
func newRunFixture(t *testing.T, compliant bool) string {
	t.Helper()
	vendorDir := filepath.Join(t.TempDir(), "vendora")
	mustMkdirAll(t, vendorDir)

	body := "\treturn true, nil, \"\"\n"
	if compliant {
		body = gatedOn(`device.GetLocalCache().FitQuota("ns", 0, 1, 0, "dev")`)
	}
	return writeFile(t, vendorDir, "device.go", fitFile(body))
}

func TestRun_UnlistedCompliant(t *testing.T) {
	path := newRunFixture(t, true)

	var buf bytes.Buffer
	code, err := run([]string{path}, nil, &buf)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no output for a compliant, unlisted vendor, got %q", buf.String())
	}
}

func TestRun_UnlistedNonCompliant(t *testing.T) {
	path := newRunFixture(t, false)

	var buf bytes.Buffer
	code, err := run([]string{path}, nil, &buf)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(buf.String(), "does not call") {
		t.Errorf("expected a violation message, got %q", buf.String())
	}
}

func TestRun_ListedNonCompliant_Allowed(t *testing.T) {
	path := newRunFixture(t, false)

	var buf bytes.Buffer
	code, err := run([]string{path}, map[string]bool{"vendora": true}, &buf)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0 for an allowed failure", code)
	}
	if !strings.Contains(buf.String(), "allowed failure") {
		t.Errorf("expected an allowed-failure note, got %q", buf.String())
	}
}

func TestRun_ListedCompliant_StaleEntry(t *testing.T) {
	path := newRunFixture(t, true)

	var buf bytes.Buffer
	code, err := run([]string{path}, map[string]bool{"vendora": true}, &buf)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 1 {
		t.Errorf("exit code = %d, want 1 for a stale allowlist entry", code)
	}
	if !strings.Contains(buf.String(), "stale entry") {
		t.Errorf("expected a stale-entry note, got %q", buf.String())
	}
}
