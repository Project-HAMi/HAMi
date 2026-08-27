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

const fitPreamble = `package fixture

type Devices struct{}

func (d *Devices) Fit() bool {
`

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

func TestCheckDeviceFile_DirectCall(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "device.go", fitPreamble+`
	return device.GetLocalCache().FitQuota("ns", 0, 1, 0, "dev")
}
`)

	violations, err := checkDeviceFile(path)
	if err != nil {
		t.Fatalf("checkDeviceFile: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("expected no violations for a direct FitQuota call, got %v", violations)
	}
}

func TestCheckDeviceFile_LocalWrapper(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "device.go", fitPreamble+`
	return fitQuota("ns", 0, 0)
}

func fitQuota(ns string, memreq, coresreq int64) bool {
	return device.GetLocalCache().FitQuota(ns, memreq, 1, coresreq, "dev")
}
`)

	violations, err := checkDeviceFile(path)
	if err != nil {
		t.Fatalf("checkDeviceFile: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("expected no violations when Fit() reaches FitQuota via a local wrapper, got %v", violations)
	}
}

func TestCheckDeviceFile_WrapperInOtherFile(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "device.go", fitPreamble+`
	return fitQuota("ns", 0, 0)
}
`)
	writeFile(t, dir, "quota.go", `package fixture

func fitQuota(ns string, memreq, coresreq int64) bool {
	return device.GetLocalCache().FitQuota(ns, memreq, 1, coresreq, "dev")
}
`)

	violations, err := checkDeviceFile(path)
	if err != nil {
		t.Fatalf("checkDeviceFile: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("expected no violations when the wrapper lives in another file of the same package, got %v", violations)
	}
}

func TestCheckDeviceFile_MissingCheck(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "device.go", fitPreamble+`
	return true
}
`)

	violations, err := checkDeviceFile(path)
	if err != nil {
		t.Fatalf("checkDeviceFile: %v", err)
	}
	if len(violations) != 1 {
		t.Fatalf("expected exactly one violation, got %v", violations)
	}
}

func TestCheckDeviceFile_UnrelatedCallsDoNotCount(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "device.go", fitPreamble+`
	return otherHelper()
}

func otherHelper() bool {
	return true
}
`)

	violations, err := checkDeviceFile(path)
	if err != nil {
		t.Fatalf("checkDeviceFile: %v", err)
	}
	if len(violations) != 1 {
		t.Fatalf("expected exactly one violation, got %v", violations)
	}
}

func TestCheckDeviceFile_UninvokedClosureDoesNotCount(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "device.go", fitPreamble+`
	check := func() bool {
		return device.GetLocalCache().FitQuota("ns", 0, 1, 0, "dev")
	}
	_ = check
	return true
}
`)

	violations, err := checkDeviceFile(path)
	if err != nil {
		t.Fatalf("checkDeviceFile: %v", err)
	}
	if len(violations) != 1 {
		t.Fatalf("expected a violation: an uninvoked closure calling FitQuota must not satisfy the check, got %v", violations)
	}
}

func TestCheckDeviceFile_InvokedClosureCounts(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "device.go", fitPreamble+`
	return func() bool {
		return device.GetLocalCache().FitQuota("ns", 0, 1, 0, "dev")
	}()
}
`)

	violations, err := checkDeviceFile(path)
	if err != nil {
		t.Fatalf("checkDeviceFile: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("expected no violations when Fit() invokes a closure that calls FitQuota, got %v", violations)
	}
}

func TestCheckDeviceFile_AsyncCallDoesNotCount(t *testing.T) {
	cases := map[string]string{
		"go statement calling FitQuota directly": fitPreamble + `
	go device.GetLocalCache().FitQuota("ns", 0, 1, 0, "dev")
	return true
}
`,
		"go statement launching a closure that calls FitQuota": fitPreamble + `
	go func() {
		device.GetLocalCache().FitQuota("ns", 0, 1, 0, "dev")
	}()
	return true
}
`,
	}

	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeFile(t, dir, "device.go", content)

			violations, err := checkDeviceFile(path)
			if err != nil {
				t.Fatalf("checkDeviceFile: %v", err)
			}
			if len(violations) != 1 {
				t.Fatalf("expected a violation: an asynchronous FitQuota call runs after Fit() returns and must not satisfy the check, got %v", violations)
			}
		})
	}
}

func TestCheckDeviceFile_AsyncCallArgsStillInspected(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "device.go", fitPreamble+`
	go log("checked", fitQuota("ns", 0, 0))
	return true
}

func fitQuota(ns string, memreq, coresreq int64) bool {
	return device.GetLocalCache().FitQuota(ns, memreq, 1, coresreq, "dev")
}

func log(msg string, ok bool) {}
`)

	violations, err := checkDeviceFile(path)
	if err != nil {
		t.Fatalf("checkDeviceFile: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("expected no violations: a `go` statement's arguments are evaluated synchronously, so fitQuota() in an argument counts, got %v", violations)
	}
}

func TestCheckDeviceFile_NoFitMethod(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "device.go", `package fixture

func NotFit() bool {
	return true
}
`)

	violations, err := checkDeviceFile(path)
	if err != nil {
		t.Fatalf("checkDeviceFile: %v", err)
	}
	if len(violations) != 1 {
		t.Fatalf("expected exactly one violation for a missing Fit() method, got %v", violations)
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
// this repository. nvidia, cambricon, and amd already re-check ResourceQuota
// in Fit() (see #2536 and the amd fix that closes #2829's amd leg) and must
// pass; the remaining backends are tracked by #2829 as still missing the
// check and must be flagged, mirroring this issue's acceptance criteria.
func TestRealBackends(t *testing.T) {
	root, err := findRepoRoot()
	if err != nil {
		t.Fatalf("findRepoRoot: %v", err)
	}

	compliant := map[string]bool{
		"amd":       true,
		"cambricon": true,
		"nvidia":    true,
	}
	nonCompliant := map[string]bool{
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

	for vendor := range compliant {
		path := filepath.Join(root, "pkg", "device", vendor, "device.go")
		violations, err := checkDeviceFile(path)
		if err != nil {
			t.Fatalf("checkDeviceFile(%s): %v", vendor, err)
		}
		if len(violations) != 0 {
			t.Errorf("%s: expected no violations, got %v", vendor, violations)
		}
	}

	for vendor := range nonCompliant {
		path := filepath.Join(root, "pkg", "device", vendor, "device.go")
		violations, err := checkDeviceFile(path)
		if err != nil {
			t.Fatalf("checkDeviceFile(%s): %v", vendor, err)
		}
		if len(violations) == 0 {
			t.Errorf("%s: expected a violation for a missing ResourceQuota re-check, got none", vendor)
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
	body := "\treturn true\n}\n"
	if compliant {
		body = "\treturn device.GetLocalCache().FitQuota(\"ns\", 0, 1, 0, \"dev\")\n}\n"
	}
	return writeFile(t, vendorDir, "device.go", fitPreamble+body)
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
