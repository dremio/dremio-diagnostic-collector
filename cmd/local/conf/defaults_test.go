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

	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/collects"
)

func setupTestSetViperDefaults(collectionType string) (map[string]interface{}, string, int) {
	hostName := "test-host"
	defaultCaptureSeconds := 30
	confData := make(map[string]interface{})
	// Run the function.
	conf.SetViperDefaults(confData, hostName, defaultCaptureSeconds, collectionType)

	return confData, hostName, defaultCaptureSeconds
}

func TestSetViperDefaultsWithHealthCheck(t *testing.T) {
	confData, hostName, defaultCaptureSeconds := setupTestSetViperDefaults(collects.HealthCheckCollection)

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
		{conf.KeyDremioPidDetection, true},
		{conf.KeyDremioUsername, "dremio"},
		{conf.KeyDremioPatToken, ""},
		{conf.KeyDremioConfDir, "/opt/dremio/conf"},
		{conf.KeyDremioRocksdbDir, "/opt/dremio/data/db"},
		{conf.KeyCollectDremioConfiguration, true},
		{conf.KeyCaptureHeapDump, false},
		{conf.KeyNumberJobProfiles, 25000},
		{conf.KeyDremioEndpoint, "http://localhost:9047"},
		{conf.KeyTarballOutDir, "/tmp/ddc"},
		{conf.KeyCollectOSConfig, true},
		{conf.KeyCollectDiskUsage, true},
		{conf.KeyDremioLogsNumDays, 7},
		{conf.KeyDremioQueriesJSONNumDays, 30},
		{conf.KeyDremioGCFilePattern, "server*.gc*"},
		{conf.KeyCollectQueriesJSON, true},
		{conf.KeyCollectServerLogs, true},
		{conf.KeyCollectMetaRefreshLog, true},
		{conf.KeyCollectReflectionLog, true},
		{conf.KeyCollectVacuumLog, true},
		{conf.KeyCollectGCLogs, true},
		{conf.KeyCollectJFR, true},
		{conf.KeyCollectJStack, false},
		{conf.KeyCollectSystemTablesExport, true},
		{conf.KeyCollectWLM, true},
		{conf.KeyCollectTtop, true},
		{conf.KeyCollectKVStoreReport, true},
		{conf.KeyDremioJStackTimeSeconds, defaultCaptureSeconds},
		{conf.KeyDremioJFRTimeSeconds, defaultCaptureSeconds},
		{conf.KeyDremioJStackFreqSeconds, 1},
		{conf.KeyDremioTtopFreqSeconds, 1},
		{conf.KeyDremioTtopTimeSeconds, defaultCaptureSeconds},
		{conf.KeyDremioGCLogsDir, ""},
		{conf.KeyNodeName, hostName},
		{conf.KeyAcceptCollectionConsent, true},
		{conf.KeyAllowInsecureSSL, true},
		{conf.KeyCollectSystemTablesTimeoutSeconds, 60},
		{conf.KeyCollectClusterIDTimeoutSeconds, 60},
	}

	for _, check := range checks {
		actual := confData[check.key]
		if actual != check.expected {
			t.Errorf("Unexpected value for '%s'. Got %v, expected %v", check.key, actual, check.expected)
		}
	}
}

