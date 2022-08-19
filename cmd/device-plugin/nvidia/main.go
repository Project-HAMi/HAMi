/*
<<<<<<< HEAD
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

=======
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
>>>>>>> 6d02e30 (major architect update: remove grpc)
package main

import (
	"encoding/json"
<<<<<<< HEAD
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	nvinfo "github.com/NVIDIA/go-nvlib/pkg/nvlib/info"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
	"github.com/fsnotify/fsnotify"
	cli "github.com/urfave/cli/v2"
	errorsutil "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"
	kubeletdevicepluginv1beta1 "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/info"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/plugin"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/rm"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/watch"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
	flagutil "github.com/Project-HAMi/HAMi/pkg/util/flag"
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
	c.Action = func(ctx *cli.Context) error {
		flagutil.PrintCliFlags(ctx)
		return start(ctx, o)
	}
	c.Commands = []*cli.Command{
		{
			Name:  "version",
			Usage: "Show the version of NVIDIA Device Plugin",
			Action: func(c *cli.Context) error {
				fmt.Printf("%s version: %s\n", c.App.Name, info.GetVersionString())
				return nil
			},
		},
	}

	flagset := flag.NewFlagSet("klog", flag.ExitOnError)
	klog.InitFlags(flagset)

	c.Before = func(ctx *cli.Context) error {
		logLevel := ctx.Int("v")
		if err := flagset.Set("v", fmt.Sprintf("%d", logLevel)); err != nil {
			return err
		}
		return nil
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
			Name:    "nvidia-driver-root",
			Value:   "/",
			Usage:   "the root path for the NVIDIA driver installation (typical values are '/' or '/run/nvidia/driver')",
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
			Name:    "gdrcopy-enabled",
			Usage:   "ensure that containers that request NVIDIA GPU resources are started with GDRCopy support",
			EnvVars: []string{"GDRCOPY_ENABLED"},
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
			Value:       kubeletdevicepluginv1beta1.KubeletSocket,
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
		&cli.IntFlag{
			Name:  "v",
			Usage: "number for the log level verbosity",
			Value: 0,
		},
	}
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
	util.NodeName = os.Getenv(util.NodeNameEnvName)
	client.InitGlobalClient()

	kubeletSocketDir := filepath.Dir(o.kubeletSocket)
	klog.Infof("Starting FS watcher for %v", kubeletSocketDir)
	watcher, err := watch.Files(kubeletSocketDir)
	if err != nil {
		return fmt.Errorf("failed to create FS watcher for %s: %v", kubeletdevicepluginv1beta1.DevicePluginPath, err)
	}
	defer watcher.Close()

	/*Loading config files*/
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
		klog.Info("Failed to start one or more plugins. Retrying in 30s...")
		restartTimeout = time.After(30 * time.Second)
	}

=======
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"syscall"

	"4pd.io/k8s-vgpu/pkg/version"

	device_plugin "4pd.io/k8s-vgpu/pkg/device-plugin"
	"4pd.io/k8s-vgpu/pkg/device-plugin/config"
	"4pd.io/k8s-vgpu/pkg/util"
	"github.com/NVIDIA/gpu-monitoring-tools/bindings/go/nvml"
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"k8s.io/klog/v2"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

var (
	failOnInitErrorFlag bool
	//nvidiaDriverRootFlag string
	//enableLegacyPreferredFlag bool
	migStrategyFlag string

	rootCmd = &cobra.Command{
		Use:   "device-plugin",
		Short: "kubernetes vgpu device-plugin",
		Run: func(cmd *cobra.Command, args []string) {
			if err := start(); err != nil {
				klog.Fatal(err)
			}
		},
	}
)

type devicePluginConfigs struct {
	Nodeconfig []struct {
		Name                string  `json:"name"`
		Devicememoryscaling float64 `json:"devicememoryscaling"`
		Devicesplitcount    int     `json:"devicesplitcount"`
		Migstrategy         string  `json:"migstrategy"`
	} `json:"nodeconfig"`
}

