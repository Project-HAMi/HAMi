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
    "os"
    "strings"

    "k8s.io/klog/v2"
)

func GlobalFlagSet() *flag.FlagSet {
    fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
    fs.StringVar(&ResourceName, "resource-name", "nvidia.com/gpu", "resource name")
    fs.BoolVar(&DebugMode, "debug", false, "debug mode")
    klog.InitFlags(fs)
    return fs
}

func EncodeContainerDevices(cd ContainerDevices) string {
    return strings.Join(cd, ",")
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
    return strings.Split(str, ",")
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
