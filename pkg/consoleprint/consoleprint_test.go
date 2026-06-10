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

package consoleprint_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/consoleprint"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/output"
)

func TestClearsScreen(t *testing.T) {
	out, err := output.CaptureOutput(func() {
		consoleprint.PrintState()
	})
	if err != nil {
		t.Fatal(err)
	}

	// In test mode isTerminal is false, so PrintState uses the
	// non-interactive fallback which prints "[completed/total] result".
	if !strings.Contains(out, "[0/0]") {
		t.Errorf("output %q did not contain non-interactive status line '[0/0]'", out)
	}
}

func TestPrintState_JSONMode(t *testing.T) {
	consoleprint.Clear()
	consoleprint.EnableStatusOutput()
	defer consoleprint.DisableStatusOutput()

	consoleprint.SetVersion("4.0.0-test")
	consoleprint.UpdateCollectionMode("standard")
	consoleprint.UpdateRuntime("4.0.0-test", "/tmp/ddc.log", 1, 0, 0)
	consoleprint.UpdateNodeState(consoleprint.NodeState{
		Node:          "host-a",
		StatusUX:      "Collecting server.log",
		IsCoordinator: true,
	})
	consoleprint.UpdateResult("COLLECTING")

	out, err := output.CaptureOutput(func() {
		consoleprint.PrintState()
	})
	if err != nil {
		t.Fatal(err)
	}

	var snap consoleprint.JSONSnapshot
	if err := json.Unmarshal([]byte(out), &snap); err != nil {
		t.Fatalf("failed to parse JSON snapshot: %v\nraw: %s", err, out)
	}
	if snap.Type != "status" {
		t.Errorf("expected type=status, got %s", snap.Type)
	}
	if snap.Mode != "standard" {
		t.Errorf("expected mode=standard, got %s", snap.Mode)
	}
	if snap.Coordinators != 1 {
		t.Errorf("expected 1 coordinator, got %d", snap.Coordinators)
	}
	if len(snap.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(snap.Nodes))
	}
	if snap.Nodes[0].Node != "host-a" {
		t.Errorf("expected node host-a, got %s", snap.Nodes[0].Node)
	}
	if !snap.Nodes[0].IsCoordinator {
		t.Error("expected node to be coordinator")
	}
}
