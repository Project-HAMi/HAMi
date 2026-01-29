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
)

func init() {
	rootCmd.Flags().SortFlags = false
	rootCmd.PersistentFlags().SortFlags = false
	rootCmd.Flags().AddGoFlagSet(util.InitKlogFlags())
	rootCmd.Flags().StringVar(&metricsBindAddress, "metrics-bind-address", ":9394", "The TCP address that the vGPUmonitor should bind to for serving prometheus metrics(e.g. 127.0.0.1:9394, :9394)")
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

	cgroupDriver = 0 // Explicitly initialize

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
					<-lockChannel
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

	// Construct cluster managers. In real code, we would assign them to
	// variables to then do something with them.
	NewClusterManager("vGPU", reg, containerLister)
	//NewClusterManager("ca", reg)

	// Uncomment to add the standard process and Go metrics to the custom registry.
	//reg.MustRegister(
	//	prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}),
	//	prometheus.NewGoCollector(),
	//)

	http.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	server := &http.Server{Addr: metricsBindAddress, Handler: nil}

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
