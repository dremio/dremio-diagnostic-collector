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
	"strings"
	"testing"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/jvmcollect"
)

func TestJStackCapture(t *testing.T) {
	jarLoc := filepath.Join("testdata", "demo.jar")
	cmd := exec.Command("java", "-jar", "-Dmyflag=1", "-Xmx128M", jarLoc)
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start() failed with %s\n", err)
	}

	defer func() {
		//in windows we may need a bit more time to kill the process
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
	confDir := filepath.Join(t.TempDir(), "ddcyaml")
	if err := os.Mkdir(confDir, 0700); err != nil {
		t.Fatal(err)
	}
	tmpOutDir := filepath.Join(t.TempDir(), "ddcout")
	if err := os.Mkdir(tmpOutDir, 0700); err != nil {
		t.Fatal(err)
	}
	nodeName := "node1"
	threadDumpsOutDir := filepath.Join(tmpOutDir, "jfr", "thread-dumps", nodeName)
	if err := os.MkdirAll(threadDumpsOutDir, 0700); err != nil {
		t.Fatal(err)
	}
	ddcYamlString := fmt.Sprintf(`
dremio-log-dir: %v
dremio-conf-dir: %v
tmp-output-dir: %v
node-name: %v
dremio-pid: %v
dremio-jstack-time-seconds: 2
dremio-jstack-freq-seconds: 1
`, filepath.Join("testdata", "logs"), filepath.Join("testdata", "conf"), strings.ReplaceAll(tmpOutDir, "\\", "\\\\"),
		nodeName,
		cmd.Process.Pid,
	)
	ddcYaml := filepath.Join(confDir, "ddc.yaml")
	if err := os.WriteFile(ddcYaml, []byte(ddcYamlString), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := conf.ReadConf(overrides, ddcYaml)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	counter := 0
	var times []time.Time
	err = jvmcollect.RunCollectJStacksWithTimeService(c, func() time.Time {
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
	if len(entries) != 2 {
		t.Fatalf("expected 2 but got %v", len(entries))
	}

	f, err := os.Stat(filepath.Join(threadDumpsOutDir, fmt.Sprintf("threadDump-%s-%s.txt", nodeName, times[0].Format("2006-01-02_15_04_05"))))
	if err != nil {
		t.Fatal(err)
	}
	if f.Size() == 0 {
		t.Errorf("expected a non empty file for the hprof but we got one")
	}

	f, err = os.Stat(filepath.Join(threadDumpsOutDir, fmt.Sprintf("threadDump-%s-%s.txt", nodeName, times[1].Format("2006-01-02_15_04_05"))))
	if err != nil {
		t.Fatal(err)
	}
	if f.Size() == 0 {
		t.Errorf("expected a non empty file for the hprof but we got one")
	}
}
