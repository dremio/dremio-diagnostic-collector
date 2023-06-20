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

// collection package provides the interface for collection implementation and the actual collection execution
package collection

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/cmd/root/cli"
	"github.com/dremio/dremio-diagnostic-collector/cmd/root/helpers"
)

type MockClusterCollector struct {
	Returns []string
	Calls   []string
}

type MockClusterCopy struct {
	HostString    string
	IsCoordinator bool
	Source        string
	Destination   string
}

func (m *MockClusterCollector) HelpText() string {
	return "you should use a production library"
}

func (m *MockClusterCollector) FindHosts(searchTerm string) (response []string, err error) {
	if searchTerm == "role=dremio-cluster-pod" {
		response = append(response, "dremio-master-0", "dremio-executor-0")
	}
	// add other check / responses in here as needed
	return response, err
}

func (m *MockClusterCollector) CopyFromHost(hostString string, isCoordinator bool, source, destination string) (response string, err error) {
	copyCall := MockCapCopy{
		HostString:    hostString,
		IsCoordinator: isCoordinator,
		Source:        source,
		Destination:   destination,
	}
	if copyCall.Source == "/var/log/dremio" {
		response = "INFO: logs copied from /var/log/dremio1"
	} else if copyCall.Source == "/var/log/missing" {
		response = "WARN: No logs found at /var/log/missing"
	} else {
		response = "no files found"
		err = fmt.Errorf("ERROR: no files found for %v", copyCall.Source)
	}
	return response, err
}

func (m *MockClusterCollector) CopyToHost(hostString string, isCoordinator bool, source, destination string) (response string, err error) {
	copyCall := MockCapCopy{
		HostString:    hostString,
		IsCoordinator: isCoordinator,
		Source:        source,
		Destination:   destination,
	}
	if copyCall.Source == "/var/log/dremio" {
		response = "INFO: logs copied from /var/log/dremio1"
	} else if copyCall.Source == "/var/log/missing" {
		response = "WARN: No logs found at /var/log/missing"
	} else {
		response = "no files found"
		err = fmt.Errorf("ERROR: no files found for %v", copyCall.Source)
	}
	return response, err
}

func (m *MockClusterCollector) CopyToHostSudo(hostString string, isCoordinator bool, _, source, destination string) (response string, err error) {
	copyCall := MockCapCopy{
		HostString:    hostString,
		IsCoordinator: isCoordinator,
		Source:        source,
		Destination:   destination,
	}
	if copyCall.Source == "/var/log/dremio" {
		response = "INFO: logs copied from /var/log/dremio1"
	} else if copyCall.Source == "/var/log/missing" {
		response = "WARN: No logs found at /var/log/missing"
	} else {
		response = "no files found"
		err = fmt.Errorf("ERROR: no files found for %v", copyCall.Source)
	}
	return response, err
}

func (m *MockClusterCollector) CopyFromHostSudo(hostString string, isCoordinator bool, _, source, destination string) (response string, err error) {
	copyCall := MockCapCopy{
		HostString:    hostString,
		IsCoordinator: isCoordinator,
		Source:        source,
		Destination:   destination,
	}
	if copyCall.Source == "/var/log/dremio" {
		response = "INFO: logs copied from /var/log/dremio1"
	} else if copyCall.Source == "/var/log/missing" {
		response = "WARN: No logs found at /var/log/missing"
	} else {
		response = "no files found"
		err = fmt.Errorf("ERROR: no files found for %v", copyCall.Source)
	}
	return response, err
}

func (m *MockClusterCollector) HostExecuteAndStream(hostString string, output cli.OutputHandler, _ bool, args ...string) error {
	fullCmd := strings.Join(args, " ")
	response := "Mock execute for " + hostString + " command: " + fullCmd
	output(response)
	return nil
}

func (m *MockClusterCollector) HostExecute(hostString string, _ bool, args ...string) (response string, err error) {

	fullCmd := strings.Join(args, " ")

	response = "Mock execute for " + hostString + " command: " + fullCmd
	return response, err
}

func (m *MockClusterCollector) GzipAllFiles(hostString string, _ bool, args ...string) (response string, err error) {

	fullCmd := strings.Join(args, " ")

	response = "Mock execute for " + hostString + " command: " + fullCmd
	return response, err
}

func (m *MockClusterCollector) Cleanup(_ helpers.Filesystem) error {

	return nil
}

