/*
Copyright 2026 The HAMi Authors.

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

	"github.com/spf13/cobra"

	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
)

// newValidateCommand groups offline checks independently of scheduler startup.
func newValidateCommand() *cobra.Command {
	validate := &cobra.Command{
		Use:   "validate",
		Short: "Validate HAMi configuration without starting the scheduler",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	validate.AddCommand(&cobra.Command{
		Use:   "device-config <path>",
		Short: "Validate a device configuration file using the startup checks",
		Long: "Parse a device configuration file and apply the existing startup checks, " +
			"including backend defaults. No Kubernetes client or scheduler servers are started. " +
			"Existing warning-only conditions do not cause validation to fail.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			cfg, err := config.LoadConfig(path)
			if err != nil {
				return fmt.Errorf("load device config %q: %w", path, err)
			}
			if err := config.InitDevicesWithConfig(cfg); err != nil {
				return fmt.Errorf("validate device config %q: %w", path, err)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Device configuration %q passed startup validation.\n", path)
			return err
		},
	})
	return validate
}