func TestSetViperDefaultsQuickCollect(t *testing.T) {
	confData, hostName, defaultCaptureSeconds := setupTestSetViperDefaults(collects.QuickCollection)
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
		{conf.KeyNumberThreads, 1},
		{conf.KeyDremioPid, 0},
		{conf.KeyDremioPidDetection, true},
		{conf.KeyDremioUsername, "dremio"},
		{conf.KeyDremioPatToken, ""},
		{conf.KeyDremioConfDir, "/opt/dremio/conf"},
		{conf.KeyDremioRocksdbDir, "/opt/dremio/data/db"},
		{conf.KeyCollectDremioConfiguration, true},
		{conf.KeyCaptureHeapDump, false},
		{conf.KeyNumberJobProfiles, 20},
		{conf.KeyDremioEndpoint, "http://localhost:9047"},
		{conf.KeyTarballOutDir, "/tmp/ddc"},
		{conf.KeyCollectOSConfig, true},
		{conf.KeyCollectDiskUsage, true},
		{conf.KeyDremioLogsNumDays, 2},
		{conf.KeyDremioQueriesJSONNumDays, 2},
		{conf.KeyDremioGCFilePattern, "server*.gc*"},
		{conf.KeyCollectQueriesJSON, true},
		{conf.KeyCollectServerLogs, true},
		{conf.KeyCollectMetaRefreshLog, true},
		{conf.KeyCollectReflectionLog, true},
		{conf.KeyCollectVacuumLog, true},
		{conf.KeyCollectGCLogs, true},
		{conf.KeyCollectJFR, false},
		{conf.KeyCollectJStack, false},
		{conf.KeyCollectSystemTablesExport, true},
		{conf.KeyCollectWLM, true},
		{conf.KeyCollectTtop, false},
		{conf.KeyCollectKVStoreReport, true},
		{conf.KeyDremioJStackTimeSeconds, defaultCaptureSeconds},
		{conf.KeyDremioJFRTimeSeconds, defaultCaptureSeconds},
		{conf.KeyDremioJStackFreqSeconds, 1},
		{conf.KeyDremioTtopFreqSeconds, 1},
		{conf.KeyDremioTtopTimeSeconds, defaultCaptureSeconds},
		{conf.KeyDremioGCLogsDir, ""},
		{conf.KeyNodeName, hostName},
		{conf.KeyAcceptCollectionConsent, true},
		{conf.KeyAllowInsecureSSL, true},
	}

	for _, check := range checks {
		actual := confData[check.key]
		if actual != check.expected {
			t.Errorf("Unexpected value for '%s'. Got %v, expected %v", check.key, actual, check.expected)
		}
	}
}

func TestSetViperDefaults(t *testing.T) {
	confData, hostName, defaultCaptureSeconds := setupTestSetViperDefaults(collects.StandardCollection)
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
		{conf.KeyDremioPidDetection, true},
		{conf.KeyDremioUsername, "dremio"},
		{conf.KeyDremioPatToken, ""},
		{conf.KeyDremioConfDir, "/opt/dremio/conf"},
		{conf.KeyDremioRocksdbDir, "/opt/dremio/data/db"},
		{conf.KeyCollectDremioConfiguration, true},
		{conf.KeyCaptureHeapDump, false},
		{conf.KeyNumberJobProfiles, 20},
		{conf.KeyDremioEndpoint, "http://localhost:9047"},
		{conf.KeyTarballOutDir, "/tmp/ddc"},
		{conf.KeyCollectOSConfig, true},
		{conf.KeyCollectDiskUsage, true},
		{conf.KeyDremioLogsNumDays, 7},
		{conf.KeyDremioQueriesJSONNumDays, 30},
		{conf.KeyDremioGCFilePattern, "server*.gc*"},
		{conf.KeyCollectQueriesJSON, true},
		{conf.KeyCollectServerLogs, true},
		{conf.KeyCollectMetaRefreshLog, true},
		{conf.KeyCollectReflectionLog, true},
		{conf.KeyCollectVacuumLog, true},
		{conf.KeyCollectGCLogs, true},
		{conf.KeyCollectJFR, true},
		{conf.KeyCollectJStack, false},
		{conf.KeyCollectSystemTablesExport, true},
		{conf.KeyCollectWLM, true},
		{conf.KeyCollectTtop, true},
		{conf.KeyCollectKVStoreReport, true},
		{conf.KeyDremioJStackTimeSeconds, defaultCaptureSeconds},
		{conf.KeyDremioJFRTimeSeconds, defaultCaptureSeconds},
		{conf.KeyDremioJStackFreqSeconds, 1},
		{conf.KeyDremioTtopFreqSeconds, 1},
		{conf.KeyDremioTtopTimeSeconds, defaultCaptureSeconds},
		{conf.KeyDremioGCLogsDir, ""},
		{conf.KeyNodeName, hostName},
		{conf.KeyAcceptCollectionConsent, true},
		{conf.KeyAllowInsecureSSL, true},
	}

	for _, check := range checks {
		actual := confData[check.key]
		if actual != check.expected {
			t.Errorf("Unexpected value for '%s'. Got %v, expected %v", check.key, actual, check.expected)
		}
	}
}
