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
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/Project-HAMi/HAMi/pkg/monitor/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/flag"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
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
)

func init() {
	rootCmd.Flags().SortFlags = false
	rootCmd.PersistentFlags().SortFlags = false
	rootCmd.Flags().AddGoFlagSet(util.InitKlogFlags())
}

func start() error {
	if err := ValidateEnvVars(); err != nil {
		return fmt.Errorf("Failed to validate environment variables: %v", err)
	}

	containerLister, err := nvidia.NewContainerLister()
	if err != nil {
		return fmt.Errorf("Failed to create container lister: %v", err)
	}

	cgroupDriver = 0 // Explicitly initialize

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	errCh := make(chan error, 2)

	// Start the metrics service
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := initMetrics(ctx, containerLister); err != nil {
			errCh <- err
		}
	}()

	// Start the monitoring and feedback service
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := watchAndFeedback(ctx, containerLister); err != nil {
			errCh <- err
		}
	}()

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
	server := &http.Server{Addr: ":9394", Handler: nil}

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

func watchAndFeedback(ctx context.Context, lister *nvidia.ContainerLister) error {
	if nvret := nvml.Init(); nvret != nvml.SUCCESS {
		return fmt.Errorf("failed to initialize NVML: %s", nvml.ErrorString(nvret))
	}
	defer nvml.Shutdown()

	for {
		select {
		case <-ctx.Done():
			klog.Info("Shutting down watchAndFeedback")
			return nil
		case <-time.After(time.Second * 5):
			if err := lister.Update(); err != nil {
				klog.Errorf("Failed to update container list: %v", err)
				continue
			}
			//klog.Infof("WatchAndFeedback srPodList=%v", srPodList)
			Observe(lister)
		}
	}
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		klog.Fatal(err)
	}
}
