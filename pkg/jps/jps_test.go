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
	"time"

	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/jps"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/shutdown"
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

	// Poll until the JVM has registered itself with hsperfdata and shows up
	// in jps output. There's a short, race-prone window between cmd.Start()
	// and JVM self-registration; in CI we routinely saw the test hit jps
	// before the PID was visible. Polling makes the test deterministic
	// without depending on an arbitrary fixed sleep.
	const (
		maxWait      = 30 * time.Second
		pollInterval = 200 * time.Millisecond
	)
	var (
		flags   string
		err     error
		attempt int
	)
	deadline := time.Now().Add(maxWait)
	for {
		attempt++
		flags, err = jps.CaptureFlagsFromPID(hook, cmd.Process.Pid)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected no error within %v (attempt %d) but got %v", maxWait, attempt, err)
		}
		time.Sleep(pollInterval)
	}
	expected := "-Dmyflag=1 -Xmx512M"
	if !strings.Contains(flags, expected) {
		t.Errorf("expected to contain '%v' in '%v'", expected, flags)
	}
}
