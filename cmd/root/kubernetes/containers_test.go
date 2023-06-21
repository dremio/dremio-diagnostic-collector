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
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

// Mock implementation of the CommandExecutor interface for testing
type MockCommandExecutor struct{}

func (m *MockCommandExecutor) ExecuteCommand(command string, args ...string) ([]byte, error) {
	// Simulate different responses based on command and arguments
	if command == "bash" && strings.HasPrefix(args[1], "kubectl get pods") {
		// Return the desired output for "kubectl get pods" command
		output := []byte("pod1\npod2\npod3")
		return output, nil
	} else if command == "bash" && strings.Contains(args[1], "jsonpath=\"{.spec['containers','initContainers'][*].name}\"") {
		// Return the desired output for "kubectl get" command for containers
		output := []byte("iContainer1 iContainer2 iContainer3")
		return output, nil
	} else if command == "bash" && strings.Contains(args[1], "jsonpath=\"{.spec['containers'][*].name}\"") {
		// Return the desired output for "kubectl get" command for init containers
		output := []byte("container1 container2 container3")
		return output, nil
	}

	// Return a default output for other commands
	output := []byte("default output")
	return output, nil
}

// Mock implementation of the exec.Command function for testing
func execCommandMock(command string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestExecCommandMockProcess", "--", command}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = []string{"GO_WANT_EXEC=1"}
	return cmd
}

// Mock process for exec.Command function
func TestExecCommandMockProcess(t *testing.T) {
	if os.Getenv("GO_WANT_EXEC") != "1" {
		return
	}

	// Simulate successful execution and capture output
	output := []byte("container1 container2 container3")
	fmt.Fprint(os.Stdout, string(output))
	os.Exit(0)
}

func TestGetK8sLogs(t *testing.T) {
	// Create an instance of the MockCommandExecutor
	mockExecutor := &MockCommandExecutor{}

	// Call the function you want to test
	err := GetK8sLogs(mockExecutor, "namespace", "outputDir")

	// Assert that the error is nil (indicating success)
	if err != nil {
		t.Errorf("Expected no error, but got: %v", err)
	}
}

func TestGetPods(t *testing.T) {
	// Create an instance of the MockCommandExecutor
	mockExecutor := &MockCommandExecutor{}

	// Call the function you want to test
	err, pods := GetPods(mockExecutor, "namespace")

	// Assert that the error is nil (indicating success)
	if err != nil {
		t.Errorf("Expected no error, but got: %v", err)
	}

	// Assert that the pods are correctly retrieved
	expectedPods := []string{"pod1", "pod2", "pod3"}
	if !reflect.DeepEqual(pods, expectedPods) {
		t.Errorf("Expected pods: %v, but got: %v", expectedPods, pods)
	}
}

func TestGetInitContainerLogs(t *testing.T) {
	// Create an instance of the MockCommandExecutor
	mockExecutor := &MockCommandExecutor{}

	// Call the function you want to test
	err := GetInitContainerLogs(mockExecutor, "podName", "outputDir")

	// Assert that the error is nil (indicating success)
	if err != nil {
		t.Errorf("Expected no error, but got: %v", err)
	}
}

func TestGetContainerLogs(t *testing.T) {
	// Create an instance of the MockCommandExecutor
	mockExecutor := &MockCommandExecutor{}

	// Call the function you want to test
	err := GetContainerLogs(mockExecutor, "podName", "outputDir")

	// Assert that the error is nil (indicating success)
	if err != nil {
		t.Errorf("Expected no error, but got: %v", err)
	}
}

// Helper function to check if two string slices are equal
func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
