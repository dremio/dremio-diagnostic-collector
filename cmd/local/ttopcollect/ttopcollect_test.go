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

package ttopcollect_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/ttopcollect"
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

func (m *MockTtopService) StartTtop(pid, interval int) error {
	if m.writeError != nil {
		return m.writeError
	}
	m.interval = interval
	m.started = true
	m.pid = pid
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
	if err := ttopcollect.OnLoop(pid, interval, duration, outDir, ttopService, timeTicker); err != nil {
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

func TestTtopExec(t *testing.T) {
	ttop := &ttopcollect.Ttop{}
	jarLoc := filepath.Join("testdata", "demo.jar")
	cmd := exec.Command("java", "-jar", jarLoc)
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

	if err := ttop.StartTtop(1, cmd.Process.Pid); err != nil {
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
	ttop := &ttopcollect.Ttop{}
	if err := ttop.StartTtop(1, 89899999999); err != nil {
		t.Error("expected an error on ttop but none happened")
	}
	time.Sleep(time.Duration(500) * time.Millisecond)
	if _, err := ttop.KillTtop(); err != nil {
		t.Errorf("we expect ttop to still not return an error with a bad pid: %v", err)
	}
}

func TestTtopHasAndInvalidInterval(t *testing.T) {
	jarLoc := filepath.Join("testdata", "demo.jar")
	cmd := exec.Command("java", "-jar", jarLoc)
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

	ttop := &ttopcollect.Ttop{}
	if err := ttop.StartTtop(0, cmd.Process.Pid); err == nil {
		t.Error("expected ttop start to fail with interval 0")
	}
}
