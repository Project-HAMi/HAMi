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

package metrics

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The skill documents under skill/ name the metrics an operator is told to
// query, but metric names are plain string literals in the collectors, so
// renaming one leaves the documents stale with nothing to catch it. That has
// happened twice already, in #2685 and #2761.
//
// These tests compare the two sides. They live in this package rather than
// beside the documents because hack/unit-test.sh runs
// `go test $(go list ./pkg/... ./cmd/...)`, which does not reach skill/ or
// dashboards/.

// repoRoot is this package's path back to the repository root.
const repoRoot = "../.."

// documentedMetricPattern matches a metric name written as inline code. Metric
// names in the skill documents are always in backticks, which is what separates
// them from the skill's own identifier in the YAML front matter and from family
// wildcards such as `hami_host_gpu_*`, whose trailing character stops the name
// short.
var documentedMetricPattern = regexp.MustCompile("`(hami_[a-z0-9_]+)`")

// registeredMetricDirs are the source trees scanned for collectors. Together
// they are everything hack/unit-test.sh builds, so a collector added anywhere
// in the project is seen.
var registeredMetricDirs = []string{"cmd", "pkg"}

// documentedMetrics returns every metric name referenced by a skill document,
// mapped to the documents that reference it.
func documentedMetrics(t *testing.T) map[string][]string {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join(repoRoot, "skill", "*", "SKILL.md"))
	if err != nil {
		t.Fatalf("glob skill documents: %v", err)
	}
	if len(paths) == 0 {
		t.Fatalf("no skill documents found under %s/skill; has the directory moved?", repoRoot)
	}

	documented := map[string][]string{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		name := filepath.Base(filepath.Dir(path))
		for _, match := range documentedMetricPattern.FindAllStringSubmatch(string(data), -1) {
			metric := match[1]
			if !slices.Contains(documented[metric], name) {
				documented[metric] = append(documented[metric], name)
			}
		}
	}
	return documented
}

// metricNames holds what a scan of Go sources found: names passed to a
// Prometheus constructor as string literals, and the positions of names it
// could not read statically.
type metricNames struct {
	registered map[string]string
	unresolved []string
}

// collectMetricNames records the metric names file declares. Only the first
// argument of prometheus.NewDesc and the Name field of a prometheus.*Opts
// literal count, so a name that merely appears in a comment, a log message or a
// label value is not mistaken for a registration. A name built at run time, for
// example with fmt.Sprintf or from Namespace and Subsystem fields, cannot be
// read from source and is recorded as unresolved instead.
func (m *metricNames) collectMetricNames(fset *token.FileSet, file *ast.File) {
	record := func(expr ast.Expr) {
		position := fset.Position(expr.Pos()).String()
		if lit, ok := expr.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if name, err := strconv.Unquote(lit.Value); err == nil {
				if _, seen := m.registered[name]; !seen {
					m.registered[name] = position
				}
				return
			}
		}
		m.unresolved = append(m.unresolved, position)
	}

	ast.Inspect(file, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.CallExpr:
			if isPrometheusSelector(node.Fun, "NewDesc") && len(node.Args) > 0 {
				record(node.Args[0])
			}
		case *ast.CompositeLit:
			if !isPrometheusSelector(node.Type, "GaugeOpts", "CounterOpts", "HistogramOpts", "SummaryOpts") {
				return true
			}
			for _, element := range node.Elts {
				field, ok := element.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, ok := field.Key.(*ast.Ident)
				if !ok {
					continue
				}
				switch key.Name {
				case "Name":
					record(field.Value)
				case "Namespace", "Subsystem":
					// The exposed name is Namespace_Subsystem_Name, which a
					// literal Name alone does not describe.
					m.unresolved = append(m.unresolved, fset.Position(field.Pos()).String())
				}
			}
		}
		return true
	})
}

// isPrometheusSelector reports whether expr is prometheus.<one of names>.
func isPrometheusSelector(expr ast.Expr, names ...string) bool {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	return ok && pkg.Name == "prometheus" && slices.Contains(names, selector.Sel.Name)
}

