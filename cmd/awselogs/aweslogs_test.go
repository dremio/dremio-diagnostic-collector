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

package awselogs_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/awselogs"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/tests"
)

func TestAWSELogs(t *testing.T) {
	efsDir := filepath.Join("testdata", "logs")
	tmpDir := filepath.Join(t.TempDir(), "ddc-out")
	if err := os.Mkdir(tmpDir, 0700); err != nil {
		t.Fatal(errors.Unwrap(err))
	}

	exeLoc, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	exeDir := filepath.Dir(exeLoc)
	yamlFile := filepath.Join(exeDir, "ddc.yaml")
	defer func() {
		if err := os.Remove(yamlFile); err != nil {
			t.Logf("cant remove yaml dir %v", yamlFile)
		}
	}()
	if err := os.WriteFile(yamlFile, []byte(`dremio-gclogs-dir: /path/to/gclogs
dremio-log-dir: /path/to/dremio/logs
node-name: node1
dremio-conf-dir: /path/to/dremio/conf
min-free-space-gb: 5
`), 0600); err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()
	outFile := filepath.Join(outDir, "diag.tgz")
	if err := awselogs.Execute(efsDir, tmpDir, outFile); err != nil {
		t.Fatal(err)
	}
	tests.TgzContainsFile(t, filepath.Join(efsDir, "coordinator", "server.out"), outFile, "logs/coordinator/server.out")
	tests.TgzContainsFile(t, filepath.Join(efsDir, "executor", "node1", "server.out"), outFile, "logs/node1/server.out")
	tests.TgzContainsFile(t, filepath.Join(efsDir, "executor", "node2", "server.out"), outFile, "logs/node2/server.out")

	///validate directory cleaned up all old tarballs and directories
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}

	var entryNames []string
	for _, e := range entries {
		entryNames = append(entryNames, e.Name())
	}
	if len(entryNames) != 1 {
		t.Fatalf("should be one entry but there was the following %#v", entryNames)
	}

	if entryNames[0] != "diag.tgz" {
		t.Fatalf("expected diag.tgz but was %v", entryNames[0])
	}
}
