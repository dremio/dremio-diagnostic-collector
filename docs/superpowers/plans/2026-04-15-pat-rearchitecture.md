# PAT Rearchitecture Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate PAT token dependency from standard mode and make it optional in diagnosis mode by replacing REST API collection with an embedded `dremio-rocksdb-viewer` binary.

**Architecture:** Vertical slices — shared embed/execution layer first, then standard mode refactoring, then diagnosis mode refactoring. The `dremio-rocksdb-viewer` binary is embedded via `go:embed` (same pattern as async-profiler), uploaded to the coordinator node at collection time, and invoked to stream RocksDB data to stdout. Collection progress shows in coordinator node-level status.

**Tech Stack:** Go 1.21+, `go:embed`, charmbracelet/huh TUI, Cobra CLI

---

## File Structure

**New files:**
- `cmd/local/rockscollect/rocksdb_embed.go` — embedded binary declarations + accessor functions
- `cmd/local/rockscollect/rocksdb_embed_test.go` — unit tests for embed accessors
- `cmd/local/rockscollect/rocksdb/dremio-rocksdb-viewer-linux-amd64` — placeholder binary (copied from source)
- `cmd/local/rockscollect/rocksdb/dremio-rocksdb-viewer-linux-arm64` — placeholder binary (copied from source)
- `cmd/root/collection/rockscollect.go` — rocksdb-viewer execution orchestration (upload, invoke, stream, split)
- `cmd/root/collection/rockscollect_test.go` — unit tests for file splitting logic

**Modified files:**
- `cmd/local/conf/conf_key_names.go` — rename key, add new keys
- `cmd/local/conf/defaults.go` — add queries-perf defaults
- `cmd/local/conf/defaults_test.go` — update tests for new defaults
- `cmd/configui/configui.go` — TUI restructuring (both modes)
- `cmd/configui/configui_test.go` — update CLI command tests
- `cmd/root.go` — flag changes, config mapping
- `cmd/root_flags_test.go` — flag placement tests
- `cmd/root/collection/collector.go` — add fields to Args struct
- `cmd/root/collection/streaming_collect.go` — replace API calls with rocksdb-viewer
- `cmd/root/collection/apicollect.go` — remove dead functions

---

### Task 1: Embed dremio-rocksdb-viewer Binaries

**Files:**
- Create: `cmd/local/rockscollect/rocksdb_embed.go`
- Create: `cmd/local/rockscollect/rocksdb/dremio-rocksdb-viewer-linux-amd64`
- Create: `cmd/local/rockscollect/rocksdb/dremio-rocksdb-viewer-linux-arm64`
- Create: `cmd/local/rockscollect/rocksdb_embed_test.go`

- [ ] **Step 1: Copy binaries into the project**

```bash
mkdir -p cmd/local/rockscollect/rocksdb
cp "C:/Users/chufe/Workspaces/golang/dremio-rocksdb-viewer/bin/dremio-rocksdb-viewer-linux-amd64" cmd/local/rockscollect/rocksdb/
cp "C:/Users/chufe/Workspaces/golang/dremio-rocksdb-viewer/bin/dremio-rocksdb-viewer-linux-arm64" cmd/local/rockscollect/rocksdb/
```

- [ ] **Step 2: Write the embed module**

Create `cmd/local/rockscollect/rocksdb_embed.go`:

```go
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

// package rockscollect handles embedding and extraction of the dremio-rocksdb-viewer binary.
package rockscollect

import (
	_ "embed"
	"fmt"
)

//go:embed rocksdb/dremio-rocksdb-viewer-linux-amd64
var rocksdbViewerAMD64 []byte

//go:embed rocksdb/dremio-rocksdb-viewer-linux-arm64
var rocksdbViewerARM64 []byte

// ErrRocksDBViewerEmpty is returned when the embedded binary is a placeholder (empty).
var ErrRocksDBViewerEmpty = fmt.Errorf("embedded dremio-rocksdb-viewer binary is empty (placeholder)")

// GetRocksDBViewerBinary returns the embedded dremio-rocksdb-viewer binary bytes
// for the given architecture string (as returned by uname -m). It maps "x86_64"
// to the amd64 binary and "aarch64" to the arm64 binary. Returns
// ErrRocksDBViewerEmpty if the binary for the resolved architecture is an empty
// placeholder.
func GetRocksDBViewerBinary(arch string) ([]byte, error) {
	var bin []byte
	switch arch {
	case "x86_64", "amd64":
		bin = rocksdbViewerAMD64
	case "aarch64", "arm64":
		bin = rocksdbViewerARM64
	default:
		return nil, fmt.Errorf("unsupported architecture for embedded dremio-rocksdb-viewer: %q", arch)
	}
	if len(bin) == 0 {
		return nil, ErrRocksDBViewerEmpty
	}
	return bin, nil
}
```

- [ ] **Step 3: Write the failing tests**

Create `cmd/local/rockscollect/rocksdb_embed_test.go`:

