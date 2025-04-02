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
	"slices"
	"strings"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/conf/autodetect"
	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/ddcio"
	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/restclient"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/collects"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/consoleprint"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/dirs"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/shutdown"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/simplelog"
	"github.com/google/uuid"
	"github.com/spf13/cast"
)

func GetString(confData map[string]interface{}, key string) string {
	if v, ok := confData[key]; ok {
		return cast.ToString(v)
	}
	return ""
}

func GetStringArray(confData map[string]interface{}, key string) []string {
	var e []string // empty array to fulfill return for noentry
	if v, ok := confData[key]; ok {
		return cast.ToStringSlice(v)
	}
	return e
}

func GetInt(confData map[string]interface{}, key string) int {
	if v, ok := confData[key]; ok {
		return cast.ToInt(v)
	}
	return 0
}

func GetUint64(confData map[string]interface{}, key string) uint64 {
	if v, ok := confData[key]; ok {
		return cast.ToUint64(v)
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
	disableFreeSpaceCheck      bool
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
	isRESTCollect              bool
	restCollectDailyJobsLimit  int
	isDremioCloud              bool
	dremioCloudProjectID       string
	dremioCloudAppEndpoint     string

	// advanced variables settable by configuration or environment variable
	outputDir                         string
	tarballOutDir                     string
	dremioTtopTimeSeconds             int
	dremioTtopFreqSeconds             int
	dremioJFRTimeSeconds              int
	dremioJStackFreqSeconds           int
	dremioJStackTimeSeconds           int
	dremioLogsNumDays                 int
	dremioGCFilePattern               string
	dremioQueriesJSONNumDays          int
	jobProfilesNumSlowExec            int
	jobProfilesNumHighQueryCost       int
	jobProfilesNumSlowPlanning        int
	jobProfilesNumRecentErrors        int
	allowInsecureSSL                  bool
	collectJFR                        bool
	collectJStack                     bool
	collectKVStoreReport              bool
	collectServerLogs                 bool
	collectMetaRefreshLogs            bool
	collectQueriesJSON                bool
	collectDremioConfiguration        bool
	collectReflectionLogs             bool
	collectVacuumLogs                 bool
	collectSystemTablesExport         bool
	collectSystemTablesTimeoutSeconds int
	systemTablesRowLimit              int
	collectClusterIDTimeoutSeconds    int
	collectOSConfig                   bool
	collectDiskUsage                  bool
	collectGCLogs                     bool
	collectHSErrFiles                 bool
	collectTtop                       bool
	collectWLM                        bool
	nodeName                          string
	restHTTPTimeout                   int
	minFreeSpaceCheckGB               uint64
	noLogDir                          bool

	// variables
	systemTables            []string
	systemtablesdremiocloud []string
	dremioPID               int
}

func ValidateAPICredentials(c *CollectConf, hook shutdown.Hook) error {
	simplelog.Debugf("Validating REST API user credentials...")
	var url string
	if !c.IsDremioCloud() {
		url = c.DremioEndpoint() + "/apiv2/login"
	} else {
		url = c.DremioEndpoint() + "/v0/projects/" + c.DremioCloudProjectID()
	}
	headers := map[string]string{"Content-Type": "application/json"}
	_, err := restclient.APIRequest(hook, url, c.DremioPATToken(), "GET", headers)
	return err
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
	// searching rocksdb
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
		"copy_errors_history",
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
		"user_defined_functions",
		"version",
		"views",
		"cache.datasets",
		"cache.mount_points",
		"cache.storage_plugins",
		// "jobs_recent", // Only collected for REST-only collections
	}
}

