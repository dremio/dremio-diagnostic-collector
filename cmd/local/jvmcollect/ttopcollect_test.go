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
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/jvmcollect"
)

type MockTtopService struct {
	text       string
	interval   int
	killed     bool
	started    bool
	writeError error
	killError  error
	pid        int
}

func (m *MockTtopService) StartTtop(args jvmcollect.TtopArgs) error {
	if m.writeError != nil {
		return m.writeError
	}
	m.interval = args.Interval
	m.started = true
	m.pid = args.PID
	return nil
}

func (m *MockTtopService) KillTtop() (string, error) {
	if m.killError != nil {
		return "", m.killError
	}
	m.killed = true
	return m.text, nil
}

type MockTimeTicker struct {
	waited   int
	interval int
}

func (m *MockTimeTicker) WaitSeconds(interval int) {
	m.interval = interval
	m.waited++
}

func TestTtopCollects(t *testing.T) {
	interval := 1
	duration := 2
	outDir := t.TempDir()
	timeTicker := &MockTimeTicker{}
	ttopService := &MockTtopService{
		text: "ttop file text",
	}
	pid := 1900
	ttopArgs := jvmcollect.TtopArgs{
		PID:      pid,
		Interval: interval,
	}
	if err := jvmcollect.OnLoop(ttopArgs, duration, outDir, ttopService, timeTicker); err != nil {
		t.Fatalf("unable to collect %v", err)
	}

	if pid != ttopService.pid {
		t.Errorf("expected pid %v but was %v", pid, ttopService.pid)
	}

	if timeTicker.interval != 1 {
		t.Errorf("expected interval to be 1 for this test but it was %v", timeTicker.interval)
	}
	if timeTicker.waited != 2 {
		t.Errorf("expected to call Wait 2 times for this test but it was %v", timeTicker.waited)
	}

	if !ttopService.started {
		t.Error("expected ttop to be started it was not")
	}
	if ttopService.interval != interval {
		t.Errorf("expected ttop to have interval %v it was %v", interval, ttopService.interval)
	}

	if !ttopService.killed {
		t.Error("expected ttop to have been killed was not ")
	}

	b, err := os.ReadFile(filepath.Join(outDir, "ttop.txt"))
	if err != nil {
		t.Fatalf("unable to read ttop due to error %v", err)
	}
	if string(b) != ttopService.text {
		t.Errorf("expected %q but was %q", ttopService.text, string(b))
	}
}

func TestTtopCollectsIntegrationTest(t *testing.T) {
	jarLoc := filepath.Join("testdata", "demo.jar")
	cmd := exec.Command("java", "-jar", jarLoc)
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start() failed with %s\n", err)
	}
	defer func() {
		//in windows we may need a bit more time to kill the process
		if runtime.GOOS == "windows" {
			time.Sleep(500 * time.Millisecond)
		}
		if err := cmd.Process.Kill(); err != nil {
			t.Fatalf("failed to kill process: %s", err)
		} else {
			t.Log("Process killed successfully.")
		}
	}()
	pid := cmd.Process.Pid

	tmpDir := t.TempDir()
	ddcYaml := filepath.Join(tmpDir, "ddc.yaml")

	if err := os.WriteFile(ddcYaml, []byte(fmt.Sprintf(`
dremio-pid: %v
dremio-ttop-freq-seconds: 1
dremio-ttop-time-seconds: 2
node-name: node1
tmp-output-dir: %v

`, pid, tmpDir)), 0600); err != nil {
		t.Fatal(err)
	}
	ttopOutDir := filepath.Join(tmpDir, "ttop", "node1")
	if err := os.MkdirAll(ttopOutDir, 0700); err != nil {
		t.Fatal(err)
	}
	overrides := make(map[string]string)
	c, err := conf.ReadConf(overrides, tmpDir)
	if err != nil {
		t.Fatalf("Unable to read conf: %v", err)
	}
	if err := jvmcollect.RunTtopCollect(c); err != nil {
		t.Fatalf("unable to collect %v", err)
	}

	ttopFile := filepath.Join(ttopOutDir, "ttop.txt")
	b, err := os.ReadFile(ttopFile)
	if err != nil {
		t.Error(err)
	}
	txt := string(b)
	expected := "Monitoring threads ..."
	if !strings.Contains(txt, expected) {
		t.Errorf("expected text '%v' to be found in '%v'", expected, txt)
	}
}

func TestTtopExec(t *testing.T) {
	ttop := &jvmcollect.Ttop{}
	jarLoc := filepath.Join("testdata", "demo.jar")
	cmd := exec.Command("java", "-jar", jarLoc)
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start() failed with %s\n", err)
	}

	defer func() {
		//in windows we may need a bit more time to kill the process
		if runtime.GOOS == "windows" {
			time.Sleep(500 * time.Millisecond)
		}
		if err := cmd.Process.Kill(); err != nil {
			t.Fatalf("failed to kill process: %s", err)
		} else {
			t.Log("Process killed successfully.")
		}
	}()

	ttopArgs := jvmcollect.TtopArgs{
		PID:      cmd.Process.Pid,
		Interval: 1,
	}
	if err := ttop.StartTtop(ttopArgs); err != nil {
		t.Error(err.Error())
	}
	time.Sleep(time.Duration(500) * time.Millisecond)
	if text, err := ttop.KillTtop(); err != nil {
		t.Errorf(err.Error())
	} else {
		t.Logf("text for top was `%v`", text)
	}
}

func TestTtopExecHasNoPidToFind(t *testing.T) {
	ttop := &jvmcollect.Ttop{}
	ttopArgs := jvmcollect.TtopArgs{
		PID:      89899999999,
		Interval: 1,
	}
	if err := ttop.StartTtop(ttopArgs); err != nil {
		t.Error("expected an error on ttop but none happened")
	}
	time.Sleep(time.Duration(500) * time.Millisecond)
	if _, err := ttop.KillTtop(); err != nil {
		t.Errorf("we expect ttop to still not return an error with a bad pid: %v", err)
	}
}

func TestTtopExecHasNoPid(t *testing.T) {
	ttop := &jvmcollect.Ttop{}
	ttopArgs := jvmcollect.TtopArgs{
		PID:      -2,
		Interval: 1,
	}
	resp := ttop.StartTtop(ttopArgs)
	time.Sleep(time.Duration(500) * time.Millisecond)
	actual := fmt.Sprint(resp)
	expected := fmt.Sprintf("invalid pid of '%v'", ttopArgs.PID)
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("expected '%v' but was '%v'", expected, actual)
	}

}

func TestTtopHasAndInvalidInterval(t *testing.T) {
	jarLoc := filepath.Join("testdata", "demo.jar")
	cmd := exec.Command("java", "-jar", jarLoc)
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start() failed with %s\n", err)
	}

	defer func() {
		//in windows we may need a bit more time to kill the process
		if runtime.GOOS == "windows" {
			time.Sleep(500 * time.Millisecond)
		}
		if err := cmd.Process.Kill(); err != nil {
			t.Fatalf("failed to kill process: %s", err)
		} else {
			t.Log("Process killed successfully.")
		}
	}()

	ttop := &jvmcollect.Ttop{}
	ttopArgs := jvmcollect.TtopArgs{
		PID:      cmd.Process.Pid,
		Interval: 0,
	}
	if err := ttop.StartTtop(ttopArgs); err == nil {
		t.Error("expected ttop start to fail with interval 0")
	}
}
