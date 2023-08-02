// Copyright 2020 Cambricon, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package mlu

import (
	"log"
	"os"
	"strings"

	"4pd.io/k8s-vgpu/pkg/util"
	flags "github.com/jessevdk/go-flags"
)

type Options struct {
	Mode               string `long:"mode" description:"device plugin mode" default:"default" choice:"default" choice:"sriov" choice:"env-share" choice:"topology-aware" choice:"mlu-share"`
	MLULinkPolicy      string `long:"mlulink-policy" description:"MLULink topology policy" default:"best-effort" choice:"best-effort" choice:"restricted" choice:"guaranteed"`
	VirtualizationNum  uint   `long:"virtualization-num" description:"the virtualization number for each MLU, used only in sriov mode or env-share mode" default:"1" env:"VIRTUALIZATION_NUM"`
	DisableHealthCheck bool   `long:"disable-health-check" description:"disable MLU health check"`
	NodeName           string `long:"node-name" description:"host node name" env:"NODE_NAME"`
	EnableConsole      bool   `long:"enable-console" description:"enable UART console device(/dev/ttyMS) in container"`
	EnableDeviceType   bool   `long:"enable-device-type" description:"enable device registration with type info"`
	CnmonPath          string `long:"cnmon-path" description:"host cnmon path"`
	SocketPath         string `long:"socket-path" description:"socket path for communication between deviceplugin and container runtime"`
}

func ParseFlags() Options {
	for index, arg := range os.Args {
		if strings.HasPrefix(arg, "-mode") {
			os.Args[index] = strings.Replace(arg, "-mode", "--mode", 1)
			break
		}
	}
	if os.Getenv("DP_DISABLE_HEALTHCHECKS") == "all" {
		os.Args = append(os.Args, "--disable-health-check")
	}
	options := Options{}
	parser := flags.NewParser(&options, flags.Default)
	if _, err := parser.Parse(); err != nil {
		code := 1
		if fe, ok := err.(*flags.Error); ok {
			if fe.Type == flags.ErrHelp {
				code = 0
			}
		}
		os.Exit(code)
	}
	util.DeviceSplitCount = &options.VirtualizationNum
	util.RuntimeSocketFlag = options.SocketPath
	log.Printf("Parsed options: %v\n", options)
	return options
}