```go
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

package rockscollect

import (
	"errors"
	"testing"
)

func TestGetRocksDBViewerBinary_UnsupportedArch(t *testing.T) {
	_, err := GetRocksDBViewerBinary("s390x")
	if err == nil {
		t.Fatal("expected error for unsupported arch s390x")
	}
}

func TestGetRocksDBViewerBinary_RealBinaries(t *testing.T) {
	for _, arch := range []string{"x86_64", "aarch64"} {
		bin, err := GetRocksDBViewerBinary(arch)
		if errors.Is(err, ErrRocksDBViewerEmpty) {
			t.Skipf("embedded binary for %s is a placeholder, skipping size check", arch)
		}
		if err != nil {
			t.Fatalf("unexpected error for arch %s: %v", arch, err)
		}
		if len(bin) < 1000 {
			t.Errorf("binary for %s unexpectedly small: %d bytes", arch, len(bin))
		}
	}
}

func TestGetRocksDBViewerBinary_ArchMapping(t *testing.T) {
	// x86_64 and amd64 must resolve to the same binary.
	a, err1 := GetRocksDBViewerBinary("x86_64")
	b, err2 := GetRocksDBViewerBinary("amd64")
	if err1 != nil || err2 != nil {
		if errors.Is(err1, ErrRocksDBViewerEmpty) && errors.Is(err2, ErrRocksDBViewerEmpty) {
			t.Skip("placeholders — skipping alias check")
		}
		t.Fatalf("errors: x86_64=%v, amd64=%v", err1, err2)
	}
	if len(a) != len(b) {
		t.Errorf("x86_64 and amd64 should resolve to same binary, got %d vs %d bytes", len(a), len(b))
	}

	// aarch64 and arm64 must resolve to the same binary.
	c, err3 := GetRocksDBViewerBinary("aarch64")
	d, err4 := GetRocksDBViewerBinary("arm64")
	if err3 != nil || err4 != nil {
		if errors.Is(err3, ErrRocksDBViewerEmpty) && errors.Is(err4, ErrRocksDBViewerEmpty) {
			t.Skip("placeholders — skipping alias check")
		}
		t.Fatalf("errors: aarch64=%v, arm64=%v", err3, err4)
	}
	if len(c) != len(d) {
		t.Errorf("aarch64 and arm64 should resolve to same binary, got %d vs %d bytes", len(c), len(d))
	}
}
```

- [ ] **Step 4: Run tests**

```bash
go test -short ./cmd/local/rockscollect/...
```

Expected: all PASS (binaries are real, not placeholders).

- [ ] **Step 5: Build binary to verify embed compiles**

```bash
go build -o bin/ddc.exe .
```

Expected: build succeeds.

---

### Task 2: Add Config Keys and Defaults for queries-perf

**Files:**
- Modify: `cmd/local/conf/conf_key_names.go:41` (rename key, add new keys)
- Modify: `cmd/local/conf/defaults.go:102-150` (standard profile), `defaults.go:59-100` (diagnosis profile)
- Modify: `cmd/local/conf/defaults_test.go`

- [ ] **Step 1: Rename queries-json key and add new keys in conf_key_names.go**

In `cmd/local/conf/conf_key_names.go`, rename and add:

```go
// Replace this line:
KeyDremioQueriesJSONNumDays    = "dremio-queries-json-num-days"

// With:
KeyQueriesJSONNumDays          = "queries-json-num-days"
KeyCollectQueriesPerfJSON      = "collect-queries-perf-json"
KeyQueriesPerfNumDays          = "queries-perf-num-days"
```

- [ ] **Step 2: Fix all references to the renamed key**

Search and replace `KeyDremioQueriesJSONNumDays` → `KeyQueriesJSONNumDays` across all files:
- `cmd/local/conf/defaults.go` (2 occurrences: standard and diagnosis profiles)
- `cmd/root.go` (~3 occurrences: flag registration, BuildConfData)
- `cmd/local/conf/conf_test.go` (key string in test map)

Also update the flag string literal `"dremio-queries-json-num-days"` → `"queries-json-num-days"` in `cmd/root_flags_test.go:8`.

- [ ] **Step 3: Add queries-perf defaults**

In `cmd/local/conf/defaults.go`, add to `StandardCollectionProfile` (around line 130):

```go
setDefault(confData, KeyCollectQueriesPerfJSON, true)
setDefault(confData, KeyQueriesPerfNumDays, 30)
```

In `DiagnosisCollectionProfile` (around line 90):

```go
setDefault(confData, KeyCollectQueriesPerfJSON, true)
```

- [ ] **Step 4: Update defaults_test.go**

Add assertions for the new keys in `TestSetViperDefaultsWithStandard` and `TestSetViperDefaultsWithDiagnosis`:

```go
// In standard checks:
{conf.KeyCollectQueriesPerfJSON, true},
{conf.KeyQueriesPerfNumDays, 30},

// In diagnosis checks:
{conf.KeyCollectQueriesPerfJSON, true},
```

Also update the renamed key in any existing test assertions from `conf.KeyDremioQueriesJSONNumDays` to `conf.KeyQueriesJSONNumDays`, and the string literal in `conf_test.go` from `"dremio-queries-json-num-days"` to `"queries-json-num-days"`.

- [ ] **Step 5: Run tests**

```bash
go test -short ./cmd/local/conf/...
```

Expected: all PASS.

- [ ] **Step 6: Build binary**

```bash
go build -o bin/ddc.exe .
```

Expected: build succeeds (all renamed references compile).

---

### Task 3: Add queries-perf Fields to CollectionArgs and Root Flags

**Files:**
- Modify: `cmd/root/collection/collector.go:69-128` (Args struct)
- Modify: `cmd/root.go:57-128` (flag variables), `cmd/root.go:1188-1235` (flag registration), `cmd/root.go:510-543` (BuildConfData)
- Modify: `cmd/root_flags_test.go`

- [ ] **Step 1: Add fields to Args struct**

In `cmd/root/collection/collector.go`, add to the Args struct after `QueriesJSONNumDays int` (around line 88):

```go
// queries-perf collection
CollectQueriesPerf    bool
QueriesPerfNumDays    int
```

- [ ] **Step 2: Add flag variables in root.go**

In `cmd/root.go`, add after `queriesJSONNumDays int` (around line 107):

```go
queriesPerfNumDays int
collectQueriesPerf bool
```

- [ ] **Step 3: Register flags on standard commands**

In `cmd/root.go` around line 1188, in the standard commands loop, add:

