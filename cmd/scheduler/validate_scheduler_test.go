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
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes/fake"

	"github.com/Project-HAMi/HAMi/pkg/util/client"
)

func TestValidateSchedulerConfig(t *testing.T) {
	identity := []string{"--device-plugin-namespace=kube-system", "--device-plugin-service-account=hami-device-plugin"}
	tests := []struct {
		name, wantError, wantOutput                string
		args                                       []string
		noIdentity, help, beforeCommand, busyPorts bool
	}{
		{name: "valid options"},
		{name: "finite bonus", args: []string{"--node-affinity-score-weight=2.5"}},
		{name: "options before command", args: []string{"--node-affinity-score-weight=2.5"}, beforeCommand: true},
		{name: "logging option", args: []string{"--v=2"}},
		{name: "occupied ports", busyPorts: true},
		{name: "missing identity", noIdentity: true, wantError: "must both be set"},
		{name: "missing namespace", args: []string{"--device-plugin-namespace="}, wantError: `namespace=""`},
		{name: "missing service account", args: []string{"--device-plugin-service-account="}, wantError: `service-account=""`},
		{name: "negative bonus", args: []string{"--node-affinity-score-weight=-1"}, wantError: "must be a finite number >= 0"},
		{name: "NaN bonus", args: []string{"--node-affinity-score-weight=NaN"}, wantError: "must be a finite number >= 0"},
		{name: "infinite bonus", args: []string{"--node-affinity-score-weight=+Inf"}, wantError: "must be a finite number >= 0"},
		{name: "invalid flag value", args: []string{"--node-lock-timeout=invalid"}, wantError: "invalid argument"},
		{name: "unknown option", args: []string{"--unknown-option=true"}, wantError: "unknown flag"},
		{name: "unexpected path", args: []string{"config.yaml"}, wantError: "unknown command"},
		{name: "remote extender warning", args: []string{"--extender-bind=0.0.0.0:9444"}, wantOutput: "--extender-bind=0.0.0.0:9444 is not a loopback address"},
		{name: "enabled profiling warning", args: []string{"--profiling", "--profiling-bind-address=0.0.0.0:6060"}, wantOutput: "--profiling-bind-address=0.0.0.0:6060 is not a loopback address"},
		{name: "disabled profiling", args: []string{"--profiling=false", "--profiling-bind-address=0.0.0.0:6060"}},
		{name: "separate device validation", args: []string{"--device-config-file=/missing-device-config.yaml"}},
		{name: "help", args: []string{"--help"}, noIdentity: true, help: true, wantOutput: "--node-affinity-score-weight"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := []string{"validate", "scheduler-config"}
			if tt.beforeCommand {
				args = append(append([]string(nil), tt.args...), args...)
			}
			if !tt.noIdentity {
				args = append(args, identity...)
			}
			if !tt.beforeCommand {
				args = append(args, tt.args...)
			}
			if tt.busyPorts {
				args = append(args, "--profiling")
				for _, option := range []string{"http_bind", "extender-bind", "metrics-bind-address", "profiling-bind-address"} {
					listener, err := net.Listen("tcp", "127.0.0.1:0")
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = listener.Close() })
					args = append(args, "--"+option+"="+listener.Addr().String())
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			command := validateTestProcess(ctx, "TestValidateCommandProcess", append([]string{"--"}, args...)...)
			command.Env = append(os.Environ(),
				"HAMI_TEST_VALIDATE_COMMAND=1",
				"KUBECONFIG="+filepath.Join(t.TempDir(), "missing-kubeconfig"),
				"KUBERNETES_SERVICE_HOST=", "KUBERNETES_SERVICE_PORT=",
			)
			output, err := command.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("command did not exit: %v\n%s", ctx.Err(), output)
			}
			if tt.wantError != "" {
				if _, ok := errors.AsType[*exec.ExitError](err); !ok || !strings.Contains(string(output), tt.wantError) {
					t.Fatalf("want error %q, got %v:\n%s", tt.wantError, err, output)
				}
				if strings.Contains(string(output), "passed offline startup validation") {
					t.Fatalf("failed validation reported success:\n%s", output)
				}
				return
			}
			if err != nil {
				t.Fatalf("command failed: %v\n%s", err, output)
			}
			if !tt.help && !strings.Contains(string(output), "Scheduler options passed offline startup validation.") {
				t.Fatalf("missing validation result:\n%s", output)
			}
			if tt.wantOutput != "" && !strings.Contains(string(output), tt.wantOutput) {
				t.Fatalf("want output %q, got:\n%s", tt.wantOutput, output)
			}
			if tt.wantOutput == "" && strings.Contains(string(output), "is not a loopback address") {
				t.Fatalf("unexpected bind warning:\n%s", output)
			}
		})
	}
}

