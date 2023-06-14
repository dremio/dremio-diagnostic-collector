//go:build !windows

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

// apicollect provides all the methods that collect via the API, this is a substantial part of the activities of DDC so it gets it's own package
package apicollect

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/apicollect"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/ddcio"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
	"github.com/spf13/pflag"
)

type AuthResponse struct {
	Token string `json:"token"`
}

type AuthRequest struct {
	Username string `json:"userName"`
	Password string `json:"password"`
}

type JobAPIResponse struct {
	ID string `json:"id"`
}

var c *conf.CollectConf

func cleanupOutput() {
	if err := os.RemoveAll(c.OutputDir()); err != nil {
		log.Printf("WARN unable to remove %v it may have to be manually cleaned up", c.OutputDir())
	}
}

func writeConf(patToken, dremioEndpoint, tmpOutputDir string) string {
	if err := os.MkdirAll(tmpOutputDir, 0700); err != nil {
		log.Fatal(err)
	}
	testDDCYaml := filepath.Join(tmpOutputDir, "ddc.yaml")
	w, err := os.Create(testDDCYaml)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := w.Close(); err != nil {
			log.Printf("WARN: unable to close %v with reason '%v'", testDDCYaml, err)
		}
	}()
	yamlText := fmt.Sprintf(`verbose: vvvv
collect-acceleration-log: true
collect-access-log: true
dremio-gclogs-dir: ""
dremio-log-dir: /opt/dremio/data/logs
dremio-conf-dir: /opt/dremio/conf
dremio-rocksdb-dir: /opt/dremio/data/db
number-threads: 2
dremio-endpoint: %v
dremio-username: dremio
dremio-pat-token: %v
collect-dremio-configuration: true
number-job-profiles: 25
capture-heap-dump: false
accept-collection-consent: true
tmp-output-dir: %v
node-metrics-collect-duration-seconds: 10
"
`, dremioEndpoint, patToken, tmpOutputDir)
	if _, err := w.WriteString(yamlText); err != nil {
		log.Fatal(err)
	}
	return testDDCYaml
}
func GetRootProjectDir() (string, error) {
	cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	rootDir := strings.TrimSpace(string(output))
	return rootDir, nil
}

var rootDir string