```go
cmd.Flags().BoolVar(&collectQueriesPerf, "collect-queries-perf-json", conf.GetBoolDefault(stdDef, conf.KeyCollectQueriesPerfJSON), "collect queries performance data from RocksDB")
cmd.Flags().IntVar(&queriesPerfNumDays, conf.KeyQueriesPerfNumDays, conf.GetIntDefault(stdDef, conf.KeyQueriesPerfNumDays), "number of days of queries performance data to collect")
```

- [ ] **Step 4: Register flag on diagnosis commands**

In `cmd/root.go` around line 1197, in the diagnosis commands loop, add:

```go
cmd.Flags().BoolVar(&collectQueriesPerf, "collect-queries-perf-json", conf.GetBoolDefault(diagDef, conf.KeyCollectQueriesPerfJSON), "collect queries performance data from RocksDB")
```

- [ ] **Step 5: Rename the queries-json flag registration**

In `cmd/root.go` around line 1214, change:

```go
// Old:
cmd.Flags().IntVar(&queriesJSONNumDays, conf.KeyDremioQueriesJSONNumDays, ...

// New:
cmd.Flags().IntVar(&queriesJSONNumDays, conf.KeyQueriesJSONNumDays, ...
```

(The `conf.KeyDremioQueriesJSONNumDays` was already renamed to `conf.KeyQueriesJSONNumDays` in Task 2.)

- [ ] **Step 6: Wire into BuildConfData**

In `cmd/root.go` `BuildConfData` function (around line 542), add after `confData[conf.KeyDremioQueriesJSONNumDays]`:

```go
confData[conf.KeyCollectQueriesPerfJSON] = collectQueriesPerf
if collectionMode == collects.StandardCollection {
	confData[conf.KeyQueriesPerfNumDays] = queriesPerfNumDays
}
```

Also rename `conf.KeyDremioQueriesJSONNumDays` → `conf.KeyQueriesJSONNumDays` in the existing line.

- [ ] **Step 7: Update root_flags_test.go**

In `cmd/root_flags_test.go`, rename key and add new test:

```go
var perLogNumDaysFlags = []string{
	"queries-json-num-days",  // renamed from dremio-queries-json-num-days
	"server-logs-num-days",
	"tracker-json-num-days",
	"vacuum-log-num-days",
	"queries-perf-num-days",  // new
}
```

Add test for `--collect-queries-perf-json` on both modes:

```go
func TestQueriesPerfOnBothModes(t *testing.T) {
	if SSHStandardCmd.Flags().Lookup("collect-queries-perf-json") == nil {
		t.Error("SSHStandardCmd should have --collect-queries-perf-json")
	}
	if SSHDiagnosisCmd.Flags().Lookup("collect-queries-perf-json") == nil {
		t.Error("SSHDiagnosisCmd should have --collect-queries-perf-json")
	}
}
```

Add test that `--queries-perf-num-days` is standard-only:

```go
func TestQueriesPerfNumDaysOnlyOnStandard(t *testing.T) {
	if SSHStandardCmd.Flags().Lookup("queries-perf-num-days") == nil {
		t.Error("SSHStandardCmd should have --queries-perf-num-days")
	}
	if SSHDiagnosisCmd.Flags().Lookup("queries-perf-num-days") != nil {
		t.Error("SSHDiagnosisCmd should not have --queries-perf-num-days")
	}
}
```

- [ ] **Step 8: Run tests**

```bash
go test -short ./cmd/...
```

Expected: all PASS.

- [ ] **Step 9: Build binary**

```bash
go build -o bin/ddc.exe .
```

---

### Task 4: RocksDB Viewer Execution and File Splitting

**Files:**
- Create: `cmd/root/collection/rockscollect.go`
- Create: `cmd/root/collection/rockscollect_test.go`

- [ ] **Step 1: Write the file splitter test**

Create `cmd/root/collection/rockscollect_test.go`:

```go
package collection

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSplitWriter_SingleFile(t *testing.T) {
	dir := t.TempDir()
	sw := newSplitWriter(dir, "queries-perf", 1024*1024) // 1MB limit for test
	defer sw.Close()

	line := strings.Repeat("x", 100) + "\n"
	for i := 0; i < 50; i++ {
		if _, err := sw.Write([]byte(line)); err != nil {
			t.Fatalf("write failed: %v", err)
		}
	}
	sw.Close()

	files, _ := filepath.Glob(filepath.Join(dir, "queries-perf-*.json"))
	if len(files) != 1 {
		t.Errorf("expected 1 file, got %d", len(files))
	}
}

func TestSplitWriter_MultipleSplits(t *testing.T) {
	dir := t.TempDir()
	sw := newSplitWriter(dir, "queries-perf", 500) // 500 byte limit
	defer sw.Close()

	line := strings.Repeat("a", 99) + "\n" // 100 bytes per line
	for i := 0; i < 20; i++ { // 2000 bytes total → expect 4 files
		if _, err := sw.Write([]byte(line)); err != nil {
			t.Fatalf("write failed: %v", err)
		}
	}
	sw.Close()

	files, _ := filepath.Glob(filepath.Join(dir, "queries-perf-*.json"))
	if len(files) != 4 {
		t.Errorf("expected 4 files, got %d", len(files))
	}

	// Verify no partial lines
	for _, f := range files {
		data, _ := os.ReadFile(f)
		content := string(data)
		if content != "" && !strings.HasSuffix(content, "\n") {
			t.Errorf("file %s does not end with newline", f)
		}
	}
}

func TestSplitWriter_EmptyInput(t *testing.T) {
	dir := t.TempDir()
	sw := newSplitWriter(dir, "queries-perf", 1024)
	sw.Close()

	files, _ := filepath.Glob(filepath.Join(dir, "queries-perf-*.json"))
	if len(files) != 0 {
		t.Errorf("expected 0 files for empty input, got %d", len(files))
	}
}
```

