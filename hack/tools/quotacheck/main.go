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
// or through a local wrapper such as the fitQuota() helper used by nvidia
// and cambricon) *and* lets the result reject the candidate device:
//
//	if !fitQuota(pod, tmpDevs, allocated, pod.Namespace, dev.ID, memreq, coresreq) {
//		reason[common.ResourceQuotaNotFit]++
//		continue
//	}
//
// Calling FitQuota and discarding what it returns leaves the race wide open,
// so reaching the call is necessary but not sufficient: the value has to
// reach a branch condition or a return.
//
// Usage: go run ./hack/tools/quotacheck/ [-allow vendor1,vendor2,...] [path ...]
//
// With no path arguments, it discovers every pkg/device/<vendor>/device.go
// file (skipping the shared pkg/device/common package) and, for each one,
// checks the package's Fit() method, following calls into other functions
// defined in the same package. Path arguments, if given, are
// pkg/device/<vendor>/device.go paths to check instead of the default set.
//
// -allow lists vendor directory names that are still permitted to fail the
// check, for enabling this gate before every backend has been fixed (see
// #2829). A vendor that fails and isn't listed, or that passes while still
// listed (a stale entry left behind after its fix landed), fails the run.
//
// # Known limits
//
// quotacheck parses each package with go/ast alone, without type information,
// which bounds what it can prove:
//
//   - Calls are resolved to same-package declarations only, so a backend that
//     factors its re-check into a shared package would be reported as a
//     violation.
//   - It checks that the result reaches a branch or a return, not that the
//     branch rejects the device, and not that the call dominates the point
//     where the device is accepted. Reviewers still own that.
//
// See isTargetCall for how a FitQuota call is told apart from an unrelated
// method that happens to share the name.
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
// It is always called as a method on the cache the device package owns, e.g.
// device.GetLocalCache().FitQuota(...).
const targetMethod = "FitQuota"

// devicePkgPath is the package that owns QuotaManager.FitQuota. A FitQuota
// call only counts when its receiver chain roots in this package's import,
// so an unrelated method that happens to be named FitQuota cannot satisfy
// the check.
const devicePkgPath = "github.com/Project-HAMi/HAMi/pkg/device"

// fitParamCount and fitResultCount are the parameter and result counts of
// device.Devices.Fit (pkg/device/devices.go):
//
//	Fit(devices []*DeviceUsage, request ContainerDeviceRequest, pod *corev1.Pod,
//		nodeInfo *NodeInfo, allocated *PodDevices) (bool, map[string]ContainerDevices, string)
//
// They identify the interface method among any same-named helpers in the
// package. If the interface signature changes, quotacheck reports that
// explicitly rather than silently finding no Fit() to check.
const (
	fitParamCount  = 5
	fitResultCount = 3
)

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

// checkDeviceFile checks the Fit() method of the package containing path,
// returning a violation message if Fit() cannot reach a call to FitQuota
// through its package's call graph, or reaches one whose result is discarded
// instead of deciding whether the candidate device is accepted.
func checkDeviceFile(path string) ([]string, error) {
	fset := token.NewFileSet()
	pkgFiles, err := parsePackage(fset, filepath.Dir(path))
	if err != nil {
		return nil, err
	}

	fit, problem := findFitMethod(pkgFiles)
	if problem != "" {
		return []string{fmt.Sprintf("%s: %s", path, problem)}, nil
	}

	idx := newDeclIndex(pkgFiles)

	// Collect the calls in Fit()'s own body that reach the re-check, either
	// directly or through a local wrapper. Restricting this to Fit()'s body
	// is what makes the result check below meaningful: the value has to be
	// acted on where the candidate device is chosen.
	quotaCalls := map[*ast.CallExpr]bool{}
	inspectInvokedCalls(fit.Body, func(call *ast.CallExpr) {
		if idx.callReachesTarget(fit, call) {
			quotaCalls[call] = true
		}
	})

	pos := fset.Position(fit.Pos())
	location := fmt.Sprintf("%s:%d", pos.Filename, pos.Line)

	if len(quotaCalls) == 0 {
		return []string{fmt.Sprintf(
			"%s: %s() does not call %s() to re-check ResourceQuota before admitting the pod; "+
				"the re-check is recognised as a call on the cache the device package owns "+
				"(e.g. device.GetLocalCache().%s(...)), directly or through a local helper",
			location, fitMethodName, targetMethod, targetMethod)}, nil
	}

	if !resultGatesAdmission(fit.Body, quotaCalls) {
		return []string{fmt.Sprintf(
			"%s: %s() calls %s() but discards its result; the returned value must reject the "+
				"candidate device (e.g. `if !fitQuota(...) { continue }`), otherwise the re-check has no effect",
			location, fitMethodName, targetMethod)}, nil
	}

	return nil, nil
}

