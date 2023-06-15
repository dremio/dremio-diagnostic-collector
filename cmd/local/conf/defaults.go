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
	"github.com/spf13/viper"
)

func SetViperDefaults(hostName string, defaultCaptureSeconds int, outputDir string) {
	// set default config
	viper.SetDefault(KeyVerbose, "vv")
	viper.SetDefault(KeyCollectAccelerationLog, false)
	viper.SetDefault(KeyCollectAccessLog, false)
	viper.SetDefault(KeyDremioLogDir, "/var/log/dremio")
	viper.SetDefault(KeyNumberThreads, 2)
	viper.SetDefault(KeyDremioUsername, "dremio")
	viper.SetDefault(KeyDremioPatToken, "")
	viper.SetDefault(KeyDremioConfDir, "/opt/dremio/conf")
	viper.SetDefault(KeyDremioRocksdbDir, "/opt/dremio/data/db")
	viper.SetDefault(KeyCollectDremioConfiguration, true)
	viper.SetDefault(KeyCaptureHeapDump, false)
	viper.SetDefault(KeyNumberJobProfiles, 25000)
	viper.SetDefault(KeyDremioEndpoint, "http://localhost:9047")
	viper.SetDefault(KeyTmpOutputDir, outputDir)
	viper.SetDefault(KeyCollectMetrics, true)
	viper.SetDefault(KeyCollectDiskUsage, true)
	viper.SetDefault(KeyDremioLogsNumDays, 7)
	viper.SetDefault(KeyDremioQueriesJSONNumDays, 28)
	viper.SetDefault(KeyDremioGCFilePattern, "gc*.log*")
	viper.SetDefault(KeyCollectQueriesJSON, true)
	viper.SetDefault(KeyCollectServerLogs, true)
	viper.SetDefault(KeyCollectMetaRefreshLog, true)
	viper.SetDefault(KeyCollectReflectionLog, true)
	viper.SetDefault(KeyCollectGCLogs, true)
	viper.SetDefault(KeyCollectJFR, true)
	viper.SetDefault(KeyCollectJStack, true)
	viper.SetDefault(KeyCollectSystemTablesExport, true)
	viper.SetDefault(KeyCollectWLM, true)
	viper.SetDefault(KeyCollectKVStoreReport, true)
	viper.SetDefault(KeyDremioJStackTimeSeconds, defaultCaptureSeconds)
	viper.SetDefault(KeyDremioJFRTimeSeconds, defaultCaptureSeconds)
	viper.SetDefault(KeyNodeMetricsCollectDurationSeconds, defaultCaptureSeconds)
	viper.SetDefault(KeyDremioJStackFreqSeconds, 1)
	viper.SetDefault(KeyDremioGCLogsDir, "")
	viper.SetDefault(KeyNodeName, hostName)
	viper.SetDefault(KeyAcceptCollectionConsent, true)
	viper.SetDefault(KeyAllowInsecureSSL, true)
	viper.SetDefault(KeyRestHTTPTimeout, 30)

}
