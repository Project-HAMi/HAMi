/*
* SPDX-License-Identifier: Apache-2.0
*
* The HAMi Contributors require contributions made to
* this file be licensed under the Apache-2.0 license or a
* compatible open source license.
 */

/*
* Licensed to NVIDIA CORPORATION under one or more contributor
* license agreements. See the NOTICE file distributed with
* this work for additional information regarding copyright
* ownership. NVIDIA CORPORATION licenses this file to you under
* the Apache License, Version 2.0 (the "License"); you may
* not use this file except in compliance with the License.
* You may obtain a copy of the License at
*
*     http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing,
* software distributed under the License is distributed on an
* "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
* KIND, either express or implied.  See the License for the
* specific language governing permissions and limitations
* under the License.
 */

/*
* Modifications Copyright The HAMi Authors. See
* GitHub history for details.
 */

package v1

import (
	"fmt"
	"io"
	"os"

	cli "github.com/urfave/cli/v2"

	"sigs.k8s.io/yaml"
)

// Version indicates the version of the 'Config' struct used to hold configuration information.
const Version = "v1"

// Config is a versioned struct used to hold configuration information.
type Config struct {
	Version   string    `json:"version"             yaml:"version"`
	Flags     Flags     `json:"flags,omitempty"     yaml:"flags,omitempty"`
	Resources Resources `json:"resources,omitempty" yaml:"resources,omitempty"`
	Sharing   Sharing   `json:"sharing,omitempty"   yaml:"sharing,omitempty"`
	Imex      Imex      `json:"imex,omitempty"      yaml:"imex,omitempty"`
}

// NewConfig builds out a Config struct from a config file (or command line flags).
// The data stored in the config will be populated in order of precedence from
// (1) command line, (2) environment variable, (3) config file.
func NewConfig(c *cli.Context, flags []cli.Flag) (*Config, error) {
	config := &Config{Version: Version}

	if configFile := c.String("config-file"); configFile != "" {
		var err error
		config, err = parseConfig(configFile)
		if err != nil {
			return nil, fmt.Errorf("unable to parse config file: %v", err)
		}
	}

	config.Flags.UpdateFromCLIFlags(c, flags)
	// TODO: This is currently not at the flags level?
	// Does this mean that we should move UpdateFromCLIFlags to function off Config?
	if c.IsSet("imex-channel-ids") {
		config.Imex.ChannelIDs = c.IntSlice("imex-channel-ids")
	}
	if c.IsSet("imex-required") {
		config.Imex.Required = c.Bool("imex-required")
	}

	// If nvidiaDevRoot (the path to the device nodes on the host) is not set,
	// we default to using the driver root on the host.
	if config.Flags.NvidiaDevRoot == nil || *config.Flags.NvidiaDevRoot == "" {
		config.Flags.NvidiaDevRoot = config.Flags.NvidiaDriverRoot
	}

	// We explicitly set sharing.mps.failRequestsGreaterThanOne = true
	// This can be relaxed in certain cases -- such as a single GPU -- but
	// requires additional logic around when it's OK to combine requests and
	// makes the semantics of a request unclear.
	if config.Sharing.MPS != nil {
		config.Sharing.MPS.FailRequestsGreaterThanOne = true
	}

	return config, nil
}

// logger is used to issue warning in API functions without requiring an explicit implementation.
type logger interface {
	Warning(...interface{})
	Warningf(string, ...interface{})
}

// DisableResourceNamingInConfig temporarily disable the resource renaming feature of the plugin.
// This may be reenabled in a future release.
func DisableResourceNamingInConfig(logger logger, config *Config) {
	// Disable resource renaming through config.Resource
	if len(config.Resources.GPUs) > 0 || len(config.Resources.MIGs) > 0 {
		logger.Warning("Customizing the 'resources' field is not yet supported in the config. Ignoring...")
	}
	config.Resources.GPUs = nil
	config.Resources.MIGs = nil

	// Disable renaming / device selection in Sharing.TimeSlicing.Resources
	config.Sharing.TimeSlicing.disableResoureRenaming(logger, "timeSlicing")
	// Disable renaming / device selection in Sharing.MPS.Resources
	config.Sharing.MPS.disableResoureRenaming(logger, "mps")
}

// parseConfig parses a config file as either YAML of JSON and unmarshals it into a Config struct.
func parseConfig(configFile string) (*Config, error) {
	reader, err := os.Open(configFile)
	if err != nil {
		return nil, fmt.Errorf("error opening config file: %v", err)
	}
	defer reader.Close()

	config, err := parseConfigFrom(reader)
	if err != nil {
		return nil, fmt.Errorf("error parsing config file: %v", err)
	}

	return config, nil
}

func parseConfigFrom(reader io.Reader) (*Config, error) {
	var err error
	var configYaml []byte

	configYaml, err = io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read error: %v", err)
	}

	var config Config
	err = yaml.Unmarshal(configYaml, &config)
	if err != nil {
		return nil, fmt.Errorf("unmarshal error: %v", err)
	}

	if config.Version == "" {
		config.Version = Version
	}

	if config.Version != Version {
		return nil, fmt.Errorf("unknown version: %v", config.Version)
	}

	return &config, nil
}
