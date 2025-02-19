/*
 * Copyright (c) 2019-2021, NVIDIA CORPORATION.  All rights reserved.
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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	nvinfo "github.com/NVIDIA/go-nvlib/pkg/nvlib/info"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/fsnotify/fsnotify"
	"github.com/urfave/cli/v2"
	"k8s.io/klog/v2"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"

	spec "github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/api/config/v1"
	"github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/pkg/info"
	"github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/pkg/logger"
	"github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/pkg/plugin"
	"github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/pkg/rm"
	"github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/pkg/watch"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

type options struct {
	flags         []cli.Flag
	configFile    string
	kubeletSocket string
}

func main() {
	c := cli.NewApp()
	o := &options{}
	c.Name = "NVIDIA Device Plugin"
	c.Usage = "NVIDIA device plugin for Kubernetes"
	c.Version = info.GetVersionString()
	c.Action = func(ctx *cli.Context) error {
		return start(ctx, o)
	}

	c.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:    "mig-strategy",
			Value:   spec.MigStrategyNone,
			Usage:   "the desired strategy for exposing MIG devices on GPUs that support it:\n\t\t[none | single | mixed]",
			EnvVars: []string{"MIG_STRATEGY"},
		},
		&cli.BoolFlag{
			Name:    "fail-on-init-error",
			Value:   true,
			Usage:   "fail the plugin if an error is encountered during initialization, otherwise block indefinitely",
			EnvVars: []string{"FAIL_ON_INIT_ERROR"},
		},
		&cli.StringFlag{
			Name:    "driver-root",
			Aliases: []string{"nvidia-driver-root"},
			Value:   "/",
			Usage:   "the root path for the NVIDIA driver installation on the host (typical values are '/' or '/run/nvidia/driver')",
			EnvVars: []string{"NVIDIA_DRIVER_ROOT"},
		},
		&cli.StringFlag{
			Name:    "dev-root",
			Aliases: []string{"nvidia-dev-root"},
			Usage:   "the root path for the NVIDIA device nodes on the host (typical values are '/' or '/run/nvidia/driver')",
			EnvVars: []string{"NVIDIA_DEV_ROOT"},
		},
		&cli.BoolFlag{
			Name:    "pass-device-specs",
			Value:   false,
			Usage:   "pass the list of DeviceSpecs to the kubelet on Allocate()",
			EnvVars: []string{"PASS_DEVICE_SPECS"},
		},
		&cli.StringSliceFlag{
			Name:    "device-list-strategy",
			Value:   cli.NewStringSlice(string(spec.DeviceListStrategyEnvVar)),
			Usage:   "the desired strategy for passing the device list to the underlying runtime:\n\t\t[envvar | volume-mounts | cdi-annotations]",
			EnvVars: []string{"DEVICE_LIST_STRATEGY"},
		},
		&cli.StringFlag{
			Name:    "device-id-strategy",
			Value:   spec.DeviceIDStrategyUUID,
			Usage:   "the desired strategy for passing device IDs to the underlying runtime:\n\t\t[uuid | index]",
			EnvVars: []string{"DEVICE_ID_STRATEGY"},
		},
		&cli.BoolFlag{
			Name:    "gds-enabled",
			Usage:   "ensure that containers are started with NVIDIA_GDS=enabled",
			EnvVars: []string{"GDS_ENABLED"},
		},
		&cli.BoolFlag{
			Name:    "mofed-enabled",
			Usage:   "ensure that containers are started with NVIDIA_MOFED=enabled",
			EnvVars: []string{"MOFED_ENABLED"},
		},
		&cli.StringFlag{
			Name:        "kubelet-socket",
			Value:       pluginapi.KubeletSocket,
			Usage:       "specify the socket for communicating with the kubelet; if this is empty, no connection with the kubelet is attempted",
			Destination: &o.kubeletSocket,
			EnvVars:     []string{"KUBELET_SOCKET"},
		},
		&cli.StringFlag{
			Name:        "config-file",
			Usage:       "the path to a config file as an alternative to command line options or environment variables",
			Destination: &o.configFile,
			EnvVars:     []string{"CONFIG_FILE"},
		},
		&cli.StringFlag{
			Name:    "cdi-annotation-prefix",
			Value:   spec.DefaultCDIAnnotationPrefix,
			Usage:   "the prefix to use for CDI container annotation keys",
			EnvVars: []string{"CDI_ANNOTATION_PREFIX"},
		},
		&cli.StringFlag{
			Name:    "nvidia-cdi-hook-path",
			Aliases: []string{"nvidia-ctk-path"},
			Value:   spec.DefaultNvidiaCTKPath,
			Usage:   "the path to use for NVIDIA CDI hooks in the generated CDI specification",
			EnvVars: []string{"NVIDIA_CDI_HOOK_PATH", "NVIDIA_CTK_PATH"},
		},
		&cli.StringFlag{
			Name:    "driver-root-ctr-path",
			Aliases: []string{"container-driver-root"},
			Value:   spec.DefaultContainerDriverRoot,
			Usage:   "the path where the NVIDIA driver root is mounted in the container; used for generating CDI specifications",
			EnvVars: []string{"DRIVER_ROOT_CTR_PATH", "CONTAINER_DRIVER_ROOT"},
		},
		&cli.StringFlag{
			Name:    "mps-root",
			Usage:   "the path on the host where MPS-specific mounts and files are created by the MPS control daemon manager",
			EnvVars: []string{"MPS_ROOT"},
		},
		&cli.StringFlag{
			Name:    "device-discovery-strategy",
			Value:   "auto",
			Usage:   "the strategy to use to discover devices: 'auto', 'nvml', or 'tegra'",
			EnvVars: []string{"DEVICE_DISCOVERY_STRATEGY"},
		},
		&cli.IntSliceFlag{
			Name:    "imex-channel-ids",
			Usage:   "A list of IMEX channels to inject.",
			EnvVars: []string{"IMEX_CHANNEL_IDS"},
		},
		&cli.BoolFlag{
			Name:    "imex-required",
			Usage:   "The specified IMEX channels are required",
			EnvVars: []string{"IMEX_REQUIRED"},
		},
	}
	// add extra flags for HAMi
	c.Flags = append(c.Flags, addFlags()...)
	o.flags = c.Flags

	err := c.Run(os.Args)
	if err != nil {
		klog.Error(err)
		os.Exit(1)
	}
}

func validateFlags(infolib nvinfo.Interface, config *spec.Config) error {
	deviceListStrategies, err := spec.NewDeviceListStrategies(*config.Flags.Plugin.DeviceListStrategy)
	if err != nil {
		return fmt.Errorf("invalid --device-list-strategy option: %v", err)
	}

	hasNvml, _ := infolib.HasNvml()
	if deviceListStrategies.AnyCDIEnabled() && !hasNvml {
		return fmt.Errorf("CDI --device-list-strategy options are only supported on NVML-based systems")
	}

	if *config.Flags.Plugin.DeviceIDStrategy != spec.DeviceIDStrategyUUID && *config.Flags.Plugin.DeviceIDStrategy != spec.DeviceIDStrategyIndex {
		return fmt.Errorf("invalid --device-id-strategy option: %v", *config.Flags.Plugin.DeviceIDStrategy)
	}

	if config.Sharing.SharingStrategy() == spec.SharingStrategyMPS {
		if *config.Flags.MigStrategy == spec.MigStrategyMixed {
			return fmt.Errorf("using --mig-strategy=mixed is not supported with MPS")
		}
		if config.Flags.MpsRoot == nil || *config.Flags.MpsRoot == "" {
			return fmt.Errorf("using MPS requires --mps-root to be specified")
		}
	}

	switch *config.Flags.DeviceDiscoveryStrategy {
	case "auto":
	case "nvml":
	case "tegra":
	default:
		return fmt.Errorf("invalid --device-discovery-strategy option %v", *config.Flags.DeviceDiscoveryStrategy)
	}

	switch *config.Flags.MigStrategy {
	case spec.MigStrategyNone:
	case spec.MigStrategySingle:
	case spec.MigStrategyMixed:
	default:
		return fmt.Errorf("unknown MIG strategy: %v", *config.Flags.MigStrategy)
	}

	if err := spec.AssertChannelIDsValid(config.Imex.ChannelIDs); err != nil {
		return fmt.Errorf("invalid IMEX channel IDs: %w", err)
	}

	return nil
}

func loadConfig(c *cli.Context, flags []cli.Flag) (*spec.Config, error) {
	config, err := spec.NewConfig(c, flags)
	if err != nil {
		return nil, fmt.Errorf("unable to finalize config: %v", err)
	}
	config.Flags.GFD = nil
	return config, nil
}

func start(c *cli.Context, o *options) error {
	klog.InfoS(fmt.Sprintf("Starting %s", c.App.Name), "version", c.App.Version)

	util.NodeName = os.Getenv(util.NodeNameEnvName)
	// watcher, err := newFSWatcher(kubeletdevicepluginv1beta1.DevicePluginPath)
	// if err != nil {
	// 	return fmt.Errorf("failed to create FS watcher: %v", err)
	// }
	// defer watcher.Close()

	kubeletSocketDir := filepath.Dir(o.kubeletSocket)
	klog.Infof("Starting FS watcher for %v", kubeletSocketDir)
	watcher, err := watch.Files(kubeletSocketDir)
	if err != nil {
		return fmt.Errorf("failed to create FS watcher for %s: %v", pluginapi.DevicePluginPath, err)
	}
	defer watcher.Close()

	klog.Infof("Start working on node %s", util.NodeName)
	klog.Info("Starting OS watcher.")
	sigs := watch.Signals(syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	var started bool
	var restartTimeout <-chan time.Time
	var plugins []plugin.Interface
restart:
	// If we are restarting, stop plugins from previous run.
	if started {
		err := stopPlugins(plugins)
		if err != nil {
			return fmt.Errorf("error stopping plugins from previous run: %v", err)
		}
	}

	klog.Info("Starting Plugins.")
	plugins, restartPlugins, err := startPlugins(c, o)
	if err != nil {
		return fmt.Errorf("error starting plugins: %v", err)
	}
	started = true

	if restartPlugins {
		klog.Infof("Failed to start one or more plugins. Retrying in 30s...")
		restartTimeout = time.After(30 * time.Second)
	}

	// Start an infinite loop, waiting for several indicators to either log
	// some messages, trigger a restart of the plugins, or exit the program.
	for {
		select {
		// If the restart timeout has expired, then restart the plugins
		case <-restartTimeout:
			goto restart

		// Detect a kubelet restart by watching for a newly created
		// 'pluginapi.KubeletSocket' file. When this occurs, restart this loop,
		// restarting all of the plugins in the process.
		case event := <-watcher.Events:
			if o.kubeletSocket != "" && event.Name == o.kubeletSocket && event.Op&fsnotify.Create == fsnotify.Create {
				klog.Infof("inotify: %s created, restarting.", o.kubeletSocket)
				goto restart
			}

		// Watch for any other fs errors and log them.
		case err := <-watcher.Errors:
			klog.Infof("inotify: %s", err)

		// Watch for any signals from the OS. On SIGHUP, restart this loop,
		// restarting all of the plugins in the process. On all other
		// signals, exit the loop and exit the program.
		case s := <-sigs:
			switch s {
			case syscall.SIGHUP:
				klog.Info("Received SIGHUP, restarting.")
				goto restart
			default:
				klog.Infof("Received signal \"%v\", shutting down.", s)
				goto exit
			}
		}
	}
exit:
	err = stopPlugins(plugins)
	if err != nil {
		return fmt.Errorf("error stopping plugins: %v", err)
	}
	return nil
}

func startPlugins(c *cli.Context, o *options) ([]plugin.Interface, bool, error) {
	// Load the configuration file
	klog.Info("Loading configuration.")
	config, err := loadConfig(c, o.flags)
	if err != nil {
		return nil, false, fmt.Errorf("unable to load config: %v", err)
	}
	spec.DisableResourceNamingInConfig(logger.ToKlog, config)

	devConfig, err := generateDeviceConfigFromNvidia(config, c, o.flags)
	if err != nil {
		klog.Errorf("failed to load config file %s", err.Error())
		return nil, false, err
	}

	driverRoot := root(*devConfig.Config.Flags.Plugin.ContainerDriverRoot)
	// We construct an NVML library specifying the path to libnvidia-ml.so.1
	// explicitly so that we don't have to rely on the library path.
	nvmllib := nvml.New(
		nvml.WithLibraryPath(driverRoot.tryResolveLibrary("libnvidia-ml.so.1")),
	)
	devicelib := device.New(nvmllib)
	infolib := nvinfo.New(
		nvinfo.WithNvmlLib(nvmllib),
		nvinfo.WithDeviceLib(devicelib),
	)

	err = validateFlags(infolib, devConfig.Config)
	if err != nil {
		return nil, false, fmt.Errorf("unable to validate flags: %v", err)
	}

	// Update the configuration file with default resources.
	klog.Info("Updating config with default resource matching patterns.")
	err = rm.AddDefaultResourcesToConfig(infolib, nvmllib, devicelib, devConfig.Config)
	if err != nil {
		return nil, false, fmt.Errorf("unable to add default resources to config: %v", err)
	}

	// Print the config to the output.
	configJSON, err := json.MarshalIndent(devConfig.Config, "", "  ")
	if err != nil {
		return nil, false, fmt.Errorf("failed to marshal config to JSON: %v", err)
	}
	klog.Infof("\nRunning with config:\n%v", string(configJSON))

	// Get the set of plugins.
	klog.Info("Retrieving plugins.")
	plugins, err := GetPlugins(infolib, nvmllib, devicelib, devConfig)
	if err != nil {
		return nil, false, fmt.Errorf("error getting plugins: %v", err)
	}

	// Loop through all plugins, starting them if they have any devices
	// to serve. If even one plugin fails to start properly, try
	// starting them all again.
	started := 0
	for _, p := range plugins {
		// Just continue if there are no devices to serve for plugin p.
		if len(p.Devices()) == 0 {
			continue
		}

		// Start the gRPC server for plugin p and connect it with the kubelet.
		if err := p.Start(o.kubeletSocket); err != nil {
			klog.Errorf("Failed to start plugin: %v", err)
			return plugins, true, nil
		}
		started++
	}

	if started == 0 {
		klog.Info("No devices found. Waiting indefinitely.")
	}

	return plugins, false, nil
}

func stopPlugins(plugins []plugin.Interface) error {
	klog.Info("Stopping plugins.")
	var errs error
	for _, p := range plugins {
		errs = errors.Join(errs, p.Stop())
	}
	return errs
}
