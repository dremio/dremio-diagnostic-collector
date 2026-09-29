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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/cli"
	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/helpers"
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

func TestRunRocksDBCollectionSkipsWhenNoCatalog(t *testing.T) {
	var calls []string
	mc := &mockStreamCollector{
		hostExecuteFunc: func(_ bool, _ string, args ...string) (string, error) {
			joined := strings.Join(args, " ")
			calls = append(calls, joined)
			// Catalog check: return empty (no catalog present)
			if strings.HasPrefix(joined, "test -f") && strings.Contains(joined, "/catalog/CURRENT") {
				return "", nil
			}
			return "", fmt.Errorf("unexpected host command: %s", joined)
		},
	}
	args := RocksCollectArgs{
		Collector:  mc,
		Host:       "dremio-coordinator-0",
		RocksDBDir: "/opt/dremio/data/db",
	}

	files, _, err := RunRocksDBCollection(args)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if files != nil {
		t.Errorf("expected no files, got %v", files)
	}
	// Only the catalog check should have run — no binary upload (uname -m).
	for _, c := range calls {
		if strings.Contains(c, "uname") {
			t.Errorf("binary upload attempted despite missing catalog: %q", c)
		}
	}
}

func TestRunRocksDBCollectionCollectsWhenCatalogPresent(t *testing.T) {
	var calls []string
	mc := &mockStreamCollector{
		hostExecuteFunc: func(_ bool, _ string, args ...string) (string, error) {
			joined := strings.Join(args, " ")
			calls = append(calls, joined)
			// Catalog check: return "exists" (catalog is present)
			if strings.HasPrefix(joined, "test -f") && strings.Contains(joined, "/catalog/CURRENT") {
				return "exists", nil
			}
			// uname -m: return an error to stop execution — we only care that it was reached.
			if strings.Contains(joined, "uname -m") {
				return "", fmt.Errorf("stop here: uname reached")
			}
			return "", fmt.Errorf("unexpected host command: %s", joined)
		},
	}
	args := RocksCollectArgs{
		Collector:  mc,
		Host:       "dremio-coordinator-0",
		RocksDBDir: "/opt/dremio/data/db",
	}

	// We expect an error (from the uname stub), but NOT a silent nil/nil skip.
	_, _, err := RunRocksDBCollection(args)
	if err == nil {
		t.Fatal("expected an error propagated from uname stub, got nil — catalog gate may have skipped incorrectly")
	}
	// The uname command must have been attempted, proving the gate was passed.
	unameAttempted := false
	for _, c := range calls {
		if strings.Contains(c, "uname") {
			unameAttempted = true
			break
		}
	}
	if !unameAttempted {
		t.Errorf("uname -m was never called despite catalog being present; gate may have incorrectly skipped. calls: %v", calls)
	}
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
			case strings.HasPrefix(cmd, "test -f") && strings.Contains(cmd, "/catalog/CURRENT"):
				return "exists", nil
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
		Collector:              mc,
		CopyStrategy:           cs,
		Host:                   "dremio-master-0",
		NodeType:               "coordinator",
		RocksDBDir:             "/opt/dremio/data/db",
		CollectSystemTables:    false,
		CollectWLM:             true,
		CollectWLMClusterUsage: true,
		CollectQueriesPerf:     false,
	}

	got, _, err := RunRocksDBCollection(args)
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

