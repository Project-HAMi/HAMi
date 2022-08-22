// Copyright 2021 Cambricon, Inc.
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

package cntopo

import (
	"encoding/json"
	"io/ioutil"
	"os/exec"
	"sync"
)

type cntopo struct {
	sync.Mutex
}

type Input map[string][]struct {
	Size      int    `json:"num_devices"`
	WhiteList []uint `json:"white_dev_list"`
}

type Output []struct {
	Info struct {
		Ordinals []uint `json:"ordinal_list"`
	} `json:"info_by_host"`
	// The traffic is duplex, so this value is twice the number of rings,
	// except for the cases of less equal to 2 cards, that is,
	// "A>B>A" conflicts with "B>A>B", while "A>B>C>A" does not conflict with "A>C>B>A"
	NonConflictRings struct {
		Num int `json:"nonconflict_rings_num"`
	} `json:"nonconflict_rings"`
}

type Ring struct {
	Ordinals           []uint
	NonConflictRingNum int
}

type Cntopo interface {
	GetRings(available []uint, size int) ([]Ring, error)
}

func New() Cntopo {
	return &cntopo{}
}

func (c *cntopo) GetRings(available []uint, size int) ([]Ring, error) {
	i := Input{
		"host_list": {
			{
				Size:      size,
				WhiteList: available,
			},
		},
	}
	b, err := json.Marshal(i)
	if err != nil {
		return nil, err
	}
	c.Lock()
	defer c.Unlock()
	err = ioutil.WriteFile("/tmp/cntopo_input.json", b, 0666)
	if err != nil {
		return nil, err
	}
	err = exec.Command("sh", "-c", "cntopo find -I /tmp/cntopo_input.json -O /tmp/cntopo_output.json -R 1000000 -C").Run()
	if err != nil {
		return nil, err
	}
	j, err := ioutil.ReadFile("/tmp/cntopo_output.json")
	if err != nil {
		return nil, err
	}
	var output Output
	err = json.Unmarshal(j, &output)
	if err != nil {
		return nil, err
	}
	rings := []Ring{}
	for _, o := range output {
		rings = append(rings, Ring{
			NonConflictRingNum: o.NonConflictRings.Num,
			Ordinals:           o.Info.Ordinals,
		})
	}
	return rings, nil
}
