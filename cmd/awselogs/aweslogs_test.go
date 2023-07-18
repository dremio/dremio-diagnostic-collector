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

	"github.com/dremio/dremio-diagnostic-collector/cmd/awselogs"
	"github.com/dremio/dremio-diagnostic-collector/pkg/tests"
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
`), 0600); err != nil {
		t.Fatal(err)
	}
	outFile := filepath.Join(t.TempDir(), "diag.tgz")
	if err := awselogs.Execute(efsDir, tmpDir, outFile); err != nil {
		t.Fatal(errors.Unwrap(err))
	}
	tests.TgzContainsFile(t, filepath.Join(efsDir, "coordinator", "server.out"), outFile, "logs/coordinator/server.out")
	tests.TgzContainsFile(t, filepath.Join(efsDir, "executor", "node1", "server.out"), outFile, "logs/node1/server.out")
	tests.TgzContainsFile(t, filepath.Join(efsDir, "executor", "node2", "server.out"), outFile, "logs/node2/server.out")
}
