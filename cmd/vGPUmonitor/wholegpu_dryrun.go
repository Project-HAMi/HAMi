/*
Copyright 2025 The HAMi Authors.

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
	"io"
	"os"
	"path/filepath"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/Project-HAMi/HAMi/pkg/monitor/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

var newWholeGPUDryRunClient = newWholeGPUDryRunKubernetesClient
var newWholeGPUDryRunNVML = nvml.New

func newDryRunCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "dry-run",
		Short: "Run read-only monitor diagnostics",
	}
	command.AddCommand(newWholeGPUDryRunCommand())
	return command
}

func newWholeGPUDryRunCommand() *cobra.Command {
	var options nvidia.WholeGPUDryRunOptions
	command := &cobra.Command{
		Use:   "whole-gpu",
		Short: "Inspect whole-GPU allocations and sample NVML metrics without starting the monitor",
		Long: "Lists Pods scheduled on NODE_NAME and reads their NVIDIA allocation annotations and node device registry. " +
			"It only sends Kubernetes GET/LIST requests and reads NVML; it does not start the monitor, access libvgpu cache files, or apply feedback controls.",
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeName := os.Getenv(util.NodeNameEnvName)
			if nodeName == "" {
				return fmt.Errorf("environment variable %s is not set", util.NodeNameEnvName)
			}
			client, err := newWholeGPUDryRunClient()
			if err != nil {
				return err
			}
			report, err := nvidia.RunWholeGPUDryRun(cmd.Context(), client, newWholeGPUDryRunNVML(), nodeName, options)
			if err != nil {
				return err
			}
			writeWholeGPUDryRunReport(cmd.OutOrStdout(), report)
			return nil
		},
	}
	command.Flags().StringVar(&options.Namespace, "namespace", "", "Only inspect Pods in this namespace")
	command.Flags().StringVar(&options.Pod, "pod", "", "Only inspect this Pod name")
	command.Flags().StringVar(&options.Container, "container", "", "Only inspect this container name")
	return command
}

func newWholeGPUDryRunKubernetesClient() (kubernetes.Interface, error) {
	config, err := loadWholeGPUDryRunKubeConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load Kubernetes configuration: %w", err)
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}
	return client, nil
}

func loadWholeGPUDryRunKubeConfig() (*rest.Config, error) {
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}
	if kubeconfig != "" {
		config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("failed to load explicit kubeconfig %s: %w", kubeconfig, err)
		}
		return config, nil
	}

	inClusterConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("in-cluster config: %w", err)
	}
	return inClusterConfig, nil
}

func writeWholeGPUDryRunReport(writer io.Writer, report *nvidia.WholeGPUDryRunReport) {
	fmt.Fprintf(writer, "whole-gpu dry-run\n")
	fmt.Fprintf(writer, "node: %s\n", report.Node)
	fmt.Fprintln(writer, "mode: read-only")
	fmt.Fprintf(writer, "scanned_pods: %d\n", report.ScannedPods)
	fmt.Fprintf(writer, "candidate_containers: %d\n", report.CandidateContainers)
	fmt.Fprintf(writer, "confirmed_whole_gpu_containers: %d\n", report.ConfirmedContainers)

	if len(report.Containers) == 0 {
		fmt.Fprintln(writer, "confirmed_containers: none")
	} else {
		fmt.Fprintln(writer, "confirmed_containers:")
		for _, container := range report.Containers {
			fmt.Fprintf(writer, "  - namespace=%s pod=%s container=%s\n", container.Namespace, container.Pod, container.Container)
			for _, device := range container.Devices {
				fmt.Fprintf(writer, "    device index=%d uuid=%s memory_used=%d memory_total=%d sm_util=%d", device.Index, device.UUID, device.MemoryUsed, device.MemoryTotal, device.SMUtil)
				if device.Error != "" {
					fmt.Fprintf(writer, " error=%q", device.Error)
				}
				fmt.Fprintln(writer)
			}
		}
	}

	if len(report.Diagnostics) == 0 {
		fmt.Fprintln(writer, "diagnostics: none")
		return
	}
	fmt.Fprintln(writer, "diagnostics:")
	for _, diagnostic := range report.Diagnostics {
		fmt.Fprintf(writer, "  - namespace=%s pod=%s", diagnostic.Namespace, diagnostic.Pod)
		if diagnostic.Container != "" {
			fmt.Fprintf(writer, " container=%s", diagnostic.Container)
		}
		fmt.Fprintf(writer, " status=%s message=%q\n", diagnostic.Status, diagnostic.Message)
	}
}

func runWholeGPUDryRun(ctx context.Context, client kubernetes.Interface, nvmllib nvml.Interface, nodeName string, options nvidia.WholeGPUDryRunOptions) (*nvidia.WholeGPUDryRunReport, error) {
	return nvidia.RunWholeGPUDryRun(ctx, client, nvmllib, nodeName, options)
}
