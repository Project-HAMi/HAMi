<<<<<<< HEAD
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
	"os"
	"strings"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/plugin"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/util"

	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
	cli "github.com/urfave/cli/v2"
	"k8s.io/klog/v2"
=======
package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"4pd.io/k8s-vgpu/pkg/util"
	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
	cli "github.com/urfave/cli/v2"
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
)

func addFlags() []cli.Flag {
	addition := []cli.Flag{
		&cli.StringFlag{
			Name:    "node-name",
<<<<<<< HEAD
			Value:   os.Getenv(util.NodeNameEnvName),
=======
			Value:   os.Getenv("NodeName"),
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
			Usage:   "node name",
			EnvVars: []string{"NodeName"},
		},
		&cli.UintFlag{
			Name:    "device-split-count",
			Value:   2,
			Usage:   "the number for NVIDIA device split",
			EnvVars: []string{"DEVICE_SPLIT_COUNT"},
		},
		&cli.Float64Flag{
			Name:    "device-memory-scaling",
			Value:   1.0,
			Usage:   "the ratio for NVIDIA device memory scaling",
			EnvVars: []string{"DEVICE_MEMORY_SCALING"},
		},
		&cli.Float64Flag{
			Name:    "device-cores-scaling",
			Value:   1.0,
			Usage:   "the ratio for NVIDIA device cores scaling",
			EnvVars: []string{"DEVICE_CORES_SCALING"},
		},
		&cli.BoolFlag{
			Name:    "disable-core-limit",
			Value:   false,
			Usage:   "If set, the core utilization limit will be ignored",
			EnvVars: []string{"DISABLE_CORE_LIMIT"},
		},
		&cli.StringFlag{
			Name:  "resource-name",
			Value: "nvidia.com/gpu",
			Usage: "the name of field for number GPU visible in container",
		},
	}
	return addition
}

<<<<<<< HEAD
// prt returns a reference to whatever type is passed into it.
=======
// prt returns a reference to whatever type is passed into it
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
func ptr[T any](x T) *T {
	return &x
}

<<<<<<< HEAD
// updateFromCLIFlag conditionally updates the config flag at 'pflag' to the value of the CLI flag with name 'flagName'.
=======
// updateFromCLIFlag conditionally updates the config flag at 'pflag' to the value of the CLI flag with name 'flagName'
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
func updateFromCLIFlag[T any](pflag **T, c *cli.Context, flagName string) {
	if c.IsSet(flagName) || *pflag == (*T)(nil) {
		switch flag := any(pflag).(type) {
		case **string:
			*flag = ptr(c.String(flagName))
		case **[]string:
			*flag = ptr(c.StringSlice(flagName))
		case **bool:
			*flag = ptr(c.Bool(flagName))
		case **float64:
			*flag = ptr(c.Float64(flagName))
		case **uint:
			*flag = ptr(c.Uint(flagName))
		default:
			panic(fmt.Errorf("unsupported flag type for %v: %T", flagName, flag))
		}
	}
}

<<<<<<< HEAD
func generateDeviceConfigFromNvidia(cfg *spec.Config, c *cli.Context, flags []cli.Flag) (nvidia.DeviceConfig, error) {
	devcfg := nvidia.DeviceConfig{}
	devcfg.Config = cfg

	klog.Infoln("flags=", flags)
	for _, flag := range flags {
		for _, n := range flag.Names() {
			// Common flags
			if strings.Compare(n, "config-file") == 0 {
				updateFromCLIFlag(&plugin.ConfigFile, c, n)
			}
		}
	}

	config, err := config.LoadConfig(*plugin.ConfigFile)
	if err != nil {
		klog.Fatalf("failed to load ascend vnpu config file %s: %v", *plugin.ConfigFile, err)
	}
	devcfg.ResourceName = &config.NvidiaConfig.ResourceCountName
	klog.Infoln("reading config=", config.NvidiaConfig.ResourceCountName, "devcfg", *devcfg.ResourceName, "configfile=", *plugin.ConfigFile)
=======
func readFromConfigFile() error {
	jsonbyte, err := ioutil.ReadFile("/config/config.json")
	if err != nil {
		return err
	}
	var deviceConfigs util.DevicePluginConfigs
	err = json.Unmarshal(jsonbyte, &deviceConfigs)
	if err != nil {
		return err
	}
	fmt.Println("json=", deviceConfigs)
	for _, val := range deviceConfigs.Nodeconfig {
		if strings.Compare(os.Getenv("NodeName"), val.Name) == 0 {
			fmt.Println("Reading config from file", val.Name)
			if val.Devicememoryscaling > 0 {
				util.DeviceMemoryScaling = &val.Devicememoryscaling
			}
			if val.Devicecorescaling > 0 {
				util.DeviceCoresScaling = &val.Devicecorescaling
			}
			if val.Devicesplitcount > 0 {
				util.DeviceSplitCount = &val.Devicesplitcount
			}
		}
	}
	return nil
}

func generateDeviceConfigFromNvidia(cfg *spec.Config, c *cli.Context, flags []cli.Flag) (util.DeviceConfig, error) {
	devcfg := util.DeviceConfig{}
	devcfg.Config = cfg

	for _, flag := range flags {
		for _, n := range flag.Names() {
			// Common flags
			switch n {
			case "device-split-count":
				updateFromCLIFlag(&util.DeviceSplitCount, c, n)
			case "device-memory-scaling":
				updateFromCLIFlag(&util.DeviceMemoryScaling, c, n)
			case "device-cores-scaling":
				updateFromCLIFlag(&util.DeviceCoresScaling, c, n)
			case "disable-core-limit":
				updateFromCLIFlag(&util.DisableCoreLimit, c, n)
			case "resource-name":
				updateFromCLIFlag(&devcfg.ResourceName, c, n)
			}
		}
	}
	readFromConfigFile()
	util.NodeName = os.Getenv("NodeName")
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
	return devcfg, nil
}
