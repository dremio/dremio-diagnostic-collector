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
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/cmd/root/cli"
	"github.com/dremio/dremio-diagnostic-collector/cmd/root/helpers"
	"github.com/dremio/dremio-diagnostic-collector/pkg/archive"
	"github.com/dremio/dremio-diagnostic-collector/pkg/tests"
)

type MockCopyStrategy struct {
	Returns []string
	Calls   []string
}

type MockStrategy struct {
	StrategyName string             // the name of the output strategy (defasult, healthcheck etc)
	TmpDir       string             // tmp dir used for staging files
	BaseDir      string             // the base dir of where the output is routed
	Fs           helpers.Filesystem // filesystem interface (so we can pass in realof fake filesystem, assists testing)

}

func NewMockStrategy(ddcfs helpers.Filesystem) *MockStrategy {
	dir := time.Now().Format("20060102-150405-DDC")
	tmpDir, _ := ddcfs.MkdirTemp("", "*")
	return &MockStrategy{
		StrategyName: "basic",
		BaseDir:      dir,
		TmpDir:       tmpDir,
		Fs:           ddcfs,
	}
}
func (s *MockStrategy) GetTmpDir() string {
	return path.Join(s.TmpDir, s.BaseDir)
}

func (s *MockStrategy) CreatePath(fileType, source, nodeType string) (path string, err error) {
	var isK8s bool
	if strings.Contains(source, "dremio-master") || strings.Contains(source, "dremio-executor") || strings.Contains(source, "dremio-coordinator") {
		isK8s = true
	}
	if !isK8s {
		if nodeType == "coordinator" {
			path = filepath.Join(s.TmpDir, fileType, source+"-C")
		} else {
			path = filepath.Join(s.TmpDir, fileType, source+"-E")
		}
	} else {
		path = filepath.Join(s.TmpDir, fileType, source)
	}
	return path, nil
}

func (s *MockStrategy) GzipAllFiles(_ string) ([]helpers.CollectedFile, error) {
	return nil, nil
}

func (s *MockStrategy) ArchiveDiag(_ string, _ string) error {
	return nil
}

func (s *MockStrategy) Cleanup() error {

	return nil
}

type MockCapCollector struct {
	Returns []string
	Calls   []string
}

type MockCapCopy struct {
	HostString    string
	IsCoordinator bool
	Source        string
	Destination   string
}

func (m *MockCapCollector) Name() string {
	return "Mock"
}
func (m *MockCapCollector) HelpText() string {
	return "you should use a production library"
}

func (m *MockCapCollector) FindHosts(searchTerm string) (response []string, err error) {
	response = strings.Split(searchTerm, "-")
	if len(response) > 1 && response[1] != "ok" {
		err = fmt.Errorf("ERROR: no hosts found matching %v", searchTerm)
	}
	return response, err
}

func (m *MockCapCollector) CopyFromHost(hostString string, isCoordinator bool, source, destination string) (response string, err error) {
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

func (m *MockCapCollector) CopyToHost(hostString string, isCoordinator bool, source, destination string) (response string, err error) {
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

func (m *MockCapCollector) CopyToHostSudo(hostString string, isCoordinator bool, _, source, destination string) (response string, err error) {
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

func (m *MockCapCollector) CopyFromHostSudo(hostString string, isCoordinator bool, _, source, destination string) (response string, err error) {
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

func (m *MockCapCollector) HostExecuteAndStream(_ bool, hostString string, output cli.OutputHandler, _ bool, args ...string) error {
	fullCmd := strings.Join(args, " ")
	response := "Mock execute for " + hostString + " command: " + fullCmd
	output(response)
	return nil
}

func (m *MockCapCollector) HostExecute(_ bool, hostString string, _ bool, args ...string) (response string, err error) {
	fullCmd := strings.Join(args, " ")
	response = "Mock execute for " + hostString + " command: " + fullCmd
	return response, err
}

func (m *MockCapCollector) GzipAllFiles(hostString string, _ bool, args ...string) (response string, err error) {
	fullCmd := strings.Join(args, " ")
	response = "Mock execute for " + hostString + " command: " + fullCmd
	return response, err
}

func (m *MockCapCollector) Cleanup(_ helpers.Filesystem) error {

	return nil
}

func TestFindHostsCoordinators(t *testing.T) {
	var returnValues []string
	var callValues []string
	callValues = append(callValues, "dremio-coordinator-1", "dremio-eecutor-0", "dremio-executor-1")
	mockCollector := &MockCapCollector{
		Calls:   callValues,
		Returns: returnValues,
	}

	fakeFS := helpers.NewFakeFileSystem()
	mockStrategy := NewMockStrategy(fakeFS)
	fakeTmp := mockStrategy.TmpDir
	fakeArgs := Args{
		DDCfs:          fakeFS,
		CoordinatorStr: "10.1.2.3-nok",
		ExecutorsStr:   "10.2.3.4-nok",
		OutputLoc:      fakeTmp,
		CopyStrategy:   mockStrategy,
	}

	// Test for incorrect host
	fakeArgs.CoordinatorStr = "dremio-master-99"
	expected := "ERROR: no hosts found matching dremio-master-99"
	err := Execute(mockCollector, fakeArgs.CopyStrategy, fakeArgs)
	if err.Error() != expected {
		t.Errorf("\nERROR: finding coordinators: \nexpected:\t%v\nactual:\t\t%v\n", expected, err)
	}

}

func TestFindHostsExecutors(t *testing.T) {
	var returnValues []string
	var callValues []string
	callValues = append(callValues, "dremio-coordinator-1", "dremio-eecutor-0", "dremio-executor-1")
	mockCollector := &MockCapCollector{
		Calls:   callValues,
		Returns: returnValues,
	}

	fakeFS := helpers.NewFakeFileSystem()
	mockStrategy := NewMockStrategy(fakeFS)
	fakeTmp := mockStrategy.TmpDir
	fakeArgs := Args{
		DDCfs:          fakeFS,
		CoordinatorStr: "10.1.2.3-ok",
		ExecutorsStr:   "10.2.3.4-nok",
		OutputLoc:      fakeTmp,
		CopyStrategy:   mockStrategy,
	}

	fakeArgs.ExecutorsStr = "dremio-executor-99"
	expected := "ERROR: no hosts found matching dremio-executor-99"
	err := Execute(mockCollector, fakeArgs.CopyStrategy, fakeArgs)
	if err.Error() != expected {
		t.Errorf("\nERROR: finding executors: \nexpected:\t%v\nactual:\t\t%v\n", expected, err)
	}
}

func TestTgzArchive(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	str := "my row"
	err := os.WriteFile(testFile, []byte(str), 0600)
	if err != nil {
		t.Fatal(err)
	}
	//testing tgz
	archiveFile := filepath.Join(tmpDir, "test.tgz")
	err = archive.TarGzDir(tmpDir, archiveFile)
	if err != nil {
		t.Fatal(err)
	}
	tests.TgzContainsFile(t, testFile, archiveFile, "test.txt")
}
