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

package conf_test

import (
	"fmt"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/pkg/output"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
)

var (
	tmpDir      string
	cfgFilePath string
	overrides   map[string]string
	err         error
	cfg         *conf.CollectConf
)

var genericConfSetup = func(cfgContent string) {
	tmpDir, err = os.MkdirTemp("", "testdataabdc")
	if err != nil {
		log.Fatalf("unable to create dir with error %v", err)
	}
	cfgFilePath = fmt.Sprintf("%s/%s", tmpDir, "ddc.yaml")

	if cfgContent == "" {
		// Create a sample configuration file.
		cfgContent = `
accept-collection-consent: true
disable-rest-api: false
collect-acceleration-log: true
collect-access-log: true
collect-audit-log: true
collect-jvm-flags: true
dremio-pid-detection: false
dremio-gclogs-dir: "/path/to/gclogs"
dremio-log-dir: "/path/to/dremio/logs"
node-name: "node1"
dremio-conf-dir: "/path/to/dremio/conf"
tarball-out-dir: "/my-tarball-dir"
number-threads: 4
dremio-endpoint: "http://localhost:9047"
dremio-username: "admin"
dremio-pat-token: "your_personal_access_token"
dremio-rocksdb-dir: "/path/to/dremio/rocksdb"
collect-dremio-configuration: true
number-job-profiles: 10
capture-heap-dump: true
collect-metrics: true
collect-os-config: true
collect-disk-usage: true
tmp-output-dir: "/path/to/tmp"
dremio-logs-num-days: 7
dremio-queries-json-num-days: 7
dremio-gc-file-pattern: "*.log"
collect-queries-json: true
collect-server-logs: true
collect-meta-refresh-log: true
collect-reflection-log: true
collect-gc-logs: true
collect-jfr: true
dremio-jfr-time-seconds: 60
collect-jstack: true
dremio-jstack-time-seconds: 60
dremio-jstack-freq-seconds: 10
dremio-ttop-time-seconds: 30 
dremio-ttop-freq-seconds: 5
collect-wlm: true
collect-ttop: true
collect-system-tables-export: true
collect-kvstore-report: true
`
	}
	// Write the sample configuration to a file.
	err := os.WriteFile(cfgFilePath, []byte(cfgContent), 0600)
	if err != nil {
		log.Fatalf("unable to create conf file with error %v", err)
	}
	overrides = make(map[string]string)
}

var afterEachConfTest = func() {
	// Remove the configuration file after each test.
	err := os.Remove(cfgFilePath)
	if err != nil {
		log.Fatalf("unable to remove conf file with error %v", err)
	}
	// Remove the temporary directory after each test.
	err = os.RemoveAll(tmpDir)
	if err != nil {
		log.Fatalf("unable to remove conf dir with error %v", err)
	}
}