func SystemTableListCloud() []string {
	return []string{
		"organization.clouds",
		"organization.privileges",
		"organization.projects",
		"organization.roles",
		"organization.usage",
		"project.engines",
		"project.jobs",
		"project.materializations",
		"project.privileges",
		"project.reflection_dependencies",
		"project.reflections",
		"project.\\\"tables\\\"",
		"project.views",
		// "project.history.events",
		"project.history.jobs",
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

func ReadConf(hook shutdown.Hook, overrides map[string]string, ddcYamlLoc, collectionMode string) (*CollectConf, error) {
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

	SetViperDefaults(confData, hostName, defaultCaptureSeconds, collectionMode)
	c := &CollectConf{}
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
	if c.isDremioCloud {
		// Dremio Cloud is a type of REST API collect, so this is flipped to true
		c.isRESTCollect = true
	} else {
		c.isRESTCollect = GetBool(confData, KeyIsRESTCollect)
	}
	c.restCollectDailyJobsLimit = GetInt(confData, KeyRESTCollectDailyJobsLimit)
	c.dremioPIDDetection = GetBool(confData, KeyDremioPidDetection)
	c.dremioCloudProjectID = GetString(confData, KeyDremioCloudProjectID)
	c.collectAccelerationLogs = GetBool(confData, KeyCollectAccelerationLog)
	c.collectAccessLogs = GetBool(confData, KeyCollectAccessLog)
	c.collectAuditLogs = GetBool(confData, KeyCollectAuditLog)
	c.nodeName = GetString(confData, KeyNodeName)
	c.numberThreads = GetInt(confData, KeyNumberThreads)
	// log collect
	c.tarballOutDir = GetString(confData, KeyTarballOutDir)
	// validate tarball output directory is not empty, because it must be empty or we will end up archiving a lot
	_, err = os.Stat(c.tarballOutDir)
	if err != nil {
		if os.IsNotExist(err) {
			// go ahead and make the directory if it is not present
			if err := os.MkdirAll(c.tarballOutDir, 0o700); err != nil {
				return &CollectConf{}, fmt.Errorf("failed making tarball out dir: %w", err)
			}
		} else {
			// all other errors exit
			return &CollectConf{}, err
		}
	}
	dirEntries, err := os.ReadDir(c.tarballOutDir)
	if err != nil {
		return &CollectConf{}, err
	}
	var entryNames []string
	var entryCount int
	allowedList := []string{"ddc", "ddc.log", "ddc.yaml", fmt.Sprintf("%v.tar.gz", c.nodeName)}
	pidFile := GetString(confData, "pid")
	if pidFile != "" {
		allowedList = append(allowedList, filepath.Base(pidFile))
	}
	for _, e := range dirEntries {
		if slices.Contains(allowedList, e.Name()) {
			continue
		}
		entryCount++
		entryNames = append(entryNames, e.Name())
	}
	if entryCount > 0 {
		return &CollectConf{}, fmt.Errorf("cannot use directory '%v' for tarball output as it contains %v entries: (%v)", c.tarballOutDir, entryCount, entryNames)
	}
	outputDir := GetString(confData, KeyTmpOutputDir)
	if outputDir != "" {
		simplelog.Warningf("key %v is deprecated and will be removed in version 1.0 use %v key instead", KeyTmpOutputDir, KeyTarballOutDir)
		c.outputDir = outputDir
	} else {
		c.outputDir = filepath.Join(c.tarballOutDir, getOutputDir(time.Now()))
	}

	c.dremioLogsNumDays = GetInt(confData, KeyDremioLogsNumDays)
	c.dremioQueriesJSONNumDays = GetInt(confData, KeyDremioQueriesJSONNumDays)
	c.collectQueriesJSON = GetBool(confData, KeyCollectQueriesJSON)
	c.collectServerLogs = GetBool(confData, KeyCollectServerLogs)
	c.collectMetaRefreshLogs = GetBool(confData, KeyCollectMetaRefreshLog)
	c.collectReflectionLogs = GetBool(confData, KeyCollectReflectionLog)
	c.collectVacuumLogs = GetBool(confData, KeyCollectVacuumLog)
	c.collectGCLogs = GetBool(confData, KeyCollectGCLogs)
	c.collectHSErrFiles = GetBool(confData, KeyCollectHSErrFiles)
	c.dremioUsername = GetString(confData, KeyDremioUsername)
	c.disableFreeSpaceCheck = GetBool(confData, KeyDisableFreeSpaceCheck)
	c.minFreeSpaceCheckGB = GetUint64(confData, KeyMinFreeSpaceGB)
	c.noLogDir = GetBool(confData, KeyNoLogDir)
	c.disableRESTAPI = GetBool(confData, KeyDisableRESTAPI)

	c.dremioPATToken = GetString(confData, KeyDremioPatToken)
	if c.dremioPATToken == "" && collectionMode == collects.HealthCheckCollection && !c.disableRESTAPI {
		return &CollectConf{}, errors.New("INVALID CONFIGURATION: the pat is not set and --collect health-check mode requires one")
	}
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
		dremioPID, err := autodetect.GetDremioPID(hook)
		if err != nil {
			simplelog.Errorf("disabling Heap Dump Capture, Jstack and JFR collection: %v", err)
		} else {
			c.dremioPID = dremioPID
		}
	}
	dremioPIDIsValid := c.dremioPID > 0
	if dremioPIDIsValid {
		gcLogPattern, logDir, err := autodetect.FindGCLogLocation(hook, c.dremioPID)
		if err != nil {
			msg := fmt.Sprintf("GC LOG DETECTION DISABLED: will rely on ddc.yaml configuration as ddc is unable to retrieve configuration from pid %v: %v", c.dremioPID, err)
			consoleprint.ErrorPrint(msg)
			simplelog.Error(msg)
			c.gcLogsDir = GetString(confData, KeyDremioGCLogsDir)
			c.dremioGCFilePattern = GetString(confData, KeyDremioGCFilePattern)
		} else {
			c.gcLogsDir = logDir
			c.dremioGCFilePattern = gcLogPattern
		}
	} else {
		c.gcLogsDir = GetString(confData, KeyDremioGCLogsDir)
		c.dremioGCFilePattern = GetString(confData, KeyDremioGCFilePattern)
	}
	// captures that wont work if the dremioPID is invalid
	c.captureHeapDump = GetBool(confData, KeyCaptureHeapDump) && dremioPIDIsValid
	c.collectJFR = GetBool(confData, KeyCollectJFR) && dremioPIDIsValid
	c.collectJStack = GetBool(confData, KeyCollectJStack) && dremioPIDIsValid

	// we do not want to validate configuration of logs for dremio cloud
	if !c.isRESTCollect {
		var detectedConfig DremioConfig
		capturesATypeOfLog := c.collectServerLogs || c.collectAccelerationLogs || c.collectAccessLogs || c.collectAuditLogs || c.collectMetaRefreshLogs || c.collectReflectionLogs || c.collectQueriesJSON
		// because so few people would change the ddc.yaml to skip log capture when they didn't want it we have added this flag
		if capturesATypeOfLog && !c.noLogDir {
			// enable some autodetected directories
			if dremioPIDIsValid {
				var err error
				detectedConfig, err = GetConfiguredDremioValuesFromPID(hook, c.dremioPID)
				if err != nil {
					msg := fmt.Sprintf("AUTODETECTION DISABLED: will rely on ddc.yaml configuration as ddc is unable to retrieve configuration from pid %v: %v", c.dremioPID, err)
					consoleprint.ErrorPrint(msg)
					simplelog.Error(msg)
				} else {
					simplelog.Infof("configured values retrieved from ps output: %v:%v, %v:%v", KeyDremioLogDir, detectedConfig.LogDir, KeyCollectDremioConfiguration, detectedConfig.ConfDir)
					c.dremioLogDir = detectedConfig.LogDir
					c.dremioConfDir = detectedConfig.ConfDir
				}
			} else {
				consoleprint.ErrorPrint("AUTODETECTION DISABLED: will rely on ddc.yaml configuration as the ddc user does not have permissions to the dremio process consider using --sudo-user to resolve this")
				simplelog.Warning("no valid pid found therefor the log and configuration autodetection will not function")
			}

			// function check to validate the logs directory contains
			// files with valid prefix names
			containsValidLog := func(de []fs.DirEntry) error {
				var entries []string
				for _, e := range de {
					entries = append(entries, e.Name())
					if strings.HasPrefix(e.Name(), "server.log") || strings.HasPrefix(e.Name(), "queries.json") {
						return nil
					}
				}
				return fmt.Errorf("no server.log or queries.json present, files in dir (%v)", strings.Join(entries, ","))
			}

			// configure log dir
			configuredLogDir := GetString(confData, KeyDremioLogDir)
			fmt.Printf("configured log dir is: %v\ndetected log dir is: %v\n", configuredLogDir, detectedConfig.LogDir)
			// see if the configured dir is valid
			if err := dirs.CheckDirectory(configuredLogDir, containsValidLog); err != nil {
				msg := fmt.Sprintf("configured log %v is invalid: %v", configuredLogDir, err)
				consoleprint.ErrorPrint(msg)
				simplelog.Warning(msg)
			} else {
				c.dremioLogDir = configuredLogDir
			}
			msg := fmt.Sprintf("using log dir '%v'", c.dremioLogDir)
			simplelog.Info(msg)
			fmt.Println(msg)
			if err := dirs.CheckDirectory(c.dremioLogDir, containsValidLog); err != nil {
				return &CollectConf{}, fmt.Errorf("invalid dremio log dir '%v', set dremio-log-dir in ddc.yaml or if you want to skip log dir collection run --no-log-dir: %w", c.dremioLogDir, err)
			}

		}
		if c.collectDremioConfiguration {
			// configure configuration directory
			configuredConfDir := GetString(confData, KeyDremioConfDir)
			// see if the configured dir is valid
			if err := dirs.CheckDirectory(configuredConfDir, func(de []fs.DirEntry) error {
				if len(de) > 0 {
					return nil
				} else {
					return errors.New("configuration directory is empty")
				}
			}); err != nil {
				msg := fmt.Sprintf("configured dir %v is invalid: %v", configuredConfDir, err)
				fmt.Println(msg)
				simplelog.Warning(msg)
			} else {
				// if the configured directory is valid ALWAYS pick that
				c.dremioConfDir = configuredConfDir
			}
			msg := fmt.Sprintf("using config dir '%v'", c.dremioConfDir)
			simplelog.Info(msg)
			fmt.Println(msg)
			if err := dirs.CheckDirectory(c.dremioConfDir, func(de []fs.DirEntry) error {
				if len(de) > 0 {
					return nil
				} else {
					return errors.New("configuration directory is empty")
				}
			}); err != nil {
				return &CollectConf{}, fmt.Errorf("invalid dremio conf dir '%v', update ddc.yaml and fix it: %w", c.dremioConfDir, err)
			}
		}
		// now try and configure rocksdb
		validateRocks := func(de []fs.DirEntry) error {
			var entries []string
			for _, e := range de {
				entries = append(entries, e.Name())
				if e.Name() == "catalog" {
					return nil
				}
			}
			return fmt.Errorf("catalog is not present in rocksdb dir: entries (%v)", strings.Join(entries, ","))
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

	c.allowInsecureSSL = GetBool(confData, KeyAllowInsecureSSL)
	c.restHTTPTimeout = GetInt(confData, KeyRestHTTPTimeout)
	c.collectClusterIDTimeoutSeconds = GetInt(confData, KeyCollectClusterIDTimeoutSeconds)
	c.collectSystemTablesTimeoutSeconds = GetInt(confData, KeyCollectSystemTablesTimeoutSeconds)
	// collect rest apis
	disableRESTAPI := c.disableRESTAPI || c.dremioPATToken == ""
	if disableRESTAPI {
		simplelog.Debugf("disabling all Workload Manager, System Table, KV Store, and Job Profile collection since the --dremio-pat-token is not set")
		c.numberJobProfilesToCollect = 0
		c.jobProfilesNumHighQueryCost = 0
		c.jobProfilesNumSlowExec = 0
		c.jobProfilesNumRecentErrors = 0
		c.jobProfilesNumSlowPlanning = 0
		c.collectWLM = false
		c.collectSystemTablesExport = false
		c.systemTablesRowLimit = 0
		c.collectKVStoreReport = false
	} else {
		numberJobProfilesToCollect, jobProfilesNumHighQueryCost, jobProfilesNumSlowExec, jobProfilesNumRecentErrors, jobProfilesNumSlowPlanning := CalculateJobProfileSettingsWithViperConfig(c)
		c.numberJobProfilesToCollect = numberJobProfilesToCollect
		c.jobProfilesNumHighQueryCost = jobProfilesNumHighQueryCost
		c.jobProfilesNumSlowExec = jobProfilesNumSlowExec
		c.jobProfilesNumRecentErrors = jobProfilesNumRecentErrors
		c.jobProfilesNumSlowPlanning = jobProfilesNumSlowPlanning
		c.collectWLM = GetBool(confData, KeyCollectWLM)
		c.collectSystemTablesExport = GetBool(confData, KeyCollectSystemTablesExport)
		c.systemTablesRowLimit = GetInt(confData, KeySystemTablesRowLimit)
		c.systemTables = GetStringArray(confData, KeySysTables)
		c.systemtablesdremiocloud = GetStringArray(confData, KeySysTablesCloud)
		c.collectKVStoreReport = GetBool(confData, KeyCollectKVStoreReport)
		restclient.InitClient(c.allowInsecureSSL, c.restHTTPTimeout)
		// validate rest api configuration
		if err := ValidateAPICredentials(c, hook); err != nil {
			return &CollectConf{}, fmt.Errorf("CRITICAL ERROR invalid Dremio API configuration: (url: %v, user: %v) %w", c.dremioEndpoint, c.dremioUsername, err)
		}
	}

	// TODO figure out if this makes any sense as nothing changed these values
	// this is just logging logic and not actually useful for anything but reporting
	IsAWSEfromLogDirs, err := autodetect.IsAWSEfromLogDirs()
	if err != nil {
		simplelog.Warningf("unable to determine if node is AWSE or not: %v", err)
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

func GetConfiguredDremioValuesFromPID(hook shutdown.CancelHook, dremioPID int) (DremioConfig, error) {
	psOut, err := ReadPSEnv(hook, dremioPID)
	if err != nil {
		return DremioConfig{}, err
	}
	return ParsePSForConfig(psOut)
}

func ReadPSEnv(hook shutdown.CancelHook, dremioPID int) (string, error) {
	var w bytes.Buffer
	// grep -v /etc/dremio/preview filters out the AWSE discount preview engine
	err := ddcio.Shell(hook, &w, fmt.Sprintf("ps eww %v | grep dremio | grep -v /etc/dremio/preview | awk '{$1=$2=$3=$4=\"\"; print $0}'", dremioPID))
	if err != nil {
		return "", err
	}
	return w.String(), nil
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
		simplelog.Warningf("key not found: %v", dremioLogDirKey)
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
	startIndex := strings.LastIndex(input, key)
	if startIndex == -1 {
		return "", fmt.Errorf("key not found: %v", key)
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
	return strings.TrimSpace(value), nil
}

func getOutputDir(now time.Time) string {
	return now.Format("20060102-150405")
}

func (c CollectConf) DisableRESTAPI() bool {
	return c.disableRESTAPI
}

func (c CollectConf) DisableFreeSpaceCheck() bool {
	return c.disableFreeSpaceCheck
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

func (c *CollectConf) CollectHSErrFiles() bool {
	return c.collectHSErrFiles
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
	return c.systemTables
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

func (c *CollectConf) CollectVacuumLogs() bool {
	return c.collectVacuumLogs
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

func (c *CollectConf) IsRESTCollect() bool {
	return c.isRESTCollect
}

func (c *CollectConf) RestCollectDailyJobsLimit() int {
	return c.restCollectDailyJobsLimit
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

func (c *CollectConf) MinFreeSpaceGB() uint64 {
	return c.minFreeSpaceCheckGB
}

func (c *CollectConf) CollectSystemTablesTimeoutSeconds() int {
	return c.collectSystemTablesTimeoutSeconds
}

func (c *CollectConf) CollectClusterIDTimeoutSeconds() int {
	return c.collectClusterIDTimeoutSeconds
}