- [ ] **Step 2: Write the rockscollect module**

Create `cmd/root/collection/rockscollect.go`:

```go
package collection

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/local/rockscollect"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/consoleprint"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/simplelog"
)

const (
	rocksdbViewerRemotePath = "/tmp/dremio-rocksdb-viewer"
	queriesPerfSplitSize    = 128 * 1024 * 1024 // 128 MB
)

// splitWriter writes lines to sequentially numbered files, splitting at a size threshold.
type splitWriter struct {
	dir      string
	prefix   string
	maxBytes int64
	seq      int
	written  int64
	file     *os.File
	buf      *bufio.Writer
}

func newSplitWriter(dir, prefix string, maxBytes int64) *splitWriter {
	return &splitWriter{dir: dir, prefix: prefix, maxBytes: maxBytes}
}

// Write implements a line-aware writer. Each call should pass one complete line
// (ending with \n). The writer splits between lines when the size threshold is
// exceeded.
func (sw *splitWriter) Write(p []byte) (int, error) {
	if sw.file == nil {
		if err := sw.rotate(); err != nil {
			return 0, err
		}
	}
	if sw.written >= sw.maxBytes {
		if err := sw.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := sw.buf.Write(p)
	sw.written += int64(n)
	return n, err
}

func (sw *splitWriter) rotate() error {
	if sw.buf != nil {
		sw.buf.Flush()
	}
	if sw.file != nil {
		sw.file.Close()
	}
	sw.seq++
	path := filepath.Join(sw.dir, fmt.Sprintf("%s-%d.json", sw.prefix, sw.seq))
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create split file %s: %w", path, err)
	}
	sw.file = f
	sw.buf = bufio.NewWriterSize(f, 8*1024*1024)
	sw.written = 0
	return nil
}

func (sw *splitWriter) Close() error {
	if sw.buf != nil {
		sw.buf.Flush()
	}
	if sw.file != nil {
		return sw.file.Close()
	}
	return nil
}

// RocksCollectArgs holds arguments for a rocksdb-viewer collection run.
type RocksCollectArgs struct {
	Collector    Collector
	CopyStrategy CopyStrategy
	Host         string
	NodeType     string // "coordinator"
	RocksDBDir   string // value of --dremio-rocksdb-dir
	// What to collect
	CollectSystemTables bool
	SystemTables        []string
	CollectWLM          bool
	CollectQueriesPerf  bool
	QueriesPerfDays     int    // standard mode: from --queries-perf-num-days
	Days                int    // diagnosis mode: from --days
	DateStart           string // diagnosis mode: from --date-start
	DateEnd             string // diagnosis mode: from --date-end
}

// wlmTypes lists the WLM data types available in dremio-rocksdb-viewer.
var wlmTypes = []string{"wlm_queues", "wlm_rules", "wlm_engines", "wlm_cluster_usage"}

// RunRocksDBCollection uploads rocksdb-viewer to the coordinator, executes it
// for each requested data type, and writes output to the local temp directory.
func RunRocksDBCollection(args RocksCollectArgs) error {
	c := args.Collector
	host := args.Host
	dbPath := args.RocksDBDir + "/catalog"

	// Upload binary
	consoleprint.UpdateNodeState(consoleprint.NodeState{
		Node:     host,
		StatusUX: "Uploading rocksdb-viewer",
	})

	archStr, err := c.HostExecute(false, host, "uname", "-m")
	if err != nil {
		return fmt.Errorf("uname -m on %s: %w", host, err)
	}
	archStr = strings.TrimSpace(archStr)

	bin, err := rockscollect.GetRocksDBViewerBinary(archStr)
	if err != nil {
		return fmt.Errorf("rocksdb-viewer binary for %s: %w", archStr, err)
	}

	localTmp, err := os.MkdirTemp("", "ddc-rocksdb-dist-*")
	if err != nil {
		return fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(localTmp)

	localBin := filepath.Join(localTmp, "dremio-rocksdb-viewer")
	if err := os.WriteFile(localBin, bin, 0700); err != nil {
		return fmt.Errorf("write local binary: %w", err)
	}

	if _, err := c.CopyToHost(host, localBin, rocksdbViewerRemotePath); err != nil {
		return fmt.Errorf("upload rocksdb-viewer to %s: %w", host, err)
	}
	if _, err := c.HostExecute(false, host, "chmod", "+x", rocksdbViewerRemotePath); err != nil {
		return fmt.Errorf("chmod rocksdb-viewer on %s: %w", host, err)
	}

	// Deferred cleanup
	defer func() {
		if _, err := c.HostExecute(false, host, "rm", "-f", rocksdbViewerRemotePath); err != nil {
			simplelog.Warningf("rocksdb cleanup on %s: %v", host, err)
		}
	}()

	// Collect cluster_stats
	if err := collectRocksType(c, args.CopyStrategy, host, args.NodeType, dbPath, "cluster_stats", "system-tables", "cluster-stats.json", nil); err != nil {
		simplelog.Errorf("rocksdb cluster_stats on %s: %v", host, err)
	}

	// Collect system tables
	if args.CollectSystemTables {
		consoleprint.UpdateNodeState(consoleprint.NodeState{
			Node:     host,
			StatusUX: "Collecting system tables from RocksDB",
		})
		for _, table := range args.SystemTables {
			fname := fmt.Sprintf("%s.json", table)
			if err := collectRocksType(c, args.CopyStrategy, host, args.NodeType, dbPath, table, "system-tables", fname, nil); err != nil {
				simplelog.Errorf("rocksdb %s on %s: %v", table, host, err)
			}
		}
	}

	// Collect WLM
	if args.CollectWLM {
		consoleprint.UpdateNodeState(consoleprint.NodeState{
			Node:     host,
			StatusUX: "Collecting WLM from RocksDB",
		})
		for _, wt := range wlmTypes {
			fname := fmt.Sprintf("%s.json", wt)
			if err := collectRocksType(c, args.CopyStrategy, host, args.NodeType, dbPath, wt, "wlm", fname, nil); err != nil {
				simplelog.Errorf("rocksdb %s on %s: %v", wt, host, err)
			}
		}
	}

	// Collect queries-perf
	if args.CollectQueriesPerf {
		consoleprint.UpdateNodeState(consoleprint.NodeState{
			Node:     host,
			StatusUX: "Collecting queries-perf from RocksDB",
		})
		if err := collectQueriesPerf(c, args.CopyStrategy, host, args.NodeType, dbPath, args); err != nil {
			simplelog.Errorf("rocksdb queries_perf on %s: %v", host, err)
		}
	}

	return nil
}

// collectRocksType invokes rocksdb-viewer for a single type and writes the
// output to a single file under the given strategy directory.
func collectRocksType(c Collector, cs CopyStrategy, host, nodeType, dbPath, dataType, strategyType, filename string, extraArgs []string) error {
	cmdArgs := []string{rocksdbViewerRemotePath, "-db", dbPath, "-type", dataType}
	cmdArgs = append(cmdArgs, extraArgs...)

	// Join as a single command string for SSH compatibility (no sh -c wrapping).
	cmdStr := strings.Join(cmdArgs, " ")
	out, err := c.HostExecute(false, host, cmdStr)
	if err != nil {
		return fmt.Errorf("execute rocksdb-viewer -type %s: %w", dataType, err)
	}

	if strings.TrimSpace(out) == "" {
		simplelog.Infof("rocksdb-viewer -type %s returned empty output on %s", dataType, host)
		return nil
	}

	destDir, err := cs.CreatePath(strategyType, host, nodeType)
	if err != nil {
		return fmt.Errorf("create path for %s: %w", strategyType, err)
	}
	destPath := filepath.Join(destDir, filename)
	if err := os.WriteFile(destPath, []byte(out), 0600); err != nil {
		return fmt.Errorf("write %s: %w", destPath, err)
	}

	simplelog.Infof("rocksdb-viewer: collected %s → %s (%d bytes)", dataType, destPath, len(out))
	return nil
}

// collectQueriesPerf streams queries_perf output and splits into 128MB files.
func collectQueriesPerf(c Collector, cs CopyStrategy, host, nodeType, dbPath string, args RocksCollectArgs) error {
	cmdArgs := []string{rocksdbViewerRemotePath, "-db", dbPath, "-type", "queries_perf"}

	// Date filtering: prefer days, then date range
	days := args.QueriesPerfDays
	if days == 0 {
		days = args.Days
	}
	if days > 0 {
		cmdArgs = append(cmdArgs, "-days", fmt.Sprintf("%d", days))
	} else if args.DateStart != "" {
		cmdArgs = append(cmdArgs, "-date-start", args.DateStart)
		if args.DateEnd != "" {
			cmdArgs = append(cmdArgs, "-date-end", args.DateEnd)
		}
	}

	cmdStr := strings.Join(cmdArgs, " ")
	out, err := c.HostExecute(false, host, cmdStr)
	if err != nil {
		return fmt.Errorf("execute rocksdb-viewer queries_perf: %w", err)
	}

	if strings.TrimSpace(out) == "" {
		simplelog.Infof("rocksdb-viewer queries_perf returned empty output on %s", host)
		return nil
	}

	destDir, err := cs.CreatePath("queries-perf", host, nodeType)
	if err != nil {
		return fmt.Errorf("create path for queries-perf: %w", err)
	}

	sw := newSplitWriter(destDir, "queries-perf", queriesPerfSplitSize)
	defer sw.Close()

	scanner := bufio.NewScanner(strings.NewReader(out))
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024) // 10MB max line
	for scanner.Scan() {
		line := scanner.Text() + "\n"
		if _, err := sw.Write([]byte(line)); err != nil {
			return fmt.Errorf("write queries-perf split: %w", err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan queries-perf output: %w", err)
	}

	simplelog.Infof("rocksdb-viewer: collected queries_perf → %s (%d files)", destDir, sw.seq)
	return nil
}

// readOutputLines is a helper that reads HostExecute output as an io.Reader.
func readOutputLines(out string) io.Reader {
	return strings.NewReader(out)
}
```

