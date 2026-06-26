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
	"reflect"
	"runtime"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/tests"
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
	expectedCall = []string{"kubectl", "--context", k8sContext, "-n", "testns", "get", "pods", podName, "-o", "jsonpath={.spec.containers[*].name}"}
	if !reflect.DeepEqual(calls[0], expectedCall) {
		t.Errorf("\nexpected call\n%v\nbut got\n%v", expectedCall, calls[0])
	}
	expectedCall = []string{"kubectl", "--context", k8sContext, "exec", "-n", namespace, "-c", "dremio-executor", podName, "--", "sh", "-c", "ls -l"}
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
		cli:                 cli,
		detectLabelSelector: "role=dremio-pods",
		kubectlPath:         "kubectl",
		namespace:           namespace,
		k8sContext:          k8sContext,
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
	expectedCall := []string{"kubectl", "--context", k8sContext, "get", "pods", "-n", namespace, "-l", "role=dremio-pods", "--field-selector", "status.phase=Running", "-o", "name"}
	if !reflect.DeepEqual(calls[0], expectedCall) {
		t.Errorf("\nexpected call\n%v\nbut got\n%v", expectedCall, calls[0])
	}
}

func TestKubectlContainerDetectionWithSidecars(t *testing.T) {
	namespace := "testns"
	k8sContext := "west-f1"
	podName := "pod1"

	// Test case 1: Pod with sidecar first, then dremio-coordinator
	cli := &tests.MockCli{
		StoredResponse: []string{"istio-proxy dremio-coordinator", "success"},
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
		t.Errorf("expected 2 calls but got %v", len(calls))
	}

	// Should use dremio-coordinator, not istio-proxy
	expectedCall := []string{"kubectl", "--context", k8sContext, "exec", "-n", namespace, "-c", "dremio-coordinator", podName, "--", "sh", "-c", "ls -l"}
	if !reflect.DeepEqual(calls[1], expectedCall) {
		t.Errorf("\nexpected call\n%v\nbut got\n%v", expectedCall, calls[1])
	}
}

func TestKubectlContainerDetectionFallback(t *testing.T) {
	namespace := "testns"
	k8sContext := "west-f1"
	podName := "pod1"

	// Test case: Pod with no known dremio containers, should fall back to first container
	cli := &tests.MockCli{
		StoredResponse: []string{"istio-proxy some-other-container", "success"},
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
		t.Errorf("expected 2 calls but got %v", len(calls))
	}

	// Should fall back to first container (istio-proxy)
	expectedCall := []string{"kubectl", "--context", k8sContext, "exec", "-n", namespace, "-c", "istio-proxy", podName, "--", "sh", "-c", "ls -l"}
	if !reflect.DeepEqual(calls[1], expectedCall) {
		t.Errorf("\nexpected call\n%v\nbut got\n%v", expectedCall, calls[1])
	}
}

func TestKubectlContainerDetectionWithDremioInName(t *testing.T) {
	namespace := "testns"
	k8sContext := "west-f1"
	podName := "pod1"

	// Test case: Pod with custom dremio container name
	cli := &tests.MockCli{
		StoredResponse: []string{"istio-proxy my-custom-dremio-app", "success"},
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
		t.Errorf("expected 2 calls but got %v", len(calls))
	}

	// Should use my-custom-dremio-app because it contains "dremio"
	expectedCall := []string{"kubectl", "--context", k8sContext, "exec", "-n", namespace, "-c", "my-custom-dremio-app", podName, "--", "sh", "-c", "ls -l"}
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
		defer func() { _ = out.Close() }()

		// Get the data
		resp, err := http.Get(url) //nolint
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

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

func TestCliK8sActions_k8sFlags(t *testing.T) {
	tests := []struct {
		name       string
		kubeconfig string
		context    string
		want       []string
	}{
		{"both empty", "", "", nil},
		{"only kubeconfig", "/tmp/cfg", "", []string{"--kubeconfig", "/tmp/cfg"}},
		{"only context", "", "prod", []string{"--context", "prod"}},
		{"both", "/tmp/cfg", "prod", []string{"--kubeconfig", "/tmp/cfg", "--context", "prod"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := &CliK8sActions{kubeconfigPath: tc.kubeconfig, k8sContext: tc.context}
			got := c.k8sFlags()
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGetContainerNameCached(t *testing.T) {
	cli := &tests.MockCli{
		// call 1: get pods -> container; call 2: exec -> ok; call 3: exec -> ok
		StoredResponse: []string{"dremio-coordinator", "ok", "ok"},
		StoredErrors:   []error{nil, nil, nil},
	}
	k := CliK8sActions{cli: cli, kubectlPath: "kubectl", namespace: "ns", k8sContext: "ctx"}

	if _, err := k.HostExecute(false, "pod1", "ls"); err != nil {
		t.Fatalf("first exec: %v", err)
	}
	if _, err := k.HostExecute(false, "pod1", "ls"); err != nil {
		t.Fatalf("second exec: %v", err)
	}

	getPods := 0
	for _, call := range cli.Calls {
		for _, a := range call {
			if a == "pods" {
				getPods++
			}
		}
	}
	if getPods != 1 {
		t.Errorf("expected container name resolved once, got %d get-pods calls", getPods)
	}
}

func TestSearchPods_FieldSelectorFiltersRunningPods(t *testing.T) {
	cli := &tests.MockCli{
		// First call: get pods returns one pod; second call: getContainerName returns container name
		StoredResponse: []string{"pod/dremio-master-0", "dremio-master-coordinator"},
		StoredErrors:   []error{nil, nil},
	}
	k := CliK8sActions{
		cli:                 cli,
		kubectlPath:         "kubectl",
		namespace:           "testns",
		k8sContext:          "west-f1",
		detectLabelSelector: "app=dremio",
	}
	pods, err := k.SearchPods(func(container string) bool {
		return true
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pods) != 1 || pods[0] != "dremio-master-0" {
		t.Errorf("expected [dremio-master-0] but got %v", pods)
	}

	// Verify the first call (get pods) includes --field-selector status.phase=Running
	if len(cli.Calls) < 1 {
		t.Fatal("expected at least 1 call")
	}
	getPodArgs := cli.Calls[0]
	foundFieldSelector := false
	for i, arg := range getPodArgs {
		if arg == "--field-selector" && i+1 < len(getPodArgs) && getPodArgs[i+1] == "status.phase=Running" {
			foundFieldSelector = true
			break
		}
	}
	if !foundFieldSelector {
		t.Errorf("expected --field-selector status.phase=Running in kubectl args, got %v", getPodArgs)
	}
}
