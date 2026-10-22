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
	"flag"
	"os"
	"path/filepath"
	"testing"

	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
	cli "github.com/urfave/cli/v2"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/plugin"
)

func newTestConfigFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
		t.Fatalf("failed to write test config file: %v", err)
	}
	return path
}

func newReportNodeCapacityContext(t *testing.T, set bool, value bool) *cli.Context {
	t.Helper()
	app := &cli.App{Flags: addFlags()}
	flagSet := flag.NewFlagSet("test", flag.ContinueOnError)
	flagSet.Bool("report-node-capacity", false, "")
	if set {
		if err := flagSet.Set("report-node-capacity", boolString(value)); err != nil {
			t.Fatalf("failed to set report-node-capacity flag: %v", err)
		}
	}
	return cli.NewContext(app, flagSet, nil)
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func TestGenerateDeviceConfigFromNvidiaReportNodeCapacity(t *testing.T) {
	tests := []struct {
		name         string
		ctx          func(t *testing.T) *cli.Context
		wantCapacity *bool
	}{
		{
			name: "flag explicitly set to true",
			ctx: func(t *testing.T) *cli.Context {
				return newReportNodeCapacityContext(t, true, true)
			},
			wantCapacity: new(true),
		},
		{
			name: "flag not set, defaults to false",
			ctx: func(t *testing.T) *cli.Context {
				return newReportNodeCapacityContext(t, false, false)
			},
			wantCapacity: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := newTestConfigFile(t)
			plugin.ConfigFile = &configPath

			devcfg, err := generateDeviceConfigFromNvidia(&spec.Config{}, tt.ctx(t), nil)
			if err != nil {
				t.Fatalf("generateDeviceConfigFromNvidia() error = %v", err)
			}

			if tt.wantCapacity == nil {
				if devcfg.ReportNodeCapacity != nil {
					t.Errorf("ReportNodeCapacity = %v, want nil", *devcfg.ReportNodeCapacity)
				}
				return
			}
			if devcfg.ReportNodeCapacity == nil || *devcfg.ReportNodeCapacity != *tt.wantCapacity {
				t.Errorf("ReportNodeCapacity = %v, want %v", devcfg.ReportNodeCapacity, *tt.wantCapacity)
			}
		})
	}
}

func TestGenerateDeviceConfigFromNvidiaDisableCoreLimit(t *testing.T) {
	tests := []struct {
		name string
		args []string
		env  string
		want bool
	}{
		{name: "unset defaults to false"},
		{name: "CLI true", args: []string{"--disable-core-limit=true"}, want: true},
		{name: "CLI false", args: []string{"--disable-core-limit=false"}},
		{name: "bare CLI flag", args: []string{"--disable-core-limit"}, want: true},
		{name: "environment true", env: "true", want: true},
		{name: "environment false", env: "false"},
		{name: "CLI false overrides environment true", args: []string{"--disable-core-limit=false"}, env: "true"},
		{name: "CLI true overrides environment false", args: []string{"--disable-core-limit=true"}, env: "false", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DISABLE_CORE_LIMIT", tt.env)
			previous := plugin.ConfigFile
			t.Cleanup(func() { plugin.ConfigFile = previous })
			configPath := newTestConfigFile(t)
			plugin.ConfigFile = &configPath
			cfg := &spec.Config{}
			called := false
			app := &cli.App{
				Flags: addFlags(),
				Action: func(c *cli.Context) error {
					called = true
					got, err := generateDeviceConfigFromNvidia(cfg, c, nil)
					if err != nil {
						return err
					}
					if got.DisableCoreLimit != tt.want {
						t.Errorf("DisableCoreLimit = %v, want %v", got.DisableCoreLimit, tt.want)
					}
					if got.Config != cfg {
						t.Error("upstream NVIDIA config was not preserved")
					}
					return nil
				},
			}
			if err := app.Run(append([]string{"test"}, tt.args...)); err != nil {
				t.Fatalf("parse CLI and generate config: %v", err)
			}
			if !called {
				t.Fatal("CLI action was not called")
			}
		})
	}
}
