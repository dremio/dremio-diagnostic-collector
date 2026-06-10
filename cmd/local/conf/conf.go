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

// package conf provides configuration for the collect command
package conf

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/local/conf/autodetect"
	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/local/ddcio"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/collects"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/consoleprint"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/dirs"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/shutdown"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/simplelog"
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
	disableFreeSpaceCheck   bool
	numberThreads           int
	gcLogsDir               string
	dremioLogDir            string
	dremioConfDir           string
	dremioRocksDBDir        string
	dremioPIDDetection      bool
	collectAccelerationLogs bool
	collectAccessLogs       bool
	collectJVMFlags         bool
	captureHeapDump         bool
	// advanced variables settable by configuration or environment variable
	outputDir                  string
	tarballOutDir              string
	diagTimeSeconds            int
	dremioLogsNumDays          int
	dremioGCFilePattern        string
	dremioQueriesJSONNumDays   int
	collectJFR                 bool
	collectJStack              bool
	collectServerLogs          bool
	collectMetaRefreshLogs     bool
	collectQueriesJSON         bool
	collectDremioConfiguration bool
	collectReflectionLogs      bool
	collectVacuumLogs          bool
	collectOSConfig            bool
	collectDiskUsage           bool
	collectGCLogs              bool
	collectHSErrFiles          bool
	collectTop                 bool
	nodeName                   string
	// new v4 fields
	collectTrackerJSON       bool
	collectHiveDeprecatedLog bool
	collectAsyncProfiler     bool
	serverLogsNumDays        int
	metaRefreshLogNumDays    int
	reflectionLogNumDays     int
	trackerJSONNumDays       int
	vacuumLogNumDays         int
	diskBandwidthLimitPct    int
	startDate                time.Time

	// variables
	dremioPID int
}

func DetectRocksDB(dremioHome string, dremioConfDir string) string {
	dremioConfFile := filepath.Join(dremioConfDir, "dremio.conf")

	// Use the HOCON parser
	hoconConfig, err := ParseDremioConf(dremioConfFile, dremioHome)
	if err == nil {
		return hoconConfig.GetRocksDBPath(dremioHome)
	}

	// If parsing fails, use the default path
	simplelog.Warningf("Failed to parse dremio.conf with HOCON parser: %v", err)
	simplelog.Infof("Using default RocksDB path")
	return filepath.Join(dremioHome, "data", "db")
}

// SystemTableList returns the default system tables to collect.
// This excludes expensive tables (tables, views, jobs_recent) which must be
// explicitly opted-in via the TUI or --system-tables flag.
func SystemTableList() []string {
	return []string{
		"version",
		"options",
		"roles",
		"membership",
		"privileges",
		"reflections",
		"materializations",
		"refreshes",
		"reflection_dependencies",
	}
}

