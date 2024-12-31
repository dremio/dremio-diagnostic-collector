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

// package consoleprint contains the logic to update the console UI
package consoleprint

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/strutils"
)

// NodeCaptureStats represents stats for a node capture.
type NodeCaptureStats struct {
	startTime int64
	endTime   int64
	status    string
}

// CollectionStats represents stats for a collection.
type CollectionStats struct {
	collectionMode       string                       // shows the collectionMode used that sets defaults: light, standard, or healthcheck
	collectionArgs       string                       // collectionArgs arguments passed to ddc, useful for debugging
	ddcVersion           string                       // ddcVersion used during the collection
	logFile              string                       // logFile location of the ddc.log file
	ddcYaml              string                       // ddcYaml location of the ddc.yaml file
	TransfersComplete    int                          // TransfersComplete shows the number of tarball transfers completed
	totalTransfers       int                          // totalTransfers shows the number of transfers of tarballs attempted
	collectionType       string                       // collectionType shows the type of transfer used to collect tarballs: kubectl, ssh, or kubernetes api
	k8sContext           string                       // k8sContexst is the kubernetes context used during capture, this is to help debug when the incorrect context was used
	tarball              string                       // tarball is the location of the final tarball
	nodeCaptureStats     map[string]*NodeCaptureStats // nodeCaptureStats is the map of nodes and their basic collection stats such as startTime, endTime and status
	nodeDetectDisabled   map[string]bool              // nodeDetectDisabled shows the nodes where node configuration detection failed and the only the ddc.yaml or defaults are used for finding logs and configuration
	result               string                       // result is the current result of the collection process
	k8sFilesCollected    []string                     // k8sFileCollected is the list of files collected during the kubernetes file collection step
	lastK8sFileCollected string                       // lastK8sFileCollected collected during the kubernetes configuration file and log collection
	enabled              []string                     // enabled shows all the collection steps enabled usually via defaults, ddc.yaml or preconditions being present
	disabled             []string                     // disabled shows all the collection steps disabled via ddc.yaml or missing preconditions
	patSet               bool                         // patSet indicates if the pat is set or not
	startTime            int64                        // startTime in epoch seconds for the collection
	endTime              int64                        // endTime in epoch seconds for the collection
	warnings             []string                     // warnings encountered during the collection
	mu                   sync.RWMutex                 // mu is the mutex to protect access to various fields (nodeCaptureStats, warnings, lastK8sFileCollected, etc)
}

// GetCollectionType generates a friendly message useful for indicating
// what mechanism is used to transfer tarballs
func (c *CollectionStats) GetCollectionType() string {
	if c.k8sContext == "" {
		// if no k8s context is used then just pass the collection type
		return c.collectionType
	}
	// include the kubernetes contexts for debugging
	return fmt.Sprintf("%v - context used: %v", c.collectionType, c.k8sContext)
}

var statusOut bool

// EnableStatusOutput enables the DDC json output
// that enables communication between DDC and the Dremio UI
func EnableStatusOutput() {
	statusOut = true
}

// ErrorOut is for error messages so that the
// Dremio UI can display it.
type ErrorOut struct {
	Error string `json:"error"`
}

// WarnOut is for warning messages so that the
// Dremio UI can display it. They should
// be interesting but not fatal
type WarnOut struct {
	Warning string `json:"warning"`
}

// WarningPrint will either output either in json or pure text
// depending on if statusOut is enabled or not
// these should be interesting but not fatal
func WarningPrint(msg string) {
	if statusOut {
		b, err := json.Marshal(WarnOut{Warning: msg})
		if err != nil {
			// output a nested error if unable to marshal
			fmt.Printf("{\"warning\": \"%q\", \"nested\": \"%q\"}\n", err, msg)
			return
		}
		fmt.Println(string(b))
	} else {
		fmt.Println(msg)
	}
}

// ErrorPrint will either output either in json or pure text
// depending on if statusOut is enabled or not
func ErrorPrint(msg string) {
	if statusOut {
		b, err := json.Marshal(ErrorOut{Error: msg})
		if err != nil {
			fmt.Printf("{\"error\": \"%q\", \"nested\": \"%q\"}\n", err, msg)
			return
		}
		fmt.Println(string(b))
	} else {
		fmt.Println(msg)
	}
}

// Update updates the CollectionStats fields in a thread-safe manner.
func UpdateRuntime(ddcVersion, logFile, ddcYaml, collectionType string, enabled []string, disabled []string, patSet bool, transfersComplete, totalTransfers int) {
	c.mu.Lock()
	c.ddcVersion = ddcVersion
	c.logFile = logFile
	c.ddcYaml = ddcYaml
	c.TransfersComplete = transfersComplete
	c.totalTransfers = totalTransfers
	c.collectionType = collectionType
	sort.Strings(enabled)
	c.enabled = enabled
	sort.Strings(disabled)
	c.disabled = disabled
	c.patSet = patSet
	c.mu.Unlock()
}

func UpdateK8sFiles(fileName string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.k8sFilesCollected = append(c.k8sFilesCollected, fileName)
	c.lastK8sFileCollected = fileName
}

