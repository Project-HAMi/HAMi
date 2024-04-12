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

package dcu

import (
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

func TestInit(t *testing.T) {
	str := initCoreUsage(60)
	t.Log("str=", str)
	assert.Equal(t, strings.Compare(str, "000000000000000"), 0)
}

func TestAddCoreUsage(t *testing.T) {
	str := initCoreUsage(60)
	str1 := "abcde000ad00012"
	res, _ := addCoreUsage(str, str1)
	t.Log("res1=", res)
	assert.Equal(t, strings.Compare(res, str1), 0)
	str1 = "50200fff4000000"
	res, _ = addCoreUsage(res, str1)
	t.Log("res1=", res)
	assert.Equal(t, strings.Compare(res, "fbedefffed00012"), 0)
}

func TestAllocCoreUsage(t *testing.T) {
	str1 := "50200fff4000000"
	res, _ := allocCoreUsage(str1, 16)
	t.Log("res=", res)
	assert.Equal(t, strings.Compare(res, "afdfe0000000000"), 0)
	str1 = "abcde000ad00012"
	res, _ = allocCoreUsage(str1, 32)
	t.Log("res=", res)
}
