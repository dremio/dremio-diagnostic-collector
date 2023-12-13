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

// package conf provides configuration for the local-collect command
package conf

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf/autodetect"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/ddcio"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/restclient"
	"github.com/dremio/dremio-diagnostic-collector/pkg/dirs"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
	"github.com/google/uuid"
	"github.com/spf13/cast"
)

func GetString(confData map[string]interface{}, key string) string {
	if v, ok := confData[key]; ok {
		return cast.ToString(v)
	}
	return ""
}

func GetInt(confData map[string]interface{}, key string) int {
	if v, ok := confData[key]; ok {
		return cast.ToInt(v)
	}
	return 0
}

func GetBool(confData map[string]interface{}, key string) bool {
	if v, ok := confData[key]; ok {
		return cast.ToBool(v)
	}
	return false
}

// We just strip suffix at the moment. More checks can be added here
func SanitiseURL(url string) string {
	return strings.TrimSuffix(url, "/")
}

type CollectConf struct {
	// flags that are configurable by env or configuration
	numberThreads              int
	disableRESTAPI             bool
	gcLogsDir                  string
	dremioLogDir               string
	dremioConfDir              string
	dremioEndpoint             string
	dremioUsername             string
	dremioPATToken             string
	dremioRocksDBDir           string
	numberJobProfilesToCollect int
	dremioPIDDetection         bool
	collectAccelerationLogs    bool
	collectAccessLogs          bool
	collectAuditLogs           bool
	collectJVMFlags            bool
	captureHeapDump            bool
	acceptCollectionConsent    bool
	isDremioCloud              bool
	dremioCloudProjectID       string
	dremioCloudAppEndpoint     string

	// advanced variables setable by configuration or environement variable
	outputDir                   string
	tarballOutDir               string
	dremioTtopTimeSeconds       int
	dremioTtopFreqSeconds       int
	dremioJFRTimeSeconds        int
	dremioJStackFreqSeconds     int
	dremioJStackTimeSeconds     int
	dremioLogsNumDays           int
	dremioGCFilePattern         string
	dremioQueriesJSONNumDays    int
	jobProfilesNumSlowExec      int
	jobProfilesNumHighQueryCost int
	jobProfilesNumSlowPlanning  int
	jobProfilesNumRecentErrors  int
	allowInsecureSSL            bool
	collectJFR                  bool
	collectJStack               bool
	collectKVStoreReport        bool
	collectServerLogs           bool
	collectMetaRefreshLogs      bool
	collectQueriesJSON          bool
	collectDremioConfiguration  bool
	collectReflectionLogs       bool
	collectSystemTablesExport   bool
	systemTablesRowLimit        int
	collectOSConfig             bool
	collectDiskUsage            bool
	collectGCLogs               bool
	collectTtop                 bool
	collectWLM                  bool
	nodeName                    string
	restHTTPTimeout             int

	// variables
	systemtables            []string
	systemtablesdremiocloud []string
	dremioPID               int
}

func DetectRocksDB(dremioHome string, dremioConfDir string) string {
	dremioConfFile := filepath.Join(dremioConfDir, "dremio.conf")
	content, err := os.ReadFile(filepath.Clean(dremioConfFile))
	if err != nil {
		simplelog.Errorf("configuration directory incorrect : %v", err)
	}
	confValues, err := parseAndResolveConfig(string(content), dremioHome)
	if err != nil {
		simplelog.Errorf("configuration directory incorrect : %v", err)
	}
	//searching rocksdb
	var rocksDBDir string
	if value, ok := confValues["db"]; ok {
		rocksDBDir = value
	} else {
		rocksDBDir = filepath.Join(dremioHome, "data", "db")
	}
	return rocksDBDir
}

func SystemTableList() []string {
	return []string{
		"\\\"tables\\\"",
		"boot",
		"fragments",
		"jobs",
		"materializations",
		"membership",
		"memory",
		"nodes",
		"options",
		"privileges",
		"reflection_dependencies",
		"reflections",
		"refreshes",
		"roles",
		"services",
		"slicing_threads",
		"table_statistics",
		"threads",
		"version",
		"views",
		"cache.datasets",
		"cache.mount_points",
		"cache.objects",
		"cache.storage_plugins",
	}
}