func UpdateTarballDir(tarballDir string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tarball = tarballDir
}

// StatusUpdate struct for json communication
// with the Dremio UI, this is how the Dremio UI
// picks up status changes for the collection
type StatusUpdate struct {
	Result string `json:"result"` // Result shows the current result of the process, this can change over time
}

// UpdateResult either outputs a json text for
// the Dremio UI indicating the result status has been updated
// or just stores the result and updates the end time for processing later
func UpdateResult(result string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if statusOut {
		b, err := json.Marshal(StatusUpdate{Result: result})
		if err != nil {
			fmt.Printf("{\"error\": \"%q\", \"nested\": \"%q\"}\n", err, result)
			return
		}
		fmt.Println(string(b))
	}
	c.result = result
	c.endTime = time.Now().Unix()
}

func UpdateCollectionArgs(collectionArgs string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.collectionArgs = collectionArgs
}

func UpdateCollectionMode(collectionMode string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.collectionMode = collectionMode
}

func UpdateK8SContext(k8sContext string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.k8sContext = k8sContext
}

// AddWarningToConsole adds a trimed string to the list of warnings
// lines after the first line are also trimmed
func AddWarningToConsole(warning string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	tokens := strings.Split(warning, "\n")
	trimmed := tokens[0]
	c.warnings = append(c.warnings, strutils.TruncateString(trimmed, 120))
}

// c is the singleton that is the global collection
// stats that stores all the status updates used by
// the collection process.
var c *CollectionStats

func init() {
	initialize()
}

func initialize() {
	c = &CollectionStats{
		nodeCaptureStats:   make(map[string]*NodeCaptureStats),
		nodeDetectDisabled: make(map[string]bool),
		startTime:          time.Now().Unix(),
	}
	if strings.HasSuffix(os.Args[0], ".test") {
		clearCode = "CLEAR SCREEN"
	}
}

// Clear resets the UI entirely, this is really
// only useful for debugging and testing
func Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	initialize()
}

// Update updates the CollectionStats fields in a thread-safe manner.
func UpdateNodeAutodetectDisabled(node string, enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nodeDetectDisabled[node] = enabled
}

type NodeState struct {
	Status     string `json:"status"`
	StatusUX   string `json:"status_ux"`
	Node       string `json:"node"`
	Message    string `json:"message"`
	Result     string `json:"result"`
	EndProcess bool   `json:"end_process"`
}

const (
	ResultPending = "PENDING"
	ResultFailure = "FAILURE"
)

// this is the list of different collection steps that are also communicated
// back to the Dremio UI, changing this involves a code change in Dremio
// as well.
const (
	Starting                   = "STARTING"
	CreatingRemoteDir          = "CREATING_REMOTE_DIR"
	CopyDDCToHost              = "COPY_DDC_TO_HOST"
	SettingDDCPermissions      = "SETTING_DDC_PERMISSIONS"
	CopyDDCYaml                = "COPY_DDC_YAML"
	Collecting                 = "COLLECTING"
	CollectingAwaitingTransfer = "COLLECTING_AWAITING_TRANSFER"
	TarballTransfer            = "TARBALL_TRANSFER"
	Completed                  = "COMPLETED"
	DiskUsage                  = "DISK_USAGE"
	DremioConfig               = "DREMIO_CONFIG"
	GcLog                      = "GC_LOG"
	Jfr                        = "JFR"
	Jstack                     = "JSTACK"
	JVMFlags                   = "JVM_FLAGS"
	MetadataLog                = "METADATA_LOG"
	OSConfig                   = "OS_CONFIG"
	Queries                    = "QUERIES"
	ReflectionLog              = "REFLECTION_LOG"
	ServerLog                  = "SERVER_LOG"
	Ttop                       = "TTOP"
	AccelerationLog            = "ACCELERATION_LOG"
	AccessLog                  = "ACCESS_LOG"
	AuditLog                   = "AUDIT_LOG"
	JobProfiles                = "JOB_PROFILES"
	KVStore                    = "KV_STORE"
	SystemTable                = "SYSTEM_TABLE"
	Wlm                        = "WLM"
	HeapDump                   = "HEAP_DUMP"
)

// Update updates the CollectionStats fields in a thread-safe manner.
func UpdateNodeState(nodeState NodeState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if statusOut {
		b, err := json.Marshal(nodeState)
		if err != nil {
			fmt.Printf("{\"error\": \"%v\"}\n", strconv.Quote(err.Error()))
		} else {
			fmt.Println(string(b))
		}
	}
	node := nodeState.Node
	status := nodeState.StatusUX
	result := nodeState.Result
	message := nodeState.Message
	if _, ok := c.nodeCaptureStats[node]; ok {
		var statusText string
		if message != "" {
			// add the message if present
			statusText = fmt.Sprintf("(%v) %v", status, message)
		} else {
			// if there is no message just the status
			statusText = status
		}
		// failures should include a clear failure output in the status text
		if result == ResultFailure {
			statusText = ResultFailure + " - " + statusText
		}
		// set the status message on the node directly so we can display it when the
		// display is updated
		c.nodeCaptureStats[node].status = statusText
		if nodeState.EndProcess {
			// set the end time and then increment the transfers complete counter
			// we only want to count it the first time, so we check to see if
			// the endTime has been set or not, if it has, then we do nothing.
			if c.nodeCaptureStats[node].endTime == 0 {
				c.TransfersComplete++
				c.nodeCaptureStats[node].endTime = time.Now().Unix()
			}
		}
	} else {
		// if the node is not present we initialize it and can
		// safely set the start time.
		c.nodeCaptureStats[node] = &NodeCaptureStats{
			startTime: time.Now().Unix(),
			status:    status,
		}
	}
}

