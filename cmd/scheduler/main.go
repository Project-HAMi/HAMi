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
	"fmt"
	"net/http"
	"net/http/pprof"

	"github.com/julienschmidt/httprouter"
	"github.com/spf13/cobra"
	klog "k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/scheduler"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/routes"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
	"github.com/Project-HAMi/HAMi/pkg/util/flag"
	"github.com/Project-HAMi/HAMi/pkg/version"
)

//var version string

var (
	sher            *scheduler.Scheduler
	tlsKeyFile      string
	tlsCertFile     string
	enableProfiling bool
	rootCmd         = &cobra.Command{
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

	rootCmd.Flags().StringVar(&config.HTTPBind, "http_bind", "127.0.0.1:8080", "http server bind address")
	rootCmd.Flags().StringVar(&tlsCertFile, "cert_file", "", "tls cert file")
	rootCmd.Flags().StringVar(&tlsKeyFile, "key_file", "", "tls key file")
	rootCmd.Flags().StringVar(&config.SchedulerName, "scheduler-name", "", "the name to be added to pod.spec.schedulerName if not empty")
	rootCmd.Flags().Int32Var(&config.DefaultMem, "default-mem", 0, "default gpu device memory to allocate")
	rootCmd.Flags().Int32Var(&config.DefaultCores, "default-cores", 0, "default gpu core percentage to allocate")
	rootCmd.Flags().Int32Var(&config.DefaultResourceNum, "default-gpu", 1, "default gpu to allocate")
	rootCmd.Flags().StringVar(&config.NodeSchedulerPolicy, "node-scheduler-policy", util.NodeSchedulerPolicyBinpack.String(), "node scheduler policy")
	rootCmd.Flags().StringVar(&config.GPUSchedulerPolicy, "gpu-scheduler-policy", util.GPUSchedulerPolicySpread.String(), "GPU scheduler policy")
	rootCmd.Flags().StringVar(&config.MetricsBindAddress, "metrics-bind-address", ":9395", "The TCP address that the scheduler should bind to for serving prometheus metrics(e.g. 127.0.0.1:9395, :9395)")
	rootCmd.Flags().StringToStringVar(&config.NodeLabelSelector, "node-label-selector", nil, "key=value pairs separated by commas")

	rootCmd.Flags().Float32Var(&config.QPS, "kube-qps", client.DefaultQPS, "QPS to use while talking with kube-apiserver.")
	rootCmd.Flags().IntVar(&config.Burst, "kube-burst", client.DefaultBurst, "Burst to use while talking with kube-apiserver.")
	rootCmd.Flags().IntVar(&config.Timeout, "kube-timeout", client.DefaultTimeout, "Timeout to use while talking with kube-apiserver.")
	rootCmd.Flags().BoolVar(&enableProfiling, "profiling", false, "Enable pprof profiling via HTTP server")

	rootCmd.PersistentFlags().AddGoFlagSet(device.GlobalFlagSet())
	rootCmd.AddCommand(version.VersionCmd)
	rootCmd.Flags().AddGoFlagSet(util.InitKlogFlags())

}

// injectProfilingRoute injects pprof routes into the router.
func injectProfilingRoute(router *httprouter.Router) {
	router.GET("/debug/pprof/*suffix", func(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
		suffix := params.ByName("suffix")
		switch suffix {
		case "/cmdline":
			pprof.Cmdline(w, r)
		case "/profile":
			pprof.Profile(w, r)
		case "/symbol":
			pprof.Symbol(w, r)
		case "/trace":
			pprof.Trace(w, r)
		default:
			pprof.Index(w, r)
		}
	})
}

func start() error {
	client.InitGlobalClient(
		client.WithBurst(config.Burst),
		client.WithQPS(config.QPS),
		client.WithTimeout(config.Timeout),
	)

	device.InitDevices()
	sher = scheduler.NewScheduler()
	sher.Start()
	defer sher.Stop()

	// start monitor metrics
	go sher.RegisterFromNodeAnnotations()
	go initMetrics(config.MetricsBindAddress)

	// start http server
	router := httprouter.New()
	router.POST("/filter", routes.PredicateRoute(sher))
	router.POST("/bind", routes.Bind(sher))
	router.POST("/webhook", routes.WebHookRoute())
	router.GET("/healthz", routes.HealthzRoute())
	klog.Info("listen on ", config.HTTPBind)

	if enableProfiling {
		injectProfilingRoute(router)
		klog.Infof("Profiling enabled, visit %s/debug/pprof/ to view profiles", config.HTTPBind)
	}

	if len(tlsCertFile) == 0 || len(tlsKeyFile) == 0 {
		if err := http.ListenAndServe(config.HTTPBind, router); err != nil {
			return fmt.Errorf("listen and Serve error, %v", err)
		}
	} else {
		if err := http.ListenAndServeTLS(config.HTTPBind, tlsCertFile, tlsKeyFile, router); err != nil {
			return fmt.Errorf("listen and Serve error, %v", err)
		}
	}
	return nil
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		klog.Fatal(err)
	}
}