- [ ] **Step 3: Run tests**

```bash
go test -short ./cmd/root/collection/... -run TestSplitWriter
```

Expected: all 3 split writer tests PASS.

- [ ] **Step 4: Build binary**

```bash
go build -o bin/ddc.exe .
```

---

### Task 5: Standard Mode TUI Refactoring

**Files:**
- Modify: `cmd/configui/configui.go:58-95` (StandardConfig struct), `configui.go:386-407` (remove pages), `configui.go:691-734` (add queries-perf), `configui.go:819-870` (generated CLI)
- Modify: `cmd/configui/configui_test.go`
- Modify: `cmd/root.go:1347-1383` (config mapping)

- [ ] **Step 1: Update StandardConfig struct**

In `cmd/configui/configui.go`, modify the `StandardConfig` struct (lines 58-95):

Remove fields:
```go
DremioEndpoint       string
PATToken             string
CollectKVStore       bool
```

Add field after `QueriesJSONDays int`:
```go
CollectQueriesPerf bool
QueriesPerfDays    int
```

Keep `CollectWLM`, `CollectSystemTables`, `SystemTables` — these are still collected via rocksdb-viewer, just not PAT-gated.

- [ ] **Step 2: Remove "API-based collection" and "PAT-enabled collections" pages**

In `cmd/configui/configui.go`, remove the "API-based collection" group (lines 386-401) and the "PAT-enabled collections" group (lines 403-407) from the standard mode form builder. The WLM, system tables, and container logs toggles should remain but move — add them to the "Log collection" page group.

