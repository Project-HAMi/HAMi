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

package flag

import (
	"github.com/spf13/pflag"
	"github.com/urfave/cli/v2"
	"k8s.io/klog/v2"
)

func PrintPFlags(flags *pflag.FlagSet) {
	flags.VisitAll(func(flag *pflag.Flag) {
		klog.Infof("FLAG: --%s=%q", flag.Name, flag.Value)
	})
}

func PrintCliFlags(c *cli.Context) {
	for _, flag := range c.App.Flags {
		names := flag.Names()
		for _, name := range names {
			value := c.Generic(name)
			klog.Infof("FLAG: --%s=%q\n", name, value)
		}

	}
}
