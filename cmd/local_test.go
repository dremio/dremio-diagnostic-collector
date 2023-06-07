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
	"bufio"
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

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/nodeinfocollect"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/restclient"
	"github.com/dremio/dremio-diagnostic-collector/cmd/simplelog"
	"github.com/dremio/dremio-diagnostic-collector/pkg/ddcio"
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
number-job-profiles: 0
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

func TestMain(m *testing.M) {
	simplelog.InitLogger(4)
	exitCode := func() (exitCode int) {
		restclient.InitClient(true)
		// ctx := context.Background()

		// pwd, err := os.Getwd()
		// if err != nil {
		// 	log.Fatalf("failed to get working directory: %s", err)
		// }
		// req := testcontainers.ContainerRequest{
		// 	Image:        "dremio/dremio-oss:24.0",
		// 	ExposedPorts: []string{"9047/tcp"},
		// 	WaitingFor:   wait.ForLog("Dremio Daemon Started as master"),
		// 	Files: []testcontainers.ContainerFile{
		// 		{
		// 			HostFilePath:      fmt.Sprintf("%s/testdata/conf/dremio.conf", pwd), // a directory
		// 			ContainerFilePath: "/opt/dremio/conf/dremio.conf",                   // important! its parent already exists
		// 			FileMode:          644,
		// 		},
		// 	},
		// }
		// dremioC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		// 	ContainerRequest: req,
		// 	Started:          true,
		// })
		// if err != nil {
		// 	panic(err)
		// }
		// defer func() {
		// 	if err := dremioC.Terminate(ctx); err != nil {
		// 		panic(fmt.Sprintf("failed to terminate container: %s", err.Error()))
		// 	}
		// }()
		// dremioTestPortRaw, err := dremioC.MappedPort(ctx, "9047/tcp")
		// if err != nil {
		// 	log.Fatalf("could not get dremio port: %s", err)
		// }
		rootDir, err := GetRootProjectDir()
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
			if err := shutdownCmd.Wait(); err != nil {
				log.Print(err)
			}
			// if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			// 	log.Print(err)
			// }
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
		//exit, _, err := dremioC.Exec(context.Background(), []string{"mkdir", "/tmp/dremio-source"})
		//if err != nil {
		//	log.Fatalf("could not make dremio source: %s", err)
		//}
		//if exit > 0 {
		//	log.Fatalf("unable to make dremio source due to exit code %d", exit)
		//}

		dremioEndpoint := fmt.Sprintf("http://localhost:%v", dremioTestPort)

		res, err := http.Get(dremioEndpoint) //nolint
		if err != nil {
			log.Fatalf("error making http request: %s\n", err)
		}
		expectedCode := 200
		if res.StatusCode != expectedCode {
			log.Fatalf("expected status code %v but instead got %v. Dremio is not ready", expectedCode, res.StatusCode)
		}
		// accept EULA
		//var empty bytes.Buffer
		//eulaURL := fmt.Sprintf("http://localhost:%v/apiv2/eula/accept", dremioTestPort)
		//res, err = http.Post(eulaURL, "application/json", &empty) //nolint
		//if err != nil {
		//	log.Fatalf("error accepting EULA request: %s\n", err)
		//}
		//if res.StatusCode != 204 {
		//	log.Fatalf("expected status code 204 but instead got %v while trying to accept EULA", res.StatusCode)
		//}

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

func TestCreateAllDirs(t *testing.T) {
	err := createAllDirs(c)
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
}

// until we add back the dremio-ee image
// func TestCollectWlm(t *testing.T) {
// 	err := runCollectWLM(c)
// 	if err != nil {
// 		t.Errorf("unexpected error %v", err)
// 	}
// }

func TestCollectKVReport(t *testing.T) {
	simplelog.InitLogger(4)
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
	err = runCollectKvReport(c)
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
	sql := `{
		"sql": "CREATE TABLE tester.table1 AS SELECT \"a\", \"b\" FROM (values (CAST(1 AS INTEGER), CAST(2 AS INTEGER))) as t(\"a\", \"b\")"
	}`
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%v/api/v3/sql/", c.DremioEndpoint()), bytes.NewBuffer([]byte(sql)))
	if err != nil {
		t.Fatalf("unable to create table request %v", err)
	}
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "_dremio"+c.DremioPATToken())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("unable to create table %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode > 299 {
		text, err := io.ReadAll(res.Body)
		if err != nil {
			log.Fatalf("fatal attempt to make job api call %v and unable to read body for debugging", err)
		}
		log.Printf("body was %s", string(text))
		t.Fatalf("expected status code greater than 299 but instead got %v while trying to create source", res.StatusCode)
	}
	var jobResponse JobAPIResponse
	err = json.NewDecoder(res.Body).Decode(&jobResponse)
	if err != nil {
		text, err := io.ReadAll(res.Body)
		if err != nil {
			log.Fatalf("fatal attempt to decode body from dremio job api call %v and unable to read body for debugging", err)
		}
		log.Printf("body was %s", string(text))
		log.Fatalf("fatal attempt to decode body from dremio job api %v", err)
	}
	time.Sleep(10 * time.Second)
	jobid := jobResponse.ID
	err = downloadJobProfile(c, jobid)
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
}

func TestValidateAPICredentials(t *testing.T) {
	err := validateAPICredentials(c)
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
}

func TestValidateCollectJobProfiles(t *testing.T) {
	err := runCollectJobProfiles(c)
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
}

func TestCaptureSystemMetrics(t *testing.T) {
	if err := os.MkdirAll(c.NodeInfoOutDir(), 0700); err != nil {
		t.Errorf("cannot make output dir due to error %v", err)
	}
	defer func() {
		if err := os.RemoveAll(c.NodeInfoOutDir()); err != nil {
			t.Logf("error cleaning up dir %v due to error %v", c.NodeInfoOutDir(), err)
		}
	}()
	if err := runCollectNodeMetrics(c); err != nil {
		t.Errorf("expected no errors but had %v", err)
	}
	metricsFile := filepath.Join(c.NodeInfoOutDir(), "metrics.json")
	fs, err := os.Stat(metricsFile)
	if err != nil {
		t.Errorf("expected to find file but got error %v", err)
	}
	if fs.Size() == 0 {
		t.Errorf("should not have an empty file")
	}
	f, err := os.Open(metricsFile)
	if err != nil {
		t.Errorf("while opening file %v we had error %v", metricsFile, err)
	}
	scanner := bufio.NewScanner(f)
	var rows []nodeinfocollect.SystemMetricsRow
	for scanner.Scan() {
		var row nodeinfocollect.SystemMetricsRow
		text := scanner.Text()
		if err := json.Unmarshal([]byte(text), &row); err != nil {
			t.Errorf("unable to convert text %v to json due to error %v", text, err)
		}
		rows = append(rows, row)
	}
	if len(rows) > 12 {
		t.Errorf("%v rows created by metrics file, this is too many and the default should be around 10", len(rows))
	}
	if len(rows) < 8 {
		t.Errorf("%v rows created by metrics file, this is too few and the default should be around 10", len(rows))
	}
	t.Logf("%v rows of metrics captured", len(rows))
}

// func TestFindGCLocation(t *testing.T) {
// 	expectedOutput := "1 com.dremio.dac.daemon.DremioDaemon\n2 myfoo\n3 nothing"
// 	var returnValues [][]interface{}
// 	e := []interface{}{expectedOutput, nil}
// 	returnValues = append(returnValues, e)
// 	processFlags := `1:
//     VM Arguments:
//     jvm_args: -Djava.util.logging.config.class=org.slf4j.bridge.SLF4JBridgeHandler -Djava.library.path=/opt/dremio/lib -XX:+PrintGCDetails -XX:+PrintGCDateStamps -Ddremio.plugins.path=/opt/dremio/plugins -Xmx4096m -XX:MaxDirectMemorySize=2048m -XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath=/var/log/dremio -Dio.netty.maxDirectMemory=0 -Dio.netty.tryReflectionSetAccessible=true -DMAPR_IMPALA_RA_THROTTLE -DMAPR_MAX_RA_STREAMS=400 -XX:+UseG1GC -Ddremio.log.path=/opt/dremio/data/log -Xloggc:/opt/dremio/data/log/gc.log -XX:+UseGCLogFileRotation -XX:NumberOfGCLogFiles=5 -XX:GCLogFileSize=4000k -XX:+PrintGCDetails -XX:+PrintGCTimeStamps -XX:+PrintGCDateStamps -XX:+PrintClassHistogramBeforeFullGC -XX:+PrintClassHistogramAfterFullGC -XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath=/opt/dremio/data -XX:+UseG1GC -XX:G1HeapRegionSize=32M -XX:MaxGCPauseMillis=500 -XX:InitiatingHeapOccupancyPercent=25 -XX:+PrintAdaptiveSizePolicy -XX:+PrintReferenceGC -XX:ErrorFile=/opt/dremio/data/hs_err_pid%p.log -Dzookeeper=zk-hs:2181 -Dservices.coordinator.enabled=false -Dservices.coordinator.master.enabled=false -Dservices.coordinator.master.embedded-zookeeper.enabled=false -Dservices.executor.enabled=true -Dservices.conduit.port=45679 -Dservices.node-tag=default -XX:+PrintClassHistogramBeforeFullGC -XX:+PrintClassHistogramAfterFullGC
//     java_command: com.dremio.dac.daemon.DremioDaemon
//     java_class_path (initial): /opt/dremio/conf:/opt/dremio/jars/dremio-services-coordinator-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-hive-function-registry-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-ce-sabot-serializer-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-hive2-plugin-launcher-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-ee-services-credentials-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-ee-services-accesscontrol-common-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-ce-sabot-scheduler-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-dac-common-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-services-usersessions-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-ee-services-sysflight-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-protocol-20.0.0-202201050826310141-8cc7162b-proto.jar:/opt/dremio/jars/dremio-services-telemetry-impl-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-services-jobtelemetry-client-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-hive3-plugin-launcher-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-ce-services-cachemanager-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-ee-dac-tools-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-services-base-rpc-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-services-datastore-20.0.0-202201050826310141-8cc7162b-proto.jar:/opt/dremio/jars/dremio-sabot-logical-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-services-transientstore-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-services-resourcescheduler-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-dac-daemon-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-ee-services-namespace-20.0.0-202201050826310141-8cc7162b-tests.jar:/opt/dremio/j
//     Launcher Type: SUN_STANDARD`
// 	e = []interface{}{processFlags, nil}
// 	returnValues = append(returnValues, e)
// 	mockCollector := &MockCollector{
// 		Returns: returnValues,
// 	}

// 	location, err := findGCLogLocation()
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	expected := "/opt/dremio/data/log/gc.log*"
// 	if location != expected {
// 		t.Errorf("expected '%v' but was '%v'", expected, location)
// 	}
// }