// clearCode is the terminal code to clear the screen
var clearCode = "\033[H\033[2J"

// PrintState clears the screen to prevent stale state, then prints out
// all of the current stats of the collection. Ideally this is executed quickly
// so we will want to avoid too many calculations in this method.
// This could be optimized for some future use case with a lot of executors and coordinators
func PrintState() {
	c.mu.Lock()
	// clear the screen
	fmt.Print(clearCode)
	total := c.totalTransfers
	// put the keys in a stable order so the UI update is consistent
	// and doesn't jump around. TODO move this to happening on write
	// since this function is called much more frequently
	var keys []string
	for k := range c.nodeCaptureStats {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var nodes strings.Builder
	// once we have started collection of kubernetes files we should start updating the ui
	// with the stats from this collection. This at once shows people when these files are successfully
	// collected such as when ddc has the rights to do so. We don't want to surprise people, it
	// should be obvious as possible what we are collecting.
	if c.lastK8sFileCollected != "" {
		nodes.WriteString("Kubernetes:\n-----------\n")
		nodes.WriteString(fmt.Sprintf("Last file collected   : %v\n", c.lastK8sFileCollected))
		nodes.WriteString(fmt.Sprintf("files collected       : %v\n", len(c.k8sFilesCollected)))
		nodes.WriteString("\n")
	}
	// if there are any node capture status write the header
	if len(c.nodeCaptureStats) > 0 {
		nodes.WriteString("Nodes:\n------\n")
	}

	// iterate through the keys using the sorted array
	for i, key := range keys {
		node := c.nodeCaptureStats[key]
		var secondsElapsed int
		if node.endTime > 0 {
			// if not finished output duration. TODO considering saving this on write.
			secondsElapsed = int(node.endTime) - int(node.startTime)
		} else {
			// if not finished calculated time elasped.
			secondsElapsed = int(time.Now().Unix()) - int(node.startTime)
		}
		status := node.status
		if _, ok := c.nodeDetectDisabled[key]; ok {
			status = fmt.Sprintf("(NO PID) %v", status)
		}
		nodes.WriteString(fmt.Sprintf("%v. node %v - elapsed %v secs - status %v \n", i+1, key, secondsElapsed, status))
	}
	var patMessage string
	if c.patSet {
		patMessage = "Yes"
	} else {
		patMessage = "No (disables Job Profiles, WLM, KV Store and System Table Reports use --collect health-check if you want these)"
	}
	autodetectEnabled := "Yes"
	if len(c.nodeDetectDisabled) > 0 {
		autodetectEnabled = fmt.Sprintf("Disabled on %v/%v nodes files may be missing try again with the --sudo-user flag", len(c.nodeDetectDisabled), len(c.nodeCaptureStats))
	}
	// write out duration elapsed to provide a sense of time passing in the UI
	durationElapsed := time.Now().Unix() - c.startTime
	// set the default version of Unknown
	ddcVersion := "Unknown Version"
	if c.ddcVersion != "" {
		// since we have a version overwrite the default
		ddcVersion = c.ddcVersion
	}
	var warningsBuilder strings.Builder
	// write out all the warnings as a numbered list
	for i, w := range c.warnings {
		_, err := warningsBuilder.WriteString(fmt.Sprintf("%v. %v\n", i+1, w))
		if err != nil {
			fmt.Printf("unable to write string %v: (%v)", w, err)
		}
	}
	_, err := fmt.Printf(
		`=================================
== Dremio Diagnostic Collector ==
=================================
%v

Version              : %v
Yaml                 : %v
Log File             : %v
Collection Type      : %v
Collections Enabled  : %v
Collections Disabled : %v
Collection Mode      : %v
Collection Args      : %v
Dremio PAT Set       : %v
Autodetect Enabled   : %v

-- status --
Transfers Complete   : %v/%v
Collect Duration     : elapsed %v seconds
Tarball              : %v
Result               : %v

-- Warnings --
%v


%v
`, time.Now().Format(time.RFC1123), strings.TrimSpace(ddcVersion), c.ddcYaml, c.logFile, c.GetCollectionType(), strings.Join(c.enabled, ","), strings.Join(c.disabled, ","), strings.ToUpper(c.collectionMode), c.collectionArgs, patMessage, autodetectEnabled, c.TransfersComplete, total,
		durationElapsed, c.tarball, c.result, warningsBuilder.String(), nodes.String())
	if err != nil {
		fmt.Printf("unable to write output: (%v)\n", err)
	}
	c.mu.Unlock()
}
