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

// Package tools implements the read-only MCP tools exposing HAMi's GPU
// scheduling state: node inventory, pod allocations, quota usage, node
// detail, and Prometheus metrics.
package tools

import (
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	corev1 "k8s.io/api/core/v1"
)

// errorResult builds a CallToolResult that reports a tool-level failure to
// the MCP client. Per the SDK's guidance, tool errors are returned as a
// normal result with IsError set — not as a Go error from the handler — so
// the calling model can see what happened and self-correct, instead of the
// error being swallowed into an opaque protocol-level failure.
func errorResult(format string, args ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, args...)}},
	}
}

// isTerminalPhase reports whether a pod is in a phase Kubernetes itself
// excludes from ResourceQuota accounting. Tools that sum live usage from
// pod annotations must apply the same filter, or their numbers won't match
// what a ResourceQuota reports.
func isTerminalPhase(phase corev1.PodPhase) bool {
	return phase == corev1.PodSucceeded || phase == corev1.PodFailed
}
