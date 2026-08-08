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
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/plugin"
	versionmetrics "github.com/Project-HAMi/HAMi/pkg/metrics"
	"github.com/Project-HAMi/HAMi/pkg/monitor/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/flag"
	"github.com/Project-HAMi/HAMi/pkg/version"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
)

var (
	rootCmd = &cobra.Command{
		Use:   "vGPUmonitor",
		Short: "Hami vgpu vGPUmonitor",
		RunE: func(cmd *cobra.Command, args []string) error {
			flag.PrintPFlags(cmd.Flags())
			return start()
		},
	}
	metricsBindAddress string
	legacyMetrics      bool
)

// isMigApplyLockExistFn is a package-level testability seam: production code
// always calls plugin.IsMigApplyLockExist(); tests may override this var
// (restoring it via t.Cleanup) to control lock-present/absent state in the
// wait-loop without real filesystem or GPU infrastructure.
var isMigApplyLockExistFn = plugin.IsMigApplyLockExist

// waitForLockRemoval blocks until lockExistFn reports the lock is gone, ctx is
// cancelled, or sigChan is closed. It returns true when the lock has been
// released and the caller should restart watchAndFeedback, and false when the
// caller should exit instead (ctx cancelled or channel closed).
//
// This is the logic that was previously inlined in start()'s goroutine; it is
// extracted so that unit tests can call it directly and Codecov instruments the
// real source lines.
func waitForLockRemoval(ctx context.Context, sigChan <-chan struct{}, lockExistFn func() bool) bool {
	for lockExistFn() {
		select {
		case <-ctx.Done():
			return false
		case _, ok := <-sigChan:
			if !ok {
				return false
			}
		}
	}
	return true
}

func init() {
	rootCmd.Flags().SortFlags = false
	rootCmd.PersistentFlags().SortFlags = false
	rootCmd.Flags().AddGoFlagSet(util.InitKlogFlags())
	rootCmd.Flags().StringVar(&metricsBindAddress, "metrics-bind-address", ":9394", "The TCP address that the vGPUmonitor should bind to for serving prometheus metrics(e.g. 127.0.0.1:9394, :9394)")
	rootCmd.Flags().BoolVar(&legacyMetrics, "legacy-metrics", false, "Emit legacy metric names alongside new ones for backward compatibility")
	rootCmd.AddCommand(version.VersionCmd)
}

func start() error {
	if err := ValidateEnvVars(); err != nil {
		return fmt.Errorf("failed to validate environment variables: %v", err)
	}

	containerLister, err := nvidia.NewContainerLister()
	if err != nil {
		return fmt.Errorf("failed to create container lister: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Prepare the lock file sub directory.Due to the sequence of startup processes, both the device plugin
	// and the vGPU monitor should attempt to create this directory by default to ensure its creation.
	err = plugin.CreateMigApplyLockDir()
	if err != nil {
		return fmt.Errorf("failed to create MIG apply lock directory: %v", err)
	}

	lockChannel, err := plugin.WatchLockFile()
	if err != nil {
		return fmt.Errorf("failed to watch lock file: %v", err)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 2)

	// Start the metrics service
	wg.Go(func() {
		if err := initMetrics(ctx, containerLister); err != nil {
			errCh <- err
		}
	})

	// Start the monitoring and feedback service
	wg.Go(func() {
		for {
			if err := watchAndFeedback(ctx, containerLister, lockChannel); err != nil {
				// if err is temporary closed, wait for lock file to be removed
				if errors.Is(err, errTemporaryClosed) {
					klog.Info("MIG apply lock file detected, waiting for lock file to be removed")
					if !waitForLockRemoval(ctx, lockChannel, isMigApplyLockExistFn) {
						return
					}
					klog.Info("MIG apply lock file has been removed, restarting watchAndFeedback")
					continue
				}
				errCh <- err
				return
			}
			return
		}
	})

	// Capture system signals
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-signalCh:
		klog.Infof("Received signal: %s", sig)
		cancel()
	case err := <-errCh:
		klog.Errorf("Received error: %v", err)
		cancel()
	}

	// Wait for all goroutines to complete
	wg.Wait()
	close(errCh)
	return nil
}

func initMetrics(ctx context.Context, containerLister *nvidia.ContainerLister) error {
	klog.V(4).Info("Initializing metrics for vGPUmonitor")
	reg := prometheus.NewRegistry()
	//reg := prometheus.NewPedanticRegistry()

	reg.MustRegister(versionmetrics.NewBuildInfoCollector())

	NewClusterManager("vGPU", reg, containerLister, legacyMetrics)

	// Uncomment to add the standard process and Go metrics to the custom registry.
	//reg.MustRegister(
	//	prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}),
	//	prometheus.NewGoCollector(),
	//)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	server := &http.Server{Addr: metricsBindAddress, Handler: mux, ReadHeaderTimeout: 15 * time.Second, ReadTimeout: 60 * time.Second}

	// Starting the HTTP server in a goroutine
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			klog.Errorf("Failed to serve metrics: %v", err)
		}
	}()

	// Graceful shutdown on context cancellation
	<-ctx.Done()
	klog.V(4).Info("Shutting down metrics server")
	if err := server.Shutdown(context.Background()); err != nil {
		return err
	}

	return nil
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		klog.Fatal(err)
	}
}
