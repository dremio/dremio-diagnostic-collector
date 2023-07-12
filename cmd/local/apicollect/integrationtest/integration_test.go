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

package integrationtest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/apicollect"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/ddcio"
	"github.com/dremio/dremio-diagnostic-collector/cmd/root/collection"
	"github.com/dremio/dremio-diagnostic-collector/cmd/root/helpers"
	"github.com/dremio/dremio-diagnostic-collector/cmd/root/kubernetes"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
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
	if c != nil {
		if _, err := os.Stat(c.OutputDir()); err != nil {
			if os.IsNotExist(err) {
				return
			}
		}
		if err := os.RemoveAll(c.OutputDir()); err != nil {
			log.Printf("WARN unable to remove %v it may have to be manually cleaned up", c.OutputDir())
		}
	}

	yamlFile := filepath.Join("testdata", "dremio.yaml")
	cmdApply := exec.Command("kubectl", "delete", "-n", namespace, "-f", yamlFile)
	cmdApply.Stderr = os.Stderr
	cmdApply.Stdout = os.Stdout
	if err := cmdApply.Run(); err != nil {
		log.Printf("Error during kubectl apply: %v", err)
	}
	time.Sleep(time.Duration(15) * time.Second)
	cmdApply = exec.Command("kubectl", "delete", "namespace", namespace)
	cmdApply.Stderr = os.Stderr
	cmdApply.Stdout = os.Stdout
	if err := cmdApply.Run(); err != nil {
		log.Printf("Error during kubectl delete: %v", err)
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
collect-audit-log: true
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
`, dremioEndpoint, patToken, strings.ReplaceAll(tmpOutputDir, "\\", "\\\\"))
	if _, err := w.WriteString(yamlText); err != nil {
		log.Fatal(err)
	}
	return testDDCYaml
}

func getFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer l.Close()

	return l.Addr().(*net.TCPAddr).Port, nil
}

var namespace string

func TestMain(m *testing.M) {
	simplelog.InitLogger(4)
	exitCode := func() (exitCode int) {
		var err error

		// Define the name and type of the resource you are waiting for.
		ts := time.Now().Unix()
		namespace = fmt.Sprintf("ddc-test-%v", ts)
		cmdApply := exec.Command("kubectl", "create", "namespace", namespace)
		cmdApply.Stderr = os.Stderr
		cmdApply.Stdout = os.Stdout
		err = cmdApply.Run()
		if err != nil {
			log.Printf("Error during kubectl apply: %v", err)
		}

		yamlFile := filepath.Join("testdata", "dremio.yaml")
		// Execute the `kubectl apply` command.
		cmdApply = exec.Command("kubectl", "apply", "-n", namespace, "-f", yamlFile)
		cmdApply.Stderr = os.Stderr
		cmdApply.Stdout = os.Stdout
		err = cmdApply.Run()
		if err != nil {
			log.Printf("Error during kubectl apply: %v", err)
			return
		}
		// Give Kubernetes some extra time to get everything ready.
		time.Sleep(5 * time.Second)

		// Wait for the resource to become ready.

		fmt.Println("waiting on Dremio master!")
		cmdWait := exec.Command("kubectl", "-n", namespace, "wait", "pod", "dremio-master-0", "--for=condition=Ready", "--timeout=180s")
		//cmdWait.Stderr = os.Stderr
		//cmdWait.Stdout = os.Stdout
		err = cmdWait.Run()
		if err != nil {
			log.Printf("Error during kubectl wait: '%v'", err)
			return 1
		}

		// Give Kubernetes some extra time to get everything ready.
		time.Sleep(10 * time.Second)

		fmt.Println("Dremio master is now ready!")

		//kubectl portforward

		// Let the system choose a free port.
		dremioTestPort, err := getFreePort()
		if err != nil {
			log.Printf("Failed to find a free port: %v", err)
			return 1
		}

		// Start the port forwarding.
		cmd := exec.Command("kubectl", "port-forward", "dremio-master-0", fmt.Sprintf("%v:9047", dremioTestPort), "-n", namespace)
		if err := cmd.Start(); err != nil {
			log.Printf("Failed to start command: %v", err)
			return 1

		}

		// Ensure the command is stopped when main returns.
		defer func() {
			if err := cmd.Process.Kill(); err != nil {
				log.Printf("Failed to kill process: %v", err)
			}
		}()

		//give port foward time to work
		time.Sleep(5 * time.Second)

		dremioEndpoint := fmt.Sprintf("http://localhost:%v", dremioTestPort)

		res, err := http.Get(dremioEndpoint) //nolint
		if err != nil {
			log.Printf("error making http request: %s\n", err)
			return 1
		}
		expectedCode := 200
		if res.StatusCode != expectedCode {
			log.Printf("expected status code %v but instead got %v. Dremio is not ready", expectedCode, res.StatusCode)
			return 1
		}

		authRequest := &AuthRequest{
			Username: "dremio",
			Password: "dremio123",
		}
		body, err := json.Marshal(authRequest)
		if err != nil {
			log.Printf("Error marshaling JSON: %v", err)
			return 1
		}
		res, err = http.Post(fmt.Sprintf("http://localhost:%v/apiv2/login", dremioTestPort), "application/json", bytes.NewBuffer(body))
		if err != nil {
			log.Printf("error logging in to get token : %s\n", err)
			return 1
		}
		defer res.Body.Close()
		if res.StatusCode != expectedCode {
			text, err := io.ReadAll(res.Body)
			if err != nil {
				log.Printf("fatal attempt to decode body from dremio auth %v and unable to read body for debugging", err)
				return 1
			}
			log.Printf("body was %s", string(text))
			log.Printf("expected status code %v but instead got %v with message %v. Unable to get dremio PAT", expectedCode, res.StatusCode, res.Status)
			return 1
		}
		var authResponse AuthResponse
		err = json.NewDecoder(res.Body).Decode(&authResponse)
		if err != nil {
			text, err := io.ReadAll(res.Body)
			if err != nil {
				log.Printf("fatal attempt to decode body from dremio auth %v and unable to read body for debugging", err)
				return 1
			}
			log.Printf("body was %s", string(text))
			log.Printf("fatal attempt to decode body from dremio auth %v", err)
			return 1
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
			log.Printf("unable to create data source request")
			return 1
		}
		httpReq.Header.Add("Content-Type", "application/json")
		httpReq.Header.Add("Authorization", "_dremio"+dremioPATToken)
		res, err = http.DefaultClient.Do(httpReq)
		if err != nil {
			log.Printf("unable to create data source due to error %v", err)
			return 1
		}
		if res.StatusCode != 200 {
			log.Printf("expected status code 200 but instead got %v while trying to create source", res.StatusCode)
			return 1
		}
		tmpDirForConf, err := os.MkdirTemp("", "ddc")
		if err != nil {
			log.Printf("unexpected error %v", err)
			return 1
		}
		yamlLocation := writeConf(dremioPATToken, dremioEndpoint, tmpDirForConf)
		c, err = conf.ReadConf(make(map[string]string), filepath.Dir(yamlLocation))
		if err != nil {
			log.Printf("reading config %v", err)
			return 1
		}
		_, err = submitSQLQuery("CREATE TABLE tester.table1 AS SELECT a, b FROM (values (CAST(1 AS INTEGER), CAST(2 AS INTEGER))) as t(a, b)")
		if err != nil {
			log.Printf("unable to create table for testing %v", err)
			return 1
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

type MockTimeService struct {
	now time.Time
}

func (m *MockTimeService) GetNow() time.Time {
	return m.now
}

func TestClusterConfigCapture(t *testing.T) {
	ddcfs := helpers.NewRealFileSystem()
	now := time.Now()
	mockTimeService := &MockTimeService{
		now: now,
	}
	tmpDir := filepath.Join(t.TempDir(), "ddc-test")
	if err := os.MkdirAll(tmpDir, 0700); err != nil {
		t.Fatal(err)
	}
	baseDir := "hc-dir"
	if err := os.MkdirAll(filepath.Join(tmpDir, baseDir), 0700); err != nil {
		t.Fatal(err)
	}
	hc := &helpers.CopyStrategyHC{
		StrategyName: "healthcheck",
		BaseDir:      baseDir,
		TmpDir:       tmpDir,
		Fs:           ddcfs,
		TimeService:  mockTimeService,
	}
	k8s := kubernetes.NewKubectlK8sActions("kubectl", "", "", namespace)
	if err := collection.ClusterK8sExecute(namespace, hc, ddcfs, k8s, "kubectl"); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(tmpDir, baseDir, "kubernetes")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 21 {
		t.Errorf("expected to find 21 entries but found %v", len(entries))
	}
	for _, e := range entries {
		fs, err := e.Info()
		if err != nil {
			t.Fatal(err)
		}
		if fs.Size() == 0 {
			t.Errorf("file %v is empty", e.Name())
		}
	}
}

func TestCollectDremioSystemTables(t *testing.T) {
	if err := os.MkdirAll(c.SystemTablesOutDir(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := apicollect.RunCollectDremioSystemTables(c); err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	entries, err := os.ReadDir(c.SystemTablesOutDir())
	if err != nil {
		t.Fatal(err)
	}
	//we substract 3 of the jobs that fail due to missing features in oss
	// - sys.privileges
	// - sys.membership
	// - sys.roles
	// and system.tables because it seems to not be setup
	// - sys.\"tables\"
	expectedEntries := len(c.Systemtables()) - 4
	actualEntries := len(entries)
	if actualEntries == 0 {
		t.Error("expected more than 0 entries")
	}
	if actualEntries != expectedEntries {
		t.Errorf("expected %v but was %v", expectedEntries, actualEntries)
	}
}

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
			return "", fmt.Errorf("fatal attempt to make job api call %v and unable to read body for debugging", err)
		}
		simplelog.Debugf("body was %s", string(text))
		return "", fmt.Errorf("expected status code greater than 299 but instead got %v while trying to run sql %v ", res.StatusCode, query)
	}
	var jobResponse JobAPIResponse
	err = json.NewDecoder(res.Body).Decode(&jobResponse)
	if err != nil {
		text, err := io.ReadAll(res.Body)
		if err != nil {
			return "", fmt.Errorf("fatal attempt to decode body from dremio job api call %v and unable to read body for debugging", err)
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
	cmdApply := exec.Command("kubectl", "--namespace", namespace, "exec", "-it", "dremio-master-0", "--", "ls", "-la", "/opt/dremio/data/logs/")
	cmdApply.Stderr = os.Stderr
	cmdApply.Stdout = os.Stdout
	err := cmdApply.Run()
	if err != nil {
		t.Fatalf("Error during kubectl ls: %v", err)
	}
	cmdApply = exec.Command("kubectl", "--namespace", namespace, "cp", "dremio-master-0:/opt/dremio/data/logs/queries.json", filepath.Join(strings.Replace(c.QueriesOutDir(), "C:", "", 1), "queries.json"))
	cmdApply.Stderr = os.Stderr
	cmdApply.Stdout = os.Stdout
	err = cmdApply.Run()
	if err != nil {
		t.Fatalf("Error during kubectl cp: %v", err)
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

func TestCollectContainerLogs(t *testing.T) {
	ddcfs := helpers.NewRealFileSystem()
	now := time.Now()
	mockTimeService := &MockTimeService{
		now: now,
	}
	tmpDir := filepath.Join(t.TempDir(), "ddc-test")
	if err := os.MkdirAll(tmpDir, 0700); err != nil {
		t.Fatal(err)
	}
	baseDir := "hc-dir"
	if err := os.MkdirAll(filepath.Join(tmpDir, baseDir), 0700); err != nil {
		t.Fatal(err)
	}
	hc := &helpers.CopyStrategyHC{
		StrategyName: "healthcheck",
		BaseDir:      baseDir,
		TmpDir:       tmpDir,
		Fs:           ddcfs,
		TimeService:  mockTimeService,
	}
	pods := []string{"dremio-master-0", "dremio-executor-0"}
	if err := collection.GetClusterLogs(namespace, hc, ddcfs, "kubectl", pods); err != nil {
		t.Fatal(err)
	}

	// We expect to find the following logs:
	/*

		    dremio-executor-0-chown-cloudcache-directory.out
		    dremio-executor-0-chown-data-directory.out
		    dremio-executor-0-dremio-executor.out
		    dremio-executor-0-wait-for-zookeeper.out
		    dremio-master-0-chown-data-directory.out
		    dremio-master-0-dremio-master-coordinator.out
		    dremio-master-0-start-only-one-dremio-master.out
		    dremio-master-0-upgrade-task.out
		    dremio-master-0-wait-for-zookeeper.out

			The following files are usually empty

			dremio-executor-0-chown-cloudcache-directory.out
		    dremio-executor-0-chown-data-directory.out
		    dremio-master-0-chown-data-directory.out
		    dremio-master-0-start-only-one-dremio-master.out

	*/

	expectedFiles := []string{"dremio-executor-0-chown-data-directory.out", "dremio-executor-0-chown-cloudcache-directory.out", "dremio-executor-0-dremio-executor.out", "dremio-executor-0-wait-for-zookeeper.out", "dremio-master-0-chown-data-directory.out", "dremio-master-0-dremio-master-coordinator.out", "dremio-master-0-start-only-one-dremio-master.out", "dremio-master-0-upgrade-task.out", "dremio-master-0-wait-for-zookeeper.out"}
	expectedEmptyFiles := []string{"dremio-executor-0-chown-data-directory.out", "dremio-executor-0-chown-cloudcache-directory.out", "dremio-master-0-chown-data-directory.out", "dremio-master-0-start-only-one-dremio-master.out"}
	dir := filepath.Join(tmpDir, baseDir, "kubernetes", "container-logs")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		t.Logf("directories %v", entry.Name())
	}
	if len(entries) != 9 {
		t.Errorf("expected to find 9 entries but found %v", len(entries))
	}
	foundFiles := []string{}
	foundEmptyFiles := []string{}
	for _, e := range entries {
		fs, err := e.Info()
		if err != nil {
			t.Fatal(err)
		}
		if fs.Size() == 0 {
			foundEmptyFiles = append(foundEmptyFiles, fs.Name())
		}
		foundFiles = append(foundFiles, fs.Name())
	}

	// sort the strings before checking equality
	sort.Strings(foundEmptyFiles)
	sort.Strings(expectedEmptyFiles)
	sort.Strings(foundFiles)
	sort.Strings(expectedFiles)

	if !reflect.DeepEqual(expectedEmptyFiles, foundEmptyFiles) {
		t.Errorf("Expected the following files to be empty:\n %v\n But found the following:\n %v", expectedEmptyFiles, foundEmptyFiles)
	}

	if !reflect.DeepEqual(foundFiles, expectedFiles) {
		t.Errorf("Expected the following files to be present:\n %v\n But found the following:\n %v", expectedFiles, foundFiles)
	}
}