func ReadConf(hook shutdown.Hook, overrides map[string]string, collectionMode collects.CollectionMode) (*CollectConf, error) {
	confData := make(map[string]interface{})
	for k, v := range overrides {
		if v == "\"\"" {
			confData[k] = ""
		} else {
			confData[k] = v
		}
	}
	simplelog.Debugf("logging parsed configuration from overrides")
	defaultCaptureSeconds := 60
	// set node name
	hostName, err := os.Hostname()
	if err != nil {
		hostName = fmt.Sprintf("unknown-%v", uuid.New())
	}

	SetViperDefaults(confData, hostName, defaultCaptureSeconds, collectionMode)
	c := &CollectConf{}
	for k, v := range confData {
		if k == KeyDremioPatToken {
			simplelog.Debugf("conf key '%v':'REDACTED'", k)
		} else {
			simplelog.Debugf("conf key '%v':'%v'", k, v)
		}
	}
	c.dremioPIDDetection = GetBool(confData, KeyDremioPidDetection)
	c.collectAccelerationLogs = GetBool(confData, KeyCollectAccelerationLog)
	c.collectAccessLogs = GetBool(confData, KeyCollectAccessLog)
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
			return &CollectConf{}, fmt.Errorf("failed checking tarball out dir: %w", err)
		}
	}
	dirEntries, err := os.ReadDir(c.tarballOutDir)
	if err != nil {
		return &CollectConf{}, err
	}
	var entryNames []string
	var entryCount int
	allowedList := []string{"ddc", "ddc.log", fmt.Sprintf("%v.tar.gz", c.nodeName)}
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
	c.dremioQueriesJSONNumDays = GetInt(confData, KeyQueriesJSONNumDays)
	c.collectQueriesJSON = GetBool(confData, KeyCollectQueriesJSON)
	c.collectServerLogs = GetBool(confData, KeyCollectServerLogs)
	c.collectMetaRefreshLogs = GetBool(confData, KeyCollectMetaRefreshLog)
	c.collectReflectionLogs = GetBool(confData, KeyCollectReflectionLog)
	c.collectVacuumLogs = GetBool(confData, KeyCollectVacuumLog)
	c.collectGCLogs = GetBool(confData, KeyCollectGCLogs)
	c.collectHSErrFiles = GetBool(confData, KeyCollectHSErrFiles)
	c.disableFreeSpaceCheck = GetBool(confData, KeyDisableFreeSpaceCheck)
	c.collectDremioConfiguration = GetBool(confData, KeyCollectDremioConfiguration)

	// v4 new fields
	c.collectTrackerJSON = GetBool(confData, KeyCollectTrackerJSON)
	c.collectHiveDeprecatedLog = GetBool(confData, KeyCollectHiveDeprecatedLog)
	c.collectAsyncProfiler = GetBool(confData, KeyCollectAsyncProfiler)
	c.serverLogsNumDays = GetInt(confData, KeyServerLogsNumDays)
	c.metaRefreshLogNumDays = GetInt(confData, KeyMetaRefreshLogNumDays)
	c.reflectionLogNumDays = GetInt(confData, KeyReflectionLogNumDays)
	c.trackerJSONNumDays = GetInt(confData, KeyTrackerJSONNumDays)
	c.vacuumLogNumDays = GetInt(confData, KeyVacuumLogNumDays)
	c.diskBandwidthLimitPct = GetInt(confData, KeyDiskBandwidthLimitPct)

	// date-range filtering
	if startDateStr := GetString(confData, KeyStartDate); startDateStr != "" {
		t, err := parseDateString(startDateStr)
		if err != nil {
			return &CollectConf{}, fmt.Errorf("invalid %v value %q: %w", KeyStartDate, startDateStr, err)
		}
		c.startDate = t
	}

	// system diag
	c.collectOSConfig = GetBool(confData, KeyCollectOSConfig)
	c.collectDiskUsage = GetBool(confData, KeyCollectDiskUsage)
	c.collectJVMFlags = GetBool(confData, KeyCollectJVMFlags)

	// diagnostic tools timing
	c.diagTimeSeconds = GetInt(confData, KeyDiagTimeSeconds)

	// top
	c.collectTop = GetBool(confData, KeyCollectTop)

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
			msg := fmt.Sprintf("GC LOG DETECTION DISABLED: will rely on autodetection fallback as ddc is unable to retrieve configuration from pid %v: %v", c.dremioPID, err)
			consoleprint.ErrorPrint(msg)
			simplelog.Error(msg)
			c.dremioGCFilePattern = GetString(confData, KeyDremioGCFilePattern)
		} else {
			c.gcLogsDir = logDir
			c.dremioGCFilePattern = gcLogPattern
		}
	} else {
		c.dremioGCFilePattern = GetString(confData, KeyDremioGCFilePattern)
	}
	// Fallback: if GC log dir is still empty, scan common paths
	if c.gcLogsDir == "" {
		fallbackDirs := []string{"/var/log/dremio", "/opt/dremio/log", "/opt/dremio/data/log", "/opt/dremio/data/gclog"}
		pattern, dir := autodetect.FindGCLogsFallback(c.dremioLogDir, fallbackDirs, c.dremioLogsNumDays)
		if dir != "" {
			c.gcLogsDir = dir
			c.dremioGCFilePattern = pattern
			simplelog.Infof("GC log fallback detected directory: %v with pattern: %v", dir, pattern)
		}
	}
	// captures that wont work if the dremioPID is invalid
	c.captureHeapDump = GetBool(confData, KeyCaptureHeapDump) && dremioPIDIsValid
	c.collectJFR = GetBool(confData, KeyCollectJFR) && dremioPIDIsValid
	c.collectJStack = GetBool(confData, KeyCollectJStack) && dremioPIDIsValid

	{
		var detectedConfig DremioConfig
		capturesATypeOfLog := c.collectServerLogs || c.collectAccelerationLogs || c.collectAccessLogs || c.collectMetaRefreshLogs || c.collectReflectionLogs || c.collectQueriesJSON
		// because so few people would change the config to skip log capture when they didn't want it we have added this flag
		if capturesATypeOfLog {
			// enable some autodetected directories
			if dremioPIDIsValid {
				var err error
				detectedConfig, err = GetConfiguredDremioValuesFromPID(hook, c.dremioPID)
				if err != nil {
					msg := fmt.Sprintf("AUTODETECTION DISABLED: will rely on CLI flag configuration as ddc is unable to retrieve configuration from pid %v: %v", c.dremioPID, err)
					consoleprint.ErrorPrint(msg)
					simplelog.Error(msg)
				} else {
					simplelog.Infof("configured values retrieved from ps output: %v:%v, %v:%v", KeyDremioLogDir, detectedConfig.LogDir, KeyCollectDremioConfiguration, detectedConfig.ConfDir)
					c.dremioLogDir = detectedConfig.LogDir
					c.dremioConfDir = detectedConfig.ConfDir
				}
			} else {
				consoleprint.ErrorPrint("AUTODETECTION DISABLED: will rely on CLI flag configuration as the ddc user does not have permissions to the dremio process consider using --sudo-user to resolve this")
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
			simplelog.Debugf("configured log dir is: %v, detected log dir is: %v", configuredLogDir, detectedConfig.LogDir)
			// see if the configured dir is valid
			if err := dirs.CheckDirectory(configuredLogDir, containsValidLog); err != nil {
				msg := fmt.Sprintf("the configured log directory %v is not usable (%v), therefore we are using autodetected value of %v for log collection", configuredLogDir, err, detectedConfig.LogDir)
				consoleprint.ErrorPrint(msg)
				simplelog.Warning(msg)
			} else {
				// if the configured directory is valid ALWAYS pick that
				simplelog.Infof("using configured log directory for log collection: %v", configuredLogDir)
				c.dremioLogDir = configuredLogDir
			}
			if err := dirs.CheckDirectory(c.dremioLogDir, containsValidLog); err != nil {
				return &CollectConf{}, fmt.Errorf("invalid dremio log dir '%v', set --dremio-log-dir flag: %w", c.dremioLogDir, err)
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
				msg := fmt.Sprintf("the configured configuration directory %v is not usable (%v), therefore we are using autodetected value of %v for configuration collection", configuredConfDir, err, detectedConfig.ConfDir)
				consoleprint.WarningPrint(msg)
				simplelog.Warning(msg)
			} else {
				// if the configured directory is valid ALWAYS pick that
				simplelog.Infof("using configured configuration directory for configuration collection: %v", configuredConfDir)
				c.dremioConfDir = configuredConfDir
			}
			if err := dirs.CheckDirectory(c.dremioConfDir, func(de []fs.DirEntry) error {
				if len(de) > 0 {
					return nil
				} else {
					return errors.New("configuration directory is empty")
				}
			}); err != nil {
				return &CollectConf{}, fmt.Errorf("invalid dremio conf dir '%v', update --dremio-conf-dir flag and fix it: %w", c.dremioConfDir, err)
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
			consoleprint.WarningPrint(msg)
			simplelog.Warning(msg)
			// detected value
			c.dremioRocksDBDir = DetectRocksDB(detectedConfig.Home, c.dremioConfDir)
		} else {
			c.dremioRocksDBDir = configuredRocksDb
		}
		simplelog.Infof("using rocks db dir %v", c.dremioRocksDBDir)
		if err := dirs.CheckDirectory(c.dremioRocksDBDir, validateRocks); err != nil {
			simplelog.Warningf("only applies to coordinators - invalid rocksdb dir '%v', update --dremio-rocksdb-dir flag and fix it: %v", c.dremioConfDir, err)
		}

	}

	return c, nil
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

// ReadPSEnvFunc is a function type for reading process environment
type ReadPSEnvFunc func(hook shutdown.CancelHook, dremioPID int) (string, error)

// DefaultReadPSEnv is the default implementation for reading process environment
var DefaultReadPSEnv ReadPSEnvFunc = func(hook shutdown.CancelHook, dremioPID int) (string, error) {
	var w bytes.Buffer
	// grep -v /etc/dremio/preview filters out the preview engine
	err := ddcio.Shell(hook, &w, fmt.Sprintf("ps eww %v | grep dremio | grep -v /etc/dremio/preview | awk '{$1=$2=$3=$4=\"\"; print $0}'", dremioPID))
	if err != nil {
		return "", err
	}
	return w.String(), nil
}

// ReadPSEnv reads the process environment for a given PID
func ReadPSEnv(hook shutdown.CancelHook, dremioPID int) (string, error) {
	return DefaultReadPSEnv(hook, dremioPID)
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

func (c *CollectConf) CollectAccessLogs() bool {
	return c.collectAccessLogs
}

func (c *CollectConf) CollectJVMFlags() bool {
	return c.collectJVMFlags
}

func (c *CollectConf) TopOutDir() string {
	return filepath.Join(c.outputDir, "top")
}

func (c *CollectConf) HeapDumpsOutDir() string { return filepath.Join(c.outputDir, "heap-dumps") }

func (c *CollectConf) KubernetesOutDir() string { return filepath.Join(c.outputDir, "kubernetes") }

func (c *CollectConf) ClusterStatsOutDir() string {
	return filepath.Join(c.outputDir, "cluster-stats", c.nodeName)
}

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

func (c *CollectConf) DremioPID() int {
	return c.dremioPID
}

func (c *CollectConf) DremioPIDDetection() bool {
	return c.dremioPIDDetection
}

func (c *CollectConf) DremioConfDir() string {
	return c.dremioConfDir
}

func (c *CollectConf) DiagTimeSeconds() int {
	return c.diagTimeSeconds
}

func (c *CollectConf) CollectTop() bool {
	return c.collectTop
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

func (c *CollectConf) DremioRocksDBDir() string {
	return c.dremioRocksDBDir
}

func (c *CollectConf) CollectTrackerJSON() bool {
	return c.collectTrackerJSON
}

func (c *CollectConf) CollectHiveDeprecatedLog() bool {
	return c.collectHiveDeprecatedLog
}

func (c *CollectConf) CollectAsyncProfiler() bool {
	return c.collectAsyncProfiler
}

// DiskBandwidthLimitPct returns the disk bandwidth limit percentage for standard mode.
func (c *CollectConf) DiskBandwidthLimitPct() int {
	return c.diskBandwidthLimitPct
}

func (c *CollectConf) ServerLogsNumDays() int {
	return c.serverLogsNumDays
}

func (c *CollectConf) MetaRefreshLogNumDays() int {
	return c.metaRefreshLogNumDays
}

func (c *CollectConf) ReflectionLogNumDays() int {
	return c.reflectionLogNumDays
}

func (c *CollectConf) TrackerJSONNumDays() int {
	return c.trackerJSONNumDays
}

func (c *CollectConf) VacuumLogNumDays() int {
	return c.vacuumLogNumDays
}

// StartDate returns the configured start date for date-range filtering.
func (c *CollectConf) StartDate() time.Time {
	return c.startDate
}

func parseDateString(s string) (time.Time, error) {
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("unsupported date format, expected 2006-01-02")
}
