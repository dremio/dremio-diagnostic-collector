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

package ttopcollect

import (
	"bufio"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
)

type TtopService interface {
	StartTtop(int, int) error
	KillTtop() (string, error)
}

//go:embed lib/sjk.jar
var f embed.FS

// Ttop provides access to the ttop sjk.jar application
type Ttop struct {
	cmd    *exec.Cmd
	tmpDir string
	output []byte
	mu     sync.Mutex // Mutex to protect concurrent access to p.output
	tmpMu  sync.Mutex //mutext for tmpDir
}

func (t *Ttop) StartTtop(interval, pid int) error {
	if interval == 0 {
		return errors.New("invalid interval of 0 seconds")
	}
	t.tmpMu.Lock()
	defer t.tmpMu.Unlock()
	tmpDir, err := os.MkdirTemp("", "ddc-sjkttop")
	if err != nil {
		return err
	}
	t.tmpDir = tmpDir
	// referencing a part interior to go always use / path
	data, err := fs.ReadFile(f, "lib/sjk.jar")
	if err != nil {
		return err
	}

	sjk := filepath.Join(t.tmpDir, "sjk.jar")
	if err := os.WriteFile(sjk, data, 0600); err != nil {
		return err
	}

	t.cmd = exec.Command("java", "-jar", sjk, "ttop", "-ri", fmt.Sprintf("%vs", interval), "-n", "100", "-p", fmt.Sprintf("%v", pid))

	stdout, err := t.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := t.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	err = t.cmd.Start()
	if err != nil {
		return fmt.Errorf("failed to start command: %w", err)
	}

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			t.mu.Lock()
			t.output = append(t.output, []byte(scanner.Text()+"\n")...)
			t.mu.Unlock()
		}
	}()

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			t.mu.Lock()
			t.output = append(t.output, []byte(scanner.Text()+"\n")...)
			t.mu.Unlock()
		}
	}()
	return nil
}

func (t *Ttop) KillTtop() (string, error) {
	t.tmpMu.Lock()
	defer t.tmpMu.Unlock()
	if err := t.cmd.Process.Kill(); err != nil {
		return "", fmt.Errorf("failed to kill process: %w", err)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.tmpDir == "" {
		return "", errors.New("unable to get data from ttop as it is not yet started")
	}
	if err := os.RemoveAll(t.tmpDir); err != nil {
		simplelog.Warningf("must remove manually directory %v where sjk.jar is installed due to error: '%v'", t.tmpDir, err)
	}
	t.tmpDir = ""
	return string(t.output), nil
}

type TimeTicker interface {
	WaitSeconds(int)
}

type DateTimeTicker struct {
}

func (d *DateTimeTicker) WaitSeconds(interval int) {
	time.Sleep(time.Duration(interval) * time.Second)
}

func RunTtopCollect(c *conf.CollectConf) error {
	simplelog.Debug("Starting ttop collection")
	return OnLoop(c.DremioPID(), c.DremioTtopFreqSeconds(), c.DremioTtopTimeSeconds(), c.TtopOutDir(), &Ttop{}, &DateTimeTicker{})
}

func OnLoop(pid, interval, duration int, outDir string, ttopService TtopService, timeTicker TimeTicker) error {
	err := ttopService.StartTtop(pid, interval)
	if err != nil {
		return fmt.Errorf("unable to start ttop: %w", err)
	}
	times := duration / interval
	for i := 0; i < times; i++ {
		timeTicker.WaitSeconds(interval)
	}
	txt, err := ttopService.KillTtop()
	if err != nil {
		return fmt.Errorf("unable to get text from ttop: %w", err)
	}
	outFile := filepath.Join(outDir, "ttop.txt")
	if err := os.WriteFile(outFile, []byte(txt), 0600); err != nil {
		return fmt.Errorf("unable to write ttop output to file %v due to error: %w", outFile, err)
	}
	return nil
}
