/**
 * Copyright 2021 Advanced Micro Devices, Inc.  All rights reserved.
 *
 *  Licensed under the Apache License, Version 2.0 (the "License");
 *  you may not use this file except in compliance with the License.
 *  You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 *  Unless required by applicable law or agreed to in writing, software
 *  distributed under the License is distributed on an "AS IS" BASIS,
 *  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 *  See the License for the specific language governing permissions and
 *  limitations under the License.
**/

// Package hwloc is a collection of utility functions to get NUMA membership
// of AMD GPU via the hwloc library
package hwloc

// #cgo pkg-config: hwloc
// #include <stdint.h>
// #include <hwloc.h>
import "C"
import (
	"fmt"
	"unsafe"
)

func GetVersions() string {
	return fmt.Sprintf("hwloc: _VERSION: %s, _API_VERSION: %#08x, _COMPONENT_ABI: %d, Runtime: %#08x",
		C.HWLOC_VERSION,
		C.HWLOC_API_VERSION,
		C.HWLOC_COMPONENT_ABI,
		uint(C.hwloc_get_api_version()))
}

type Hwloc struct {
	topology C.hwloc_topology_t
}

func (h *Hwloc) Init() error {
	rc := C.hwloc_topology_init(&h.topology)
	if rc != 0 {
		return fmt.Errorf("Problem initializing hwloc topology rc: %d", rc)
	}

	rc = C.hwloc_topology_set_type_filter(h.topology,
		C.HWLOC_OBJ_PCI_DEVICE,
		C.HWLOC_TYPE_FILTER_KEEP_IMPORTANT)
	if rc != 0 {
		C.hwloc_topology_destroy(h.topology)
		return fmt.Errorf("Problem setting type filter rc: %d", rc)
	}

	rc = C.hwloc_topology_load(h.topology)
	if rc != 0 {
		C.hwloc_topology_destroy(h.topology)
		return fmt.Errorf("Problem loading topology rc: %d", rc)
	}

	return nil
}

func (h *Hwloc) Destroy() {
	C.hwloc_topology_destroy(h.topology)
}

func (h *Hwloc) GetNUMANodes(busid string) ([]uint64, error) {
	var gpu C.hwloc_obj_t
	var ancestor C.hwloc_obj_t

	busidstr := C.CString(busid)
	defer C.free(unsafe.Pointer(busidstr))

	gpu = C.hwloc_get_pcidev_by_busidstring(h.topology, busidstr)
	if gpu == nil {
		return []uint64{},
			fmt.Errorf("Fail to find GPU with bus ID: %s", busid)
	}
	ancestor = C.hwloc_get_non_io_ancestor_obj(h.topology, gpu)

	if ancestor == nil || ancestor.memory_arity <= 0 {
		return []uint64{},
			fmt.Errorf("No NUMA node found with bus ID: %s", busid)
	}

	var results []uint64
	nn := ancestor.memory_first_child

	for nn != nil {
		results = append(results, uint64(nn.logical_index))
		nn = nn.next_sibling
	}

	return results, nil
}
