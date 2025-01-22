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
	"bytes"
	"flag"
	"strings"
	"testing"

	"github.com/spf13/pflag"
	"github.com/urfave/cli/v2"
	"k8s.io/klog/v2"
)

func TestPrintPFlags(t *testing.T) {
	var buf bytes.Buffer
	klog.SetOutput(&buf)
	klog.LogToStderr(false)
	defer klog.LogToStderr(true)
	tests := []struct {
		name     string
		flags    func() *pflag.FlagSet
		expected string
	}{
		{
			name: "Test with name flags",
			flags: func() *pflag.FlagSet {
				fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
				fs.String("name", "bob", "set name")
				return fs
			},
			expected: `FLAG: --name="bob"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf.Reset()
			PrintPFlags(tt.flags())
			if got := buf.String(); !strings.Contains(got, tt.expected) {
				t.Errorf("PrintPFlags() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestPrintCliFlags(t *testing.T) {
	var buf bytes.Buffer
	klog.SetOutput(&buf)
	klog.LogToStderr(false)
	defer klog.LogToStderr(true)

	tests := []struct {
		name     string
		cliCtx   func() *cli.Context
		expected string
	}{
		{
			name: "Test with name flag",
			cliCtx: func() *cli.Context {
				app := &cli.App{
					Flags: []cli.Flag{
						&cli.StringFlag{
							Name:  "name",
							Value: "bob",
							Usage: "set user name",
						},
					},
				}
				flagSet := flag.NewFlagSet("test", flag.ContinueOnError)
				flagSet.String("name", "bob", "")
				return cli.NewContext(app, flagSet, nil)
			},
			expected: `FLAG: --name="bob"
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf.Reset()
			PrintCliFlags(tt.cliCtx())
			got := buf.String()
			if !strings.Contains(got, tt.expected) {
				t.Errorf("PrintCliFlags() output = %q, want %q", got, tt.expected)
			}
		})
	}
}
