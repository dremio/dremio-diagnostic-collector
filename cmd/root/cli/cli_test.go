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
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/cmd/root/cli"
)

var (
	c              *cli.Cli
	outputHandler  cli.OutputHandler
	executedOutput string
)

func setupTestCLI() {
	c = &cli.Cli{}
	executedOutput = ""
	outputHandler = func(line string) {
		executedOutput += line + "\n"
	}
}

func TestExecuteAndStreamOutput_WithValidCommand(t *testing.T) {
	setupTestCLI()
	err := c.ExecuteAndStreamOutput(outputHandler, "ls", "-v")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if strings.TrimSpace(executedOutput) == "" {
		t.Errorf("Expected executedOutput to be not empty")
	}
}

func TestExecuteAndStreamOutput_WithCommandProducesStderr(t *testing.T) {
	setupTestCLI()
	err := c.ExecuteAndStreamOutput(outputHandler, "cat", "nonexistentfile")
	if err == nil {
		t.Errorf("Expected error but got nil")
	}
	if !strings.Contains(strings.TrimSpace(executedOutput), "No such file or directory") {
		t.Errorf("Expected executedOutput to contain 'No such file or directory'")
	}
}

func TestExecuteAndStreamOutput_WithInvalidCommand(t *testing.T) {
	setupTestCLI()
	err := c.ExecuteAndStreamOutput(outputHandler, "22JIDJMJMHHF")
	if err == nil {
		t.Errorf("Expected error but got nil")
	}
	expectedErr := "unable to start command '22JIDJMJMHHF' due to error"
	if !strings.Contains(err.Error(), expectedErr) {
		t.Errorf("Expected error message to contain '%s', but it didn't", expectedErr)
	}
}

func TestExecute_WhenCommandIsValid(t *testing.T) {
	setupTestCLI()
	var expectedOut string
	var out string
	var err error
	if runtime.GOOS == "windows" {
		out, err = c.Execute("cmd.exe", "/c", "dir", "/B", filepath.Join("testdata", "ls"))
		expectedOut = "file1\r\nfile2\r\n"
	} else {
		out, err = c.Execute("ls", "-a", filepath.Join("testdata", "ls"))
		expectedOut = "file1\nfile2\n"
	}
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if !strings.Contains(out, expectedOut) {
		t.Errorf("Expected output to contain '%s', but it didn't", expectedOut)
	}
}

func TestExecute_WhenNoArgumentsProvidedForCommand(t *testing.T) {
	setupTestCLI()
	var expectedOut string
	var out string
	var err error
	if runtime.GOOS == "windows" {
		out, err = c.Execute("cmd.exe")
		expectedOut = "Microsoft"
	} else {
		out, err = c.Execute("ls")
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
	_, err := c.Execute("22JIDJMJMHHF")
	if err == nil {
		t.Errorf("Expected error but got nil")
	}
	expectedErr := "unable to start command '22JIDJMJMHHF' due to error"
	if !strings.Contains(err.Error(), expectedErr) {
		t.Errorf("Expected error message to contain '%s', but it didn't", expectedErr)
	}
}
