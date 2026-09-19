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

package remotegpu

import (
	"github.com/Project-HAMi/HAMi/pkg/device"
)

// PoolDevice is one card of the lupine fleet as the scheduler's metrics
// endpoint reports it: by the server that owns it, not by the client nodes
// that can reach it.
type PoolDevice struct {
	Server   string
	Endpoint string
	Device   device.DeviceInfo
	// Reserved is true when a pod anywhere in the cluster holds the card, or
	// this scheduler booked it since the last refresh. It is the same answer
	// Fit gets, so the metric agrees with what the next pod would be offered.
	Reserved bool
}

// Inspect reports the fleet as the pool last saw it, without refreshing it.
//
// The per-node usage view hands the whole pool to every client node and only
// counts the pods on that node, so the metrics built from it repeat each card
// once per node and can miss a card taken through another node. This is the
// cluster-wide view instead, read for free: a scrape must not cost an API
// call, and the pool is kept fresh by the register loop anyway.
func (dev *RemoteGPUDevices) Inspect() []PoolDevice {
	return dev.pool.inspect()
}

func (p *pool) inspect() []PoolDevice {
	p.mu.Lock()
	out := make([]PoolDevice, 0, len(p.devices))
	for _, d := range p.devices {
		server := serverOf(d.ID)
		out = append(out, PoolDevice{Server: server, Endpoint: p.endpoints[server], Device: d.DeepCopy()})
	}
	p.mu.Unlock()
	// reserved takes the lock itself; a card changing hands between the two
	// reads is one scrape out of date, which the next scrape corrects.
	for i := range out {
		out[i].Reserved = p.reserved(out[i].Device.ID)
	}
	return out
}
