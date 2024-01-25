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
