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

package scheduler

import (
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

func getNodeSelectorFromEnv() map[string]string {
	nodeSelector := make(map[string]string)
	for _, env := range os.Environ() {
		pair := strings.SplitN(env, "=", 2)
		if strings.HasPrefix(pair[0], "NODE_SELECTOR_") {
			key := strings.ToLower(strings.TrimPrefix(pair[0], "NODE_SELECTOR_"))
			value := pair[1]
			nodeSelector[key] = value
		}
	}
	return nodeSelector
}

func filterNodesBySelector(nodes []*corev1.Node, nodeSelector map[string]string) []*corev1.Node {
	// If no nodeSelector is specified, return all nodes.
	if len(nodeSelector) == 0 {
		return nodes
	}
	// Filter nodes by nodeSelector.
	var filteredNodes []*corev1.Node
	for _, node := range nodes {
		if matchesNodeSelector(node, nodeSelector) {
			filteredNodes = append(filteredNodes, node)
		}
	}
	return filteredNodes
}

func matchesNodeSelector(node *corev1.Node, nodeSelector map[string]string) bool {
	for key, value := range nodeSelector {
		if nodeValue, ok := node.Labels[key]; !ok || nodeValue != value {
			return false
		}
	}
	return true
}
