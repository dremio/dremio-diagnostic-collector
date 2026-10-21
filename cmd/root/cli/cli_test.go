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

package cli_test

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/cli"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/output"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/shutdown"
)

var (
	hook          = shutdown.NewHook()
	c             = cli.NewCli(hook)
	outputHandler cli.OutputHandler
)

func setupTestCLI() {
	outputHandler = func(line string) {
		fmt.Println(line)
	}
}

func TestExecuteAndStreamOutput_WithValidCommand(t *testing.T) {
	setupTestCLI()
	var err error
	var captureErr error
	var out string
	var expectedOut string
	if runtime.GOOS != "windows" {
		expectedOut = "file1\nfile2\n"
		out, captureErr = output.CaptureOutput(func() {
			err = c.ExecuteAndStreamOutput(false, outputHandler, "", "ls", "-1", filepath.Join("testdata", "ls"))
		})
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
	} else {
		expectedOut = "file1\nfile2\n"
		out, captureErr = output.CaptureOutput(func() {
			err = c.ExecuteAndStreamOutput(false, outputHandler, "", "cmd.exe", "/c", "dir", "/B", filepath.Join("testdata", "ls"))
		})
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
	}
	if captureErr != nil {
		t.Errorf("Unexpected error: %v", captureErr)
	}
	if out != expectedOut {
		t.Errorf("expected '%q' but was '%q'", expectedOut, out)
	}
}

func TestExecuteAndStreamOutput_WithCommandProducesStderr(t *testing.T) {
	setupTestCLI()
	var err error
	var captureErr error
	var out string
	var expectedOut string
	if runtime.GOOS != "windows" {
		expectedOut = "No such file or directory"
		out, captureErr = output.CaptureOutput(func() {
			err = c.ExecuteAndStreamOutput(false, outputHandler, "", "cat", "nonexistentfile")
		})
		if err == nil {
			t.Errorf("Expected error but got nil")
		}
	} else {
		out, captureErr = output.CaptureOutput(func() {
			err = c.ExecuteAndStreamOutput(false, outputHandler, "", "cmd.exe", "/c", "dir", "doesntexist")
		})
		if err == nil {
			t.Errorf("Expected error but got nil")
		}
	}
	if captureErr != nil {
		t.Errorf("Unexpected error: %v", captureErr)
	}
	if !strings.Contains(out, expectedOut) {
		t.Errorf("Expected output to contain '%v' but was '%v'", expectedOut, out)
	}
}

func TestExecuteAndStreamOutput_WithInvalidCommand(t *testing.T) {
	setupTestCLI()
	err := c.ExecuteAndStreamOutput(false, outputHandler, "", "22JIDJMJMHHF")
	if err == nil {
		t.Fatal("Expected error but got nil")
	}
	if !strings.Contains(err.Error(), "executable file not found") {
		t.Errorf("Expected error message to contain 'executable file not found', but it was %v", err)
	}
}

func TestExecute_WhenCommandIsValid(t *testing.T) {
	setupTestCLI()
	var expectedOut string
	var out string
	var err error
	if runtime.GOOS != "windows" {
		out, err = c.Execute(false, "ls", "-1", filepath.Join("testdata", "ls"))
		expectedOut = "file1\nfile2\n"
	} else {
		out, err = c.Execute(false, "cmd.exe", "/c", "dir", "/B", filepath.Join("testdata", "ls"))
		expectedOut = "file1\r\nfile2\r\n"
	}
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if out != expectedOut {
		t.Errorf("expected %q but was %q", expectedOut, out)
	}
}

func TestExecute_WhenNoArgumentsProvidedForCommand(t *testing.T) {
	setupTestCLI()
	var expectedOut string
	var out string
	var err error
	if runtime.GOOS == "windows" {
		out, err = c.Execute(false, "cmd.exe")
		expectedOut = "Microsoft"
	} else {
		out, err = c.Execute(false, "ls")
		expectedOut = "cli.go"
	}
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if !strings.Contains(out, expectedOut) {
		t.Errorf("Expected output to contain '%s', but it didn't", expectedOut)
	}
}

