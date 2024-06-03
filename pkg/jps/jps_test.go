//	Copyright 2023 Dremio Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// package jps_test validates the jps package
package jps_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/pkg/jps"
	"github.com/dremio/dremio-diagnostic-collector/pkg/shutdown"
)

func TestJvmFlagCapture(t *testing.T) {
	jarLoc := filepath.Join("testdata", "demo.jar")
	cmd := exec.Command("java", "-jar", "-Dmyflag=1", "-Xmx512M", jarLoc)
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start() failed with %s\n", err)
	}
	defer func() {
		if err := cmd.Process.Kill(); err != nil {
			t.Fatalf("failed to kill process: %s", err)
		} else {
			t.Log("Process killed successfully.")
		}
	}()
	hook := shutdown.NewHook()
	defer hook.Cleanup()
	flags, err := jps.CaptureFlagsFromPID(hook, cmd.Process.Pid)
	if err != nil {
		t.Fatalf("expected no error but got %v", err)
	}
	expected := "-Dmyflag=1 -Xmx512M"
	if !strings.Contains(flags, expected) {
		t.Errorf("expected to contain '%v' in '%v'", expected, flags)
	}
}
