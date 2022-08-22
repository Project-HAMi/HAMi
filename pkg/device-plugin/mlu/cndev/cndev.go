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

package cndev

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type pcie struct {
	domain   int
	bus      int
	device   int
	function int
}

type Device struct {
	Slot        uint
	UUID        string
	SN          string
	Path        string
	MotherBoard string
	pcie        *pcie
}

func NewDeviceLite(idx uint, pcieAware bool) (*Device, error) {
	var pcie *pcie

	uuid, sn, motherBoard, path, err := getDeviceInfo(idx)
	if err != nil {
		return nil, err
	}

	if pcieAware {
		pcie, err = getDevicePCIeInfo(idx)
		if err != nil {
			return nil, err
		}
	}

	return &Device{
		Slot:        idx,
		UUID:        uuid,
		SN:          sn,
		Path:        path,
		MotherBoard: motherBoard,
		pcie:        pcie,
	}, nil
}

func (d *Device) GetGeviceHealthState(delayTime int) (int, error) {
	return getDeviceHealthState(d.Slot, delayTime)
}

func (d *Device) GetPCIeID() (string, error) {
	if d.pcie == nil {
		return "", errors.New("device has no PCIe info")
	}
	domain := strconv.FormatInt(int64(d.pcie.domain), 16)
	domain = strings.Repeat("0", 4-len([]byte(domain))) + domain
	bus := strconv.FormatInt(int64(d.pcie.bus), 16)
	if d.pcie.bus < 16 {
		bus = "0" + bus
	}
	device := strconv.FormatInt(int64(d.pcie.device), 16)
	if d.pcie.device < 16 {
		device = "0" + device
	}
	function := strconv.FormatInt(int64(d.pcie.function), 16)
	return domain + ":" + bus + ":" + device + "." + function, nil
}

func (d *Device) EnableSriov(num int) error {
	err := d.ValidateSriovNum(num)
	if err != nil {
		return err
	}
	id, err := d.GetPCIeID()
	if err != nil {
		return err
	}
	path := "/sys/bus/pci/devices/" + id + "/sriov_numvfs"
	vf, err := getNumFromFile(path)
	if err != nil {
		return err
	}
	if vf == num {
		log.Println("sriov already enabled, pass")
		return nil
	}
	if vf != 0 {
		if err = setSriovNum(id, 0); err != nil {
			return fmt.Errorf("failed to set sriov num to 0, pcie: %s now: %d", id, vf)
		}
	}
	return setSriovNum(id, num)
}

func (d *Device) ValidateSriovNum(num int) error {
	id, err := d.GetPCIeID()
	if err != nil {
		return err
	}
	path := "/sys/bus/pci/devices/" + id + "/sriov_totalvfs"
	max, err := getNumFromFile(path)
	if err != nil {
		return err
	}
	if num < 1 || num > max {
		return fmt.Errorf("invalid sriov number %d, maximum: %d, minimum: 1", num, max)
	}
	return nil
}

func getNumFromFile(path string) (int, error) {
	output, err := ioutil.ReadFile(path)
	if err != nil {
		return 0, err
	}
	output = bytes.Trim(output, "\n")
	num, err := strconv.ParseInt(string(output), 10, 64)
	return int(num), err
}

func setSriovNum(id string, num int) error {
	path := "/sys/bus/pci/devices/" + id + "/sriov_numvfs"
	command := "echo " + strconv.Itoa(num) + " > " + path
	err := exec.Command("bash", "-c", command).Run()
	if err != nil {
		return fmt.Errorf("echo %d to file %s, err: %v", num, path, err)
	}
	time.Sleep(time.Second)
	got, err := getNumFromFile(path)
	if err != nil || got != num {
		return fmt.Errorf("the number of VFs is not expected. got: %d, err: %v, expected: %d", got, err, num)
	}
	return nil
}
