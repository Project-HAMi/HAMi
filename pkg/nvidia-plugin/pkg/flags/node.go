/*
 * Copyright 2023 The Kubernetes Authors.
 * Copyright 2024 NVIDIA CORPORATION.
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

package flags

import (
	"github.com/urfave/cli/v2"
)

type NodeConfig struct {
	Name      string
	Namespace string
}

func (n *NodeConfig) Flags() []cli.Flag {
	flags := []cli.Flag{
		&cli.StringFlag{
			Name:        "namespace",
			Usage:       "The namespace used for the custom resources.",
			Value:       "default",
			Destination: &n.Namespace,
			EnvVars:     []string{"NAMESPACE"},
		},
		&cli.StringFlag{
			Name:        "node-name",
			Usage:       "The name of the node to be worked on.",
			Destination: &n.Name,
			EnvVars:     []string{"NODE_NAME"},
		},
	}
	return flags
}