// findFitMethod returns the package's device.Devices Fit implementation: the
// method named fitMethodName whose signature matches the interface. Looking
// across every file of the package (not just device.go) keeps the check
// working if a backend moves Fit() into another file, and matching on the
// signature keeps an unrelated helper that happens to be named Fit from
// shadowing the real one.
//
// The second result is a non-empty message when no single implementation can
// be identified.
func findFitMethod(files []*ast.File) (*ast.FuncDecl, string) {
	var named, candidates []*ast.FuncDecl
	for _, f := range files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Name.Name != fitMethodName {
				continue
			}
			named = append(named, fn)
			if fieldCount(fn.Type.Params) == fitParamCount && fieldCount(fn.Type.Results) == fitResultCount {
				candidates = append(candidates, fn)
			}
		}
	}

	switch {
	case len(candidates) == 1:
		return candidates[0], ""
	case len(candidates) > 1:
		return nil, fmt.Sprintf("found %d methods named %s() matching device.Devices; cannot tell which one implements the interface",
			len(candidates), fitMethodName)
	case len(named) > 0:
		return nil, fmt.Sprintf("found %d method(s) named %s() but none take %d parameters and return %d values; "+
			"if device.Devices.%s changed, update fitParamCount/fitResultCount in quotacheck",
			len(named), fitMethodName, fitParamCount, fitResultCount, fitMethodName)
	default:
		return nil, fmt.Sprintf("no %s() method found", fitMethodName)
	}
}

// fieldCount returns the number of parameters or results in a signature,
// counting each name in a grouped field (e.g. `a, b int` is two) and an
// unnamed field as one.
func fieldCount(fl *ast.FieldList) int {
	if fl == nil {
		return 0
	}
	n := 0
	for _, f := range fl.List {
		if len(f.Names) == 0 {
			n++
			continue
		}
		n += len(f.Names)
	}
	return n
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

// declIndex indexes a package's declarations so a call expression can be
// resolved back to the function or method it invokes. Package-level functions
// and methods are kept apart, and methods are also indexed by receiver type,
// so two same-named methods on different receivers stay distinct instead of
// one overwriting the other.
type declIndex struct {
	funcs   map[string][]*ast.FuncDecl
	methods map[string][]*ast.FuncDecl
	byRecv  map[string][]*ast.FuncDecl

	// devicePkg is the identifier the device package is imported under in
	// the file each declaration lives in, so an aliased import still
	// resolves. It is empty for a file that doesn't import it at all.
	devicePkg map[*ast.FuncDecl]string

	// deviceVars caches, per function, the local variables assigned from an
	// expression rooted in the device package.
	deviceVars map[*ast.FuncDecl]map[string]bool
}

func newDeclIndex(files []*ast.File) *declIndex {
	idx := &declIndex{
		funcs:      map[string][]*ast.FuncDecl{},
		methods:    map[string][]*ast.FuncDecl{},
		byRecv:     map[string][]*ast.FuncDecl{},
		devicePkg:  map[*ast.FuncDecl]string{},
		deviceVars: map[*ast.FuncDecl]map[string]bool{},
	}
	for _, f := range files {
		devicePkg := devicePkgIdent(f)
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			idx.devicePkg[fn] = devicePkg
			if fn.Recv == nil {
				idx.funcs[fn.Name.Name] = append(idx.funcs[fn.Name.Name], fn)
				continue
			}
			idx.methods[fn.Name.Name] = append(idx.methods[fn.Name.Name], fn)
			key := receiverType(fn) + "." + fn.Name.Name
			idx.byRecv[key] = append(idx.byRecv[key], fn)
		}
	}
	return idx
}

