/*
 * Copyright © 2021 peizhaoyou <peizhaoyou@4paradigm.com>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/scheduler"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/routes"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
	"github.com/Project-HAMi/HAMi/pkg/version"
	"github.com/google/uuid"
	"github.com/julienschmidt/httprouter"
	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	klog "k8s.io/klog/v2"
)

//var version string

var (
	sher        *scheduler.Scheduler
	tlsKeyFile  string
	tlsCertFile string
	rootCmd     = &cobra.Command{
		Use:   "scheduler",
		Short: "kubernetes vgpu scheduler",
		Run: func(cmd *cobra.Command, args []string) {
			Run(context.Background())
		},
	}
)

func init() {
	rootCmd.Flags().SortFlags = false
	rootCmd.PersistentFlags().SortFlags = false

	rootCmd.Flags().StringVar(&config.HttpBind, "http_bind", "127.0.0.1:8080", "http server bind address")
	rootCmd.Flags().StringVar(&tlsCertFile, "cert_file", "", "tls cert file")
	rootCmd.Flags().StringVar(&tlsKeyFile, "key_file", "", "tls key file")
	rootCmd.Flags().StringVar(&config.SchedulerName, "scheduler-name", "", "the name to be added to pod.spec.schedulerName if not empty")
	rootCmd.Flags().Int32Var(&config.DefaultMem, "default-mem", 0, "default gpu device memory to allocate")
	rootCmd.Flags().Int32Var(&config.DefaultCores, "default-cores", 0, "default gpu core percentage to allocate")
	rootCmd.Flags().Int32Var(&config.DefaultResourceNum, "default-gpu", 1, "default gpu to allocate")
	rootCmd.Flags().StringVar(&config.MetricsBindAddress, "metrics-bind-address", ":9395", "The TCP address that the scheduler should bind to for serving prometheus metrics(e.g. 127.0.0.1:9395, :9395)")
	rootCmd.Flags().BoolVar(&config.LeaderElection.LeaderElect, "leader-elect", true, "leaderElect enables a leader election")
	rootCmd.Flags().StringVar(&config.LeaderElection.ResourceLock, "leader-elect-resource-lock", "leases", "resourceLock indicates the resource object type that will be used to lock during leader election cycles.")
	rootCmd.Flags().StringVar(&config.LeaderElection.ResourceName, "leader-elect-resource-name", "hami-scheduler", "resourceName indicates the name of resource object that will be used to lock during leader election cycles.")
	rootCmd.Flags().StringVar(&config.LeaderElection.ResourceNamespace, "leader-elect-resource-namespace", "default", "resourceNamespace indicates the namespace of resource object that will be used to lock during leader election cycles.")

	rootCmd.PersistentFlags().AddGoFlagSet(device.GlobalFlagSet())
	rootCmd.AddCommand(version.VersionCmd)
	rootCmd.Flags().AddGoFlagSet(util.InitKlogFlags())
}

func start(ctx context.Context) {
	sher = scheduler.NewScheduler()
	sher.Start()
	go sher.Stop(ctx)

	// start monitor metrics
	go sher.RegisterFromNodeAnnotatons()
	go initmetrics(config.MetricsBindAddress)

	// start http server
	router := httprouter.New()
	router.POST("/filter", routes.PredicateRoute(sher))
	router.POST("/bind", routes.Bind(sher))
	router.POST("/webhook", routes.WebHookRoute())
	klog.Info("listen on ", config.HttpBind)
	if len(tlsCertFile) == 0 || len(tlsKeyFile) == 0 {
		if err := http.ListenAndServe(config.HttpBind, router); err != nil {
			klog.Fatal("Listen and Serve error, ", err)
		}
	} else {
		if err := http.ListenAndServeTLS(config.HttpBind, tlsCertFile, tlsKeyFile, router); err != nil {
			klog.Fatal("Listen and Serve error, ", err)
		}
	}
}

func Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ch
		klog.Info("Received termination, signaling shutdown")
		cancel()
	}()

	if !config.LeaderElection.LeaderElect {
		klog.Infof("skip leader election")
		start(ctx)
		return nil
	}
	kubeClient := client.GetClient()
	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("unable to get hostname: %v", err)
	}
	uid, _ := uuid.NewUUID()
	id := hostname + "_" + uid.String()
	lc := resourcelock.ResourceLockConfig{
		Identity: id,
	}
	rl, err := resourcelock.New(config.LeaderElection.ResourceLock, config.LeaderElection.ResourceName, config.LeaderElection.ResourceNamespace, kubeClient.CoreV1(), kubeClient.CoordinationV1(), lc)
	if err != nil {
		return fmt.Errorf("create resourcelock error: %v", err)
	}
	leaderElectionConfig := leaderelection.LeaderElectionConfig{
		Lock:            rl,
		LeaseDuration:   60 * time.Second,
		RenewDeadline:   15 * time.Second,
		RetryPeriod:     5 * time.Second,
		WatchDog:        leaderelection.NewLeaderHealthzAdaptor(time.Second * 20),
		Name:            config.LeaderElection.ResourceName,
		ReleaseOnCancel: true,
	}
	leaderElectionConfig.Callbacks = leaderelection.LeaderCallbacks{
		OnStartedLeading: func(ctx context.Context) {
			start(ctx)
		},
		OnStoppedLeading: func() {
			select {
			case <-ctx.Done():
				// We were asked to terminate. Exit 0.
				klog.Info("Requested to terminate, exiting")
				os.Exit(0)
			default:
				// We lost the lock.
				klog.Error(nil, "Leaderelection lost")
				klog.FlushAndExit(klog.ExitFlushTimeout, 1)
			}
		},
	}
	leaderElector, err := leaderelection.NewLeaderElector(leaderElectionConfig)
	if err != nil {
		return fmt.Errorf("couldn't create leader elector: %v", err)
	}
	leaderElector.Run(ctx)
	return nil
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		klog.Fatal(err)
	}
}
