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

package conf

import (
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/collects"
)

func setDefault(confData map[string]interface{}, key string, value interface{}) {
	// if key is not present go ahead and set it
	if _, ok := confData[key]; !ok {
		confData[key] = value
	}
}

// Defaults sets base configuration values shared by all collection modes.
// These are only environment/path/infra defaults — mode-specific collection
// settings (which collectors are enabled, timing, etc.) are set by each
// profile function and may override these values.
func Defaults(confData map[string]interface{}, hostName string) {
	setDefault(confData, KeyVerbose, "vv")
	setDefault(confData, KeyCollectJVMFlags, true)
	setDefault(confData, KeyDremioLogDir, "/var/log/dremio")
	setDefault(confData, KeyDremioPid, 0)
	setDefault(confData, KeyDremioPidDetection, true)
	setDefault(confData, KeyDremioUsername, "dremio")
	setDefault(confData, KeyDremioPatToken, "")
	setDefault(confData, KeyDremioConfDir, "/opt/dremio/conf")
	setDefault(confData, KeyDremioRocksdbDir, "/opt/dremio/data/db")
	setDefault(confData, KeyCollectDremioConfiguration, true)
	setDefault(confData, KeyDremioEndpoint, "http://localhost:9047")
	setDefault(confData, KeyTarballOutDir, ".")
	setDefault(confData, KeyCollectOSConfig, true)
	setDefault(confData, KeyCollectDiskUsage, true)
	setDefault(confData, KeyDremioGCFilePattern, "server*.gc*")
	setDefault(confData, KeyCollectSystemTablesExport, true)
	setDefault(confData, KeySystemTablesRowLimit, 100000)
	setDefault(confData, KeyNodeName, hostName)
	setDefault(confData, KeyAcceptCollectionConsent, true)
	setDefault(confData, KeyAllowInsecureSSL, true)
	setDefault(confData, KeyRestHTTPTimeout, 30)
	setDefault(confData, KeyDisableFreeSpaceCheck, false)
	setDefault(confData, KeyCollectClusterIDTimeoutSeconds, 60)
	setDefault(confData, KeyNumberThreads, 1)
}

// DiagnosisCollectionProfile sets defaults for diagnosis mode.
// Diagnosis mode collects comprehensive diagnostic data including JVM tools,
// all log types, and API-based artifacts when a PAT token is provided.
func DiagnosisCollectionProfile(confData map[string]interface{}, hostName string, _ int) {
	Defaults(confData, hostName)
	setDefault(confData, KeySysTables, SystemTableList())
	setDefault(confData, KeyCollectKVStoreReport, false)
	setDefault(confData, KeyCollectWLM, true)
	setDefault(confData, KeyCollectProblematicProfiles, false)

	// JVM diagnostic tools — all opt-in; user enables via TUI checkboxes or CLI flags
	setDefault(confData, KeyCollectJStack, false)
	setDefault(confData, KeyCollectJFR, false)
	setDefault(confData, KeyCollectTop, false)
	setDefault(confData, KeyCollectAsyncProfiler, false)
	setDefault(confData, KeyCaptureHeapDump, false) // opt-in via --collect-heap-dump

	// JVM tool timing — fixed at 60s regardless of defaultCaptureSeconds
	setDefault(confData, KeyDiagTimeSeconds, 60)

	// Log collection — date range applies uniformly via --start-date or --days=3
	setDefault(confData, KeyDremioLogsNumDays, 3)
	setDefault(confData, KeyQueriesJSONNumDays, 3)
	setDefault(confData, KeyCollectQueriesPerfJSON, true)
	setDefault(confData, KeyCollectServerLogs, true)
	setDefault(confData, KeyCollectGCLogs, true)
	setDefault(confData, KeyCollectMetaRefreshLog, true)
	setDefault(confData, KeyCollectReflectionLog, true)
	setDefault(confData, KeyCollectVacuumLog, true)
	setDefault(confData, KeyCollectAccelerationLog, true)
	setDefault(confData, KeyCollectAccessLog, true)
	setDefault(confData, KeyCollectHSErrFiles, true)
	setDefault(confData, KeyCollectQueriesJSON, true)

	// New log types for v4
	setDefault(confData, KeyCollectTrackerJSON, true)
	setDefault(confData, KeyCollectHiveDeprecatedLog, true)

	// Job profiles — auto-identified from server.log, not sampling-based
	setDefault(confData, KeyNumberJobProfiles, 0)

	setDefault(confData, KeyCollectSystemTablesTimeoutSeconds, 120)
}

