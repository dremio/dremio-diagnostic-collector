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

package consoleprint

import (
	"strings"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/output"
)

// TestCoordinatorExecutorCategorization verifies that coordinator nodes
// appear under the "coordinator nodes" section and executor nodes appear
// under the "executor nodes" section in the terminal status UI.
func TestCoordinatorExecutorCategorization(t *testing.T) {
	// Reset global state.
	Clear()

	// Register two coordinator nodes.
	UpdateNodeState(NodeState{
		Node:          "coord-1",
		StatusUX:      Collecting,
		IsCoordinator: true,
	})
	UpdateNodeState(NodeState{
		Node:          "coord-2",
		StatusUX:      Collecting,
		IsCoordinator: true,
	})

	// Register two executor nodes.
	UpdateNodeState(NodeState{
		Node:          "exec-1",
		StatusUX:      Collecting,
		IsCoordinator: false,
	})
	UpdateNodeState(NodeState{
		Node:          "exec-2",
		StatusUX:      Collecting,
		IsCoordinator: false,
	})

	// Enable terminal rendering so PrintState uses the full path
	// (not the non-interactive fallback).
	origTerminal := isTerminal
	isTerminal = true
	defer func() { isTerminal = origTerminal }()

	out, err := output.CaptureOutput(func() {
		PrintState()
	})
	if err != nil {
		t.Fatalf("CaptureOutput failed: %v", err)
	}

	// Locate section boundaries.
	coordIdx := strings.Index(out, "coordinator nodes")
	execIdx := strings.Index(out, "executor nodes")

	if coordIdx < 0 {
		t.Fatalf("output missing 'coordinator nodes' section:\n%s", out)
	}
	if execIdx < 0 {
		t.Fatalf("output missing 'executor nodes' section:\n%s", out)
	}
	if coordIdx >= execIdx {
		t.Fatalf("'coordinator nodes' section should appear before 'executor nodes' section (coord@%d, exec@%d)", coordIdx, execIdx)
	}

	// Extract section text for targeted assertions.
	coordSection := out[coordIdx:execIdx]
	execSection := out[execIdx:]

	// Coordinators appear in coordinator section.
	for _, name := range []string{"coord-1", "coord-2"} {
		if !strings.Contains(coordSection, name) {
			t.Errorf("coordinator %q not found in coordinator section", name)
		}
		if strings.Contains(execSection, name) {
			t.Errorf("coordinator %q incorrectly found in executor section", name)
		}
	}

	// Executors appear in executor section.
	for _, name := range []string{"exec-1", "exec-2"} {
		if !strings.Contains(execSection, name) {
			t.Errorf("executor %q not found in executor section", name)
		}
		if strings.Contains(coordSection, name) {
			t.Errorf("executor %q incorrectly found in coordinator section", name)
		}
	}
}

// TestThreadStatusRendering verifies that the Threads and Queued rows
// appear in the TUI output with the correct values when maxThreads > 0.
func TestThreadStatusRendering(t *testing.T) {
	Clear()

	// Set thread status: 3 active out of 5, 2 queued.
	UpdateThreadStatus(3, 5, 2)

	origTerminal := isTerminal
	isTerminal = true
	defer func() { isTerminal = origTerminal }()

	out, err := output.CaptureOutput(func() {
		PrintState()
	})
	if err != nil {
		t.Fatalf("CaptureOutput failed: %v", err)
	}

	// Threads row should show "3/5".
	if !strings.Contains(out, "Threads") {
		t.Errorf("output missing 'Threads' label:\n%s", out)
	}
	if !strings.Contains(out, "3/5") {
		t.Errorf("output missing '3/5' thread count:\n%s", out)
	}

	// Queued row should show "2".
	if !strings.Contains(out, "Queued") {
		t.Errorf("output missing 'Queued' label:\n%s", out)
	}
	// Check for the queued value "2" appearing after the Queued label.
	queuedIdx := strings.Index(out, "Queued")
	if queuedIdx >= 0 {
		after := out[queuedIdx:]
		if !strings.Contains(after, "2") {
			t.Errorf("output missing queued count '2' after Queued label:\n%s", out)
		}
	}
}

