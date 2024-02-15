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

package ssh

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/cmd"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/cmd/root/collection"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
	"github.com/dremio/dremio-diagnostic-collector/pkg/tests"
)

type SSHTestConf struct {
	SudoUser         string `json:"sudo_user"`
	User             string `json:"user"`
	Public           string `json:"public"`
	Private          string `json:"private"`
	Executor         string `json:"executor"`
	Coordinator      string `json:"coordinator"`
	DremioLogDir     string `json:"dremio-log-dir"`
	DremioConfDir    string `json:"dremio-conf-dir"`
	DremioRocksDBDir string `json:"dremio-rocksdb-dir"`
	DremioUsername   string `json:"dremio-username"`
	DremioPAT        string `json:"dremio-pat"`
	DremioEndpoint   string `json:"dremio-endpoint"`
	IsEnterprise     bool   `json:"is-enterprise"`
}

func TestSSHBasedRemoteCollect(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testing in short mode")
	}

	b, err := os.ReadFile(filepath.Join("testdata", "ssh.json"))
	if err != nil {
		t.Fatalf(`unable to read ssh.json in ./integrationtest/ssh/testdata/ssh.json
you must make one with the following format:
{
    "sudo_user": "dremio",
    "user": "myuser", 
    "public": "ssh-ed25519 publickey", 
    "private":"-----BEGIN OPENSSH PRIVATE KEY-----\nprivatekey\n-----END OPENSSH PRIVATE KEY-----\n",
    "coordinator": "coordinator-ip",
    "executor": "executor1",
    "dremio-log-dir": "/opt/dremio/log",
    "dremio-conf-dir": "/opt/dremio/conf",
    "dremio-rocksdb-dir": "/opt/dremio/cm/db/",
    "dremio-username": "dremio",
    "dremio-pat": "mytoken",
    "dremio-endpoint": "http://localhost:9047",
    "is-enterprise": true
}


Error was: %v`, err)
	}
	var sshConf SSHTestConf
	if err := json.Unmarshal(b, &sshConf); err != nil {
		t.Errorf("failed unmarshalling string: %v", err)
	}
	tgzFile := filepath.Join(t.TempDir(), "diag.tgz")
	localYamlFileDir := filepath.Join(t.TempDir(), "ddc-conf")
	if err := os.Mkdir(localYamlFileDir, 0700); err != nil {
		t.Fatalf("cannot make yaml dir %v due to error: %v", localYamlFileDir, err)
	}
	localYamlFile := filepath.Join(localYamlFileDir, "ddc.yaml")
	yamlText := fmt.Sprintf(`verbose: vvvv
dremio-log-dir: %v
dremio-conf-dir: %v
dremio-rocksdb-dir: %v
number-threads: 2
dremio-endpoint: '%v'
dremio-username: %v
dremio-pat-token: '%v'
collect-dremio-configuration: true
number-job-profiles: 25
dremio-jstack-time-seconds: 10
dremio-jfr-time-seconds: 10
`, sshConf.DremioLogDir, sshConf.DremioConfDir, sshConf.DremioRocksDBDir, sshConf.DremioEndpoint, sshConf.DremioUsername, sshConf.DremioPAT)
	if err := os.WriteFile(localYamlFile, []byte(yamlText), 0600); err != nil {
		t.Fatalf("not able to write yaml %v at due to %v", localYamlFile, err)
	}

	privateKey := filepath.Join(t.TempDir(), "ssh_key")
	if err := os.WriteFile(privateKey, []byte(sshConf.Private), 0600); err != nil {
		t.Fatalf("unable to write ssh private key: %v", err)
	}
	publicKey := filepath.Join(t.TempDir(), "ssh_key.pub")
	if err := os.WriteFile(publicKey, []byte(sshConf.Public), 0600); err != nil {
		t.Fatalf("unable to write ssh public key: %v", err)
	}
	args := []string{"ddc", "-s", privateKey, "-u", sshConf.User, "--sudo-user", sshConf.SudoUser, "-c", sshConf.Coordinator, "-e", sshConf.Executor, "--ddc-yaml", localYamlFile, "--output-file", tgzFile, "--collect", "standard"}
	err = cmd.Execute(args)
	if err != nil {
		t.Fatalf("unable to run collect: %v", err)
	}
	simplelog.Info("remote collect complete now verifying the results")
	testOut := filepath.Join(t.TempDir(), "ddcout")
	err = os.Mkdir(testOut, 0700)
	if err != nil {
		t.Fatalf("could not make test out dir %v", err)
	}
	simplelog.Infof("now in the test we are extracting tarball %v to %v", tgzFile, testOut)

	if err := collection.ExtractTarGz(tgzFile, testOut); err != nil {
		t.Fatalf("could not extract tgz %v to dir %v due to error %v", tgzFile, testOut, err)
	}
	simplelog.Infof("now we are reading the %v dir", testOut)
	entries, err := os.ReadDir(testOut)
	if err != nil {
		t.Fatal(err)
	}
	hcDir := ""
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
		if e.IsDir() {
			hcDir = filepath.Join(testOut, e.Name())
			simplelog.Infof("now found the health check directory which is %v", hcDir)
		}
	}

	if len(names) != 2 {
		t.Fatalf("expected 1 entry but had %v", strings.Join(names, ","))
	}
	tests.AssertFileHasContent(t, filepath.Join(testOut, "summary.json"))

	coordinator, err := getHostName(sshConf.Coordinator, privateKey, sshConf)
	if err != nil {
		t.Fatal(err)
	}
	executor, err := getHostName(sshConf.Executor, privateKey, sshConf)
	if err != nil {
		t.Fatal(err)
	}

	// check server.logs
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "logs", coordinator, "server.log.gz"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "logs", executor, "server.log.gz"))
	// check queries.json
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "queries", coordinator, "queries.json.gz"))
	// check conf files

	tests.AssertFileHasContent(t, filepath.Join(hcDir, "configuration", coordinator, "dremio.conf"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "configuration", coordinator, "dremio-env"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "configuration", coordinator, "logback.xml"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "configuration", coordinator, "logback-access.xml"))

	tests.AssertFileHasContent(t, filepath.Join(hcDir, "configuration", executor, "dremio.conf"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "configuration", executor, "dremio-env"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "configuration", executor, "logback.xml"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "configuration", executor, "logback-access.xml"))

	// check nodeinfo files
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "node-info", coordinator, "diskusage.txt"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "node-info", coordinator, "jvm_settings.txt"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "node-info", coordinator, "os_info.txt"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "node-info", coordinator, "rocksdb_disk_allocation.txt"))

	tests.AssertFileHasContent(t, filepath.Join(hcDir, "node-info", executor, "diskusage.txt"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "node-info", executor, "jvm_settings.txt"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "node-info", executor, "os_info.txt"))

	// check file contents
	t.Logf("checking file %v", filepath.Join(hcDir, "node-info", coordinator, "os_info.txt"))
	tests.AssertFileHasExpectedLines(t, []string{">>> mount", ">>> lsblk"}, filepath.Join(hcDir, "node-info", coordinator, "os_info.txt"))
	t.Logf("checking file %v", filepath.Join(hcDir, "node-info", executor, "os_info.txt"))
	tests.AssertFileHasExpectedLines(t, []string{">>> mount", ">>> lsblk"}, filepath.Join(hcDir, "node-info", executor, "os_info.txt"))

	//kvstore report
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "kvstore", coordinator, "kvstore-report.zip"))

	//ttop files
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "ttop", coordinator, "ttop.txt"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "ttop", executor, "ttop.txt"))

	//jfr files
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "jfr", coordinator+".jfr"))
	tests.AssertFileHasContent(t, filepath.Join(hcDir, "jfr", executor+".jfr"))

	//thread dump files
	entries, err = os.ReadDir(filepath.Join(hcDir, "jfr", "thread-dumps", executor))
	if err != nil {
		t.Fatalf("cannot read thread dumps dir for the "+coordinator+" due to: %v", err)
	}
	if len(entries) < 9 {
		//giving some wiggle room on timing so allowing a tolerance of 9 entries instead of the required 10
		t.Errorf("should be at least 9 jstack entries for "+executor+" but there was %v", len(entries))
	}

	entries, err = os.ReadDir(filepath.Join(hcDir, "jfr", "thread-dumps", coordinator))
	if err != nil {
		t.Fatalf("cannot read thread dumps dir for the "+coordinator+" due to: %v", err)
	}

	if len(entries) < 9 {
		//giving some wiggle room on timing so allowing a tolerance of 9 entries instead of the required 10
		t.Errorf("should be at least 9 jstack entries for "+executor+" but there was %v", len(entries))
	}

	// System tables
	var systemTables []string
	for _, e := range conf.SystemTableList() {
		if !sshConf.IsEnterprise {
			//we skip the known ones we don't care about when using oss for testing
			if e == "roles" || e == "membership" || e == "privileges" || e == "tables" {
				continue
			}
		}
		if e == "options" {
			//we double up for options since it's big
			systemTables = append(systemTables, "sys.options_offset_500_limit_500")
		}
		//we do the trim because sys.\"tables\" becomes sys.tables on the filesystem
		fullFileName := fmt.Sprintf("sys.%v_offset_0_limit_500.json", e)
		systemTables = append(systemTables, strings.ReplaceAll(fullFileName, "\\\"", ""))
	}
	sort.Strings(systemTables)
	var expectedEntriesCount int
	if sshConf.IsEnterprise {
		expectedEntriesCount = len(systemTables)
	} else {
		//we subtract 3 of the jobs that fail due to missing features in oss
		// - sys.privileges
		// - sys.membership
		// - sys.roles
		// and system.tables because it seems to not be setup
		// - sys.\"tables\"

		expectedEntriesCount = len(systemTables) - 4
	}

	entries, err = os.ReadDir(filepath.Join(hcDir, "system-tables", coordinator))
	if err != nil {
		t.Fatalf("cannot read system-tables dir for the "+coordinator+" due to: %v", err)
	}
	actualEntriesCount := len(entries)
	if actualEntriesCount == 0 {
		t.Error("expected more than 0 entries")
	}
	var actualEntries []string
	for _, e := range entries {
		actualEntries = append(actualEntries, e.Name())
	}
	sort.Strings(actualEntries)

	uniqueOnFileSystem, uniqueToSystemTables := tests.FindUniqueElements(actualEntries, systemTables)

	if len(uniqueOnFileSystem) == 0 && len(uniqueToSystemTables) == 0 {
		t.Errorf("expected %v but was %v we had the following entries missing:\n\n%v\n\nextra entries on filesystem:\n\n%v\n", expectedEntriesCount, actualEntriesCount, strings.Join(uniqueToSystemTables, "\n"), strings.Join(uniqueOnFileSystem, "\n"))
	}

	//validate job downloads

	entries, err = os.ReadDir(filepath.Join(hcDir, "job-profiles", coordinator))
	if err != nil {
		t.Fatalf("cannot read job profiles dir for the "+coordinator+" due to: %v", err)
	}

	// so there is some vagueness and luck with how many job profiles we download, so we are going to see if there are at least 10 of them and call that good enough
	expected := 10
	if len(entries) < 10 {
		t.Errorf("there were %v job profiles downloaded, we expected at least %v", len(entries), expected)
	}
}

