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

package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Project-HAMi/HAMi/pkg/util/client"
)

// Device initialization changes package globals and some backends fail through
// klog.Fatal, so run the real command in a fresh process for each configuration.
func TestValidateDeviceConfig(t *testing.T) {
	const valid = "nvidia:\n  resourceCountName: nvidia.com/gpu\n  defaultGPUNum: 0\n"
	tests := []struct {
		name, config, wantError, wantOutput string
		args                                []string
		missing                             bool
	}{
		{name: "valid defaults", config: valid},
		{name: "malformed YAML", config: "nvidia: [", wantError: "yaml:"},
		{name: "unknown field", config: "nvidia:\n  unknownField: true\n", wantError: "field unknownField not found"},
		{name: "duplicate field", config: "nvidia:\n  defaultCores: 10\n  defaultCores: 20\n", wantError: "already set"},
		{name: "empty config", wantError: "all configurations are empty"},
		{name: "invalid default", config: "nvidia:\n  defaultCores: 101\n", wantError: "defaultCores is a percentage"},
		{name: "backend fatal validation", config: "nvidia:\n  migProfileAllowlist:\n  - models: [A100]\n    profiles: [invalid]\n", wantError: "invalid MIG profile allowlist"},
		{name: "negative memory factor", config: "nvidia:\n  resourceMemoryName: nvidia.com/gpumem\n  memoryFactor: -1\n", wantError: "memoryFactor must not be negative"},
		{name: "duplicate resource", config: "amd:\n  resourceCountName: shared.com/gpu\nbiren:\n  resourceCountName: shared.com/gpu\n", wantError: "both claim resource"},
		{name: "warning stays nonfatal", config: "nvidia:\n  resourceCountName: invalid resource name\n", wantOutput: "not a valid Kubernetes qualified name"},
		{name: "missing file", missing: true, wantError: "load device config"},
		{name: "missing argument", args: []string{"validate", "device-config"}, wantError: "accepts 1 arg(s), received 0"},
		{name: "extra argument", args: []string{"validate", "device-config", "one.yaml", "two.yaml"}, wantError: "accepts 1 arg(s), received 2"},
		{name: "unknown config type", args: []string{"validate", "unknown-config", "one.yaml"}, wantError: "unknown command"},
		{name: "inherited vendor flag", config: "vnpus:\n  configs:\n  - commonWord: AscendA\n    resourceName: shared.com/npu\n  - commonWord: AscendB\n    resourceName: shared.com/npu\n", args: []string{"validate", "device-config", "<path>", "--enable-ascend"}, wantError: "both claim resource"},
		{name: "root help", args: []string{"--help"}, wantOutput: "validate"},
		{name: "parent without a subcommand", args: []string{"validate"}, wantOutput: "device-config"},
		{name: "parent help", args: []string{"validate", "--help"}, wantOutput: "device-config"},
		{name: "device config help", args: []string{"validate", "device-config", "--help"}, wantOutput: "warning-only conditions"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			directory := t.TempDir()
			path := filepath.Join(directory, "device config.yaml")
			if !tt.missing {
				if err := os.WriteFile(path, []byte(tt.config), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			args := tt.args
			if args == nil {
				args = []string{"validate", "device-config", path}
			}
			args = slices.Clone(args)
			for i, arg := range args {
				if arg == "<path>" {
					args[i] = path
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			command := validateTestProcess(ctx, "TestValidateCommandProcess", append([]string{"--"}, args...)...)
			command.Env = append(os.Environ(),
				"HAMI_TEST_VALIDATE_COMMAND=1",
				"KUBECONFIG="+filepath.Join(directory, "missing-kubeconfig"),
				"KUBERNETES_SERVICE_HOST=", "KUBERNETES_SERVICE_PORT=",
			)
			output, err := command.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("command did not exit: %v\n%s", ctx.Err(), output)
			}
			if tt.wantError != "" {
				exitError, ok := errors.AsType[*exec.ExitError](err)
				if !ok || exitError.ExitCode() == 0 {
					t.Fatalf("want nonzero exit, got %v\n%s", err, output)
				}
				if !strings.Contains(string(output), tt.wantError) {
					t.Fatalf("want error %q, got:\n%s", tt.wantError, output)
				}
				if strings.Contains(string(output), "passed startup validation") {
					t.Fatalf("failed validation reported success:\n%s", output)
				}
				return
			}
			if err != nil {
				t.Fatalf("command failed: %v\n%s", err, output)
			}
			if tt.wantOutput != "" && !strings.Contains(string(output), tt.wantOutput) {
				t.Fatalf("want output %q, got:\n%s", tt.wantOutput, output)
			}
			if tt.args == nil && !strings.Contains(string(output), "passed startup validation") {
				t.Fatalf("missing validation result:\n%s", output)
			}
		})
	}
}

// TestValidateCommandProcess exercises root command routing without sharing any
// initialized devices or Kubernetes clients with the parent test process.
func TestValidateCommandProcess(t *testing.T) {
	if os.Getenv("HAMI_TEST_VALIDATE_COMMAND") != "1" {
		return
	}
	separator := slices.Index(os.Args, "--")
	if separator < 0 {
		t.Fatal("missing command arguments")
	}
	rootCmd.SetArgs(os.Args[separator+1:])
	main()
	if sher != nil || client.GetClient() != nil {
		t.Fatal("validation started scheduler or Kubernetes client")
	}
}

type validateErrorWriter struct{}

func (validateErrorWriter) Write([]byte) (int, error) {
	return 0, io.ErrClosedPipe
}

func validateTestProcess(ctx context.Context, test string, args ...string) *exec.Cmd {
	testArgs := []string{"-test.run=^" + test + "$"}
	// go test sets GOCOVERDIR, but successful test subprocesses also need the
	// testing flag to write their counters into the parent's coverage report.
	if testing.CoverMode() != "" {
		if directory := os.Getenv("GOCOVERDIR"); directory != "" {
			testArgs = append(testArgs, "-test.gocoverdir="+directory)
		}
	}
	return exec.CommandContext(ctx, os.Args[0], append(testArgs, args...)...)
}

// Execute directly in an isolated process so returned errors can be checked
// without main's fatal exit preventing the testing framework from finishing.
func TestValidateCommandReturnsErrors(t *testing.T) {
	if scenario := os.Getenv("HAMI_TEST_VALIDATE_RESULT_CASE"); scenario != "" {
		path := filepath.Join(t.TempDir(), "device config.yaml")
		input := "nvidia:\n  resourceCountName: nvidia.com/gpu\n"
		if scenario == "invalid config" {
			input = "nvidia:\n  defaultCores: 101\n"
		}
		if scenario != "missing file" {
			if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		command := newValidateCommand()
		command.SetOut(io.Discard)
		command.SetErr(io.Discard)
		command.SetArgs([]string{"device-config", path})
		if scenario == "output error" {
			command.SetOut(validateErrorWriter{})
		}
		err := command.Execute()
		switch scenario {
		case "missing file":
			if !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), "load device config") || !strings.Contains(err.Error(), path) {
				t.Fatalf("want wrapped file error with path, got %v", err)
			}
		case "invalid config":
			if err == nil || !strings.Contains(err.Error(), "validate device config") || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "defaultCores is a percentage") {
				t.Fatalf("want config validation error with path, got %v", err)
			}
		case "output error":
			if !errors.Is(err, io.ErrClosedPipe) {
				t.Fatalf("want output write error, got %v", err)
			}
		default:
			t.Fatalf("unknown scenario %q", scenario)
		}
		if sher != nil || client.GetClient() != nil {
			t.Fatal("validation started scheduler or Kubernetes client")
		}
		return
	}

	for _, scenario := range []string{"missing file", "invalid config", "output error"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			command := validateTestProcess(ctx, "TestValidateCommandReturnsErrors")
			command.Env = append(os.Environ(),
				"HAMI_TEST_VALIDATE_RESULT_CASE="+scenario,
				"KUBECONFIG="+filepath.Join(t.TempDir(), "missing-kubeconfig"),
				"KUBERNETES_SERVICE_HOST=", "KUBERNETES_SERVICE_PORT=",
			)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("command result check failed: %v\n%s", err, output)
			}
		})
	}
}
