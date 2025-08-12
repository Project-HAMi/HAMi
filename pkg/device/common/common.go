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

package common

import (
	"fmt"
	"strings"
)

const (
	CardTypeMismatch                  = "CardTypeMismatch"
	CardUUIDMismatch                  = "CardUuidMismatch"
	CardTimeSlicingExhausted          = "CardTimeSlicingExhausted"
	CardComputeUnitsExhausted         = "CardComputeUnitsExhausted"
	CardInsufficientMemory            = "CardInsufficientMemory"
	CardInsufficientCore              = "CardInsufficientCore"
	NumaNotFit                        = "NumaNotFit"
	ExclusiveDeviceAllocateConflict   = "ExclusiveDeviceAllocateConflict"
	CardNotFoundCustomFilterRule      = "CardNotFoundCustomFilterRule"
	NodeInsufficientDevice            = "NodeInsufficientDevice"
	AllocatedCardsInsufficientRequest = "AllocatedCardsInsufficientRequest"
	NodeUnfitPod                      = "NodeUnfitPod"
	NodeFitPod                        = "NodeFitPod"
)

func GenReason(reasons map[string]int, cards int) string {
	var reason []string
	for r, cnt := range reasons {
		reason = append(reason, fmt.Sprintf("%d/%d %s", cnt, cards, r))
	}
	return strings.Join(reason, ", ")
}
