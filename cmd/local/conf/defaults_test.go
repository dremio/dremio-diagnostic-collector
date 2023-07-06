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

// package conf_test tests the conf package

package conf_test

import (
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf"
	"github.com/spf13/viper"
)

func setupTestSetViperDefaults() (string, int, string) {
	hostName := "test-host"
	defaultCaptureSeconds := 30
	outputDir := "/tmp"

	viper.Reset()
	// Run the function.
	conf.SetViperDefaults(hostName, defaultCaptureSeconds, outputDir)

	return hostName, defaultCaptureSeconds, outputDir
}

func TestSetViperDefaults(t *testing.T) {
	hostName, defaultCaptureSeconds, outputDir := setupTestSetViperDefaults()

	checks := []struct {
		key      string
		expected interface{}
	}{
		{conf.KeyDisableRESTAPI, false},
		{conf.KeyCollectAccelerationLog, false},
		{conf.KeyCollectAccessLog, false},
		{conf.KeyCollectAuditLog, false},
		{conf.KeyCollectJVMFlags, true},
		{conf.KeyDremioLogDir, "/var/log/dremio"},
		{conf.KeyNumberThreads, 2},
		{conf.KeyDremioPid, 0},
		{conf.KeyDremioUsername, "dremio"},
		{conf.KeyDremioPatToken, ""},
		{conf.KeyDremioConfDir, "/opt/dremio/conf"},
		{conf.KeyDremioRocksdbDir, "/opt/dremio/data/db"},
		{conf.KeyCollectDremioConfiguration, true},
		{conf.KeyCaptureHeapDump, false},
		{conf.KeyNumberJobProfiles, 25000},
		{conf.KeyDremioEndpoint, "http://localhost:9047"},
		{conf.KeyTmpOutputDir, outputDir},
		{conf.KeyCollectMetrics, true},
		{conf.KeyCollectOSConfig, true},
		{conf.KeyCollectDiskUsage, true},
		{conf.KeyDremioLogsNumDays, 7},
		{conf.KeyDremioQueriesJSONNumDays, 28},
		{conf.KeyDremioGCFilePattern, "gc*.log*"},
		{conf.KeyCollectQueriesJSON, true},
		{conf.KeyCollectServerLogs, true},
		{conf.KeyCollectMetaRefreshLog, true},
		{conf.KeyCollectReflectionLog, true},
		{conf.KeyCollectGCLogs, true},
		{conf.KeyCollectJFR, true},
		{conf.KeyCollectJStack, true},
		{conf.KeyCollectSystemTablesExport, true},
		{conf.KeyCollectWLM, true},
		{conf.KeyCollectTtop, true},
		{conf.KeyCollectKVStoreReport, true},
		{conf.KeyDremioJStackTimeSeconds, defaultCaptureSeconds},
		{conf.KeyDremioJFRTimeSeconds, defaultCaptureSeconds},
		{conf.KeyNodeMetricsCollectDurationSeconds, defaultCaptureSeconds},
		{conf.KeyDremioJStackFreqSeconds, 1},
		{conf.KeyDremioTtopFreqSeconds, 1},
		{conf.KeyDremioTtopTimeSeconds, defaultCaptureSeconds},
		{conf.KeyDremioGCLogsDir, ""},
		{conf.KeyNodeName, hostName},
		{conf.KeyAcceptCollectionConsent, true},
		{conf.KeyAllowInsecureSSL, true},
	}

	for _, check := range checks {
		if viper.Get(check.key) != check.expected {
			t.Errorf("Unexpected value for '%s'. Got %v, expected %v", check.key, viper.Get(check.key), check.expected)
		}
	}
}
