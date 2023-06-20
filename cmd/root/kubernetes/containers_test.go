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

//  Copyright 2023 Dremio Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// kubernetes package provides access to log collections on k8s
package kubernetes

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"
)

func TestGetInitContainers(t *testing.T) {
	podName := "example-pod"
	outputDir := "output/"

	// Backup original os.Stdout
	originalStdout := os.Stdout

	// Create a pipe for capturing the standard output
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Mock the `exec.Command` function
	execCommand = func(command string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=TestHelperProcess", "--", command}
		cs = append(cs, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
		cmd.Stdout = w
		return cmd
	}

	// Helper process to simulate the external command
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		command := os.Args[2]
		switch command {
		case "bash":
			fmt.Println("initContainer1 initContainer2")
		case "kubectl":
			fmt.Println("Logs for container initContainer1 in pod example-pod:")
			fmt.Println("Log line 1")
			fmt.Println("Log line 2")
			fmt.Println("Logs for container initContainer2 in pod example-pod:")
			fmt.Println("Log line 3")
		}
		return
	}

	err := GetInitContainerLogs(podName, outputDir)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Restore original os.Stdout
	w.Close()
	os.Stdout = originalStdout

	var capturedOutput bytes.Buffer
	_, _ = io.Copy(&capturedOutput, r)

	expectedOutput := "Logs for container initContainer1 in pod example-pod:\nLog line 1\nLog line 2\n" +
		"Logs for container initContainer2 in pod example-pod:\nLog line 3\n"

	if capturedOutput.String() != expectedOutput {
		t.Errorf("Unexpected output. Expected:\n%s\nGot:\n%s", expectedOutput, capturedOutput.String())
	}
}

// Helper function to simulate `exec.Command`
var execCommand = exec.Command

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	os.Exit(func() int {
		cmd := execCommand(os.Args[2], os.Args[3:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}())
}