func TestExecute_WhenCommandIsInvalid(t *testing.T) {
	setupTestCLI()
	_, err := c.Execute(false, "22JIDJMJMHHF")
	if err == nil {
		t.Fatal("Expected error but got nil")
	}
	if !strings.Contains(err.Error(), "executable file not found") {
		t.Errorf("Expected error message to contain 'executable file not found', but it was %v", err)
	}
}

// writeLongLine writes n 'x' bytes followed by a newline in chunks.
func writeLongLine(n int) {
	w := bufio.NewWriterSize(os.Stdout, 1<<20)
	chunk := []byte(strings.Repeat("x", 1<<20))
	for n > 0 {
		k := min(n, len(chunk))
		_, _ = w.Write(chunk[:k])
		n -= k
	}
	_, _ = w.WriteString("\n")
	_ = w.Flush()
}

// TestHelperProcess is re-executed as a child process by runHelper. It is a
// no-op during normal test runs and always exits when acting as the helper.
func TestHelperProcess(t *testing.T) {
	mode := os.Getenv("DDC_CLI_TEST_HELPER")
	if mode == "" {
		return
	}
	const guardLine = "error: memory guard: refusing to start: source=host used_mib=1 limit_mib=1 reserve_mib=1"
	switch mode {
	case "long1m":
		writeLongLine(1 << 20)
		fmt.Println("end")
		os.Exit(0)
	case "toolong":
		writeLongLine((64 << 20) + 1)
		os.Exit(0)
	case "exit3":
		fmt.Fprintln(os.Stderr, guardLine)
		os.Exit(3)
	case "toolong-exit3":
		writeLongLine((64 << 20) + 1)
		os.Exit(3)
	}
	os.Exit(99)
}

func runHelper(t *testing.T, mode string) ([]string, error) {
	t.Helper()
	t.Setenv("DDC_CLI_TEST_HELPER", mode)
	var lines []string
	collect := func(line string) { lines = append(lines, line) }
	done := make(chan error, 1)
	go func() {
		done <- c.ExecuteAndStreamOutput(false, collect, "", os.Args[0], "-test.run=^TestHelperProcess$")
	}()
	select {
	case err := <-done:
		return lines, err
	case <-time.After(60 * time.Second):
		t.Fatal("ExecuteAndStreamOutput hung")
		return nil, nil
	}
}

func TestExecuteAndStreamOutput_LongLineIntact(t *testing.T) {
	lines, err := runHelper(t, "long1m")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != 2 || lines[0] != strings.Repeat("x", 1<<20) || lines[1] != "end" {
		t.Fatalf("unexpected lines: count=%d", len(lines))
	}
}

func TestExecuteAndStreamOutput_LineOverMaxReturnsErrorNoHang(t *testing.T) {
	_, err := runHelper(t, "toolong")
	if !errors.Is(err, bufio.ErrTooLong) {
		t.Fatalf("expected bufio.ErrTooLong, got: %v", err)
	}
}

func TestExecuteAndStreamOutput_ExitCodeReachable(t *testing.T) {
	lines, err := runHelper(t, "exit3")
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 3 {
		t.Fatalf("expected ExitError code 3, got: %v", err)
	}
	found := false
	for _, l := range lines {
		if strings.HasPrefix(l, "error: memory guard: refusing to start") {
			found = true
		}
	}
	if !found {
		t.Fatalf("guard line missing: %v", lines)
	}
}

func TestExecuteAndStreamOutput_TooLongAndExitCode(t *testing.T) {
	_, err := runHelper(t, "toolong-exit3")
	if !errors.Is(err, bufio.ErrTooLong) {
		t.Fatalf("expected bufio.ErrTooLong, got: %v", err)
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 3 {
		t.Fatalf("expected ExitError code 3, got: %v", err)
	}
}
