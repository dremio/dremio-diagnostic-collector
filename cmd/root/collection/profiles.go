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

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/local/restclient"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/collects"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/consoleprint"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/logparser"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/shutdown"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/simplelog"
)

const maxProblematicProfiles = 500

// LogProfileArgs holds the parameters for log-based profile collection.
type LogProfileArgs struct {
	TmpDir           string
	DremioEndpoint   string
	DremioPAT        string
	NodeName         string
	CollectionMode   collects.CollectionMode
	AllowInsecureSSL bool
	RestHTTPTimeout  int
	Hook             shutdown.Hook
}

// RunLogBasedProfileCollection scans extracted server.log files for job IDs
// associated with failures/OOM/cancellations and downloads their profiles.
// Only runs in diagnosis mode. Errors are logged but never abort the caller.
func RunLogBasedProfileCollection(args LogProfileArgs) error {
	if args.CollectionMode != collects.DiagnosisCollection {
		simplelog.Debugf("log-based profile collection skipped: mode is %q, not %q", args.CollectionMode, collects.DiagnosisCollection)
		return nil
	}

	if args.DremioPAT == "" {
		simplelog.Info("log-based profile collection skipped: no PAT token provided")
		return nil
	}

	simplelog.Info("=== LOG-BASED PROFILE COLLECTION START ===")

	// Find server.log files in extracted tarballs.
	// Layout after extraction: <tmpDir>/<nodeName>/logs/server.log*
	logFiles, err := findServerLogs(args.TmpDir)
	if err != nil {
		return fmt.Errorf("searching for server.log files: %w", err)
	}

	if len(logFiles) == 0 {
		simplelog.Info("log-based profile collection: no server.log files found in extracted data")
		return nil
	}

	simplelog.Infof("log-based profile collection: found %d server.log file(s) to scan", len(logFiles))

	// Calculate total size of all log files for progress tracking.
	var totalLogBytes int64
	for _, logFile := range logFiles {
		if fi, err := os.Stat(logFile); err == nil {
			totalLogBytes += fi.Size()
		}
	}

	// Scan all log files and merge results.
	consoleprint.UpdateResult("Scanning log files for problematic jobs...")
	consoleprint.UpdateArchiveProgress(0, totalLogBytes)
	scanner := logparser.NewScanner()
	mergedResult := &logparser.Result{
		Matches:    make(map[string]*logparser.Match),
		Exclusions: make(map[string]bool),
	}

	var scannedBytes int64
	for _, logFile := range logFiles {
		result, err := scanner.ScanFile(logFile)
		if err != nil {
			simplelog.Warningf("log-based profile collection: error scanning %v: %v", logFile, err)
			continue
		}
		for id, match := range result.Matches {
			if _, exists := mergedResult.Matches[id]; !exists {
				mergedResult.Matches[id] = match
			}
		}
		for id := range result.Exclusions {
			mergedResult.Exclusions[id] = true
		}
		mergedResult.TotalLinesScanned += result.TotalLinesScanned
		if fi, err := os.Stat(logFile); err == nil {
			scannedBytes += fi.Size()
		}
		consoleprint.UpdateArchiveProgress(scannedBytes, totalLogBytes)
		simplelog.Debugf("log-based profile collection: scanned %v (%d lines, %d job IDs)", logFile, result.TotalLinesScanned, len(result.Matches))
	}
	// Reset progress bar so it doesn't linger during download phase.
	consoleprint.UpdateArchiveProgress(0, 0)

	if len(mergedResult.Exclusions) > 0 {
		simplelog.Infof("log-based profile collection: excluding %d globally cancelled queries", len(mergedResult.Exclusions))
	}

	jobIDs := mergedResult.MostRecent(maxProblematicProfiles)
	if len(jobIDs) == 0 {
		simplelog.Info("log-based profile collection: no job IDs found in server.log files, falling back to sampling")
		simplelog.Info("=== LOG-BASED PROFILE COLLECTION END ===")
		return nil
	}

	totalMatches := len(mergedResult.Matches) - len(mergedResult.Exclusions)
	if totalMatches < 0 {
		totalMatches = 0
	}
	if len(jobIDs) < totalMatches {
		simplelog.Infof("log-based profile collection: %d unique job IDs found, limited to %d most recent (across %d lines scanned)", totalMatches, len(jobIDs), mergedResult.TotalLinesScanned)
	} else {
		simplelog.Infof("log-based profile collection: %d unique job IDs found across %d lines scanned", len(jobIDs), mergedResult.TotalLinesScanned)
	}
	consoleprint.UpdateResult(fmt.Sprintf("Found %d problematic job IDs, downloading profiles...", len(jobIDs)))

	// Ensure the REST client is initialized.
	restclient.InitClient(args.AllowInsecureSSL, args.RestHTTPTimeout)

	// Ensure the job profiles output directory exists (per-node subdirectory).
	jobProfilesDir := filepath.Join(args.TmpDir, "job-profiles", args.NodeName)
	if err := os.MkdirAll(jobProfilesDir, 0o750); err != nil {
		return fmt.Errorf("creating job profiles output directory: %w", err)
	}

	// Download profiles sequentially (job profile API is typically the bottleneck).
	var (
		downloaded int
		failures   int
	)

	for i, jobID := range jobIDs {
		consoleprint.UpdateResult(fmt.Sprintf("Downloading problematic profiles... %d of %d", i+1, len(jobIDs)))
		if err := downloadJobProfile(args.Hook, args.DremioEndpoint, args.DremioPAT, jobProfilesDir, jobID); err != nil {
			simplelog.Warningf("log-based profile collection: failed to download profile for %v: %v", jobID, err)
			failures++
			continue
		}
		downloaded++
	}

	simplelog.Infof("log-based profile collection: %d job IDs found, %d profiles downloaded, %d failures", len(jobIDs), downloaded, failures)
	simplelog.Info("=== LOG-BASED PROFILE COLLECTION END ===")
	return nil
}

// findServerLogs walks tmpDir looking for server.log files (plain and .gz).
// Expected layout: <tmpDir>/logs/<nodeName>/server.log*
func findServerLogs(tmpDir string) ([]string, error) {
	var logFiles []string
	err := filepath.Walk(tmpDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip inaccessible entries
		}
		if info.IsDir() {
			return nil
		}
		name := info.Name()
		if strings.HasPrefix(name, "server.") && strings.Contains(name, ".log") {
			// Ensure it's under a logs/ directory anywhere in the path.
			// Layout is <tmpDir>/logs/<nodeName>/server.log*
			rel, relErr := filepath.Rel(tmpDir, path)
			if relErr == nil && strings.Contains(filepath.ToSlash(rel), "logs/") {
				logFiles = append(logFiles, path)
			}
		}
		return nil
	})
	return logFiles, err
}

// downloadJobProfile downloads a single job profile via the Dremio REST API.
func downloadJobProfile(hook shutdown.Hook, endpoint, pat, outDir, jobID string) error {
	apipath := "/apiv2/support/" + jobID + "/download"
	url := endpoint + apipath
	headers := map[string]string{"Accept": "application/octet-stream"}
	body, err := restclient.APIRequest(hook, url, pat, "POST", headers)
	if err != nil {
		return err
	}
	filename := filepath.Join(outDir, jobID+".zip")
	if err := os.WriteFile(filename, body, 0o600); err != nil {
		return fmt.Errorf("unable to write profile %s: %w", jobID, err)
	}
	return nil
}