func TestSchedulerStartupRejectsInvalidOptions(t *testing.T) {
	for _, tt := range []struct{ name, weight, want string }{
		{"weight", "-1", "--node-affinity-score-weight must be a finite number >= 0"},
		{"identity", "0", "--device-plugin-namespace and --device-plugin-service-account must both be set"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			deviceConfig, kubeconfig := schedulerStartupFiles(t)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			command := validateTestProcess(ctx, "TestValidateCommandProcess", "--",
				"--device-config-file="+deviceConfig, "--node-affinity-score-weight="+tt.weight)
			command.Env = append(os.Environ(), "HAMI_TEST_VALIDATE_COMMAND=1", "KUBECONFIG="+kubeconfig,
				"KUBERNETES_SERVICE_HOST=", "KUBERNETES_SERVICE_PORT=")
			output, err := command.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("startup did not reject the options: %v\n%s", ctx.Err(), output)
			}
			if _, ok := errors.AsType[*exec.ExitError](err); !ok || !strings.Contains(string(output), tt.want) {
				t.Fatalf("want existing startup error %q, got %v:\n%s", tt.want, err, output)
			}
		})
	}
}

// Isolate startup's clients, device registry and informers from other tests.
// A fake client lets startup reach its listener checks without a cluster.
func TestSchedulerStartupExtenderBindFailure(t *testing.T) {
	if address := os.Getenv("HAMI_TEST_STARTUP_EXTENDER_ADDRESS"); address != "" {
		deviceConfig, kubeconfig := schedulerStartupFiles(t)
		t.Setenv("KUBECONFIG", kubeconfig)
		// Finish the client's one-time construction before substituting the
		// fake used by startup's informers. No API requests are made here.
		client.InitGlobalClient()
		client.KubeClient = fake.NewClientset()

		occupied, err := net.Listen("tcp", address)
		if err != nil {
			t.Fatal(err)
		}
		defer occupied.Close()
		address = occupied.Addr().String()
		rootCmd.SetArgs([]string{
			"--device-config-file=" + deviceConfig,
			"--device-plugin-namespace=kube-system",
			"--device-plugin-service-account=hami-device-plugin",
			"--extender-bind=" + address,
		})
		err = rootCmd.Execute()
		if !errors.Is(err, syscall.EADDRINUSE) || !strings.Contains(err.Error(), "failed to listen on the extender address") {
			t.Fatalf("want synchronous extender bind error, got %v", err)
		}
		return
	}
	for _, address := range []string{"127.0.0.1:0", "0.0.0.0:0"} {
		t.Run(address, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			command := validateTestProcess(ctx, "TestSchedulerStartupExtenderBindFailure")
			command.Env = append(os.Environ(), "HAMI_TEST_STARTUP_EXTENDER_ADDRESS="+address,
				"KUBERNETES_SERVICE_HOST=", "KUBERNETES_SERVICE_PORT=")
			output, err := command.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("startup did not return its listener error: %v\n%s", ctx.Err(), output)
			}
			if err != nil {
				t.Fatalf("startup listener check failed: %v\n%s", err, output)
			}
			warned := strings.Contains(string(output), "is not a loopback address: /filter and /bind authenticate no caller")
			if want := !isLoopbackAddr(address); warned != want {
				t.Fatalf("startup warning = %v, want %v:\n%s", warned, want, output)
			}
		})
	}
}

func schedulerStartupFiles(t *testing.T) (deviceConfig, kubeconfig string) {
	t.Helper()
	directory := t.TempDir()
	deviceConfig = filepath.Join(directory, "devices.yaml")
	if err := os.WriteFile(deviceConfig, []byte("nvidia:\n  resourceCountName: nvidia.com/gpu\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	kubeconfig = filepath.Join(directory, "kubeconfig")
	// Client construction needs a config, but these option checks must
	// reject startup before any Kubernetes API operation is required.
	data := "apiVersion: v1\nkind: Config\nclusters:\n- name: offline\n  cluster:\n    server: https://127.0.0.1:1\n    insecure-skip-tls-verify: true\ncontexts:\n- name: offline\n  context:\n    cluster: offline\n    user: offline\ncurrent-context: offline\nusers:\n- name: offline\n  user:\n    token: unused\n"
	if err := os.WriteFile(kubeconfig, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return deviceConfig, kubeconfig
}