// TestWLMClusterUsageSkippedByDefault verifies that wlm_cluster_usage is NOT
// exported unless CollectWLMClusterUsage is set (default false), while the
// other three WLM types are still collected under CollectWLM.
func TestWLMClusterUsageSkippedByDefault(t *testing.T) {
	tmpDir := t.TempDir()
	cs := &mockCopyStrategy{tmpDir: tmpDir}

	wlmPayloads := map[string]string{
		"wlm_queues":        `{"queues":[]}`,
		"wlm_rules":         `{"rules":[]}`,
		"wlm_engines":       `{"engines":[]}`,
		"wlm_cluster_usage": `{"cluster_usage":[]}`,
	}

	var mu sync.Mutex
	var calls []string
	mc := &mockStreamCollector{
		coordinators: []string{"dremio-master-0"},
		hostExecuteFunc: func(_ bool, _ string, args ...string) (string, error) {
			cmd := strings.Join(args, " ")
			mu.Lock()
			calls = append(calls, cmd)
			mu.Unlock()
			switch {
			case strings.HasPrefix(cmd, "test -f") && strings.Contains(cmd, "/catalog/CURRENT"):
				return "exists", nil
			case strings.Contains(cmd, "uname -m"):
				return "x86_64\n", nil
			case strings.Contains(cmd, "chmod +x"):
				return "", nil
			case strings.Contains(cmd, "rm -f"):
				return "", nil
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
		// CollectWLMClusterUsage deliberately left false (the default)
		CollectQueriesPerf: false,
	}

	got, _, err := RunRocksDBCollection(args)
	if err != nil {
		t.Fatalf("RunRocksDBCollection failed: %v", err)
	}

	seen := map[string]bool{}
	for _, cf := range got {
		seen[filepath.Base(cf.Path)] = true
	}
	for _, want := range []string{"queues.json", "rules.json", "engines.json"} {
		if !seen[want] {
			t.Errorf("expected WLM file %s to be collected, but it was not", want)
		}
	}
	if seen["cluster_usage.json"] {
		t.Error("cluster_usage.json was collected despite CollectWLMClusterUsage=false")
	}
	for _, c := range calls {
		if strings.Contains(c, "-type wlm_cluster_usage") {
			t.Errorf("rocksdb-viewer was invoked with -type wlm_cluster_usage despite CollectWLMClusterUsage=false: %s", c)
		}
	}
}

// perfLine is one queries_perf record dated 2026-04-16 (UTC).
func perfLine(i int) string {
	return fmt.Sprintf(`{"query_id":"q%d","query_start_epoch_ms":1776352188426}`, i)
}

// queriesPerfCollector streams fixed lines through HostExecuteAndStream, the
// path used by transports without KeepaliveFreeStreamer (SSH, local, kubectl).
// mockStreamCollector holds an atomic.Bool, so it is embedded by pointer.
type queriesPerfCollector struct {
	*mockStreamCollector
	lines     []string
	streamErr error
	calls     int
}

func (q *queriesPerfCollector) HostExecuteAndStream(_ bool, _ string, out cli.OutputHandler, _ string, _ ...string) error {
	q.calls++
	for _, l := range q.lines {
		out(l)
	}
	return q.streamErr
}

// keepaliveFreeCollector also implements KeepaliveFreeStreamer, like the
// Kubernetes API transport.
type keepaliveFreeCollector struct {
	*queriesPerfCollector
	noKeepaliveCalls int
}

func (k *keepaliveFreeCollector) HostExecuteAndStreamNoKeepalive(_ string, out cli.OutputHandler, _ ...string) error {
	k.noKeepaliveCalls++
	for _, l := range k.lines {
		out(l)
	}
	return k.streamErr
}

// newQueriesPerfCollector answers the pre-flight -count with len(lines).
func newQueriesPerfCollector(lines []string, streamErr error) *queriesPerfCollector {
	return &queriesPerfCollector{
		mockStreamCollector: &mockStreamCollector{
			hostExecuteFunc: func(_ bool, _ string, args ...string) (string, error) {
				if strings.Contains(strings.Join(args, " "), "-count") {
					return strconv.Itoa(len(lines)), nil
				}
				return "", fmt.Errorf("unexpected host command: %v", args)
			},
		},
		lines:     lines,
		streamErr: streamErr,
	}
}

// runQueriesPerf calls collectQueriesPerf and returns its results plus the
// copy-strategy root directory.
func runQueriesPerf(t *testing.T, c Collector) ([]helpers.CollectedFile, string, error) {
	t.Helper()
	dir := t.TempDir()
	files, err := collectQueriesPerf(c, &mockCopyStrategy{tmpDir: dir}, "dremio-master-0", "coordinator",
		"/opt/dremio/data/db/catalog", RocksCollectArgs{QueriesPerfDays: 7})
	return files, dir, err
}

func TestCollectQueriesPerf_UsesKeepaliveFreeStreamer(t *testing.T) {
	k := &keepaliveFreeCollector{queriesPerfCollector: newQueriesPerfCollector([]string{perfLine(1), perfLine(2)}, nil)}
	files, _, err := runQueriesPerf(t, k)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if k.noKeepaliveCalls != 1 || k.calls != 0 {
		t.Fatalf("noKeepaliveCalls=%d fallbackCalls=%d, want 1 and 0", k.noKeepaliveCalls, k.calls)
	}
	if len(files) != 1 {
		t.Fatalf("files = %d, want 1", len(files))
	}
}

func TestCollectQueriesPerf_IncompleteKeepsRecords(t *testing.T) {
	k := &keepaliveFreeCollector{queriesPerfCollector: newQueriesPerfCollector([]string{perfLine(1), perfLine(2)}, ErrStreamIncomplete)}
	files, dir, err := runQueriesPerf(t, k)
	var inc *IncompleteCollectionError
	if !errors.As(err, &inc) {
		t.Fatalf("err = %v, want *IncompleteCollectionError", err)
	}
	if inc.Item != "queries-perf" || inc.Records != 2 || !errors.Is(err, ErrStreamIncomplete) {
		t.Errorf("got %+v, want queries-perf / 2 records / ErrStreamIncomplete", inc)
	}
	want := "queries-perf incomplete after 2 records (remote stream ended before end-of-stream marker)"
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
	if len(files) != 1 {
		t.Fatalf("files = %d, want 1 (partial data kept)", len(files))
	}
	data, readErr := os.ReadFile(filepath.Join(dir, "queries-perf", "dremio-master-0", "queries-perf.2026-04-16.json"))
	if readErr != nil || countLines(data) != 2 {
		t.Errorf("kept file: err=%v lines=%d, want 2 lines", readErr, countLines(data))
	}
}

func TestCollectQueriesPerf_ErrorBeforeFirstRecordFails(t *testing.T) {
	k := &keepaliveFreeCollector{queriesPerfCollector: newQueriesPerfCollector(nil, errors.New("exec failed"))}
	files, _, err := runQueriesPerf(t, k)
	var inc *IncompleteCollectionError
	if err == nil || errors.As(err, &inc) {
		t.Fatalf("err = %v, want a plain failure", err)
	}
	if files != nil {
		t.Errorf("files = %v, want nil", files)
	}
}

func TestCollectQueriesPerf_FallbackTransportIncomplete(t *testing.T) {
	q := newQueriesPerfCollector([]string{perfLine(1), perfLine(2)}, errors.New("ssh: connection lost"))
	files, _, err := runQueriesPerf(t, q)
	var inc *IncompleteCollectionError
	if !errors.As(err, &inc) || inc.Records != 2 {
		t.Fatalf("err = %v, want *IncompleteCollectionError with 2 records", err)
	}
	if q.calls != 1 || len(files) != 1 {
		t.Fatalf("fallbackCalls=%d files=%d, want 1 and 1", q.calls, len(files))
	}
}

func TestRunRocksDBCollection_ReportsIncompleteQueriesPerf(t *testing.T) {
	q := newQueriesPerfCollector([]string{perfLine(1), perfLine(2)}, ErrStreamIncomplete)
	q.hostExecuteFunc = func(_ bool, _ string, args ...string) (string, error) {
		cmd := strings.Join(args, " ")
		switch {
		case strings.HasPrefix(cmd, "test -f") && strings.Contains(cmd, "/catalog/CURRENT"):
			return "exists", nil
		case strings.Contains(cmd, "uname -m"):
			return "x86_64\n", nil
		case strings.Contains(cmd, "chmod +x"), strings.Contains(cmd, "rm -f"):
			return "", nil
		case strings.Contains(cmd, "-type cluster_stats"):
			return `{"cluster":"stub"}`, nil
		case strings.Contains(cmd, "-count"):
			return "2", nil
		}
		return "", fmt.Errorf("unexpected host command: %s", cmd)
	}
	q.copyToHostFunc = func(_, _, _ string) (string, error) { return "", nil }
	k := &keepaliveFreeCollector{queriesPerfCollector: q}

	files, incomplete, err := RunRocksDBCollection(RocksCollectArgs{
		Collector:          k,
		CopyStrategy:       &mockCopyStrategy{tmpDir: t.TempDir()},
		Host:               "dremio-master-0",
		NodeType:           "coordinator",
		RocksDBDir:         "/opt/dremio/data/db",
		CollectQueriesPerf: true,
		QueriesPerfDays:    7,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(incomplete) != 1 || incomplete[0].Item != "queries-perf" || incomplete[0].Records != 2 {
		t.Fatalf("incomplete = %+v, want one queries-perf item with 2 records", incomplete)
	}
	if len(files) != 2 {
		t.Errorf("files = %d, want 2 (cluster-stats + partial queries-perf)", len(files))
	}
}