type ExpectedJSON struct {
	APIVersion string
	Kind       string
	Value      int
}

func TestClusterCopyJSON(t *testing.T) {
	tmpDir := t.TempDir()
	// Read a file bytes
	testjson := filepath.Join("testdata", "test.json")
	actual, err := os.ReadFile(testjson)
	if err != nil {
		log.Printf("ERROR: when reading json file\n%v\nerror returned was:\n %v", actual, err)
	}

	afile := filepath.Join(tmpDir, "actual.json")
	// Write a file with the same bytes
	err = os.WriteFile(afile, actual, DirPerms)
	if err != nil {
		t.Errorf("ERROR: trying to write file %v, error was %v", afile, err)
	}

	expected := ExpectedJSON{
		APIVersion: "v1",
		Kind:       "Data",
		Value:      100,
	}

	// Create a model file
	efile := filepath.Join(tmpDir, "expected.json")
	edata, _ := json.MarshalIndent(expected, "", "    ")
	err = os.WriteFile(efile, edata, DirPerms)
	if err != nil {
		t.Errorf("ERROR: trying to write file %v, error was %v", efile, err)
	}
	// Read back files and compare
	acheck, err := os.ReadFile(afile)
	if err != nil {
		t.Errorf("ERROR: trying to read file %v, error was %v", afile, err)
	}
	echeck, err := os.ReadFile(efile)
	if err != nil {
		t.Errorf("ERROR: trying to read file %v, error was %v", efile, err)
	}

	expStr := strings.ReplaceAll((string(echeck)), "\r\n", "\n")
	actStr := strings.ReplaceAll((string(acheck)), "\r\n", "\n")

	if expStr != actStr {
		t.Errorf("\nERROR: \nexpected:\t%q\nactual:\t\t%q\n", expStr, actStr)
	}

}

func TestGetClusterLogs(t *testing.T) {
	//var returnValues []string
	var callValues []string
	callValues = append(callValues, "dremio-coordinator-1", "dremio-eecutor-0-ok", "dremio-executor-1-ok")
	//mockClusterCollector := &MockClusterCollector{
	//	Calls:   callValues,
	//	Returns: returnValues,
	//}

	// crete tst namespace and delete at the end of the test with a defer
	// then deploy as part of the setup the cluster (dremio deployment)
	// include condition to check and wait for pods
	// if pods are up execute tests
	// check that the files are not zero for common files that have everthing

	// helm install
	// check pods

	/*&
	fakeFS := helpers.NewFakeFileSystem()

	namespace := "defaultb"
	pods :=
	//cs := NewMockStrategy(fakeFS)
	wrap := func() {
		err := GetClusterLogs(namespace, cs, fakeFS, mockClusterCollector, "kubectl")
		if err != nil {
			t.Errorf("Error testing GetClusterLogs, error was %v", err)
		}
	}

	out, err := captureAllOutput(wrap)
	if err != nil {
		t.Error("blah")
	}
	t.Logf("output %v", out)
	*/
}

func captureAllOutput(f func()) (string, error) {
	var err error
	old := os.Stdout
	olderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		return "", err
	}
	os.Stdout = w
	os.Stderr = w

	f()

	w.Close()
	os.Stdout = old
	os.Stderr = olderr

	var buf bytes.Buffer
	_, err = io.Copy(&buf, r)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

func checkstds() {
	os.Stdout.Write([]byte("This is stdout\n"))
	os.Stderr.Write([]byte("This is stderr\n"))
}

func TestSetupCluster(t *testing.T) {
	k := "kubectl"
	ns := "default"
	t.Deadline()
	expected := `poddisruptionbudget.policy/zk-pdb created

configmap/dremio-config created
configmap/dremio-hive2-config created
configmap/dremio-hive3-config created
service/dremio-client created
service/dremio-cluster-pod created
service/zk-hs created
service/zk-cs created
statefulset.apps/dremio-coordinator created
statefulset.apps/dremio-executor created
statefulset.apps/dremio-master created
statefulset.apps/zk created`

	manifest, err := os.Open("testdata/cluster.yaml")
	if err != nil {
		t.Errorf("Error when finding cluster manifest file. Error was %v", err)
	}
	cli := &cli.Cli{}
	kubectlArgs := []string{k, "-n", ns, "apply", "-f", manifest.Name()}
	out, err := cli.Execute(kubectlArgs...)
	if !reflect.DeepEqual(out, expected) {
		t.Logf("Unexpected output when setting up test cluster.\nExpected:\n%v\nGot:\n%v", expected, out)
	}
	if err != nil {
		t.Errorf("Error when setting up test cluster: %v", err)
	}

	success := checkTimeout(checkPodsRunning)
	if success == true {
		t.Log("setup ok")
	} else {
		t.Error("setup NOT ok")
	}
}

