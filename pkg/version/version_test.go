package version

import (
	"bytes"
	"io"
	"os"
	"testing"

	"gotest.tools/v3/assert"
)


func TestVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    string
		wantErr error
	}{
		{
			name:    "check version v1.0.0.1234567890",
			version: "v1.0.0.1234567890",
			want:    "v1.0.0.1234567890\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			version = test.version

			var out bytes.Buffer
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatalf("os.Pipe() failed: %v", err)
			}
			defer r.Close()
			originalStdout := os.Stdout
			defer func() {
				os.Stdout = originalStdout
				w.Close()
			}()
			os.Stdout = w

			VersionCmd.Run(nil, nil)
			w.Close()

			io.Copy(&out, r)

			versionGet := out.String()
			assert.Equal(t, test.want, versionGet)
		})
	}
}
