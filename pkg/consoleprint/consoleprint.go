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
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// NodeCaptureStats represents stats for a node capture.
type NodeCaptureStats struct {
	startTime int64
	endTime   int64
	status    string
}

// CollectionStats represents stats for a collection.
type CollectionStats struct {
	collectionMode       string
	ddcVersion           string
	logFile              string
	ddcYaml              string
	TransfersComplete    int
	totalTransfers       int
	collectionType       string
	tarball              string
	nodeCaptureStats     map[string]*NodeCaptureStats
	nodeDetectDisabled   map[string]bool
	result               string
	k8sFilesCollected    []string
	lastK8sFileCollected string
	enabled              []string
	disabled             []string
	patSet               bool
	mu                   sync.RWMutex // Mutex to protect access
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

func UpdateResult(result string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.result = result
}

func UpdateCollectionMode(collectionMode string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.collectionMode = collectionMode
}

var c *CollectionStats

func init() {
	c = &CollectionStats{
		nodeCaptureStats:   make(map[string]*NodeCaptureStats),
		nodeDetectDisabled: make(map[string]bool),
	}
	if strings.HasSuffix(os.Args[0], ".test") {
		clearCode = "CLEAR SCREEN"
	}
}

// Update updates the CollectionStats fields in a thread-safe manner.
func UpdateNodeAutodetectDisabled(node string, enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nodeDetectDisabled[node] = enabled
}

// Update updates the CollectionStats fields in a thread-safe manner.
func UpdateNodeState(node string, status string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.nodeCaptureStats[node]; ok {
		c.nodeCaptureStats[node].status = status
		if status == "COMPLETED" || strings.HasPrefix(status, "FAILED") {
			if c.nodeCaptureStats[node].endTime == 0 {
				c.TransfersComplete++
				c.nodeCaptureStats[node].endTime = time.Now().Unix()
			}
		}
	} else {
		c.nodeCaptureStats[node] = &NodeCaptureStats{
			startTime: time.Now().Unix(),
			status:    status,
		}
	}
}

var clearCode = "\033[H\033[2J"

func PrintState() {
	c.mu.Lock()
	fmt.Print(clearCode)
	total := c.totalTransfers
	var keys []string
	for k := range c.nodeCaptureStats {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var nodes strings.Builder
	if c.lastK8sFileCollected != "" {
		nodes.WriteString("Kubernetes:\n-----------\n")
		nodes.WriteString(fmt.Sprintf("Last file collected   : %v\n", c.lastK8sFileCollected))
		nodes.WriteString(fmt.Sprintf("files collected       : %v\n", len(c.k8sFilesCollected)))
		nodes.WriteString("\n")
	}
	if len(c.nodeCaptureStats) > 0 {
		nodes.WriteString("Nodes:\n------\n")
	}

	for i, key := range keys {
		node := c.nodeCaptureStats[key]
		var secondsElapsed int
		if node.endTime > 0 {
			secondsElapsed = int(node.endTime) - int(node.startTime)
		} else {
			secondsElapsed = int(time.Now().Unix()) - int(node.startTime)
		}
		status := node.status
		if _, ok := c.nodeDetectDisabled[key]; ok {
			status = fmt.Sprintf("(NO PID) %v", status)
		}
		nodes.WriteString(fmt.Sprintf("%v. node %v - elapsed %v secs - status %v \n", i+1, key, secondsElapsed, status))
	}
	patMessage := ""
	if c.patSet {
		patMessage = "Yes"
	} else {
		patMessage = "No (disables Job Profiles, WLM, KV Store and System Table Reports use --dremio-pat-prompt if you want these)"
	}
	autodetectEnabled := "Yes"
	if len(c.nodeDetectDisabled) > 0 {
		autodetectEnabled = fmt.Sprintf("Disabled on %v/%v nodes files may be missing try again with the --sudo-user flag", len(c.nodeDetectDisabled), len(c.nodeCaptureStats))
	}
	fmt.Printf(
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
Dremio PAT Set       : %v
Autodetect Enabled   : %v
Collection Mode      : %v

-- status --
Transfers Complete   : %v/%v
Tarball              : %v
Result               : %v


%v
`, time.Now().Format(time.RFC1123), strings.TrimSpace(c.ddcVersion), c.ddcYaml, c.logFile, c.collectionType, strings.Join(c.enabled, ","), strings.Join(c.disabled, ","), patMessage, autodetectEnabled, strings.ToUpper(c.collectionMode), c.TransfersComplete, total,
		c.tarball, c.result, nodes.String())
	c.mu.Unlock()

}