func TestMain(m *testing.M) {
	simplelog.InitLogger(4)
	exitCode := func() (exitCode int) {
		var err error
		rootDir, err = GetRootProjectDir()
		if err != nil {
			log.Fatal(err)
		}
		if err := ddcio.CopyFile(filepath.Join("testdata", "conf", "dremio.conf"), filepath.Join(rootDir, "server-install", "conf", "dremio.conf")); err != nil {
			log.Fatal(err)
		}
		dremioExec := filepath.Join(rootDir, "server-install", "bin", "dremio")
		cmd := exec.Command(dremioExec, "start")
		// Attach to standard output and error
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		// Start the process
		if err := cmd.Start(); err != nil {
			log.Fatal(err)
		}

		fmt.Println("sleeping 60 seconds so that dremio can start")
		time.Sleep(60 * time.Second)

		defer func() {
			//send shutdown
			shutdownCmd := exec.Command(dremioExec, "stop")
			// Attach to standard output and error
			shutdownCmd.Stdout = os.Stdout
			shutdownCmd.Stderr = os.Stderr
			// Start the process
			if err := shutdownCmd.Start(); err != nil {
				log.Print(err)
			}
		}()

		dremioTestPort := 9047
		if err := os.RemoveAll(filepath.Join("/tmp", "dremio-source")); err != nil {
			log.Printf("unable to remove dremio-source do to error %v", err)
		}

		if err := os.MkdirAll("/tmp/dremio-source", 0700); err != nil {
			log.Fatalf("need to make the source dir to do the test %v", err)
		}
		defer func() {
			if err := os.RemoveAll(filepath.Join("/tmp", "dremio-source")); err != nil {
				log.Printf("unable to remove dremio-source do to error %v", err)
			}
		}()

		dremioEndpoint := fmt.Sprintf("http://localhost:%v", dremioTestPort)

		res, err := http.Get(dremioEndpoint) //nolint
		if err != nil {
			log.Fatalf("error making http request: %s\n", err)
		}
		expectedCode := 200
		if res.StatusCode != expectedCode {
			log.Fatalf("expected status code %v but instead got %v. Dremio is not ready", expectedCode, res.StatusCode)
		}

		authRequest := &AuthRequest{
			Username: "dremio",
			Password: "dremio123",
		}
		body, err := json.Marshal(authRequest)
		if err != nil {
			log.Fatalf("Error marshaling JSON: %v", err)
		}
		res, err = http.Post(fmt.Sprintf("http://localhost:%v/apiv2/login", dremioTestPort), "application/json", bytes.NewBuffer(body))
		if err != nil {
			log.Fatalf("error logging in to get token : %s\n", err)
		}
		defer res.Body.Close()
		if res.StatusCode != expectedCode {
			text, err := io.ReadAll(res.Body)
			if err != nil {
				log.Fatalf("fatal attempt to decode body from dremio auth %v and unable to read body for debugging", err)
			}
			log.Printf("body was %s", string(text))
			log.Fatalf("expected status code %v but instead got %v with message %v. Unable to get dremio PAT", expectedCode, res.StatusCode, res.Status)
		}
		var authResponse AuthResponse
		err = json.NewDecoder(res.Body).Decode(&authResponse)
		if err != nil {
			text, err := io.ReadAll(res.Body)
			if err != nil {
				log.Fatalf("fatal attempt to decode body from dremio auth %v and unable to read body for debugging", err)
			}
			log.Printf("body was %s", string(text))
			log.Fatalf("fatal attempt to decode body from dremio auth %v", err)
		}
		dremioPATToken := authResponse.Token

		nasSource := `{
			"metadataPolicy": {
				"authTTLMs":86400000,
        		"namesRefreshMs":3600000,
        		"datasetRefreshAfterMs": 3600000,
        		"datasetExpireAfterMs": 10800000,
        		"datasetUpdateMode":"PREFETCH_QUERIED",
        		"deleteUnavailableDatasets": true,
        		"autoPromoteDatasets": true
        	},
			"config": {
			  	"path": "/tmp/dremio-source/",
			  	"defaultCtasFormat": "ICEBERG"
			},
			"entityType": "source",
			"type": "NAS",
			"name": "tester"
		  }`
		httpReq, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://localhost:%v/apiv3/catalog", dremioTestPort), bytes.NewBuffer([]byte(nasSource)))
		if err != nil {
			log.Fatalf("unable to create data source request")
		}
		httpReq.Header.Add("Content-Type", "application/json")
		httpReq.Header.Add("Authorization", "_dremio"+dremioPATToken)
		res, err = http.DefaultClient.Do(httpReq)
		if err != nil {
			log.Fatalf("unable to create data source due to error %v", err)
		}
		if res.StatusCode != 200 {
			log.Fatalf("expected status code 200 but instead got %v while trying to create source", res.StatusCode)
		}
		tmpDirForConf, err := os.MkdirTemp("", "ddc")
		if err != nil {
			log.Fatal(err)
		}
		yamlLocation := writeConf(dremioPATToken, dremioEndpoint, tmpDirForConf)
		c, err = conf.ReadConf(make(map[string]*pflag.Flag), filepath.Dir(yamlLocation))
		if err != nil {
			log.Fatalf("reading config %v", err)
		}
		_, err = submitSQLQuery("CREATE TABLE tester.table1 AS SELECT a, b FROM (values (CAST(1 AS INTEGER), CAST(2 AS INTEGER))) as t(a, b)")
		if err != nil {
			log.Fatalf("unable to create table for testing %v", err)
		}
		return m.Run()
	}()

	// handle panic
	if r := recover(); r != nil {
		// handle the panic and terminate gracefully
		// ...
		exitCode = 1
	}
	cleanupOutput()
	os.Exit(exitCode)
}

// until we add back the dremio-ee image
// func TestCollectWlm(t *testing.T) {
// 	err := runCollectWLM(c)
// 	if err != nil {
// 		t.Errorf("unexpected error %v", err)
// 	}
// }

func TestCollectKVReport(t *testing.T) {
	kvStoreDir := c.KVstoreOutDir()
	err := os.MkdirAll(kvStoreDir, 0755)
	if err != nil {
		t.Errorf("unable to make kvstore output dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(kvStoreDir); err != nil {
			t.Logf("error removing kvstore out dir %v", err)
		}
	}()
	err = apicollect.RunCollectKvReport(c)
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
}

// TODO figure out why this is failing
// func TestCollectDremioSystemTables(t *testing.T) {
// 	err := collectDremioSystemTables()
// 	if err != nil {
// 		t.Errorf("unexpected error %v", err)
// 	}
// }