- [ ] **Step 3: Add queries-perf selector to "Log collection"**

In `buildStandardLogGroup` (lines 691-734), add a `queries-perf` selector after the queries.json selector (after line 701):

```go
huh.NewSelect[int]().
	Title("queries-perf").
	OptionsFunc(func() []huh.Option[int] {
		return []huh.Option[int]{
			huh.NewOption("Collect (30 days)", 30).Selected(cfg.QueriesPerfDays == 30),
			huh.NewOption("Collect (14 days)", 14).Selected(cfg.QueriesPerfDays == 14),
			huh.NewOption("Collect (7 days)", 7).Selected(cfg.QueriesPerfDays == 7),
			huh.NewOption("Collect (3 days)", 3).Selected(cfg.QueriesPerfDays == 3),
			huh.NewOption("Collect (1 day)", 1).Selected(cfg.QueriesPerfDays == 1),
			huh.NewOption("Skip", 0).Selected(cfg.QueriesPerfDays == 0),
		}
	}, cfg).
	Value(&cfg.QueriesPerfDays),
```

- [ ] **Step 4: Update generated CLI command**

In `buildStandardCLICommand` (lines 819-870):

Remove the PAT-dependent sections (lines 853-867: `--collect-wlm`, `--collect-kvstore-report`, endpoint, and PAT lines).

Add after the queries.json line (line 843):
```go
if cfg.QueriesPerfDays > 0 {
	parts = append(parts, fmt.Sprintf("  --collect-queries-perf-json=true --queries-perf-num-days=%d"+cont, cfg.QueriesPerfDays))
} else {
	parts = append(parts, "  --collect-queries-perf-json=false"+cont)
}
```

Update the queries.json line to use the renamed flag:
```go
// Old:
parts = append(parts, fmt.Sprintf("  --collect-queries-json=true --dremio-queries-json-num-days=%d"+cont, queryDays))
// New:
parts = append(parts, fmt.Sprintf("  --collect-queries-json=true --queries-json-num-days=%d"+cont, queryDays))
```

Add WLM and system tables flags (no longer PAT-gated):
```go
parts = append(parts, fmt.Sprintf("  --collect-wlm=%t"+cont, cfg.CollectWLM))
if len(cfg.SystemTables) > 0 {
	parts = append(parts, fmt.Sprintf("  --system-tables=%s", strings.Join(cfg.SystemTables, ",")))
}
```

Update the function signature to no longer need PAT-related parameters. Remove trailing continuation cleanup that was keyed on `cfg.PATToken`.

- [ ] **Step 5: Update config mapping in root.go**

In `cmd/root.go` `runStandardConfigScreen` (lines 1354-1383):

Remove:
```go
dremioEndpoint = cfg.DremioEndpoint
if cfg.PATToken != "" {
	cliAuthToken = cfg.PATToken
}
collectKVStoreReport = cfg.CollectKVStore
```

Add:
```go
collectQueriesPerf = cfg.CollectQueriesPerf
queriesPerfNumDays = cfg.QueriesPerfDays
```

- [ ] **Step 6: Remove standard-mode-only flags from root.go**

In `cmd/root.go`, in the standard commands flag registration loop (around line 1188), remove:
```go
cmd.Flags().BoolVar(&collectKVStoreReport, "collect-kvstore-report", ...)
```

The `--dremio-pat-token` and `--dremio-endpoint` flags are registered on `CollectCmd.PersistentFlags()` (line 1180-1181). They need to be moved from `CollectCmd.PersistentFlags()` to only the diagnosis command loop. Check whether `--allow-insecure-ssl` and `--dremio-endpoint` are registered as persistent on `CollectCmd` — if so, move them to diagnosis-only registration.

- [ ] **Step 7: Update configui_test.go**

Update `TestBuildStandardCLICommand_*` tests to:
- Remove PAT/endpoint assertions
- Add `--collect-queries-perf-json` and `--queries-perf-num-days` assertions
- Add `--queries-json-num-days` (renamed) assertions
- Add `CollectQueriesPerf` and `QueriesPerfDays` to test StandardConfig

- [ ] **Step 8: Run tests**

```bash
go test -short ./cmd/configui/... && go test -short ./cmd/...
```

Expected: all PASS.

- [ ] **Step 9: Build binary**

```bash
go build -o bin/ddc.exe .
```

---

### Task 6: Wire RocksDB Collection into Standard Mode Streaming

**Files:**
- Modify: `cmd/root/collection/streaming_collect.go:957-1027` (orchestrator goroutine)
- Modify: `cmd/root.go` (wire Args fields)

- [ ] **Step 1: Wire new Args fields in root.go**

In `cmd/root.go`, where `collectionArgs` is assembled (search for `CollectQueriesJSON:`), add:

```go
CollectQueriesPerf:    collectQueriesPerf,
QueriesPerfNumDays:    queriesPerfNumDays,
```

- [ ] **Step 2: Add rocksdb-viewer call to coordinator streaming phase**

In `cmd/root/collection/streaming_collect.go`, add a new phase after node file streaming completes but within the per-node goroutines (or as a separate coordinator-scoped step). The key location is the orchestrator goroutine (around line 957).

Replace the API collection goroutine (lines 970-1018) for standard mode with rocksdb-viewer. The existing code checks `collectionArgs.DremioEndpoint != ""` and `collectionArgs.DremioPAT != ""` to gate API calls. For standard mode, replace with:

