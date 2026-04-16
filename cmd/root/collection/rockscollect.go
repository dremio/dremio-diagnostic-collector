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
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/local/rockscollect"
	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/cli"
	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/helpers"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/consoleprint"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/simplelog"
)

const rocksdbViewerRemotePath = "/tmp/dremio-rocksdb-viewer"

// dateSplitWriter routes JSON lines to per-day files based on the start_time field.
// Records must arrive sorted by date (rocksdb-viewer guarantees this).
type dateSplitWriter struct {
	dir     string
	prefix  string
	curDate string
	dates   []string // ordered list of dates seen
	file    *os.File
	buf     *bufio.Writer
}

func newDateSplitWriter(dir, prefix string) *dateSplitWriter {
	return &dateSplitWriter{dir: dir, prefix: prefix}
}

// WriteLine writes a JSON line to the file for its date. Extracts the date from "start_time":"...".
func (dw *dateSplitWriter) WriteLine(line string) error {
	date := extractDate(line)
	if date != dw.curDate {
		if err := dw.switchFile(date); err != nil {
			return err
		}
	}
	_, err := dw.buf.WriteString(line)
	if err != nil {
		return fmt.Errorf("write queries-perf line: %w", err)
	}
	_, err = dw.buf.WriteString("\n")
	return err
}

func (dw *dateSplitWriter) switchFile(date string) error {
	if dw.buf != nil {
		_ = dw.buf.Flush()
	}
	if dw.file != nil {
		_ = dw.file.Close()
	}
	dw.curDate = date
	dw.dates = append(dw.dates, date)
	fname := fmt.Sprintf("%s.%s.json", dw.prefix, date)
	path := filepath.Join(dw.dir, fname)
	f, err := os.Create(path) // #nosec G304 -- path is derived from controlled internal dir + date
	if err != nil {
		return fmt.Errorf("create date file %s: %w", path, err)
	}
	dw.file = f
	dw.buf = bufio.NewWriterSize(f, 8*1024*1024)
	return nil
}

func (dw *dateSplitWriter) Close() error {
	if dw.buf != nil {
		_ = dw.buf.Flush()
	}
	if dw.file != nil {
		return dw.file.Close()
	}
	return nil
}

// extractDate pulls the date (YYYY-MM-DD) from the "query_start_epoch_ms" JSON field.
// The field value is a Unix epoch in milliseconds (numeric, no quotes).
// Falls back to "unknown" if the field is missing or unparseable.
func extractDate(line string) string {
	const key = `"query_start_epoch_ms":`
	idx := strings.Index(line, key)
	if idx < 0 {
		return "unknown"
	}
	start := idx + len(key)
	// Find the end of the numeric value (next comma, brace, or end of string)
	end := start
	for end < len(line) && line[end] != ',' && line[end] != '}' {
		end++
	}
	ms, err := strconv.ParseInt(strings.TrimSpace(line[start:end]), 10, 64)
	if err != nil {
		return "unknown"
	}
	return time.UnixMilli(ms).UTC().Format("2006-01-02")
}

// RocksCollectArgs holds arguments for a rocksdb-viewer collection run.
type RocksCollectArgs struct {
	Collector           Collector
	CopyStrategy        CopyStrategy
	Host                string
	NodeType            string // "coordinator"
	RocksDBDir          string // value of --dremio-rocksdb-dir
	CollectSystemTables bool
	SystemTables        []string
	CollectWLM          bool
	CollectQueriesPerf  bool
	QueriesPerfDays     int    // standard mode: from --queries-perf-num-days
	Days                int    // diagnosis mode: from --days
	StartDate           string // diagnosis mode (date-only, e.g. 2026-04-07)
}

var wlmTypes = []string{"wlm_queues", "wlm_rules", "wlm_engines", "wlm_cluster_usage"}

