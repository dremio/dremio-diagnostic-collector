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
tmp-output-dir: %v
%v
`, strings.ReplaceAll(cleaned, "\\", "\\\\"), yamlTextMinusTmpOutputDir)
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
dremio-log-dir: "/var/log/dremio" # where the dremio log is located
dremio-conf-dir: "/opt/dremio/conf/..data" #where the dremio conf files are located
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
dremio-log-dir: "/var/log/dremio" # where the dremio log is located
dremio-conf-dir: "/opt/dremio/conf/..data" #where the dremio conf files are located
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
