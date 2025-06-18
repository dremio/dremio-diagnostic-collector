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
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/collects"
)

func setDefault(confData map[string]interface{}, key string, value interface{}) {
	// if key is not present go ahead and set it
	if _, ok := confData[key]; !ok {
		confData[key] = value
	}
}

func Defaults(confData map[string]interface{}, hostName string, defaultCaptureSeconds int) {
	// set default config
	setDefault(confData, KeyVerbose, "vv")
	setDefault(confData, KeyCollectAccelerationLog, false)
	setDefault(confData, KeyCollectAccessLog, false)
	setDefault(confData, KeyCollectAuditLog, false)
	setDefault(confData, KeyCollectJVMFlags, true)
	setDefault(confData, KeyDremioLogDir, "/var/log/dremio")
	setDefault(confData, KeyDremioPid, 0)
	setDefault(confData, KeyDremioPidDetection, true)
	setDefault(confData, KeyDremioUsername, "dremio")
	setDefault(confData, KeyDremioPatToken, "")
	setDefault(confData, KeyDremioConfDir, "/opt/dremio/conf")
	setDefault(confData, KeyDremioRocksdbDir, "/opt/dremio/data/db")
	setDefault(confData, KeyCollectDremioConfiguration, true)
	setDefault(confData, KeyCaptureHeapDump, false)
	setDefault(confData, KeyDremioEndpoint, "http://localhost:9047")
	setDefault(confData, KeyTarballOutDir, "/tmp/ddc")
	setDefault(confData, KeyCollectOSConfig, true)
	setDefault(confData, KeyCollectDiskUsage, true)
	setDefault(confData, KeyDremioGCFilePattern, "server*.gc*")
	setDefault(confData, KeyCollectQueriesJSON, true)
	setDefault(confData, KeyCollectServerLogs, true)
	setDefault(confData, KeyCollectMetaRefreshLog, true)
	setDefault(confData, KeyCollectReflectionLog, true)
	setDefault(confData, KeyCollectVacuumLog, true)
	setDefault(confData, KeyCollectGCLogs, true)
	setDefault(confData, KeyCollectHSErrFiles, true)
	setDefault(confData, KeyCollectSystemTablesExport, true)
	setDefault(confData, KeySystemTablesRowLimit, 100000)
	setDefault(confData, KeyCollectWLM, true)
	setDefault(confData, KeyDremioJStackTimeSeconds, defaultCaptureSeconds)
	setDefault(confData, KeyDremioJFRTimeSeconds, defaultCaptureSeconds)
	setDefault(confData, KeyDremioJStackFreqSeconds, 1)
	setDefault(confData, KeyDremioTtopFreqSeconds, 1)
	setDefault(confData, KeyDremioTtopTimeSeconds, defaultCaptureSeconds)
	setDefault(confData, KeyDremioGCLogsDir, "")
	setDefault(confData, KeyNodeName, hostName)
	setDefault(confData, KeyAcceptCollectionConsent, true)
	setDefault(confData, KeyIsRESTCollect, false)
	setDefault(confData, KeyRESTCollectDailyJobsLimit, 100000)
	setDefault(confData, KeyIsDremioCloud, false)
	setDefault(confData, KeyDremioCloudProjectID, "")
	setDefault(confData, KeyAllowInsecureSSL, true)
	setDefault(confData, KeyRestHTTPTimeout, 30)
	setDefault(confData, KeyDisableFreeSpaceCheck, false)
	setDefault(confData, KeyNoLogDir, false)
	setDefault(confData, KeyMinFreeSpaceGB, 40)
	setDefault(confData, KeyCollectClusterIDTimeoutSeconds, 60)
	setDefault(confData, KeySysTablesCloud, SystemTableListCloud())
	setDefault(confData, KeyNumberThreads, 1)
	setDefault(confData, KeyArchiveSizeLimitMB, 256)
	setDefault(confData, KeyDisableArchiveSplitting, false)
}

func QuickCollectionProfile(confData map[string]interface{}, hostName string, defaultCaptureSeconds int) {
	Defaults(confData, hostName, defaultCaptureSeconds)
	setDefault(confData, KeySysTables, SystemTableList())
	setDefault(confData, KeyCollectKVStoreReport, true)
	setDefault(confData, KeyCollectJStack, false)
	setDefault(confData, KeyCollectJFR, false)
	setDefault(confData, KeyCollectTtop, false)
	setDefault(confData, KeyDremioLogsNumDays, 2)
	setDefault(confData, KeyDremioQueriesJSONNumDays, 2)
	setDefault(confData, KeyCollectSystemTablesTimeoutSeconds, 120)
	setDefault(confData, KeyNumberJobProfiles, 20)

}

