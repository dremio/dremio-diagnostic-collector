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
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf"
)

func writeConfWithYamlText(tmpOutputDir, yamlTextMinusTmpOutputDir string) string {

	cleaned := filepath.Clean(tmpOutputDir)
	if err := os.MkdirAll(cleaned, 0700); err != nil {
		log.Fatal(err)
	}
	testDDCYaml := filepath.Join(tmpOutputDir, "ddc.yaml")
	yamlText := fmt.Sprintf(`
dremio-log-dir: %v
dremio-conf-dir: %v
tmp-output-dir: %v
%v
`, filepath.Join("testdata", "fs", "opt", "dremio", "logs"), filepath.Join("testdata", "fs", "opt", "dremio", "conf"), strings.ReplaceAll(cleaned, "\\", "\\\\"), yamlTextMinusTmpOutputDir)
	fmt.Printf("yaml text is\n%q\n", yamlText)
	if err := os.WriteFile(testDDCYaml, []byte(yamlText), 0600); err != nil {
		log.Fatal(err)
	}
	return testDDCYaml
}

func writeConf(tmpOutputDir string) string {

	defaultText := `
verbose: vvvv
node-metrics-collect-duration-seconds: 10
`
	return writeConfWithYamlText(tmpOutputDir, defaultText)
}

func TestCaptureSystemMetrics(t *testing.T) {
	tmpDirForConf := t.TempDir() + string(filepath.Separator) + "ddc"
	err := os.Mkdir(tmpDirForConf, 0700)
	if err != nil {
		log.Fatal(err)
	}
	yamlLocation := writeConf(tmpDirForConf)
	c, err := conf.ReadConf(make(map[string]string), yamlLocation)
	if err != nil {
		log.Fatalf("reading config %v", err)
	}
	log.Printf("NODE INFO DIR %v", c.NodeInfoOutDir())
	if err := os.MkdirAll(c.NodeInfoOutDir(), 0700); err != nil {
		t.Errorf("cannot make output dir due to error %v", err)
	}
	defer func() {
		if err := os.RemoveAll(c.NodeInfoOutDir()); err != nil {
			t.Logf("error cleaning up dir %v due to error %v", c.NodeInfoOutDir(), err)
		}
	}()
}

func TestCreateAllDirs(t *testing.T) {
	tmpDirForConf := filepath.Join(t.TempDir(), "ddc")
	err := os.Mkdir(tmpDirForConf, 0700)
	if err != nil {
		log.Fatal(err)
	}
	yamlLocation := writeConf(tmpDirForConf)
	c, err := conf.ReadConf(make(map[string]string), yamlLocation)
	if err != nil {
		log.Fatalf("reading config %v", err)
	}
	err = createAllDirs(c)
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}

	// WLM should end with nodename
	if !strings.HasSuffix(c.WLMOutDir(), c.NodeName()) {
		t.Errorf("expected %v to end with %v", c.WLMOutDir(), c.NodeName())
	}
	// System table should end with nodename
	if !strings.HasSuffix(c.SystemTablesOutDir(), c.NodeName()) {
		t.Errorf("expected %v to end with %v", c.SystemTablesOutDir(), c.NodeName())
	}
	// job profiles should end with nodename
	if !strings.HasSuffix(c.JobProfilesOutDir(), c.NodeName()) {
		t.Errorf("expected %v to end with %v", c.JobProfilesOutDir(), c.NodeName())
	}
	// kvreport should end with nodename
	// job profiles should end with nodename
	if !strings.HasSuffix(c.KVstoreOutDir(), c.NodeName()) {
		t.Errorf("expected %v to end with %v", c.KVstoreOutDir(), c.NodeName())
	}
}

func TestCollectJVMFlags(t *testing.T) {
	tmpDirForConf := filepath.Join(t.TempDir(), "ddc")
	err := os.Mkdir(tmpDirForConf, 0700)
	if err != nil {
		t.Fatalf("unable to make test dir: %v", err)
	}
	jarLoc := filepath.Join("testdata", "demo.jar")
	cmd := exec.Command("java", "-Dtestflag=1", "-jar", jarLoc)
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start() failed with %s\n", err)
	}

	defer func() {
		if cmd != nil && cmd.ProcessState != nil && !cmd.ProcessState.Exited() {
			if err := cmd.Process.Kill(); err != nil {
				t.Logf("failed to kill process: %s", err)
			} else {
				t.Log("Process killed successfully.")
			}
		}
	}()

	time.Sleep(100 * time.Millisecond)
	fmt.Printf("pid is %v", cmd.Process.Pid)
	yaml := fmt.Sprintf(`
dremio-rocksdb-dir: /opt/dremio/data/db # used for locating Dremio's KV Metastore

collect-acceleration-log: false
collect-access-log: false
collect-audit-log: false
collect-dremio-configuration: false 
capture-heap-dump: false 
number-threads: 2

dremio-pid: %v
collect-metrics: false
collect-os-config: false
collect-disk-usage: false
collect-queries-json: false
collect-jvm-flags: true
collect-server-logs: false
collect-meta-refresh-log: false
collect-reflection-log: false
collect-gc-logs: false
collect-jfr: false
collect-jstack: false
collect-ttop: false
collect-system-tables-export: false
collect-wlm: false
collect-kvstore-report: false
is-dremio-cloud: false
`, cmd.Process.Pid)
	yamlLocation := writeConfWithYamlText(tmpDirForConf, yaml)
	c, err := conf.ReadConf(make(map[string]string), yamlLocation)
	if err != nil {
		t.Fatalf("reading config %v", err)
	}
	if err := collect(c); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("failed to kill process: %s", err)
	} else {
		t.Logf("Process %v killed successfully.", cmd.Process.Pid)
	}

	entries, err := os.ReadDir(c.NodeInfoOutDir())
	if err != nil {
		t.Fatal(err)
	}
	var items []string
	var found bool
	var text string
	for _, e := range entries {
		items = append(items, e.Name())
		if e.Name() == "jvm_settings.txt" {
			found = true
			r, err := os.ReadFile(filepath.Join(c.NodeInfoOutDir(), e.Name()))
			if err != nil {
				t.Fatalf("unable to read matching test file: %v", err)
			}
			text = string(r)
		}
	}
	if !found {
		t.Errorf("did not find jvm_settings.txt in entries '%v'", strings.Join(items, ", "))
	}
	containsDemoJar := strings.Contains(text, "demo.jar")
	containsFlag := strings.Contains(text, "-Dtestflag=1")
	successful := containsFlag && containsDemoJar
	if !successful {
		t.Errorf("expected '-Dtestflag=1' and 'demo.jar' in the flags but was '%q'", text)
	}
}

func TestSkipCollect(t *testing.T) {
	tmpDirForConf := filepath.Join(t.TempDir(), "ddcSkipCollect")
	err := os.Mkdir(tmpDirForConf, 0700)
	if err != nil {
		t.Fatalf("unable to make test dir: %v", err)
	}
	jarLoc := filepath.Join("testdata", "demo.jar")
	cmd := exec.Command("java", "-jar", jarLoc)
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start() failed with %s\n", err)
	}

	defer func() {
		if cmd != nil && cmd.ProcessState != nil && !cmd.ProcessState.Exited() {
			if err := cmd.Process.Kill(); err != nil {
				t.Logf("failed to kill process: %s", err)
			} else {
				t.Log("Process killed successfully.")
			}
		}
	}()
	yaml := fmt.Sprintf(`
dremio-rocksdb-dir: /opt/dremio/data/db # used for locating Dremio's KV Metastore

collect-acceleration-log: false
collect-access-log: false
collect-audit-log: false
collect-dremio-configuration: false 
capture-heap-dump: false # when true a heap dump will be captured on each node that the collector is run against
number-threads: 2
dremio-pid: %v

collect-metrics: false
collect-os-config: false
collect-disk-usage: false
collect-queries-json: false
collect-jvm-flags: false
collect-server-logs: false
collect-meta-refresh-log: false
collect-reflection-log: false
collect-gc-logs: false
collect-jfr: false
collect-jstack: false
collect-ttop: false
collect-system-tables-export: false
collect-wlm: false
collect-kvstore-report: false
is-dremio-cloud: false
`, cmd.Process.Pid)
	yamlLocation := writeConfWithYamlText(tmpDirForConf, yaml)
	c, err := conf.ReadConf(make(map[string]string), yamlLocation)
	if err != nil {
		t.Fatalf("reading config %v", err)
	}
	if err := collect(c); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("failed to kill process: %s", err)
	} else {
		t.Log("Process killed successfully.")
	}
	entries, err := os.ReadDir(c.NodeInfoOutDir())
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) > 0 {
		t.Errorf("expecting no entries but there were %v", len(entries))
	}
}

