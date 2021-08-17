package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"golang.org/x/exp/mmap"
)

const magic = 19920718
const maxDevices = 16

type shrregProcSlotT struct {
	pid    int32
	used   [16]uint64
	status int32
}

type uuid struct {
	uuid [96]byte
}

type sharedRegionT struct {
	initializedFlag int32
	ownerPid        uint32
	sem             uint32
	num             uint64
	uuids           [16]uuid

	limit    [16]uint64
	sm_limit [16]uint64
	procs    [16]shrregProcSlotT
}

type nvidiaCollector struct {
	// Exposed for testing
	cudevshrPath string
	at           *mmap.ReaderAt
}

func setProcSlot(offset int64, at *mmap.ReaderAt) (shrregProcSlotT, error) {
	temp := shrregProcSlotT{}
	buff := make([]byte, 4)
	at.ReadAt(buff, offset)
	bytesbuffer := bytes.NewBuffer(buff)
	binary.Read(bytesbuffer, binary.LittleEndian, &temp.pid)
	//fmt.Println("pid==", temp.pid, "buff=", buff)
	buff = make([]byte, 8)
	for i := 0; i < maxDevices; i++ {
		at.ReadAt(buff, offset+8+8*int64(i))
		bytesbuffer = bytes.NewBuffer(buff)
		binary.Read(bytesbuffer, binary.LittleEndian, &temp.used[i])
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
		sum += val.used[idx]
	}
	return sum, nil
}

func getvGPUMemoryInfo(nc *nvidiaCollector) (sharedRegionT, error) {
	if len(nc.cudevshrPath) > 0 {
		if nc.at == nil {
			//	fmt.Println("path=", nc.cudevshrPath)
			nc.at, _ = mmap.Open(nc.cudevshrPath)
		}
		if nc.at != nil {
			//	fmt.Println("Processing at.....")
			buff := make([]byte, 4)
			sharedregion := sharedRegionT{}
			nc.at.ReadAt(buff, 0)
			bytesbuffer := bytes.NewBuffer(buff)
			binary.Read(bytesbuffer, binary.LittleEndian, &sharedregion.initializedFlag)
			if sharedregion.initializedFlag == 19920718 {
				buff = make([]byte, 8)
				var t uint64
				nc.at.ReadAt(buff, 0x30)
				bytesbuffer = bytes.NewBuffer(buff)
				binary.Read(bytesbuffer, binary.LittleEndian, &t)
				sharedregion.num = t
				for i := 0; i < maxDevices; i++ {
					nc.at.ReadAt(sharedregion.uuids[i].uuid[:], 0x38+96*int64(i))
				}

				for i := 0; i < maxDevices; i++ {
					nc.at.ReadAt(buff, 0x638+int64(i)*8)
					bytesbuffer = bytes.NewBuffer(buff)
					binary.Read(bytesbuffer, binary.LittleEndian, &t)
					sharedregion.limit[i] = t
					//		fmt.Println("limit=", t, "buffer=", buff)
				}
				for i := 0; i < maxDevices; i++ {
					nc.at.ReadAt(buff, 0x6b8+int64(i)*8)
					bytesbuffer = bytes.NewBuffer(buff)
					binary.Read(bytesbuffer, binary.LittleEndian, &t)
					sharedregion.sm_limit[i] = t
				}
				for i := 0; ; i++ {
					sharedregion.procs[i] = shrregProcSlotT{}
					var err error
					sharedregion.procs[i], err = setProcSlot(0x738+0x90*int64(i), nc.at)
					if err != nil {
						fmt.Println(err.Error())
						return sharedRegionT{}, err
					}
					if sharedregion.procs[i].pid == 0 {
						break
					}
				}
			}
			return sharedregion, nil
			//deviceused, err := getDeviceUsedMemory(idx, sharedregion)
			//return sharedregion.limit[idx], deviceused, err
		}
	}
	return sharedRegionT{}, errors.New("not found path")
}
