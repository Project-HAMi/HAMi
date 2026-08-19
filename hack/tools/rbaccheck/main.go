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

// rbaccheck verifies that production Go code only calls Kubernetes API
// methods (Get, Patch, Update, Delete, Create, List, Watch) that are
// granted by the RBAC roles in the helm chart.
//
// Usage: go run ./hack/tools/rbaccheck/ [path ...]
//
// It reads the ClusterRole/Role templates under charts/hami/templates to
// extract allowed (resource, verb) pairs, then walks Go source files looking
// for method chains like CoreV1().Nodes().Update(...) and reports any verb
// that is not allowed for the matching resource.
//
// Different binaries run under different service accounts, so each source
// directory is checked against the role of the binary that runs its code:
//
//	cmd/scheduler/, pkg/scheduler/, pkg/device/   scheduler roles
//	cmd/device-plugin/, pkg/device-plugin/        device-plugin role
//	shared code (e.g. pkg/util/)                  union of all roles
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type rule struct {
	APIGroups []string `yaml:"apiGroups"`
	Resources []string `yaml:"resources"`
	Verbs     []string `yaml:"verbs"`
}

type roleDoc struct {
	Rules []rule `yaml:"rules"`
}

// resourceToMethod maps YAML resource names to the CoreV1 Go method names.
// Sub-resources (e.g. pods/binding) are not included — they are rare and the
// scheduler code doesn't call them directly.
var resourceToMethod = map[string]string{
	"nodes":                  "Nodes",
	"pods":                   "Pods",
	"configmaps":             "ConfigMaps",
	"events":                 "Events",
	"resourcequotas":         "ResourceQuotas",
	"namespaces":             "Namespaces",
	"services":               "Services",
	"endpoints":              "Endpoints",
	"persistentvolumes":      "PersistentVolumes",
	"persistentvolumeclaims": "PersistentVolumeClaims",
	"serviceaccounts":        "ServiceAccounts",
	"secrets":                "Secrets",
	"limitranges":            "LimitRanges",
	"replicationcontrollers": "ReplicationControllers",
	"componentstatuses":      "ComponentStatuses",
}

// verbMethods is the set of k8s client-go verb method names we check for.
// These are the Go method name forms of the RBAC verbs.
var verbMethods = map[string]struct{}{
	"Get": {}, "List": {}, "Watch": {},
	"Create": {}, "Update": {}, "Patch": {},
	"Delete": {}, "DeleteCollection": {},
}

// roleFiles lists the RBAC role templates in the chart, relative to
// charts/hami/templates. Only the first YAML document of each file is
// parsed, so conditional extra roles (e.g. the mock device plugin) that
// are bound to different service accounts are not merged in.
var roleFiles = []struct {
	name string
	path string
}{
	{"scheduler", "scheduler/clusterrole.yaml"},
	{"scheduler", "scheduler/role.yaml"},
	{"device-plugin", "device-plugin/monitorrole.yaml"},
}

// dirRoles maps code directories (relative to the repo root) to the roles
// of the binaries that run their code. A nil roles slice means the directory
// is shared between binaries and is checked against the union of all roles,
// as are directories not listed here.
var dirRoles = []struct {
	prefix string
	roles  []string
}{
	{"cmd/scheduler/", []string{"scheduler"}},
	{"pkg/scheduler/", []string{"scheduler"}},
	// pkg/device/nvidia is shared: the device plugin also runs it
	// (e.g. CalculateGPUScore from register.go).
	{"pkg/device/nvidia/", nil},
	{"pkg/device/", []string{"scheduler"}},
	{"cmd/device-plugin/", []string{"device-plugin"}},
	{"pkg/device-plugin/", []string{"device-plugin"}},
}

func main() {
	root, err := findRepoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "rbaccheck: %v\n", err)
		os.Exit(1)
	}

	templates := filepath.Join(root, "charts", "hami", "templates")
	roles := make(map[string]map[string]bool)
	for _, rf := range roleFiles {
		set := loadAllowedVerbs(
			filepath.Join(templates, filepath.FromSlash(rf.path)), rf.name)
		if roles[rf.name] == nil {
			roles[rf.name] = set
			continue
		}
		for k := range set {
			roles[rf.name][k] = true
		}
	}

	paths := os.Args[1:]
	if len(paths) == 0 {
		paths = []string{filepath.Join(root, "pkg"), filepath.Join(root, "cmd")}
	}

	exitCode := 0
	for _, path := range paths {
		if err := filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				base := filepath.Base(p)
				if base == "vendor" || base == ".git" || strings.HasPrefix(base, ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			allowed, roleName := allowedFor(p, root, roles)
			violations := checkFile(p, allowed, roleName)
			for _, v := range violations {
				fmt.Printf("%s: %s\n", p, v)
				exitCode = 1
			}
			return nil
		}); err != nil {
			fmt.Fprintf(os.Stderr, "rbaccheck: walk error: %v\n", err)
			os.Exit(1)
		}
	}

	if exitCode != 0 {
		os.Exit(1)
	}
}

