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
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/exp/mmap"
)

const maxDevices = 16

type deviceMemory struct {
	contextSize uint64
	moduleSize  uint64
	bufferSize  uint64
	offset      uint64
	total       uint64
}

type deviceUtilization struct {
	decUtil uint64
	encUtil uint64
	smUtil  uint64
}

type shrregProcSlotT struct {
	pid         int32
	hostpid     int32
	used        [16]deviceMemory
	monitorused [16]uint64
	deviceUtil  [16]deviceUtilization
	status      int32
}

type uuid struct {
	uuid [96]byte
}

type semT struct {
	sem [32]byte
}

type sharedRegionT struct {
	initializedFlag int32
	smInitFlag      int32
	ownerPid        uint32
	sem             semT
	num             uint64
	uuids           [16]uuid

	limit   [16]uint64
	smLimit [16]uint64
	procs   [1024]shrregProcSlotT

	procnum           int32
	utilizationSwitch int32
	recentKernel      int32
	priority          int32
}

type SharedRegionInfoT struct {
	pid          int32
	fd           int32
	initStatus   int16
	sharedRegion sharedRegionT
}

type nvidiaCollector struct {
	// Exposed for testing
	cudevshrPath string
	at           *mmap.ReaderAt
	cudaCache    *sharedRegionT
}

func setProcSlot(offset int64, at *mmap.ReaderAt) (shrregProcSlotT, error) {
	temp := shrregProcSlotT{}
	buff := make([]byte, 4)
	at.ReadAt(buff, offset)
	bytesbuffer := bytes.NewBuffer(buff)
	binary.Read(bytesbuffer, binary.LittleEndian, &temp.pid)
	var monitorused uint64
	//fmt.Println("pid==", temp.pid, "buff=", buff)
	buff = make([]byte, 8)
	for i := 0; i < maxDevices; i++ {
		at.ReadAt(buff, offset+8+8*int64(i))
		bytesbuffer = bytes.NewBuffer(buff)
		binary.Read(bytesbuffer, binary.LittleEndian, &temp.used[i])
	}
	for i := 0; i < maxDevices; i++ {
		at.ReadAt(buff, offset+8+8*16+8*int64(i))
		bytesbuffer = bytes.NewBuffer(buff)
		binary.Read(bytesbuffer, binary.LittleEndian, &monitorused)
		if monitorused > temp.used[i].total {
			temp.used[i].total = monitorused
		}
	}
	//fmt.Println("used=", temp.used)
	return temp, nil
}

func getDeviceUsedMemory(idx int, sharedregion sharedRegionT) (uint64, error) {
	var sum uint64
	sum = 0
	if idx < 0 || idx > 16 {
		return 0, errors.New("out of device idx")
	}
	for _, val := range sharedregion.procs {
		sum += val.used[idx].total
	}
	return sum, nil
}

func mmapcachefile(filename string, nc *nvidiaCollector) error {
	var m = &sharedRegionT{}
	f, err := os.OpenFile(filename, os.O_RDWR, 0666)
	if err != nil {
		fmt.Println("openfile error=", err.Error())
		return err
	}
	data, err := syscall.Mmap(int(f.Fd()), 0, int(unsafe.Sizeof(*m)), syscall.PROT_WRITE|syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return err
	}
	var cachestr *sharedRegionT = *(**sharedRegionT)(unsafe.Pointer(&data))
	fmt.Println("sizeof=", unsafe.Sizeof(*m), "cachestr=", cachestr.utilizationSwitch, cachestr.recentKernel)
	nc.cudaCache = cachestr
	return nil
}

func getvGPUMemoryInfo(nc *nvidiaCollector) (*sharedRegionT, error) {
	if len(nc.cudevshrPath) > 0 {
		if nc.cudaCache == nil {
			mmapcachefile(nc.cudevshrPath, nc)
		}
		return nc.cudaCache, nil
	}
	return &sharedRegionT{}, errors.New("not found path")
}
