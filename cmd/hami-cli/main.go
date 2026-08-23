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
	"github.com/spf13/cobra"
	klog "k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/version"
)

var rootCmd = &cobra.Command{
	Use:   "hami-cli",
	Short: "Read-only inspection tool for HAMi vGPU device allocations",
}

var getCmd = &cobra.Command{
	Use:   "get",
	Short: "Display one or many HAMi resources",
}

func init() {
	rootCmd.AddCommand(getCmd)
	getCmd.AddCommand(allocationsCmd)
	rootCmd.AddCommand(version.VersionCmd)
	rootCmd.PersistentFlags().AddGoFlagSet(util.InitKlogFlags())
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		klog.Fatal(err)
	}
}
