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
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/cli"
	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/helpers"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/clusterstats"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/collects"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/shutdown"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/simplelog"
)

var DirPerms fs.FileMode = 0o750

type CopyStrategy interface {
	CreatePath(fileType, source, nodeType string) (path string, err error)
	ArchiveDiag(o string, outputLoc string) error
	GetTmpDir() string
}

type Collector interface {
	CopyToHost(hostString string, source, destination string) (out string, err error)
	GetCoordinators() (podName []string, err error)
	GetExecutors() (podName []string, err error)
	HostExecute(mask bool, hostString string, args ...string) (stdOut string, err error)
	HostExecuteAndStream(mask bool, hostString string, output cli.OutputHandler, pat string, args ...string) error
	HelpText() string
	Name() string
	// Protocol returns the transport protocol in use, e.g. "WebSocket", "SPDY", "SSH", "Local".
	Protocol() string
	SetHostPid(host, pidFile string)
	CleanupRemote() error
	// StreamFromHost streams the raw bytes of a remote file to writer.
	// When useGzip is true the remote command uses "gzip -c" instead of "cat"
	// so the stream arrives compressed; the caller is responsible for decompression.
	// Implementations must preserve binary integrity (no line splitting or encoding).
	StreamFromHost(host, remotePath string, writer io.Writer, useGzip bool) error
	// DiscoverFiles runs lightweight shell commands on a remote host to enumerate
	// log files, config files, GC logs, and the Dremio PID. Individual command
	// failures are logged as warnings — partial results are always returned.
	// When logDir or confDir are non-empty, they override probing for that path.
	DiscoverFiles(host, logDir, confDir string) (*RemoteNodeInfo, error)
}

type Args struct {
	DDCfs                 helpers.Filesystem
	OutputLoc             string
	CopyStrategy          CopyStrategy
	DremioPAT             string
	Disabled              []string
	Enabled               []string
	DisableFreeSpaceCheck bool
	CollectionMode        collects.CollectionMode
	CollectionThreads     int
	CoordinatorLogDir     string
	ExecutorLogDir        string
	DremioConfDir         string
	DremioRocksDBDir      string

	// Per-log day counts (standard mode)
	ServerLogsNumDays  int
	TrackerJSONNumDays int
	VacuumLogNumDays   int
	QueriesJSONNumDays int

	// queries-perf collection
	CollectQueriesPerf bool
	QueriesPerfNumDays int

	// Diagnosis mode: unified day limit for all log types (from --days)
	DiagLogDays int

	// Start date (diagnosis mode, date-only format 2006-01-02)
	StartDate string

	// JVM collection (diagnosis mode)
	CollectJStack        bool
	CollectTop           bool
	CollectJVMFlags      bool
	CollectJFR           bool
	CollectHeapDump      bool
	CollectAsyncProfiler bool
	DiagTimeSeconds      int

	// Node filtering (diagnosis mode node selection or --nodes/--exclude-nodes flags)
	IncludeNodes []string // if non-empty, only collect from these nodes
	ExcludeNodes []string // if non-empty, skip these nodes

	// File collection gating
	CollectGCLogs          bool
	CollectServerLogs      bool
	CollectQueriesJSON     bool
	CollectTrackerJSON     bool
	CollectVacuumLog       bool
	CollectAccelerationLog bool
	CollectAccessLog       bool
	CollectHSErrFiles      bool
	CollectHiveDeprecated  bool

	// API collections (run from orchestrator)
	DremioEndpoint             string
	AllowInsecureSSL           bool
	RestHTTPTimeout            int
	CollectWLM                 bool
	CollectKVStoreReport       bool
	CollectProblematicProfiles bool
	CollectSystemTables        bool
	SystemTables               []string
}

func FilterCoordinators(coordinators []string) []string {
	// use a map for the unique key property, so we handle duplicates in the list
	filteredList := make(map[string]bool)
	for _, e := range coordinators {
		filteredList[e] = true
	}
	// convert back into a slice
	// again we are dealing with not
	// large enough sizes to warrant optimization
	var result []string
	for k := range filteredList {
		result = append(result, k)
	}
	// sort for consistent behavior and testing
	slices.Sort(result)
	slices.Reverse(result)
	return result
}

func FilterExecutors(executors []string, coordinators []string) []string {
	// use a map for the unique key property, so we handle duplicates in the list
	filteredList := make(map[string]bool)
	for _, e := range executors {
		// we're not going to bother optimizing this:
		// the list will not be long enough to matter
		var dupe bool
		// if it's a coordinator we don't need it
		for _, c := range coordinators {
			if c == e {
				dupe = true
				simplelog.Warningf("found %v in coordinator and executor list, removing from executor list", e)
				break
			}
		}
		if !dupe {
			filteredList[e] = true
		}
	}
	// convert back into a slice
	// again we are dealing with not
	// large enough sizes to warrant optimization
	var result []string
	for k := range filteredList {
		result = append(result, k)
	}
	// sort for consistent behavior and testing
	sort.Strings(result)
	slices.Reverse(result)
	return result
}

