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
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/spf13/cobra"
	klog "k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/scheduler"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/routes"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
	"github.com/Project-HAMi/HAMi/pkg/util/flag"
	"github.com/Project-HAMi/HAMi/pkg/util/nodelock"
	"github.com/Project-HAMi/HAMi/pkg/version"
)

//var version string

var (
	sher                 *scheduler.Scheduler
	tlsKeyFile           string
	tlsCertFile          string
	enableProfiling      bool
	profilingBindAddress string
	legacyMetrics        bool
	rootCmd              = &cobra.Command{
		Use:   "scheduler",
		Short: "kubernetes vgpu scheduler",
		RunE: func(cmd *cobra.Command, args []string) error {
			flag.PrintPFlags(cmd.Flags())
			return start()
		},
	}
)

func init() {
	rootCmd.Flags().SortFlags = false
	rootCmd.PersistentFlags().SortFlags = false

	rootCmd.Flags().StringVar(&config.HTTPBind, "http_bind", "127.0.0.1:8080", "http server bind address, serving /webhook, /refit and the probes")
	rootCmd.Flags().StringVar(&config.ExtenderBind, "extender-bind", "127.0.0.1:9444", "bind address for the scheduler extender's /filter and /bind, called by the kube-scheduler container in this pod. Point it at a routable address only when kube-scheduler runs outside this pod.")
	rootCmd.Flags().StringVar(&tlsCertFile, "cert_file", "", "tls cert file")
	rootCmd.Flags().StringVar(&tlsKeyFile, "key_file", "", "tls key file")
	rootCmd.Flags().StringVar(&config.SchedulerName, "scheduler-name", "", "the name to be added to pod.spec.schedulerName if not empty")
	rootCmd.Flags().StringVar(&config.NodeSchedulerPolicy, "node-scheduler-policy", util.NodeSchedulerPolicyBinpack.String(), "node scheduler policy")
	rootCmd.Flags().StringVar(&device.GPUSchedulerPolicy, "gpu-scheduler-policy", util.GPUSchedulerPolicySpread.String(), "GPU scheduler policy")
	rootCmd.Flags().StringVar(&config.MetricsBindAddress, "metrics-bind-address", ":9395", "The TCP address that the scheduler should bind to for serving prometheus metrics(e.g. 127.0.0.1:9395, :9395)")
	rootCmd.Flags().StringToStringVar(&config.NodeLabelSelector, "node-label-selector", nil, "key=value pairs separated by commas")

	rootCmd.Flags().Float32Var(&config.QPS, "kube-qps", client.DefaultQPS, "QPS to use while talking with kube-apiserver.")
	rootCmd.Flags().IntVar(&config.Burst, "kube-burst", client.DefaultBurst, "Burst to use while talking with kube-apiserver.")
	rootCmd.Flags().IntVar(&config.Timeout, "kube-timeout", client.DefaultTimeout, "Timeout to use while talking with kube-apiserver.")
	rootCmd.Flags().BoolVar(&enableProfiling, "profiling", false, "Enable pprof profiling via HTTP server")
	rootCmd.Flags().StringVar(&profilingBindAddress, "profiling-bind-address", "127.0.0.1:6060", "Bind address for the dedicated pprof HTTP server when profiling is enabled")
	rootCmd.Flags().DurationVar(&config.NodeLockTimeout, "node-lock-timeout", time.Minute*5, "timeout for node locks")
	rootCmd.Flags().DurationVar(&config.NodeLockRetryTimeout, "node-lock-retry-timeout", 28*time.Second, "timeout for retrying LockNode when contended by another PodGroup member (0 disables retry). Align the Extender's httpTimeout in KubeSchedulerConfiguration with this value.")
	rootCmd.Flags().BoolVar(&config.ForceOverwriteDefaultScheduler, "force-overwrite-default-scheduler", true, "Overwrite schedulerName in Pod Spec when set to the const DefaultSchedulerName in https://k8s.io/api/core/v1 package")
	rootCmd.Flags().StringVar(&config.DevicePluginNamespace, "device-plugin-namespace", "", "namespace of the device-plugin ServiceAccount allowed to call the /refit endpoint")
	rootCmd.Flags().StringVar(&config.DevicePluginServiceAccount, "device-plugin-service-account", "", "name of the device-plugin ServiceAccount allowed to call the /refit endpoint")

	rootCmd.Flags().BoolVar(&config.LeaderElect, "leader-elect", false, "The pod of hami-scheduler enable leader select")
	rootCmd.Flags().StringVar(&config.LeaderElectResourceName, "leader-elect-resource-name", "", "The name of resource object that is used for leader election")
	rootCmd.Flags().StringVar(&config.LeaderElectResourceNamespace, "leader-elect-resource-namespace", "", "The namespace of resource object that is used for leader election")
	rootCmd.Flags().BoolVar(&legacyMetrics, "legacy-metrics", false, "Emit legacy metric names alongside new ones for backward compatibility")

	rootCmd.PersistentFlags().AddGoFlagSet(config.GlobalFlagSet())
	rootCmd.AddCommand(version.VersionCmd)
	rootCmd.Flags().AddGoFlagSet(util.InitKlogFlags())
}

// profilingMux keeps diagnostic handlers off the cluster and extender routers.
func profilingMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /debug/pprof/", pprof.Index)
	mux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)
	return mux
}

func listenProfiling(enabled bool, address string) (net.Listener, error) {
	if !enabled {
		return nil, nil
	}
	if !isLoopbackAddr(address) {
		klog.Warningf("--profiling-bind-address=%s is not a loopback address: pprof authenticates no caller and exposes process diagnostics", address)
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on the profiling address %s: %w", address, err)
	}
	return listener, nil
}

