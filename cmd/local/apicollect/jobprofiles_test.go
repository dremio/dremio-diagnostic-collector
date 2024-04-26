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
	"github.com/dremio/dremio-diagnostic-collector/pkg/collects"
)

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
	sysTableDir := filepath.Join(tmpDir, "system-tables", "node1")
	err = os.MkdirAll(sysTableDir, 0700)
	if err != nil {
		t.Fatalf("cant make system-tables dir %v", err)
	}
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
	c, err := conf.ReadConf(overrides, ddcYaml, collects.StandardCollection)
	if err != nil {
		t.Fatalf("unable to read conf %v", err)
	}

	// Get number of profiles to collect based on queries.json
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

	if err := os.WriteFile(filepath.Join(sysTableDir, "sys.jobs_recent.json"), []byte(`
{"rows": [
	{"job_id": "Query1", "status": "FAILED", "query_type": "REST", "submitted_epoch_millis": 1713968783248,	"planning_start_epoch_millis": 0, "execution_start_epoch_millis": 0, "final_state_epoch_millis": 1713968783250, "planner_estimated_cost": 2.8234000035E5},
	{"job_id": "Query2", "status": "COMPLETED", "query_type": "REST", "submitted_epoch_millis": 1714033458006, "planning_start_epoch_millis": 1714033458008, "execution_start_epoch_millis": 1714033458042, "final_state_epoch_millis": 1714033458061, "planner_estimated_cost": 3.8154000035E9}
]}
`), 0600); err != nil {
		t.Fatalf("unable to write sys.jobs_recent.json %v", err)
	}

	// Get number of profiles to collect based on sys.jobs_recent
	tried, collected, err = apicollect.GetNumberOfJobProfilesCollected(c)
	if err != nil {
		t.Fatalf("failed running job profile numbers generation\n%v", err)
	}
	if collected != 0 {
		t.Errorf("collected was supposed to be 0 but got %v", collected)
	}
	if tried != 2 {
		t.Errorf("tried was supposed to be 2 but got %v", tried)
	}
}