// RunRocksDBCollection runs the rocksdb-viewer on the coordinator node and returns
// the list of files collected (for inclusion in per-node file counts and byte totals).
func RunRocksDBCollection(args RocksCollectArgs) ([]helpers.CollectedFile, error) {
	c := args.Collector
	host := args.Host
	dbPath := args.RocksDBDir + "/catalog"
	var collected []helpers.CollectedFile

	// Upload binary
	consoleprint.UpdateNodeState(consoleprint.NodeState{
		Node:     host,
		StatusUX: "Uploading dremio-rocksdb-viewer",
	})

	archStr, err := c.HostExecute(false, host, "uname -m")
	if err != nil {
		return nil, fmt.Errorf("uname -m on %s: %w", host, err)
	}
	archStr = strings.TrimSpace(archStr)

	bin, err := rockscollect.GetRocksDBViewerBinary(archStr)
	if err != nil {
		return nil, fmt.Errorf("rocksdb-viewer binary for %s: %w", archStr, err)
	}

	localTmp, err := os.MkdirTemp("", "ddc-rocksdb-dist-*")
	if err != nil {
		return nil, fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(localTmp)

	localBin := filepath.Join(localTmp, "dremio-rocksdb-viewer")
	if err := os.WriteFile(localBin, bin, 0o600); err != nil {
		return nil, fmt.Errorf("write local binary: %w", err)
	}

	if _, err := c.CopyToHost(host, localBin, rocksdbViewerRemotePath); err != nil {
		return nil, fmt.Errorf("upload rocksdb-viewer to %s: %w", host, err)
	}
	if _, err := c.HostExecute(false, host, "chmod +x "+rocksdbViewerRemotePath); err != nil {
		return nil, fmt.Errorf("chmod rocksdb-viewer on %s: %w", host, err)
	}

	consoleprint.UpdateNodeState(consoleprint.NodeState{
		Node:     host,
		StatusUX: "rocksdb-viewer ready",
	})

	defer func() {
		consoleprint.UpdateNodeState(consoleprint.NodeState{
			Node:     host,
			StatusUX: "Cleaning up rocksdb-viewer",
		})
		if _, err := c.HostExecute(false, host, "rm -f "+rocksdbViewerRemotePath); err != nil {
			simplelog.Warningf("rocksdb cleanup on %s: %v", host, err)
		}
	}()

	// Collect cluster_stats
	consoleprint.UpdateNodeState(consoleprint.NodeState{
		Node:     host,
		StatusUX: "Collecting cluster stats from RocksDB",
	})
	if cf, err := collectRocksType(c, args.CopyStrategy, host, args.NodeType, dbPath, "cluster_stats", "cluster-stats", "cluster-stats.json"); err != nil {
		simplelog.Errorf("rocksdb cluster_stats on %s: %v", host, err)
	} else if cf != nil {
		collected = append(collected, *cf)
	}

	// Collect system tables
	if args.CollectSystemTables {
		for _, table := range args.SystemTables {
			viewerType := "sys." + table // rocksdb-viewer expects sys.version, sys.options, etc.
			consoleprint.UpdateNodeState(consoleprint.NodeState{
				Node:     host,
				StatusUX: fmt.Sprintf("Collecting system table: %s", viewerType),
			})
			fname := fmt.Sprintf("sys.%s.json", table)
			if cf, err := collectRocksType(c, args.CopyStrategy, host, args.NodeType, dbPath, viewerType, "system-tables", fname); err != nil {
				simplelog.Errorf("rocksdb %s on %s: %v", viewerType, host, err)
			} else if cf != nil {
				collected = append(collected, *cf)
			}
		}
	}

	// Collect WLM
	if args.CollectWLM {
		for _, wt := range wlmTypes {
			consoleprint.UpdateNodeState(consoleprint.NodeState{
				Node:     host,
				StatusUX: fmt.Sprintf("Collecting WLM: %s", wt),
			})
			fname := fmt.Sprintf("%s.json", wt)
			if cf, err := collectRocksType(c, args.CopyStrategy, host, args.NodeType, dbPath, wt, "wlm", fname); err != nil {
				simplelog.Errorf("rocksdb %s on %s: %v", wt, host, err)
			} else if cf != nil {
				collected = append(collected, *cf)
			}
		}
	}

	// Collect queries-perf
	if args.CollectQueriesPerf {
		consoleprint.UpdateNodeState(consoleprint.NodeState{
			Node:     host,
			StatusUX: "Collecting queries-perf from RocksDB",
		})
		if files, err := collectQueriesPerf(c, args.CopyStrategy, host, args.NodeType, dbPath, args); err != nil {
			simplelog.Errorf("rocksdb queries_perf on %s: %v", host, err)
		} else {
			collected = append(collected, files...)
		}
	}

	return collected, nil
}

func collectRocksType(c Collector, cs CopyStrategy, host, nodeType, dbPath, dataType, strategyType, filename string) (*helpers.CollectedFile, error) {
	cmdStr := fmt.Sprintf("%s -db %s -type %s", rocksdbViewerRemotePath, dbPath, dataType)
	out, err := c.HostExecute(false, host, cmdStr)
	if err != nil {
		return nil, fmt.Errorf("execute rocksdb-viewer -type %s: %w", dataType, err)
	}
	if strings.TrimSpace(out) == "" {
		simplelog.Infof("rocksdb-viewer -type %s returned empty output on %s", dataType, host)
		return nil, nil
	}
	destDir, err := cs.CreatePath(strategyType, host, nodeType)
	if err != nil {
		return nil, fmt.Errorf("create path for %s: %w", strategyType, err)
	}
	destPath := filepath.Join(destDir, filename)
	if err := os.WriteFile(destPath, []byte(out), 0600); err != nil {
		return nil, fmt.Errorf("write %s: %w", destPath, err)
	}
	size := int64(len(out))
	simplelog.Infof("rocksdb-viewer: collected %s -> %s (%d bytes)", dataType, destPath, size)
	return &helpers.CollectedFile{Path: destPath, Size: size}, nil
}

// buildQueriesPerfFilterArgs returns the day/date filter portion of the rocksdb-viewer command.
// Diagnosis-mode filters (Days / StartDate) take priority over the standard-mode
// QueriesPerfDays default, since they represent explicit user intent.
func buildQueriesPerfFilterArgs(args RocksCollectArgs) string {
	// Diagnosis mode: -start-date + -days, or just -days.
	if args.StartDate != "" {
		return fmt.Sprintf(" -start-date %s -days %d", args.StartDate, args.Days)
	}
	if args.Days > 0 {
		return fmt.Sprintf(" -days %d", args.Days)
	}
	// Standard mode: use the queries-perf-specific day count.
	if args.QueriesPerfDays > 0 {
		return fmt.Sprintf(" -days %d", args.QueriesPerfDays)
	}
	return ""
}

func collectQueriesPerf(c Collector, cs CopyStrategy, host, nodeType, dbPath string, args RocksCollectArgs) ([]helpers.CollectedFile, error) {
	filterArgs := buildQueriesPerfFilterArgs(args)
	simplelog.Infof("rocksdb-viewer queries_perf filter: QueriesPerfDays=%d Days=%d StartDate=%q filterArgs=%q", args.QueriesPerfDays, args.Days, args.StartDate, filterArgs)

	// Pre-flight: get total record count for progress display.
	consoleprint.UpdateNodeState(consoleprint.NodeState{
		Node:     host,
		StatusUX: "Getting number of queries for queries-perf",
	})
	countCmd := fmt.Sprintf("%s -db %s -type queries_perf -count%s", rocksdbViewerRemotePath, dbPath, filterArgs)
	countOut, err := c.HostExecute(false, host, countCmd)
	if err != nil {
		simplelog.Warningf("rocksdb-viewer queries_perf -count failed on %s: %v (continuing without progress)", host, err)
	}
	totalRecords, _ := strconv.Atoi(strings.TrimSpace(countOut))

	if totalRecords == 0 {
		consoleprint.UpdateNodeState(consoleprint.NodeState{
			Node:     host,
			StatusUX: "Collecting queries-perf data",
		})
	} else {
		consoleprint.UpdateNodeState(consoleprint.NodeState{
			Node:     host,
			StatusUX: fmt.Sprintf("Collecting queries-perf data (0 of %d queries)     [0%%]", totalRecords),
		})
	}

	destDir, err := cs.CreatePath("queries-perf", host, nodeType)
	if err != nil {
		return nil, fmt.Errorf("create path for queries-perf: %w", err)
	}

	dw := newDateSplitWriter(destDir, "queries-perf")
	defer dw.Close()

	var mu sync.Mutex
	var writeErr error
	lineCount := 0

	handler := func(line string) {
		mu.Lock()
		defer mu.Unlock()
		if writeErr != nil {
			return
		}
		if err := dw.WriteLine(line); err != nil {
			writeErr = fmt.Errorf("write queries-perf: %w", err)
			return
		}
		lineCount++
		if totalRecords > 0 && (lineCount%100 == 0 || lineCount >= totalRecords) {
			if lineCount >= totalRecords {
				consoleprint.UpdateNodeState(consoleprint.NodeState{
					Node:     host,
					StatusUX: fmt.Sprintf("Finalizing queries-perf data (%d queries)", lineCount),
				})
			} else {
				pct := lineCount * 100 / totalRecords
				consoleprint.UpdateNodeState(consoleprint.NodeState{
					Node:     host,
					StatusUX: fmt.Sprintf("Collecting queries-perf data (%d of %d)     [%d%%]", lineCount, totalRecords, pct),
				})
			}
		}
	}

	dataCmd := fmt.Sprintf("%s -db %s -type queries_perf%s", rocksdbViewerRemotePath, dbPath, filterArgs)
	if err := c.HostExecuteAndStream(false, host, cli.OutputHandler(handler), "", dataCmd); err != nil {
		return nil, fmt.Errorf("execute rocksdb-viewer queries_perf: %w", err)
	}
	if writeErr != nil {
		return nil, writeErr
	}

	// Final status update
	if totalRecords > 0 {
		consoleprint.UpdateNodeState(consoleprint.NodeState{
			Node:     host,
			StatusUX: fmt.Sprintf("Collecting queries-perf data (%d of %d)     [100%%]", lineCount, totalRecords),
		})
	} else if lineCount > 0 {
		consoleprint.UpdateNodeState(consoleprint.NodeState{
			Node:     host,
			StatusUX: fmt.Sprintf("Collected queries-perf data (%d queries)", lineCount),
		})
	}

	// Collect file info for all date files
	var collected []helpers.CollectedFile
	for _, date := range dw.dates {
		path := filepath.Join(destDir, fmt.Sprintf("queries-perf.%s.json", date))
		if fi, err := os.Stat(path); err == nil {
			collected = append(collected, helpers.CollectedFile{Path: path, Size: fi.Size()})
		}
	}

	simplelog.Infof("rocksdb-viewer: collected queries_perf -> %s (%d files, %d records)", destDir, len(dw.dates), lineCount)
	return collected, nil
}
