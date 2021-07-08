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

    "k8s.io/klog/v2"
)

const (
    //ResourceName = "nvidia.com/gpu"
    ResourceName = "4pd.io/vgpu"
    AssignedTimeAnnotations = "4pd.io/vgpu-time"
    AssignedIDsAnnotations = "4pd.io/vgpu-ids"
    AssignedNodeAnnotations = "4pd.io/vgpu-node"

    TimeLayout = "ANSIC"
)

func GlobalFlagSet() *flag.FlagSet {
    fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
    klog.InitFlags(fs)
    return fs
}