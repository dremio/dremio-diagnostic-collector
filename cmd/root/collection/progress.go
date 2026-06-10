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

package collection

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/simplelog"
)

// ProgressTracker tracks collection progress across nodes.
type ProgressTracker struct {
	mu         sync.Mutex
	totalNodes int
	completed  int
	failed     int
	inProgress int
	startTime  time.Time
	jsonOutput bool
	stopCh     chan struct{}
	nodeStatus map[string]string // node -> "pending"|"in_progress"|"completed"|"failed"
}

// NewProgressTracker creates a progress tracker.
func NewProgressTracker(totalNodes int, jsonOutput bool) *ProgressTracker {
	return &ProgressTracker{
		totalNodes: totalNodes,
		startTime:  time.Now(),
		jsonOutput: jsonOutput,
		stopCh:     make(chan struct{}),
		nodeStatus: make(map[string]string),
	}
}

// MarkInProgress marks a node as in-progress.
func (p *ProgressTracker) MarkInProgress(node string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.nodeStatus[node] = "in_progress"
	p.inProgress++
}

// MarkCompleted marks a node as completed.
func (p *ProgressTracker) MarkCompleted(node string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.nodeStatus[node] == "in_progress" {
		p.inProgress--
	}
	p.nodeStatus[node] = "completed"
	p.completed++
}

// MarkFailed marks a node as failed.
func (p *ProgressTracker) MarkFailed(node string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.nodeStatus[node] == "in_progress" {
		p.inProgress--
	}
	p.nodeStatus[node] = "failed"
	p.failed++
}

// Start begins periodic progress reporting at the given interval.
func (p *ProgressTracker) Start(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				p.printProgress()
			case <-p.stopCh:
				return
			}
		}
	}()
}

// Stop halts periodic progress reporting.
func (p *ProgressTracker) Stop() {
	close(p.stopCh)
}

func (p *ProgressTracker) printProgress() {
	p.mu.Lock()
	defer p.mu.Unlock()

	elapsed := time.Since(p.startTime)
	pct := 0.0
	if p.totalNodes > 0 {
		pct = float64(p.completed) / float64(p.totalNodes) * 100
	}

	var eta time.Duration
	if p.completed > 0 {
		perNode := elapsed / time.Duration(p.completed)
		remaining := p.totalNodes - p.completed - p.failed
		eta = perNode * time.Duration(remaining)
	}

	if p.jsonOutput {
		p.printJSON(elapsed, pct, eta)
	} else {
		p.printText(elapsed, pct, eta)
	}
}

type progressJSON struct {
	Completed  int     `json:"completed"`
	Total      int     `json:"total"`
	Failed     int     `json:"failed"`
	InProgress int     `json:"in_progress"`
	Percent    float64 `json:"percent"`
	ElapsedMs  int64   `json:"elapsed_ms"`
	ETAMs      int64   `json:"eta_ms"`
}

func (p *ProgressTracker) printJSON(elapsed time.Duration, pct float64, eta time.Duration) {
	j := progressJSON{
		Completed:  p.completed,
		Total:      p.totalNodes,
		Failed:     p.failed,
		InProgress: p.inProgress,
		Percent:    pct,
		ElapsedMs:  elapsed.Milliseconds(),
		ETAMs:      eta.Milliseconds(),
	}
	data, err := json.Marshal(j)
	if err == nil {
		fmt.Println(string(data))
	}
}

func (p *ProgressTracker) printText(elapsed time.Duration, pct float64, eta time.Duration) {
	msg := fmt.Sprintf("=== Collection Progress ===\nCompleted: %d/%d nodes (%.1f%%)\nFailed: %d nodes | In Progress: %d nodes\nElapsed: %s | ETA: %s",
		p.completed, p.totalNodes, pct,
		p.failed, p.inProgress,
		formatDuration(elapsed), formatDuration(eta))
	simplelog.Info(msg)
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm %ds", m, s)
}