// StandardCollectionProfile sets defaults for standard mode.
// Standard mode collects server logs, metadata refresh logs, reflection logs,
// queries.json, configuration, and OS info. No JVM diagnostics. Each log type
// has its own configurable number of days.
func StandardCollectionProfile(confData map[string]interface{}, hostName string, defaultCaptureSeconds int) {
	Defaults(confData, hostName)
	setDefault(confData, KeySysTables, SystemTableList())
	setDefault(confData, KeyCollectKVStoreReport, false)

	// JVM diagnostic tools — all disabled in standard mode
	setDefault(confData, KeyCollectJStack, false)
	setDefault(confData, KeyCollectJFR, false)
	setDefault(confData, KeyCollectTop, false)
	setDefault(confData, KeyCollectAsyncProfiler, false)
	setDefault(confData, KeyCaptureHeapDump, false)

	// JVM tool timing defaults (tools are disabled but values needed if toggled via flags)
	setDefault(confData, KeyDiagTimeSeconds, defaultCaptureSeconds)

	// Log collection — each type has its own day count in standard mode
	setDefault(confData, KeyCollectServerLogs, true)
	setDefault(confData, KeyServerLogsNumDays, 1)
	setDefault(confData, KeyCollectTrackerJSON, true)
	setDefault(confData, KeyTrackerJSONNumDays, 1)
	setDefault(confData, KeyCollectVacuumLog, true)
	setDefault(confData, KeyVacuumLogNumDays, 1)
	setDefault(confData, KeyCollectQueriesJSON, true)
	setDefault(confData, KeyQueriesJSONNumDays, 30)
	setDefault(confData, KeyCollectQueriesPerfJSON, true)
	setDefault(confData, KeyQueriesPerfNumDays, 30)

	// Explicitly disabled log types
	setDefault(confData, KeyCollectGCLogs, false)
	setDefault(confData, KeyCollectMetaRefreshLog, false)
	setDefault(confData, KeyCollectReflectionLog, false)
	setDefault(confData, KeyCollectAccelerationLog, false)
	setDefault(confData, KeyCollectAccessLog, false)
	setDefault(confData, KeyCollectHSErrFiles, false)
	setDefault(confData, KeyCollectHiveDeprecatedLog, false)

	// No job profile collection in standard mode
	setDefault(confData, KeyNumberJobProfiles, 0)

	// WLM enabled in standard mode (collected via dremio-rocksdb-viewer)
	setDefault(confData, KeyCollectWLM, true)

	// Transfer rate limiting
	setDefault(confData, KeyDiskBandwidthLimitPct, 20)

	setDefault(confData, KeyCollectSystemTablesTimeoutSeconds, 120)
}

// SetViperDefaults wires up default values based on the collection mode.
func SetViperDefaults(confData map[string]interface{}, hostName string, defaultCaptureSeconds int, collectionMode collects.CollectionMode) {
	switch collectionMode {
	case collects.DiagnosisCollection:
		DiagnosisCollectionProfile(confData, hostName, defaultCaptureSeconds)
	case collects.StandardCollection:
		StandardCollectionProfile(confData, hostName, defaultCaptureSeconds)
	default:
		StandardCollectionProfile(confData, hostName, defaultCaptureSeconds)
	}
}

// DiagnosisDefaultMap returns diagnosis mode defaults as a map.
// Used by CLI flag registration and TUI initialization to derive defaults
// from a single source of truth.
func DiagnosisDefaultMap() map[string]interface{} {
	m := make(map[string]interface{})
	DiagnosisCollectionProfile(m, "", 0)
	return m
}

// StandardDefaultMap returns standard mode defaults as a map.
func StandardDefaultMap() map[string]interface{} {
	m := make(map[string]interface{})
	StandardCollectionProfile(m, "", 0)
	return m
}

// GetBoolDefault reads a bool from a defaults map, returning false if missing or wrong type.
func GetBoolDefault(m map[string]interface{}, key string) bool {
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

// GetIntDefault reads an int from a defaults map, returning 0 if missing or wrong type.
func GetIntDefault(m map[string]interface{}, key string) int {
	if v, ok := m[key]; ok {
		if i, ok := v.(int); ok {
			return i
		}
	}
	return 0
}

// GetStringDefault reads a string from a defaults map, returning "" if missing or wrong type.
func GetStringDefault(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
