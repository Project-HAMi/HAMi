/**
# Copyright 2024 NVIDIA CORPORATION
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

package mps

import (
	"context"
	"os"
	"os/exec"
)

// tailer tails the contents of a file.
type tailer struct {
	filename string
	cmd      *exec.Cmd
	cancel   context.CancelFunc
}

// newTailer creates a tailer.
func newTailer(filename string) *tailer {
	return &tailer{
		filename: filename,
	}
}

// Start starts tailing the specified filename.
func (t *tailer) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel

	//nolint:gosec // G204: Subprocess launched with a potential tainted input or cmd arguments (gosec)
	cmd := exec.CommandContext(ctx, "tail", "-n", "+1", "-f", t.filename)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return err
	}
	t.cmd = cmd
	return nil
}

// Stop stops the tailer.
// The associated cancel function is called after which the command wait is
// called -- if applicable.
func (t *tailer) Stop() error {
	if t.cancel != nil {
		t.cancel()
	}

	if t.cmd == nil {
		return nil
	}

	return t.cmd.Wait()
}