// devicePkgIdent returns the identifier devicePkgPath is imported under in f
// (its alias, or the package name), or "" if f does not import it.
func devicePkgIdent(f *ast.File) string {
	for _, imp := range f.Imports {
		if strings.Trim(imp.Path.Value, `"`) != devicePkgPath {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		// Unaliased: the package declares itself as `package device`.
		return "device"
	}
	return ""
}

// isTargetCall reports whether call is the QuotaManager re-check.
//
// Matching on the method name alone would let any same-named method satisfy
// the gate — a local no-op stub named FitQuota, or an unrelated type's method
// — which is exactly the regression this tool exists to catch. Without type
// information the receiver cannot be resolved to device.QuotaManager, so
// instead the receiver chain has to root in the device package's import:
//
//	device.GetLocalCache().FitQuota(...)   // root ident is the device import
//	cache := device.GetLocalCache()        // ...or a local bound to it
//	cache.FitQuota(...)
//
// A backend that reaches the re-check some other way is reported as a
// violation rather than silently passing, which is the safe direction: the
// failure is loud and the message says which shape is recognised.
func (idx *declIndex) isTargetCall(from *ast.FuncDecl, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != targetMethod {
		return false
	}

	devicePkg := idx.devicePkg[from]
	if devicePkg == "" {
		return false
	}

	root := rootIdent(sel.X)
	if root == "" {
		return false
	}
	return root == devicePkg || idx.deviceRootedVars(from)[root]
}

// deviceRootedVars returns the local variables in fn assigned from an
// expression rooted in the device package, so the receiver can be held in a
// variable instead of being called inline.
func (idx *declIndex) deviceRootedVars(fn *ast.FuncDecl) map[string]bool {
	if cached, ok := idx.deviceVars[fn]; ok {
		return cached
	}

	devicePkg := idx.devicePkg[fn]
	names := map[string]bool{}
	idx.deviceVars[fn] = names
	if devicePkg == "" {
		return names
	}

	record := func(lhs []ast.Expr, rhs []ast.Expr) {
		for i, r := range rhs {
			if rootIdent(r) != devicePkg || i >= len(lhs) {
				continue
			}
			if id, ok := lhs[i].(*ast.Ident); ok && id.Name != "_" {
				names[id.Name] = true
			}
		}
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.AssignStmt:
			record(v.Lhs, v.Rhs)
		case *ast.ValueSpec:
			lhs := make([]ast.Expr, len(v.Names))
			for i, name := range v.Names {
				lhs[i] = name
			}
			record(lhs, v.Values)
		}
		return true
	})
	return names
}

// rootIdent returns the identifier an expression chain starts from, e.g.
// "device" for device.GetLocalCache().FitQuota, or "" if it doesn't start
// from a plain identifier.
func rootIdent(expr ast.Expr) string {
	for {
		switch e := expr.(type) {
		case *ast.Ident:
			return e.Name
		case *ast.SelectorExpr:
			expr = e.X
		case *ast.CallExpr:
			expr = e.Fun
		case *ast.ParenExpr:
			expr = e.X
		case *ast.StarExpr:
			expr = e.X
		case *ast.IndexExpr:
			expr = e.X
		case *ast.IndexListExpr:
			expr = e.X
		default:
			return ""
		}
	}
}