func StandardCollectionProfile(confData map[string]interface{}, hostName string, defaultCaptureSeconds int) {
	Defaults(confData, hostName, defaultCaptureSeconds)
	setDefault(confData, KeySysTables, SystemTableList())
	setDefault(confData, KeyCollectKVStoreReport, true)
	setDefault(confData, KeyCollectJStack, false)
	setDefault(confData, KeyCollectJFR, true)
	setDefault(confData, KeyCollectTtop, true)
	setDefault(confData, KeyDremioLogsNumDays, 7)
	setDefault(confData, KeyDremioQueriesJSONNumDays, 30)
	setDefault(confData, KeyCollectSystemTablesTimeoutSeconds, 120)
	setDefault(confData, KeyNumberJobProfiles, 20)
}

func WithJStackCollectionProfile(confData map[string]interface{}, hostName string, defaultCaptureSeconds int) {
	Defaults(confData, hostName, defaultCaptureSeconds)
	setDefault(confData, KeySysTables, SystemTableList())
	setDefault(confData, KeyCollectKVStoreReport, true)
	setDefault(confData, KeyCollectJStack, true)
	setDefault(confData, KeyCollectJFR, true)
	setDefault(confData, KeyCollectTtop, true)
	setDefault(confData, KeyDremioLogsNumDays, 7)
	setDefault(confData, KeyDremioQueriesJSONNumDays, 30)
	setDefault(confData, KeyCollectSystemTablesTimeoutSeconds, 120)
	setDefault(confData, KeyNumberJobProfiles, 20)
}

func HealthCheckCollectionProfile(confData map[string]interface{}, hostName string, defaultCaptureSeconds int) {
	Defaults(confData, hostName, defaultCaptureSeconds)
	setDefault(confData, KeySysTables, SystemTableList())
	setDefault(confData, KeyCollectKVStoreReport, true)
	setDefault(confData, KeyCollectJStack, false)
	setDefault(confData, KeyCollectJFR, true)
	setDefault(confData, KeyCollectTtop, true)
	setDefault(confData, KeyDremioLogsNumDays, 7)
	setDefault(confData, KeyDremioQueriesJSONNumDays, 30)
	setDefault(confData, KeyCollectSystemTablesTimeoutSeconds, 1440) // 24 minutes for health check system tables collection since they're very important for health check analysis.
	setDefault(confData, KeyNumberJobProfiles, 10000)
}

func WAFCollectionProfile(confData map[string]interface{}, hostName string, defaultCaptureSeconds int) {
	Defaults(confData, hostName, defaultCaptureSeconds)
	setDefault(confData, KeySysTables, SystemTableListWaf())
	setDefault(confData, KeyCollectKVStoreReport, false)
	setDefault(confData, KeyCollectJStack, false)
	setDefault(confData, KeyCollectJFR, false)
	setDefault(confData, KeyCollectTtop, false)
	setDefault(confData, KeyDremioLogsNumDays, 3)
	setDefault(confData, KeyDremioQueriesJSONNumDays, 3)
	setDefault(confData, KeyCollectSystemTablesTimeoutSeconds, 7200) // 2 hours for health check system tables collection since they're very important for health check analysis.
	setDefault(confData, KeyNumberJobProfiles, 25000)
}

// SetViperDefaults wires up default values for viper when the ddc.yaml or the cli flags do not set the value
func SetViperDefaults(confData map[string]interface{}, hostName string, defaultCaptureSeconds int, collectionMode string) {
	// defaults change depending on the collection mode
	switch collectionMode {
	case collects.StandardCollection:
		StandardCollectionProfile(confData, hostName, defaultCaptureSeconds)
	case collects.StandardPlusJSTACKCollection:
		WithJStackCollectionProfile(confData, hostName, defaultCaptureSeconds)
	case collects.HealthCheckCollection:
		HealthCheckCollectionProfile(confData, hostName, defaultCaptureSeconds)
	case collects.WAFCollection:
		WAFCollectionProfile(confData, hostName, defaultCaptureSeconds)
	default:
		QuickCollectionProfile(confData, hostName, defaultCaptureSeconds)
	}
}
