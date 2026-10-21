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
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

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

	files, _, _, err := RunRocksDBCollection(args)
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
	_, _, _, err := RunRocksDBCollection(args)
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

	got, _, _, err := RunRocksDBCollection(args)
	if err != nil {
		t.Fatalf("RunRocksDBCollection failed: %v", err)
	}

	wantContent := map[string]string{
		"queues.json":        `{"queues":[]}` + "\n",
		"rules.json":         `{"rules":[]}` + "\n",
		"engines.json":       `{"engines":[]}` + "\n",
		"cluster_usage.json": `{"cluster_usage":[]}` + "\n",
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

	got, _, _, err := RunRocksDBCollection(args)
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
// Only -type queries_perf is served from lines/streamErr; every other command
// goes to mockStreamCollector.HostExecuteAndStream.
// mockStreamCollector holds an atomic.Bool, so it is embedded by pointer.
type queriesPerfCollector struct {
	*mockStreamCollector
	lines      []string
	streamErr  error
	calls      int
	streamCmds []string // every command served by HostExecuteAndStream
}

func (q *queriesPerfCollector) HostExecuteAndStream(mask bool, host string, out cli.OutputHandler, pat string, args ...string) error {
	cmd := strings.Join(args, " ")
	q.streamCmds = append(q.streamCmds, cmd)
	if !strings.Contains(cmd, "-type queries_perf") {
		return q.mockStreamCollector.HostExecuteAndStream(mask, host, out, pat, args...)
	}
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
	noKeepaliveCmds  []string // every command served by HostExecuteAndStreamNoKeepalive
}

func (k *keepaliveFreeCollector) HostExecuteAndStreamNoKeepalive(host string, out cli.OutputHandler, args ...string) error {
	cmd := strings.Join(args, " ")
	k.noKeepaliveCmds = append(k.noKeepaliveCmds, cmd)
	if !strings.Contains(cmd, "-type queries_perf") {
		return k.mockStreamCollector.HostExecuteAndStream(false, host, out, "", args...)
	}
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

func TestCollectQueriesPerf_CountGuardSkipsStream(t *testing.T) {
	k := &keepaliveFreeCollector{queriesPerfCollector: newQueriesPerfCollector([]string{perfLine(1)}, nil)}
	k.hostExecuteFunc = func(_ bool, _ string, args ...string) (string, error) {
		if strings.Contains(strings.Join(args, " "), "-count") {
			return guardRefuseLine, &RemoteExitError{Code: 3}
		}
		return "", fmt.Errorf("unexpected host command: %v", args)
	}
	files, _, err := runQueriesPerf(t, k)
	var mg *MemoryGuardError
	if !errors.As(err, &mg) {
		t.Fatalf("err = %v, want *MemoryGuardError", err)
	}
	if mg.Item != "queries-perf" || mg.Detail != refuseDetail {
		t.Errorf("got %+v, want queries-perf / %q", mg, refuseDetail)
	}
	if files != nil {
		t.Errorf("files = %v, want nil", files)
	}
	if k.noKeepaliveCalls != 0 || k.calls != 0 {
		t.Errorf("noKeepaliveCalls=%d calls=%d, want 0 and 0 (stream must not start)", k.noKeepaliveCalls, k.calls)
	}
}

func TestCollectQueriesPerf_CountFailureNonGuardStillStreams(t *testing.T) {
	k := &keepaliveFreeCollector{queriesPerfCollector: newQueriesPerfCollector([]string{perfLine(1), perfLine(2)}, nil)}
	k.hostExecuteFunc = func(_ bool, _ string, args ...string) (string, error) {
		if strings.Contains(strings.Join(args, " "), "-count") {
			return "", errors.New("boom")
		}
		return "", fmt.Errorf("unexpected host command: %v", args)
	}
	files, _, err := runQueriesPerf(t, k)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 || k.noKeepaliveCalls != 1 {
		t.Fatalf("files=%d noKeepaliveCalls=%d, want 1 and 1", len(files), k.noKeepaliveCalls)
	}
}

func TestCollectQueriesPerf_StreamGuardDiscardsFiles(t *testing.T) {
	k := &keepaliveFreeCollector{queriesPerfCollector: newQueriesPerfCollector(
		[]string{perfLine(1), perfLine(2)}, &RemoteExitError{Code: 3, Stderr: guardAbortLine})}
	files, dir, err := runQueriesPerf(t, k)
	var mg *MemoryGuardError
	var inc *IncompleteCollectionError
	if !errors.As(err, &mg) || errors.As(err, &inc) {
		t.Fatalf("err = %v, want *MemoryGuardError and not *IncompleteCollectionError", err)
	}
	if mg.Item != "queries-perf" || mg.Detail != abortDetail {
		t.Errorf("got %+v, want queries-perf / %q", mg, abortDetail)
	}
	if files != nil {
		t.Errorf("files = %v, want nil", files)
	}
	left, _ := filepath.Glob(filepath.Join(dir, "queries-perf", "dremio-master-0", "*.json"))
	if len(left) != 0 {
		t.Errorf("leftover files: %v", left)
	}
}

func TestCollectQueriesPerf_FallbackGuardLineNotWritten(t *testing.T) {
	q := newQueriesPerfCollector([]string{perfLine(1), guardAbortLine}, &RemoteExitError{Code: 3})
	files, dir, err := runQueriesPerf(t, q)
	var mg *MemoryGuardError
	if !errors.As(err, &mg) || mg.Detail != abortDetail {
		t.Fatalf("err = %v, want *MemoryGuardError with abort detail", err)
	}
	if files != nil {
		t.Errorf("files = %v, want nil", files)
	}
	var jsons []string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, _ error) error {
		if d != nil && !d.IsDir() && strings.HasSuffix(p, ".json") {
			jsons = append(jsons, p)
		}
		return nil
	})
	if len(jsons) != 0 {
		t.Errorf("leftover json files: %v", jsons)
	}
}

