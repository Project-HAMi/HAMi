/**
# Copyright 2024 NVIDIA CORPORATION
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
**/

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/urfave/cli/v2"
	"k8s.io/klog/v2"

	"github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	nvinfo "github.com/NVIDIA/go-nvlib/pkg/nvlib/info"
	"github.com/NVIDIA/go-nvml/pkg/nvml"

	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/mps-control-daemon/mount"
	"github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/mps-control-daemon/mps"
	"github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/pkg/info"
	"github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/pkg/logger"
	"github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/pkg/rm"
	"github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/pkg/watch"

	spec "github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/api/config/v1"
)

// Config represents a collection of config options for the device plugin.
type Config struct {
	configFile string

	// flags stores the CLI flags for later processing.
	flags []cli.Flag
}

func main() {
	config := &Config{}

	c := cli.NewApp()
	c.Name = "NVIDIA MPS Control Daemon"
	c.Version = info.GetVersionString()
	c.Action = func(ctx *cli.Context) error {
		return start(ctx, config)
	}
	c.Commands = []*cli.Command{
		mount.NewCommand(),
	}

	config.flags = []cli.Flag{
		&cli.StringFlag{
			Name:        "config-file",
			Usage:       "the path to a config file as an alternative to command line options or environment variables",
			Destination: &config.configFile,
			EnvVars:     []string{"CONFIG_FILE"},
		},
		&cli.StringFlag{
			Name:    "mig-strategy",
			Value:   spec.MigStrategyNone,
			Usage:   "the desired strategy for exposing MIG devices on GPUs that support it:\n\t\t[none | single | mixed]",
			EnvVars: []string{"MIG_STRATEGY"},
		},
	}
	c.Flags = config.flags

	klog.InfoS(c.Name, "version", c.Version)
	err := c.Run(os.Args)
	if err != nil {
		klog.Error(err)
		os.Exit(1)
	}
}

// TODO: This needs to do similar validation to the plugin.
func validateFlags(config *spec.Config) error {
	return nil
}

// loadConfig loads the config from the spec file.
func (cfg *Config) loadConfig(c *cli.Context) (*spec.Config, error) {
	config, err := spec.NewConfig(c, cfg.flags)
	if err != nil {
		return nil, fmt.Errorf("unable to finalize config: %w", err)
	}
	err = validateFlags(config)
	if err != nil {
		return nil, fmt.Errorf("unable to validate flags: %w", err)
	}
	config.Flags.GFD = nil

	return config, nil
}

// loadConfig loads the config from the spec file.
func (cfg *Config) loadNvidiaConfig(c *cli.Context) (*nvidia.DeviceConfig, error) {
	devcfg := &nvidia.DeviceConfig{}

	config, err := spec.NewConfig(c, cfg.flags)
	if err != nil {
		return nil, fmt.Errorf("unable to finalize config: %w", err)
	}
	err = validateFlags(config)
	if err != nil {
		return nil, fmt.Errorf("unable to validate flags: %w", err)
	}
	config.Flags.GFD = nil
	// Set the config in the device config.
	devcfg.Config = config
	return devcfg, nil
}

func start(c *cli.Context, cfg *Config) error {
	klog.Info("Starting OS watcher.")
	sigs := watch.Signals(syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	var started bool
	var restartTimeout <-chan time.Time
	var daemons []*mps.Daemon
restart:
	// If we are restarting, stop daemons from previous run.
	if started {
		err := stopDaemons(daemons...)
		if err != nil {
			return fmt.Errorf("error stopping plugins from previous run: %v", err)
		}
	}

	klog.Info("Starting Daemons.")
	daemons, restartDaemons, err := startDaemons(c, cfg)
	if err != nil {
		return fmt.Errorf("error starting plugins: %v", err)
	}
	started = true

	if restartDaemons {
		klog.Infof("Failed to start one or more MPS deamons. Retrying in 30s...")
		restartTimeout = time.After(30 * time.Second)
	}

	// Start an infinite loop, waiting for several indicators to either log
	// some messages, trigger a restart of the plugins, or exit the program.
	for {
		select {
		// If the restart timeout has expired, then restart the plugins
		case <-restartTimeout:
			goto restart

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
	if err := stopDaemons(daemons...); err != nil {
		return fmt.Errorf("error stopping daemons: %v", err)
	}
	return nil
}

func startDaemons(c *cli.Context, cfg *Config) ([]*mps.Daemon, bool, error) {
	// Load the configuration file
	klog.Info("Loading configuration.")
	config, err := cfg.loadNvidiaConfig(c)
	if err != nil {
		return nil, false, fmt.Errorf("unable to load config: %v", err)
	}
	spec.DisableResourceNamingInConfig(logger.ToKlog, config.Config)

	nvmllib := nvml.New()
	devicelib := device.New(nvmllib)
	infolib := nvinfo.New(
		nvinfo.WithNvmlLib(nvmllib),
		nvinfo.WithDeviceLib(devicelib),
	)

	// Update the configuration file with default resources.
	klog.Info("Updating config with default resource matching patterns.")
	err = rm.AddDefaultResourcesToConfig(infolib, nvmllib, devicelib, config.Config)
	if err != nil {
		return nil, false, fmt.Errorf("unable to add default resources to config: %v", err)
	}

	// Print the config to the output.
	configJSON, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, false, fmt.Errorf("failed to marshal config to JSON: %v", err)
	}
	klog.Infof("\nRunning with config:\n%v", string(configJSON))

	// Get the set of daemons.
	// Note that a daemon is only created for resources with at least one device.
	klog.Info("Retrieving MPS daemons.")
	mpsDaemons, err := mps.NewDaemons(infolib, nvmllib, devicelib,
		mps.WithConfig(config),
	)
	if err != nil {
		return nil, false, fmt.Errorf("error getting daemons: %v", err)
	}

	if len(mpsDaemons) == 0 {
		klog.Info("No devices are configured for MPS sharing; Waiting indefinitely.")
	}

	// Loop through all MPS daemons and start them.
	// If any daemon fails to start, all daemons are started again.
	for _, mpsDaemon := range mpsDaemons {
		if err := mpsDaemon.Start(); err != nil {
			klog.Errorf("Failed to start MPS daemon: %v", err)
			return mpsDaemons, true, nil
		}
	}
	readyFile, err := os.Create("/mps/.ready")
	if err != nil {
		return mpsDaemons, true, fmt.Errorf("failed to create .ready file")
	}
	defer readyFile.Close()

	return mpsDaemons, false, nil
}

func stopDaemons(mpsDaemons ...*mps.Daemon) error {
	if err := os.Remove("/mps/.ready"); err != nil {
		klog.Warningf("Failed to remove .ready file: %v", err)
	}
	klog.Info("Stopping MPS daemons.")
	var errs error
	for _, p := range mpsDaemons {
		errs = errors.Join(errs, p.Stop())
	}
	return errs
}