func start() error {
	// Initialize node lock timeout from config
	nodelock.NodeLockTimeout = config.NodeLockTimeout
	klog.InfoS("Set node lock timeout", "timeout", nodelock.NodeLockTimeout)
	client.InitGlobalClient(
		client.WithBurst(config.Burst),
		client.WithQPS(config.QPS),
		client.WithTimeout(config.Timeout),
	)

	config.InitDevices()

	var err error
	config.HostName, err = os.Hostname()
	if err != nil {
		return fmt.Errorf("unable to get hostname: %v", err)
	}
	if config.HostName == "" {
		return fmt.Errorf("empty hostname returned")
	}

	// Refuse to start with an unusable /refit identity: empty namespace or
	// service-account means every TokenReview will fail, but silently – the
	// scheduler would appear healthy while all refit calls are rejected.
	if config.DevicePluginNamespace == "" || config.DevicePluginServiceAccount == "" {
		return fmt.Errorf(
			"--device-plugin-namespace and --device-plugin-service-account must both be set; "+
				"they identify the ServiceAccount whose token is accepted on the /refit endpoint "+
				"(see issue #2878). Got namespace=%q service-account=%q",
			config.DevicePluginNamespace, config.DevicePluginServiceAccount,
		)
	}

	sher = scheduler.NewScheduler()
	go sher.RegisterFromNodeAnnotations()
	err = sher.Start()
	if err != nil {
		return err
	}
	defer sher.Stop()

	router := clusterRouter(sher)

	if !isLoopbackAddr(config.ExtenderBind) {
		klog.Warningf("--extender-bind=%s is not a loopback address: /filter and /bind authenticate no caller, "+
			"so anyone able to reach it can drop a pod's device reservation or have its annotations patched", config.ExtenderBind)
	}

	var tlsCfg *tls.Config
	if len(tlsCertFile) > 0 && len(tlsKeyFile) > 0 {
		certWatcher, err := certwatcher.New(tlsCertFile, tlsKeyFile)
		if err != nil {
			return fmt.Errorf("failed to create cert watcher: %w", err)
		}
		tlsCfg = &tls.Config{
			GetCertificate: certWatcher.GetCertificate,
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			if err := certWatcher.Start(ctx); err != nil && err != context.Canceled {
				klog.ErrorS(err, "cert watcher error")
			}
		}()
	}

	// Extender, cluster, and profiling ports are claimed before serving, so a port already in use
	// fails startup outright instead of surfacing from inside a goroutine once
	// the scheduler looks healthy.
	extenderListener, err := net.Listen("tcp", config.ExtenderBind)
	if err != nil {
		return fmt.Errorf("failed to listen on the extender address %s: %w", config.ExtenderBind, err)
	}
	defer extenderListener.Close()
	clusterListener, err := net.Listen("tcp", config.HTTPBind)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", config.HTTPBind, err)
	}
	defer clusterListener.Close()
	profilingListener, err := listenProfiling(enableProfiling, profilingBindAddress)
	if err != nil {
		return err
	}
	if profilingListener != nil {
		defer profilingListener.Close()
	}

	go initMetrics(config.MetricsBindAddress, sher, legacyMetrics)

	// Supervise profiling alongside the scheduler listeners so server failures
	// are returned to the caller instead of leaving a partially working process.
	errCh := make(chan error, 3)
	if profilingListener != nil {
		klog.Infof("Profiling enabled, visit http://%s/debug/pprof/ to view profiles", profilingListener.Addr())
		go func() {
			errCh <- fmt.Errorf("profiling server error: %w", serve(profilingListener, profilingMux(), nil))
		}()
	}
	go func() {
		errCh <- fmt.Errorf("extender server error: %w", serve(extenderListener, extenderRouter(sher), tlsCfg))
	}()
	go func() {
		errCh <- fmt.Errorf("server error: %w", serve(clusterListener, router, tlsCfg))
	}()
	return <-errCh
}

// extenderRouter serves the scheduler extender verbs. kube-scheduler calls
// them over the loopback interface it shares with this container, so they are
// kept off the address the rest of the cluster reaches.
func extenderRouter(s *scheduler.Scheduler) *httprouter.Router {
	router := httprouter.New()
	router.POST("/filter", routes.PredicateRoute(s))
	router.POST("/bind", routes.Bind(s))
	return router
}

// clusterRouter serves the endpoints whose callers live outside this pod:
// /webhook from kube-apiserver, /refit from the device-plugin, and the probes
// from kubelet.
func clusterRouter(s *scheduler.Scheduler) *httprouter.Router {
	router := httprouter.New()
	router.POST("/refit", routes.NumaRefit(s))
	router.POST("/webhook", routes.WebHookRoute())
	router.GET("/healthz", routes.HealthzRoute())
	router.GET("/readyz", routes.ReadyzRoute(s))
	return router
}

func serve(listener net.Listener, handler http.Handler, tlsCfg *tls.Config) error {
	server := &http.Server{
		Handler:           handler,
		TLSConfig:         tlsCfg,
		ReadHeaderTimeout: 15 * time.Second,
		ReadTimeout:       60 * time.Second,
	}
	if tlsCfg == nil {
		klog.InfoS("Starting HTTP server", "address", listener.Addr())
		return server.Serve(listener)
	}
	klog.InfoS("Starting HTTPS server", "address", listener.Addr())
	return server.ServeTLS(listener, "", "")
}

// isLoopbackAddr reports whether addr binds the loopback interface only. An
// empty host (":9444") binds every interface and is not loopback.
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		klog.Fatal(err)
	}
}
