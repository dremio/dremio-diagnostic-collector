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

func setDefault(confData map[string]interface{}, key string, value interface{}) {
	// if key is not present go ahead and set it
	if _, ok := confData[key]; !ok {
		confData[key] = value
	}
}

// SetViperDefaults wires up default values for viper when the ddc.yaml or the cli flags do not set the value
func SetViperDefaults(confData map[string]interface{}, hostName string, defaultCaptureSeconds int) {
	// set default config
	setDefault(confData, KeyVerbose, "vv")
	setDefault(confData, KeyDisableRESTAPI, false)
	setDefault(confData, KeyCollectAccelerationLog, false)
	setDefault(confData, KeyCollectAccessLog, false)
	setDefault(confData, KeyCollectAuditLog, false)
	setDefault(confData, KeyCollectJVMFlags, true)
	setDefault(confData, KeyDremioLogDir, "/var/log/dremio")
	setDefault(confData, KeyNumberThreads, 2)
	setDefault(confData, KeyDremioPid, 0)
	setDefault(confData, KeyDremioPidDetection, true)
	setDefault(confData, KeyDremioUsername, "dremio")
	setDefault(confData, KeyDremioPatToken, "")
	setDefault(confData, KeyDremioConfDir, "/opt/dremio/conf")
	setDefault(confData, KeyDremioRocksdbDir, "/opt/dremio/data/db")
	setDefault(confData, KeyCollectDremioConfiguration, true)
	setDefault(confData, KeyCaptureHeapDump, false)
	setDefault(confData, KeyNumberJobProfiles, 25000)
	setDefault(confData, KeyDremioEndpoint, "http://localhost:9047")
	setDefault(confData, KeyTarballOutDir, "/tmp/ddc")
	setDefault(confData, KeyCollectOSConfig, true)
	setDefault(confData, KeyCollectDiskUsage, true)
	setDefault(confData, KeyDremioLogsNumDays, 7)
	setDefault(confData, KeyDremioQueriesJSONNumDays, 28)
	setDefault(confData, KeyDremioGCFilePattern, "gc*.log*")
	setDefault(confData, KeyCollectQueriesJSON, true)
	setDefault(confData, KeyCollectServerLogs, true)
	setDefault(confData, KeyCollectMetaRefreshLog, true)
	setDefault(confData, KeyCollectReflectionLog, true)
	setDefault(confData, KeyCollectGCLogs, true)
	setDefault(confData, KeyCollectJFR, true)
	setDefault(confData, KeyCollectTtop, true)
	setDefault(confData, KeyCollectJStack, true)
	setDefault(confData, KeyCollectSystemTablesExport, true)
	setDefault(confData, KeySystemTablesRowLimit, 100000)
	setDefault(confData, KeyCollectWLM, true)
	setDefault(confData, KeyCollectKVStoreReport, true)
	setDefault(confData, KeyDremioJStackTimeSeconds, defaultCaptureSeconds)
	setDefault(confData, KeyDremioJFRTimeSeconds, defaultCaptureSeconds)
	setDefault(confData, KeyDremioJStackFreqSeconds, 1)
	setDefault(confData, KeyDremioTtopFreqSeconds, 1)
	setDefault(confData, KeyDremioTtopTimeSeconds, defaultCaptureSeconds)
	setDefault(confData, KeyDremioGCLogsDir, "")
	setDefault(confData, KeyNodeName, hostName)
	setDefault(confData, KeyAcceptCollectionConsent, true)
	setDefault(confData, KeyIsDremioCloud, false)
	setDefault(confData, KeyDremioCloudProjectID, "")
	setDefault(confData, KeyAllowInsecureSSL, true)
	setDefault(confData, KeyRestHTTPTimeout, 30)
}