func TestDDCYamlFlagDefault(t *testing.T) {
	ddcYamlFlag := LocalCollectCmd.Flag("ddc-yaml")
	defaultValue := ddcYamlFlag.Value.String()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	exeDir := filepath.Dir(exe)
	expected := filepath.Join(exeDir, "ddc.yaml")
	if defaultValue != expected {
		t.Errorf("expected %v actual %v", expected, defaultValue)
	}
}

func TestFindClusterID(t *testing.T) {
	tmpDirForConf := filepath.Join(t.TempDir(), "ddcSkipCollect")
	err := os.Mkdir(tmpDirForConf, 0700)
	if err != nil {
		t.Fatalf("unable to make test dir: %v", err)
	}
	jarLoc := filepath.Join("testdata", "demo.jar")
	cmd := exec.Command("java", "-jar", jarLoc)
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start() failed with %s\n", err)
	}

	defer func() {
		if cmd != nil && cmd.ProcessState != nil && !cmd.ProcessState.Exited() {
			if err := cmd.Process.Kill(); err != nil {
				t.Logf("failed to kill process: %s", err)
			} else {
				t.Log("Process killed successfully.")
			}
		}
	}()
	dremioHome := filepath.Join("testdata", "fs", "opt", "dremio")
	yaml := fmt.Sprintf(`
dremio-rocksdb-dir: %v
`, filepath.Join(dremioHome, "db"))
	yamlLocation := writeConfWithYamlText(tmpDirForConf, yaml)
	c, err := conf.ReadConf(make(map[string]string), yamlLocation)
	if err != nil {
		t.Fatalf("reading config %v", err)
	}
	clusterID, err := findClusterID(c)
	if err != nil {
		t.Errorf("expected nil but was: %v", err)
	}
	expected := "4aede9fd-f5fe-4f6d-94df-b4ff17307872"
	if clusterID != expected {
		t.Errorf("expected %v but was: %v", expected, err)
	}
}

