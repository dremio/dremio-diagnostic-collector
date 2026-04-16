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

// package kube contains the kuberneters integration tests
package kube

import (
	"bufio"
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

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/archive"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/tests"
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

var (
	setupIsRun = false
	outputDir  string
)

func cleanupOutput() {
	mustRemove := true
	if _, err := os.Stat(outputDir); err != nil {
		if os.IsNotExist(err) {
			mustRemove = false
		}
	}
	if mustRemove {
		if err := os.RemoveAll(outputDir); err != nil {
			log.Printf("WARN unable to remove %v it may have to be manually cleaned up", outputDir)
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

func getFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer func() { _ = l.Close() }()

	return l.Addr().(*net.TCPAddr).Port, nil
}

var (
	namespace      string
	dremioPATToken string
)

func TestMain(m *testing.M) {
	isIntegration := os.Getenv("SKIP_INTEGRATION_SETUP")
	if isIntegration == "true" {
		return
	}

	exitCode := func() (exitCode int) {
		var err error

		var buf bytes.Buffer
		// check to see if there is already a namespaces that matches the formula and delete it
		cmdApply := exec.Command("kubectl", "get", "namespace")
		cmdApply.Stderr = os.Stderr
		cmdApply.Stdout = &buf
		err = cmdApply.Run()
		if err != nil {
			log.Printf("Error during kubectl apply: %v", err)
			return 1
		}
		nsScanner := bufio.NewScanner(&buf)
		for nsScanner.Scan() {
			line := nsScanner.Text()
			tokens := strings.Split(line, " ")
			if len(tokens) > 0 {
				existingNS := tokens[0]
				if strings.HasPrefix(existingNS, "ddc-test-") {
					// delete it
					log.Printf("found an existing namespace %v deleting it", existingNS)
					cmdApply = exec.Command("kubectl", "delete", "namespace", existingNS)
					cmdApply.Stderr = os.Stderr
					cmdApply.Stdout = os.Stdout
					err = cmdApply.Run()
					if err != nil {
						log.Printf("Error during kubectl delete ns: %v", err)
					}
				}
			}
		}
		// Define the name and type of the resource you are waiting for.
		ts := time.Now().Unix()
		namespace = fmt.Sprintf("ddc-test-%v", ts)
		cmdApply = exec.Command("kubectl", "create", "namespace", namespace)
		cmdApply.Stderr = os.Stderr
		cmdApply.Stdout = os.Stdout
		err = cmdApply.Run()
		if err != nil {
			log.Printf("Error during kubectl apply: %v", err)
			return 1
		}

		yamlFile := filepath.Join("testdata", "dremio.yaml")
		// Execute the `kubectl apply` command.
		cmdApply = exec.Command("kubectl", "apply", "-n", namespace, "-f", yamlFile)
		cmdApply.Stderr = os.Stderr
		cmdApply.Stdout = os.Stdout
		err = cmdApply.Run()
		if err != nil {
			log.Printf("Error during kubectl apply: %v", err)
			return 1
		}
		// Give Kubernetes some extra time to get everything ready.
		time.Sleep(5 * time.Second)

		// Wait for the resource to become ready.

		fmt.Println("waiting on Dremio master!")
		cmdWait := exec.Command("kubectl", "-n", namespace, "wait", "pod", "dremio-master-0", "--for=condition=Ready", "--timeout=180s")
		// cmdWait.Stderr = os.Stderr
		// cmdWait.Stdout = os.Stdout
		err = cmdWait.Run()
		if err != nil {
			log.Printf("Error during kubectl wait: '%v'", err)
			return 1
		}

		// Give Kubernetes some extra time to get everything ready.
		time.Sleep(20 * time.Second)

		fmt.Println("Dremio master is now ready!")

		// kubectl portforward

		// Let the system choose a free port.
		dremioTestPort, err := getFreePort()
		if err != nil {
			log.Printf("Failed to find a free port: %v", err)
			return 1
		}

		// Start the port forwarding.
		command := exec.Command("kubectl", "port-forward", "dremio-master-0", fmt.Sprintf("%v:9047", dremioTestPort), "-n", namespace)
		if err := command.Start(); err != nil {
			log.Printf("Failed to start port-forward command: %v", err)
			return 1
		}
		log.Printf("port-forward to port %v successful", dremioTestPort)

		// Ensure the command is stopped when main returns.
		defer func() {
			if err := command.Process.Kill(); err != nil {
				log.Printf("Failed to kill process: %v", err)
			}
		}()

		// give port forward time to work
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
		defer func() { _ = res.Body.Close() }()
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
		dremioPATToken = authResponse.Token

		nasSource := `{
			"metadataPolicy": {
				"authTTLMs":86400000,
        		"namesRefreshMs":3600000,
        		"datasetRefreshAfterMs": 3600000,
        		"datasetExpireAfterMs": 10800000,
        		"datasetUpdateMode":"PREFETCH_QUERID",
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
			log.Printf("unable to create data source: %v", err)
			return 1
		}
		if res.StatusCode != http.StatusOK {
			log.Printf("expected status code 200 but instead got %v while trying to create source", res.StatusCode)
			return 1
		}
		tmpDirForConf, err := os.MkdirTemp("", "ddc")
		if err != nil {
			log.Printf("unexpected error %v", err)
			return 1

		}
		defer func() {
			if err := os.RemoveAll(tmpDirForConf); err != nil {
				log.Printf("unable to clean up dir %v: %v", tmpDirForConf, err)
			}
		}()
		log.Printf("the directory for conf was %v", tmpDirForConf)
		entries, err := os.ReadDir(tmpDirForConf)
		if err != nil {
			log.Printf("unable to read the conf dir %v: %v", tmpDirForConf, err)
			return 1
		}
		for _, e := range entries {
			log.Printf("the %v in directory %v", e.Name(), tmpDirForConf)
		}
		_, err = submitSQLQuery("CREATE TABLE tester.table1 AS SELECT a, b FROM (values (CAST(1 AS INTEGER), CAST(2 AS INTEGER))) as t(a, b)", dremioEndpoint, dremioPATToken)
		if err != nil {
			log.Printf("unable to create table for testing %v", err)
			return 1
		}
		for i := 0; i < 25; i++ {
			_, err := submitSQLQuery("SELECT a,b FROM tester.table1", dremioEndpoint, dremioPATToken)
			if err != nil {
				log.Printf("failed query #%v with error %v", i+1, err)
				return 1
			}
		}
		setupIsRun = true
		return m.Run()
	}()

	// handle panic
	if r := recover(); r != nil {
		// handle the panic and terminate gracefully
		// ...
		fmt.Printf("panic occurred: %v\n", r)
		exitCode = 1
	}
	cleanupOutput()
	os.Exit(exitCode)
}

func TestValidateTestWasCorrectlyRun(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testing in short mode")
	}
	if !setupIsRun {
		t.Error("integration tests broken, nothing was run, if all other tests are passing this means there is an error in setup")
	}
}

func TestRemoteCollectOnK8s(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testing in short mode")
	}
	var err error
	tmpOutputDir := "/opt/dremio/data/ddc-tmp-out"
	tgzFile := filepath.Join(t.TempDir(), "diag.tgz")
	outputDir = tmpOutputDir

	args := []string{"ddc", "collect", "k8s", "standard", "--enable-kubectl", "--collect-container-logs", "--namespace", namespace, "--output-file", tgzFile}
	err = cmd.Execute(args)
	if err != nil {
		t.Fatalf("unable to run collect: %v", err)
	}
	log.Printf("remote collect complete now verifying the results")
	testOut := filepath.Join(t.TempDir(), "ddcout")
	err = os.Mkdir(testOut, 0o700)
	if err != nil {
		t.Fatalf("could not make test out dir %v", err)
	}
	log.Printf("now in the test we are extracting tarball %v to %v", tgzFile, testOut)

	if err := archive.ExtractTarGz(tgzFile, testOut); err != nil {
		t.Fatalf("could not extract tgz %v to dir %v: %v", tgzFile, testOut, err)
	}

	t.Logf("now we are reading the %v dir", testOut)
	entries, err := os.ReadDir(testOut)
	if err != nil {
		t.Fatalf("uanble to read dir %v: %v", testOut, err)
	}
	hcDir := ""
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
		if e.IsDir() {
			hcDir = filepath.Join(testOut, e.Name())
			t.Logf("now found the health check directory which is %v", hcDir)
		}
	}

	if len(names) != 2 {
		t.Fatalf("expected 2 entries but had %v", strings.Join(names, ","))
	}
	tests.AssertFileHasContent(t, filepath.Join(testOut, "summary.json"))

	// check k8s files
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "cronjob.json"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "daemonset.json"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "deployments.json"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "endpoints.json"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "events.json"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "hpa.json"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "ingress.json"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "job.json"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "limitrange.json"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "nodes.json"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "pc.json"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "pdb.json"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "pods.json"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "pv.json"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "pvc.json"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "replicaset.json"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "resourcequota.json"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "sc.json"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "service.json"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "statefulsets.json"))

	expectedFiles := []string{
		"dremio-executor-1-chown-data-directory.txt",
		"dremio-executor-1-chown-cloudcache-directory.txt",
		"dremio-executor-1-dremio-executor.txt",
		"dremio-executor-1-wait-for-zookeeper.txt",
		"dremio-executor-0-chown-data-directory.txt",
		"dremio-executor-0-chown-cloudcache-directory.txt",
		"dremio-executor-0-dremio-executor.txt",
		"dremio-executor-0-wait-for-zookeeper.txt",
		"dremio-master-0-chown-data-directory.txt",
		"dremio-master-0-dremio-master-coordinator.txt",
		"dremio-master-0-start-only-one-dremio-master.txt",
		"dremio-master-0-upgrade-task.txt",
		"dremio-master-0-wait-for-zookeeper.txt",
	}

	expectedEmptyFiles := []string{
		"dremio-executor-1-chown-data-directory.txt",
		"dremio-executor-1-chown-cloudcache-directory.txt",
		"dremio-executor-0-chown-data-directory.txt",
		"dremio-executor-0-chown-cloudcache-directory.txt",
		"dremio-master-0-chown-data-directory.txt",
		"dremio-master-0-start-only-one-dremio-master.txt",
	}
	dir := filepath.Join(hcDir, "kubernetes", "container-logs")
	entries, err = os.ReadDir(dir)
	if err != nil {
		t.Fatalf("unable to read dir %v: %v", dir, err)
	}
	for _, entry := range entries {
		t.Logf("directories %v", entry.Name())
	}
	expectedEntries := 13
	if len(entries) != expectedEntries {
		t.Errorf("expected to find %v entries but found %v", expectedEntries, len(entries))
	}
	foundFiles := []string{}
	foundEmptyFiles := []string{}
	compareEmptyFiles := []string{}
	for _, e := range entries {
		fs, err := e.Info()
		if err != nil {
			t.Fatalf("error getting entry %v info: %v", e, err)
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

	for _, expectedEmptyFile := range expectedEmptyFiles {
		for _, foundEmptyFile := range foundEmptyFiles {
			if expectedEmptyFile == foundEmptyFile {
				compareEmptyFiles = append(compareEmptyFiles, expectedEmptyFile)
			}
		}
	}
	if !reflect.DeepEqual(expectedEmptyFiles, compareEmptyFiles) {
		t.Errorf("Expected the following files to be empty:\n %v\n But found the following:\n %v", expectedEmptyFiles, compareEmptyFiles)
	}

	if !reflect.DeepEqual(foundFiles, expectedFiles) {
		t.Errorf("Expected the following files to be present:\n %v\n But found the following:\n %v", expectedFiles, foundFiles)
	}
	replicas := 2
	// check server.logs - only if they exist (test cluster may not produce them)
	if _, err := os.Stat(filepath.Join(hcDir, "logs", "dremio-master-0", "server.log.gz")); err == nil {
		tests.AssertFileHasContent(t, filepath.Join(hcDir, "logs", "dremio-master-0", "server.log.gz"))
	}
	for i := 0; i < replicas; i++ {
		host := fmt.Sprintf("dremio-executor-%v", i)
		if _, err := os.Stat(filepath.Join(hcDir, "logs", host, "server.log.gz")); err == nil {
			tests.AssertFileHasContent(t, filepath.Join(hcDir, "logs", host, "server.log.gz"))
		}
	}
	// check server.json logs - only if they exist
	if _, err := os.Stat(filepath.Join(hcDir, "logs", "dremio-master-0", "server.json.gz")); err == nil {
		tests.AssertFileHasContent(t, filepath.Join(hcDir, "logs", "dremio-master-0", "server.json.gz"))
	}
	for i := 0; i < replicas; i++ {
		host := fmt.Sprintf("dremio-executor-%v", i)
		if _, err := os.Stat(filepath.Join(hcDir, "logs", host, "server.json.gz")); err == nil {
			tests.AssertFileHasContent(t, filepath.Join(hcDir, "logs", host, "server.json.gz"))
		}
	}
	// check queries.json - only if it exists (test cluster may not produce it)
	if _, err := os.Stat(filepath.Join(hcDir, "queries", "dremio-master-0", "queries.json.gz")); err == nil {
		tests.AssertFileHasContent(t, filepath.Join(hcDir, "queries", "dremio-master-0", "queries.json.gz"))
	}
	// check conf files

	tests.AssertFileHasContent(t, filepath.Join(hcDir, "configuration", "dremio-master-0", "dremio.conf"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "configuration", "dremio-master-0", "dremio-env"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "configuration", "dremio-master-0", "logback.xml"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "configuration", "dremio-master-0", "logback-access.xml"))

	for i := 0; i < replicas; i++ {
		host := fmt.Sprintf("dremio-executor-%v", i)
		tests.AssertFileHasContent(t, filepath.Join(hcDir, "configuration", host, "dremio.conf"))
		tests.AssertFileHasContent(t, filepath.Join(hcDir, "configuration", host, "dremio-env"))
		tests.AssertFileHasContent(t, filepath.Join(hcDir, "configuration", host, "logback.xml"))
		tests.AssertFileHasContent(t, filepath.Join(hcDir, "configuration", host, "logback-access.xml"))
	}

	// check nodeinfo files
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "node-info", "dremio-master-0", "diskusage.txt"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "node-info", "dremio-master-0", "jvm_settings.txt"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "node-info", "dremio-master-0", "os_info.txt"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "node-info", "dremio-master-0", "rocksdb_disk_allocation.txt"))

	for i := 0; i < replicas; i++ {
		host := fmt.Sprintf("dremio-executor-%v", i)
		tests.AssertFileHasContent(t, filepath.Join(hcDir, "node-info", host, "diskusage.txt"))
		tests.AssertFileHasContent(t, filepath.Join(hcDir, "node-info", host, "jvm_settings.txt"))
		tests.AssertFileHasContent(t, filepath.Join(hcDir, "node-info", host, "os_info.txt"))
	}

	// check file contents
	t.Logf("checking file %v", filepath.Join(hcDir, "node-info", "dremio-master-0", "os_info.txt"))
	tests.AssertFileHasExpectedLines(t, []string{">>> mount", ">>> lsblk"}, filepath.Join(hcDir, "node-info", "dremio-master-0", "os_info.txt"))
	for i := 0; i < replicas; i++ {
		host := fmt.Sprintf("dremio-executor-%v", i)
		t.Logf("checking file %v", filepath.Join(hcDir, "node-info", host, "os_info.txt"))
		tests.AssertFileHasExpectedLines(t, []string{">>> mount", ">>> lsblk"}, filepath.Join(hcDir, "node-info", host, "os_info.txt"))
	}
}

func TestRemoteCollectOnK8sUsingTheK8sGoAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testing in short mode")
	}
	var err error
	tmpOutputDir := "/opt/dremio/data/ddc-tmp-out"
	tgzFile := filepath.Join(t.TempDir(), "diag.tgz")
	outputDir = tmpOutputDir

	args := []string{"ddc", "collect", "k8s", "standard", "--collect-container-logs", "--namespace", namespace, "--output-file", tgzFile}
	err = cmd.Execute(args)
	if err != nil {
		t.Fatalf("unable to run collect: %v", err)
	}
	log.Printf("remote collect complete now verifying the results")
	testOut := filepath.Join(t.TempDir(), "ddcout")
	err = os.Mkdir(testOut, 0o700)
	if err != nil {
		t.Fatalf("could not make test out dir %v", err)
	}
	log.Printf("now in the test we are extracting tarball %v to %v", tgzFile, testOut)

	if err := archive.ExtractTarGz(tgzFile, testOut); err != nil {
		t.Fatalf("could not extract tgz %v to dir %v: %v", tgzFile, testOut, err)
	}

	t.Logf("now we are reading the %v dir", testOut)
	entries, err := os.ReadDir(testOut)
	if err != nil {
		t.Fatalf("uanble to read dir %v: %v", testOut, err)
	}
	hcDir := ""
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
		if e.IsDir() {
			hcDir = filepath.Join(testOut, e.Name())
			t.Logf("now found the health check directory which is %v", hcDir)
		}
	}

	if len(names) != 2 {
		t.Fatalf("expected 2 entries but had %v", strings.Join(names, ","))
	}
	tests.AssertFileHasContent(t, filepath.Join(testOut, "summary.json"))

	// check k8s files
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "cronjob.json"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "daemonset.json"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "deployments.json"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "endpoints.json"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "events.json"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "hpa.json"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "ingress.json"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "job.json"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "limitrange.json"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "nodes.json"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "pc.json"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "pdb.json"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "pods.json"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "pv.json"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "pvc.json"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "replicaset.json"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "resourcequota.json"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "sc.json"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "service.json"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kubernetes", "statefulsets.json"))

	expectedFiles := []string{
		"dremio-executor-1-chown-data-directory.txt",
		"dremio-executor-1-chown-cloudcache-directory.txt",
		"dremio-executor-1-dremio-executor.txt",
		"dremio-executor-1-wait-for-zookeeper.txt",
		"dremio-executor-0-chown-data-directory.txt",
		"dremio-executor-0-chown-cloudcache-directory.txt",
		"dremio-executor-0-dremio-executor.txt",
		"dremio-executor-0-wait-for-zookeeper.txt",
		"dremio-master-0-chown-data-directory.txt",
		"dremio-master-0-dremio-master-coordinator.txt",
		"dremio-master-0-start-only-one-dremio-master.txt",
		"dremio-master-0-upgrade-task.txt",
		"dremio-master-0-wait-for-zookeeper.txt",
	}

	expectedEmptyFiles := []string{
		"dremio-executor-1-chown-data-directory.txt",
		"dremio-executor-1-chown-cloudcache-directory.txt",
		"dremio-executor-0-chown-data-directory.txt",
		"dremio-executor-0-chown-cloudcache-directory.txt",
		"dremio-master-0-chown-data-directory.txt",
		"dremio-master-0-start-only-one-dremio-master.txt",
	}
	dir := filepath.Join(hcDir, "kubernetes", "container-logs")
	entries, err = os.ReadDir(dir)
	if err != nil {
		t.Fatalf("unable to read dir %v: %v", dir, err)
	}
	for _, entry := range entries {
		t.Logf("directories %v", entry.Name())
	}
	expectedEntries := 13
	if len(entries) != expectedEntries {
		t.Errorf("expected to find %v entries but found %v", expectedEntries, len(entries))
	}
	foundFiles := []string{}
	foundEmptyFiles := []string{}
	compareEmptyFiles := []string{}
	for _, e := range entries {
		fs, err := e.Info()
		if err != nil {
			t.Fatalf("error getting entry %v info: %v", e, err)
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

	for _, expectedEmptyFile := range expectedEmptyFiles {
		for _, foundEmptyFile := range foundEmptyFiles {
			if expectedEmptyFile == foundEmptyFile {
				compareEmptyFiles = append(compareEmptyFiles, expectedEmptyFile)
			}
		}
	}
	if !reflect.DeepEqual(expectedEmptyFiles, compareEmptyFiles) {
		t.Errorf("Expected the following files to be empty:\n %v\n But found the following:\n %v", expectedEmptyFiles, compareEmptyFiles)
	}

	if !reflect.DeepEqual(foundFiles, expectedFiles) {
		t.Errorf("Expected the following files to be present:\n %v\n But found the following:\n %v", expectedFiles, foundFiles)
	}
	replicas := 2
	// check server.logs - only if they exist (test cluster may not produce them)
	if _, err := os.Stat(filepath.Join(hcDir, "logs", "dremio-master-0", "server.log.gz")); err == nil {
		tests.AssertFileHasContent(t, filepath.Join(hcDir, "logs", "dremio-master-0", "server.log.gz"))
	}
	for i := 0; i < replicas; i++ {
		host := fmt.Sprintf("dremio-executor-%v", i)
		if _, err := os.Stat(filepath.Join(hcDir, "logs", host, "server.log.gz")); err == nil {
			tests.AssertFileHasContent(t, filepath.Join(hcDir, "logs", host, "server.log.gz"))
		}
	}
	// check server.json logs - only if they exist
	if _, err := os.Stat(filepath.Join(hcDir, "logs", "dremio-master-0", "server.json.gz")); err == nil {
		tests.AssertFileHasContent(t, filepath.Join(hcDir, "logs", "dremio-master-0", "server.json.gz"))
	}
	for i := 0; i < replicas; i++ {
		host := fmt.Sprintf("dremio-executor-%v", i)
		if _, err := os.Stat(filepath.Join(hcDir, "logs", host, "server.json.gz")); err == nil {
			tests.AssertFileHasContent(t, filepath.Join(hcDir, "logs", host, "server.json.gz"))
		}
	}
	// check queries.json - only if it exists (test cluster may not produce it)
	if _, err := os.Stat(filepath.Join(hcDir, "queries", "dremio-master-0", "queries.json.gz")); err == nil {
		tests.AssertFileHasContent(t, filepath.Join(hcDir, "queries", "dremio-master-0", "queries.json.gz"))
	}
	// check conf files

	tests.AssertFileHasContent(t, filepath.Join(hcDir, "configuration", "dremio-master-0", "dremio.conf"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "configuration", "dremio-master-0", "dremio-env"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "configuration", "dremio-master-0", "logback.xml"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "configuration", "dremio-master-0", "logback-access.xml"))

	for i := 0; i < replicas; i++ {
		host := fmt.Sprintf("dremio-executor-%v", i)
		tests.AssertFileHasContent(t, filepath.Join(hcDir, "configuration", host, "dremio.conf"))
		tests.AssertFileHasContent(t, filepath.Join(hcDir, "configuration", host, "dremio-env"))
		tests.AssertFileHasContent(t, filepath.Join(hcDir, "configuration", host, "logback.xml"))
		tests.AssertFileHasContent(t, filepath.Join(hcDir, "configuration", host, "logback-access.xml"))
	}

	// check nodeinfo files
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "node-info", "dremio-master-0", "diskusage.txt"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "node-info", "dremio-master-0", "jvm_settings.txt"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "node-info", "dremio-master-0", "os_info.txt"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "node-info", "dremio-master-0", "rocksdb_disk_allocation.txt"))

	for i := 0; i < replicas; i++ {
		host := fmt.Sprintf("dremio-executor-%v", i)
		tests.AssertFileHasContent(t, filepath.Join(hcDir, "node-info", host, "diskusage.txt"))
		tests.AssertFileHasContent(t, filepath.Join(hcDir, "node-info", host, "jvm_settings.txt"))
		tests.AssertFileHasContent(t, filepath.Join(hcDir, "node-info", host, "os_info.txt"))
	}

	// check file contents
	t.Logf("checking file %v", filepath.Join(hcDir, "node-info", "dremio-master-0", "os_info.txt"))
	tests.AssertFileHasExpectedLines(t, []string{">>> mount", ">>> lsblk"}, filepath.Join(hcDir, "node-info", "dremio-master-0", "os_info.txt"))
	for i := 0; i < replicas; i++ {
		host := fmt.Sprintf("dremio-executor-%v", i)
		t.Logf("checking file %v", filepath.Join(hcDir, "node-info", host, "os_info.txt"))
		tests.AssertFileHasExpectedLines(t, []string{">>> mount", ">>> lsblk"}, filepath.Join(hcDir, "node-info", host, "os_info.txt"))
	}
}

func submitSQLQuery(query, dremioEndpoint, dremioPat string) (string, error) {
	sql := fmt.Sprintf(`{
		"sql": "%v"
	}`, query)
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%v/api/v3/sql/", dremioEndpoint), bytes.NewBuffer([]byte(sql)))
	if err != nil {
		return "", fmt.Errorf("unable to run sql %w", err)
	}
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "_dremio"+dremioPat)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("unable to run sql %v: %w", query, err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode > 299 {
		text, err := io.ReadAll(res.Body)
		if err != nil {
			return "", fmt.Errorf("fatal attempt to make job api call, unable to read body for debugging: %w", err)
		}
		log.Printf("body was %s", string(text))
		return "", fmt.Errorf("expected status code greater than 299 but instead got %v while trying to run sql %v ", res.StatusCode, query)
	}
	var jobResponse JobAPIResponse
	err = json.NewDecoder(res.Body).Decode(&jobResponse)
	if err != nil {
		text, err := io.ReadAll(res.Body)
		if err != nil {
			return "", fmt.Errorf("fatal attempt to decode body from dremio job api call, unable to read body for debugging: %w", err)
		}
		log.Printf("body was %s", string(text))
		return "", fmt.Errorf("fatal attempt to decode body from dremio job api: %w", err)
	}
	return jobResponse.ID, nil
}

func TestValidateBadCollectFlag(t *testing.T) {
	// In the v4 CLI, mode is a subcommand (standard|diagnosis), not a --mode flag.
	// Passing an unknown flag should produce a Cobra error.
	args := []string{"ddc", "collect", "k8s", "standard", "--bogus-flag"}
	err := cmd.Execute(args)
	if err == nil {
		t.Fatal("collect should fail with unknown flag")
	}
	expected := "unknown flag"
	if !strings.Contains(err.Error(), expected) {
		t.Errorf("expected to contain '%v' in '%v'", expected, err.Error())
	}
}
