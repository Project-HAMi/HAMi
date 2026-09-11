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
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// clientMemoryMetric is reported once per client connection per card the
	// connection touches, so a line existing at all is what matters here: it
	// means something is on that card, whether or not this scheduler put it
	// there.
	clientMemoryMetric = "lupine_client_device_memory_used_bytes"

	// deviceUUIDLabel ties a metric line back to a card in the node's
	// registration annotation.
	deviceUUIDLabel = "device_uuid"

	// metricsTimeout keeps one unresponsive server from holding up a
	// scheduling decision for the whole fleet.
	metricsTimeout = 2 * time.Second
)

// Swappable for tests, like the API listers above.
var fetchBusyDevices = httpBusyDevices

var metricsClient = &http.Client{Timeout: metricsTimeout}

// httpBusyDevices asks one lupine server which of its cards currently have a
// client on them, and returns their GPU UUIDs.
//
// The scheduler's own bookkeeping only covers pods it placed. A card can also
// be busy with something it did not place, an interactive session or a client
// configured by hand, and handing that card to a pod would put two workloads on
// it. The server is the only party that can see those.
func httpBusyDevices(ctx context.Context, endpoint string) (map[string]struct{}, error) {
	url := fmt.Sprintf("http://%s/metrics", endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := metricsClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned %s", url, resp.Status)
	}
	return parseBusyDevices(resp.Body), nil
}

// parseBusyDevices reads the UUID out of every client memory line. Prometheus
// text is line oriented and only one metric is wanted, so it is scanned
// directly rather than pulling in an exposition parser.
func parseBusyDevices(r io.Reader) map[string]struct{} {
	busy := map[string]struct{}{}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, clientMemoryMetric+"{") {
			continue
		}
		if uuid := labelValue(line, deviceUUIDLabel); uuid != "" {
			busy[uuid] = struct{}{}
		}
	}
	return busy
}

// labelValue pulls one label out of a Prometheus sample line.
func labelValue(line, label string) string {
	key := label + `="`
	start := strings.Index(line, key)
	if start < 0 {
		return ""
	}
	rest := line[start+len(key):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return ""
	}
	return rest[:end]
}
