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

// Package mcp implements a read-only Model Context Protocol server exposing
// HAMi's GPU scheduling state (nodes, pods, quota, metrics) to MCP clients
// such as Claude Desktop, Claude Code, and Cursor.
package mcp

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	klog "k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/mcp/client"
	"github.com/Project-HAMi/HAMi/pkg/mcp/resources"
	"github.com/Project-HAMi/HAMi/pkg/mcp/tools"
	"github.com/Project-HAMi/HAMi/pkg/version"
)

// ServerConfig configures the HAMi MCP server.
type ServerConfig struct {
	Kubeconfig     string
	PrometheusURL  string
	MetricsEnabled bool

	// AuthToken, if non-empty, is required as a Bearer token on the HTTP
	// /mcp endpoint. Empty means the endpoint is unauthenticated and must be
	// restricted at the network layer (e.g. a NetworkPolicy).
	AuthToken string

	// SchedulerConfigMapName/Namespace locate the HAMi scheduler device
	// ConfigMap exposed as the hami://config/scheduler resource. Both must
	// be set for that resource to be registered.
	SchedulerConfigMapName      string
	SchedulerConfigMapNamespace string
}

// Server wraps the MCP protocol server together with the clients its tools
// and resources depend on.
type Server struct {
	mcpServer  *mcp.Server
	k8sClient  *client.K8sClient
	promClient *client.PrometheusClient
	config     *ServerConfig
}

// NewServer builds a Server, constructing its Kubernetes and Prometheus
// clients from config.
func NewServer(ctx context.Context, config *ServerConfig) (*Server, error) {
	if config == nil {
		return nil, fmt.Errorf("mcp: ServerConfig must not be nil")
	}

	k8sClient, err := client.NewK8sClient(config.Kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s client: %w", err)
	}

	var promClient *client.PrometheusClient
	if config.PrometheusURL != "" {
		promClient, err = client.NewPrometheusClient(config.PrometheusURL)
		if err != nil {
			return nil, fmt.Errorf("failed to create prometheus client: %w", err)
		}
	}

	return NewServerWithClients(config, k8sClient, promClient)
}

// NewServerWithClients builds a Server from pre-constructed clients. It is
// the entry point used by tests and by callers that want to inject fakes.
func NewServerWithClients(config *ServerConfig, k8sClient *client.K8sClient, promClient *client.PrometheusClient) (*Server, error) {
	if config == nil {
		return nil, fmt.Errorf("mcp: ServerConfig must not be nil")
	}
	if k8sClient == nil {
		return nil, fmt.Errorf("mcp: k8sClient must not be nil")
	}

	impl := &mcp.Implementation{
		Name:    "hami-mcp-server",
		Version: version.Print(),
	}
	mcpServer := mcp.NewServer(impl, nil)

	s := &Server{
		mcpServer:  mcpServer,
		k8sClient:  k8sClient,
		promClient: promClient,
		config:     config,
	}

	tools.RegisterListGPUNodes(mcpServer, k8sClient)
	tools.RegisterListGPUPods(mcpServer, k8sClient)
	tools.RegisterDescribeNode(mcpServer, k8sClient)
	tools.RegisterGetQuotaUsage(mcpServer, k8sClient)
	if promClient != nil {
		tools.RegisterGetGPUMetrics(mcpServer, promClient)
	}

	if config.SchedulerConfigMapName != "" && config.SchedulerConfigMapNamespace != "" {
		resources.RegisterConfigResource(mcpServer, k8sClient,
			config.SchedulerConfigMapNamespace, config.SchedulerConfigMapName)
	} else {
		klog.InfoS("scheduler ConfigMap location not set; hami://config/scheduler resource disabled",
			"hint", "set --scheduler-configmap-name and --scheduler-configmap-namespace")
	}

	return s, nil
}

// Connect attaches the server to an already-constructed transport (used by
// tests, e.g. an in-memory transport pair).
func (s *Server) Connect(ctx context.Context, t mcp.Transport) (*mcp.ServerSession, error) {
	return s.mcpServer.Connect(ctx, t, nil)
}

// Run serves the MCP protocol over stdio. It blocks until ctx is cancelled
// or the client disconnects.
func (s *Server) Run(ctx context.Context) error {
	return s.mcpServer.Run(ctx, &mcp.StdioTransport{})
}

// RunHTTP serves the MCP streamable HTTP endpoint at /mcp on addr, plus
// /healthz and (if enabled) /metrics. It blocks until ctx is cancelled or
// the server returns an error.
func (s *Server) RunHTTP(ctx context.Context, addr string) error {
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return s.mcpServer
	}, nil)

	mux := http.NewServeMux()
	mux.Handle("/mcp", s.authMiddleware(handler))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	if s.config.MetricsEnabled {
		registry := prometheus.NewRegistry()
		registry.MustRegister(collectors.NewGoCollector())
		registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
		mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
		klog.InfoS("Metrics endpoint enabled", "path", "/metrics")
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      120 * time.Second, // streamable HTTP responses can be long-lived
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		klog.InfoS("MCP HTTP server listening", "addr", addr)
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

// authMiddleware enforces a bearer token on /mcp when one is configured. It
// is a no-op (with a startup warning) when AuthToken is empty, so operators
// must consciously accept an unauthenticated endpoint.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	if s.config.AuthToken == "" {
		klog.InfoS("MCP HTTP endpoint is unauthenticated; restrict access with a NetworkPolicy or set --auth-token-file")
		return next
	}
	want := []byte("Bearer " + s.config.AuthToken)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get("Authorization"))
		if subtle.ConstantTimeCompare(got, want) != 1 || len(got) != len(want) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="hami-mcp"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