func TestParseClassPathVersion(t *testing.T) {
	f := `java.class.path=/opt/dremio/conf\:/opt/dremio/jars/dremio-ee-services-namespace-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-dac-tools-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-sabot-kernel-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-provision-common-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-services-wlm-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-jobcounts-server-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-services-fabric-rpc-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-jobs-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-teradata-plugin-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-configuration-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-scheduler-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ce-sabot-joust-java-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-statistics-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ce-hive3-plugin-launcher-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ui-common-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-services-roles-common-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-credential-provider-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-fabric-rpc-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-provision-common-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-usersessions-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-script-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-nessie-storage-upgrade-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ce-sabot-kernel-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-jobcounts-common-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ce-hive2-plugin-launcher-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-services-userdirectory-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-dac-ui-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-services-script-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-protocol-24.2.6-202311250456170399-68acbe47-proto.jar\:/opt/dremio/jars/dremio-ee-dac-upgrade-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-sabot-kernel-24.2.6-202311250456170399-68acbe47-proto.jar\:/opt/dremio/jars/dremio-ee-services-accelerator-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ce-jdbc-plugin-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-protocol-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-datastore-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-credentials-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-services-rulesexecutor-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-gcs-plugin-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ce-jdbc-fetcher-api-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-s3-plugin-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-mongo-plugin-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-partition-stats-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ce-mongo-plugin-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-common-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-resourcescheduler-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ui-lib-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-client-base-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-maestro-client-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-arrow-flight-common-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-orphanagecleaner-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-services-jwt-validator-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-services-roles-client-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-services-statistics-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-dac-backend-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-dac-daemon-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-execselector-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-dac-daemon-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-options-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-hive3-plugin-launcher-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ce-sabot-joust-cpp-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-userpreferences-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-telemetry-impl-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-sabot-kernel-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-nessie-grpc-common-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-services-userdirectory-api-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-dac-backend-24.2.6-202311250456170399-68acbe47-proto.jar\:/opt/dremio/jars/dremio-hive2-plugin-launcher-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-elasticsearch-plugin-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-services-accesscontrol-client-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-services-jobs-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-spill-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-services-wlmfunctions-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-hdfs-plugin-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ce-parquet-plugin-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-dac-common-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-services-external-users-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ce-elasticsearch-plugin-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-jobresults-common-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-tokens-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-services-rulesservice-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-telemetry-utils-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-jobtelemetry-server-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-jobresults-server-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-transientstore-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-sysflight-plugin-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-jobs-24.2.6-202311250456170399-68acbe47-proto.jar\:/opt/dremio/jars/dremio-dataplane-plugin-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-services-accesscontrol-common-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-adls-plugin-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-hive-plugin-common-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-catalog-api-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-authorizer-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-users-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-namespace-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-services-ownership-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-accelerator-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-pdfs-plugin-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-nessie-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-plugin-common-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-jobtelemetry-common-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-services-credentials-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-orphanage-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-nessie-grpc-client-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-nessie-grpc-server-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-tpch-sample-data-1.0.0.jar\:/opt/dremio/jars/dremio-services-accelerator-24.2.6-202311250456170399-68acbe47-proto.jar\:/opt/dremio/jars/dremio-services-datastore-24.2.6-202311250456170399-68acbe47-proto.jar\:/opt/dremio/jars/dremio-ee-services-sysflight-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-plugin-awsauth-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-autocomplete-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-services-usergroups-common-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-hive-plugin-common-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-common-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-hive3-plugin-launcher-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-dac-ui-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-services-activation-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ce-services-cachemanager-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-nessie-proxy-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-services-userpreferences-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-awsglue-plugin-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-services-execselector-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-grpc-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-nas-plugin-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-services-roles-server-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-sabot-serializer-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-sabot-vector-tools-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-base-rpc-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-azure-storage-plugin-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-connector-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-accelerator-api-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-dac-backend-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-jobresults-client-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-attach-tool-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-services-accesscontrol-server-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ce-sabot-scheduler-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-telemetry-api-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-coordinator-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-sysflight-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-commandpool-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-hive-function-registry-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-hdfs-plugin-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-sabot-logical-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-hive2-plugin-launcher-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-services-catalogevents-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-maestro-common-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-arrow-flight-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-namespace-24.2.6-202311250456170399-68acbe47-proto.jar\:/opt/dremio/jars/dremio-yarn-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-services-usergroups-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-jobtelemetry-client-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-services-authorization-server-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-executorservice-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-ee-services-reflection-recommender-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/dremio-services-functions-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/ext/zookeeper-3.4.14.jar\:/opt/dremio/jars/3rdparty/metrics-jvm-4.1.19.jar\:/opt/dremio/jars/3rdparty/nessie-model-0.64.0.jar\:/opt/dremio/jars/3rdparty/logstash-logback-encoder-7.2.jar\:/opt/dremio/jars/3rdparty/jakarta.validation-api-2.0.2.jar\:/opt/dremio/jars/3rdparty/guice-6.0.0.jar\:/opt/dremio/jars/3rdparty/opentelemetry-instrumentation-annotations-1.27.0.jar\:/opt/dremio/jars/3rdparty/lucene-join-7.7.3.jar\:/opt/dremio/jars/3rdparty/hadoop-common-3.3.2-dremio-202310112100510489-6e65eff.jar\:/opt/dremio/jars/3rdparty/parquet-jackson-1.12.0-202309080020000384-9c11bcb.jar\:/opt/dremio/jars/3rdparty/hadoop-yarn-common-3.3.2-dremio-202310112100510489-6e65eff.jar\:/opt/dremio/jars/3rdparty/jetty-security-9.4.51.v20230217.jar\:/opt/dremio/jars/3rdparty/cel-generated-antlr-0.3.12.jar\:/opt/dremio/jars/3rdparty/aws-java-sdk-kms-1.12.400.jar\:/opt/dremio/jars/3rdparty/nessie-services-0.64.0.jar\:/opt/dremio/jars/3rdparty/aopalliance-1.0.jar\:/opt/dremio/jars/3rdparty/secretsmanager-2.17.295.jar\:/opt/dremio/jars/3rdparty/jersey-media-json-jackson-2.40.jar\:/opt/dremio/jars/3rdparty/javax.el-api-3.0.0.jar\:/opt/dremio/jars/3rdparty/jetty-util-ajax-9.4.51.v20230217.jar\:/opt/dremio/jars/3rdparty/netty-transport-rxtx-4.1.100.Final.jar\:/opt/dremio/jars/3rdparty/protobuf-java-3.21.9.jar\:/opt/dremio/jars/3rdparty/jakarta.inject-2.6.1.jar\:/opt/dremio/jars/3rdparty/netty-transport-native-unix-common-4.1.100.Final.jar\:/opt/dremio/jars/3rdparty/commons-collections4-4.4.jar\:/opt/dremio/jars/3rdparty/oauth2-oidc-sdk-9.3.jar\:/opt/dremio/jars/3rdparty/util-2.2.2-dremio-202306291124120084-8ab9811.jar\:/opt/dremio/jars/3rdparty/iceberg-nessie-1.3.0-7dbdfd3-20230614154222-545fbe0.jar\:/opt/dremio/jars/3rdparty/hadoop-azure-datalake-3.3.2-dremio-202310112100510489-6e65eff.jar\:/opt/dremio/jars/3rdparty/curator-framework-4.2.0.jar\:/opt/dremio/jars/3rdparty/arns-2.17.295.jar\:/opt/dremio/jars/3rdparty/aws-core-2.17.295.jar\:/opt/dremio/jars/3rdparty/animal-sniffer-annotations-1.23.jar\:/opt/dremio/jars/3rdparty/annotations-2.17.295.jar\:/opt/dremio/jars/3rdparty/async-http-client-netty-utils-2.7.0.jar\:/opt/dremio/jars/3rdparty/iceberg-data-1.3.0-7dbdfd3-20230614154222-545fbe0.jar\:/opt/dremio/jars/3rdparty/aws-java-sdk-redshift-arcadia-internal-1.0.jar\:/opt/dremio/jars/3rdparty/s3-2.17.295.jar\:/opt/dremio/jars/3rdparty/aws-json-protocol-2.17.295.jar\:/opt/dremio/jars/3rdparty/netty-tcnative-classes-2.0.61.Final.jar\:/opt/dremio/jars/3rdparty/opentelemetry-opentracing-shim-1.27.0.jar\:/opt/dremio/jars/3rdparty/jmespath-java-1.12.400.jar\:/opt/dremio/jars/3rdparty/jersey-mvc-freemarker-2.40.jar\:/opt/dremio/jars/3rdparty/javassist-3.28.0-GA.jar\:/opt/dremio/jars/3rdparty/jline-3.9.0.jar\:/opt/dremio/jars/3rdparty/jackson-dataformat-cbor-2.15.2.jar\:/opt/dremio/jars/3rdparty/osdt_cert-19.3.0.0.jar\:/opt/dremio/jars/3rdparty/log4j-api-2.19.0.jar\:/opt/dremio/jars/3rdparty/aws-query-protocol-2.17.295.jar\:/opt/dremio/jars/3rdparty/commons-net-3.9.0.jar\:/opt/dremio/jars/3rdparty/kerby-util-1.0.1.jar\:/opt/dremio/jars/3rdparty/lucene-sandbox-7.7.3.jar\:/opt/dremio/jars/3rdparty/regions-2.17.295.jar\:/opt/dremio/jars/3rdparty/xalan-2.7.3.jar\:/opt/dremio/jars/3rdparty/jna-platform-5.12.1.jar\:/opt/dremio/jars/3rdparty/serializer-2.7.3.jar\:/opt/dremio/jars/3rdparty/stringtemplate-3.2.1.jar\:/opt/dremio/jars/3rdparty/jakarta.xml.bind-api-2.3.3.jar\:/opt/dremio/jars/3rdparty/simpleclient_tracer_otel-0.16.0.jar\:/opt/dremio/jars/3rdparty/jna-5.12.1.jar\:/opt/dremio/jars/3rdparty/jersey-server-2.40.jar\:/opt/dremio/jars/3rdparty/antlr-2.7.7.jar\:/opt/dremio/jars/3rdparty/java-semver-0.9.0.jar\:/opt/dremio/jars/3rdparty/iceberg-bundled-guava-1.3.0-7dbdfd3-20230614154222-545fbe0.jar\:/opt/dremio/jars/3rdparty/xercesImpl-2.12.2.jar\:/opt/dremio/jars/3rdparty/javax.interceptor-api-1.2.jar\:/opt/dremio/jars/3rdparty/auth-2.17.295.jar\:/opt/dremio/jars/3rdparty/netty-transport-classes-kqueue-4.1.100.Final.jar\:/opt/dremio/jars/3rdparty/util-hadoop-hadoop2-2.2.2-dremio-202306291124120084-8ab9811.jar\:/opt/dremio/jars/3rdparty/slugify-2.1.7.jar\:/opt/dremio/jars/3rdparty/simpleclient_tracer_common-0.16.0.jar\:/opt/dremio/jars/3rdparty/auto-value-annotations-1.7.4.jar\:/opt/dremio/jars/3rdparty/opentelemetry-api-1.27.0.jar\:/opt/dremio/jars/3rdparty/flogger-0.5.1.jar\:/opt/dremio/jars/3rdparty/google-api-client-1.31.3.jar\:/opt/dremio/jars/3rdparty/javax.servlet-api-3.1.0.jar\:/opt/dremio/jars/3rdparty/commons-lang-2.4.jar\:/opt/dremio/jars/3rdparty/netty-transport-native-kqueue-4.1.100.Final-osx-x86_64.jar\:/opt/dremio/jars/3rdparty/hadoop-shaded-guava-1.1.1.jar\:/opt/dremio/jars/3rdparty/dremio-hive3-exec-shaded-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/3rdparty/hk2-utils-2.6.1.jar\:/opt/dremio/jars/3rdparty/kerby-pkix-1.0.1.jar\:/opt/dremio/jars/3rdparty/modelmapper-2.3.0.jar\:/opt/dremio/jars/3rdparty/netty-resolver-dns-native-macos-4.1.100.Final-osx-aarch_64.jar\:/opt/dremio/jars/3rdparty/jakarta.activation-api-1.2.1.jar\:/opt/dremio/jars/3rdparty/kotlin-stdlib-jdk8-1.8.0.jar\:/opt/dremio/jars/3rdparty/avatica-metrics-1.23.0.jar\:/opt/dremio/jars/3rdparty/kotlin-stdlib-1.6.20.jar\:/opt/dremio/jars/3rdparty/jaeger-client-1.5.0.jar\:/opt/dremio/jars/3rdparty/koloboke-api-jdk8-1.0.0.jar\:/opt/dremio/jars/3rdparty/opentelemetry-exporter-otlp-1.27.0.jar\:/opt/dremio/jars/3rdparty/simpleclient-0.16.0.jar\:/opt/dremio/jars/3rdparty/lucene-grouping-7.7.3.jar\:/opt/dremio/jars/3rdparty/kerb-core-1.0.1.jar\:/opt/dremio/jars/3rdparty/logback-access-1.2.12.jar\:/opt/dremio/jars/3rdparty/simpleclient_hotspot-0.16.0.jar\:/opt/dremio/jars/3rdparty/nimbus-jose-jwt-8.8.jar\:/opt/dremio/jars/3rdparty/logback-classic-1.2.12.jar\:/opt/dremio/jars/3rdparty/google-api-services-iamcredentials-v1-rev20201022-1.31.0.jar\:/opt/dremio/jars/3rdparty/netty-codec-mqtt-4.1.100.Final.jar\:/opt/dremio/jars/3rdparty/snappy-java-1.1.10.5.jar\:/opt/dremio/jars/3rdparty/bson-record-codec-4.8.2.jar\:/opt/dremio/jars/3rdparty/kerby-asn1-1.0.1.jar\:/opt/dremio/jars/3rdparty/LatencyUtils-2.0.3.jar\:/opt/dremio/jars/3rdparty/hadoop-hdfs-client-3.3.2-dremio-202310112100510489-6e65eff.jar\:/opt/dremio/jars/3rdparty/simpleclient_dropwizard-0.16.0.jar\:/opt/dremio/jars/3rdparty/xmlbeans-3.1.0.jar\:/opt/dremio/jars/3rdparty/scim2-sdk-common-2.3.5.jar\:/opt/dremio/jars/3rdparty/arrow-gandiva-12.0.1-20231103121511-850ae5a2d6-dremio.jar\:/opt/dremio/jars/3rdparty/netty-codec-memcache-4.1.100.Final.jar\:/opt/dremio/jars/3rdparty/micrometer-observation-1.11.1.jar\:/opt/dremio/jars/3rdparty/nessie-server-store-proto-0.64.0.jar\:/opt/dremio/jars/3rdparty/mimepull-1.9.15.jar\:/opt/dremio/jars/3rdparty/arrow-format-12.0.1-20231103121511-850ae5a2d6-dremio.jar\:/opt/dremio/jars/3rdparty/mssql-jdbc-7.0.0.jre8.jar\:/opt/dremio/jars/3rdparty/iceberg-parquet-1.3.0-7dbdfd3-20230614154222-545fbe0.jar\:/opt/dremio/jars/3rdparty/commons-pool2-2.5.0.jar\:/opt/dremio/jars/3rdparty/jersey-mvc-2.40.jar\:/opt/dremio/jars/3rdparty/asciilist-0.0.3.jar\:/opt/dremio/jars/3rdparty/validation-api-2.0.1.Final.jar\:/opt/dremio/jars/3rdparty/perfmark-api-0.26.0.jar\:/opt/dremio/jars/3rdparty/hadoop-client-3.3.2-dremio-202310112100510489-6e65eff.jar\:/opt/dremio/jars/3rdparty/netty-tcnative-boringssl-static-2.0.61.Final-linux-aarch_64.jar\:/opt/dremio/jars/3rdparty/netty-resolver-dns-native-macos-4.1.100.Final-osx-x86_64.jar\:/opt/dremio/jars/3rdparty/google-http-client-1.39.0.jar\:/opt/dremio/jars/3rdparty/hadoop-yarn-client-3.3.2-dremio-202310112100510489-6e65eff.jar\:/opt/dremio/jars/3rdparty/jetty-xml-9.4.51.v20230217.jar\:/opt/dremio/jars/3rdparty/lucene-queries-7.7.3.jar\:/opt/dremio/jars/3rdparty/gcsio-2.2.2-dremio-202306291124120084-8ab9811.jar\:/opt/dremio/jars/3rdparty/google-api-services-storage-v1-rev20190624-1.30.1.jar\:/opt/dremio/jars/3rdparty/aircompressor-0.24.jar\:/opt/dremio/jars/3rdparty/hk2-locator-2.6.1.jar\:/opt/dremio/jars/3rdparty/lucene-misc-7.7.3.jar\:/opt/dremio/jars/3rdparty/j2objc-annotations-2.8.jar\:/opt/dremio/jars/3rdparty/netty-codec-4.1.100.Final.jar\:/opt/dremio/jars/3rdparty/parquet-generator-1.12.0-202309080020000384-9c11bcb.jar\:/opt/dremio/jars/3rdparty/guava-32.0.1-jre.jar\:/opt/dremio/jars/3rdparty/jul-to-slf4j-1.7.36.jar\:/opt/dremio/jars/3rdparty/opentracing-grpc-0.2.0.jar\:/opt/dremio/jars/3rdparty/curator-x-discovery-4.2.0.jar\:/opt/dremio/jars/3rdparty/osgi-resource-locator-1.0.3.jar\:/opt/dremio/jars/3rdparty/aws-xml-protocol-2.17.295.jar\:/opt/dremio/jars/3rdparty/gax-1.48.0.jar\:/opt/dremio/jars/3rdparty/websocket-server-9.4.51.v20230217.jar\:/opt/dremio/jars/3rdparty/jetty-continuation-9.4.51.v20230217.jar\:/opt/dremio/jars/3rdparty/httpcore5-5.1.3.jar\:/opt/dremio/jars/3rdparty/parquet-avro-1.13.1.jar\:/opt/dremio/jars/3rdparty/httpclient-4.5.14.jar\:/opt/dremio/jars/3rdparty/sketches-core-0.9.0.jar\:/opt/dremio/jars/3rdparty/cel-core-0.3.12.jar\:/opt/dremio/jars/3rdparty/hadoop-azure-2.8.5-dremio-r2-202307051740280715-b762c27.jar\:/opt/dremio/jars/3rdparty/netty-3.10.6.Final-nohttp.jar\:/opt/dremio/jars/3rdparty/cel-tools-0.3.12.jar\:/opt/dremio/jars/3rdparty/kerb-admin-1.0.1.jar\:/opt/dremio/jars/3rdparty/agrona-1.18.2.jar\:/opt/dremio/jars/3rdparty/aws-java-sdk-sts-1.12.400.jar\:/opt/dremio/jars/3rdparty/javax.activation-1.2.0.jar\:/opt/dremio/jars/3rdparty/minlog-1.3.0.jar\:/opt/dremio/jars/3rdparty/hadoop-shaded-protobuf_3_7-1.1.1.jar\:/opt/dremio/jars/3rdparty/kerb-identity-1.0.1.jar\:/opt/dremio/jars/3rdparty/netty-transport-classes-epoll-4.1.100.Final.jar\:/opt/dremio/jars/3rdparty/commons-cli-1.2.jar\:/opt/dremio/jars/3rdparty/parquet-encoding-1.12.0-202309080020000384-9c11bcb.jar\:/opt/dremio/jars/3rdparty/jackson-datatype-protobuf-0.9.13.jar\:/opt/dremio/jars/3rdparty/protostuff-collectionschema-1.4.4.jar\:/opt/dremio/jars/3rdparty/zookeeper-3.4.14.jar\:/opt/dremio/jars/3rdparty/aggdesigner-algorithm-6.0.jar\:/opt/dremio/jars/3rdparty/netty-tcnative-boringssl-static-2.0.61.Final-linux-x86_64.jar\:/opt/dremio/jars/3rdparty/flogger-system-backend-0.5.1.jar\:/opt/dremio/jars/3rdparty/flatbuffers-java-1.12.0.jar\:/opt/dremio/jars/3rdparty/profiles-2.17.295.jar\:/opt/dremio/jars/3rdparty/dremio-client-base-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/3rdparty/websocket-api-9.4.51.v20230217.jar\:/opt/dremio/jars/3rdparty/opencensus-contrib-dropwizard-0.31.1.jar\:/opt/dremio/jars/3rdparty/nessie-versioned-persist-serialize-proto-0.64.0.jar\:/opt/dremio/jars/3rdparty/foodmart-data-json-0.4.jar\:/opt/dremio/jars/3rdparty/metrics-spi-2.17.295.jar\:/opt/dremio/jars/3rdparty/jetty-server-9.4.51.v20230217.jar\:/opt/dremio/jars/3rdparty/bcprov-jdk15on-1.64.jar\:/opt/dremio/jars/3rdparty/spotbugs-annotations-3.1.9.jar\:/opt/dremio/jars/3rdparty/opentelemetry-sdk-trace-1.27.0.jar\:/opt/dremio/jars/3rdparty/HdrHistogram-2.1.9.jar\:/opt/dremio/jars/3rdparty/jersey-common-2.40.jar\:/opt/dremio/jars/3rdparty/javax.ws.rs-api-2.1.1.jar\:/opt/dremio/jars/3rdparty/opentracing-util-0.33.0.jar\:/opt/dremio/jars/3rdparty/opentelemetry-sdk-metrics-1.27.0.jar\:/opt/dremio/jars/3rdparty/checker-compat-qual-2.5.3.jar\:/opt/dremio/jars/3rdparty/iceberg-views-0.64.0.jar\:/opt/dremio/jars/3rdparty/db2-shade-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/3rdparty/hadoop-yarn-api-3.3.2-dremio-202310112100510489-6e65eff.jar\:/opt/dremio/jars/3rdparty/listenablefuture-9999.0-empty-to-avoid-conflict-with-guava.jar\:/opt/dremio/jars/3rdparty/reactive-streams-1.0.2.jar\:/opt/dremio/jars/3rdparty/grpc-netty-1.56.1.jar\:/opt/dremio/jars/3rdparty/jackson-module-afterburner-2.15.2.jar\:/opt/dremio/jars/3rdparty/oraclepki-19.3.0.0.jar\:/opt/dremio/jars/3rdparty/flight-grpc-12.0.1-20231103121511-850ae5a2d6-dremio.jar\:/opt/dremio/jars/3rdparty/netty-transport-udt-4.1.100.Final.jar\:/opt/dremio/jars/3rdparty/http-client-spi-2.17.295.jar\:/opt/dremio/jars/3rdparty/rsql-parser-2.1.0.jar\:/opt/dremio/jars/3rdparty/grpc-stub-1.56.1.jar\:/opt/dremio/jars/3rdparty/simpleclient_tracer_otel_agent-0.16.0.jar\:/opt/dremio/jars/3rdparty/jackson-dataformat-xml-2.15.2.jar\:/opt/dremio/jars/3rdparty/wildfly-openssl-1.1.3.Final.jar\:/opt/dremio/jars/3rdparty/jettison-1.5.4.jar\:/opt/dremio/jars/3rdparty/grpc-api-1.56.1.jar\:/opt/dremio/jars/3rdparty/jersey-container-jetty-http-2.40.jar\:/opt/dremio/jars/3rdparty/grpc-context-1.56.1.jar\:/opt/dremio/jars/3rdparty/jaeger-tracerresolver-1.5.0.jar\:/opt/dremio/jars/3rdparty/failureaccess-1.0.1.jar\:/opt/dremio/jars/3rdparty/nessie-client-0.64.0.jar\:/opt/dremio/jars/3rdparty/re2j-1.1.jar\:/opt/dremio/jars/3rdparty/metrics-core-4.1.19.jar\:/opt/dremio/jars/3rdparty/netty-handler-ssl-ocsp-4.1.100.Final.jar\:/opt/dremio/jars/3rdparty/nessie-versioned-persist-adapter-0.64.0.jar\:/opt/dremio/jars/3rdparty/log4j-over-slf4j-1.7.36.jar\:/opt/dremio/jars/3rdparty/netty-codec-haproxy-4.1.100.Final.jar\:/opt/dremio/jars/3rdparty/calcite-linq4j-1.17.0-202306292205070184-28a9ae90.jar\:/opt/dremio/jars/3rdparty/netty-codec-redis-4.1.100.Final.jar\:/opt/dremio/jars/3rdparty/jersey-entity-filtering-2.40.jar\:/opt/dremio/jars/3rdparty/dremio-client-jdbc-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/3rdparty/netty-tcnative-boringssl-static-2.0.61.Final-osx-x86_64.jar\:/opt/dremio/jars/3rdparty/netty-resolver-dns-4.1.100.Final.jar\:/opt/dremio/jars/3rdparty/hadoop-aws-3.3.2-dremio-202310112100510489-6e65eff.jar\:/opt/dremio/jars/3rdparty/mongodb-driver-sync-4.8.2.jar\:/opt/dremio/jars/3rdparty/opentelemetry-exporter-otlp-common-1.27.0.jar\:/opt/dremio/jars/3rdparty/commons-io-2.11.0.jar\:/opt/dremio/jars/3rdparty/jackson-datatype-guava-2.15.2.jar\:/opt/dremio/jars/3rdparty/azure-core-1.22.0.jar\:/opt/dremio/jars/3rdparty/iceberg-core-1.3.0-7dbdfd3-20230614154222-545fbe0.jar\:/opt/dremio/jars/3rdparty/jersey-client-2.40.jar\:/opt/dremio/jars/3rdparty/azure-storage-common-12.14.1.jar\:/opt/dremio/jars/3rdparty/netty-tcnative-boringssl-static-2.0.61.Final.jar\:/opt/dremio/jars/3rdparty/snowflake-jdbc-3.13.33.jar\:/opt/dremio/jars/3rdparty/hadoop-auth-3.3.2-dremio-202310112100510489-6e65eff.jar\:/opt/dremio/jars/3rdparty/esri-geometry-api-2.0.0.jar\:/opt/dremio/jars/3rdparty/opentelemetry-micrometer-1.5-1.27.0-alpha.jar\:/opt/dremio/jars/3rdparty/reflections-0.10.2.jar\:/opt/dremio/jars/3rdparty/azure-keyvault-core-1.0.0.jar\:/opt/dremio/jars/3rdparty/nessie-gc-iceberg-0.64.0.jar\:/opt/dremio/jars/3rdparty/kotlin-stdlib-jdk7-1.8.0.jar\:/opt/dremio/jars/3rdparty/glue-2.17.295.jar\:/opt/dremio/jars/3rdparty/calcite-core-1.17.0-202306292205070184-28a9ae90.jar\:/opt/dremio/jars/3rdparty/hadoop-hdfs-3.3.2-dremio-202310112100510489-6e65eff.jar\:/opt/dremio/jars/3rdparty/antlr-runtime-3.4.jar\:/opt/dremio/jars/3rdparty/rocksdbjni-7.10.2.jar\:/opt/dremio/jars/3rdparty/lucene-highlighter-7.7.3.jar\:/opt/dremio/jars/3rdparty/opencensus-impl-0.31.1.jar\:/opt/dremio/jars/3rdparty/parquet-arrow-1.12.0-202309080020000384-9c11bcb.jar\:/opt/dremio/jars/3rdparty/lang-tag-1.4.4.jar\:/opt/dremio/jars/3rdparty/hadoop-mapreduce-client-jobclient-3.3.2-dremio-202310112100510489-6e65eff.jar\:/opt/dremio/jars/3rdparty/iceberg-api-1.3.0-7dbdfd3-20230614154222-545fbe0.jar\:/opt/dremio/jars/3rdparty/opentelemetry-api-events-1.27.0-alpha.jar\:/opt/dremio/jars/3rdparty/aws-java-sdk-dynamodb-1.12.400.jar\:/opt/dremio/jars/3rdparty/poi-ooxml-4.1.2.jar\:/opt/dremio/jars/3rdparty/commons-pool-1.6.jar\:/opt/dremio/jars/3rdparty/jsr305-3.0.2.jar\:/opt/dremio/jars/3rdparty/opentracing-tracerresolver-0.1.8.jar\:/opt/dremio/jars/3rdparty/leveldbjni-all-1.8.jar\:/opt/dremio/jars/3rdparty/azure-data-lake-store-sdk-2.3.10-202208021035330109-f5bda9e.jar\:/opt/dremio/jars/3rdparty/avro-1.11.3.jar\:/opt/dremio/jars/3rdparty/apache-client-2.17.295.jar\:/opt/dremio/jars/3rdparty/netty-buffer-4.1.100.Final.jar\:/opt/dremio/jars/3rdparty/classmate-1.5.1.jar\:/opt/dremio/jars/3rdparty/azure-storage-8.3.0.jar\:/opt/dremio/jars/3rdparty/api-common-1.8.1.jar\:/opt/dremio/jars/3rdparty/nessie-protobuf-relocated-0.64.0.jar\:/opt/dremio/jars/3rdparty/javax.el-3.0.1-b11.jar\:/opt/dremio/jars/3rdparty/dnsjava-2.1.7.jar\:/opt/dremio/jars/3rdparty/content-type-2.1.jar\:/opt/dremio/jars/3rdparty/redshift-jdbc42-2.1.0.8.jar\:/opt/dremio/jars/3rdparty/flight-sql-12.0.1-20231103121511-850ae5a2d6-dremio.jar\:/opt/dremio/jars/3rdparty/gcs-connector-hadoop3-2.2.2-dremio-202306291124120084-8ab9811-shaded.jar\:/opt/dremio/jars/3rdparty/curvesapi-1.06.jar\:/opt/dremio/jars/3rdparty/opentelemetry-context-1.27.0.jar\:/opt/dremio/jars/3rdparty/okio-3.2.0.jar\:/opt/dremio/jars/3rdparty/kerb-server-1.0.1.jar\:/opt/dremio/jars/3rdparty/commons-text-1.10.0.jar\:/opt/dremio/jars/3rdparty/kryo-4.0.1.jar\:/opt/dremio/jars/3rdparty/micrometer-commons-1.11.1.jar\:/opt/dremio/jars/3rdparty/netty-transport-native-kqueue-4.1.100.Final-osx-aarch_64.jar\:/opt/dremio/jars/3rdparty/httpcore5-h2-5.1.3.jar\:/opt/dremio/jars/3rdparty/metrics-jetty9-4.1.19.jar\:/opt/dremio/jars/3rdparty/commons-codec-1.15.jar\:/opt/dremio/jars/3rdparty/simplefan-19.3.0.0.jar\:/opt/dremio/jars/3rdparty/utils-2.17.295.jar\:/opt/dremio/jars/3rdparty/config-1.4.2.jar\:/opt/dremio/jars/3rdparty/netty-resolver-dns-classes-macos-4.1.100.Final.jar\:/opt/dremio/jars/3rdparty/google-cloud-core-http-1.88.0.jar\:/opt/dremio/jars/3rdparty/jetty-io-9.4.51.v20230217.jar\:/opt/dremio/jars/3rdparty/ojdbc8-19.3.0.0.jar\:/opt/dremio/jars/3rdparty/jaeger-core-1.5.0.jar\:/opt/dremio/jars/3rdparty/hibernate-validator-6.2.0.Final.jar\:/opt/dremio/jars/3rdparty/opentelemetry-sdk-1.27.0.jar\:/opt/dremio/jars/3rdparty/grpc-core-1.56.1.jar\:/opt/dremio/jars/3rdparty/unboundid-ldapsdk-4.0.9.jar\:/opt/dremio/jars/3rdparty/aws-java-sdk-s3-1.12.400.jar\:/opt/dremio/jars/3rdparty/pf4j-3.6.0.jar\:/opt/dremio/jars/3rdparty/jackson-datatype-jdk8-2.15.2.jar\:/opt/dremio/jars/3rdparty/websocket-common-9.4.51.v20230217.jar\:/opt/dremio/jars/3rdparty/jackson-databind-2.15.2.jar\:/opt/dremio/jars/3rdparty/javax.el-2.2.6.jar\:/opt/dremio/jars/3rdparty/netty-transport-native-epoll-4.1.100.Final-linux-x86_64.jar\:/opt/dremio/jars/3rdparty/websocket-client-9.4.51.v20230217.jar\:/opt/dremio/jars/3rdparty/grpc-protobuf-lite-1.56.1.jar\:/opt/dremio/jars/3rdparty/nessie-gc-base-0.64.0.jar\:/opt/dremio/jars/3rdparty/opentelemetry-sdk-common-1.27.0.jar\:/opt/dremio/jars/3rdparty/cel-jackson-0.3.12.jar\:/opt/dremio/jars/3rdparty/nessie-rest-services-0.64.0.jar\:/opt/dremio/jars/3rdparty/netty-codec-xml-4.1.100.Final.jar\:/opt/dremio/jars/3rdparty/websocket-servlet-9.4.51.v20230217.jar\:/opt/dremio/jars/3rdparty/dremio-twill-shaded-24.2.6-202311250456170399-68acbe47.jar\:/opt/dremio/jars/3rdparty/jersey-container-jetty-servlet-2.40.jar\:/opt/dremio/jars/3rdparty/annotations-4.1.1.4.jar\:/opt/dremio/jars/3rdparty/netty-tcnative-boringssl-static-2.0.61.Final-osx-aarch_64.jar\:/opt/dremio/jars/3rdparty/hadoop-mapreduce-client-core-3.3.2-dremio-202310112100510489-6e65eff.jar\:/opt/dremio/jars/3rdparty/lucene-backward-codecs-7.7.3.jar\:/opt/dremio/jars/3rdparty/kerby-xdr-1.0.1.jar\:/opt/dremio/jars/3rdparty/opencensus-contrib-http-util-0.28.0.jar\:/opt/dremio/jars/3rdparty/checker-qual-3.33.0.jar\:/opt/dremio/jars/3rdparty/jackson-dataformat-smile-2.15.2.jar\:/opt/dremio/jars/3rdparty/lucene-spatial-7.7.3.jar\:/opt/dremio/jars/3rdparty/xml-apis-1.4.01.jar\:/opt/dremio/jars/3rdparty/jersey-guice-1.19.jar\:/opt/dremio/jars/3rdparty/google-http-client-apache-v2-1.39.0.jar\:/opt/dremio/jars/3rdparty/postgresql-42.4.1.jar\:/opt/dremio/jars/3rdparty/aws-java-sdk-redshift-internal-1.12.x.jar\:/opt/dremio/jars/3rdparty/mongodb-driver-core-4.8.2.jar\:/opt/dremio/jars/3rdparty/paranamer-2.5.6.jar\:/opt/dremio/jars/3rdparty/netty-codec-dns-4.1.100.Final.jar\:/opt/dremio/jars/3rdparty/protoparser-4.0.3.jar\:/opt/dremio/jars/3rdparty/google-cloud-core-1.88.0.jar\:/opt/dremio/jars/3rdparty/okhttp-4.11.0.jar\:/opt/dremio/jars/3rdparty/proto-google-common-protos-2.17.0.jar\:/opt/dremio/jars/3rdparty/iceberg-aws-1.3.0-7dbdfd3-20230614154222-545fbe0.jar\:/opt/dremio/jars/3rdparty/jackson-dataformat-yaml-2.15.2.jar\:/opt/dremio/jars/3rdparty/netty-handler-4.1.100.Final.jar\:/opt/dremio/jars/3rdparty/commons-daemon-1.0.13.jar\:/opt/dremio/jars/3rdparty/affinity-3.1.7.jar\:/opt/dremio/jars/3rdparty/json-utils-2.17.295.jar\:/opt/dremio/jars/3rdparty/jackson-jaxrs-base-2.15.2.jar\:/opt/dremio/jars/3rdparty/jetty-util-9.4.51.v20230217.jar\:/opt/dremio/jars/3rdparty/netty-codec-socks-4.1.100.Final.jar\:/opt/dremio/jars/3rdparty/netty-transport-native-epoll-4.1.100.Final-linux-aarch_64.jar\:/opt/dremio/jars/3rdparty/okio-jvm-3.4.0.jar\:/opt/dremio/jars/3rdparty/bson-4.8.2.jar\:/opt/dremio/jars/3rdparty/gax-httpjson-0.65.0.jar\:/opt/dremio/jars/3rdparty/opentelemetry-sdk-extension-jaeger-remote-sampler-1.27.0.jar\:/opt/dremio/jars/3rdparty/hppc-0.7.1.jar\:/opt/dremio/jars/3rdparty/netty-codec-http2-4.1.100.Final.jar\:/opt/dremio/jars/3rdparty/cel-generated-pb-0.3.12.jar\:/opt/dremio/jars/3rdparty/simpleclient_common-0.16.0.jar\:/opt/dremio/jars/3rdparty/protostuff-core-1.4.4.jar\:/opt/dremio/jars/3rdparty/jersey-servlet-1.19.jar\:/opt/dremio/jars/3rdparty/protostuff-runtime-1.4.4.jar\:/opt/dremio/jars/3rdparty/lucene-spatial3d-7.7.3.jar\:/opt/dremio/jars/3rdparty/jaxb-api-2.2.11.jar\:/opt/dremio/jars/3rdparty/opencensus-impl-core-0.31.1.jar\:/opt/dremio/jars/3rdparty/ucp-19.3.0.0.jar\:/opt/dremio/jars/3rdparty/shims-0.9.44.jar\:/opt/dremio/jars/3rdparty/jersey-client-1.19.jar\:/opt/dremio/jars/3rdparty/jetty-http-9.4.51.v20230217.jar\:/opt/dremio/jars/3rdparty/aws-java-sdk-core-1.12.400.jar\:/opt/dremio/jars/3rdparty/opencensus-api-0.31.1.jar\:/opt/dremio/jars/3rdparty/jersey-hk2-2.40.jar\:/opt/dremio/jars/3rdparty/jersey-container-servlet-2.40.jar\:/opt/dremio/jars/3rdparty/lz4-java-1.7.1.jar\:/opt/dremio/jars/3rdparty/jcl-over-slf4j-1.7.36.jar\:/opt/dremio/jars/3rdparty/accessors-smart-2.4.9.jar\:/opt/dremio/jars/3rdparty/scim2-sdk-client-2.3.5.jar\:/opt/dremio/jars/3rdparty/hamcrest-2.1.jar\:/opt/dremio/jars/3rdparty/jakarta.inject-api-2.0.1.jar\:/opt/dremio/jars/3rdparty/jcommander-1.82.jar\:/opt/dremio/jars/3rdparty/gson-2.10.1.jar\:/opt/dremio/jars/3rdparty/ion-java-1.0.2.jar\:/opt/dremio/jars/3rdparty/azure-core-http-netty-1.11.2.jar\:/opt/dremio/jars/3rdparty/google-http-client-gson-1.39.0.jar\:/opt/dremio/jars/3rdparty/kerb-simplekdc-1.0.1.jar\:/opt/dremio/jars/3rdparty/audience-annotations-0.5.0.jar\:/opt/dremio/jars/3rdparty/oauth2-client-2.40.jar\:/opt/dremio/jars/3rdparty/rolling-metrics-2.0.5.jar\:/opt/dremio/jars/3rdparty/netty-tcnative-boringssl-static-2.0.61.Final-windows-x86_64.jar\:/opt/dremio/jars/3rdparty/google-auth-library-credentials-0.17.1.jar\:/opt/dremio/jars/3rdparty/aws-java-sdk-redshift-1.12.400.jar\:/opt/dremio/jars/3rdparty/protostuff-json-1.4.4.jar\:/opt/dremio/jars/3rdparty/netty-common-4.1.100.Final.jar\:/opt/dremio/jars/3rdparty/jakarta.ws.rs-api-2.1.6.jar\:/opt/dremio/jars/3rdparty/nessie-versioned-persist-in-memory-0.64.0.jar\:/opt/dremio/jars/3rdparty/scim2-sdk-server-2.3.5.jar\:/opt/dremio/jars/3rdparty/sts-2.17.295.jar\:/opt/dremio/jars/3rdparty/netty-resolver-4.1.100.Final.jar\:/opt/dremio/jars/3rdparty/google-oauth-client-1.34.1.jar\:/opt/dremio/jars/3rdparty/netty-codec-stomp-4.1.100.Final.jar\:/opt/dremio/jars/3rdparty/jackson-dataformat-protobuf-2.15.2.jar\:/opt/dremio/jars/3rdparty/metrics-jmx-4.1.19.jar\:/opt/dremio/jars/3rdparty/nessie-server-store-0.64.0.jar\:/opt/dremio/jars/3rdparty/opentelemetry-sdk-logs-1.27.0.jar\:/opt/dremio/jars/3rdparty/opentracing-api-0.33.0.jar\:/opt/dremio/jars/3rdparty/async-http-client-2.7.0.jar\:/opt/dremio/jars/3rdparty/jetty-webapp-9.4.51.v20230217.jar\:/opt/dremio/jars/3rdparty/protostuff-api-1.4.4.jar\:/opt/dremio/jars/3rdparty/jersey-media-multipart-2.40.jar\:/opt/dremio/jars/3rdparty/mariadb-java-client-3.0.8.jar\:/opt/dremio/jars/3rdparty/janino-3.1.6.jar\:/opt/dremio/jars/3rdparty/koloboke-impl-common-jdk8-1.0.0.jar\:/opt/dremio/jars/3rdparty/proto-google-iam-v1-1.0.5.jar\:/opt/dremio/jars/3rdparty/opentelemetry-semconv-1.27.0-alpha.jar\:/opt/dremio/jars/3rdparty/annotations-13.0.jar\:/opt/dremio/jars/3rdparty/jakarta.annotation-api-1.3.5.jar\:/opt/dremio/jars/3rdparty/netty-codec-http-4.1.100.Final.jar\:/opt/dremio/jars/3rdparty/jackson-jaxrs-json-provider-2.15.2.jar\:/opt/dremio/jars/3rdparty/netty-reactive-streams-2.0.0.jar\:/opt/dremio/jars/3rdparty/commons-lang3-3.12.0.jar\:/opt/dremio/jars/3rdparty/sdk-core-2.17.295.jar\:/opt/dremio/jars/3rdparty/iceberg-common-1.3.0-7dbdfd3-20230614154222-545fbe0.jar\:/opt/dremio/jars/3rdparty/cdi-api-2.0.jar\:/opt/dremio/jars/3rdparty/kerb-crypto-1.0.1.jar\:/opt/dremio/jars/3rdparty/jackson-annotations-2.15.2.jar\:/opt/dremio/jars/3rdparty/hk2-api-2.6.1.jar\:/opt/dremio/jars/3rdparty/microprofile-openapi-api-3.1.jar\:/opt/dremio/jars/3rdparty/joda-time-2.12.1.jar\:/opt/dremio/jars/3rdparty/json-smart-2.4.10.jar\:/opt/dremio/jars/3rdparty/google-http-client-jackson2-1.38.0.jar\:/opt/dremio/jars/3rdparty/annotations-3.0.1u2.jar\:/opt/dremio/jars/3rdparty/nessie-versioned-persist-serialize-0.64.0.jar\:/opt/dremio/jars/3rdparty/kotlin-stdlib-common-1.6.20.jar\:/opt/dremio/jars/3rdparty/lucene-suggest-7.7.3.jar\:/opt/dremio/jars/3rdparty/jersey-container-servlet-core-2.40.jar\:/opt/dremio/jars/3rdparty/netty-transport-4.1.100.Final.jar\:/opt/dremio/jars/3rdparty/lucene-queryparser-7.7.3.jar\:/opt/dremio/jars/3rdparty/bcpkix-jdk15on-1.64.jar\:/opt/dremio/jars/3rdparty/opentracing-noop-0.33.0.jar\:/opt/dremio/jars/3rdparty/nessie-versioned-persist-non-transactional-0.64.0.jar\:/opt/dremio/jars/3rdparty/netty-nio-client-2.17.295.jar\:/opt/dremio/jars/3rdparty/datasketches-java-2.0.0.jar\:/opt/dremio/jars/3rdparty/guice-servlet-6.0.0.jar\:/opt/dremio/jars/3rdparty/asciitable-0.2.5.jar\:/opt/dremio/jars/3rdparty/memory-0.9.0.jar\:/opt/dremio/jars/3rdparty/stax2-api-4.2.jar\:/opt/dremio/jars/3rdparty/objenesis-2.4.jar\:/opt/dremio/jars/3rdparty/nessie-versioned-spi-0.64.0.jar\:/opt/dremio/jars/3rdparty/freemarker-2.3.32.jar\:/opt/dremio/jars/3rdparty/poi-ooxml-schemas-4.1.2.jar\:/opt/dremio/jars/3rdparty/parquet-common-1.12.0-202309080020000384-9c11bcb.jar\:/opt/dremio/jars/3rdparty/elasticsearch-core-6.8.23.jar\:/opt/dremio/jars/3rdparty/jcip-annotations-1.0-1.jar\:/opt/dremio/jars/3rdparty/commons-beanutils-1.9.4.jar\:/opt/dremio/jars/3rdparty/parquet-column-1.12.0-202309080020000384-9c11bcb.jar\:/opt/dremio/jars/3rdparty/third-party-jackson-core-2.17.295.jar\:/opt/dremio/jars/3rdparty/adal4j-1.6.7.jar\:/opt/dremio/jars/3rdparty/nessie-versioned-persist-store-0.64.0.jar\:/opt/dremio/jars/3rdparty/arrow-jdbc-12.0.1-20231103121511-850ae5a2d6-dremio.jar\:/opt/dremio/jars/3rdparty/netty-all-4.1.100.Final.jar\:/opt/dremio/jars/3rdparty/jackson-core-2.15.2.jar\:/opt/dremio/jars/3rdparty/jaeger-thrift-1.5.0.jar\:/opt/dremio/jars/3rdparty/threetenbp-1.3.3.jar\:/opt/dremio/jars/3rdparty/caffeine-2.9.3.jar\:/opt/dremio/jars/3rdparty/hadoop-annotations-3.3.2-dremio-202310112100510489-6e65eff.jar\:/opt/dremio/jars/3rdparty/commons-configuration2-2.1.1.jar\:/opt/dremio/jars/3rdparty/commons-compiler-3.1.6.jar\:/opt/dremio/jars/3rdparty/opentelemetry-extension-trace-propagators-1.27.0.jar\:/opt/dremio/jars/3rdparty/opentelemetry-sdk-extension-autoconfigure-spi-1.27.0.jar\:/opt/dremio/jars/3rdparty/univocity-parsers-1.3.0.jar\:/opt/dremio/jars/3rdparty/avatica-core-1.23.0.jar\:/opt/dremio/jars/3rdparty/opentelemetry-exporter-logging-otlp-1.27.0.jar\:/opt/dremio/jars/3rdparty/jcc-11.5.8.0.jar\:/opt/dremio/jars/3rdparty/codemodel-2.6.jar\:/opt/dremio/jars/3rdparty/kerb-common-1.0.1.jar\:/opt/dremio/jars/3rdparty/google-http-client-appengine-1.31.0.jar\:/opt/dremio/jars/3rdparty/parquet-format-structures-1.12.0-202309080020000384-9c11bcb.jar\:/opt/dremio/jars/3rdparty/opentelemetry-instrumentation-api-1.27.0.jar\:/opt/dremio/jars/3rdparty/micrometer-core-1.11.1.jar\:/opt/dremio/jars/3rdparty/osdt_core-19.3.0.0.jar\:/opt/dremio/jars/3rdparty/netty-handler-proxy-4.1.100.Final.jar\:/opt/dremio/jars/3rdparty/snakeyaml-2.0.jar\:/opt/dremio/jars/3rdparty/RoaringBitmap-0.9.44.jar\:/opt/dremio/jars/3rdparty/jackson-module-jaxb-annotations-2.15.2.jar\:/opt/dremio/jars/3rdparty/netty-codec-smtp-4.1.100.Final.jar\:/opt/dremio/jars/3rdparty/google-api-client-jackson2-1.31.3.jar\:/opt/dremio/jars/3rdparty/opentelemetry-extension-incubator-1.27.0-alpha.jar\:/opt/dremio/jars/3rdparty/simpleclient_servlet_common-0.16.0.jar\:/opt/dremio/jars/3rdparty/kerby-config-1.0.1.jar\:/opt/dremio/jars/3rdparty/opentelemetry-instrumentation-api-semconv-1.27.0-alpha.jar\:/opt/dremio/jars/3rdparty/curator-client-4.2.0.jar\:/opt/dremio/jars/3rdparty/protocol-core-2.17.295.jar\:/opt/dremio/jars/3rdparty/reflectasm-1.11.3.jar\:/opt/dremio/jars/3rdparty/libthrift-0.13.0.jar\:/opt/dremio/jars/3rdparty/jetty-client-9.4.51.v20230217.jar\:/opt/dremio/jars/3rdparty/jackson-datatype-jsr310-2.15.2.jar\:/opt/dremio/jars/3rdparty/SparseBitSet-1.2.jar\:/opt/dremio/jars/3rdparty/parquet-hadoop-1.12.0-202309080020000384-9c11bcb.jar\:/opt/dremio/jars/3rdparty/disruptor-3.4.2.jar\:/opt/dremio/jars/3rdparty/kerb-util-1.0.1.jar\:/opt/dremio/jars/3rdparty/arrow-vector-12.0.1-20231103121511-850ae5a2d6-dremio.jar\:/opt/dremio/jars/3rdparty/modelmapper-protobuf-2.3.0.jar\:/opt/dremio/jars/3rdparty/woodstox-core-5.4.0.jar\:/opt/dremio/jars/3rdparty/arrow-memory-netty-12.0.1-20231103121511-850ae5a2d6-dremio.jar\:/opt/dremio/jars/3rdparty/jboss-logging-3.4.1.Final.jar\:/opt/dremio/jars/3rdparty/aws-java-sdk-lakeformation-1.12.400.jar\:/opt/dremio/jars/3rdparty/httpcore-4.4.16.jar\:/opt/dremio/jars/3rdparty/logback-core-1.2.12.jar\:/opt/dremio/jars/3rdparty/jetty-servlets-9.4.51.v20230217.jar\:/opt/dremio/jars/3rdparty/eventstream-1.0.1.jar\:/opt/dremio/jars/3rdparty/commons-dbcp2-2.2.0.jar\:/opt/dremio/jars/3rdparty/arrow-memory-core-12.0.1-20231103121511-850ae5a2d6-dremio.jar\:/opt/dremio/jars/3rdparty/grpc-protobuf-1.56.1.jar\:/opt/dremio/jars/3rdparty/token-provider-1.0.1.jar\:/opt/dremio/jars/3rdparty/opentelemetry-exporter-common-1.27.0.jar\:/opt/dremio/jars/3rdparty/google-auth-library-oauth2-http-0.22.2.jar\:/opt/dremio/jars/3rdparty/elasticsearch-6.8.23.jar\:/opt/dremio/jars/3rdparty/kerb-client-1.0.1.jar\:/opt/dremio/jars/3rdparty/commons-math3-3.6.1.jar\:/opt/dremio/jars/3rdparty/jackson-module-jsonSchema-2.15.2.jar\:/opt/dremio/jars/3rdparty/lucene-memory-7.7.3.jar\:/opt/dremio/jars/3rdparty/protobuf-java-util-3.21.9.jar\:/opt/dremio/jars/3rdparty/log4j-to-slf4j-2.19.0.jar\:/opt/dremio/jars/3rdparty/google-extensions-0.5.1.jar\:/opt/dremio/jars/3rdparty/javax.inject-1.jar\:/opt/dremio/jars/3rdparty/annotations-12.0.jar\:/opt/dremio/jars/3rdparty/native-lib-loader-2.3.4.jar\:/opt/dremio/jars/3rdparty/t-digest-3.2.jar\:/opt/dremio/jars/3rdparty/elasticsearch-x-content-6.8.23.jar\:/opt/dremio/jars/3rdparty/ons-19.3.0.0.jar\:/opt/dremio/jars/3rdparty/hadoop-mapreduce-client-common-3.3.2-dremio-202310112100510489-6e65eff.jar\:/opt/dremio/jars/3rdparty/commons-compress-1.23.0.jar\:/opt/dremio/jars/3rdparty/datasketches-memory-1.3.0.jar\:/opt/dremio/jars/3rdparty/slf4j-api-1.7.36.jar\:/opt/dremio/jars/3rdparty/simpleclient_servlet-0.16.0.jar\:/opt/dremio/jars/3rdparty/curator-recipes-4.2.0.jar\:/opt/dremio/jars/3rdparty/jsch-0.1.55.jar\:/opt/dremio/jars/3rdparty/netty-transport-sctp-4.1.100.Final.jar\:/opt/dremio/jars/3rdparty/lucene-core-7.7.3.jar\:/opt/dremio/jars/3rdparty/asm-9.5.jar\:/opt/dremio/jars/3rdparty/flight-core-12.0.1-20231103121511-850ae5a2d6-dremio.jar\:/opt/dremio/jars/3rdparty/lucene-analyzers-common-7.7.3.jar\:/opt/dremio/jars/3rdparty/poi-4.1.2.jar\:/opt/dremio/jars/3rdparty/zstd-jni-1.5.0-1.jar\:/opt/dremio/jars/3rdparty/parquet-format-2.7.0-201901172054060715-5352a59.jar\:/opt/dremio/jars/3rdparty/commons-collections-3.2.2.jar\:/opt/dremio/jars/3rdparty/jetty-servlet-9.4.51.v20230217.jar\:/opt/dremio/jars/3rdparty/lucene-spatial-extras-7.7.3.jar\:/opt/dremio/jars/3rdparty/httpclient5-5.1.3.jar\:/opt/dremio/jars/3rdparty/aopalliance-repackaged-2.6.1.jar\:/opt/dremio/jars/3rdparty/google-cloud-storage-1.88.0.jar\:/opt/java/openjdk/lib/tools.jar`
	version := parseVersionFromClassPath(f)
	expected := "24.2.6-202311250456170399-68acbe47"
	if version != expected {
		t.Errorf("expected %v but was '%v'", expected, version)
	}
}