func LogConfData(confData map[string]string) {
	for k, v := range confData {
		if k == KeyDremioPatToken && v != "" {
			simplelog.Debugf("conf key '%v':'REDACTED'", k)
		} else {
			simplelog.Debugf("conf key '%v':'%v'", k, v)
		}
	}
}
func ReadConf(overrides map[string]string, ddcYamlLoc string) (*CollectConf, error) {
	confData, err := ParseConfig(ddcYamlLoc, overrides)
	if err != nil {
		return &CollectConf{}, fmt.Errorf("config failed: %w", err)
	}
	simplelog.Debugf("logging parsed configuration from ddc.yaml")
	defaultCaptureSeconds := 60
	// set node name
	hostName, err := os.Hostname()
	if err != nil {
		hostName = fmt.Sprintf("unknown-%v", uuid.New())
	}

	SetViperDefaults(confData, hostName, defaultCaptureSeconds, getOutputDir(time.Now()))

	c := &CollectConf{}
	c.systemtables = SystemTableList()
	c.systemtablesdremiocloud = []string{
		"organization.clouds",
		"organization.privileges",
		"organization.projects",
		"organization.roles",
		"organization.usage",
		"project.engines",
		"project.jobs",
		"project.privileges",
		"project.reflections",
		"project.\\\"tables\\\"",
		"project.views",
		// "project.history.events",
		"project.history.jobs",
	}

	for k, v := range confData {
		if k == KeyDremioPatToken && v != "" {
			simplelog.Debugf("conf key '%v':'REDACTED'", k)
		} else {
			simplelog.Debugf("conf key '%v':'%v'", k, v)
		}
	}
	// now we can setup verbosity as we are parsing it in the ParseConfig function
	// TODO REMOVE OR CHANGE MEANING
	// verboseString := GetString(confData, "verbose")
	// verbose := strings.Count(verboseString, "v")
	// simplelog.InitLogger(verbose)
	// we use dremio cloud option here to know if we should validate the log and conf dirs or not
	c.isDremioCloud = GetBool(confData, KeyIsDremioCloud)

	c.dremioPIDDetection = GetBool(confData, KeyDremioPidDetection)
	c.acceptCollectionConsent = GetBool(confData, KeyAcceptCollectionConsent)
	c.dremioCloudProjectID = GetString(confData, KeyDremioCloudProjectID)
	c.collectAccelerationLogs = GetBool(confData, KeyCollectAccelerationLog)
	c.collectAccessLogs = GetBool(confData, KeyCollectAccessLog)
	c.collectAuditLogs = GetBool(confData, KeyCollectAuditLog)
	c.gcLogsDir = GetString(confData, KeyDremioGCLogsDir)
	c.nodeName = GetString(confData, KeyNodeName)
	c.numberThreads = GetInt(confData, KeyNumberThreads)
	// log collect
	c.tarballOutDir = GetString(confData, KeyTarballOutDir)
	c.outputDir = GetString(confData, KeyTmpOutputDir)
	c.dremioLogsNumDays = GetInt(confData, KeyDremioLogsNumDays)
	c.dremioQueriesJSONNumDays = GetInt(confData, KeyDremioQueriesJSONNumDays)
	c.dremioGCFilePattern = GetString(confData, KeyDremioGCFilePattern)
	c.collectQueriesJSON = GetBool(confData, KeyCollectQueriesJSON)
	c.collectServerLogs = GetBool(confData, KeyCollectServerLogs)
	c.collectMetaRefreshLogs = GetBool(confData, KeyCollectMetaRefreshLog)
	c.collectReflectionLogs = GetBool(confData, KeyCollectReflectionLog)
	c.collectGCLogs = GetBool(confData, KeyCollectGCLogs)
	c.gcLogsDir = GetString(confData, KeyDremioGCLogsDir)
	c.dremioUsername = GetString(confData, KeyDremioUsername)
	c.disableRESTAPI = GetBool(confData, KeyDisableRESTAPI)

	c.dremioPATToken = GetString(confData, KeyDremioPatToken)
	c.collectDremioConfiguration = GetBool(confData, KeyCollectDremioConfiguration)
	c.numberJobProfilesToCollect = GetInt(confData, KeyNumberJobProfiles)

	// system diag
	c.collectOSConfig = GetBool(confData, KeyCollectOSConfig)
	c.collectDiskUsage = GetBool(confData, KeyCollectDiskUsage)
	c.collectJVMFlags = GetBool(confData, KeyCollectJVMFlags)

	// jfr config
	c.dremioJFRTimeSeconds = GetInt(confData, KeyDremioJFRTimeSeconds)
	// jstack config
	c.dremioJStackTimeSeconds = GetInt(confData, KeyDremioJStackTimeSeconds)
	c.dremioJStackFreqSeconds = GetInt(confData, KeyDremioJStackFreqSeconds)

	// ttop
	c.collectTtop = GetBool(confData, KeyCollectTtop)
	c.dremioTtopFreqSeconds = GetInt(confData, KeyDremioTtopFreqSeconds)
	c.dremioTtopTimeSeconds = GetInt(confData, KeyDremioTtopTimeSeconds)

	c.dremioPID = GetInt(confData, KeyDremioPid)
	if c.dremioPID < 1 && c.dremioPIDDetection {
		dremioPID, err := autodetect.GetDremioPID()
		if err != nil {
			simplelog.Errorf("disabling Heap Dump Capture, Jstack and JFR collection: %v", err)
			//return &CollectConf{}, fmt.Errorf("read config stopped due to error %v", err)
		} else {
			c.dremioPID = dremioPID
		}
	}
	dremioPIDIsValid := c.dremioPID > 0
	// captures that wont work if the dremioPID is invalid
	c.captureHeapDump = GetBool(confData, KeyCaptureHeapDump) && dremioPIDIsValid
	c.collectJFR = GetBool(confData, KeyCollectJFR) && dremioPIDIsValid
	c.collectJStack = GetBool(confData, KeyCollectJStack) && dremioPIDIsValid

	//we do not want to validate configuration of logs for dremio cloud
	if !c.isDremioCloud {
		var detectedConfig DremioConfig
		capturesATypeOfLog := c.collectServerLogs || c.collectAccelerationLogs || c.collectAccessLogs || c.collectAuditLogs || c.collectMetaRefreshLogs || c.collectReflectionLogs
		if capturesATypeOfLog {
			// enable some autodetected directories
			if dremioPIDIsValid {
				var err error
				detectedConfig, err = GetConfiguredDremioValuesFromPID(c.dremioPID)
				if err != nil {
					msg := fmt.Sprintf("unable to retrieve configuration from pid %v: %v", c.dremioPID, err)
					fmt.Println(msg)
					simplelog.Errorf(msg)
				} else {
					c.dremioLogDir = detectedConfig.LogDir
					c.dremioConfDir = detectedConfig.ConfDir
				}
			} else {
				fmt.Println("no valid pid found therefore the log and configuration autodetection will not function")
				simplelog.Warning("no valid pid found therefor the log and configuration autodetection will not function")
			}

			// configure log dir
			configuredLogDir := GetString(confData, KeyDremioLogDir)
			fmt.Printf("configured log dir is %v detected is %v\n", configuredLogDir, detectedConfig.LogDir)
			// see if the configured dir is valid
			if err := dirs.CheckDirectory(configuredLogDir, func(de []fs.DirEntry) bool { return len(de) > 1 }); err != nil {
				msg := fmt.Sprintf("configured log %v is invalid: %v", configuredLogDir, err)
				fmt.Println(msg)
				simplelog.Warning(msg)
			} else {
				c.dremioLogDir = configuredLogDir
			}
			msg := fmt.Sprintf("using log dir '%v'", c.dremioLogDir)
			simplelog.Info(msg)
			fmt.Println(msg)
			if err := dirs.CheckDirectory(c.dremioLogDir, func(de []fs.DirEntry) bool {
				// in a common misconfigured directory server.out will still be present
				return len(de) > 1
			}); err != nil {
				return &CollectConf{}, fmt.Errorf("invalid dremio log dir '%v', update ddc.yaml and fix it: %v", c.dremioLogDir, err)
			}

		}
		if c.collectDremioConfiguration {
			// configure configuration directory
			configuredConfDir := GetString(confData, KeyDremioConfDir)
			// see if the configured dir is valid
			if err := dirs.CheckDirectory(configuredConfDir, func(de []fs.DirEntry) bool { return len(de) > 0 }); err != nil {
				msg := fmt.Sprintf("configured dir %v is invalid: %v", configuredConfDir, err)
				fmt.Println(msg)
				simplelog.Warningf(msg)
			} else {
				// if the configured directory is valid ALWAYS pick that
				c.dremioConfDir = configuredConfDir
			}
			msg := fmt.Sprintf("using config dir '%v'", c.dremioConfDir)
			simplelog.Info(msg)
			fmt.Println(msg)
			if err := dirs.CheckDirectory(c.dremioConfDir, func(de []fs.DirEntry) bool {
				return len(de) > 0
			}); err != nil {
				return &CollectConf{}, fmt.Errorf("invalid dremio conf dir '%v', update ddc.yaml and fix it: %v", c.dremioConfDir, err)
			}
		}
		// now try and configure rocksdb
		validateRocks := func(de []fs.DirEntry) bool {
			for _, e := range de {
				if e.Name() == "catalog" {
					return true
				}
			}
			return false
		}
		// configured value
		configuredRocksDb := GetString(confData, KeyDremioRocksdbDir)
		if err := dirs.CheckDirectory(configuredRocksDb, validateRocks); err != nil {
			msg := fmt.Sprintf("configured rocks '%v' is invalid %v", configuredRocksDb, err)
			fmt.Println(msg)
			simplelog.Warning(msg)
			// detected value
			c.dremioRocksDBDir = DetectRocksDB(detectedConfig.Home, c.dremioConfDir)
		} else {
			c.dremioRocksDBDir = configuredRocksDb
		}
		msg := fmt.Sprintf("using rocks db dir %v", c.dremioRocksDBDir)
		fmt.Println(msg)
		simplelog.Info(msg)
		if err := dirs.CheckDirectory(c.dremioRocksDBDir, validateRocks); err != nil {
			simplelog.Warningf("only applies to coordinators - invalid rocksdb dir '%v', update ddc.yaml and fix it: %v", c.dremioConfDir, err)
		}

	}
	// end discovering minimal configuration

	c.dremioEndpoint = GetString(confData, KeyDremioEndpoint)
	if c.isDremioCloud {
		if len(c.dremioCloudProjectID) != 36 {
			simplelog.Warningf("dremio cloud project id is expected to have 36 characters - the following provided id may be incorrect: %v", c.dremioCloudProjectID)
		}
		if strings.Contains(c.dremioEndpoint, "eu.dremio.cloud") {
			c.dremioEndpoint = "https://api.eu.dremio.cloud"
			c.dremioCloudAppEndpoint = "https://app.eu.dremio.cloud"
		} else if strings.Contains(c.dremioEndpoint, "dremio.cloud") {
			c.dremioEndpoint = "https://api.dremio.cloud"
			c.dremioCloudAppEndpoint = "https://app.dremio.cloud"
		} else {
			simplelog.Warningf("unexpected dremio cloud endpoint: %v - Known endpoints are https://app.dremio.cloud and https://app.eu.dremio.cloud", c.dremioEndpoint)
		}
	}

	// collect rest apis
	disableRESTAPI := c.disableRESTAPI || c.dremioPATToken == ""
	if disableRESTAPI {
		simplelog.Debugf("disabling all Workload Manager, System Table, KV Store, and Job Profile collection since the --dremio-pat-token is not set")
	}
	c.allowInsecureSSL = GetBool(confData, KeyAllowInsecureSSL)
	c.collectWLM = GetBool(confData, KeyCollectWLM) && !disableRESTAPI
	c.collectSystemTablesExport = GetBool(confData, KeyCollectSystemTablesExport) && !disableRESTAPI
	c.systemTablesRowLimit = GetInt(confData, KeySystemTablesRowLimit)
	c.collectKVStoreReport = GetBool(confData, KeyCollectKVStoreReport) && !disableRESTAPI
	c.restHTTPTimeout = GetInt(confData, KeyRestHTTPTimeout)
	restclient.InitClient(c.allowInsecureSSL, c.restHTTPTimeout)

	numberJobProfilesToCollect, jobProfilesNumHighQueryCost, jobProfilesNumSlowExec, jobProfilesNumRecentErrors, jobProfilesNumSlowPlanning := CalculateJobProfileSettingsWithViperConfig(c)
	c.numberJobProfilesToCollect = numberJobProfilesToCollect
	c.jobProfilesNumHighQueryCost = jobProfilesNumHighQueryCost
	c.jobProfilesNumSlowExec = jobProfilesNumSlowExec
	c.jobProfilesNumRecentErrors = jobProfilesNumRecentErrors
	c.jobProfilesNumSlowPlanning = jobProfilesNumSlowPlanning
	// TODO figure out if this makes any sense as nothing changed these values
	// this is just logging logic and not actually useful for anything but reporting
	IsAWSEfromLogDirs, err := autodetect.IsAWSEfromLogDirs()
	if err != nil {
		simplelog.Warningf("unable to determine if node is AWSE or not due to error %v", err)
	}
	if IsAWSEfromLogDirs {
		isCoord, logPath, err := autodetect.IsAWSECoordinator()
		if err != nil {
			simplelog.Errorf("unable to detect if this node %v was a coordinator so will not apply AWSE log path fix this may mean no log collection %v", c.nodeName, err)
		}
		if isCoord {
			simplelog.Debugf("AWSE coordinator node detected, using log dir %v, symlinked to %v", c.dremioLogDir, logPath)
		} else {
			simplelog.Debugf("AWSE executor node detected, using log dir %v, symlinked to %v", c.dremioLogDir, logPath)
		}
	}
	return c, nil
}