func init() {
	// https://github.com/spf13/viper/issues/461
	viper.BindEnv("node-name", "NODE_NAME")

	rootCmd.Flags().SortFlags = false
	rootCmd.PersistentFlags().SortFlags = false

	rootCmd.Flags().StringVar(&migStrategyFlag, "mig-strategy", "none", "the desired strategy for exposing MIG devices on GPUs that support it:\n\t\t[none | single | mixed]")
	rootCmd.Flags().BoolVar(&failOnInitErrorFlag, "fail-on-init-error", true, "fail the plugin if an error is encountered during initialization, otherwise block indefinitely")
	rootCmd.Flags().StringVar(&config.RuntimeSocketFlag, "runtime-socket", "/var/lib/vgpu/vgpu.sock", "runtime socket")
	rootCmd.Flags().UintVar(&config.DeviceSplitCount, "device-split-count", 2, "the number for NVIDIA device split")
	rootCmd.Flags().Float64Var(&config.DeviceMemoryScaling, "device-memory-scaling", 1.0, "the ratio for NVIDIA device memory scaling")
	rootCmd.Flags().Float64Var(&config.DeviceCoresScaling, "device-cores-scaling", 1.0, "the ratio for NVIDIA device cores scaling")
	rootCmd.Flags().StringVar(&config.SchedulerEndpoint, "scheduler-endpoint", "127.0.0.1:9090", "scheduler extender endpoint")
	rootCmd.Flags().IntVar(&config.SchedulerTimeout, "scheduler-timeout", 10, "scheduler connection timeout")
	rootCmd.Flags().StringVar(&config.NodeName, "node-name", viper.GetString("node-name"), "node name")
	rootCmd.Flags().BoolVar(&config.DisableCoreLimit, "disable-core-limit", false, "If set, the core utilization limit will be ignored")

	rootCmd.PersistentFlags().AddGoFlagSet(util.GlobalFlagSet())
	rootCmd.AddCommand(version.VersionCmd)
}

func readFromConfigFile() error {
	jsonbyte, err := ioutil.ReadFile("/config/config.json")
	if err != nil {
		return err
	}
	var deviceConfigs devicePluginConfigs
	err = json.Unmarshal(jsonbyte, &deviceConfigs)
	if err != nil {
		return err
	}
	fmt.Println("json=", deviceConfigs)
	for _, val := range deviceConfigs.Nodeconfig {
		if strings.Compare(os.Getenv("NODE_NAME"), val.Name) == 0 {
			fmt.Println("Reading config from file", val.Name)
			if val.Devicememoryscaling > 0 {
				config.DeviceMemoryScaling = val.Devicememoryscaling
			}
			if val.Devicesplitcount > 0 {
				config.DeviceSplitCount = uint(val.Devicesplitcount)
			}
		}
	}
	return nil
}

