// Copyright 2023 Dremio Corporation
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

package collection

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/crgimenes/native/power"

	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/shutdown"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/simplelog"
)

// captureLog redirects ddc.log into a temp dir and returns a reader for it.
func captureLog(t *testing.T) func() string {
	t.Helper()
	dir := t.TempDir()
	simplelog.InitLoggerWithOutputDir(dir)
	t.Cleanup(func() { _ = simplelog.Close(); simplelog.InitLogger() })
	return func() string {
		b, err := os.ReadFile(filepath.Join(dir, "ddc.log"))
		if err != nil {
			t.Fatalf("read ddc.log: %v", err)
		}
		return string(b)
	}
}

func swapKeepAwake(t *testing.T, f func(string) (func() error, error)) {
	t.Helper()
	orig := keepAwake
	keepAwake = f
	t.Cleanup(func() { keepAwake = orig })
}

func TestStartKeepAwake_AcquiresAndReleases(t *testing.T) {
	readLog := captureLog(t)
	var gotReason string
	releases := 0
	swapKeepAwake(t, func(reason string) (func() error, error) {
		gotReason = reason
		return func() error { releases++; return nil }, nil
	})

	stop := startKeepAwake()
	if gotReason != keepAwakeReason {
		t.Errorf("reason = %q, want %q", gotReason, keepAwakeReason)
	}
	stop()
	if releases != 1 {
		t.Errorf("releases = %d, want 1", releases)
	}
	stop()
	if releases != 1 {
		t.Errorf("releases after second stop = %d, want 1", releases)
	}
	log := readLog()
	for _, want := range []string{
		"keep-awake: idle system sleep blocked until the collection ends",
		"keep-awake: released",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("ddc.log missing %q:\n%s", want, log)
		}
	}
}

func TestStartKeepAwake_Unsupported(t *testing.T) {
	readLog := captureLog(t)
	swapKeepAwake(t, func(string) (func() error, error) { return nil, power.ErrUnsupported })

	stop := startKeepAwake()
	stop()
	log := readLog()
	if !strings.Contains(log, "keep-awake: not supported on "+runtime.GOOS) {
		t.Errorf("ddc.log missing unsupported line:\n%s", log)
	}
	if strings.Contains(log, "WARN") {
		t.Errorf("unsupported must not warn:\n%s", log)
	}
}

func TestStartKeepAwake_OtherErrorIsWarning(t *testing.T) {
	readLog := captureLog(t)
	swapKeepAwake(t, func(string) (func() error, error) { return nil, errors.New("boom") })

	startKeepAwake()()
	if log := readLog(); !strings.Contains(log, "keep-awake: unavailable: boom") {
		t.Errorf("ddc.log missing warning:\n%s", log)
	}
}

func TestStartKeepAwake_ReleaseErrorIsWarning(t *testing.T) {
	readLog := captureLog(t)
	swapKeepAwake(t, func(string) (func() error, error) {
		return func() error { return errors.New("gone") }, nil
	})

	startKeepAwake()()
	if log := readLog(); !strings.Contains(log, "keep-awake: release failed: gone") {
		t.Errorf("ddc.log missing release warning:\n%s", log)
	}
}

func TestExecute_ReleasesKeepAwakeOnError(t *testing.T) {
	captureLog(t)
	acquires, releases := 0, 0
	swapKeepAwake(t, func(string) (func() error, error) {
		acquires++
		return func() error { releases++; return nil }, nil
	})

	tmpDir := t.TempDir()
	cs := &mockCopyStrategy{tmpDir: tmpDir}
	args := Args{
		OutputLoc:         filepath.Join(tmpDir, "output.tar.gz"),
		CopyStrategy:      cs,
		CollectionMode:    "standard",
		CollectionThreads: 1,
		CollectServerLogs: true,
	}
	hook := shutdown.NewHook()
	defer hook.Cleanup()

	err := Execute(&mockStreamCollector{}, cs, args, hook, func() {})
	if err == nil || !strings.Contains(err.Error(), "no hosts found") {
		t.Fatalf("err = %v, want one containing %q", err, "no hosts found")
	}
	if acquires != 1 || releases != 1 {
		t.Errorf("acquires=%d releases=%d, want 1 and 1", acquires, releases)
	}
}