// TestThreadStatusHiddenWhenZeroMax verifies that Threads and Queued rows
// are NOT rendered when maxThreads is 0 (no collection in progress).
func TestThreadStatusHiddenWhenZeroMax(t *testing.T) {
	Clear()

	// Don't call UpdateThreadStatus — maxThreads stays 0.

	origTerminal := isTerminal
	isTerminal = true
	defer func() { isTerminal = origTerminal }()

	out, err := output.CaptureOutput(func() {
		PrintState()
	})
	if err != nil {
		t.Fatalf("CaptureOutput failed: %v", err)
	}

	if strings.Contains(out, "Threads") {
		t.Errorf("output should not contain 'Threads' when maxThreads is 0:\n%s", out)
	}
	if strings.Contains(out, "Queued") {
		t.Errorf("output should not contain 'Queued' when maxThreads is 0:\n%s", out)
	}
}

// TestToolErrorAccumulation verifies that ToolErrors from multiple
// UpdateNodeState calls accumulate on the node and are joined into
// endProcessError when EndProcess is set without an explicit error.
func TestToolErrorAccumulation(t *testing.T) {
	Clear()

	// First call: register the node with one tool error.
	UpdateNodeState(NodeState{
		Node:       "node-1",
		StatusUX:   Collecting,
		ToolErrors: []string{"jstack: permission denied"},
	})

	// Second call: add another tool error.
	UpdateNodeState(NodeState{
		Node:       "node-1",
		StatusUX:   Streaming,
		ToolErrors: []string{"jfr: timeout after 30s"},
	})

	// Third call: end process with failure, no explicit EndProcessError.
	UpdateNodeState(NodeState{
		Node:       "node-1",
		StatusUX:   Completed,
		Result:     ResultFailure,
		EndProcess: true,
	})

	c.mu.RLock()
	stats := c.nodeCaptureStats["node-1"]
	toolErrs := stats.toolErrors
	endErr := stats.endProcessError
	c.mu.RUnlock()

	// Should have both tool errors accumulated.
	if len(toolErrs) != 2 {
		t.Fatalf("expected 2 tool errors, got %d: %v", len(toolErrs), toolErrs)
	}
	if toolErrs[0] != "jstack: permission denied" {
		t.Errorf("toolErrors[0] = %q, want %q", toolErrs[0], "jstack: permission denied")
	}
	if toolErrs[1] != "jfr: timeout after 30s" {
		t.Errorf("toolErrors[1] = %q, want %q", toolErrs[1], "jfr: timeout after 30s")
	}

	// endProcessError should be the joined tool errors.
	want := "jstack: permission denied; jfr: timeout after 30s"
	if endErr != want {
		t.Errorf("endProcessError = %q, want %q", endErr, want)
	}
}

// TestToolErrorExplicitEndProcessErrorTakesPrecedence verifies that when
// EndProcessError is explicitly set, it takes precedence over accumulated tool errors.
func TestToolErrorExplicitEndProcessErrorTakesPrecedence(t *testing.T) {
	Clear()

	UpdateNodeState(NodeState{
		Node:       "node-1",
		StatusUX:   Collecting,
		ToolErrors: []string{"jstack: permission denied"},
	})

	UpdateNodeState(NodeState{
		Node:            "node-1",
		StatusUX:        Completed,
		Result:          ResultFailure,
		EndProcess:      true,
		EndProcessError: "explicit fatal error",
	})

	c.mu.RLock()
	endErr := c.nodeCaptureStats["node-1"].endProcessError
	c.mu.RUnlock()

	if endErr != "explicit fatal error" {
		t.Errorf("endProcessError = %q, want %q", endErr, "explicit fatal error")
	}
}

