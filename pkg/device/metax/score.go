/*
Copyright 2025 The HAMi Authors.

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

package metax

import (
	"fmt"
	"strings"
)

const DirectLinkScore = 10

type LinkDevice struct {
	uuid     string
	linkZone int32
}

func (from *LinkDevice) score(to *LinkDevice) int {
	if from.uuid == to.uuid {
		return 0
	}

	if from.linkZone == 0 || to.linkZone == 0 {
		return 0
	}

	if from.linkZone == to.linkZone {
		return DirectLinkScore
	} else {
		return 0
	}
}

type LinkDevices []*LinkDevice

func (devs LinkDevices) Score() int {
	score := 0

	for i := range devs {
		for j := i + 1; j < len(devs); j++ {
			score += devs[i].score(devs[j])
		}
	}

	return score
}

func (devs LinkDevices) String() string {
	var str strings.Builder
	str.WriteString("[")
	for _, dev := range devs {
		str.WriteString(fmt.Sprintf("%v", *dev))
	}
	str.WriteString("]")

	return str.String()
}