func getHostName(ip string, sshKey string, sshConf SSHTestConf) (string, error) {
	var stdOut bytes.Buffer
	var stdErr bytes.Buffer
	c := exec.Command("ssh", "-i", sshKey, fmt.Sprintf("%v@%v", sshConf.User, ip), "-o", "LogLevel=error", "-o", "UserKnownHostsFile=/dev/null", "-o", "StrictHostKeyChecking=no", "hostname")
	c.Stdout = &stdOut
	c.Stderr = &stdErr
	if err := c.Start(); err != nil {
		return "", fmt.Errorf("unable to run ssh for ip: %v with error %v", ip, err)
	}
	if err := c.Wait(); err != nil {
		return "", fmt.Errorf("getting hostname for ip %v failed with stderr out of %v and stdout of %v and go error of %v", ip, stdErr.String(), stdOut.String(), err)
	}

	scanner := bufio.NewScanner(&stdOut)
	txt := ""
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Warning") {
			continue
		}
		txt += line
	}
	if txt == "" {
		return "", fmt.Errorf("no host name present: stderror was %v", stdErr.String())
	}
	return strings.TrimSpace(txt), nil
}

func TestValidateBadCollectFlag(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testing in short mode")
	}
	b, err := os.ReadFile(filepath.Join("testdata", "ssh.json"))
	if err != nil {
		t.Fatalf(`unable to read ssh.json in ./integrationtest/ssh/testdata/ssh.json
you must make one with the following format:
{
    "sudo_user": "dremio",
    "user": "myuser", 
    "public": "ssh-ed25519 publickey", 
    "private":"-----BEGIN OPENSSH PRIVATE KEY-----\nprivatekey\n-----END OPENSSH PRIVATE KEY-----\n",
    "coordinator": "coordinator-ip",
    "executor": "executor1",
    "dremio-log-dir": "/opt/dremio/log",
    "dremio-conf-dir": "/opt/dremio/conf",
    "dremio-rocksdb-dir": "/opt/dremio/cm/db/",
    "dremio-username": "dremio",
    "dremio-pat": "mytoken",
    "dremio-endpoint": "http://localhost:9047",
    "is-enterprise": true
}


Error was: %v`, err)
	}
	var sshConf SSHTestConf
	if err := json.Unmarshal(b, &sshConf); err != nil {
		t.Errorf("failed unmarshalling string: %v", err)
	}
	privateKey := filepath.Join(t.TempDir(), "ssh_key")
	if err := os.WriteFile(privateKey, []byte(sshConf.Private), 0600); err != nil {
		t.Fatalf("unable to write ssh private key: %v", err)
	}
	ddcYaml := filepath.Join(t.TempDir(), "ddc.yaml")
	if err := os.WriteFile(ddcYaml, []byte("#comment"), 0600); err != nil {
		t.Fatalf("unable to write ddc yaml: %v", err)
	}
	args := []string{"ddc", "-s", privateKey, "-u", sshConf.User, "--sudo-user", sshConf.SudoUser, "-c", sshConf.Coordinator, "-e", sshConf.Executor, "--ddc-yaml", ddcYaml, "--collect", "wrong", "--" + conf.KeyDisableFreeSpaceCheck}
	err = cmd.Execute(args)
	if err == nil {
		t.Error("collect should fail")
	}
	expected := "invalid --collect option"
	if !strings.Contains(err.Error(), expected) {
		t.Errorf("expected to contain '%v' in '%v'", expected, err.Error())
	}
}