// TestFailedNodesAfterQueued verifies that the "Failed Nodes" row appears
// after the "Queued" row in the rendered TUI output.
func TestFailedNodesAfterQueued(t *testing.T) {
	Clear()

	// Set thread status so Queued row is rendered.
	UpdateThreadStatus(2, 4, 1)

	// Register a failed node so Failed Nodes row shows content.
	UpdateNodeState(NodeState{
		Node:       "fail-node",
		StatusUX:   Completed,
		Result:     ResultFailure,
		EndProcess: true,
	})

	origTerminal := isTerminal
	isTerminal = true
	defer func() { isTerminal = origTerminal }()

	out, err := output.CaptureOutput(func() {
		PrintState()
	})
	if err != nil {
		t.Fatalf("CaptureOutput failed: %v", err)
	}

	queuedIdx := strings.Index(out, "Queued")
	failedIdx := strings.Index(out, "Failed Nodes")

	if queuedIdx < 0 {
		t.Fatalf("output missing 'Queued' row:\n%s", out)
	}
	if failedIdx < 0 {
		t.Fatalf("output missing 'Failed Nodes' row:\n%s", out)
	}
	if failedIdx <= queuedIdx {
		t.Errorf("'Failed Nodes' (idx=%d) should appear after 'Queued' (idx=%d)", failedIdx, queuedIdx)
	}
}

// TestToolErrorSectionRendersAccumulatedErrors verifies that the "failed node errors"
// section renders each accumulated tool error on its own line.
func TestToolErrorSectionRendersAccumulatedErrors(t *testing.T) {
	Clear()

	// Register a failed node with tool errors.
	UpdateNodeState(NodeState{
		Node:       "err-node",
		StatusUX:   Collecting,
		ToolErrors: []string{"jstack: permission denied"},
	})
	UpdateNodeState(NodeState{
		Node:       "err-node",
		StatusUX:   Streaming,
		ToolErrors: []string{"jfr: timeout after 30s"},
	})
	UpdateNodeState(NodeState{
		Node:       "err-node",
		StatusUX:   Completed,
		Result:     ResultFailure,
		EndProcess: true,
	})

	// Register a successful node with tool errors (node-info failed but streaming OK).
	UpdateNodeState(NodeState{
		Node:       "ok-node",
		StatusUX:   "Done: 9 files (5.3MB), 0 skipped",
		ToolErrors: []string{"jvm-flags: timed out"},
		EndProcess: true,
	})

	origTerminal := isTerminal
	isTerminal = true
	defer func() { isTerminal = origTerminal }()

	out, err := output.CaptureOutput(func() {
		PrintState()
	})
	if err != nil {
		t.Fatalf("CaptureOutput failed: %v", err)
	}

	// Should have "errors" section.
	if !strings.Contains(out, "errors") {
		t.Fatalf("output missing 'errors' section:\n%s", out)
	}

	// Failed node tool errors should appear.
	if !strings.Contains(out, "jstack: permission denied") {
		t.Errorf("output missing first tool error 'jstack: permission denied':\n%s", out)
	}
	if !strings.Contains(out, "jfr: timeout after 30s") {
		t.Errorf("output missing second tool error 'jfr: timeout after 30s':\n%s", out)
	}

	// Successful node with tool errors should also appear in the error section.
	if !strings.Contains(out, "jvm-flags: timed out") {
		t.Errorf("output missing tool error from successful node 'jvm-flags: timed out':\n%s", out)
	}

	// Both node names should appear in the error section.
	if !strings.Contains(out, "err-node") {
		t.Errorf("error section missing node name 'err-node':\n%s", out)
	}
	if !strings.Contains(out, "ok-node") {
		t.Errorf("error section missing node name 'ok-node':\n%s", out)
	}
}