// parseAndResolveConfig parses the dremio.conf content and resolves placeholders based on the provided DREMIO_HOME.
func parseAndResolveConfig(confContent, dremioHome string) (map[string]string, error) {
	scanner := bufio.NewScanner(strings.NewReader(confContent))
	config := make(map[string]string)

	for scanner.Scan() {
		line := scanner.Text()
		// Replace DREMIO_HOME placeholder

		line = strings.ReplaceAll(line, "${DREMIO_HOME}", dremioHome)
		trimmedLine := strings.TrimSpace(line)

		// Skip empty lines and comments
		if trimmedLine == "" || strings.HasPrefix(trimmedLine, "#") {
			continue
		}

		parts := strings.SplitN(trimmedLine, ":", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.Trim(parts[1], " ,\"'")

		// Store in map
		config[key] = value
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	local := ""
	for key, value := range config {
		if strings.Contains("local", key) || strings.Contains("path.local", key) {
			local = value
			break
		}
	}
	for key, value := range config {
		config[key] = strings.ReplaceAll(value, "${paths.local}", local)
	}

	return config, nil
}

// DremioConfig represents the configuration details for Dremio.
type DremioConfig struct {
	Home    string
	LogDir  string
	ConfDir string
}

func GetConfiguredDremioValuesFromPID(dremioPID int) (DremioConfig, error) {
	var w bytes.Buffer
	err := ddcio.Shell(&w, fmt.Sprintf("ps eww %v | grep dremio | awk '{$1=$2=$3=$4=\"\"; print $0}'", dremioPID))
	if err != nil {
		return DremioConfig{}, err
	}
	return ParsePSForConfig(w.String())
}

func ParsePSForConfig(ps string) (DremioConfig, error) {
	// Define the keys to search for
	dremioHomeKey := "DREMIO_HOME="
	dremioLogDirKey := "-Ddremio.log.path="
	dremioConfDirKey := "DREMIO_CONF_DIR="
	dremioLogDirKeyBackup := "DREMIO_LOG_DIR="

	// Find and extract the values
	dremioHome, err := extractValue(ps, dremioHomeKey)
	if err != nil {
		return DremioConfig{}, err
	}

	dremioLogDir, err := extractValue(ps, dremioLogDirKey)
	if err != nil {
		return DremioConfig{}, err
	}
	if dremioLogDir == "" {
		dremioLogDir, err = extractValue(ps, dremioLogDirKeyBackup)
		if err != nil {
			return DremioConfig{}, err
		}
	}

	dremioConfDir, err := extractValue(ps, dremioConfDirKey)
	if err != nil {
		return DremioConfig{}, err
	}

	return DremioConfig{
		Home:    dremioHome,
		LogDir:  dremioLogDir,
		ConfDir: dremioConfDir,
	}, nil
}

// extractValue searches for a key in the input string and extracts the corresponding value.
func extractValue(input string, key string) (string, error) {
	startIndex := strings.Index(input, key)
	if startIndex == -1 {
		return "", errors.New("key not found: " + key)
	}

	// Find the end of the value (space or end of string)
	endIndex := strings.Index(input[startIndex:], " ")
	if endIndex == -1 {
		endIndex = len(input)
	} else {
		endIndex += startIndex
	}

	// Extract the value
	value := input[startIndex+len(key) : endIndex]
	if value == "" {
		return "", fmt.Errorf("did not find %v in string %v", key, input)
	}
	return value, nil
}

func getOutputDir(now time.Time) string {
	nowStr := now.Format("20060102-150405")
	return filepath.Join(os.TempDir(), "ddc", nowStr)
}

func (c CollectConf) DisableRESTAPI() bool {
	return c.disableRESTAPI
}

func (c *CollectConf) GcLogsDir() string {
	return c.gcLogsDir
}

func (c *CollectConf) CollectJFR() bool {
	return c.collectJFR
}

func (c *CollectConf) CollectJStack() bool {
	return c.collectJStack
}

func (c *CollectConf) CaptureHeapDump() bool {
	return c.captureHeapDump
}

func (c *CollectConf) CollectWLM() bool {
	return c.collectWLM
}

func (c *CollectConf) CollectGCLogs() bool {
	return c.collectGCLogs
}

func (c *CollectConf) CollectOSConfig() bool {
	return c.collectOSConfig
}

func (c *CollectConf) CollectDiskUsage() bool {
	return c.collectDiskUsage
}

func (c *CollectConf) CollectDremioConfiguration() bool {
	return c.collectDremioConfiguration
}

func (c *CollectConf) CollectSystemTablesExport() bool {
	return c.collectSystemTablesExport
}

func (c *CollectConf) SystemTablesRowLimit() int {
	return c.systemTablesRowLimit
}

func (c *CollectConf) CollectKVStoreReport() bool {
	return c.collectKVStoreReport
}

func (c *CollectConf) Systemtables() []string {
	return c.systemtables
}

func (c *CollectConf) SystemtablesDremioCloud() []string {
	return c.systemtablesdremiocloud
}

func (c *CollectConf) CollectServerLogs() bool {
	return c.collectServerLogs
}

func (c *CollectConf) CollectQueriesJSON() bool {
	return c.collectQueriesJSON
}

func (c *CollectConf) CollectMetaRefreshLogs() bool {
	return c.collectMetaRefreshLogs
}

func (c *CollectConf) CollectReflectionLogs() bool {
	return c.collectReflectionLogs
}

func (c *CollectConf) CollectAccelerationLogs() bool {
	return c.collectAccelerationLogs
}

func (c *CollectConf) NumberJobProfilesToCollect() int {
	return c.numberJobProfilesToCollect
}

func (c *CollectConf) CollectAccessLogs() bool {
	return c.collectAccessLogs
}

func (c *CollectConf) CollectJVMFlags() bool {
	return c.collectJVMFlags
}

func (c *CollectConf) CollectAuditLogs() bool {
	return c.collectAuditLogs
}

func (c *CollectConf) TtopOutDir() string {
	return filepath.Join(c.outputDir, "ttop", c.nodeName)
}

func (c *CollectConf) HeapDumpsOutDir() string { return filepath.Join(c.outputDir, "heap-dumps") }

func (c *CollectConf) JobProfilesOutDir() string {
	return filepath.Join(c.outputDir, "job-profiles", c.nodeName)
}
func (c *CollectConf) KubernetesOutDir() string { return filepath.Join(c.outputDir, "kubernetes") }
func (c *CollectConf) KVstoreOutDir() string {
	return filepath.Join(c.outputDir, "kvstore", c.nodeName)
}
func (c *CollectConf) SystemTablesOutDir() string {
	return filepath.Join(c.outputDir, "system-tables", c.nodeName)
}

func (c *CollectConf) ClusterStatsOutDir() string {
	return filepath.Join(c.outputDir, "cluster-stats", c.nodeName)
}

func (c *CollectConf) WLMOutDir() string { return filepath.Join(c.outputDir, "wlm", c.nodeName) }

// works on all nodes but includes node name in file name
func (c *CollectConf) JFROutDir() string { return filepath.Join(c.outputDir, "jfr") }

// per node out directories
func (c *CollectConf) ConfigurationOutDir() string {
	return filepath.Join(c.outputDir, "configuration", c.nodeName)
}
func (c *CollectConf) LogsOutDir() string { return filepath.Join(c.outputDir, "logs", c.nodeName) }
func (c *CollectConf) NodeInfoOutDir() string {
	return filepath.Join(c.outputDir, "node-info", c.nodeName)
}
func (c *CollectConf) QueriesOutDir() string {
	return filepath.Join(c.outputDir, "queries", c.nodeName)
}
func (c *CollectConf) ThreadDumpsOutDir() string {
	return filepath.Join(c.outputDir, "jfr", "thread-dumps", c.nodeName)
}

func (c *CollectConf) DremioEndpoint() string {
	return SanitiseURL(c.dremioEndpoint)
}

func (c *CollectConf) DremioPATToken() string {
	return c.dremioPATToken
}

func (c *CollectConf) AcceptCollectionConsent() bool {
	return c.acceptCollectionConsent
}

func (c *CollectConf) IsDremioCloud() bool {
	return c.isDremioCloud
}

func (c *CollectConf) DremioCloudProjectID() string {
	return c.dremioCloudProjectID
}

func (c *CollectConf) DremioCloudAppEndpoint() string {
	return c.dremioCloudAppEndpoint
}

func (c *CollectConf) NodeName() string {
	return c.nodeName
}

func (c *CollectConf) TarballOutDir() string {
	return c.tarballOutDir
}

func (c *CollectConf) OutputDir() string {
	return c.outputDir
}

func (c *CollectConf) NumberThreads() int {
	return c.numberThreads
}

func (c *CollectConf) JobProfilesNumSlowPlanning() int {
	return c.jobProfilesNumSlowPlanning
}

func (c *CollectConf) JobProfilesNumSlowExec() int {
	return c.jobProfilesNumSlowExec
}

func (c *CollectConf) JobProfilesNumHighQueryCost() int {
	return c.jobProfilesNumHighQueryCost
}

func (c *CollectConf) JobProfilesNumRecentErrors() int {
	return c.jobProfilesNumRecentErrors
}

func (c *CollectConf) DremioPID() int {
	return c.dremioPID
}

func (c *CollectConf) DremioPIDDetection() bool {
	return c.dremioPIDDetection
}

func (c *CollectConf) DremioConfDir() string {
	return c.dremioConfDir
}

func (c *CollectConf) DremioJFRTimeSeconds() int {
	return c.dremioJFRTimeSeconds
}

func (c *CollectConf) DremioTtopTimeSeconds() int {
	return c.dremioTtopTimeSeconds
}

func (c *CollectConf) DremioTtopFreqSeconds() int {
	return c.dremioTtopFreqSeconds
}

func (c *CollectConf) CollectTtop() bool {
	return c.collectTtop
}

func (c *CollectConf) DremioJStackTimeSeconds() int {
	return c.dremioJStackTimeSeconds
}

func (c *CollectConf) DremioJStackFreqSeconds() int {
	return c.dremioJStackFreqSeconds
}

func (c *CollectConf) DremioLogDir() string {
	return c.dremioLogDir
}

func (c *CollectConf) DremioGCFilePattern() string {
	return c.dremioGCFilePattern
}

func (c *CollectConf) DremioQueriesJSONNumDays() int {
	return c.dremioQueriesJSONNumDays
}

func (c *CollectConf) DremioLogsNumDays() int {
	return c.dremioLogsNumDays
}

func (c *CollectConf) RestHTTPTimeout() int {
	return c.restHTTPTimeout
}

func (c *CollectConf) DremioRocksDBDir() string {
	return c.dremioRocksDBDir
}
