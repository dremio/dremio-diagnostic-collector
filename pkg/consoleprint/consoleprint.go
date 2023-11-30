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
	ddcVersion        string
	logFile           string
	ddcYaml           string
	TransfersComplete int
	totalTransfers    int
	collectionType    string
	tarball           string
	nodeCaptureStats  map[string]*NodeCaptureStats
	result            string
	mu                sync.RWMutex // Mutex to protect access
}

// Update updates the CollectionStats fields in a thread-safe manner.
func UpdateRuntime(ddcVersion, logFile, ddcYaml, collectionType string, transfersComplete, totalTransfers int) {
	c.mu.Lock()
	c.ddcVersion = ddcVersion
	c.logFile = logFile
	c.ddcYaml = ddcYaml
	c.TransfersComplete = transfersComplete
	c.totalTransfers = totalTransfers
	c.collectionType = collectionType
	c.mu.Unlock()
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

var c *CollectionStats

func init() {
	c = &CollectionStats{
		nodeCaptureStats: make(map[string]*NodeCaptureStats),
	}
}

// Update updates the CollectionStats fields in a thread-safe manner.
func UpdateNodeState(node string, status string) {
	c.mu.Lock()
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
	c.mu.Unlock()
}

func PrintState() {
	c.mu.Lock()
	fmt.Print("\033[H\033[2J")
	total := c.totalTransfers
	var keys []string
	for k := range c.nodeCaptureStats {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var nodes strings.Builder
	for i, key := range keys {
		node := c.nodeCaptureStats[key]
		var secondsElapsed int
		if node.endTime > 0 {
			secondsElapsed = int(node.endTime) - int(node.startTime)
		} else {
			secondsElapsed = int(time.Now().Unix()) - int(node.startTime)
		}
		nodes.WriteString(fmt.Sprintf("%v. elapsed %v secs - node %v - status %v \n", i+1, secondsElapsed, key, node.status))
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
Transfers Complete   : %v/%v
Tarball              : %v
Result               : %v

Collection Stats
-----------------

%v
`, time.Now().Format(time.RFC1123), strings.TrimSpace(c.ddcVersion), c.ddcYaml, c.logFile, c.collectionType, c.TransfersComplete, total,
		c.tarball, c.result, nodes.String())
	c.mu.Unlock()

}
