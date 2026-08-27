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

// quotacheck verifies that every device backend's Fit() implementation
// re-checks ResourceQuota before admitting a pod. device.Devices.Fit() is
// implemented separately by every vendor backend under pkg/device/<vendor>/,
// and the admission-vs-schedule TOCTOU race is only closed if Fit() re-reads
// namespace ResourceQuota usage via device.QuotaManager.FitQuota (directly,
// or through a local wrapper such as the fitQuota() helper used by nvidia,
// cambricon, and amd).
//
// Usage: go run ./hack/tools/quotacheck/ [-allow vendor1,vendor2,...] [path ...]
//
// With no path arguments, it discovers every pkg/device/<vendor>/device.go
// file (skipping the shared pkg/device/common package) and, for each one,
// checks that the package-level Fit() method declared there reaches a call
// to FitQuota, following calls into other functions defined in the same
// package. Path arguments, if given, are pkg/device/<vendor>/device.go
// paths to check instead of the default set.
//
// -allow lists vendor directory names that are still permitted to fail the
// check, for enabling this gate before every backend has been fixed (see
// #2829). A vendor that fails and isn't listed, or that passes while still
// listed (a stale entry left behind after its fix landed), fails the run.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// fitMethodName is the device.Devices interface method every backend must
// implement (pkg/device/devices.go).
const fitMethodName = "Fit"

// targetMethod is the shared re-check helper backends must reach from Fit().
// It is always called as a method, e.g. device.GetLocalCache().FitQuota(...)
// or, through a local wrapper, plain FitQuota(...).
const targetMethod = "FitQuota"

// skipDirs are pkg/device subdirectories that do not implement a vendor
// backend and so have no Fit() method to check.
var skipDirs = map[string]bool{
	"common": true,
}

// allowFlag lists vendor directory names (pkg/device/<vendor>) that are
// currently permitted to fail the check. It exists so this CI gate can be
// enabled before every backend has been fixed: each backend's fix PR (see
// #2829) removes its own entry. A vendor left in the list after its Fit()
// is fixed is reported as a stale entry, so the allowlist can't silently
// mask a regression once a backend is compliant.
var allowFlag = flag.String("allow", "", "comma-separated list of vendor directory names allowed to fail the check")

func main() {
	flag.Parse()

	root, err := findRepoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "quotacheck: %v\n", err)
		os.Exit(1)
	}

	paths := flag.Args()
	if len(paths) == 0 {
		paths, err = defaultDeviceFiles(root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "quotacheck: %v\n", err)
			os.Exit(1)
		}
	}

	allowed := parseAllowList(*allowFlag)

	exitCode, err := run(paths, allowed, os.Stdout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "quotacheck: %v\n", err)
		os.Exit(1)
	}
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

// run checks each of paths and reports one line per path to out: silence
// for a compliant, unlisted vendor; an allowed-failure or stale-entry note
// for a listed vendor; or the check's violation message for a non-compliant,
// unlisted vendor. It returns a non-zero exit code if any path still needs
// attention.
func run(paths []string, allowed map[string]bool, out io.Writer) (int, error) {
	exitCode := 0
	for _, path := range paths {
		violations, err := checkDeviceFile(path)
		if err != nil {
			return 0, err
		}

		vendor := filepath.Base(filepath.Dir(path))
		switch {
		case len(violations) > 0 && allowed[vendor]:
			fmt.Fprintf(out, "%s: allowed failure (see hack/verify-quota.sh); remove from -allow once fixed\n", path)
		case len(violations) > 0:
			for _, v := range violations {
				fmt.Fprintln(out, v)
			}
			exitCode = 1
		case allowed[vendor]:
			fmt.Fprintf(out, "%s: passes the check but is still listed in -allow; remove its stale entry\n", path)
			exitCode = 1
		}
	}
	return exitCode, nil
}

// parseAllowList splits a comma-separated -allow value into a lookup set,
// ignoring blank entries.
func parseAllowList(s string) map[string]bool {
	allowed := make(map[string]bool)
	for v := range strings.SplitSeq(s, ",") {
		v = strings.TrimSpace(v)
		if v != "" {
			allowed[v] = true
		}
	}
	return allowed
}

