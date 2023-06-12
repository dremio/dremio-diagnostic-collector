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

	"github.com/spf13/pflag"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
	"github.com/dremio/dremio-diagnostic-collector/pkg/tests"
)

var (
	tmpDir      string
	cfgFilePath string
	overrides   map[string]*pflag.Flag
	err         error
	cfg         *conf.CollectConf
)

var beforeEachContTest = func() {
	tmpDir, err = os.MkdirTemp("", "testdataabdc")
	if err != nil {
		log.Fatalf("unable to create dir with error %v", err)
	}
	cfgFilePath = fmt.Sprintf("%s/%s", tmpDir, "ddc.yaml")

	// Create a sample configuration file.
	cfgContent := `
accept-collection-consent: true
collect-acceleration-log: true
collect-access-log: true
dremio-gclogs-dir: "/path/to/gclogs"
dremio-log-dir: "/path/to/dremio/logs"
node-name: "node1"
dremio-conf-dir: "/path/to/dremio/conf"
number-threads: 4
dremio-endpoint: "http://localhost:9047"
dremio-username: "admin"
dremio-pat-token: "your_personal_access_token"
dremio-rocksdb-dir: "/path/to/dremio/rocksdb"
collect-dremio-configuration: true
number-job-profiles: 10
capture-heap-dump: true
collect-metrics: true
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
collect-wlm: true
collect-system-tables-export: true
collect-kvstore-report: true
`

	// Write the sample configuration to a file.
	err := os.WriteFile(cfgFilePath, []byte(cfgContent), 0600)
	if err != nil {
		log.Fatalf("unable to create conf file with error %v", err)
	}
	overrides = map[string]*pflag.Flag{}
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
	beforeEachContTest()
	//should parse the configuration correctly
	cfg, err = conf.ReadConf(overrides, tmpDir)
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	if cfg == nil {
		t.Error("invalid conf")
	}

	if cfg.AcceptCollectionConsent() != true {
		t.Errorf("Expected AcceptCollectionConsent to be true, got false")
	}

	if cfg.CollectAccelerationLogs() != true {
		t.Errorf("Expected CollectAccelerationLogs to be true, got false")
	}

	if cfg.CollectAccessLogs() != true {
		t.Errorf("Expected CollectAccessLogs to be true, got false")
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

	if cfg.DremioConfDir() != "/path/to/dremio/conf" {
		t.Errorf("Expected DremioConfDir to be '/path/to/dremio/conf', got '%s'", cfg.DremioConfDir())
	}
	afterEachConfTest()
}

func TestConfReadingWhenLoggingParsingOfDdcYAML(t *testing.T) {
	beforeEachContTest()
	//should log redacted when token is present
	out, err := tests.CaptureOutput(func() {
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
