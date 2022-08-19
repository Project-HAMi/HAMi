// Copyright 2020 Cambricon, Inc.
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

package cndev

import (
	"unsafe"
)

// #include <dlfcn.h>
// #include "include/cndev.h"
import "C"

type dlhandles struct{ handles []unsafe.Pointer }

var dl dlhandles

// Initialize CNDEV, open a dynamic reference to the CNDEV library in the process.
func (dl *dlhandles) cndevInit() C.cndevRet_t {
	handle := C.dlopen(C.CString("libcndev.so"), C.RTLD_LAZY|C.RTLD_GLOBAL)
	if handle == C.NULL {
		return C.CNDEV_ERROR_UNINITIALIZED
	}
	dl.handles = append(dl.handles, handle)
	return C.cndevInit(C.int(0))
}

// Release CNDEV, close the dynamic reference to the CNDEV library in the process.
func (dl *dlhandles) cndevRelease() C.cndevRet_t {
	ret := C.cndevRelease()
	if ret != C.CNDEV_SUCCESS {
		return ret
	}

	for _, handle := range dl.handles {
		err := C.dlclose(handle)
		if err != 0 {
			return C.CNDEV_ERROR_UNKNOWN
		}
	}
	return C.CNDEV_SUCCESS
}