// receiverType returns the bare type name a method is declared on, with any
// pointer star and type parameters stripped, or "" if it can't be determined.
func receiverType(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	expr := fn.Recv.List[0].Type
	for {
		switch t := expr.(type) {
		case *ast.StarExpr:
			expr = t.X
		case *ast.IndexExpr: // generic receiver, e.g. Foo[T]
			expr = t.X
		case *ast.IndexListExpr:
			expr = t.X
		case *ast.Ident:
			return t.Name
		default:
			return ""
		}
	}
}

// resolve returns the same-package declarations a call made from caller may
// invoke.
//
// A bare call like fitQuota(...) is either a package-level function or a
// method on the caller's own receiver, both of which resolve exactly. A
// selector call like x.helper(...) cannot be resolved without type
// information, so every same-named method in the package is returned. That
// over-approximates rather than dropping the real callee, which is the safe
// direction for a check whose job is to find backends that never reach the
// re-check at all.
func (idx *declIndex) resolve(caller *ast.CallExpr, from *ast.FuncDecl) []*ast.FuncDecl {
	switch fun := caller.Fun.(type) {
	case *ast.Ident:
		out := idx.funcs[fun.Name]
		if recv := receiverType(from); recv != "" {
			out = append(out, idx.byRecv[recv+"."+fun.Name]...)
		}
		return out
	case *ast.SelectorExpr:
		return idx.methods[fun.Sel.Name]
	default:
		return nil
	}
}

// callReachesTarget reports whether call is the FitQuota re-check itself, or
// a call to a same-package function that reaches it.
func (idx *declIndex) callReachesTarget(from *ast.FuncDecl, call *ast.CallExpr) bool {
	if idx.isTargetCall(from, call) {
		return true
	}
	for _, callee := range idx.resolve(call, from) {
		if idx.reachesTargetMethod(callee, map[*ast.FuncDecl]bool{}) {
			return true
		}
	}
	return false
}

// reachesTargetMethod reports whether fn's body, or any local function it
// calls (transitively), contains a call to targetMethod. visited breaks
// recursive and mutually recursive call chains.
func (idx *declIndex) reachesTargetMethod(fn *ast.FuncDecl, visited map[*ast.FuncDecl]bool) bool {
	if fn == nil || visited[fn] {
		return false
	}
	visited[fn] = true

	found := false
	inspectInvokedCalls(fn.Body, func(call *ast.CallExpr) {
		if found {
			return
		}
		if idx.isTargetCall(fn, call) {
			found = true
			return
		}
		for _, callee := range idx.resolve(call, fn) {
			if idx.reachesTargetMethod(callee, visited) {
				found = true
				return
			}
		}
	})
	return found
}

// resultGatesAdmission reports whether the value returned by one of quotaCalls
// decides control flow in body, either directly inside a branch condition or a
// return, or through a variable it is assigned to.
//
// This is what separates a re-check that works from one that runs and is
// thrown away: `_ = fitQuota(...)`, a bare `fitQuota(...)` statement, or a
// result passed only to a logger all leave the candidate device admitted.
func resultGatesAdmission(body *ast.BlockStmt, quotaCalls map[*ast.CallExpr]bool) bool {
	gating := gatingExprs(body)

	for _, expr := range gating {
		if containsCall(expr, quotaCalls) {
			return true
		}
	}

	bound := identsBoundTo(body, quotaCalls)
	if len(bound) == 0 {
		return false
	}
	for _, expr := range gating {
		if containsIdent(expr, bound) {
			return true
		}
	}
	return false
}

// gatingExprs returns every expression in body whose value decides control
// flow: branch and loop conditions, switch tags and case values, and returned
// values.
func gatingExprs(body *ast.BlockStmt) []ast.Expr {
	var out []ast.Expr
	ast.Inspect(body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.IfStmt:
			out = append(out, v.Cond)
		case *ast.ForStmt:
			if v.Cond != nil {
				out = append(out, v.Cond)
			}
		case *ast.SwitchStmt:
			if v.Tag != nil {
				out = append(out, v.Tag)
			}
		case *ast.CaseClause:
			out = append(out, v.List...)
		case *ast.ReturnStmt:
			out = append(out, v.Results...)
		}
		return true
	})
	return out
}

