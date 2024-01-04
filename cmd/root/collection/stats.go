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

// collection package provides the interface for collection implementation and the actual collection execution
package collection

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// NodeCaptureStats represents stats for a node capture.
type NodeCaptureStats struct {
	node           string
	secondsElapsed int
	status         string
	mu             sync.Mutex // Mutex to protect access
}

// Update updates the NodeCaptureStats fields in a thread-safe manner.
func (n *NodeCaptureStats) Update(node string, secondsElapsed int, status string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.node = node
	n.secondsElapsed = secondsElapsed
	n.status = status
}

// Stats represents stats for a collection.
type Stats struct {
	ddcVersion        string
	logFile           string
	ddcYaml           string
	ddcYamlIsValid    bool
	TransfersComplete int
	totalTransfers    int
	collectionType    string
	nodeCaptureStats  map[string]*NodeCaptureStats
	mu                sync.Mutex // Mutex to protect access
}

// Update updates the CollectionStats fields in a thread-safe manner.
func (c *Stats) UpdateDDCVersion(ddcVersion, logFile, ddcYaml, collectionType string, ddcYamlIsValid bool, transfersComplete, totalTransfers int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ddcVersion = ddcVersion
	c.logFile = logFile
	c.ddcYaml = ddcYaml
	c.ddcYamlIsValid = ddcYamlIsValid
	c.TransfersComplete = transfersComplete
	c.totalTransfers = totalTransfers
	c.collectionType = collectionType
}

var CollectionStatsGlobal *Stats

func init() {
	CollectionStatsGlobal = &Stats{
		nodeCaptureStats: make(map[string]*NodeCaptureStats),
	}
}

// Update updates the CollectionStats fields in a thread-safe manner.
func (c *Stats) UpdateNodeState(node string, secondsElapsed int, status string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.nodeCaptureStats[node]; ok {
		c.nodeCaptureStats[node].Update(node, secondsElapsed, status)
	} else {
		c.nodeCaptureStats[node] = &NodeCaptureStats{

			node:           node,
			secondsElapsed: secondsElapsed,
			status:         status}
	}
}

func (c *Stats) PrintState() {
	fmt.Print("\033[H\033[2J")
	c.mu.Lock()
	defer c.mu.Unlock()
	ddcYamlStatus := "INVALID"
	if c.ddcYamlIsValid {
		ddcYamlStatus = "VALID"
	}
	total := c.totalTransfers
	keys := make([]string, 0, len(c.nodeCaptureStats))
	for k := range c.nodeCaptureStats {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var nodes strings.Builder
	for i, key := range keys {
		node := c.nodeCaptureStats[key]
		nodes.WriteString(fmt.Sprintf("%v. elapsed %v secs - node %v - status %v \n", i+1, key, node.secondsElapsed, node.status))
	}
	fmt.Printf(
		`
		DDC Version          : %v
		DDC Yaml             : %v
		DDC Yaml Status      : %v
		Log File             : %v
		Collection Type      : %v
		Transfers Complete   : %v/%v
		-----------------------------
		%v
		`, c.ddcVersion, c.ddcYaml, ddcYamlStatus, c.logFile, c.collectionType, c.TransfersComplete, total, nodes.String())
}
