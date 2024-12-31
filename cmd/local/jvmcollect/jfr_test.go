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

	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/jvmcollect"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/collects"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/shutdown"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/simplelog"
)

func TestJFRCapture(t *testing.T) {
	logLoc := filepath.Join(t.TempDir(), "ddc.log")

	simplelog.InitLoggerWithFile(logLoc)
	jarLoc := filepath.Join("testdata", "demo.jar")
	cmd := exec.Command("java", "-jar", "-Dmyflag=1", "-Xmx128M", jarLoc)
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start() failed with %s\n", err)
	}
	defer func() {
		err := simplelog.Close()
		if err != nil {
			t.Log(err)
		}
		simplelog.InitLoggerWithFile(filepath.Join(os.TempDir(), "ddc.log"))
	}()

	defer func() {
		// in windows we may need a bit more time to kill the process
		if runtime.GOOS == "windows" {
			time.Sleep(500 * time.Millisecond)
		}
		if err := cmd.Process.Kill(); err != nil {
			t.Fatalf("failed to kill process: %s", err)
		} else {
			t.Log("Process killed successfully.")
		}
	}()
	overrides := make(map[string]string)
	confDir := filepath.Join(t.TempDir(), "ddcyaml")
	if err := os.Mkdir(confDir, 0o700); err != nil {
		t.Fatal(err)
	}
	tmpOutDir := filepath.Join(t.TempDir(), "ddcout")
	if err := os.Mkdir(tmpOutDir, 0o700); err != nil {
		t.Fatal(err)
	}

	nodeName := "node1"
	ddcYamlString := fmt.Sprintf(`
dremio-log-dir: %v
dremio-conf-dir: %v
tarball-out-dir: %v
node-name: %v
dremio-pid: %v
dremio-jfr-time-seconds: 2
`,
		filepath.Join("testdata", "logs"),
		filepath.Join("testdata", "conf"),
		strings.ReplaceAll(tmpOutDir, "\\", "\\\\"),
		nodeName,
		cmd.Process.Pid,
	)
	ddcYaml := filepath.Join(confDir, "ddc.yaml")
	if err := os.WriteFile(ddcYaml, []byte(ddcYamlString), 0o600); err != nil {
		t.Fatal(err)
	}
	hook := shutdown.NewHook()
	defer hook.Cleanup()

	c, err := conf.ReadConf(hook, overrides, ddcYaml, collects.StandardCollection)
	if err != nil {
		t.Fatal(err)
	}

	// create the directory before hand as what happens when we run local-collect
	jfrOutDir := filepath.Join(c.OutputDir(), "jfr")
	if err := os.MkdirAll(jfrOutDir, 0o700); err != nil {
		t.Fatal(err)
	}

	err = jvmcollect.RunCollectJFR(c, hook)
	if err != nil {
		t.Fatalf("expected no error but got %v", err)
	}

	out, err := os.ReadFile(logLoc)
	if err != nil {
		t.Fatalf("unable to read log %v", err)
	}

	if strings.Contains(string(out), "stopped a JFR recording named \"DREMIO_JFR\"") {
		t.Errorf("expected log to NOT have notice that a jfr recording was stopped: '%v'", string(out))
	}
	f, err := os.Stat(filepath.Join(jfrOutDir, fmt.Sprintf("%v.jfr", nodeName)))
	if err != nil {
		t.Fatal(err)
	}
	if f.Size() == 0 {
		t.Errorf("expected a non empty file for the hprof but we got one")
	}
}

func TestJFRCaptureWithExistingJFR(t *testing.T) {
	logLoc := filepath.Join(t.TempDir(), "ddc.log")

	simplelog.InitLoggerWithFile(logLoc)
	defer func() {
		err := simplelog.Close()
		if err != nil {
			t.Log(err)
		}
		simplelog.InitLoggerWithFile(filepath.Join(os.TempDir(), "ddc.log"))
	}()
	jarLoc := filepath.Join("testdata", "demo.jar")
	cmd := exec.Command("java", "-jar", "-Dmyflag=1", "-Xmx128M", jarLoc)
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start() failed with %s\n", err)
	}
	jfrCaptureDir := filepath.Join(t.TempDir(), "ddcout")
	if err := os.Mkdir(jfrCaptureDir, 0o700); err != nil {
		t.Fatal(err)
	}

	jfrOutDir := filepath.Join(jfrCaptureDir, "jfr")
	if err := os.Mkdir(jfrOutDir, 0o700); err != nil {
		t.Fatal(err)
	}
	nodeName := "node1"
	jfrFile := filepath.Join(jfrOutDir, fmt.Sprintf("%v.jfr", nodeName))
	cmd2 := exec.Command("jcmd", fmt.Sprintf("%v", cmd.Process.Pid), "JFR.start", "name=\"DREMIO_JFR\"", fmt.Sprintf("filename=%v", jfrFile))
	if err := cmd2.Start(); err != nil {
		t.Fatalf("cmd2.Start() failed with %s\n", err)
	}
	if err := cmd2.Wait(); err != nil {
		t.Fatalf("cmd2.Wait() failed with %s\n", err)
	}
	defer func() {
		// in windows we may need a bit more time to kill the process
		if runtime.GOOS == "windows" {
			time.Sleep(500 * time.Millisecond)
		}
		if err := cmd.Process.Kill(); err != nil {
			t.Fatalf("failed to kill process: %s", err)
		} else {
			t.Log("Process killed successfully.")
		}
	}()
	overrides := make(map[string]string)
	confDir := filepath.Join(t.TempDir(), "ddcyaml")
	if err := os.Mkdir(confDir, 0o700); err != nil {
		t.Fatal(err)
	}
	tmpOutDir := t.TempDir()
	ddcYamlString := fmt.Sprintf(`
dremio-log-dir: %v
dremio-conf-dir: %v
tarball-out-dir: %v
node-name: %v
dremio-pid: %v
dremio-jfr-time-seconds: 2

`,
		filepath.Join("testdata", "logs"),
		filepath.Join("testdata", "conf"),
		strings.ReplaceAll(tmpOutDir, "\\", "\\\\"),
		nodeName,
		cmd.Process.Pid,
	)
	ddcYaml := filepath.Join(confDir, "ddc.yaml")
	if err := os.WriteFile(ddcYaml, []byte(ddcYamlString), 0o600); err != nil {
		t.Fatal(err)
	}
	hook := shutdown.NewHook()
	defer hook.Cleanup()
	c, err := conf.ReadConf(hook, overrides, ddcYaml, collects.StandardCollection)
	if err != nil {
		t.Fatal(err)
	}

	err = jvmcollect.RunCollectJFR(c, hook)
	if err != nil {
		t.Fatal(err)
	}

	out, err := os.ReadFile(logLoc)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(out), "stopped a JFR recording named \"DREMIO_JFR\"") {
		t.Errorf("expected log to have notice that a jfr recording was stopped: '%v'", string(out))
	}
	f, err := os.Stat(jfrFile)
	if err != nil {
		t.Fatal(err)
	}
	if f.Size() == 0 {
		t.Errorf("expected a non empty file for the hprof but we got one")
	}
}