// identsBoundTo returns the names of variables assigned the value of one of
// calls, so that `ok := fitQuota(...)` followed by `if !ok` counts as gating.
// The blank identifier is deliberately excluded: `_ = fitQuota(...)` discards
// the result.
func identsBoundTo(body *ast.BlockStmt, calls map[*ast.CallExpr]bool) map[string]bool {
	names := map[string]bool{}
	record := func(lhs []ast.Expr) {
		for _, e := range lhs {
			if id, ok := e.(*ast.Ident); ok && id.Name != "_" {
				names[id.Name] = true
			}
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.AssignStmt:
			for _, rhs := range v.Rhs {
				if containsCall(rhs, calls) {
					record(v.Lhs)
					break
				}
			}
		case *ast.ValueSpec:
			for _, val := range v.Values {
				if containsCall(val, calls) {
					for _, id := range v.Names {
						if id.Name != "_" {
							names[id.Name] = true
						}
					}
					break
				}
			}
		}
		return true
	})
	return names
}

// containsCall reports whether expr contains one of calls.
func containsCall(expr ast.Expr, calls map[*ast.CallExpr]bool) bool {
	found := false
	ast.Inspect(expr, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && calls[call] {
			found = true
		}
		return !found
	})
	return found
}

// containsIdent reports whether expr references one of names.
func containsIdent(expr ast.Expr, names map[string]bool) bool {
	found := false
	ast.Inspect(expr, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && names[id.Name] {
			found = true
		}
		return !found
	})
	return found
}

// inspectInvokedCalls walks n and reports every call expression that is
// reachable, and runs synchronously, when the enclosing function runs. It
// descends into a function literal only when that literal is the callee of an
// enclosing call expression (an IIFE), so calls inside an uninvoked closure
// are skipped; and it does not treat the callee of a `go` statement as
// synchronous, since Fit() can return before that goroutine runs.
func inspectInvokedCalls(n ast.Node, visit func(*ast.CallExpr)) {
	if n == nil {
		return
	}
	ast.Inspect(n, func(node ast.Node) bool {
		switch v := node.(type) {
		case *ast.FuncLit:
			// Do not descend into a closure body here; only an
			// immediately-invoked one is walked, via the CallExpr case.
			return false
		case *ast.GoStmt:
			inspectDeferredCall(v.Call, visit)
			return false
		case *ast.DeferStmt:
			// A deferred call runs after Fit()'s body has finished choosing
			// a device, so like `go` it cannot reject the candidate. Its
			// function value and arguments are evaluated at the `defer`
			// statement, so those are still walked.
			inspectDeferredCall(v.Call, visit)
			return false
		case *ast.CallExpr:
			visit(v)
			if lit, ok := v.Fun.(*ast.FuncLit); ok {
				inspectInvokedCalls(lit.Body, visit)
			} else {
				// Walk the callee expression (e.g. the receiver chain
				// of a.b().c()) and the arguments for nested calls.
				inspectInvokedCalls(v.Fun, visit)
			}
			for _, arg := range v.Args {
				inspectInvokedCalls(arg, visit)
			}
			return false
		}
		return true
	})
}

// inspectDeferredCall walks the parts of a `go` or `defer` call that Go
// evaluates synchronously at the statement: the function value and the
// arguments. The call itself is deliberately not reported, since it runs
// after Fit() has already chosen a device.
//
// Walking the function value matters for shapes like `go checkedRunner()()`,
// where the outer invocation is asynchronous but checkedRunner() is not. For
// `go cache.FitQuota(...)` it walks only the receiver chain, so the
// asynchronous FitQuota still does not count.
func inspectDeferredCall(call *ast.CallExpr, visit func(*ast.CallExpr)) {
	inspectInvokedCalls(call.Fun, visit)
	for _, arg := range call.Args {
		inspectInvokedCalls(arg, visit)
	}
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
