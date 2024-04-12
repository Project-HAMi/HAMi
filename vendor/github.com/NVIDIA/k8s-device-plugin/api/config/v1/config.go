/*
 * Copyright (c) 2022, NVIDIA CORPORATION.  All rights reserved.
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

	return config, nil
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
