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
)

var requiredEnvVars = map[string]bool{
	"HOOK_PATH":     true,
	"OTHER_ENV_VAR": false,
}

func ValidateEnvVars() error {
	for envVar, required := range requiredEnvVars {
		_, exists := os.LookupEnv(envVar)
		if required && !exists {
			return fmt.Errorf("required environment variable %s not set", envVar)
		}
	}
	return nil
}
