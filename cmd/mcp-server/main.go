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
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/spf13/pflag"
	klog "k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/mcp"
	"github.com/Project-HAMi/HAMi/pkg/version"
)

func main() {
	flags := NewMCPFlags()
	flags.AddFlags(pflag.CommandLine)

	showVersion := pflag.Bool("version", false, "Print version and exit")

	klogFlags := flag.NewFlagSet("klog", flag.ExitOnError)
	klog.InitFlags(klogFlags)
	pflag.CommandLine.AddGoFlagSet(klogFlags)

	pflag.Parse()

	if *showVersion {
		fmt.Println(version.Print())
		os.Exit(0)
	}

	if err := setupLogging(klogFlags, flags.LogLevel); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to setup logging: %v\n", err)
		os.Exit(1)
	}

	klog.InfoS("Starting HAMi MCP Server",
		"prometheusURL", flags.PrometheusURL,
		"logLevel", flags.LogLevel,
		"listenAddr", flags.ListenAddr,
		"metricsEnabled", flags.MetricsEnabled,
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	server, err := mcp.NewServer(ctx, &mcp.ServerConfig{
		Kubeconfig:     flags.Kubeconfig,
		PrometheusURL:  flags.PrometheusURL,
		MetricsPort:    flags.MetricsPort,
		MetricsEnabled: flags.MetricsEnabled,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create MCP server: %v\n", err)
		os.Exit(1)
	}

	var runErr error
	if flags.ListenAddr != "" {
		runErr = server.RunHTTP(ctx, flags.ListenAddr)
	} else {
		runErr = server.Run(ctx)
	}
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "MCP server error: %v\n", runErr)
		os.Exit(1)
	}
}

// setupLogging maps a human-readable log level to klog's -v verbosity flag on
// the supplied flagset. The flagset must already have klog flags registered.
func setupLogging(klogFlags *flag.FlagSet, level string) error {
	var v int
	switch level {
	case "debug":
		v = 4
	case "info":
		v = 2
	case "warn":
		v = 1
	case "error":
		v = 0
	default:
		return fmt.Errorf("invalid log level: %s (must be debug, info, warn, or error)", level)
	}
	return klogFlags.Set("v", strconv.Itoa(v))
}
