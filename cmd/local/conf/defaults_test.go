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
	"fmt"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/collects"
)

func setupTestSetViperDefaults(collectionType collects.CollectionMode) (map[string]interface{}, string, int) {
	hostName := "test-host"
	defaultCaptureSeconds := 30
	confData := make(map[string]interface{})
	// Run the function.
	conf.SetViperDefaults(confData, hostName, defaultCaptureSeconds, collectionType)

	return confData, hostName, defaultCaptureSeconds
}

func TestSetViperDefaultsWithDiagnosis(t *testing.T) {
	confData, hostName, _ := setupTestSetViperDefaults(collects.DiagnosisCollection)

	checks := []struct {
		key      string
		expected interface{}
	}{
		// Log collection — selective in diagnosis mode
		{conf.KeyCollectAccelerationLog, false},
		{conf.KeyCollectAccessLog, false},
		{conf.KeyCollectServerLogs, true},
		{conf.KeyCollectGCLogs, true},
		{conf.KeyCollectMetaRefreshLog, true},
		{conf.KeyCollectReflectionLog, true},
		{conf.KeyCollectVacuumLog, false},
		{conf.KeyCollectHSErrFiles, true},
		{conf.KeyCollectQueriesJSON, true},
		{conf.KeyCollectTrackerJSON, false},
		{conf.KeyCollectHiveDeprecatedLog, false},

		// JVM diagnostic tools — all opt-in via TUI checkboxes or CLI flags
		{conf.KeyCollectJFR, false},
		{conf.KeyCollectJStack, false},
		{conf.KeyCollectTop, false},
		{conf.KeyCollectAsyncProfiler, false},
		{conf.KeyCaptureHeapDump, false},

		// JVM tool timing
		{conf.KeyDiagTimeSeconds, 60},

		// Date range defaults
		{conf.KeyDremioLogsNumDays, 3},
		{conf.KeyQueriesJSONNumDays, 3},
		{conf.KeyCollectQueriesPerfJSON, true},

		// API collection
		{conf.KeyCollectWLM, true},
		{conf.KeyCollectKVStoreReport, false},
		{conf.KeyCollectProblematicProfiles, false},
		{conf.KeyCollectSystemTablesExport, true},
		{conf.KeySysTables, conf.SystemTableList()},
		{conf.KeyNumberJobProfiles, 0}, // auto-identified from server.log

		// Common fields
		{conf.KeyCollectJVMFlags, true},
		{conf.KeyDremioLogDir, "/var/log/dremio"},
		{conf.KeyDremioConfDir, "/opt/dremio/conf"},
		{conf.KeyDremioRocksdbDir, "/opt/dremio/data/db"},
		{conf.KeyDremioEndpoint, "http://localhost:9047"},
		{conf.KeyCollectDremioConfiguration, true},
		{conf.KeyCollectOSConfig, true},
		{conf.KeyCollectDiskUsage, true},
		{conf.KeyAllowInsecureSSL, true},
		{conf.KeyNodeName, hostName},
		{conf.KeyAcceptCollectionConsent, true},
		{conf.KeyCollectSystemTablesTimeoutSeconds, 120},
	}

	for _, check := range checks {
		actual := fmt.Sprint(confData[check.key])
		if actual != fmt.Sprint(check.expected) {
			t.Errorf("Unexpected value for '%s'. \nGot:\n %v\nExpected:\n %v", check.key, actual, check.expected)
		}
	}
}

