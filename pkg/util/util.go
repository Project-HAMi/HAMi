/*
 * Copyright © 2021 peizhaoyou <peizhaoyou@4paradigm.com>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package util

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"k8s.io/klog/v2"
)

func GlobalFlagSet() *flag.FlagSet {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	fs.StringVar(&ResourceName, "resource-name", "nvidia.com/gpu", "resource name")
	fs.StringVar(&ResourceMem, "resource-mem", "nvidia.com/gpumem", "gpu memory to allocate")
	fs.StringVar(&ResourceMemPercentage, "resource-mem-percentage", "nvidia.com/gpumem-percentage", "gpu memory fraction to allocate")
	fs.StringVar(&ResourceCores, "resource-cores", "nvidia.com/gpucores", "cores percentage to use")
	fs.StringVar(&ResourcePriority, "resource-priority", "vgputaskpriority", "vgpu task priority 0 for high and 1 for low")
	fs.BoolVar(&DebugMode, "debug", false, "debug mode")
	klog.InitFlags(fs)
	return fs
}

func EncodeContainerDevices(cd ContainerDevices) string {
	tmp := ""
	for _, val := range cd {
		tmp += val.UUID + "," + strconv.Itoa(int(val.Usedmem)) + "," + strconv.Itoa(int(val.Usedcores)) + ":"
	}
	fmt.Println("Encoded container Devices=", tmp)
	return tmp
	//return strings.Join(cd, ",")
}

func EncodePodDevices(pd PodDevices) string {
	var ss []string
	for _, cd := range pd {
		ss = append(ss, EncodeContainerDevices(cd))
	}
	return strings.Join(ss, ";")
}

func DecodeContainerDevices(str string) ContainerDevices {
	if len(str) == 0 {
		return ContainerDevices{}
	}
	cd := strings.Split(str, ":")
	contdev := ContainerDevices{}
	tmpdev := ContainerDevice{}
	//fmt.Println("before container device", str)
	if len(str) == 0 {
		return contdev
	}
	for _, val := range cd {
		if strings.Contains(val, ",") {
			//fmt.Println("cd is ", val)
			tmpstr := strings.Split(val, ",")
			tmpdev.UUID = tmpstr[0]
			devmem, _ := strconv.ParseInt(tmpstr[1], 10, 32)
			tmpdev.Usedmem = int32(devmem)
			devcores, _ := strconv.ParseInt(tmpstr[2], 10, 32)
			tmpdev.Usedcores = int32(devcores)
			contdev = append(contdev, tmpdev)
		}
	}
	//fmt.Println("Decoded container device", contdev)
	return contdev
}

func DecodePodDevices(str string) PodDevices {
	if len(str) == 0 {
		return PodDevices{}
	}
	var pd PodDevices
	for _, s := range strings.Split(str, ";") {
		cd := DecodeContainerDevices(s)
		pd = append(pd, cd)
	}
	return pd
}
