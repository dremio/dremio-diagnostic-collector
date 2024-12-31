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

// kubernetes package provides access to log collections on k8s
package kubectl

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/tests"
)

func TestKubectlExec(t *testing.T) {
	namespace := "testns"
	k8sContext := "west-f1"
	podName := "pod1"
	cli := &tests.MockCli{
		StoredResponse: []string{"dremio-executor", "success"},
		StoredErrors:   []error{nil, nil},
	}
	k := CliK8sActions{
		cli:         cli,
		kubectlPath: "kubectl",
		namespace:   namespace,
		k8sContext:  k8sContext,
	}
	out, err := k.HostExecute(false, podName, "ls", "-l")
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}

	if out != "success" {
		t.Errorf("expected success but got %v", out)
	}
	calls := cli.Calls
	if len(calls) != 2 {
		t.Errorf("expected 1 call but got %v", len(calls))
	}
	var expectedCall []string
	expectedCall = []string{"kubectl", "-n", "testns", "--context", k8sContext, "get", "pods", podName, "-o", "jsonpath={.spec.containers[0].name}"}
	if !reflect.DeepEqual(calls[0], expectedCall) {
		t.Errorf("\nexpected call\n%v\nbut got\n%v", expectedCall, calls[0])
	}
	expectedCall = []string{"kubectl", "exec", "-n", namespace, "--context", k8sContext, "-c", "dremio-executor", podName, "--", "ls", "-l"}
	if !reflect.DeepEqual(calls[1], expectedCall) {
		t.Errorf("\nexpected call\n%v\nbut got\n%v", expectedCall, calls[1])
	}
}

func TestKubectlSearch(t *testing.T) {
	namespace := "testns"
	k8sContext := "west-f1"

	cli := &tests.MockCli{
		StoredResponse: []string{"pod/pod1\npod/pod2\npod/pod3\n", "dremio-coordinator", "dremio-coordinator", "dremio-coordinator"},
		StoredErrors:   []error{nil, nil, nil, nil},
	}
	k := CliK8sActions{
		cli:         cli,
		kubectlPath: "kubectl",
		namespace:   namespace,
		k8sContext:  k8sContext,
	}
	podNames, err := k.GetCoordinators()
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	// we need to pass the namespace for later commands that may consume this
	// period is handy because it is an illegal character in a kubernetes name and so
	// can act as a separator
	expectedPods := []string{"pod1", "pod2", "pod3"}
	if !reflect.DeepEqual(podNames, expectedPods) {
		t.Errorf("expected %v call but got %v", expectedPods, podNames)
	}
	calls := cli.Calls
	if len(calls) != 4 {
		t.Errorf("expected 4 call but got %v", len(calls))
	}
	expectedCall := []string{"kubectl", "get", "pods", "-n", namespace, "--context", k8sContext, "-l", "role=dremio-cluster-pod", "-o", "name"}
	if !reflect.DeepEqual(calls[0], expectedCall) {
		t.Errorf("\nexpected call\n%v\nbut got\n%v", expectedCall, calls[0])
	}
}

func TestKubectCopyFrom(t *testing.T) {
	namespace := "testns"
	podName := "pod"
	k8sContext := "west-f1"
	source := filepath.Join(string(filepath.Separator), "podroot", "test.log")
	destination := filepath.Join(string(filepath.Separator), "mydir", "test.log")
	cli := &tests.MockCli{
		StoredResponse: []string{"dremio-executor", "success"},
		StoredErrors:   []error{nil, nil},
	}
	k := CliK8sActions{
		cli:            cli,
		kubectlPath:    "kubectl",
		namespace:      namespace,
		k8sContext:     k8sContext,
		retriesEnabled: true,
	}
	out, err := k.CopyFromHost(podName, source, destination)
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}

	if out != "success" {
		t.Errorf("expected success but got %v", out)
	}
	calls := cli.Calls
	if len(calls) != 2 {
		t.Errorf("expected 2 call but got %v", len(calls))
	}
	expectedCall := []string{"kubectl", "cp", "-n", namespace, "--context", k8sContext, "-c", "dremio-executor", "--retries", "999", fmt.Sprintf("%v:%v", podName, source), destination}
	if !reflect.DeepEqual(calls[1], expectedCall) {
		t.Errorf("\nexpected call\n%v\nbut got\n%v", expectedCall, calls[1])
	}
}

func TestKubectCopyFromWindowsHost(t *testing.T) {
	namespace := "testns"
	podName := "pod"
	k8sContext := "east-8x"
	source := filepath.Join("podroot", "test.log")
	destination := filepath.Join("C:", string(filepath.Separator), "mydir", "test.log")
	cli := &tests.MockCli{
		StoredResponse: []string{"dremio-executor", "success"},
		StoredErrors:   []error{nil, nil},
	}
	k := CliK8sActions{
		cli:            cli,
		kubectlPath:    "kubectl",
		namespace:      namespace,
		k8sContext:     k8sContext,
		retriesEnabled: true,
	}
	out, err := k.CopyFromHost(podName, source, destination)
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}

	if out != "success" {
		t.Errorf("expected success but got %v", out)
	}
	calls := cli.Calls
	if len(calls) != 2 {
		t.Errorf("expected 2 call but got %v", len(calls))
	}
	// we remove the C: due to issue found in https://github.com/kubernetes/kubernetes/issues/77310"
	expectedDestination := filepath.Join(string(filepath.Separator), "mydir", "test.log")
	expectedCall := []string{"kubectl", "cp", "-n", namespace, "--context", k8sContext, "-c", "dremio-executor", "--retries", "999", fmt.Sprintf("%v:%v", podName, source), expectedDestination}
	if !reflect.DeepEqual(calls[1], expectedCall) {
		t.Errorf("\nexpected call\n%v\nbut got\n%v", expectedCall, calls[1])
	}
}

func TestKubectlCtrlVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testing in short mode")
	}
	tmpDir := t.TempDir()
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	oldVersion := fmt.Sprintf("https://dl.k8s.io/release/v1.22.0/bin/%v/%v/kubectl", goos, goarch)
	newVersion := fmt.Sprintf("https://dl.k8s.io/release/v1.23.0/bin/%v/%v/kubectl", goos, goarch)
	downloadFile := func(url string, outFile string) error {
		out, err := os.Create(outFile)
		if err != nil {
			return err
		}
		defer out.Close()

		// Get the data
		resp, err := http.Get(url) //nolint
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		// Write the body to file
		_, err = io.Copy(out, resp.Body)
		if err != nil {
			return err
		}
		if err := os.Chmod(outFile, 0o700); err != nil {
			return err
		}
		return nil
	}
	oldExec := path.Join(tmpDir, "kubectlOld")
	if err := downloadFile(oldVersion, oldExec); err != nil {
		t.Fatalf("unable to download file %v %v: ", oldExec, err)
	}

	newExec := path.Join(tmpDir, "kubectlNew")
	if err := downloadFile(newVersion, newExec); err != nil {
		t.Fatalf("unable to download file %v %v: ", newExec, err)
	}

	result, err := CanRetryTransfers(oldExec)
	if err != nil {
		t.Errorf("unable to execute file %v %v: ", oldExec, err)
	}
	if result {
		t.Error("failed should not be able to retry on old exec")
	}
	result, err = CanRetryTransfers(newExec)
	if err != nil {
		t.Errorf("unable to execute file %v %v: ", newExec, err)
	}
	if !result {
		t.Error("failed should be able to retry on new exec")
	}
}