// FilterByNodeSelection applies include/exclude node filters to a node list.
// If includeNodes is non-empty, only nodes in that list are kept.
// If excludeNodes is non-empty, nodes in that list are removed.
func FilterByNodeSelection(nodes, includeNodes, excludeNodes []string) []string {
	if len(includeNodes) == 0 && len(excludeNodes) == 0 {
		return nodes
	}
	if len(includeNodes) > 0 {
		include := make(map[string]bool, len(includeNodes))
		for _, n := range includeNodes {
			include[n] = true
		}
		var result []string
		for _, n := range nodes {
			if include[n] {
				result = append(result, n)
			}
		}
		return result
	}
	exclude := make(map[string]bool, len(excludeNodes))
	for _, n := range excludeNodes {
		exclude[n] = true
	}
	var result []string
	for _, n := range nodes {
		if !exclude[n] {
			result = append(result, n)
		}
	}
	return result
}

func Execute(c Collector, s CopyStrategy, collectionArgs Args, hook shutdown.Hook, clusterCollection func()) error {
	return ExecuteStreamingCollect(c, s, collectionArgs, hook, clusterCollection)
}

func FindClusterID(outputDir string) (clusterStatsList []clusterstats.ClusterStats, err error) {
	err = filepath.Walk(outputDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err // Handle the error according to your needs
		}
		if info.Name() == "cluster-stats.json" {
			b, err := os.ReadFile(filepath.Clean(path)) // #nosec G122 -- path is from Walk over controlled output dir
			if err != nil {
				return err
			}
			var clusterStats clusterstats.ClusterStats

			err = json.Unmarshal(b, &clusterStats)
			if err != nil {
				return err
			}
			clusterStatsList = append(clusterStatsList, clusterStats)
		}

		return nil
	})
	return
}

// logDistributedCollectionSummary logs a comprehensive summary of the distributed collection
func logDistributedCollectionSummary(collectionMode collects.CollectionMode, coordinators, executors []string, files []helpers.CollectedFile, totalFailedFiles, totalFailedNodes, totalSkippedFiles []string, nodesConnectedTo int, duration time.Duration) {
	simplelog.Info("=== DISTRIBUTED COLLECTION SUMMARY ===")

	// Basic collection info
	simplelog.Infof("Collection Mode: %v", collectionMode)
	simplelog.Infof("Collection Duration: %v", duration.Round(time.Second))

	// Node information
	simplelog.Info("CLUSTER TOPOLOGY:")
	simplelog.Infof("  Coordinators: %d", len(coordinators))
	for i, coord := range coordinators {
		simplelog.Infof("    %d. %s", i+1, coord)
	}
	simplelog.Infof("  Executors: %d", len(executors))
	for i, exec := range executors {
		simplelog.Infof("    %d. %s", i+1, exec)
	}
	simplelog.Infof("  Total Nodes: %d", len(coordinators)+len(executors))
	simplelog.Infof("  Nodes Connected: %d", nodesConnectedTo)

	// Collection results
	totalFailures := len(totalFailedFiles) + len(totalFailedNodes)
	simplelog.Info("COLLECTION RESULTS:")
	simplelog.Infof("  Successful Collections: %d", len(files))
	simplelog.Infof("  Failed Collections: %d", totalFailures)
	simplelog.Infof("  Skipped Collections: %d", len(totalSkippedFiles))

	// File details
	if len(files) > 0 {
		simplelog.Info("COLLECTED FILES:")
		var totalSize int64
		for _, file := range files {
			sizeStr := formatFileSize(file.Size)
			simplelog.Infof("  %s (%s)", filepath.Base(file.Path), sizeStr)
			totalSize += file.Size
		}
		simplelog.Infof("  Total Size: %s", formatFileSize(totalSize))
	}

	// Failed collections
	if len(totalFailedNodes) > 0 || len(totalFailedFiles) > 0 {
		simplelog.Info("FAILED COLLECTIONS:")
		// Collection failures (nodes that failed during collection)
		for _, node := range totalFailedNodes {
			simplelog.Infof("   %s (collection failed)", node)
		}
		// Transfer failures (nodes that collected but failed to transfer)
		for _, file := range totalFailedFiles {
			simplelog.Infof("   %s (transfer failed)", file)
		}
	}

	// Skipped files
	if len(totalSkippedFiles) > 0 {
		simplelog.Info("SKIPPED COLLECTIONS:")
		for _, file := range totalSkippedFiles {
			simplelog.Infof("  - %s", file)
		}
	}

	// Success rate
	totalAttempts := len(files) + totalFailures
	if totalAttempts > 0 {
		successRate := float64(len(files)) / float64(totalAttempts) * 100
		simplelog.Infof("Success Rate: %.1f%% (%d/%d)", successRate, len(files), totalAttempts)
	}

	simplelog.Info("=== END DISTRIBUTED COLLECTION SUMMARY ===")
}

// formatFileSize formats file size in human-readable format
func formatFileSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}