func TestSetViperDefaultsWithStandard(t *testing.T) {
	confData, hostName, _ := setupTestSetViperDefaults(collects.StandardCollection)

	checks := []struct {
		key      string
		expected interface{}
	}{
		// Log collection — limited set with per-log day counts
		{conf.KeyCollectServerLogs, true},
		{conf.KeyServerLogsNumDays, 1},
		{conf.KeyCollectTrackerJSON, true},
		{conf.KeyTrackerJSONNumDays, 1},
		{conf.KeyCollectVacuumLog, true},
		{conf.KeyVacuumLogNumDays, 1},
		{conf.KeyCollectQueriesJSON, true},
		{conf.KeyQueriesJSONNumDays, 30},
		{conf.KeyCollectQueriesPerfJSON, true},
		{conf.KeyQueriesPerfNumDays, 30},

		// Explicitly disabled
		{conf.KeyCollectGCLogs, false},
		{conf.KeyCollectMetaRefreshLog, false},
		{conf.KeyCollectReflectionLog, false},
		{conf.KeyCollectAccelerationLog, false},
		{conf.KeyCollectAccessLog, false},
		{conf.KeyCollectHSErrFiles, false},
		{conf.KeyCollectHiveDeprecatedLog, false},

		// JVM diagnostic tools — all disabled
		{conf.KeyCollectJFR, false},
		{conf.KeyCollectJStack, false},
		{conf.KeyCollectTop, false},
		{conf.KeyCollectAsyncProfiler, false},
		{conf.KeyCaptureHeapDump, false},
		{conf.KeyDiagTimeSeconds, 30},

		// No job profiles in standard mode
		{conf.KeyNumberJobProfiles, 0},

		// WLM enabled (via RocksDB viewer), KV store disabled
		{conf.KeyCollectWLM, true},
		{conf.KeyCollectKVStoreReport, false},

		// Transfer rate limiting
		{conf.KeyDiskBandwidthLimitPct, 20},

		// Common fields
		{conf.KeyCollectJVMFlags, true},
		{conf.KeyDremioLogDir, "/var/log/dremio"},
		{conf.KeyDremioConfDir, "/opt/dremio/conf"},
		{conf.KeyDremioRocksdbDir, "/opt/dremio/data/db"},
		{conf.KeyDremioEndpoint, "http://localhost:9047"},
		{conf.KeyCollectDremioConfiguration, true},
		{conf.KeyCollectOSConfig, true},
		{conf.KeyCollectDiskUsage, true},
		{conf.KeyAllowInsecureSSL, true},
		{conf.KeyNodeName, hostName},
		{conf.KeyAcceptCollectionConsent, true},
		{conf.KeyCollectSystemTablesTimeoutSeconds, 120},
	}

	for _, check := range checks {
		actual := fmt.Sprint(confData[check.key])
		if actual != fmt.Sprint(check.expected) {
			t.Errorf("Unexpected value for '%s'. Got %v, expected %v", check.key, actual, check.expected)
		}
	}
}

func TestDefaultMapHelpers(t *testing.T) {
	t.Run("DiagnosisDefaultMap returns populated map", func(t *testing.T) {
		m := conf.DiagnosisDefaultMap()
		if len(m) == 0 {
			t.Fatal("DiagnosisDefaultMap returned empty map")
		}
		// Verify key types via helpers
		if conf.GetStringDefault(m, conf.KeyDremioLogDir) == "" {
			t.Error("Expected non-empty log dir default")
		}
		if conf.GetIntDefault(m, conf.KeyDiagTimeSeconds) != 60 {
			t.Errorf("Expected diag-time-seconds=60, got %d", conf.GetIntDefault(m, conf.KeyDiagTimeSeconds))
		}
	})

	t.Run("StandardDefaultMap returns populated map", func(t *testing.T) {
		m := conf.StandardDefaultMap()
		if len(m) == 0 {
			t.Fatal("StandardDefaultMap returned empty map")
		}
		if conf.GetIntDefault(m, conf.KeyServerLogsNumDays) != 1 {
			t.Errorf("Expected server-logs-num-days=1, got %d", conf.GetIntDefault(m, conf.KeyServerLogsNumDays))
		}
	})

	t.Run("GetBoolDefault missing key returns false", func(t *testing.T) {
		m := map[string]interface{}{}
		if conf.GetBoolDefault(m, "nonexistent") {
			t.Error("Expected false for missing key")
		}
	})

	t.Run("GetIntDefault missing key returns 0", func(t *testing.T) {
		m := map[string]interface{}{}
		if conf.GetIntDefault(m, "nonexistent") != 0 {
			t.Error("Expected 0 for missing key")
		}
	})

	t.Run("GetStringDefault missing key returns empty", func(t *testing.T) {
		m := map[string]interface{}{}
		if conf.GetStringDefault(m, "nonexistent") != "" {
			t.Error("Expected empty string for missing key")
		}
	})
}

func TestSetViperDefaults_UnknownMode_DefaultsStandard(t *testing.T) {
	confData, _, _ := setupTestSetViperDefaults("unknown-mode")

	// Unknown mode should default to standard profile
	if actual := fmt.Sprint(confData[conf.KeyCollectJFR]); actual != "false" {
		t.Errorf("Expected JFR disabled for unknown mode (standard fallback), got %v", actual)
	}
	if actual := fmt.Sprint(confData[conf.KeyCollectJStack]); actual != "false" {
		t.Errorf("Expected JStack disabled for unknown mode (standard fallback), got %v", actual)
	}
	if actual := fmt.Sprint(confData[conf.KeyQueriesJSONNumDays]); actual != "30" {
		t.Errorf("Expected 30 days queries.json for unknown mode (standard fallback), got %v", actual)
	}
}
