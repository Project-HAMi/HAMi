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

package mcp

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	klog "k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/mcp/client"
	"github.com/Project-HAMi/HAMi/pkg/mcp/resources"
	"github.com/Project-HAMi/HAMi/pkg/mcp/tools"
)

// ServerConfig holds the configuration for the MCP server.
type ServerConfig struct {
	Kubeconfig     string
	PrometheusURL  string
	MetricsPort    int
	MetricsEnabled bool
}

// Server wraps the MCP server with HAMi-specific functionality.
type Server struct {
	mcpServer *mcp.Server
	k8sClient *client.K8sClient
	promClient *client.PrometheusClient
	config    *ServerConfig
}

// NewServer creates a new HAMi MCP server with all tools registered.
func NewServer(ctx context.Context, config *ServerConfig) (*Server, error) {
	// Create K8s client
	k8sClient, err := client.NewK8sClient(config.Kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create K8s client: %w", err)
	}

	// Create Prometheus client
	promClient, err := client.NewPrometheusClient(config.PrometheusURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create Prometheus client: %w", err)
	}

	return newServerWithClients(config, k8sClient, promClient)
}

// NewServerWithClients creates a HAMi MCP server using pre-built clients.
// Tests can use this to inject fake K8s/Prometheus clients without touching
// kubeconfig or HTTP endpoints.
func NewServerWithClients(config *ServerConfig, k8sClient *client.K8sClient, promClient *client.PrometheusClient) (*Server, error) {
	if k8sClient == nil {
		return nil, fmt.Errorf("k8sClient is required")
	}
	if promClient == nil {
		return nil, fmt.Errorf("promClient is required")
	}
	return newServerWithClients(config, k8sClient, promClient)
}

func newServerWithClients(config *ServerConfig, k8sClient *client.K8sClient, promClient *client.PrometheusClient) (*Server, error) {
	// Create MCP server
	mcpServer := mcp.NewServer(
		&mcp.Implementation{
			Name:    "hami-mcp-server",
			Version: "v0.1.0",
		},
		&mcp.ServerOptions{
			Instructions: "HAMi MCP Server provides read-only access to GPU scheduling state in Kubernetes clusters. " +
				"Use the available tools to list GPU nodes, pods, quotas, metrics, and describe node details.",
		},
	)

	server := &Server{
		mcpServer:  mcpServer,
		k8sClient:  k8sClient,
		promClient: promClient,
		config:     config,
	}

	// Register all tools
	if err := server.registerTools(); err != nil {
		return nil, fmt.Errorf("failed to register tools: %w", err)
	}

	// Register resources
	if err := server.registerResources(); err != nil {
		return nil, fmt.Errorf("failed to register resources: %w", err)
	}

	klog.InfoS("HAMi MCP Server initialized",
		"tools", len(server.getToolNames()),
		"resources", 1,
	)

	return server, nil
}

// registerTools registers all HAMi MCP tools.
func (s *Server) registerTools() error {
	// Register list_gpu_nodes tool
	listGPUNodesTool := tools.NewListGPUNodesTool(s.k8sClient)
	mcp.AddTool(s.mcpServer, listGPUNodesTool.Tool(), listGPUNodesTool.Handler())

	// Register list_gpu_pods tool
	listGPUPodsTool := tools.NewListGPUPodsTool(s.k8sClient)
	mcp.AddTool(s.mcpServer, listGPUPodsTool.Tool(), listGPUPodsTool.Handler())

	// Register get_quota_usage tool
	getQuotaUsageTool := tools.NewGetQuotaUsageTool(s.k8sClient)
	mcp.AddTool(s.mcpServer, getQuotaUsageTool.Tool(), getQuotaUsageTool.Handler())

	// Register get_gpu_metrics tool
	getGPUMetricsTool := tools.NewGetGPUMetricsTool(s.promClient)
	mcp.AddTool(s.mcpServer, getGPUMetricsTool.Tool(), getGPUMetricsTool.Handler())

	// Register describe_node tool
	describeNodeTool := tools.NewDescribeNodeTool(s.k8sClient)
	mcp.AddTool(s.mcpServer, describeNodeTool.Tool(), describeNodeTool.Handler())

	return nil
}

// getToolNames returns a list of registered tool names for logging.
func (s *Server) getToolNames() []string {
	return []string{
		"list_gpu_nodes",
		"list_gpu_pods",
		"get_quota_usage",
		"get_gpu_metrics",
		"describe_node",
	}
}

// registerResources registers all HAMi MCP resources.
func (s *Server) registerResources() error {
	// Register HAMi config resource
	configResource := resources.NewConfigResource(s.k8sClient)
	s.mcpServer.AddResource(configResource.Resource(), configResource.Handler())

	return nil
}

// Run starts the MCP server over stdio transport.
func (s *Server) Run(ctx context.Context) error {
	klog.Info("Starting HAMi MCP Server over stdio transport")
	return s.mcpServer.Run(ctx, &mcp.StdioTransport{})
}

// RunHTTP serves the MCP streamable HTTP endpoint at /mcp on the given address.
// It blocks until the context is cancelled or the server returns an error.
func (s *Server) RunHTTP(ctx context.Context, addr string) error {
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return s.mcpServer
	}, nil)

	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	if s.config.MetricsEnabled {
		registry := prometheus.NewRegistry()
		registry.MustRegister(prometheus.NewGoCollector())
		registry.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
		mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
		klog.InfoS("Metrics endpoint enabled", "path", "/metrics")
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		klog.InfoS("Starting HAMi MCP Server over streamable HTTP", "addr", addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	}
}

// Connect connects the underlying MCP server to a custom transport.
// This is primarily intended for tests using in-memory transports.
func (s *Server) Connect(ctx context.Context, t mcp.Transport) (*mcp.ServerSession, error) {
	return s.mcpServer.Connect(ctx, t, nil)
}

// ToolNames returns the list of registered tool names.
func (s *Server) ToolNames() []string {
	return s.getToolNames()
}
