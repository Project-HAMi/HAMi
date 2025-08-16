/*
 * SPDX-License-Identifier: Apache-2.0
 *
 * The HAMi Contributors require contributions made to
 * this file be licensed under the Apache-2.0 license or a
 * compatible open source license.
 */

/*
 * Licensed to NVIDIA CORPORATION under one or more contributor
 * license agreements. See the NOTICE file distributed with
 * this work for additional information regarding copyright
 * ownership. NVIDIA CORPORATION licenses this file to you under
 * the Apache License, Version 2.0 (the "License"); you may
 * not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

/*
 * Modifications Copyright The HAMi Authors. See
 * GitHub history for details.
 */

package plugin

import "testing"

func Test_parseNvidiaNumaInfo(t *testing.T) {

	tests := []struct {
		name          string
		idx           int
		nvidiaTopoStr string
		want          int
		wantErr       bool
	}{
		{
			name: "single Tesla P4 NUMA",
			idx:  0,
			nvidiaTopoStr: `GPU0    CPU Affinity    NUMA Affinity ...
                            ...`,
			want:    0,
			wantErr: false,
		},
		{
			name: "two Tesla P4 NUMA topo with index 0",
			idx:  0,
			nvidiaTopoStr: `GPU0    GPU1    CPU Affinity    NUMA Affinity ...
                            ...`,
			want:    0,
			wantErr: false,
		},
		{
			name: "two Tesla P4 NUMA topo with index 1",
			idx:  1,
			nvidiaTopoStr: `GPU0    GPU1    CPU Affinity    NUMA Affinity ...
                            ...`,
			want:    0,
			wantErr: false,
		},
		{
			name: "NUMA Affinity is empty",
			idx:  0,
			nvidiaTopoStr: `GPU0	CPU Affinity	NUMA Affinity	GPU NUMA ID
GPU0	X`,
			want:    0,
			wantErr: false,
		},
		{
			name: "multi-gpu topo with index 4",
			idx:  4,
			nvidiaTopoStr: `	GPU0	GPU1	GPU2	GPU3	GPU4	GPU5	GPU6	GPU7	CPU Affinity	NUMA Affinity	GPU NUMA ID
GPU0	X	NV18	NV18	NV18	NV18	NV18	NV18	NV18	0-191	0	N/A
GPU1	NV18	X	NV18	NV18	NV18	NV18	NV18	NV18	0-191	0	N/A
GPU2	NV18	NV18	X	NV18	NV18	NV18	NV18	NV18	0-191	0	N/A
GPU3	NV18	NV18	NV18	X	NV18	NV18	NV18	NV18	0-191	0	N/A
GPU4	NV18	NV18	NV18	NV18	X	NV18	NV18	NV18	192-383	1	N/A
GPU5	NV18	NV18	NV18	NV18	NV18	X	NV18	NV18	192-383	1	N/A
GPU6	NV18	NV18	NV18	NV18	NV18	NV18	X	NV18	192-383	1	N/A
GPU7	NV18	NV18	NV18	NV18	NV18	NV18	NV18	X	192-383	1	N/A

Legend:

  X    = Self
  SYS  = Connection traversing PCIe as well as the SMP interconnect between NUMA nodes (e.g., QPI/UPI)
  NODE = Connection traversing PCIe as well as the interconnect between PCIe Host Bridges within a NUMA node
  PHB  = Connection traversing PCIe as well as a PCIe Host Bridge (typically the CPU)
  PXB  = Connection traversing multiple PCIe bridges (without traversing the PCIe Host Bridge)
  PIX  = Connection traversing at most a single PCIe bridge
  NV#  = Connection traversing a bonded set of # NVLinks`,
			want:    1,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseNvidiaNumaInfo(tt.idx, tt.nvidiaTopoStr)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseNvidiaNumaInfo() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseNvidiaNumaInfo() got = %v, want %v", got, tt.want)
			}
		})
	}
}