// defaultDeviceFiles returns every pkg/device/<vendor>/device.go path under
// root, skipping vendor directories with no device.go and non-backend
// directories listed in skipDirs.
func defaultDeviceFiles(root string) ([]string, error) {
	deviceDir := filepath.Join(root, "pkg", "device")
	entries, err := os.ReadDir(deviceDir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", deviceDir, err)
	}

	var paths []string
	for _, e := range entries {
		if !e.IsDir() || skipDirs[e.Name()] {
			continue
		}
		p := filepath.Join(deviceDir, e.Name(), "device.go")
		if _, err := os.Stat(p); err != nil {
			continue
		}
		paths = append(paths, p)
	}
	return paths, nil
}

// checkDeviceFile checks the Fit() method declared in path, returning one
// violation message if it cannot reach a call to FitQuota through the call
// graph of its package.
func checkDeviceFile(path string) ([]string, error) {
	fset := token.NewFileSet()
	deviceFile, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	fitDecl := findFitMethod(deviceFile)
	if fitDecl == nil {
		return []string{fmt.Sprintf("%s: no %s() method found", path, fitMethodName)}, nil
	}

	pkgFiles, err := parsePackage(fset, filepath.Dir(path))
	if err != nil {
		return nil, err
	}

	callGraph := buildCallGraph(pkgFiles)
	if reachesTargetMethod(fitDecl, callGraph, map[*ast.FuncDecl]bool{}) {
		return nil, nil
	}

	pos := fset.Position(fitDecl.Pos())
	return []string{fmt.Sprintf(
		"%s:%d: %s() does not call %s() to re-check ResourceQuota before admitting the pod",
		path, pos.Line, fitMethodName, targetMethod)}, nil
}

// findFitMethod returns the file's method declaration named fitMethodName,
// or nil if none exists.
func findFitMethod(f *ast.File) *ast.FuncDecl {
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil {
			continue
		}
		if fn.Name.Name == fitMethodName {
			return fn
		}
	}
	return nil
}

// parsePackage parses every non-test .go file in dir.
func parsePackage(fset *token.FileSet, dir string) ([]*ast.File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", dir, err)
	}

	var files []*ast.File
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", name, err)
		}
		files = append(files, f)
	}
	return files, nil
}

// buildCallGraph maps each function/method name declared in the package to
// the FuncDecls of the local functions/methods it calls directly. Only
// same-package callees can be resolved this way, which is sufficient for
// following a local wrapper like fitQuota() back to FitQuota().
func buildCallGraph(files []*ast.File) map[string][]*ast.FuncDecl {
	byName := make(map[string]*ast.FuncDecl)
	for _, f := range files {
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok {
				byName[fn.Name.Name] = fn
			}
		}
	}

	graph := make(map[string][]*ast.FuncDecl)
	for name, fn := range byName {
		ast.Inspect(fn, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			calleeName, ok := callName(call)
			if !ok {
				return true
			}
			if callee, ok := byName[calleeName]; ok {
				graph[name] = append(graph[name], callee)
			}
			return true
		})
	}
	return graph
}

// callName returns the identifier or method name a call expression invokes,
// e.g. "fitQuota" for fitQuota(...) and "FitQuota" for x.FitQuota(...).
func callName(call *ast.CallExpr) (string, bool) {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name, true
	case *ast.SelectorExpr:
		return fun.Sel.Name, true
	default:
		return "", false
	}
}

// reachesTargetMethod reports whether fn's body, or any local function it
// calls (transitively), contains a call to targetMethod.
func reachesTargetMethod(fn *ast.FuncDecl, graph map[string][]*ast.FuncDecl, visited map[*ast.FuncDecl]bool) bool {
	if visited[fn] {
		return false
	}
	visited[fn] = true

	found := false
	ast.Inspect(fn, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if name, ok := callName(call); ok && name == targetMethod {
			found = true
			return false
		}
		return true
	})
	if found {
		return true
	}

	for _, callee := range graph[fn.Name.Name] {
		if reachesTargetMethod(callee, graph, visited) {
			return true
		}
	}
	return false
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found")
		}
		dir = parent
	}
}
