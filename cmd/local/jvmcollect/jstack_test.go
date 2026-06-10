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

package jvmcollect_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/local/jvmcollect"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/collects"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/shutdown"
)

func TestJStackCapture(t *testing.T) {
	jarLoc := filepath.Join("testdata", "demo.jar")
	cmd := exec.Command("java", "-jar", "-Dmyflag=1", "-Xmx128M", jarLoc)
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start() failed with %s\n", err)
	}

	defer func() {
		// in windows we may need a bit more time to kill the process
		if runtime.GOOS == "windows" {
			time.Sleep(1 * time.Second)
		}
		if err := cmd.Process.Kill(); err != nil {
			t.Fatalf("failed to kill process: %s", err)
		} else {
			t.Log("Process killed successfully.")
		}
	}()
	overrides := make(map[string]string)
	tmpOutDir := filepath.Join(t.TempDir(), "ddcout")
	if err := os.Mkdir(tmpOutDir, 0o700); err != nil {
		t.Fatal(err)
	}
	nodeName := "node1"

	overrides["dremio-log-dir"] = filepath.Join("testdata", "logs")
	overrides["dremio-conf-dir"] = filepath.Join("testdata", "conf")
	overrides["output-file"] = tmpOutDir
	overrides["node-name"] = nodeName
	overrides["dremio-pid"] = fmt.Sprintf("%v", cmd.Process.Pid)
	overrides["diag-time-seconds"] = "2"
	hook := shutdown.NewHook()
	defer hook.Cleanup()
	c, err := conf.ReadConf(hook, overrides, collects.StandardCollection)
	if err != nil {
		t.Fatal(err)
	}
	threadDumpsOutDir := filepath.Join(c.OutputDir(), "jfr", "thread-dumps", nodeName)
	if err := os.MkdirAll(threadDumpsOutDir, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	counter := 0
	var times []time.Time
	err = jvmcollect.RunJStacksWithTimeService(c, hook, func() time.Time {
		counter++
		current := now.Add(time.Duration(counter) * time.Second)
		times = append(times, current)
		return current
	})
	if err != nil {
		t.Fatalf("expected no error but got %v", err)
	}
	entries, err := os.ReadDir(threadDumpsOutDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 2 {
		t.Fatalf("expected at least 2 thread dumps but got %v", len(entries))
	}

	// Verify first two thread dump files exist and are non-empty
	for i := 0; i < 2; i++ {
		f, err := os.Stat(filepath.Join(threadDumpsOutDir, fmt.Sprintf("threadDump-%s-%s-%d.txt", nodeName, times[i].Format("2006-01-02_15_04_05"), i)))
		if err != nil {
			t.Fatal(err)
		}
		if f.Size() == 0 {
			t.Errorf("expected a non empty file for thread dump %d but we got one", i)
		}
	}
}
