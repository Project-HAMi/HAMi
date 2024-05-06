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
	"fmt"
	"strconv"
)

func initCoreUsage(req int) string {
	res := ""
	i := 0
	for i < req/4 {
		res = res + "0"
		i++
	}
	return res
}

func addCoreUsage(tot string, c string) (string, error) {
	i := 0
	res := ""
	for {
		left := int64(0)
		right := int64(0)
		if i < len(tot) && tot[i] != 0 {
			left, _ = strconv.ParseInt(string(tot[i]), 16, 0)
			right, _ = strconv.ParseInt(string(c[i]), 16, 0)
			merged := int(left | right)
			res = fmt.Sprintf("%s%x", res, merged)
		} else {
			break
		}
		i++
	}
	fmt.Println("tot=", tot, "c=", c, "res=", res)
	return res, nil
}

func byteAlloc(b int, req int) (int, int) {
	if req == 0 {
		return 0, 0
	}
	remains := req
	leftstr := fmt.Sprintf("%b", b)
	for len(leftstr) < 4 {
		leftstr = "0" + leftstr
	}
	res := 0
	i := 0
	for i < len(leftstr) {
		res = res * 2
		if leftstr[i] == '0' && remains > 0 {
			remains--
			res = res + 1
		}
		i++
	}
	return res, remains
}

func allocCoreUsage(tot string, req int) (string, error) {
	i := 0
	res := ""
	remains := req
	for {
		left := int64(0)
		alloc := 0
		if i < len(tot) && tot[i] != 0 {
			left, _ = strconv.ParseInt(string(tot[i]), 16, 0)
			alloc, remains = byteAlloc(int(left), remains)
			res = fmt.Sprintf("%s%x", res, alloc)
		} else {
			break
		}
		i++
	}
	return res, nil
}
