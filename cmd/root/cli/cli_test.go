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
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/cmd/root/cli"
	"github.com/dremio/dremio-diagnostic-collector/pkg/output"
)

var (
	c             = &cli.Cli{}
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
			err = c.ExecuteAndStreamOutput(false, outputHandler, "ls", "-1", filepath.Join("testdata", "ls"))
		})
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
	} else {
		expectedOut = "file1\nfile2\n"
		out, captureErr = output.CaptureOutput(func() {
			err = c.ExecuteAndStreamOutput(false, outputHandler, "cmd.exe", "/c", "dir", "/B", filepath.Join("testdata", "ls"))
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
			err = c.ExecuteAndStreamOutput(false, outputHandler, "cat", "nonexistentfile")
		})
		if err == nil {
			t.Errorf("Expected error but got nil")
		}
	} else {
		out, captureErr = output.CaptureOutput(func() {
			err = c.ExecuteAndStreamOutput(false, outputHandler, "cmd.exe", "/c", "dir", "doesntexist")
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
	err := c.ExecuteAndStreamOutput(false, outputHandler, "22JIDJMJMHHF")
	if err == nil {
		t.Errorf("Expected error but got nil")
	}
	expectedErr := "executable file not found in $PATH"
	if !strings.Contains(err.Error(), expectedErr) {
		t.Errorf("Expected error message to contain '%s', but it was %v", expectedErr, err)
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
		t.Errorf("Expected error but got nil")
	}
	expectedErr := "executable file not found in $PATH"
	if !strings.Contains(err.Error(), expectedErr) {
		t.Errorf("Expected error message to contain '%s', but it was %v", expectedErr, err)
	}
}
