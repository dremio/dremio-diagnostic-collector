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

const (
	// KeyVerbose provides output verbosity when the local-collect command is running,
	// this does not affect the log files which are always debug
	KeyVerbose                           = "verbose"
	KeyDisableRESTAPI                    = "disable-rest-api"
	KeyCollectAccelerationLog            = "collect-acceleration-log"
	KeyCollectAccessLog                  = "collect-access-log"
	KeyCollectAuditLog                   = "collect-audit-log"
	KeyCollectJVMFlags                   = "collect-jvm-flags"
	KeyDremioLogDir                      = "dremio-log-dir"
	KeyNumberThreads                     = "number-threads"
	KeyDremioPid                         = "dremio-pid"
	KeyDremioPidDetection                = "dremio-pid-detection"
	KeyDremioUsername                    = "dremio-username"
	KeyDremioPatToken                    = "dremio-pat-token" // #nosec G101
	KeyDremioConfDir                     = "dremio-conf-dir"
	KeyDremioRocksdbDir                  = "dremio-rocksdb-dir"
	KeyCollectDremioConfiguration        = "collect-dremio-configuration"
	KeyCaptureHeapDump                   = "capture-heap-dump"
	KeyNumberJobProfiles                 = "number-job-profiles"
	KeyDremioEndpoint                    = "dremio-endpoint"
	KeyTarballOutDir                     = "tarball-out-dir"
	KeyTmpOutputDir                      = "tmp-output-dir"
	KeyCollectMetrics                    = "collect-metrics"
	KeyCollectOSConfig                   = "collect-os-config"
	KeyCollectDiskUsage                  = "collect-disk-usage"
	KeyDremioLogsNumDays                 = "dremio-logs-num-days"
	KeyDremioQueriesJSONNumDays          = "dremio-queries-json-num-days"
	KeyDremioGCFilePattern               = "dremio-gc-file-pattern"
	KeyCollectQueriesJSON                = "collect-queries-json"
	KeyCollectServerLogs                 = "collect-server-logs"
	KeyCollectMetaRefreshLog             = "collect-meta-refresh-log"
	KeyCollectReflectionLog              = "collect-reflection-log"
	KeyCollectGCLogs                     = "collect-gc-logs"
	KeyCollectJFR                        = "collect-jfr"
	KeyCollectJStack                     = "collect-jstack"
	KeyCollectTtop                       = "collect-ttop"
	KeyCollectSystemTablesExport         = "collect-system-tables-export"
	KeySystemTablesRowLimit              = "system-tables-row-limit"
	KeyCollectWLM                        = "collect-wlm"
	KeyCollectKVStoreReport              = "collect-kvstore-report"
	KeyDremioJStackTimeSeconds           = "dremio-jstack-time-seconds"
	KeyDremioJFRTimeSeconds              = "dremio-jfr-time-seconds"
	KeyNodeMetricsCollectDurationSeconds = "node-metrics-collect-duration-seconds"
	KeyDremioJStackFreqSeconds           = "dremio-jstack-freq-seconds"
	KeyDremioTtopFreqSeconds             = "dremio-ttop-freq-seconds"
	KeyDremioTtopTimeSeconds             = "dremio-ttop-time-seconds"
	KeyDremioGCLogsDir                   = "dremio-gclogs-dir"
	KeyNodeName                          = "node-name"
	KeyAcceptCollectionConsent           = "accept-collection-consent"
	KeyIsDremioCloud                     = "is-dremio-cloud"
	KeyDremioCloudProjectID              = "dremio-cloud-project-id"
	KeyAllowInsecureSSL                  = "allow-insecure-ssl"
	KeyJobProfilesNumHighQueryCost       = "job-profiles-num-high-query-cost"
	KeyJobProfilesNumSlowExec            = "job-profiles-num-slow-exec"
	KeyJobProfilesNumRecentErrors        = "job-profiles-num-recent-errors"
	KeyJobProfilesNumSlowPlanning        = "job-profiles-num-slow-planning"
	KeyRestHTTPTimeout                   = "rest-http-timeout"
)