func TestDownloadJobProfile(t *testing.T) {
	if err := os.MkdirAll(c.JobProfilesOutDir(), 0700); err != nil {
		t.Errorf("unable to setup directory for creation with error %v", err)
	}
	if err := ddcio.DeleteDirContents(c.JobProfilesOutDir()); err != nil {
		t.Logf("failed clearing out directory %v with error %v", c.JobProfilesOutDir(), err)
	}
	jobid, err := submitSQLQuery("SELECT * FROM tester.table1")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Second)
	err = apicollect.DownloadJobProfile(c, jobid)
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
}
func submitSQLQuery(query string) (string, error) {
	sql := fmt.Sprintf(`{
		"sql": "%v"
	}`, query)
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%v/api/v3/sql/", c.DremioEndpoint()), bytes.NewBuffer([]byte(sql)))
	if err != nil {
		return "", fmt.Errorf("unable to run sql %v", err)
	}
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "_dremio"+c.DremioPATToken())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("unable to run sql %v due to error  %v", query, err)
	}
	defer res.Body.Close()
	if res.StatusCode > 299 {
		text, err := io.ReadAll(res.Body)
		if err != nil {
			log.Fatalf("fatal attempt to make job api call %v and unable to read body for debugging", err)
		}
		simplelog.Debugf("body was %s", string(text))
		return "", fmt.Errorf("expected status code greater than 299 but instead got %v while trying to run sql %v ", res.StatusCode, query)
	}
	var jobResponse JobAPIResponse
	err = json.NewDecoder(res.Body).Decode(&jobResponse)
	if err != nil {
		text, err := io.ReadAll(res.Body)
		if err != nil {
			log.Fatalf("fatal attempt to decode body from dremio job api call %v and unable to read body for debugging", err)
		}
		simplelog.Debugf("body was %s", string(text))
		return "", fmt.Errorf("fatal attempt to decode body from dremio job api %v", err)
	}
	return jobResponse.ID, nil
}

func TestValidateAPICredentials(t *testing.T) {
	err := apicollect.ValidateAPICredentials(c)
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
}

func TestValidateCollectJobProfiles(t *testing.T) {
	for i := 0; i < 25; i++ {
		_, err := submitSQLQuery("SELECT a,b FROM tester.table1")
		if err != nil {
			t.Fatalf("failed query #%v with error %v", i+1, err)
		}
	}
	if err := os.MkdirAll(c.JobProfilesOutDir(), 0700); err != nil {
		t.Errorf("unable to setup directory for creation with error %v", err)
	}
	if err := os.MkdirAll(c.QueriesOutDir(), 0700); err != nil {
		t.Errorf("unable to setup directory for creation with error %v", err)
	}
	if err := ddcio.DeleteDirContents(c.JobProfilesOutDir()); err != nil {
		t.Logf("failed clearing out directory %v with error %v", c.JobProfilesOutDir(), err)
	}
	if err := ddcio.CopyFile(filepath.Join(rootDir, "server-install", "log", "queries.json"), filepath.Join(c.QueriesOutDir(), "queries.json")); err != nil {
		t.Errorf("failed moving queries.json to folder to allow download of jobs due to error %v", err)
	}
	defer func() {
		if err := os.RemoveAll(c.QueriesOutDir()); err != nil {
			t.Logf("unable to clean up dir %v due to error %v", c.QueriesOutDir(), err)
		}
		if err := os.RemoveAll(c.JobProfilesOutDir()); err != nil {
			t.Logf("unable to clean up dir %v due to error %v", c.JobProfilesOutDir(), err)
		}
	}()
	entries, err := os.ReadDir(c.JobProfilesOutDir())
	if err != nil {
		t.Errorf("unable to read dir %v due to error %v", c.JobProfilesOutDir(), err)
	}
	filesInDirBefore := []string{}
	for _, e := range entries {
		filesInDirBefore = append(filesInDirBefore, e.Name())
	}
	t.Logf("before running the collection - %v dir has the following files %v", c.JobProfilesOutDir(), strings.Join(filesInDirBefore, ", "))
	numberFilesInDir := len(filesInDirBefore)
	tried, _, err := apicollect.GetNumberOfJobProfilesCollected(c)
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	entries, err = os.ReadDir(c.JobProfilesOutDir())
	if err != nil {
		t.Errorf("unable to read dir %v due to error %v", c.JobProfilesOutDir(), err)
	}
	filesInDirAfter := []string{}
	for _, e := range entries {
		filesInDirAfter = append(filesInDirAfter, e.Name())
	}
	t.Logf("after running the collection - %v dir has the following files %v", c.JobProfilesOutDir(), strings.Join(filesInDirAfter, ", "))
	afterJobNumberFilesInDir := len(filesInDirAfter)
	//should have collected at the number of tried job profiles as duplicates may be less than the number asked for
	profilesCollected := afterJobNumberFilesInDir - numberFilesInDir
	if profilesCollected != tried {
		t.Errorf("expected at %v job profiles to be collected but there are %v", tried, profilesCollected)
	}
	//this is just hoping based on math, but it should be very rare that we have all duplicates out of 25
	if tried < 2 {
		t.Errorf("expected at least 3 tried but was %v", tried)
	}
}
