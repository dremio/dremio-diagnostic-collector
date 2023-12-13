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

package apicollect_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/apicollect"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf"
)

func TestGetNumberOfJobProfilesTriedWIthNoServerUp(t *testing.T) {
	overrides := make(map[string]string)
	confDir := filepath.Join(t.TempDir(), "ddcTest")
	err := os.Mkdir(confDir, 0700)
	if err != nil {
		t.Fatalf("missing conf dir %v", err)
	}
	tmpDir := t.TempDir()
	queriesDir := filepath.Join(tmpDir, "queries", "node1")
	err = os.MkdirAll(queriesDir, 0700) //"queries is from
	if err != nil {
		t.Fatalf("cant make queries dir %v", err)
	}
	if err := os.WriteFile(filepath.Join(queriesDir, "queries.json"), []byte(`
{"queryId":"123456","start":100,"outcome":"COMPLETED","queryType":"METADATA_REFRESH","queryCost":9000000000,"planningTime":0,"executionPlanningTime":440,"runningTime":10}
{"queryId":"abcdef","start":200,"outcome":"FAILED","queryType":"ODBC","queryCost":9001,"planningTime":1,"executionPlanningTime":350,"runningTime":5}
{"queryId":"dremio","start":300,"outcome":"CANCELLED","queryType":"REST","queryCost":9002,"planningTime":2,"executionPlanningTime":340,"runningTime":1235}
`), 0600); err != nil {
		t.Fatalf("unable to write queries.json %v", err)
	}

	ddcYaml := filepath.Join(confDir, "ddc.yaml")
	err = os.WriteFile(ddcYaml, []byte(fmt.Sprintf(`
dremio-log-dir: %v
dremio-conf-dir: %v
number-job-profiles: 25000
job-profiles-num-high-query-cost: 5000 
job-profiles-num-slow-exec: 10000
job-profiles-num-recent-errors: 5000
job-profiles-num-slow-planning: 5000
dremio-pat-token: my-pat-token
node-name: node1
number-threads: 4
tmp-output-dir: %v
`, LogDir(), ConfDir(), strings.ReplaceAll(tmpDir, "\\", "\\\\"))), 0600)
	if err != nil {
		t.Fatalf("missing conf file %v", err)
	}
	c, err := conf.ReadConf(overrides, ddcYaml)
	if err != nil {
		t.Fatalf("unable to read conf %v", err)
	}

	tried, collected, err := apicollect.GetNumberOfJobProfilesCollected(c)
	if err != nil {
		t.Fatalf("failed running job profile numbers generation\n%v", err)
	}
	if collected != 0 {
		t.Errorf("collected was supposed to be 0 but got %v", collected)
	}
	if tried != 3 {
		t.Errorf("tried was supposed to be 3 but got %v", tried)
	}
}

func TestGetNumberOfJobProfilesCollectedWIthServerUp(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if strings.HasSuffix(r.RequestURI, ".zip") {
			w.Header().Set("Content-Type", "application/octet-stream")

			// Write binary data to the response writer
			binaryData := []byte{0x12, 0x34, 0x56, 0x78}
			_, err := w.Write(binaryData)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
		} else {
			response := []byte(`{"message": "Hello, World!"}`)
			if _, err := w.Write(response); err != nil {
				t.Fatalf("unexpected error writing response %v", err)
			}
		}
	}))
	defer server.Close()

	overrides := make(map[string]string)
	confDir := filepath.Join(t.TempDir(), "ddcTest")
	err := os.Mkdir(confDir, 0700)
	if err != nil {
		t.Fatalf("missing conf dir %v", err)
	}
	tmpDir := t.TempDir()
	queriesDir := filepath.Join(tmpDir, "queries", "node1")
	err = os.MkdirAll(queriesDir, 0700) //"queries is from
	if err != nil {
		t.Fatalf("cant make queries dir %v", err)
	}
	if err := os.WriteFile(filepath.Join(queriesDir, "queries.json"), []byte(`
{"queryId":"123456","start":100,"outcome":"COMPLETED","queryType":"METADATA_REFRESH","queryCost":9000000000,"planningTime":0,"executionPlanningTime":440,"runningTime":10}
{"queryId":"abcdef","start":200,"outcome":"FAILED","queryType":"ODBC","queryCost":9001,"planningTime":1,"executionPlanningTime":350,"runningTime":5}
{"queryId":"dremio","start":300,"outcome":"CANCELLED","queryType":"REST","queryCost":9002,"planningTime":2,"executionPlanningTime":340,"runningTime":1235}
`), 0600); err != nil {
		t.Fatalf("unable to write queries.json %v", err)
	}

	ddcYaml := filepath.Join(confDir, "ddc.yaml")
	err = os.WriteFile(ddcYaml, []byte(fmt.Sprintf(`
dremio-log-dir: %v
dremio-conf-dir: %v
number-job-profiles: 2500
job-profiles-num-high-query-cost: 500
job-profiles-num-slow-exec: 1000
job-profiles-num-recent-errors: 500
job-profiles-num-slow-planning: 500
dremio-pat-token: my-pat-token
node-name: node1
number-threads: 4
tmp-output-dir: %v
dremio-endpoint: %v
`, LogDir(), ConfDir(), strings.ReplaceAll(tmpDir, "\\", "\\\\"), server.URL)), 0600)
	if err != nil {
		t.Fatalf("missing conf file %v", err)
	}
	c, err := conf.ReadConf(overrides, ddcYaml)
	if err != nil {
		t.Fatalf("unable to read conf %v", err)
	}

	tried, collected, err := apicollect.GetNumberOfJobProfilesCollected(c)
	if err != nil {
		t.Fatalf("failed running job profile numbers generation\n%v", err)
	}
	if collected != 0 {
		t.Errorf("collected was supposed to be 0 but got %v", collected)
	}
	if tried != 3 {
		t.Errorf("tried was supposed to be 3 but got %v", tried)
	}
}