// allowedFor returns the allowed (resource, verb) set that applies to a
// source file, based on which binary's role runs the code in its directory.
// Shared directories (nil roles in dirRoles, or not listed) get the union
// of all roles.
func allowedFor(path, root string, roles map[string]map[string]bool) (map[string]bool, string) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	rel = filepath.ToSlash(rel)

	union := func() (map[string]bool, string) {
		allowed := make(map[string]bool)
		for _, set := range roles {
			for k := range set {
				allowed[k] = true
			}
		}
		return allowed, "any"
	}

	for _, dr := range dirRoles {
		if !strings.HasPrefix(rel, dr.prefix) {
			continue
		}
		if dr.roles == nil {
			return union()
		}
		allowed := make(map[string]bool)
		for _, rn := range dr.roles {
			for k := range roles[rn] {
				allowed[k] = true
			}
		}
		return allowed, strings.Join(dr.roles, "+")
	}

	return union()
}

// loadAllowedVerbs reads an RBAC role template and returns a set of
// "resource/verb" keys that are permitted (e.g. "nodes/get", "pods/patch").
func loadAllowedVerbs(path, name string) map[string]bool {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rbaccheck: cannot read %s (%s role): %v\n", path, name, err)
		os.Exit(1)
	}

	// Strip Go template directives so the YAML parser can handle the file.
	lines := strings.Split(string(data), "\n")
	var clean []string
	for _, l := range lines {
		if strings.Contains(l, "{{") {
			continue
		}
		clean = append(clean, l)
	}

	var doc roleDoc
	if err := yaml.Unmarshal([]byte(strings.Join(clean, "\n")), &doc); err != nil {
		fmt.Fprintf(os.Stderr, "rbaccheck: cannot parse %s (%s role): %v\n", path, name, err)
		os.Exit(1)
	}

	if len(doc.Rules) == 0 {
		fmt.Fprintf(os.Stderr, "rbaccheck: no RBAC rules found in %s (%s role)\n", path, name)
		os.Exit(1)
	}

	allowed := make(map[string]bool)
	for _, r := range doc.Rules {
		for _, res := range r.Resources {
			method, ok := resourceToMethod[res]
			if !ok {
				continue // skip sub-resources (pods/binding) and unknown resources
			}
			for _, v := range r.Verbs {
				goVerb := rbacVerbToGoMethod(v)
				allowed[method+"/"+goVerb] = true
			}
		}
	}
	return allowed
}

// checkFile parses a Go file and returns a list of violation messages.
func checkFile(path string, allowed map[string]bool, roleName string) []string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return []string{fmt.Sprintf("parse error: %v", err)}
	}

	var violations []string
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		verb := sel.Sel.Name
		if _, isVerb := verbMethods[verb]; !isVerb {
			return true
		}

		// The receiver of the verb call should be a resource method call:
		//   <something>.Nodes().Get(...)
		//   ^^^^^^^^^^^^^^^^  ^^^
		//   resourceCall      verb
		resourceSel, ok := sel.X.(*ast.CallExpr)
		if !ok {
			return true
		}
		resourceFun, ok := resourceSel.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		resource := resourceFun.Sel.Name

		// Only match chains that go through CoreV1() — e.g.
		// client.CoreV1().Nodes().Get(...). This avoids false
		// positives from mock or helper types whose methods
		// happen to share names with K8s resource methods.
		corev1Call, ok := resourceFun.X.(*ast.CallExpr)
		if !ok {
			return true
		}
		corev1Fun, ok := corev1Call.Fun.(*ast.SelectorExpr)
		if !ok || corev1Fun.Sel.Name != "CoreV1" {
			return true
		}

		if !allowed[resource+"/"+verb] {
			pos := fset.Position(n.Pos())
			violations = append(violations, fmt.Sprintf(
				"%d:%d: %s().%s() uses verb %q which is not allowed for resource %q by the %s RBAC role(s)",
				pos.Line, pos.Column, resource, verb, strings.ToLower(verb), strings.ToLower(resource), roleName))
		}
		return true
	})
	return violations
}

// rbacVerbToGoMethod converts an RBAC verb name (lowercase) to the
// corresponding Go method name on k8s client interfaces.
func rbacVerbToGoMethod(verb string) string {
	switch verb {
	case "get":
		return "Get"
	case "list":
		return "List"
	case "watch":
		return "Watch"
	case "create":
		return "Create"
	case "update":
		return "Update"
	case "patch":
		return "Patch"
	case "delete":
		return "Delete"
	case "deletecollection":
		return "DeleteCollection"
	default:
		return strings.ToUpper(verb[:1]) + verb[1:]
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