// registeredMetrics parses every non-test Go source under registeredMetricDirs
// and returns the metric names their collectors declare.
func registeredMetrics(t *testing.T) metricNames {
	t.Helper()

	found := metricNames{registered: map[string]string{}}
	fset := token.NewFileSet()
	for _, dir := range registeredMetricDirs {
		root := filepath.Join(repoRoot, dir)
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if entry.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if parseErr != nil {
				return parseErr
			}
			found.collectMetricNames(fset, file)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	if len(found.registered) == 0 {
		t.Fatalf("no metric names found under %v; have the collectors changed shape?", registeredMetricDirs)
	}
	return found
}

// TestSkillDocsOnlyReferenceRegisteredMetrics fails when a skill document names
// a metric no collector declares, which is what a rename leaves behind.
func TestSkillDocsOnlyReferenceRegisteredMetrics(t *testing.T) {
	documented := documentedMetrics(t)
	found := registeredMetrics(t)

	var hint string
	if len(found.unresolved) > 0 {
		hint = fmt.Sprintf("; metric names at %s are not string literals and cannot be checked, "+
			"so if the metric is declared there, write its name as a literal",
			strings.Join(found.unresolved, ", "))
	}
	for _, metric := range slices.Sorted(maps.Keys(documented)) {
		if _, ok := found.registered[metric]; !ok {
			t.Errorf("skill document(s) %s reference metric %q, which no collector declares; "+
				"update the document, or the metric name if it was renamed%s",
				strings.Join(documented[metric], ", "), metric, hint)
		}
	}
}

// TestRegisteredMetricsAreDocumented reports metrics no skill document mentions.
// It does not fail: not every metric belongs in an operator-facing summary, and
// failing here would block unrelated work that happens to add one.
func TestRegisteredMetricsAreDocumented(t *testing.T) {
	documented := documentedMetrics(t)
	registered := registeredMetrics(t).registered

	var undocumented []string
	for _, metric := range slices.Sorted(maps.Keys(registered)) {
		// Legacy names such as GPUDeviceMemoryLimit are not what the skill
		// documents describe.
		if !strings.HasPrefix(metric, "hami_") {
			continue
		}
		if _, ok := documented[metric]; !ok {
			undocumented = append(undocumented, metric)
		}
	}
	if len(undocumented) > 0 {
		t.Logf("%d registered metric(s) are not mentioned by any skill document: %s",
			len(undocumented), strings.Join(undocumented, ", "))
	}
}

// TestDocumentedMetricPattern pins the extraction rule against the two shapes
// that are not metric references. Both appear in
// skill/hami-vgpu-metrics-summary/SKILL.md today and a bare hami_[a-z_]+ scan
// reports both as undeclared metrics.
func TestDocumentedMetricPattern(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "inline code reference",
			input: "- `hami_gpu_shared_count`\n",
			want:  []string{"hami_gpu_shared_count"},
		},
		{
			name:  "table cell reference",
			input: "| `hami_vgpu_memory_used_bytes` / `vGPU_device_memory_usage_in_bytes` | used | runtime |\n",
			want:  []string{"hami_vgpu_memory_used_bytes"},
		},
		{
			name:  "front matter skill name is not a metric",
			input: "---\nname: hami_vgpu_metrics_summarizer\n---\n",
			want:  nil,
		},
		{
			name:  "family wildcard is not a metric name",
			input: "| only `hami_host_gpu_*` present | runtime monitor metrics only |\n",
			want:  nil,
		},
		{
			name:  "prose mention without backticks is ignored",
			input: "The hami_gpu_shared_count value shows sharing density.\n",
			want:  nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got []string
			for _, match := range documentedMetricPattern.FindAllStringSubmatch(test.input, -1) {
				got = append(got, match[1])
			}
			if len(got) != len(test.want) {
				t.Fatalf("extracted %v, want %v", got, test.want)
			}
			for i := range got {
				if got[i] != test.want[i] {
					t.Errorf("extracted[%d] = %q, want %q", i, got[i], test.want[i])
				}
			}
		})
	}
}

// TestCollectMetricNames pins the collector-side rule: a name counts as declared
// only where it is handed to a Prometheus constructor, so a quoted mention
// elsewhere does not make a stale document pass, and a name the scan cannot
// read is reported rather than silently skipped.
func TestCollectMetricNames(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		wantRegistered []string
		wantUnresolved int
	}{
		{
			name:           "prometheus desc argument",
			body:           `var d = prometheus.NewDesc("hami_gpu_memory_limit_bytes", "Device memory limit", nil, nil)`,
			wantRegistered: []string{"hami_gpu_memory_limit_bytes"},
		},
		{
			name:           "prometheus opts name field",
			body:           `var o = prometheus.GaugeOpts{Name: "hami_build_info", Help: "build metadata"}`,
			wantRegistered: []string{"hami_build_info"},
		},
		{
			name: "quoted mention in a comment is ignored",
			body: `// Emitted as "hami_gpu_shared_count" per device.
var x = 1`,
		},
		{
			name: "quoted name outside a constructor is ignored",
			body: `func f() { klog.Info("hami_gpu_shared_count"); _ = []string{"hami_node_gpu_overview"} }`,
		},
		{
			name:           "name built with fmt.Sprintf is unresolved",
			body:           `var d = prometheus.NewDesc(fmt.Sprintf("hami_%s_bytes", kind), "help", nil, nil)`,
			wantUnresolved: 1,
		},
		{
			name:           "name from a constant is unresolved",
			body:           `var d = prometheus.NewDesc(metricName, "help", nil, nil)`,
			wantUnresolved: 1,
		},
		{
			name:           "namespace prefix is unresolved",
			body:           `var o = prometheus.CounterOpts{Namespace: "hami", Name: "requests_total"}`,
			wantRegistered: []string{"requests_total"},
			wantUnresolved: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "collector.go", "package collector\n\n"+test.body+"\n", parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse test source: %v", err)
			}

			found := metricNames{registered: map[string]string{}}
			found.collectMetricNames(fset, file)

			if got := slices.Sorted(maps.Keys(found.registered)); !slices.Equal(got, test.wantRegistered) {
				t.Errorf("registered %v, want %v", got, test.wantRegistered)
			}
			if len(found.unresolved) != test.wantUnresolved {
				t.Errorf("unresolved %v, want %d", found.unresolved, test.wantUnresolved)
			}
		})
	}
}
