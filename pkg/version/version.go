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

package version

import (
	"bytes"
	"fmt"
	"runtime"
	"strings"
	"text/template"

	"github.com/spf13/cobra"
)

var (
	version  = "v0.0.0-master"
	revision = "unknown" // sha1 from git, output of $(git rev-parse HEAD)

	buildDate = "unknown" // build date in ISO8601 format, output of $(date -u +'%Y-%m-%dT%H:%M:%SZ')
)

// Version information.
type Info struct {
	Version   string `json:"version"`
	Revision  string `json:"revision"`
	BuildDate string `json:"buildDate"`
	GoVersion string `json:"goVersion"`
	Compiler  string `json:"compiler"`
	Platform  string `json:"platform"`
}

// String returns a Go-syntax representation of the Info.
func (info Info) String() string {
	return fmt.Sprintf("%#v", info)
}

// versionInfoTmpl contains the template used by Info.
var versionInfoTmpl = `
version:          {{.version}}
revision:         {{.revision}}
build date:       {{.buildDate}}
go version:       {{.goVersion}}
compiler:         {{.compiler}}
platform:         {{.platform}}
`

func Print() string {
	m := map[string]string{
		"version":   version,
		"revision":  revision,
		"buildDate": buildDate,
		"goVersion": runtime.Version(),
		"compiler":  runtime.Compiler,
		"platform":  fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}
	t := template.Must(template.New("version").Parse(versionInfoTmpl))

	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "version", m); err != nil {
		panic(err)
	}
	return strings.TrimSpace(buf.String())
}

func Version() Info {
	return Info{
		Version:   version,
		Revision:  revision,
		BuildDate: buildDate,
		GoVersion: runtime.Version(),
		Compiler:  runtime.Compiler,
		Platform:  fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}

}

var (
	VersionCmd = &cobra.Command{
		Use:   "version",
		Short: "print version",
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Println(Print())
		},
	}
)
