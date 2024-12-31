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
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