func TestConfReadingWithAValidConfigurationFile(t *testing.T) {
	genericConfSetup("")
	//should parse the configuration correctly
	cfg, err = conf.ReadConf(overrides, tmpDir)
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	if cfg == nil {
		t.Error("invalid conf")
	}

	if cfg.DisableRESTAPI() != false {
		t.Errorf("Expected DisableRESTAPI to be true, got false")
	}

	if cfg.AcceptCollectionConsent() != true {
		t.Errorf("Expected AcceptCollectionConsent to be true, got false")
	}

	if cfg.CollectAccelerationLogs() != true {
		t.Errorf("Expected CollectAccelerationLogs to be true, got false")
	}

	if cfg.CollectOSConfig() != true {
		t.Errorf("Expected CollectJVMConf to be true, got false")
	}

	if cfg.CollectJVMFlags() != true {
		t.Errorf("Expected CollectJVMConf to be true, got false")
	}
	if cfg.CollectAccessLogs() != true {
		t.Errorf("Expected CollectAccessLogs to be true, got false")
	}

	if cfg.CollectAuditLogs() != true {
		t.Errorf("Expected CollectAuditLogs to be true, got false")
	}

	if cfg.CollectDiskUsage() != true {
		t.Errorf("Expected CollectDiskUsage to be true, got false")
	}

	if cfg.CollectDremioConfiguration() != true {
		t.Errorf("Expected CollectDremioConfiguration to be true, got false")
	}

	if cfg.CollectKVStoreReport() != true {
		t.Errorf("Expected CollectKVStoreReport to be true, got false")
	}

	if cfg.CollectMetaRefreshLogs() != true {
		t.Errorf("Expected CollectMetaRefreshLogs to be true, got false")
	}

	if cfg.CollectNodeMetrics() != true {
		t.Errorf("Expected CollectNodeMetrics to be true, got false")
	}

	if cfg.CollectQueriesJSON() != true {
		t.Errorf("Expected CollectQueriesJSON to be true, got false")
	}

	if cfg.CollectReflectionLogs() != true {
		t.Errorf("Expected CollectReflectionLogs to be true, got false")
	}

	if cfg.CollectServerLogs() != true {
		t.Errorf("Expected CollectServerLogs to be true, got false")
	}

	if cfg.CollectSystemTablesExport() != true {
		t.Errorf("Expected CollectSystemTablesExport to be true, got false")
	}

	if cfg.CollectWLM() != true {
		t.Errorf("Expected CollectWLM to be true, got false")
	}

	if cfg.CollectTtop() != true {
		t.Errorf("Expected CollectTtop to be true, got false")
	}

	if cfg.DremioTtopTimeSeconds() != 30 {
		t.Errorf("Expected to have 30 seconds for ttop time but was %v", cfg.DremioTtopTimeSeconds())
	}

	if cfg.DremioTtopFreqSeconds() != 5 {
		t.Errorf("Expected to have 5 seconds for ttop freq but was %v", cfg.DremioTtopFreqSeconds())
	}
	if cfg.DremioConfDir() != "/path/to/dremio/conf" {
		t.Errorf("Expected DremioConfDir to be '/path/to/dremio/conf', got '%s'", cfg.DremioConfDir())
	}
	if cfg.TarballOutDir() != "/my-tarball-dir" {
		t.Errorf("expected /my-tarball-dir but was %v", cfg.TarballOutDir())
	}
	if cfg.DremioPIDDetection() != false {
		t.Errorf("expected dremio-pid-detection to be disabled")
	}

	afterEachConfTest()
}

func TestConfReadWithDisabledRestAPIResultsInDisabledWLMJobProfileAndKVReport(t *testing.T) {
	yaml := `
disable-rest-api: true
number-threads: 4
dremio-endpoint: "http://localhost:9047"
dremio-username: "admin"
dremio-pat-token: "your_personal_access_token"
number-job-profiles: 10
collect-wlm: true
collect-system-tables-export: true
collect-kvstore-report: true
`
	genericConfSetup(yaml)
	defer afterEachConfTest()
	cfg, err = conf.ReadConf(overrides, tmpDir)
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	if cfg == nil {
		t.Fatal("invalid conf")
	}
	if cfg.CollectSystemTablesExport() == true {
		t.Error("expected collect system tables export to be false")
	}
	if cfg.CollectWLM() == true {
		t.Error("expected collect wlm to be false")
	}
	if cfg.CollectKVStoreReport() == true {
		t.Error("expected collect wlm to be false")
	}
	if cfg.NumberJobProfilesToCollect() != 0 {
		t.Errorf("expected number job profiles was %v but expected 0", cfg.NumberJobProfilesToCollect())
	}
	if cfg.JobProfilesNumHighQueryCost() != 0 {
		t.Errorf("expected number high query cost job profiles was %v but expected 0", cfg.JobProfilesNumHighQueryCost())
	}
	if cfg.JobProfilesNumRecentErrors() != 0 {
		t.Errorf("expected number high query cost job profiles was %v but expected 0", cfg.JobProfilesNumRecentErrors())
	}
	if cfg.JobProfilesNumSlowExec() != 0 {
		t.Errorf("expected number high query cost job profiles was %v but expected 0", cfg.JobProfilesNumSlowExec())
	}
	if cfg.JobProfilesNumSlowPlanning() != 0 {
		t.Errorf("expected number high query cost job profiles was %v but expected 0", cfg.JobProfilesNumSlowPlanning())
	}
}

func TestConfReadingWhenLoggingParsingOfDdcYAML(t *testing.T) {
	genericConfSetup("")
	//should log redacted when token is present
	out, err := output.CaptureOutput(func() {
		simplelog.InitLogger(4)
		cfg, err = conf.ReadConf(overrides, tmpDir)
		if err != nil {
			t.Errorf("expected no error but had %v", err)
		}
		if cfg == nil {
			t.Error("expected a valid CollectConf but it is nil")
		}
	})
	if err != nil {
		simplelog.Errorf("unable to capture output %v", err)
	}
	if !strings.Contains(out, "conf key 'dremio-pat-token':'REDACTED'") {
		t.Errorf("expected dremio-pat-token to be redacted in '%v' but it was not", out)
	}
	afterEachConfTest()
}
