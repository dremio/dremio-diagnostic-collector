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
)

// NodeCaptureStats represents stats for a node capture.
type NodeCaptureStats struct {
	startTime       int64
	endTime         int64
	status          string
	endProcessError string
	isCoordinator   bool
}

// CollectionStats represents stats for a collection.
type CollectionStats struct {
	collectionMode    string                       // shows the collectionMode used that sets defaults: light, standard, or healthcheck
	collectionArgs    string                       // collectionArgs arguments passed to ddc, useful for debugging
	ddcVersion        string                       // ddcVersion used during the collection
	logFile           string                       // logFile location of the ddc.log file
	TransfersComplete int                          // TransfersComplete shows the number of tarball transfers completed
	totalTransfers    int                          // totalTransfers shows the number of transfers of tarballs attempted
	tarball           string                       // tarball is the location of the final tarball
	nodeCaptureStats  map[string]*NodeCaptureStats // nodeCaptureStats is the map of nodes and their basic collection stats such as startTime, endTime and status
	result            string                       // result is the current result of the collection process
	startTime         int64                        // startTime in epoch seconds for the collection
	endTime           int64                        // endTime in epoch seconds for the collection
	mu                sync.RWMutex                 // mu is the mutex to protect access to various fields (nodeCaptureStats, warnings, lastK8sFileCollected, etc)
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
func UpdateRuntime(ddcVersion string, logFile string, transfersComplete, totalTransfers int) {
	c.mu.Lock()
	c.ddcVersion = ddcVersion
	c.logFile = logFile
	c.TransfersComplete = transfersComplete
	c.totalTransfers = totalTransfers
	c.mu.Unlock()
}

func UpdateTarballDir(tarballDir string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tarball = tarballDir

	// If the result already contains "COMPLETE", append the tarball information to it
	if c.result != "" && strings.Contains(c.result, "COMPLETE") && !strings.Contains(c.result, tarballDir) {
		c.result = fmt.Sprintf("%v\nTarball: %v", c.result, tarballDir)
	}
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

// c is the singleton that is the global collection
// stats that stores all the status updates used by
// the collection process.
var c *CollectionStats

func init() {
	initialize()
}

func initialize() {
	c = &CollectionStats{
		nodeCaptureStats: make(map[string]*NodeCaptureStats),
		startTime:        time.Now().Unix(),
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

type NodeState struct {
	Status          string `json:"status"`
	StatusUX        string `json:"status_ux"`
	Node            string `json:"node"`
	Result          string `json:"result"`
	EndProcess      bool   `json:"end_process"`
	EndProcessError string `json:"-"` // Use json:"-" to exclude from JSON output
	IsCoordinator   bool   `json:"-"` // Use json:"-" to exclude from JSON output
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
	if _, ok := c.nodeCaptureStats[node]; ok {
		// failures should include a clear failure output in the status text
		if result == ResultFailure {

			status = status + " - " + ResultFailure
		}
		// set the status message on the node directly so we can display it when the
		// display is updated
		c.nodeCaptureStats[node].status = status
		if nodeState.EndProcess {
			// set the end time and then increment the transfers complete counter
			// we only want to count it the first time, so we check to see if
			// the endTime has been set or not, if it has, then we do nothing.
			if c.nodeCaptureStats[node].endTime == 0 {
				if nodeState.Result != ResultFailure {
					c.TransfersComplete++
				}
				c.nodeCaptureStats[node].endProcessError = nodeState.EndProcessError
				c.nodeCaptureStats[node].endTime = time.Now().Unix()
			}
		}
	} else {
		// if the node is not present we initialize it and can
		// safely set the start time.
		c.nodeCaptureStats[node] = &NodeCaptureStats{
			startTime:     time.Now().Unix(),
			status:        status,
			isCoordinator: nodeState.IsCoordinator,
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
	// put the coordinatorKeys in a stable order so the UI update is consistent
	// and doesn't jump around. TODO move this to happening on write
	// since this function is called much more frequently

	var executorKeys []string
	var coordinatorKeys []string
	for k := range c.nodeCaptureStats {
		node := c.nodeCaptureStats[k]
		if node.isCoordinator {
			coordinatorKeys = append(coordinatorKeys, k)
		} else {
			executorKeys = append(executorKeys, k)
		}
	}
	sort.Strings(coordinatorKeys)
	var nodes strings.Builder
	// if there are any node capture status write the header
	if len(c.nodeCaptureStats) > 0 {
		nodes.WriteString("Coordinator Detail:\n-------------------\n")
	}

	// iterate through the keys using the sorted array
	for i, key := range coordinatorKeys {
		node := c.nodeCaptureStats[key]
		status := node.status
		// Only show duration for completed nodes
		if node.endTime > 0 {
			secondsElapsed := int(node.endTime) - int(node.startTime)
			// Format with duration at the end
			nodes.WriteString(fmt.Sprintf("%v. %v - %v (%v seconds)\n", i+1, key, status, secondsElapsed))
		} else {
			nodes.WriteString(fmt.Sprintf("%v. %v - %v\n", i+1, key, status))
		}
	}
	// set the default version of Unknown
	ddcVersion := "Unknown Version"
	if c.ddcVersion != "" {
		// since we have a version overwrite the default
		ddcVersion = c.ddcVersion
	}

	// Set default result to show start time if not set
	resultText := c.result
	if resultText == "" {
		startTime := time.Unix(c.startTime, 0)
		resultText = fmt.Sprintf("STARTED AT %v", startTime.Format(time.RFC1123))
	}

	// Append tarball to result if it's available and not already included
	if c.tarball != "" && !strings.Contains(resultText, c.tarball) {
		// Only append tarball to result if the collection is complete
		// This is determined by checking if the result contains "COMPLETE"
		if strings.HasPrefix(resultText, "COMPLETE AT") {
			resultText = fmt.Sprintf("%v - Tarball: %v", resultText, c.tarball)
		}
	}

	// Find failed nodes
	var failedNodes []string
	for nodeName, nodeStats := range c.nodeCaptureStats {
		if strings.Contains(nodeStats.status, ResultFailure) {
			failedNodes = append(failedNodes, nodeName)
		}
	}

	// Sort by node name
	sort.Strings(failedNodes)

	var failedNodesStr string
	errorLogs := strings.Builder{}
	for _, node := range failedNodes {
		if c.nodeCaptureStats[node] == nil {
			continue
		}
		if c.nodeCaptureStats[node] == nil {
			continue
		}
		if c.nodeCaptureStats[node].endProcessError == "" {
			continue
		}
		errorLogs.WriteString(fmt.Sprintf("Node: %v\n", node))
		errorLogs.WriteString(fmt.Sprintf("Error: %v\n", c.nodeCaptureStats[node].endProcessError))
	}
	if len(failedNodes) > 0 {
		// Just show node names for failed nodes
		failedNodesStr = strings.Join(failedNodes, ", ")
		failedNodesStr = fmt.Sprintf("%v\n\nLogs of Failed Nodes:\n---------------------\n%v", failedNodesStr, errorLogs.String())
	} else {
		failedNodesStr = "None"
	}

	_, err := fmt.Printf(
		`=================================
== Dremio Diagnostic Collector ==
=================================
%v

Version                        : %v
Collection Mode                : %v

-- status --
Transfers Complete             : %v/%v
Result                         : %v
Nodes (Coordinators/Executors) : %v/%v
Failed Nodes                   : %v

%v
`, time.Now().Format(time.RFC1123), strings.TrimSpace(ddcVersion), strings.ToUpper(c.collectionMode), c.TransfersComplete, total,
		resultText, len(coordinatorKeys), len(executorKeys), failedNodesStr, nodes.String())
	if err != nil {
		fmt.Printf("unable to write output: (%v)\n", err)
	}
	c.mu.Unlock()
}
