/*
Copyright 2026 The HAMi Authors.

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

package metrics

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"
)

func TestHAMIBuildInfo(t *testing.T) {
	buildInfo := NewBuildInfoCollector()

	want := `
# HELP hami_build_info hami build metadata exposed as labels with a constant value of 1.
# TYPE hami_build_info gauge
hami_build_info{build_date="unknown",compiler="` + runtime.Compiler + `",go_version="` + runtime.Version() + `",platform="` + fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH) + `",revision="unknown",version="v0.0.0-master"} 1
`

	if err := promtestutil.CollectAndCompare(buildInfo, strings.NewReader(want), "hami_build_info"); err != nil {
		t.Fatalf("unexpected collecting result:\n%s", err)
	}

}
