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

// cmd package contains all the command line flag and initialization logic for commands
package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/cmd/root/collection"
	"github.com/dremio/dremio-diagnostic-collector/cmd/root/ssh"
	"github.com/dremio/dremio-diagnostic-collector/pkg/output"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
)

func TestSSHDefault(t *testing.T) {
	sshPath, err := sshDefault()
	if err != nil {
		t.Fatalf("unexpected exception %v", err)
	}

	expectedPath := filepath.Join(".ssh", "id_rsa")
	if !strings.HasSuffix(sshPath, expectedPath) {
		t.Errorf("expected %v but was %v", expectedPath, sshPath)
	}
}

func TestValidateParameters(t *testing.T) {
	tc := makeTestCollection()
	tc.CoordinatorStr = ""
	err := validateParameters(tc, ssh.Args{
		SSHKeyLoc: "/home/dremio/.ssh",
		SSHUser:   "dremio",
	}, true)
	expectedError := "the coordinator string was empty you must pass a label that will match your coordinators --coordinator or -c arguments. Example: -c \"mylabel=coordinator\""
	if expectedError != err.Error() {
		t.Errorf("expected: %v but was %v", expectedError, err.Error())
	}

	tc = makeTestCollection()
	err = validateParameters(tc, ssh.Args{
		SSHKeyLoc: "",
		SSHUser:   "dremio",
	}, false)
	expectedError = "the ssh private key location was empty, pass --ssh-key or -s with the key to get past this error. Example --ssh-key ~/.ssh/id_rsa"
	if expectedError != err.Error() {
		t.Errorf("expected: %v but was %v", expectedError, err.Error())
	}

	tc = makeTestCollection()
	err = validateParameters(tc, ssh.Args{
		SSHKeyLoc: "/home/dremio/.ssh",
		SSHUser:   "",
	}, false)
	expectedError = "the ssh user was empty, pass --ssh-user or -u with the user name you want to use to get past this error. Example --ssh-user ubuntu"

	if expectedError != err.Error() {
		t.Errorf("expected: %v but was %v", expectedError, err.Error())
	}
}

func TestExecute(t *testing.T) {
	_ = makeTestCollection()
	actual, err := captureAllOutput(checkstds)
	//message, err := captureAllOutput(Execute)
	expected := "This is stdout\nThis is stderr\n"
	if expected != actual {
		t.Errorf("\nERROR: stdout : \nexpected:\t%v\nactual:\t\t%v\n", expected, actual)
	}
	if err != nil {
		t.Errorf("\nERROR: stderr : \nexpected:\t%v\nactual:\t\t%v\n", expected, err)
	}
}

// Set of args for other tests
func makeTestCollection() collection.Args {
	testCollection := collection.Args{
		CoordinatorStr: "dremio-master-0",
		ExecutorsStr:   "dremio-executor-0",
		OutputLoc:      "/tmp/diags",
	}
	return testCollection
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

func TestAllSubCommandsAreWiredUp(t *testing.T) {
	helpText, err := output.CaptureOutput(func() {
		if err := RootCmd.Help(); err != nil {
			t.Errorf("unable to process help text with error %v", err)
		}
	})
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	expected := "Available Commands:\n  awselogs      Log only collect of AWSE from the coordinator node\n  local-collect retrieves all the dremio logs and diagnostics for the local node and saves the results in a compatible format for Dremio support\n  version       Print the version number of DDC\n"
	if !strings.Contains(helpText, expected) {
		t.Errorf("missing command text in `%q`", helpText)
	}
}

func TestValidateDDCYamlValid(t *testing.T) {

	valid := filepath.Join("testdata", "ddc-valid.yaml")
	_, err := ValidateAndReadYaml(valid)
	if err != nil {
		t.Errorf("expected no error for valid yaml: %v", err)
	}
}

func TestValidateDDCYamlNotPresent(t *testing.T) {
	valid := filepath.Join("testdata", "not-found-anwhere.yaml")
	_, err := ValidateAndReadYaml(valid)
	if err == nil {
		t.Error("expected an error for missing yaml")
	}
}

func TestValidateDDCYamlNotValid(t *testing.T) {
	valid := filepath.Join("testdata", "ddc-invalid.yaml")
	_, err := ValidateAndReadYaml(valid)
	if err == nil {
		t.Errorf("expected an error for invalid yaml: %v", err)
	}
	expected := "unable to parse yaml: yaml: line 2: could not find expected ':'"
	if err.Error() != expected {
		t.Errorf("expected '%v' but was '%v'", expected, err.Error())
	}
}

func TestValidateDDCYamlMaskPAT(t *testing.T) {
	logLoc := filepath.Join(t.TempDir(), "test-ddc.log")
	simplelog.InitLoggerWithFile(4, logLoc)
	defer func() {
		if err := simplelog.Close(); err != nil {
			t.Logf("unable to close log file %v", err)
		}
		simplelog.InitLoggerWithFile(4, filepath.Join(os.TempDir(), "ddc.log"))
	}()
	valid := filepath.Join("testdata", "ddc-valid.yaml")
	_, err := ValidateAndReadYaml(valid)
	if err != nil {
		t.Errorf("expected no error for valid yaml: %v", err)
	}

	b, err := os.ReadFile(logLoc)
	if err != nil {
		t.Fatalf("need to read the log file '%v': '%v'", logLoc, err)
	}

	if !strings.Contains(string(b), "yaml key 'dremio-pat-token':'REDACTED'") {
		t.Errorf("expected redacted pat: '%v'", string(b))
	}
}
