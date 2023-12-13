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
	"github.com/dremio/dremio-diagnostic-collector/cmd/root/cli"
)

type MockCollector struct {
	Returns     [][]interface{}
	Calls       []map[string]interface{}
	CallCounter int
}

func (m *MockCollector) CopyToHost(hostString string, isCoordinator bool, source, destination string) (out string, err error) {
	args := make(map[string]interface{})
	args["call"] = "copyToHost"
	args["hostString"] = hostString
	args["isCoordinator"] = isCoordinator
	args["source"] = source
	args["destination"] = destination
	m.Calls = append(m.Calls, args)
	response := m.Returns[m.CallCounter]
	m.CallCounter++
	if response[1] == nil {
		return response[0].(string), nil

	}
	return response[0].(string), response[1].(error)
}
func (m *MockCollector) CopyToHostSudo(hostString string, isCoordinator bool, _, source, destination string) (out string, err error) {
	args := make(map[string]interface{})
	args["call"] = "copyToHostSudo"
	args["hostString"] = hostString
	args["isCoordinator"] = isCoordinator
	args["source"] = source
	args["destination"] = destination
	m.Calls = append(m.Calls, args)
	response := m.Returns[m.CallCounter]
	m.CallCounter++
	if response[1] == nil {
		return response[0].(string), nil
	}
	return response[0].(string), response[1].(error)
}
func (m *MockCollector) CopyFromHost(hostString string, isCoordinator bool, source, destination string) (out string, err error) {
	args := make(map[string]interface{})
	args["call"] = "copyFromHost"
	args["hostString"] = hostString
	args["isCoordinator"] = isCoordinator
	args["source"] = source
	args["destination"] = destination
	m.Calls = append(m.Calls, args)
	response := m.Returns[m.CallCounter]
	m.CallCounter++
	if response[1] == nil {
		return response[0].(string), nil

	}
	return response[0].(string), response[1].(error)
}
func (m *MockCollector) CopyFromHostSudo(hostString string, isCoordinator bool, _, source, destination string) (out string, err error) {
	args := make(map[string]interface{})
	args["call"] = "copyFromHostSudo"
	args["hostString"] = hostString
	args["isCoordinator"] = isCoordinator
	args["source"] = source
	args["destination"] = destination
	m.Calls = append(m.Calls, args)
	response := m.Returns[m.CallCounter]
	m.CallCounter++
	if response[1] == nil {
		return response[0].(string), nil
	}
	return response[0].(string), response[1].(error)
}

func (m *MockCollector) FindHosts(searchTerm string) (podName []string, err error) {
	args := make(map[string]interface{})
	args["searchTerm"] = searchTerm
	m.Calls = append(m.Calls, args)
	response := m.Returns[m.CallCounter]
	m.CallCounter++
	return response[0].([]string), response[1].(error)
}
func (m *MockCollector) HostExecute(_ bool, hostString string, isCoordinator bool, args ...string) (stdOut string, err error) {
	capturedArgs := make(map[string]interface{})
	capturedArgs["hostString"] = hostString
	capturedArgs["isCoordinator"] = isCoordinator
	capturedArgs["args"] = args
	m.Calls = append(m.Calls, capturedArgs)
	response := m.Returns[m.CallCounter]
	m.CallCounter++
	if response[1] == nil {
		return response[0].(string), nil

	}
	return response[0].(string), response[1].(error)
}

func (m *MockCollector) HostExecuteAndStream(_ bool, hostString string, _ cli.OutputHandler, isCoordinator bool, args ...string) error {
	capturedArgs := make(map[string]interface{})
	capturedArgs["hostString"] = hostString
	capturedArgs["isCoordinator"] = isCoordinator
	capturedArgs["args"] = args
	m.Calls = append(m.Calls, capturedArgs)
	response := m.Returns[m.CallCounter]
	m.CallCounter++
	if response[0] == nil {
		return nil

	}
	return response[0].(error)
}

func (m *MockCollector) HelpText() string {
	return "help me"
}

func (m *MockCollector) Name() string {
	return "Mock"
}

