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
	"io"
	"os"
	"regexp"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
)

// podDeviceAllocateAnnotation matches the per-vendor "hami.io/<slug>-devices-to-allocate"
// annotation keys documented in docs/develop/protocol.md. Every device backend registers
// its own key under this suffix (see device.InRequestDevices assignments), so matching by
// pattern lets hami-cli decode any vendor's allocations without importing vendor packages.
var podDeviceAllocateAnnotation = regexp.MustCompile(`^hami\.io/(.+)-devices-to-allocate$`)

type allocationRow struct {
	Node          string
	Namespace     string
	Pod           string
	Container     string
	DeviceType    string
	DeviceUUID    string
	RequestedMem  int32
	RequestedCore int32
}

var allocationsCmd = &cobra.Command{
	Use:   "allocations",
	Short: "List HAMi device allocations decoded from hami.io/* pod annotations",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewClient()
		if err != nil {
			return fmt.Errorf("failed to build kubernetes client: %w", err)
		}
		return runAllocations(cmd.Context(), c, cmd.OutOrStdout())
	},
}

var (
	nodeFilter      string
	namespaceFilter string
)

func init() {
	allocationsCmd.Flags().StringVar(&nodeFilter, "node", "", "only show allocations on this node")
	allocationsCmd.Flags().StringVar(&namespaceFilter, "namespace", "", "only show pods in this namespace (default: all namespaces)")
}

func runAllocations(ctx context.Context, c kubernetes.Interface, out io.Writer) error {
	pods, err := c.CoreV1().Pods(namespaceFilter).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	rows := collectAllocationRows(pods.Items)
	if nodeFilter != "" {
		filtered := rows[:0]
		for _, r := range rows {
			if r.Node == nodeFilter {
				filtered = append(filtered, r)
			}
		}
		rows = filtered
	}

	printAllocationTable(out, rows)
	return nil
}

// collectAllocationRows decodes hami.io/*-devices-to-allocate annotations on every pod
// into a flat, sorted list of allocation rows. Pods with no HAMi annotations are skipped
// silently; pods with malformed HAMi annotations are skipped with a warning so that one
// bad pod cannot hide the rest of the cluster's allocation state.
func collectAllocationRows(pods []corev1.Pod) []allocationRow {
	var rows []allocationRow
	for _, pod := range pods {
		node := pod.Annotations[util.AssignedNodeAnnotations]
		if node == "" {
			continue
		}

		checklist := map[string]string{}
		for key := range pod.Annotations {
			if podDeviceAllocateAnnotation.MatchString(key) {
				checklist[key] = key
			}
		}
		if len(checklist) == 0 {
			continue
		}

		podDevices, err := device.DecodePodDevices(checklist, pod.Annotations)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping pod %s/%s: %v\n", pod.Namespace, pod.Name, err)
			continue
		}

		containerNames := podContainerNames(&pod)
		for _, containers := range podDevices {
			for ctrIdx, ctrDevices := range containers {
				ctrName := fmt.Sprintf("container[%d]", ctrIdx)
				if ctrIdx < len(containerNames) {
					ctrName = containerNames[ctrIdx]
				}
				for _, d := range ctrDevices {
					rows = append(rows, allocationRow{
						Node:          node,
						Namespace:     pod.Namespace,
						Pod:           pod.Name,
						Container:     ctrName,
						DeviceType:    d.Type,
						DeviceUUID:    d.UUID,
						RequestedMem:  d.Usedmem,
						RequestedCore: d.Usedcores,
					})
				}
			}
		}
	}

	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Node != b.Node {
			return a.Node < b.Node
		}
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		if a.Pod != b.Pod {
			return a.Pod < b.Pod
		}
		if a.Container != b.Container {
			return a.Container < b.Container
		}
		return a.DeviceUUID < b.DeviceUUID
	})
	return rows
}

// podContainerNames returns container names in the same order used to build the
// hami.io/*-devices-to-allocate annotation: init containers first, then regular
// containers (see device.Resourcereqs for the matching encode-side order).
func podContainerNames(pod *corev1.Pod) []string {
	names := make([]string, 0, len(pod.Spec.InitContainers)+len(pod.Spec.Containers))
	for _, c := range pod.Spec.InitContainers {
		names = append(names, c.Name)
	}
	for _, c := range pod.Spec.Containers {
		names = append(names, c.Name)
	}
	return names
}

func printAllocationTable(out io.Writer, rows []allocationRow) {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NODE\tNAMESPACE\tPOD\tCONTAINER\tDEVICE TYPE\tDEVICE UUID\tMEMORY\tCORE")
	for _, r := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%d\t%d\n",
			r.Node, r.Namespace, r.Pod, r.Container, r.DeviceType, r.DeviceUUID, r.RequestedMem, r.RequestedCore)
	}
	w.Flush()
}
