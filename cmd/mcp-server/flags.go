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
	"github.com/spf13/pflag"
)

// MCPFlags holds the CLI flags for the MCP server.
type MCPFlags struct {
	Kubeconfig     string
	PrometheusURL  string
	LogLevel       string
	ListenAddr     string
	MetricsPort    int
	MetricsEnabled bool
}

// NewMCPFlags creates a new MCPFlags instance with default values.
func NewMCPFlags() *MCPFlags {
	return &MCPFlags{
		Kubeconfig:     "",
		PrometheusURL:  "http://localhost:9090",
		LogLevel:       "info",
		ListenAddr:     "",
		MetricsPort:    9395,
		MetricsEnabled: false,
	}
}

// AddFlags adds the MCP server flags to the given flag set.
func (f *MCPFlags) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&f.Kubeconfig, "kubeconfig", f.Kubeconfig, "Path to kubeconfig file. If not provided, in-cluster config will be used.")
	fs.StringVar(&f.PrometheusURL, "prometheus-url", f.PrometheusURL, "URL of the Prometheus server for metrics queries.")
	fs.StringVar(&f.LogLevel, "log-level", f.LogLevel, "Log level (debug, info, warn, error).")
	fs.StringVar(&f.ListenAddr, "listen-addr", f.ListenAddr, "If set (e.g. ':9395'), serve the MCP streamable HTTP endpoint at /mcp on this address. If empty, run over stdio.")
	fs.IntVar(&f.MetricsPort, "metrics-port", f.MetricsPort, "Port for HAMi scheduler metrics endpoint.")
	fs.BoolVar(&f.MetricsEnabled, "metrics-enabled", f.MetricsEnabled, "Enable the /metrics endpoint for Prometheus scraping.")
}