// func TestFindFiles(t *testing.T) {
// 	expectedOutput := "/opt/file1\n/opt/file2\n"
// 	var returnValues [][]interface{}
// 	e := []interface{}{expectedOutput, nil}
// 	returnValues = append(returnValues, e)
// 	mockCollector := &MockCollector{
// 		Returns: returnValues,
// 	}
// 	myHost := "thishost"
// 	conf := HostCaptureConfiguration{
// 		Logger:                    &log.Logger{},
// 		IsCoordinator:             false,
// 		Collector:                 mockCollector,
// 		Host:                      myHost,
// 		OutputLocation:            "",
// 		DremioConfDir:             "",
// 		DremioLogDir:              "",
// 		DurationDiagnosticTooling: 0,
// 		LogAge:                    5,
// 	}
// 	searchStr := "/opt/file*"
// 	files, err := findFiles(conf, searchStr, true)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	if !reflect.DeepEqual(files, []string{"/opt/file1", "/opt/file2"}) {
// 		t.Errorf("expected '%v' but was '%v'", expectedOutput, files)
// 	}
// 	if mockCollector.CallCounter != 1 {
// 		t.Fatalf("expected 1 call but was %v", mockCollector.CallCounter)
// 	}
// 	if len(mockCollector.Calls) != 1 {
// 		t.Fatalf("expected 1 call but was %v", len(mockCollector.Calls))
// 	}
// 	calls := mockCollector.Calls[0]
// 	if calls["hostString"] != myHost {
// 		t.Errorf("expected %v but was %v", myHost, calls["hostString"])
// 	}
// 	if calls["isCoordinator"] != conf.IsCoordinator {
// 		t.Errorf("expected %v but was %v", conf.IsCoordinator, calls["isCoordinator"])
// 	}

// 	expectedArgs := []string{"find", "/opt/file*", "-maxdepth", "4", "-type", "f", "-mtime", "-5", "2>/dev/null"}
// 	if !reflect.DeepEqual(calls["args"], expectedArgs) {
// 		t.Errorf("expected %v but was %v", expectedArgs, calls["args"])
// 	}
// }

// func TestFindAWSEFiles(t *testing.T) {
// 	expectedOutput := "/var/dremio_efs/coordinator/file1\n/var/dremio_efs/executor/ip-10-1-2-3/file2\n"
// 	var returnValues [][]interface{}
// 	e := []interface{}{expectedOutput, nil}
// 	returnValues = append(returnValues, e)
// 	mockCollector := &MockCollector{
// 		Returns: returnValues,
// 	}
// 	myHost := "thishost"
// 	conf := HostCaptureConfiguration{
// 		Logger:                    &log.Logger{},
// 		IsCoordinator:             false,
// 		Collector:                 mockCollector,
// 		Host:                      myHost,
// 		OutputLocation:            "",
// 		DremioConfDir:             "",
// 		DremioLogDir:              "",
// 		DurationDiagnosticTooling: 0,
// 		LogAge:                    5,
// 	}
// 	searchStr := "/var/dremio_efs/logs"
// 	files, err := findFiles(conf, searchStr, true)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	if !reflect.DeepEqual(files, []string{"/var/dremio_efs/coordinator/file1", "/var/dremio_efs/executor/ip-10-1-2-3/file2"}) {
// 		t.Errorf("expected '%v' but was '%v'", expectedOutput, files)
// 	}
// 	if mockCollector.CallCounter != 1 {
// 		t.Fatalf("expected 1 call but was %v", mockCollector.CallCounter)
// 	}
// 	if len(mockCollector.Calls) != 1 {
// 		t.Fatalf("expected 1 call but was %v", len(mockCollector.Calls))
// 	}
// 	calls := mockCollector.Calls[0]
// 	if calls["hostString"] != myHost {
// 		t.Errorf("expected %v but was %v", myHost, calls["hostString"])
// 	}
// 	if calls["isCoordinator"] != conf.IsCoordinator {
// 		t.Errorf("expected %v but was %v", conf.IsCoordinator, calls["isCoordinator"])
// 	}

// 	expectedArgs := []string{"find", "/var/dremio_efs/logs", "-maxdepth", "4", "-type", "f", "-mtime", "-5", "2>/dev/null"}
// 	if !reflect.DeepEqual(calls["args"], expectedArgs) {
// 		t.Errorf("expected %v but was %v", expectedArgs, calls["args"])
// 	}
// }
