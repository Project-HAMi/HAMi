/**
 * Copyright 2018 Advanced Micro Devices, Inc.  All rights reserved.
 *
 *  Licensed under the Apache License, Version 2.0 (the "License");
 *  you may not use this file except in compliance with the License.
 *  You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 *  Unless required by applicable law or agreed to in writing, software
 *  distributed under the License is distributed on an "AS IS" BASIS,
 *  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 *  See the License for the specific language governing permissions and
 *  limitations under the License.
**/

package amdgpu

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func hasAMDGPU(t *testing.T) bool {
	devices := GetAMDGPUs()

	if len(devices) <= 0 {
		return false
	}
	return true
}

func TestFirmwareVersionConsistent(t *testing.T) {
	if !hasAMDGPU(t) {
		t.Skip("Skipping test, no AMD GPU found.")
	}

	devices := GetAMDGPUs()

	for pci, dev := range devices {
		card := fmt.Sprintf("card%d", dev["card"])
		t.Logf("%s, %s", pci, card)

		//debugfs path/interface may not be stable
		debugFSfeatVersion, debugFSfwVersion :=
			parseDebugFSFirmwareInfo("/sys/kernel/debug/dri/" + card[4:] + "/amdgpu_firmware_info")
		featVersion, fwVersion, err := GetFirmwareVersions(card)
		if err != nil {
			t.Errorf("Fail to get firmware version %s", err.Error())
		}

		for k := range featVersion {
			if featVersion[k] != debugFSfeatVersion[k] {
				t.Errorf("%s feature version not consistent: ioctl: %d, debugfs: %d",
					k, featVersion[k], debugFSfeatVersion[k])
			}
			if fwVersion[k] != debugFSfwVersion[k] {
				t.Errorf("%s firmware version not consistent: ioctl: %x, debugfs: %x",
					k, fwVersion[k], debugFSfwVersion[k])
			}
		}
	}
}

func TestAMDGPUcountConsistent(t *testing.T) {
	if !hasAMDGPU(t) {
		t.Skip("Skipping test, no AMD GPU found.")
	}

	devices := GetAMDGPUs()

	matches, _ := filepath.Glob("/sys/class/drm/card[0-9]*/device/vendor")

	count := 0
	for _, vidPath := range matches {
		t.Log(vidPath)
		b, err := ioutil.ReadFile(vidPath)
		vid := string(b)

		// AMD vendor ID is 0x1002
		if err == nil && "0x1002" == strings.TrimSpace(vid) {
			count++
		} else {
			t.Log(vid)
		}

	}

	if count != len(devices) {
		t.Errorf("AMD GPU counts differ: /sys/module/amdgpu: %d, /sys/class/drm: %d", len(devices), count)
	}

}

func TestHasAMDGPU(t *testing.T) {
	if !hasAMDGPU(t) {
		t.Skip("Skipping test, no AMD GPU found.")
	}
}

func TestDevFunctional(t *testing.T) {
	if !hasAMDGPU(t) {
		t.Skip("Skipping test, no AMD GPU found.")
	}

	devices := GetAMDGPUs()

	for _, dev := range devices {
		card := fmt.Sprintf("card%d", dev["card"])

		ret := DevFunctional(card)
		t.Logf("%s functional: %t", card, ret)
	}
}

func TestParseTopologyProperties(t *testing.T) {
	var v int64
	var e error
	var re *regexp.Regexp
	var path string

	re = regexp.MustCompile(`size_in_bytes\s(\d+)`)
	path = "../../../testdata/topology-parsing/topology/nodes/1/mem_banks/0/properties"
	v, _ = ParseTopologyProperties(path, re)
	if v != 17163091968 {
		t.Errorf("Error parsing %s for `%s`: expect %d", path, re.String(), 17163091968)
	}

	re = regexp.MustCompile(`flags\s(\d+)`)
	path = "../../../testdata/topology-parsing/topology/nodes/1/mem_banks/0/properties"
	v, _ = ParseTopologyProperties(path, re)
	if v != 0 {
		t.Errorf("Error parsing %s for `%s`: expect %d", path, re.String(), 0)
	}

	re = regexp.MustCompile(`simd_count\s(\d+)`)
	path = "../../../testdata/topology-parsing/topology/nodes/2/properties"
	v, _ = ParseTopologyProperties(path, re)
	if v != 256 {
		t.Errorf("Error parsing %s for `%s`: expect %d", path, re.String(), 256)
	}

	re = regexp.MustCompile(`simd_id_base\s(\d+)`)
	path = "../../../testdata/topology-parsing/topology/nodes/2/properties"
	v, _ = ParseTopologyProperties(path, re)
	if v != 2147487744 {
		t.Errorf("Error parsing %s for `%s`: expect %d", path, re.String(), 2147487744)
	}

	re = regexp.MustCompile(`asdf\s(\d+)`)
	path = "../../../testdata/topology-parsing/topology/nodes/2/properties"
	_, e = ParseTopologyProperties(path, re)
	if e == nil {
		t.Errorf("Error parsing %s for `%s`: expect error", path, re.String())
	}

}
func TestParseDebugFSFirmwareInfo(t *testing.T) {
	expFeat := map[string]uint32{
		"VCE":   0,
		"UVD":   0,
		"MC":    0,
		"ME":    35,
		"PFP":   35,
		"CE":    35,
		"RLC":   0,
		"MEC":   33,
		"MEC2":  33,
		"SOS":   0,
		"ASD":   0,
		"SMC":   0,
		"SDMA0": 40,
		"SDMA1": 40,
	}

	expFw := map[string]uint32{
		"VCE":   0x352d0400,
		"UVD":   0x01571100,
		"MC":    0x00000000,
		"ME":    0x00000094,
		"PFP":   0x000000a4,
		"CE":    0x0000004a,
		"RLC":   0x00000058,
		"MEC":   0x00000160,
		"MEC2":  0x00000160,
		"SOS":   0x00161a92,
		"ASD":   0x0016129a,
		"SMC":   0x001c2800,
		"SDMA0": 0x00000197,
		"SDMA1": 0x00000197,
	}

	feat, fw := parseDebugFSFirmwareInfo("../../../testdata/debugfs-parsing/amdgpu_firmware_info")

	for k := range expFeat {
		val, ok := feat[k]
		if !ok || val != expFeat[k] {
			t.Errorf("Error parsing feature version for %s: expect %d", k, expFeat[k])
		}
	}

	for k := range expFw {
		val, ok := fw[k]
		if !ok || val != expFw[k] {
			t.Errorf("Error parsing firmware version for %s: expect %#08x", k, expFw[k])
		}
	}
	if len(feat) != len(expFeat) || len(fw) != len(expFw) {
		t.Errorf("Incorrect parsing of amdgpu firmware info from debugfs")
	}
}
