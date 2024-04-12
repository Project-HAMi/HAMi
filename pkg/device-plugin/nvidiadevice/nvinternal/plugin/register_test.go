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
