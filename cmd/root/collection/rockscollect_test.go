// Copyright 2023 Dremio Corporation
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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDateSplitWriter_SingleDay(t *testing.T) {
	dir := t.TempDir()
	dw := newDateSplitWriter(dir, "queries-perf")
	defer dw.Close()

	// 1776352188426 ms = 2026-04-16 UTC
	for i := 0; i < 5; i++ {
		if err := dw.WriteLine(`{"query_id":"abc","query_start_epoch_ms":1776352188426,"query_state":"COMPLETED"}`); err != nil {
			t.Fatalf("write failed: %v", err)
		}
	}
	dw.Close()

	data, err := os.ReadFile(filepath.Join(dir, "queries-perf.2026-04-16.json"))
	if err != nil {
		t.Fatalf("expected queries-perf.2026-04-14.json: %v", err)
	}
	lines := countLines(data)
	if lines != 5 {
		t.Errorf("expected 5 lines, got %d", lines)
	}
	if len(dw.dates) != 1 {
		t.Errorf("expected 1 date, got %d", len(dw.dates))
	}
}

func TestDateSplitWriter_MultipleDays(t *testing.T) {
	dir := t.TempDir()
	dw := newDateSplitWriter(dir, "queries-perf")
	defer dw.Close()

	// Epoch ms values for specific dates (UTC):
	// 1776002400000 = 2026-04-12 UTC
	// +86400000     = 2026-04-13 UTC
	// +172800000    = 2026-04-14 UTC
	epochs := []int64{1776002400000, 1776002400000, 1776088800000, 1776088800000, 1776088800000, 1776175200000}
	for _, ep := range epochs {
		line := fmt.Sprintf(`{"query_id":"abc","query_start_epoch_ms":%d,"query_state":"COMPLETED"}`, ep)
		if err := dw.WriteLine(line); err != nil {
			t.Fatalf("write failed: %v", err)
		}
	}
	dw.Close()

	if len(dw.dates) != 3 {
		t.Fatalf("expected 3 dates, got %d: %v", len(dw.dates), dw.dates)
	}

	// Check each file has the right number of lines
	want := map[string]int{"2026-04-12": 2, "2026-04-13": 3, "2026-04-14": 1}
	for date, wantLines := range want {
		data, err := os.ReadFile(filepath.Join(dir, "queries-perf."+date+".json"))
		if err != nil {
			t.Fatalf("expected file for %s: %v", date, err)
		}
		got := countLines(data)
		if got != wantLines {
			t.Errorf("date %s: expected %d lines, got %d", date, wantLines, got)
		}
	}
}

func TestDateSplitWriter_EmptyInput(t *testing.T) {
	dir := t.TempDir()
	dw := newDateSplitWriter(dir, "queries-perf")
	dw.Close()

	files, _ := filepath.Glob(filepath.Join(dir, "queries-perf.*.json"))
	if len(files) != 0 {
		t.Errorf("expected 0 files for empty input, got %d", len(files))
	}
}

func TestExtractDate(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{"normal epoch", `{"query_id":"abc","query_start_epoch_ms":1776352188426,"query_state":"COMPLETED"}`, "2026-04-16"},
		{"epoch at end", `{"query_start_epoch_ms":1776002400000}`, "2026-04-12"},
		{"missing field", `{"query":"SELECT 1"}`, "unknown"},
		{"empty line", "", "unknown"},
		{"non-numeric value", `{"query_start_epoch_ms":"abc"}`, "unknown"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractDate(tc.line)
			if got != tc.want {
				t.Errorf("extractDate(%q) = %q, want %q", tc.line, got, tc.want)
			}
		})
	}
}

func countLines(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	n := 0
	for _, b := range data {
		if b == '\n' {
			n++
		}
	}
	return n
}

func TestWLMFileLayout(t *testing.T) {
	tmpDir := t.TempDir()
	cs := &mockCopyStrategy{tmpDir: tmpDir}

	wlmPayloads := map[string]string{
		"wlm_queues":        `{"queues":[]}`,
		"wlm_rules":         `{"rules":[]}`,
		"wlm_engines":       `{"engines":[]}`,
		"wlm_cluster_usage": `{"cluster_usage":[]}`,
	}

	mc := &mockStreamCollector{
		coordinators: []string{"dremio-master-0"},
		hostExecuteFunc: func(_ bool, _ string, args ...string) (string, error) {
			cmd := strings.Join(args, " ")
			switch {
			case strings.Contains(cmd, "uname -m"):
				return "x86_64\n", nil
			case strings.Contains(cmd, "chmod +x"):
				return "", nil
			case strings.Contains(cmd, "rm -f"):
				return "", nil
			// cluster_stats must succeed so collection reaches the WLM loop
			case strings.Contains(cmd, "-type cluster_stats"):
				return `{"cluster":"stub"}`, nil
			}
			for wt, payload := range wlmPayloads {
				if strings.Contains(cmd, "-type "+wt) {
					return payload, nil
				}
			}
			return "", fmt.Errorf("unexpected host command: %s", cmd)
		},
		copyToHostFunc: func(_, _, _ string) (string, error) { return "", nil },
	}

	args := RocksCollectArgs{
		Collector:           mc,
		CopyStrategy:        cs,
		Host:                "dremio-master-0",
		NodeType:            "coordinator",
		RocksDBDir:          "/opt/dremio/data/db",
		CollectSystemTables: false,
		CollectWLM:          true,
		CollectQueriesPerf:  false,
	}

	got, err := RunRocksDBCollection(args)
	if err != nil {
		t.Fatalf("RunRocksDBCollection failed: %v", err)
	}

	wantContent := map[string]string{
		"queues.json":        `{"queues":[]}`,
		"rules.json":         `{"rules":[]}`,
		"engines.json":       `{"engines":[]}`,
		"cluster_usage.json": `{"cluster_usage":[]}`,
	}

	var wlmDir string
	seen := map[string]bool{}
	for _, cf := range got {
		base := filepath.Base(cf.Path)
		want, ok := wantContent[base]
		if !ok {
			continue // ignore cluster_stats and anything else
		}
		seen[base] = true
		if wlmDir == "" {
			wlmDir = filepath.Dir(cf.Path)
		}
		data, err := os.ReadFile(cf.Path)
		if err != nil {
			t.Errorf("read %s: %v", cf.Path, err)
			continue
		}
		if string(data) != want {
			t.Errorf("content mismatch for %s: got %q want %q", base, string(data), want)
		}
	}
	for base := range wantContent {
		if !seen[base] {
			t.Errorf("expected WLM file %s in returned collected files, but it was not present", base)
		}
	}
	if wlmDir == "" {
		t.Fatal("no WLM files were returned, cannot perform negative-glob check")
	}
	leaks, err := filepath.Glob(filepath.Join(wlmDir, "wlm_*.json"))
	if err != nil {
		t.Fatalf("glob failed: %v", err)
	}
	if len(leaks) != 0 {
		t.Errorf("unexpected v4-style filenames present: %v", leaks)
	}
}