```go
orchestratorWg.Add(1)
go func() {
	defer orchestratorWg.Done()
	if len(coordinators) == 0 || collectionArgs.DremioRocksDBDir == "" {
		simplelog.Warningf("rocksdb collection skipped: no coordinator or rocksdb dir not set")
		return
	}
	rocksArgs := RocksCollectArgs{
		Collector:           c,
		CopyStrategy:        s,
		Host:                coordinators[0],
		NodeType:            "coordinator",
		RocksDBDir:          collectionArgs.DremioRocksDBDir,
		CollectSystemTables: collectionArgs.CollectSystemTables,
		SystemTables:        collectionArgs.SystemTables,
		CollectWLM:          collectionArgs.CollectWLM,
		CollectQueriesPerf:  collectionArgs.CollectQueriesPerf,
		QueriesPerfDays:     collectionArgs.QueriesPerfNumDays,
		Days:                collectionArgs.DiagLogDays,
		DateStart:           "", // standard mode has no date-start
		DateEnd:             "",
	}
	if err := RunRocksDBCollection(rocksArgs); err != nil {
		simplelog.Errorf("RocksDB collection failed: %v", err)
	}
}()
```

Keep the cluster collection call (`clusterCollection()`) in its own goroutine as-is (it collects K8s metadata).

For standard mode, the existing API collection code paths (`RunCollectWLM`, `RunCollectSystemTables`, `RunCollectClusterStats`) should be gated to only run in diagnosis mode, or removed from this path entirely. Guard the existing API block with a mode check:

```go
if collectionArgs.CollectionMode == collects.DiagnosisCollection {
	// existing API collection code...
}
```

- [ ] **Step 3: Run tests**

```bash
go test -short ./cmd/root/collection/...
```

Expected: PASS.

- [ ] **Step 4: Build binary**

```bash
go build -o bin/ddc.exe .
```

---

### Task 7: Diagnosis Mode TUI Refactoring

**Files:**
- Modify: `cmd/configui/configui.go:97-150` (DiagnosisConfig struct), `configui.go:557-618` (TUI pages), `configui.go:738-768` (diagnostics group), `configui.go:896-959` (generated CLI)
- Modify: `cmd/configui/configui_test.go`
- Modify: `cmd/root.go:1442-1484` (config mapping)

- [ ] **Step 1: Update DiagnosisConfig struct**

Add field:
```go
CollectQueriesPerf bool
```

- [ ] **Step 2: Rename "Date range" → "Logs Collection"**

In `configui.go` around line 605, change the group title from `"Date range"` to `"Logs Collection"`.

- [ ] **Step 3: Move log types from "Diagnostic Collection" to "Logs Collection"**

Move the log types multi-select (lines 750-762 in `buildDiagnosticsCollectionGroup`) and the K8s container logs toggle (lines 764-765) into the "Logs Collection" page group.

Also move from "API-based collection" to "Logs Collection":
- "Collect problematic job profiles" toggle (change default to `false`)

Also move from "PAT-enabled collections" to "Logs Collection":
- "Collect KV store report" toggle

- [ ] **Step 4: Rename "PAT-enabled collections" → "Dremio System Tables & WLM"**

Change the group title at line 576 from `"PAT-enabled collections"` to `"Dremio System Tables & WLM"`.

Remove the `WithHideFunc` that hides when PAT is empty (line 580) — this page is no longer PAT-gated.

- [ ] **Step 5: Make "API-based collection" page conditional**

Add a `WithHideFunc` to the "API-based collection" group (around line 573) that hides the page unless `cfg.CollectProblematicProfiles` or `cfg.CollectKVStore` is true:

```go
.WithHideFunc(func() bool {
	return !cfg.CollectProblematicProfiles && !cfg.CollectKVStore
})
```