func TestRunRocksDBCollection_QueriesPerfGuardSkip(t *testing.T) {
	q := newQueriesPerfCollector([]string{perfLine(1), perfLine(2)}, &RemoteExitError{Code: 3, Stderr: guardAbortLine})
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

	files, incomplete, guardSkips, err := RunRocksDBCollection(RocksCollectArgs{
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
	if len(incomplete) != 0 {
		t.Errorf("incomplete = %+v, want none", incomplete)
	}
	if len(guardSkips) != 1 || guardSkips[0].Item != "queries-perf" || guardSkips[0].Detail != abortDetail {
		t.Fatalf("guardSkips = %+v, want one queries-perf item", guardSkips)
	}
	if len(files) != 1 || filepath.Base(files[0].Path) != "cluster-stats.json" {
		t.Errorf("files = %+v, want only cluster-stats.json", files)
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

	files, incomplete, _, err := RunRocksDBCollection(RocksCollectArgs{
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

// viewerVersionCollector stubs the host commands needed to reach cluster_stats,
// recording every command and answering the -version call with versionOut/versionErr.
func viewerVersionCollector(calls *[]string, versionOut string, versionErr error) *mockStreamCollector {
	return &mockStreamCollector{
		hostExecuteFunc: func(_ bool, _ string, args ...string) (string, error) {
			cmd := strings.Join(args, " ")
			*calls = append(*calls, cmd)
			switch {
			case strings.HasPrefix(cmd, "test -f") && strings.Contains(cmd, "/catalog/CURRENT"):
				return "exists", nil
			case strings.Contains(cmd, "uname -m"):
				return "x86_64", nil
			case strings.Contains(cmd, "chmod"), strings.Contains(cmd, "rm -f"):
				return "", nil
			case strings.HasSuffix(cmd, " -version"):
				return versionOut, versionErr
			case strings.Contains(cmd, "-type cluster_stats"):
				return `{"cluster":"stub"}`, nil
			}
			return "", fmt.Errorf("unexpected host command: %s", cmd)
		},
	}
}

func TestRunRocksDBCollection_LogsViewerVersion(t *testing.T) {
	var calls []string
	mc := viewerVersionCollector(&calls, "dremio-rocksdb-viewer v1.1.0", nil)
	_, _, _, err := RunRocksDBCollection(RocksCollectArgs{
		Collector:    mc,
		CopyStrategy: &mockCopyStrategy{tmpDir: t.TempDir()},
		Host:         "dremio-master-0",
		NodeType:     "coordinator",
		RocksDBDir:   "/opt/dremio/data/db",
	})
	if err != nil {
		t.Fatalf("RunRocksDBCollection failed: %v", err)
	}
	chmodIdx, versionIdx, firstTypeIdx := -1, -1, -1
	for i, c := range calls {
		switch {
		case c == "chmod +x /tmp/dremio-rocksdb-viewer":
			chmodIdx = i
		case c == "/tmp/dremio-rocksdb-viewer -version":
			versionIdx = i
		case strings.Contains(c, " -type ") && firstTypeIdx == -1:
			firstTypeIdx = i
		}
	}
	if versionIdx == -1 {
		t.Fatalf("-version command was never issued; calls: %v", calls)
	}
	if chmodIdx == -1 || versionIdx < chmodIdx {
		t.Errorf("-version (idx %d) must run after chmod (idx %d)", versionIdx, chmodIdx)
	}
	if firstTypeIdx == -1 || versionIdx > firstTypeIdx {
		t.Errorf("-version (idx %d) must run before the first -type command (idx %d)", versionIdx, firstTypeIdx)
	}
}

func TestRunRocksDBCollection_VersionFailureNonFatal(t *testing.T) {
	var calls []string
	mc := viewerVersionCollector(&calls, "", errors.New("exec failed"))
	got, _, _, err := RunRocksDBCollection(RocksCollectArgs{
		Collector:    mc,
		CopyStrategy: &mockCopyStrategy{tmpDir: t.TempDir()},
		Host:         "dremio-master-0",
		NodeType:     "coordinator",
		RocksDBDir:   "/opt/dremio/data/db",
	})
	if err != nil {
		t.Fatalf("expected nil error when -version fails, got %v", err)
	}
	found := false
	for _, cf := range got {
		if filepath.Base(cf.Path) == "cluster-stats.json" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected cluster-stats.json to be collected, got %v", got)
	}
}

func TestBuildJobsRecentFilterArgs(t *testing.T) {
	tests := []struct {
		name string
		args RocksCollectArgs
		want string
	}{
		{"perf days 30 capped to 7", RocksCollectArgs{QueriesPerfDays: 30}, " -days 7"},
		{"perf days 7", RocksCollectArgs{QueriesPerfDays: 7}, " -days 7"},
		{"perf days 3", RocksCollectArgs{QueriesPerfDays: 3}, " -days 3"},
		{"perf days 0 defaults to 7", RocksCollectArgs{QueriesPerfDays: 0}, " -days 7"},
		{"days 3", RocksCollectArgs{Days: 3}, " -days 3"},
		{"days 30 uncapped", RocksCollectArgs{Days: 30, QueriesPerfDays: 30}, " -days 30"},
		{"start date and days", RocksCollectArgs{StartDate: "2026-10-01", Days: 5}, " -start-date 2026-10-01 -days 5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildJobsRecentFilterArgs(tt.args); got != tt.want {
				t.Errorf("buildJobsRecentFilterArgs(%+v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestRunRocksDBCollection_JobsRecentWindow(t *testing.T) {
	var calls []string
	mc := &mockStreamCollector{
		hostExecuteFunc: func(_ bool, _ string, args ...string) (string, error) {
			cmd := strings.Join(args, " ")
			calls = append(calls, cmd)
			switch {
			case strings.HasPrefix(cmd, "test -f") && strings.Contains(cmd, "/catalog/CURRENT"):
				return "exists", nil
			case strings.Contains(cmd, "uname -m"):
				return "x86_64", nil
			case strings.Contains(cmd, "chmod"), strings.Contains(cmd, "rm -f"), strings.HasSuffix(cmd, " -version"):
				return "", nil
			case strings.Contains(cmd, " -type "):
				return `{"a":1}`, nil
			}
			return "", fmt.Errorf("unexpected host command: %s", cmd)
		},
	}
	_, _, _, err := RunRocksDBCollection(RocksCollectArgs{
		Collector:           mc,
		CopyStrategy:        &mockCopyStrategy{tmpDir: t.TempDir()},
		Host:                "dremio-master-0",
		NodeType:            "coordinator",
		RocksDBDir:          "/opt/dremio/data/db",
		CollectSystemTables: true,
		SystemTables:        []string{"jobs_recent", "views"},
		QueriesPerfDays:     30,
	})
	if err != nil {
		t.Fatalf("RunRocksDBCollection failed: %v", err)
	}
	var jobs, views string
	for _, c := range calls {
		switch {
		case strings.Contains(c, "-type sys.jobs_recent"):
			jobs = c
		case strings.Contains(c, "-type sys.views"):
			views = c
		}
	}
	if !strings.HasSuffix(jobs, "-type sys.jobs_recent -days 7") {
		t.Errorf("jobs_recent command = %q, want suffix %q", jobs, "-type sys.jobs_recent -days 7")
	}
	if views == "" || strings.Contains(views, "-days") {
		t.Errorf("views command = %q, want present and without -days", views)
	}
}

// regularFiles lists every regular file under dir.
func regularFiles(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return files
}

// streamCollector answers every command with out and err; viewer streams go
// through the mock's merged-output HostExecuteAndStream.
func streamCollector(out string, err error) *mockStreamCollector {
	return &mockStreamCollector{
		hostExecuteFunc: func(_ bool, _ string, _ ...string) (string, error) { return out, err },
	}
}

// runRocksType runs collectRocksType for item into a fresh copy-strategy dir and
// returns its results plus that dir.
func runRocksType(t *testing.T, c Collector, item, filename string) (*helpers.CollectedFile, string, error) {
	t.Helper()
	dir := t.TempDir()
	cf, err := collectRocksType(c, &mockCopyStrategy{tmpDir: dir}, "dremio-master-0", "coordinator",
		"/opt/dremio/data/db/catalog", item, "", "system-tables", filename)
	return cf, dir, err
}

func TestCollectRocksType_StreamsExactBytes(t *testing.T) {
	cf, dir, err := runRocksType(t, streamCollector("{\"a\":1}\n{\"a\":2}", nil), "sys.views", "sys.views.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cf == nil {
		t.Fatal("expected a collected file, got nil")
	}
	if filepath.Base(cf.Path) != "sys.views.json" {
		t.Errorf("path = %s, want sys.views.json", cf.Path)
	}
	data, err := os.ReadFile(cf.Path)
	if err != nil {
		t.Fatalf("read %s: %v", cf.Path, err)
	}
	if want := "{\"a\":1}\n{\"a\":2}\n"; string(data) != want {
		t.Errorf("content = %q, want %q", string(data), want)
	}
	if cf.Size != 16 {
		t.Errorf("Size = %d, want 16", cf.Size)
	}
	files := regularFiles(t, dir)
	for _, f := range files {
		if strings.HasSuffix(f, ".partial") {
			t.Errorf("temp file left behind: %s", f)
		}
	}
	if len(files) != 1 {
		t.Errorf("files = %v, want only %s", files, cf.Path)
	}
}

func TestCollectRocksType_EmptyOutputNoFile(t *testing.T) {
	cf, dir, err := runRocksType(t, streamCollector("", nil), "sys.views", "sys.views.json")
	if cf != nil || err != nil {
		t.Fatalf("got (%v, %v), want (nil, nil)", cf, err)
	}
	if files := regularFiles(t, dir); len(files) != 0 {
		t.Errorf("files = %v, want none", files)
	}
}

func TestCollectRocksType_ErrorLeavesNoFile(t *testing.T) {
	cf, dir, err := runRocksType(t, streamCollector("{\"a\":1}\n{\"a\":2}", errors.New("connection reset")), "sys.views", "sys.views.json")
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	var mg *MemoryGuardError
	if errors.As(err, &mg) {
		t.Errorf("err = %v, want a plain error, not a memory guard skip", err)
	}
	if cf != nil {
		t.Errorf("cf = %v, want nil", cf)
	}
	if files := regularFiles(t, dir); len(files) != 0 {
		t.Errorf("files = %v, want none", files)
	}
}

func TestCollectRocksType_Exit137IsPlainError(t *testing.T) {
	_, dir, err := runRocksType(t, streamCollector(`{"a":1}`, &RemoteExitError{Code: 137}), "sys.views", "sys.views.json")
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	var mg *MemoryGuardError
	if errors.As(err, &mg) {
		t.Errorf("err = %v, want a plain error for exit 137", err)
	}
	if files := regularFiles(t, dir); len(files) != 0 {
		t.Errorf("files = %v, want none", files)
	}
}

func TestCollectRocksType_GuardOnMergedOutput(t *testing.T) {
	c := streamCollector("{\"a\":1}\n"+guardRefuseLine, &RemoteExitError{Code: 3})
	cf, dir, err := runRocksType(t, c, "sys.jobs_recent", "sys.jobs_recent.json")
	var mg *MemoryGuardError
	if !errors.As(err, &mg) {
		t.Fatalf("err = %v, want *MemoryGuardError", err)
	}
	if mg.Item != "sys.jobs_recent" || mg.Detail != refuseDetail {
		t.Errorf("got %+v, want sys.jobs_recent / %q", mg, refuseDetail)
	}
	if cf != nil {
		t.Errorf("cf = %v, want nil", cf)
	}
	if files := regularFiles(t, dir); len(files) != 0 {
		t.Errorf("files = %v, want none", files)
	}
}

func TestCollectRocksType_GuardOnKeepaliveFree(t *testing.T) {
	k := &keepaliveFreeCollector{queriesPerfCollector: &queriesPerfCollector{
		mockStreamCollector: streamCollector("", &RemoteExitError{Code: 3, Stderr: guardAbortLine}),
	}}
	_, dir, err := runRocksType(t, k, "sys.views", "sys.views.json")
	var mg *MemoryGuardError
	if !errors.As(err, &mg) {
		t.Fatalf("err = %v, want *MemoryGuardError", err)
	}
	if mg.Item != "sys.views" || mg.Detail != abortDetail {
		t.Errorf("got %+v, want sys.views / %q", mg, abortDetail)
	}
	if len(k.noKeepaliveCmds) != 1 || len(k.streamCmds) != 0 {
		t.Errorf("noKeepaliveCmds=%v streamCmds=%v, want one keepalive-free call", k.noKeepaliveCmds, k.streamCmds)
	}
	if files := regularFiles(t, dir); len(files) != 0 {
		t.Errorf("files = %v, want none", files)
	}
}

func TestRunViewerStream_ExecutorPerType(t *testing.T) {
	cmdFor := func(dataType string) string {
		return rocksdbViewerRemotePath + " -db /opt/dremio/data/db/catalog -type " + dataType
	}
	types := []string{"wlm_cluster_usage", "sys.views", "cluster_stats", "wlm_queues"}
	run := func(c Collector) {
		t.Helper()
		for _, dt := range types {
			if err := runViewerStream(c, "dremio-master-0", dt, cmdFor(dt), func(string) {}); err != nil {
				t.Fatalf("runViewerStream(%s): %v", dt, err)
			}
		}
	}

	k := &keepaliveFreeCollector{queriesPerfCollector: &queriesPerfCollector{mockStreamCollector: streamCollector("", nil)}}
	run(k)
	if want := []string{cmdFor("wlm_cluster_usage")}; !slices.Equal(k.streamCmds, want) {
		t.Errorf("keepalive-capable: HostExecuteAndStream served %v, want %v", k.streamCmds, want)
	}
	if want := []string{cmdFor("sys.views"), cmdFor("cluster_stats"), cmdFor("wlm_queues")}; !slices.Equal(k.noKeepaliveCmds, want) {
		t.Errorf("keepalive-capable: HostExecuteAndStreamNoKeepalive served %v, want %v", k.noKeepaliveCmds, want)
	}

	q := &queriesPerfCollector{mockStreamCollector: streamCollector("", nil)}
	run(q)
	var want []string
	for _, dt := range types {
		want = append(want, cmdFor(dt))
	}
	if !slices.Equal(q.streamCmds, want) {
		t.Errorf("plain: HostExecuteAndStream served %v, want %v", q.streamCmds, want)
	}
}

func TestRunRocksDBCollection_GuardSkipsContinue(t *testing.T) {
	var calls []string
	mc := &mockStreamCollector{
		hostExecuteFunc: func(_ bool, _ string, args ...string) (string, error) {
			cmd := strings.Join(args, " ")
			calls = append(calls, cmd)
			switch {
			case strings.HasPrefix(cmd, "test -f") && strings.Contains(cmd, "/catalog/CURRENT"):
				return "exists", nil
			case strings.Contains(cmd, "uname -m"):
				return "x86_64", nil
			case strings.Contains(cmd, "chmod"), strings.Contains(cmd, "rm -f"), strings.HasSuffix(cmd, " -version"):
				return "", nil
			case strings.Contains(cmd, "-type sys.jobs_recent"), strings.Contains(cmd, "-type sys.views"):
				return guardRefuseLine, &RemoteExitError{Code: 3}
			case strings.Contains(cmd, "-type sys.tables"):
				return `{"t":1}`, nil
			case strings.Contains(cmd, "-type cluster_stats"):
				return `{"cluster":"stub"}`, nil
			}
			return "", fmt.Errorf("unexpected host command: %s", cmd)
		},
	}
	files, _, guards, err := RunRocksDBCollection(RocksCollectArgs{
		Collector:           mc,
		CopyStrategy:        &mockCopyStrategy{tmpDir: t.TempDir()},
		Host:                "dremio-master-0",
		NodeType:            "coordinator",
		RocksDBDir:          "/opt/dremio/data/db",
		CollectSystemTables: true,
		SystemTables:        []string{"jobs_recent", "views", "tables"},
	})
	if err != nil {
		t.Fatalf("RunRocksDBCollection failed: %v", err)
	}
	var items []string
	for _, g := range guards {
		items = append(items, g.Item)
		if g.Detail != refuseDetail {
			t.Errorf("%s: Detail = %q, want %q", g.Item, g.Detail, refuseDetail)
		}
	}
	if want := []string{"sys.jobs_recent", "sys.views"}; !slices.Equal(items, want) {
		t.Errorf("guard skips = %v, want %v", items, want)
	}
	found := false
	for _, cf := range files {
		if filepath.Base(cf.Path) == "sys.tables.json" {
			found = true
		}
	}
	if !found {
		t.Errorf("sys.tables.json not collected; files = %v", files)
	}
	if !slices.Contains(calls, "rm -f "+rocksdbViewerRemotePath) {
		t.Errorf("viewer cleanup not issued; calls = %v", calls)
	}
}

func stubRename(t *testing.T, failures int) *int {
	t.Helper()
	origRename, origSleep := renameFile, renameSleep
	t.Cleanup(func() { renameFile, renameSleep = origRename, origSleep })
	calls := 0
	renameFile = func(_, _ string) error {
		calls++
		if calls <= failures {
			return errors.New("access is denied")
		}
		return nil
	}
	renameSleep = func(time.Duration) {}
	return &calls
}

func TestRenameWithRetry_SucceedsAfterTransientFailures(t *testing.T) {
	calls := stubRename(t, 2)
	if err := renameWithRetry("a.partial", "a"); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if *calls != 3 {
		t.Fatalf("expected 3 rename calls, got %d", *calls)
	}
}

func TestRenameWithRetry_GivesUpAfterFiveAttempts(t *testing.T) {
	calls := stubRename(t, 1000)
	if err := renameWithRetry("a.partial", "a"); err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if *calls != 5 {
		t.Fatalf("expected 5 rename calls, got %d", *calls)
	}
}