func TestTerminateCluster(t *testing.T) {
	k := "kubectl"
	ns := "default"
	expected := `poddisruptionbudget.policy/zk-pdb deleted
configmap "dremio-config" deleted
configmap "dremio-hive2-config" deleted
configmap "dremio-hive3-config" deleted
service "dremio-client" deleted
service "dremio-cluster-pod" deleted
service "zk-hs" deleted
service "zk-cs" deleted
statefulset.apps "dremio-coordinator" deleted
statefulset.apps "dremio-executor" deleted
statefulset.apps "dremio-master" deleted
statefulset.apps "zk" deleted`

	manifest, err := os.Open("testdata/cluster.yaml")
	if err != nil {
		t.Errorf("Error when finding cluster manifest file. Error was %v", err)
	}
	cli := &cli.Cli{}
	kubectlArgs := []string{k, "-n", ns, "delete", "-f", manifest.Name()}
	out, err := cli.Execute(kubectlArgs...)
	if !reflect.DeepEqual(out, expected) {
		t.Logf("Unexpected output when terminating  test cluster.\nExpected\n%v\nGot\n%v", expected, out)
	}
	if err != nil {
		t.Errorf("Error when terminating test cluster: %v", err)
	}

	success := checkTimeout(checkPodsStopped)
	if success == true {
		t.Log("terminate ok")
	} else {
		t.Error("terminate NOT ok")
	}
}

func checkTimeout(f func() (bool, error)) bool {
	// Create a ticker that ticks every 2 seconds
	ticker := time.NewTicker(2 * time.Second)

	// Maximum timeout
	maxTimeout := 120 * time.Second

	// Keep track of elapsed time
	elapsed := 0 * time.Second
	// set flag for complete / not complete
	check := false

	// Run the loop until the maximum timeout is reached
	for elapsed < maxTimeout {
		select {
		case <-ticker.C:
			// check if pods are running or deleted
			check, _ = f()
			if check == true {
				return true
			}

		case <-time.After(maxTimeout - elapsed):
			// Timeout reached, exit the loop
			return false
		}

		// Update elapsed time
		elapsed += 2 * time.Second
	}
	// default return
	return false
}

func checkPodsRunning() (bool, error) {
	k := "kubectl"
	ns := "default"
	expected := `dremio-executor-0   Running
dremio-executor-1   Running
dremio-master-0     Running
zk-0                Running
zk-1                Running
zk-2                Running
`

	cli := &cli.Cli{}
	// kubectl get pods -o custom-columns=NAME:metadata.name,STATUS:status.phase --no-headers
	kubectlArgs := []string{k, "-n", ns, "get", "pods", "-o", `custom-columns=NAME:metadata.name,STATUS:status.phase`, `--no-headers`}
	out, err := cli.Execute(kubectlArgs...)
	fmt.Printf("check for pods\n%v", out)
	if reflect.DeepEqual(out, expected) {
		return true, nil
	}
	if err != nil {
		//fmt.Errorf("Error checking pods running: %v", err)
		return false, err
	}
	return false, err
}

func checkPodsStopped() (bool, error) {
	k := "kubectl"
	ns := "default"
	not_expected := []string{"dremio-master", "dremio-executor", "dremio-coordinator", "zk"}
	cli := &cli.Cli{}
	// kubectl get pods -o custom-columns=NAME:metadata.name,STATUS:status.phase --no-headers
	kubectlArgs := []string{k, "-n", ns, "get", "pods", "-o", `custom-columns=NAME:metadata.name,STATUS:status.phase`, `--no-headers`}
	out, err := cli.Execute(kubectlArgs...)
	for _, ne := range not_expected {
		if strings.Contains(out, ne) {
			return false, nil
		}
	}
	if err != nil {
		//fmt.Errorf("Error checking pods running: %v", err)
		return false, err
	}
	return true, nil
}