When the page IS shown, make the PAT token field required (add validation that it's non-empty).

- [ ] **Step 6: Remove "Diagnostic Collection" page**

The "Diagnostic Collection" page's contents have been moved. Remove the page group. The diagnostic tools (JFR, jstack, top, async-profiler, heap dump) stay where they are — they live in their own separate section, not in "Diagnostic Collection". Only the log types and container logs moved.

- [ ] **Step 7: Update generated CLI command**

In `buildDiagnosisCLICommand` (lines 896-959):

Add `--collect-queries-perf-json` flag:
```go
parts = append(parts, fmt.Sprintf("  --collect-queries-perf-json=%t"+cont, cfg.CollectQueriesPerf))
```

Ensure WLM and system tables flags no longer require PAT in the generated output.

- [ ] **Step 8: Update config mapping in root.go**

In `cmd/root.go` `runDiagnosisConfigScreen` (lines 1442-1484), add:
```go
collectQueriesPerf = cfg.CollectQueriesPerf
```

- [ ] **Step 9: Update configui_test.go**

Update diagnosis CLI tests with new field, check for `--collect-queries-perf-json`.

- [ ] **Step 10: Run tests**

```bash
go test -short ./cmd/configui/... && go test -short ./cmd/...
```

Expected: all PASS.

- [ ] **Step 11: Build binary**

```bash
go build -o bin/ddc.exe .
```

---

### Task 8: Wire RocksDB Collection into Diagnosis Mode

**Files:**
- Modify: `cmd/root/collection/streaming_collect.go:957-1027`
- Modify: `cmd/root.go` (wire diagnosis config)

- [ ] **Step 1: Replace API collection with rocksdb-viewer for diagnosis mode**

In the orchestrator goroutine (streaming_collect.go lines 970-1018), the existing code already has the API collection block. Replace the `RunCollectWLM`, `RunCollectSystemTables`, and `RunCollectClusterStats` calls with a `RunRocksDBCollection` call (similar to Task 6), but keep `RunCollectKVStore` and the problematic profiles path gated behind PAT:

```go
// RocksDB collection (both modes)
if len(coordinators) > 0 && collectionArgs.DremioRocksDBDir != "" {
	rocksArgs := RocksCollectArgs{
		Collector:           c,
		CopyStrategy:        s,
		Host:                coordinators[0],
		NodeType:            "coordinator",
		RocksDBDir:          collectionArgs.DremioRocksDBDir,
		CollectSystemTables: collectionArgs.CollectSystemTables,
		SystemTables:        collectionArgs.SystemTables,
		CollectWLM:          collectionArgs.CollectWLM,
		CollectQueriesPerf:  collectionArgs.CollectQueriesPerf,
		QueriesPerfDays:     collectionArgs.QueriesPerfNumDays,
		Days:                collectionArgs.DiagLogDays,
		DateStart:           "", // will be set from collectionArgs below
		DateEnd:             "",
	}
	if err := RunRocksDBCollection(rocksArgs); err != nil {
		simplelog.Errorf("RocksDB collection failed: %v", err)
	}
}

// PAT-dependent collections (diagnosis mode only)
if collectionArgs.DremioPAT != "" && len(coordinators) > 0 {
	apiArgs := APICollectionArgs{
		TmpDir:           s.GetTmpDir(),
		CoordinatorNode:  coordinators[0],
		DremioEndpoint:   collectionArgs.DremioEndpoint,
		DremioPAT:        collectionArgs.DremioPAT,
		AllowInsecureSSL: collectionArgs.AllowInsecureSSL,
		RestHTTPTimeout:  collectionArgs.RestHTTPTimeout,
		Hook:             hook,
	}
	if collectionArgs.CollectKVStoreReport {
		if err := RunCollectKVStore(apiArgs); err != nil {
			simplelog.Errorf("KV store collection failed: %v", err)
		}
	}
}
```

- [ ] **Step 2: Wire date-start/date-end into RocksCollectArgs for diagnosis**

Add `DateStart` and `DateEnd` fields to the `Args` struct in `collector.go`:

```go
// Date range (diagnosis mode)
DateStart string
DateEnd   string
```

Wire them from `root.go` where `collectionArgs` is assembled:
```go
DateStart: startDate,
DateEnd:   endDate,
```

Then pass them through to `RocksCollectArgs`:
```go
DateStart: collectionArgs.DateStart,
DateEnd:   collectionArgs.DateEnd,
```

- [ ] **Step 3: Run tests**

```bash
go test -short ./cmd/root/collection/... && go test -short ./cmd/...
```

Expected: all PASS.

- [ ] **Step 4: Build binary**

```bash
go build -o bin/ddc.exe .
```

---

### Task 9: Remove Dead Code

**Files:**
- Modify: `cmd/root/collection/apicollect.go`
- Modify: `cmd/root/collection/streaming_collect.go` (remove old API call paths)
- Modify: `cmd/root.go` (clean up endpoint detection for standard mode)

- [ ] **Step 1: Remove dead functions from apicollect.go**

Delete these functions from `cmd/root/collection/apicollect.go`:
- `RunCollectClusterStats` (lines 57-137)
- `RunCollectWLM` (lines 140-185)
- `RunCollectSystemTables` (lines 223-266)
- Supporting helpers only used by deleted functions: `downloadSysTable`, `buildSQLForTable`, `waitForJobCompletion`, `retrieveAndSaveResults`, `formatSystemTableFilename`

Keep:
- `RunCollectKVStore` (lines 188-220)
- The `APICollectionArgs` struct (still used by KV store and profile collection)

- [ ] **Step 2: Remove standard mode endpoint autodetection**

In `cmd/root.go`, find the endpoint autodetection logic for standard mode (around line 1555 for local, line 1743 for SSH/K8s — the `detectedRocksDBDir` is fine, but the endpoint detection for standard mode can be removed if it's only used for API collection).

Check if the endpoint detection is shared with diagnosis mode. If so, gate it behind mode check. If standard-only, remove it.

- [ ] **Step 3: Remove `--collect-kvstore-report` description mentioning PAT from standard flags**

Already done in Task 5 step 6. Verify no standard-mode references remain.

- [ ] **Step 4: Clean up unused imports**

Run `go build` and `gofmt` to catch any unused imports from deleted code:

```bash
go fmt ./...
go vet ./...
go build -o bin/ddc.exe .
```

- [ ] **Step 5: Run full test suite**

```bash
go test -short ./...
```

Expected: all PASS.

- [ ] **Step 6: Build binary**

```bash
go build -o bin/ddc.exe .
```

---

### Task 10: Final Validation

**Files:** None (verification only)

- [ ] **Step 1: Run full test suite**

```bash
go test -short ./...
```

Expected: all PASS.

- [ ] **Step 2: Run linter**

```bash
go fmt ./...
golangci-lint run
```

Expected: no new violations.

- [ ] **Step 3: Build binary**

```bash
go build -o bin/ddc.exe .
```

Expected: binary builds successfully with embedded rocksdb-viewer binaries.

- [ ] **Step 4: Verify binary size is reasonable**

The embedded rocksdb-viewer binaries are ~2.7MB (amd64) + ~2.2MB (arm64) = ~4.9MB additional. Verify the final binary size reflects this.

```bash
ls -la bin/ddc.exe
```

- [ ] **Step 5: Verify flag help output**

```bash
bin/ddc.exe collect ssh standard --help
bin/ddc.exe collect ssh diagnosis --help
```

Verify:
- Standard mode: no `--dremio-pat-token`, `--dremio-endpoint`, `--allow-insecure-ssl`, `--collect-kvstore-report`
- Standard mode: has `--collect-queries-perf-json`, `--queries-perf-num-days`, `--queries-json-num-days` (renamed)
- Diagnosis mode: has `--collect-queries-perf-json`, retains PAT-related flags
- Both modes: `--collect-wlm` and `--system-tables` present (no longer PAT-gated)