// TestUpfrontQueuedRegistration verifies that nodes registered with Queued
// status appear in TUI output, and that updating one node to Collecting
// transitions only that node while others remain Queued.
func TestUpfrontQueuedRegistration(t *testing.T) {
	Clear()

	// Register 2 coordinators and 1 executor with Queued status.
	UpdateNodeState(NodeState{Node: "coord-1", StatusUX: "Queued", IsCoordinator: true})
	UpdateNodeState(NodeState{Node: "coord-2", StatusUX: "Queued", IsCoordinator: true})
	UpdateNodeState(NodeState{Node: "exec-1", StatusUX: "Queued", IsCoordinator: false})

	// Enable terminal rendering.
	origTerminal := isTerminal
	isTerminal = true
	defer func() { isTerminal = origTerminal }()

	out, err := output.CaptureOutput(func() {
		PrintState()
	})
	if err != nil {
		t.Fatalf("CaptureOutput failed: %v", err)
	}

	// All three nodes should appear with Queued status.
	for _, name := range []string{"coord-1", "coord-2", "exec-1"} {
		if !strings.Contains(out, name) {
			t.Errorf("expected node %q in output, got:\n%s", name, out)
		}
	}
	if count := strings.Count(out, "Queued"); count < 3 {
		t.Errorf("expected at least 3 occurrences of 'Queued', got %d in:\n%s", count, out)
	}

	// Transition coord-1 to Collecting; others should stay Queued.
	UpdateNodeState(NodeState{Node: "coord-1", StatusUX: "Collecting", IsCoordinator: true})
	out, err = output.CaptureOutput(func() {
		PrintState()
	})
	if err != nil {
		t.Fatalf("CaptureOutput failed: %v", err)
	}

	if !strings.Contains(out, "Collecting") {
		t.Errorf("expected 'Collecting' in output after transition:\n%s", out)
	}
	// coord-2 and exec-1 should still be Queued.
	if count := strings.Count(out, "Queued"); count < 2 {
		t.Errorf("expected at least 2 remaining 'Queued' nodes, got %d in:\n%s", count, out)
	}
}

func TestMasterFirstCoordinatorSorting(t *testing.T) {
	Clear()

	// Register a mix of master and regular coordinator pods.
	for _, name := range []string{"dremio-coordinator-0", "dremio-coordinator-1", "dremio-master-0", "dremio-master-1"} {
		UpdateNodeState(NodeState{
			Node:          name,
			StatusUX:      "Collecting",
			IsCoordinator: true,
		})
	}

	// Enable terminal rendering so PrintState uses the full path.
	origTerminal := isTerminal
	isTerminal = true
	defer func() { isTerminal = origTerminal }()

	out, err := output.CaptureOutput(func() {
		PrintState()
	})
	if err != nil {
		t.Fatalf("CaptureOutput failed: %v", err)
	}

	// Both masters should appear before both regular coordinators.
	m0 := strings.Index(out, "dremio-master-0")
	m1 := strings.Index(out, "dremio-master-1")
	c0 := strings.Index(out, "dremio-coordinator-0")
	c1 := strings.Index(out, "dremio-coordinator-1")

	if m0 < 0 || m1 < 0 || c0 < 0 || c1 < 0 {
		t.Fatalf("expected all four nodes in output:\n%s", out)
	}

	if m0 > c0 || m0 > c1 {
		t.Errorf("dremio-master-0 (pos %d) should appear before coordinators (pos %d, %d)", m0, c0, c1)
	}
	if m1 > c0 || m1 > c1 {
		t.Errorf("dremio-master-1 (pos %d) should appear before coordinators (pos %d, %d)", m1, c0, c1)
	}

	// Alphabetical within each group: master-0 before master-1, coordinator-0 before coordinator-1.
	if m0 > m1 {
		t.Errorf("dremio-master-0 (pos %d) should appear before dremio-master-1 (pos %d)", m0, m1)
	}
	if c0 > c1 {
		t.Errorf("dremio-coordinator-0 (pos %d) should appear before dremio-coordinator-1 (pos %d)", c0, c1)
	}
}
