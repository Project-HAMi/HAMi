/**
# Copyright (c) NVIDIA CORPORATION.  All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
**/

package dxcore

/*
#cgo LDFLAGS: -Wl,--unresolved-symbols=ignore-in-object-files
#include <dxcore.h>
*/
import "C"
import (
	"fmt"
	"unsafe"
)

type context C.struct_dxcore_context
type adapter C.struct_dxcore_adapter

// initContext initializes the dxcore context and populates the list of adapters.
func initContext() (*context, error) {
	cContext := C.struct_dxcore_context{}
	if C.dxcore_init_context(&cContext) != 0 {
		return nil, fmt.Errorf("failed to initialize dxcore context")
	}
	c := (*context)(&cContext)
	return c, nil
}

// deinitContext deinitializes the dxcore context and frees the list of adapters.
func (c context) deinitContext() {
	cContext := C.struct_dxcore_context(c)
	C.dxcore_deinit_context(&cContext)
}

func (c context) getAdapterCount() int {
	return int(c.adapterCount)
}

func (c context) getAdapter(index int) adapter {
	arrayPointer := (*[1 << 30]C.struct_dxcore_adapter)(unsafe.Pointer(c.adapterList))
	return adapter(arrayPointer[index])
}

func (a adapter) getDriverStorePath() string {
	return C.GoString(a.pDriverStorePath)
}