func start() error {
	klog.Info("Loading NVML")
	if err := nvml.Init(); err != nil {
		klog.Infof("Failed to initialize NVML: %v.", err)
		klog.Infof("If this is a GPU node, did you set the docker default runtime to `nvidia`?")
		klog.Infof("You can check the prerequisites at: https://github.com/NVIDIA/k8s-device-plugin#prerequisites")
		klog.Infof("You can learn how to set the runtime at: https://github.com/NVIDIA/k8s-device-plugin#quick-start")
		klog.Infof("If this is not a GPU node, you should set up a toleration or nodeSelector to only deploy this plugin on GPU nodes")
		if failOnInitErrorFlag {
			return fmt.Errorf("failed to initialize NVML: %v", err)
		}
		select {}
	}
	defer func() { klog.Info("Shutdown of NVML returned:", nvml.Shutdown()) }()

	/*Loading config files*/
	fmt.Println("NodeName=", config.NodeName)
	err := readFromConfigFile()
	if err != nil {
		fmt.Printf("failed to load config file %s", err.Error())
	}

	klog.Info("Starting FS watcher.")
	watcher, err := NewFSWatcher(pluginapi.DevicePluginPath)
	if err != nil {
		return fmt.Errorf("failed to create FS watcher: %v", err)
	}
	defer watcher.Close()

	klog.Info("Starting OS watcher.")
	sigs := NewOSWatcher(syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	cache := device_plugin.NewDeviceCache()
	cache.Start()
	defer cache.Stop()

	register := device_plugin.NewDeviceRegister(cache)
	register.Start()
	defer register.Stop()

	var plugins []*device_plugin.NvidiaDevicePlugin
restart:
	// If we are restarting, idempotently stop any running plugins before
	// recreating them below.
	for _, p := range plugins {
		p.Stop()
	}
	klog.Info("Retreiving plugins.")
	migStrategy, err := device_plugin.NewMigStrategy(migStrategyFlag)
	if err != nil {
		return fmt.Errorf("error creating MIG strategy: %v", err)
	}
	plugins = migStrategy.GetPlugins(cache)

	/*plugins = []*device_plugin.NvidiaDevicePlugin{
		device_plugin.NewNvidiaDevicePlugin(
			util.ResourceName,
			cache,
			gpuallocator.NewBestEffortPolicy(),
			pluginapi.DevicePluginPath+"nvidia-gpu.sock"),
	}*/

	// Loop through all plugins, starting them if they have any devices
	// to serve. If even one plugin fails to start properly, try
	// starting them all again.
	started := 0
	pluginStartError := make(chan struct{})
	for _, p := range plugins {
		// Just continue if there are no devices to serve for plugin p.
		if len(p.Devices()) == 0 {
			continue
		}

		// Start the gRPC server for plugin p and connect it with the kubelet.
		if err := p.Start(); err != nil {
			//klog.SetOutput(os.Stderr)
			klog.Info("Could not contact Kubelet, retrying. Did you enable the device plugin feature gate?")
			klog.Info("You can check the prerequisites at: https://github.com/NVIDIA/k8s-device-plugin#prerequisites")
			klog.Info("You can learn how to set the runtime at: https://github.com/NVIDIA/k8s-device-plugin#quick-start")
			close(pluginStartError)
			goto events
		}
		started++
	}

	if started == 0 {
		klog.Info("No devices found. Waiting indefinitely.")
	}

events:
>>>>>>> 6d02e30 (major architect update: remove grpc)
	// Start an infinite loop, waiting for several indicators to either log
	// some messages, trigger a restart of the plugins, or exit the program.
	for {
		select {
<<<<<<< HEAD
		// If the restart timeout has expired, then restart the plugins
		case <-restartTimeout:
			goto restart

			// Detect a kubelet restart by watching for a newly created
			// 'kubeletdevicepluginv1beta1.KubeletSocket' file. When this occurs, restart this loop,
			// restarting all of the plugins in the process.
		case event := <-watcher.Events:
			if o.kubeletSocket != "" && event.Name == o.kubeletSocket && event.Op&fsnotify.Create == fsnotify.Create {
				klog.Infof("inotify: %s created, restarting.", o.kubeletSocket)
=======
		// If there was an error starting any plugins, restart them all.
		case <-pluginStartError:
			goto restart

		// Detect a kubelet restart by watching for a newly created
		// 'pluginapi.KubeletSocket' file. When this occurs, restart this loop,
		// restarting all of the plugins in the process.
		case event := <-watcher.Events:
			if event.Name == pluginapi.KubeletSocket && event.Op&fsnotify.Create == fsnotify.Create {
				klog.Infof("inotify: %s created, restarting.", pluginapi.KubeletSocket)
>>>>>>> 6d02e30 (major architect update: remove grpc)
				goto restart
			}

		// Watch for any other fs errors and log them.
		case err := <-watcher.Errors:
<<<<<<< HEAD
			klog.Errorf("inotify: %s", err)
=======
			klog.Infof("inotify: %s", err)
>>>>>>> 6d02e30 (major architect update: remove grpc)

		// Watch for any signals from the OS. On SIGHUP, restart this loop,
		// restarting all of the plugins in the process. On all other
		// signals, exit the loop and exit the program.
		case s := <-sigs:
			switch s {
			case syscall.SIGHUP:
				klog.Info("Received SIGHUP, restarting.")
				goto restart
			default:
<<<<<<< HEAD
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
	disableResourceRenamingInConfig(config)

	/*Loading config files*/
	//fmt.Println("NodeName=", config.NodeName)
	devConfig, err := generateDeviceConfigFromNvidia(config, c, o.flags)
	if err != nil {
		klog.Errorf("failed to load config file %s", err.Error())
		return nil, false, err
	}

	driverRoot := root(*config.Flags.Plugin.ContainerDriverRoot)
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

	err = validateFlags(infolib, config)
	if err != nil {
		return nil, false, fmt.Errorf("unable to validate flags: %v", err)
	}

	// Update the configuration file with default resources.
	klog.Info("Updating config with default resource matching patterns.")
	err = rm.AddDefaultResourcesToConfig(infolib, nvmllib, devicelib, &devConfig)
	if err != nil {
		return nil, false, fmt.Errorf("unable to add default resources to config: %v", err)
	}

	// Print the config to the output.
	configJSON, err := json.MarshalIndent(devConfig, "", "  ")
	if err != nil {
		return nil, false, fmt.Errorf("failed to marshal config to JSON: %v", err)
	}
	klog.Infof("\nRunning with config:\n%v", string(configJSON))

	// Get the set of plugins.
	klog.Info("Retrieving plugins.")
	plugins, err := GetPlugins(c.Context, infolib, nvmllib, devicelib, &devConfig)
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
	errs := []error{}
	for _, p := range plugins {
		err := p.Stop()
		errs = append(errs, err)
	}
	return errorsutil.NewAggregate(errs)
}

// disableResourceRenamingInConfig temporarily disable the resource renaming feature of the plugin.
// We plan to reeenable this feature in a future release.
func disableResourceRenamingInConfig(config *spec.Config) {
	// Disable resource renaming through config.Resource
	if len(config.Resources.GPUs) > 0 || len(config.Resources.MIGs) > 0 {
		klog.Infof("Customizing the 'resources' field is not yet supported in the config. Ignoring...")
	}
	config.Resources.GPUs = nil
	config.Resources.MIGs = nil

	// Disable renaming / device selection in Sharing.TimeSlicing.Resources
	renameByDefault := config.Sharing.TimeSlicing.RenameByDefault
	setsNonDefaultRename := false
	setsDevices := false
	for i, r := range config.Sharing.TimeSlicing.Resources {
		if !renameByDefault && r.Rename != "" {
			setsNonDefaultRename = true
			config.Sharing.TimeSlicing.Resources[i].Rename = ""
		}
		if renameByDefault && r.Rename != r.Name.DefaultSharedRename() {
			setsNonDefaultRename = true
			config.Sharing.TimeSlicing.Resources[i].Rename = r.Name.DefaultSharedRename()
		}
		if !r.Devices.All {
			setsDevices = true
			config.Sharing.TimeSlicing.Resources[i].Devices.All = true
			config.Sharing.TimeSlicing.Resources[i].Devices.Count = 0
			config.Sharing.TimeSlicing.Resources[i].Devices.List = nil
		}
	}
	if setsNonDefaultRename {
		klog.Warning("Setting the 'rename' field in sharing.timeSlicing.resources is not yet supported in the config. Ignoring...")
	}
	if setsDevices {
		klog.Warning("Customizing the 'devices' field in sharing.timeSlicing.resources is not yet supported in the config. Ignoring...")
=======
				klog.Infof("Received signal %v, shutting down.", s)
				for _, p := range plugins {
					p.Stop()
				}
				break events
			}
		}
	}
	return nil
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		klog.Fatal(err)
>>>>>>> 6d02e30 (major architect update: remove grpc)
	}
}
