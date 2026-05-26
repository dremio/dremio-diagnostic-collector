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
	"compress/gzip"
	"crypto/md5" // #nosec G501 -- MD5 used as checksum fallback, not for security
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"sort"

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/local/jvmcollect"
	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/helpers"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/collects"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/consoleprint"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/masking"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/shutdown"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/simplelog"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/versions"
)

// maxRetries is the number of retry attempts for transient streaming errors.
const maxRetries = 3

// Buffer and threshold constants for the streaming pipeline.
const (
	// streamWriteBufSize is the buffered-writer size for batching disk writes (8 MB).
	streamWriteBufSize = 8 * 1024 * 1024
	// streamReadBufSize is the buffered-reader size for gzip decompression reads (1 MB).
	streamReadBufSize = 1024 * 1024
	// copyBufSize is the io.CopyBuffer scratch buffer size (1 MB), larger than
	// io.Copy's 32 KB default to reduce syscall overhead on large transfers.
	copyBufSize = 1024 * 1024
	// minDiskSpaceBytes is the minimum free disk space (500 MB) required on a
	// remote node before uploading async-profiler artifacts.
	minDiskSpaceBytes = 500 * 1024 * 1024
	// progressUpdateInterval is the minimum time between TUI progress updates
	// for a single file transfer, to avoid flooding the display.
	progressUpdateInterval = 250 * time.Millisecond
)

// hashResult carries the hex digest (or error) from a background hash goroutine.
type hashResult struct {
	hex string
	err error
}

// hashFileInBackground re-reads the file at path and computes the hash using
// the named checksumTool ("sha256sum" or "md5sum"). If checksumTool is empty,
// it immediately returns a closed channel with an empty hex string (no hash
// needed). The channel is always closed exactly once and must be read by the
// caller to avoid goroutine leaks.
func hashFileInBackground(path, checksumTool string) <-chan hashResult {
	ch := make(chan hashResult, 1)
	if checksumTool == "" {
		ch <- hashResult{}
		close(ch)
		return ch
	}
	go func() {
		defer close(ch)
		f, err := os.Open(filepath.Clean(path))
		if err != nil {
			ch <- hashResult{err: fmt.Errorf("hash: open %v: %w", path, err)}
			return
		}
		defer f.Close() //nolint:errcheck

		var h hash.Hash
		switch checksumTool {
		case "md5sum":
			h = md5.New() // #nosec G401 -- MD5 used as checksum fallback, not for security
		default: // sha256sum or any unrecognised tool — default to sha256
			h = sha256.New()
		}

		if _, err := io.Copy(h, f); err != nil {
			ch <- hashResult{err: fmt.Errorf("hash: read %v: %w", path, err)}
			return
		}
		ch <- hashResult{hex: hex.EncodeToString(h.Sum(nil))}
	}()
	return ch
}

// getAsprofBinaryFn is a package-level function used by runJVMCollection to
// resolve the asprof binary for a given architecture. It defaults to the real
// jvmcollect.GetAsprofBinary but can be overridden in tests.
var getAsprofBinaryFn = jvmcollect.GetAsprofBinary

// getAsprofFilesFn resolves both asprof binary and libasyncProfiler.so for
// a given architecture. Used by the pre-distribution phase.
var getAsprofFilesFn = jvmcollect.GetAsprofFiles

// isTransientError returns true for errors that may succeed on retry (connection
// issues, EOF, timeouts) and false for permanent errors (permission denied,
// file not found) that should cause an immediate skip.
func isTransientError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	// Permanent errors — skip immediately.
	permanentPatterns := []string{
		"permission denied",
		"no such file",
		"not found",
		"enoent",
		"eacces",
	}
	for _, p := range permanentPatterns {
		if strings.Contains(msg, p) {
			return false
		}
	}
	// Transient patterns — retry.
	transientPatterns := []string{
		"connection reset",
		"connection refused",
		"broken pipe",
		"eof",
		"timeout",
		"deadline exceeded",
		"i/o timeout",
		"temporary failure",
	}
	for _, p := range transientPatterns {
		if strings.Contains(msg, p) {
			return true
		}
	}
	// Default: treat unknown errors as transient to avoid skipping recoverable problems.
	return true
}

// fileTypeToStrategyType maps RemoteFileInfo.FileType values to the directory
// names used by CopyStrategy.CreatePath.
func fileTypeToStrategyType(ft string) string {
	switch ft {
	case "log", "gc-log":
		return "logs"
	case "config":
		return "configuration"
	case "queries":
		return "queries"
	default:
		return ft
	}
}

// streamFile streams a single remote file from host to a local destination path.
// It retries up to maxRetries times on transient errors and returns the bytes
// written along with a channel that will receive the hash result. On permanent
// error it returns immediately without retrying. Progress is reported to the
// TUI via progressWriter.
func streamFile(c Collector, host, remotePath, destPath string, retries int, expectedSize int64, filename, checksumTool string, useGzip bool) (int64, <-chan hashResult, error) {
	var lastErr error
	for attempt := 1; attempt <= retries; attempt++ {
		if attempt > 1 {
			simplelog.Infof("stream retry %d/%d for %v:%v", attempt, retries, host, remotePath)
		}

		n, hashCh, err := streamFileOnce(c, host, remotePath, destPath, expectedSize, filename, checksumTool, useGzip)
		if err == nil {
			return n, hashCh, nil
		}
		lastErr = err

		if !isTransientError(err) {
			return 0, nil, fmt.Errorf("permanent error streaming %v:%v: %w", host, remotePath, err)
		}
		simplelog.Warningf("transient error streaming %v:%v (attempt %d/%d): %v", host, remotePath, attempt, retries, err)
		// Exponential backoff: 500ms, 1s, 2s (capped)
		shift := attempt - 1
		if shift < 0 {
			shift = 0
		}
		backoff := time.Duration(1<<shift) * 500 * time.Millisecond // #nosec G115 -- shift bounded by maxRetries
		if backoff > 2*time.Second {
			backoff = 2 * time.Second
		}
		time.Sleep(backoff)
	}
	return 0, nil, fmt.Errorf("exhausted %d retries streaming %v:%v: %w", retries, host, remotePath, lastErr)
}

// streamFileOnce opens a local file, streams the remote content via
// StreamFromHost through a buffered writer (no inline hashing), then kicks
// off a background goroutine to re-read the written file and compute a single
// hash. Returns bytes written plus a channel carrying the hash result.
func streamFileOnce(c Collector, host, remotePath, destPath string, expectedSize int64, filename, checksumTool string, useGzip bool) (int64, <-chan hashResult, error) {
	if err := os.MkdirAll(filepath.Dir(destPath), DirPerms); err != nil {
		return 0, nil, fmt.Errorf("failed to create destination dir for %v: %w", destPath, err)
	}

	f, err := os.Create(filepath.Clean(destPath))
	if err != nil {
		return 0, nil, fmt.Errorf("failed to create file %v: %w", destPath, err)
	}

	// Buffered writer batches chunks into large disk writes.
	bf := bufio.NewWriterSize(f, streamWriteBufSize)

	// progressWriter wraps bf to count bytes and report TUI progress.
	pw := &progressWriter{w: bf, expectedSize: expectedSize, host: host, filename: filename}

	// Copy buffer larger than io.Copy's 32 KB default reduces syscall
	// overhead and improves throughput for large file transfers.
	copyBuf := make([]byte, copyBufSize)

	if useGzip {
		// Gzip decompression pipeline: goroutine runs StreamFromHost writing
		// compressed bytes to pipe writer; main thread reads through gzip.NewReader
		// into the progressWriter (which counts decompressed bytes).
		pr, pipew := io.Pipe()

		var streamErr error
		go func() {
			streamErr = c.StreamFromHost(host, remotePath, pipew, true)
			pipew.CloseWithError(streamErr) // signals EOF or error to reader side
		}()

		// Buffer the pipe reader so gzip decompression reads in larger
		// chunks, reducing back-pressure on the SPDY stream goroutine.
		bufPR := bufio.NewReaderSize(pr, streamReadBufSize)

		gz, gzErr := gzip.NewReader(bufPR)
		if gzErr != nil {
			_ = pr.Close()
			_ = f.Close()
			_ = os.Remove(destPath)
			return 0, nil, fmt.Errorf("gzip reader init failed for %v:%v: %w", host, remotePath, gzErr)
		}

		_, copyErr := io.CopyBuffer(pw, gz, copyBuf) // #nosec G110 -- source is trusted dremio cluster output
		_ = gz.Close()
		_ = pr.Close()

		// Check both the stream error and the copy error.
		if streamErr != nil {
			_ = bf.Flush()
			_ = f.Close()
			_ = os.Remove(destPath)
			return 0, nil, streamErr
		}
		if copyErr != nil {
			_ = bf.Flush()
			_ = f.Close()
			_ = os.Remove(destPath)
			return 0, nil, fmt.Errorf("gzip decompress copy failed for %v:%v: %w", host, remotePath, copyErr)
		}
	} else {
		// Direct path: StreamFromHost writes raw bytes to progressWriter.
		streamErr := c.StreamFromHost(host, remotePath, pw, false)
		if streamErr != nil {
			_ = bf.Flush()
			_ = f.Close()
			_ = os.Remove(destPath)
			return 0, nil, streamErr
		}
	}

	if flushErr := bf.Flush(); flushErr != nil {
		_ = f.Close()
		return 0, nil, fmt.Errorf("flush failed for %v: %w", destPath, flushErr)
	}
	closeErr := f.Close()
	if closeErr != nil {
		return 0, nil, fmt.Errorf("close failed for %v: %w", destPath, closeErr)
	}

	// Kick off background hash of the written file.
	hashCh := hashFileInBackground(destPath, checksumTool)
	return pw.n, hashCh, nil
}

// progressWriter wraps an io.Writer, counts bytes written, and reports
// per-file progress to the TUI via consoleprint.UpdateNodeState. Updates
// are throttled to at most once per 250ms to avoid flooding the display.
type progressWriter struct {
	w            io.Writer
	n            int64
	expectedSize int64
	host         string
	filename     string
	lastUpdate   time.Time
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	n, err := pw.w.Write(p)
	pw.n += int64(n)

	now := time.Now()
	if n > 0 && now.Sub(pw.lastUpdate) >= progressUpdateInterval {
		pw.lastUpdate = now
		var statusUX string
		if pw.expectedSize > 0 {
			pct := int(pw.n * 100 / pw.expectedSize)
			if pct > 100 {
				pct = 100
			}
			statusUX = fmt.Sprintf("%s %s of %s [%d%%]", pw.filename, humanizeBytes(pw.n), humanizeBytes(pw.expectedSize), pct)
		} else {
			statusUX = fmt.Sprintf("%s %s", pw.filename, humanizeBytes(pw.n))
		}
		consoleprint.UpdateNodeState(consoleprint.NodeState{
			Node:     pw.host,
			Status:   consoleprint.Streaming,
			StatusUX: statusUX,
		})
	}
	return n, err
}

// humanizeBytes formats a byte count as a human-readable string
// (B, KB, MB, GB) with one decimal place for units above bytes.
func humanizeBytes(n int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case n >= gb:
		return fmt.Sprintf("%.1fGB", float64(n)/float64(gb))
	case n >= mb:
		return fmt.Sprintf("%.1fMB", float64(n)/float64(mb))
	case n >= kb:
		return fmt.Sprintf("%.1fKB", float64(n)/float64(kb))
	default:
		return fmt.Sprintf("%dB", n)
	}
}

// parseChecksumOutput extracts the hex digest from sha256sum/md5sum output.
// The expected format is "<hash>  <path>\n" (GNU coreutils) or
// "TYPE (<path>) = <hash>\n" (BSD). Returns empty string on malformed input.
func parseChecksumOutput(output string) string {
	s := strings.TrimSpace(output)
	if s == "" {
		return ""
	}
	// GNU format: first whitespace-delimited field is the hex hash.
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// verifyChecksum runs the probed checksum tool on the remote node to verify
// the transferred file's integrity. If checksumTool is empty, verification
// is skipped. Mismatches are logged as warnings; errors are logged and
// skipped. The function always returns nil — checksum verification is advisory
// per D013.
func verifyChecksum(c Collector, host, remotePath, localHash, checksumTool string) error {
	if checksumTool == "" {
		simplelog.Warningf("checksum verification skipped for %v:%v — no checksum tool available", host, remotePath)
		return nil
	}

	out, err := c.HostExecute(false, host, checksumTool, remotePath)
	if err != nil {
		simplelog.Warningf("checksum verification skipped for %v:%v — %v failed: %v", host, remotePath, checksumTool, err)
		return nil
	}

	remoteHash := parseChecksumOutput(out)
	if remoteHash == "" {
		simplelog.Warningf("checksum verification skipped for %v:%v — empty output from %v", host, remotePath, checksumTool)
		return nil
	}

	if remoteHash == localHash {
		simplelog.Infof("checksum verified for %v:%v (%v)", host, remotePath, checksumTool)
	} else {
		simplelog.Warningf("checksum mismatch for %v:%v — local %v=%v remote %v=%v", host, remotePath, checksumTool, localHash, checksumTool, remoteHash)
	}
	return nil
}

// maskLocalConfigFile reads a config file from disk, applies secret masking,
// and writes it back. Returns nil on success. Callers should log the error
// as a warning rather than failing the collection (advisory pattern per K011).
func maskLocalConfigFile(path string) error {
	data, err := os.ReadFile(filepath.Clean(path)) // #nosec G304 -- path is an internally built archive path
	if err != nil {
		return fmt.Errorf("reading %v for masking: %w", path, err)
	}
	masked := masking.MaskConfigData(data)
	if err := os.WriteFile(path, masked, 0o600); err != nil { // #nosec G703 -- path is an internally built archive path
		return fmt.Errorf("writing masked %v: %w", path, err)
	}
	return nil
}

// standardModeLogAllowlist defines the only log file base-name prefixes
// collected in standard mode. Archives (e.g. server.log.2024-01-01) are
// included because they share the same prefix.
// standardModeLogAllowlist uses short prefixes to match both current log files
// (e.g. server.log, tracker.json) and their dated archives
// (e.g. server.2026-03-31.0.log.gz, tracker.2026-03-28.0.json.gz).
var standardModeLogAllowlist = []string{
	"server.",
	"tracker.",
	"vacuum.",
}

// alwaysExcludedPrefixes are log files that should never be collected in any mode.
var alwaysExcludedPrefixes = []string{
	"admin_backup",
	"audit.",
	"server.json",
	"server.out",
}

// alwaysExcludedSuffixes are file extensions that hold secrets (keystores,
// certificates, private keys) and must never be collected in any mode.
// Matched case-insensitively against the file's base name.
var alwaysExcludedSuffixes = []string{
	".jks",
	".pem",
	".key",
	".cer",
	".crt",
}

// isLogAllowedInStandardMode returns true if the log file base name matches
// the standard-mode allowlist (prefix match to cover archived rotations).
func isLogAllowedInStandardMode(baseName string) bool {
	for _, prefix := range standardModeLogAllowlist {
		if strings.HasPrefix(baseName, prefix) {
			return true
		}
	}
	return false
}

// isAlwaysExcluded returns true if the file should never be collected.
// Combines a name-prefix blocklist (e.g. admin_backup, audit.) with a
// suffix blocklist for secret-bearing extensions (.jks, .pem, .key, .cer, .crt).
func isAlwaysExcluded(baseName string) bool {
	for _, prefix := range alwaysExcludedPrefixes {
		if strings.HasPrefix(baseName, prefix) {
			return true
		}
	}
	lower := strings.ToLower(baseName)
	for _, suffix := range alwaysExcludedSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

// logDayLimit returns the maximum age in days for a given log file base name,
// based on the configured per-log day counts. Returns -1 if no limit applies
// (file should always be collected).
// logDayLimit returns the maximum age in days for a given log file base name.
// Uses short prefixes to match both current files (server.log, queries.json)
// and dated archives (server.2026-03-31.0.log.gz, queries.2026-03-28.0.json.gz).
// isLogTypeEnabled returns true if the log file should be collected based on
// the user's log type selections. Files that don't match any known prefix are
// collected by default (e.g. meta-refresh, reflection logs have no toggle).
func isLogTypeEnabled(baseName string, args Args) bool {
	switch {
	case strings.HasPrefix(baseName, "server."):
		return args.CollectServerLogs
	case strings.HasPrefix(baseName, "tracker."):
		return args.CollectTrackerJSON
	case strings.HasPrefix(baseName, "vacuum."):
		return args.CollectVacuumLog
	case strings.HasPrefix(baseName, "acceleration."):
		return args.CollectAccelerationLog
	case strings.HasPrefix(baseName, "access."):
		return args.CollectAccessLog
	case strings.HasPrefix(baseName, "hs_err"):
		return args.CollectHSErrFiles
	case strings.HasPrefix(baseName, "hive-deprecated."):
		return args.CollectHiveDeprecated
	default:
		return true
	}
}

func logDayLimit(base string, args Args) int {
	// Diagnosis mode: all log types use the unified day limit.
	if args.CollectionMode == collects.DiagnosisCollection && args.DiagLogDays > 0 {
		return args.DiagLogDays
	}
	// Standard mode: per-log day counts.
	switch {
	case strings.HasPrefix(base, "server."):
		return args.ServerLogsNumDays
	case strings.HasPrefix(base, "tracker."):
		return args.TrackerJSONNumDays
	case strings.HasPrefix(base, "vacuum."):
		return args.VacuumLogNumDays
	case strings.HasPrefix(base, "queries."):
		return args.QueriesJSONNumDays
	default:
		return -1 // no limit
	}
}

// extractFilenameDate extracts a YYYY-MM-DD date from a log filename like
// "server.2026-03-31.0.log.gz" or "queries.2026-04-01.0.json.gz".
// Returns the date string if found, or "" for undated files like "server.log" / "queries.json".
func extractFilenameDate(baseName string) string {
	// Split on '.' and look for a part matching YYYY-MM-DD (10 chars, two dashes).
	for _, part := range strings.Split(baseName, ".") {
		if len(part) == 10 && part[4] == '-' && part[7] == '-' {
			if _, err := time.Parse("2006-01-02", part); err == nil {
				return part
			}
		}
	}
	return ""
}

// isWithinDateRange checks whether a log file falls within the configured date range.
// For dated files (e.g. queries.2026-04-01.0.json.gz) the embedded date is compared
// directly against the range. For undated files (e.g. queries.json, server.log)
// the file represents "today" and is included only if today is within the range.
// When no startDate is set the window is [now-days, now] using file mtime.
// Always returns true if dayLimit <= 0 (no limit).
func isWithinDateRange(baseName string, modTime int64, dayLimit int, startDate string) bool {
	if dayLimit <= 0 {
		return true
	}

	// When an explicit start date is provided, compare against the date in the filename.
	if sd, err := time.Parse("2006-01-02", startDate); err == nil {
		end := sd.AddDate(0, 0, dayLimit)
		fileDate := extractFilenameDate(baseName)
		if fileDate != "" {
			fd, _ := time.Parse("2006-01-02", fileDate)
			return !fd.Before(sd) && fd.Before(end)
		}
		// Undated file (e.g. queries.json, server.log) = current/today's data.
		today, _ := time.Parse("2006-01-02", time.Now().Format("2006-01-02"))
		return !today.Before(sd) && today.Before(end)
	}

	// No start date: fall back to mtime-based filtering (now - days).
	if modTime == 0 {
		return true
	}
	cutoff := time.Now().AddDate(0, 0, -dayLimit)
	return time.Unix(modTime, 0).After(cutoff)
}

// streamNodeFiles iterates over discovered files for a single node, streams
// each one to the appropriate CopyStrategy destination, and returns the list
// of collected files plus any file paths that were skipped (due to errors).
// Files excluded by mode or policy are silently ignored and not counted as skipped.
func streamNodeFiles(c Collector, host string, info *RemoteNodeInfo, cs CopyStrategy, nodeType string, collectionMode collects.CollectionMode, collectGCLogs bool, collectionArgs Args) ([]helpers.CollectedFile, []string) {
	var collected []helpers.CollectedFile
	var skipped []string

	for _, rf := range info.Files {
		// Skip 0-byte files — nothing to collect.
		if rf.Size == 0 {
			simplelog.Infof("stream exclude (0 bytes): %v:%v", host, rf.Path)
			continue
		}

		base := filepath.Base(rf.Path)

		// Always exclude admin_backup files regardless of mode.
		if isAlwaysExcluded(base) {
			simplelog.Infof("stream exclude (blocked): %v:%v", host, rf.Path)
			continue
		}

		// Silently exclude GC logs when not enabled.
		if rf.FileType == "gc-log" && !collectGCLogs {
			simplelog.Infof("stream exclude gc-log (disabled): %v:%v", host, rf.Path)
			continue
		}

		// Exclude log types that are disabled by the user.
		if rf.FileType == "log" && !isLogTypeEnabled(base, collectionArgs) {
			simplelog.Infof("stream exclude (log type disabled): %v:%v", host, rf.Path)
			continue
		}

		// Exclude queries.json when disabled.
		if rf.FileType == "queries" && !collectionArgs.CollectQueriesJSON {
			simplelog.Infof("stream exclude (queries disabled): %v:%v", host, rf.Path)
			continue
		}

		// In standard mode, only collect allowlisted log files.
		// Config files are always collected regardless of mode.
		if collectionMode == collects.StandardCollection && rf.FileType == "log" {
			if !isLogAllowedInStandardMode(base) {
				simplelog.Infof("stream exclude (not in standard allowlist): %v:%v", host, rf.Path)
				continue
			}
		}

		// Apply date-range filtering for log and queries files.
		if rf.FileType == "log" || rf.FileType == "queries" {
			dayLimit := logDayLimit(base, collectionArgs)
			if !isWithinDateRange(base, rf.ModTime, dayLimit, collectionArgs.StartDate) {
				simplelog.Infof("stream exclude (outside date range, days=%d start=%q): %v:%v", dayLimit, collectionArgs.StartDate, host, rf.Path)
				continue
			}
		}
		strategyType := fileTypeToStrategyType(rf.FileType)
		destDir, err := cs.CreatePath(strategyType, host, nodeType)
		if err != nil {
			simplelog.Errorf("stream: failed to create path for %v on %v: %v", rf.Path, host, err)
			skipped = append(skipped, rf.Path)
			continue
		}

		destPath := filepath.Join(destDir, filepath.Base(rf.Path))

		simplelog.Infof("stream start: %v:%v → %v", host, rf.Path, destPath)

		n, hashCh, err := streamFile(c, host, rf.Path, destPath, maxRetries, rf.Size, filepath.Base(rf.Path), info.ChecksumTool, info.GzipAvailable)
		if err != nil {
			simplelog.Warningf("stream skip: %v:%v — %v", host, rf.Path, err)
			skipped = append(skipped, rf.Path)
			continue
		}

		// Consume the hash channel (always read to avoid goroutine leak).
		var localHash string
		if hashCh != nil {
			hr := <-hashCh
			if hr.err != nil {
				simplelog.Warningf("stream hash: %v:%v — %v", host, rf.Path, hr.err)
			} else {
				localHash = hr.hex
			}
		}

		// Advisory checksum verification — always returns nil.
		_ = verifyChecksum(c, host, rf.Path, localHash, info.ChecksumTool)

		// Apply secret masking to config files after streaming (advisory per K011).
		if rf.FileType == "config" {
			if maskErr := maskLocalConfigFile(destPath); maskErr != nil {
				simplelog.Warningf("stream mask: failed to mask config %v — %v", destPath, maskErr)
			}
		}

		simplelog.Infof("stream complete: %v:%v (%d bytes)", host, rf.Path, n)
		collected = append(collected, helpers.CollectedFile{
			Path: destPath,
			Size: n,
		})
	}

	return collected, skipped
}

// ExecuteStreamingCollect implements streaming collection: discover files on
// each remote node, stream them individually via cat, and archive the result.
// No binary deployment to remote nodes is required.
func ExecuteStreamingCollect(c Collector, s CopyStrategy, collectionArgs Args, hook shutdown.Hook, clusterCollection func()) error {
	start := time.Now().UTC()
	outputLoc := collectionArgs.OutputLoc
	collectionMode := collectionArgs.CollectionMode
	collectionThreads := collectionArgs.CollectionThreads
	if collectionThreads <= 0 {
		if collectionMode == "diagnosis" {
			collectionThreads = 20
		} else {
			collectionThreads = 5
		}
	}

	// Discover cluster topology.
	coordinators, err := c.GetCoordinators()
	if err != nil {
		return fmt.Errorf("failed to get coordinators: %w", err)
	}
	executorsRaw, err := c.GetExecutors()
	if err != nil {
		return fmt.Errorf("failed to get executors: %w", err)
	}
	coordinators = FilterCoordinators(coordinators)
	executors := FilterExecutors(executorsRaw, coordinators)

	// Apply node selection filters (from TUI node selection or --nodes/--exclude-nodes flags).
	coordinators = FilterByNodeSelection(coordinators, collectionArgs.IncludeNodes, collectionArgs.ExcludeNodes)
	executors = FilterByNodeSelection(executors, collectionArgs.IncludeNodes, collectionArgs.ExcludeNodes)

	totalNodes := len(coordinators) + len(executors)
	if totalNodes == 0 {
		return fmt.Errorf("no hosts found, nothing to collect: %v", c.HelpText())
	}

	simplelog.Infof("streaming collect: discovered %d coordinator(s) and %d executor(s)", len(coordinators), len(executors))

	// Count extra tasks for JVM diagnostic tools and heap dumps.
	var extraTasks int
	if collectionArgs.CollectJFR || collectionArgs.CollectJStack || collectionArgs.CollectTop || collectionArgs.CollectAsyncProfiler {
		extraTasks++
	}
	if collectionArgs.CollectHeapDump {
		extraTasks++
	}

	consoleprint.UpdateRuntime(
		versions.GetCLIVersion(),
		simplelog.GetLogLoc(),
		len(coordinators),
		len(executors),
		extraTasks,
	)
	consoleprint.UpdateCollectionMode(collectionMode)

	// Register all discovered nodes in TUI immediately with Queued status
	// so they are visible before any thread is acquired.
	for _, host := range coordinators {
		consoleprint.UpdateNodeState(consoleprint.NodeState{Node: host, StatusUX: "Queued", IsCoordinator: true})
	}
	for _, host := range executors {
		consoleprint.UpdateNodeState(consoleprint.NodeState{Node: host, StatusUX: "Queued", IsCoordinator: false})
	}

	// Bounded parallelism via a semaphore channel.
	sem := make(chan struct{}, collectionThreads)

	var activeThreads, queuedCount atomic.Int64
	// Initialize the TUI thread status display. In diagnosis mode, start with N/A
	// since discovery runs before the semaphore-bounded streaming phase.
	if collectionMode == collects.DiagnosisCollection {
		consoleprint.UpdateThreadStatus(-1, collectionThreads, 0)
	} else {
		consoleprint.UpdateThreadStatus(0, collectionThreads, 0)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var collectedFiles []helpers.CollectedFile
	var totalFailedNodes []string
	var totalSkippedFiles []string
	pidByHost := make(map[string]int)                  // DremioPID per host, populated during discovery
	nodeTypeByHost := make(map[string]string)          // "coordinator" or "executor" per host
	nodeInfoByHost := make(map[string]*RemoteNodeInfo) // discovery results per host, used by log streaming phase

	// ========================================================================
	// Phase 1: Discovery + node-info collection (parallel across nodes)
	// ========================================================================
	discoverNode := func(host, nodeType string) {
		defer wg.Done()
		queuedCount.Add(1)
		consoleprint.UpdateThreadStatus(int(activeThreads.Load()), collectionThreads, int(queuedCount.Load()))
		sem <- struct{}{}
		queuedCount.Add(-1)
		activeThreads.Add(1)
		consoleprint.UpdateThreadStatus(int(activeThreads.Load()), collectionThreads, int(queuedCount.Load()))
		defer func() {
			<-sem
			activeThreads.Add(-1)
			consoleprint.UpdateThreadStatus(int(activeThreads.Load()), collectionThreads, int(queuedCount.Load()))
		}()

		consoleprint.UpdateNodeState(consoleprint.NodeState{
			Node:          host,
			Status:        consoleprint.Starting,
			StatusUX:      "Discovering files",
			IsCoordinator: nodeType == "coordinator",
		})
		simplelog.Infof("stream discover start: %v (%v)", host, nodeType)

		logDir := collectionArgs.CoordinatorLogDir
		if nodeType == "executor" {
			logDir = collectionArgs.ExecutorLogDir
		}
		info, err := c.DiscoverFiles(host, logDir, collectionArgs.DremioConfDir)
		if err != nil {
			simplelog.Errorf("stream discover failure: %v — %v", host, err)
			mu.Lock()
			totalFailedNodes = append(totalFailedNodes, host)
			mu.Unlock()
			consoleprint.UpdateNodeState(consoleprint.NodeState{
				Node:          host,
				Status:        consoleprint.Completed,
				StatusUX:      "Discovery failed",
				Result:        consoleprint.ResultFailure,
				IsCoordinator: nodeType == "coordinator",
			})
			return
		}
		simplelog.Infof("stream discover complete: %v — %d files found, pid=%d", host, len(info.Files), info.DremioPID)

		protocolTag := c.Protocol()
		if info.GzipAvailable {
			protocolTag += "/GZIP"
		} else {
			protocolTag += "/No compression"
		}
		consoleprint.UpdateNodeState(consoleprint.NodeState{
			Node:     host,
			Protocol: protocolTag,
		})

		mu.Lock()
		pidByHost[host] = info.DremioPID
		nodeTypeByHost[host] = nodeType
		nodeInfoByHost[host] = info
		mu.Unlock()

		if len(info.Files) == 0 {
			simplelog.Warningf("stream: no files discovered on %v, skipping", host)
			consoleprint.UpdateNodeState(consoleprint.NodeState{
				Node:          host,
				StatusUX:      "No files found",
				IsCoordinator: nodeType == "coordinator",
			})
		}

		consoleprint.UpdateNodeState(consoleprint.NodeState{
			Node:          host,
			StatusUX:      "Discovery complete",
			IsCoordinator: nodeType == "coordinator",
		})
	}

	for _, host := range coordinators {
		wg.Add(1)
		go discoverNode(host, "coordinator")
	}
	for _, host := range executors {
		wg.Add(1)
		go discoverNode(host, "executor")
	}
	wg.Wait()

	// ========================================================================
	// Phase 2+3: JVM collection (diagnosis mode only)
	// ========================================================================
	var jvmFilesByHost map[string][]helpers.CollectedFile
	if collectionArgs.CollectionMode == collects.DiagnosisCollection {
		jvmFilesByHost = runJVMCollection(c, s, collectionArgs, pidByHost, nodeTypeByHost, collectionThreads)
	}

	// ========================================================================
	// Phase 4+5: Log streaming + orchestrator collection (parallel)
	// ========================================================================
	streamNodeLogs := func(host string) {
		defer wg.Done()
		nodeType := nodeTypeByHost[host]
		info := nodeInfoByHost[host]
		if info == nil {
			return
		}

		queuedCount.Add(1)
		consoleprint.UpdateThreadStatus(int(activeThreads.Load()), collectionThreads, int(queuedCount.Load()))
		sem <- struct{}{}
		queuedCount.Add(-1)
		activeThreads.Add(1)
		consoleprint.UpdateThreadStatus(int(activeThreads.Load()), collectionThreads, int(queuedCount.Load()))
		defer func() {
			<-sem
			activeThreads.Add(-1)
			consoleprint.UpdateThreadStatus(int(activeThreads.Load()), collectionThreads, int(queuedCount.Load()))
		}()

		// --- Node-info collection (OS info, disk usage, RocksDB, JVM settings) ---
		var nodeInfoToolErrors []string
		var niToolsFailed int
		nodeInfoDone := make(chan struct{})
		go func() {
			defer close(nodeInfoDone)
			nodeInfoDir, niErr := s.CreatePath("node-info", host, nodeType)
			if niErr != nil {
				simplelog.Errorf("node-info-collect: failed to create node-info path for %s: %v", host, niErr)
				nodeInfoToolErrors = append(nodeInfoToolErrors, fmt.Sprintf("node-info path: %v", niErr))
				return
			}
			consoleprint.UpdateNodeState(consoleprint.NodeState{
				Node: host, Status: consoleprint.Collecting, StatusUX: "Collecting OS info",
				IsCoordinator: nodeType == "coordinator",
			})
			if err := CollectOSInfo(c, host, nodeInfoDir); err != nil {
				simplelog.Warningf("node-info-collect: os-info failed on %s: %v", host, err)
				niToolsFailed++
				nodeInfoToolErrors = append(nodeInfoToolErrors, err.Error())
			}
			consoleprint.UpdateNodeState(consoleprint.NodeState{
				Node: host, Status: consoleprint.Collecting, StatusUX: "Collecting disk usage",
				IsCoordinator: nodeType == "coordinator",
			})
			if err := CollectDiskUsage(c, host, nodeInfoDir); err != nil {
				simplelog.Warningf("node-info-collect: disk-usage failed on %s: %v", host, err)
				niToolsFailed++
				nodeInfoToolErrors = append(nodeInfoToolErrors, err.Error())
			}
			if nodeType == "coordinator" {
				consoleprint.UpdateNodeState(consoleprint.NodeState{
					Node: host, Status: consoleprint.Collecting, StatusUX: "Collecting RocksDB disk allocation",
					IsCoordinator: true,
				})
				// Prefer the per-node discovered path; fall back to the CLI flag/default.
				rocksDBDir := info.RocksDBDir
				if rocksDBDir == "" {
					rocksDBDir = collectionArgs.DremioRocksDBDir
				}
				if err := CollectRocksDBDiskUsage(c, host, nodeInfoDir, rocksDBDir); err != nil {
					simplelog.Warningf("node-info-collect: rocksdb-disk-usage failed on %s: %v", host, err)
					niToolsFailed++
					nodeInfoToolErrors = append(nodeInfoToolErrors, err.Error())
				}
			}
			if info.DremioPID > 0 {
				consoleprint.UpdateNodeState(consoleprint.NodeState{
					Node: host, Status: consoleprint.Collecting, StatusUX: "Collecting JVM settings",
					IsCoordinator: nodeType == "coordinator",
				})
				if err := CollectJVMFlags(c, host, info.DremioPID, nodeInfoDir); err != nil {
					simplelog.Warningf("node-info-collect: jvm-flags failed on %s: %v", host, err)
					niToolsFailed++
					nodeInfoToolErrors = append(nodeInfoToolErrors, err.Error())
				}
			}
		}()
		nodeInfoTimeout := 2 * time.Minute
		select {
		case <-nodeInfoDone:
		case <-time.After(nodeInfoTimeout):
			simplelog.Warningf("node-info-collect: timed out after %v on %s", nodeInfoTimeout, host)
			niToolsFailed++
			nodeInfoToolErrors = append(nodeInfoToolErrors, fmt.Sprintf("node-info: timed out after %v", nodeInfoTimeout))
		}
		simplelog.Infof("node-info-collect: %s — %d failed", host, niToolsFailed)

		// --- Stream log/config files ---
		var nodeCollected []helpers.CollectedFile
		var nodeSkipped []string
		if len(info.Files) > 0 {
			nodeCollected, nodeSkipped = streamNodeFiles(c, host, info, s, nodeType, collectionMode, collectionArgs.CollectGCLogs, collectionArgs)
		}

		// --- RocksDB viewer collection (coordinator only) ---
		// Prefer the per-node autodetected RocksDB dir; fall back to the CLI flag.
		// Mirrors the resolution used by the rocksdb-disk-allocation block above so
		// queries-perf / cluster-stats / WLM / system-tables aren't silently skipped
		// when the user omits --dremio-rocksdb-dir.
		if nodeType == "coordinator" {
			rocksDBDir := info.RocksDBDir
			if rocksDBDir == "" {
				rocksDBDir = collectionArgs.DremioRocksDBDir
			}
			if rocksDBDir != "" {
				rocksArgs := RocksCollectArgs{
					Collector:           c,
					CopyStrategy:        s,
					Host:                host,
					NodeType:            nodeType,
					RocksDBDir:          rocksDBDir,
					CollectSystemTables: collectionArgs.CollectSystemTables,
					SystemTables:        collectionArgs.SystemTables,
					CollectWLM:          collectionArgs.CollectWLM,
					CollectQueriesPerf:  collectionArgs.CollectQueriesPerf,
					QueriesPerfDays:     collectionArgs.QueriesPerfNumDays,
					Days:                collectionArgs.DiagLogDays,
					StartDate:           collectionArgs.StartDate,
				}
				if rocksFiles, err := RunRocksDBCollection(rocksArgs); err != nil {
					simplelog.Errorf("RocksDB collection failed on %s: %v", host, err)
				} else {
					nodeCollected = append(nodeCollected, rocksFiles...)
				}
			} else {
				simplelog.Warningf("RocksDB collection skipped on %s: no RocksDB dir detected and --dremio-rocksdb-dir not set", host)
			}
		}

		// Include JVM diagnostic tool files in this node's totals.
		if jvmFiles := jvmFilesByHost[host]; len(jvmFiles) > 0 {
			nodeCollected = append(nodeCollected, jvmFiles...)
		}

		mu.Lock()
		collectedFiles = append(collectedFiles, nodeCollected...)
		totalSkippedFiles = append(totalSkippedFiles, nodeSkipped...)
		if len(nodeCollected) == 0 && len(info.Files) > 0 {
			totalFailedNodes = append(totalFailedNodes, host)
		}
		mu.Unlock()

		var nodeBytes int64
		for _, f := range nodeCollected {
			nodeBytes += f.Size
		}

		statusUX := fmt.Sprintf("Done: %d files (%s), %d skipped", len(nodeCollected), humanizeBytes(nodeBytes), len(nodeSkipped))
		if len(nodeSkipped) > 0 {
			names := make([]string, len(nodeSkipped))
			for i, p := range nodeSkipped {
				names[i] = filepath.Base(p)
			}
			statusUX = fmt.Sprintf("Done: %d files (%s), %d skipped (%s)", len(nodeCollected), humanizeBytes(nodeBytes), len(nodeSkipped), strings.Join(names, ", "))
		}
		if niToolsFailed > 0 {
			failedTools := make([]string, 0, len(nodeInfoToolErrors))
			for _, te := range nodeInfoToolErrors {
				if idx := strings.Index(te, ":"); idx > 0 {
					failedTools = append(failedTools, te[:idx])
				}
			}
			if len(failedTools) > 0 {
				statusUX += fmt.Sprintf(" (%s failed)", strings.Join(failedTools, ", "))
			} else {
				statusUX += fmt.Sprintf(", %d node-info tool(s) failed", niToolsFailed)
			}
		}
		consoleprint.UpdateNodeState(consoleprint.NodeState{
			Node:          host,
			Status:        consoleprint.Completed,
			StatusUX:      statusUX,
			EndProcess:    true,
			IsCoordinator: nodeType == "coordinator",
		})
	}

	// Launch log streaming for all discovered nodes.
	for host := range nodeInfoByHost {
		wg.Add(1)
		go streamNodeLogs(host)
	}

	// Orchestrator tasks (K8s data + API collections) run in parallel with log streaming.
	wg.Add(1)
	go func() {
		defer wg.Done()
		var orchestratorWg sync.WaitGroup

		orchestratorWg.Add(1)
		go func() {
			defer orchestratorWg.Done()
			consoleprint.UpdateResult("Collecting Kubernetes cluster data...")
			clusterCollection()
			consoleprint.UpdateResult("Kubernetes cluster data collected")
		}()

		orchestratorWg.Add(1)
		go func() {
			defer orchestratorWg.Done()

			// PAT-dependent REST collections (diagnosis mode only): KV store
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
		}()

		orchestratorWg.Wait()
		if collectionArgs.CollectProblematicProfiles && collectionArgs.DremioPAT != "" {
			consoleprint.UpdateResult("Waiting for node streams to download problematic profiles...")
		} else {
			consoleprint.IncrementTransfersComplete()
			consoleprint.UpdateResult("Orchestrator collection complete, waiting for node streams...")
		}
	}()

	wg.Wait()

	// Log-based profile collection — runs after all node streams complete so
	// server.log files are fully written to tmpDir.
	if collectionArgs.CollectProblematicProfiles && collectionArgs.DremioPAT != "" && len(coordinators) > 0 {
		if err := RunLogBasedProfileCollection(LogProfileArgs{
			TmpDir:           s.GetTmpDir(),
			DremioEndpoint:   collectionArgs.DremioEndpoint,
			DremioPAT:        collectionArgs.DremioPAT,
			NodeName:         coordinators[0],
			CollectionMode:   collectionArgs.CollectionMode,
			AllowInsecureSSL: collectionArgs.AllowInsecureSSL,
			RestHTTPTimeout:  collectionArgs.RestHTTPTimeout,
			Hook:             hook,
		}); err != nil {
			simplelog.Errorf("Log-based profile collection failed: %v", err)
		}
		consoleprint.IncrementTransfersComplete()
	}

	// Build summary.
	end := time.Now().UTC()
	var summaryInfo SummaryInfo
	summaryInfo.StartTimeUTC = start
	summaryInfo.EndTimeUTC = end
	summaryInfo.TotalRuntimeSeconds = end.Unix() - start.Unix()
	summaryInfo.ClusterInfo.TotalNodesAttempted = totalNodes
	summaryInfo.ClusterInfo.NumberNodesContacted = totalNodes - len(totalFailedNodes)
	summaryInfo.CollectedFiles = collectedFiles
	summaryInfo.SkippedFiles = totalSkippedFiles
	var totalBytes int64
	for _, f := range collectedFiles {
		totalBytes += f.Size
	}
	summaryInfo.TotalBytesCollected = totalBytes
	summaryInfo.Coordinators = coordinators
	summaryInfo.Executors = executors
	summaryInfo.DDCVersion = versions.GetCLIVersion()
	summaryInfo.CollectionsEnabled = collectionArgs.Enabled
	summaryInfo.CollectionsDisabled = collectionArgs.Disabled

	if len(collectedFiles) == 0 {
		return fmt.Errorf("streaming collection completed but no files were collected from %d node(s); failed nodes: %v", totalNodes, totalFailedNodes)
	}

	outString, err := summaryInfo.String()
	if err != nil {
		return err
	}

	logDistributedCollectionSummary(
		collectionMode, coordinators, executors, collectedFiles,
		nil, totalFailedNodes, totalSkippedFiles,
		totalNodes-len(totalFailedNodes), time.Since(start),
	)

	if len(totalFailedNodes) > 0 {
		simplelog.Warningf("streaming collection completed with %d failed node(s): %v", len(totalFailedNodes), totalFailedNodes)
	}

	consoleprint.UpdateResult("Creating final archive...")
	if err := s.ArchiveDiag(outString, outputLoc); err != nil {
		return err
	}
	fullPath, err := filepath.Abs(outputLoc)
	if err != nil {
		return err
	}
	consoleprint.UpdateTarballDir(fullPath)
	return nil
}

// createFlatPath returns the category directory and host prefix for flat output.
// Unlike CreatePath, it does not create a per-host subdirectory — files are
// written directly into the category directory with the host prefix in the filename.
func createFlatPath(s CopyStrategy, fileType, host, nodeType string) (catDir, hostPrefix string, err error) {
	// CreatePath returns <tmp>/<base>/<fileType>/<hostWithSuffix> and creates it.
	// We use it to derive the category dir and host prefix, then remove the
	// unwanted per-host subdirectory.
	fullPath, err := s.CreatePath(fileType, host, nodeType)
	if err != nil {
		return "", "", err
	}
	catDir = filepath.Dir(fullPath)
	hostPrefix = filepath.Base(fullPath)
	// Remove the empty per-host subdirectory that CreatePath created.
	_ = os.Remove(fullPath)
	return catDir, hostPrefix, nil
}

// computeMaxToolDuration returns the configured diagnostic tool duration for countdown display.
func computeMaxToolDuration(args Args) time.Duration {
	return time.Duration(args.DiagTimeSeconds) * time.Second
}

// runJVMCollection runs diagnostic tools in parallel per node (Phase 2),
// then heap dump synchronized across nodes (Phase 3). Errors are logged
// but do not fail the overall collection.
func runJVMCollection(c Collector, s CopyStrategy, args Args, pidByHost map[string]int, nodeTypeByHost map[string]string, maxThreads int) map[string][]helpers.CollectedFile {
	type jvmNode struct {
		host     string
		pid      int
		nodeType string
	}
	var nodes []jvmNode
	for host, pid := range pidByHost {
		if pid <= 0 {
			simplelog.Warningf("jvm-collect: skipping %s — PID is %d", host, pid)
			continue
		}
		nodes = append(nodes, jvmNode{host: host, pid: pid, nodeType: nodeTypeByHost[host]})
	}

	if len(nodes) == 0 {
		simplelog.Infof("jvm-collect: no nodes with PID > 0, skipping JVM collection")
		return nil
	}

	// Per-host JVM file tracking for inclusion in final status.
	jvmFilesMu := &sync.Mutex{}
	jvmFilesByHost := make(map[string][]helpers.CollectedFile)

	// trackJVMFile stats a local file and records it for a given host.
	trackJVMFile := func(host, path string) {
		fi, err := os.Stat(path)
		if err != nil {
			simplelog.Warningf("jvm-collect: cannot stat %s for tracking: %v", path, err)
			return
		}
		jvmFilesMu.Lock()
		jvmFilesByHost[host] = append(jvmFilesByHost[host], helpers.CollectedFile{Path: path, Size: fi.Size()})
		jvmFilesMu.Unlock()
	}

	// trackJVMDir stats all files in a directory and records them for a given host.
	trackJVMDir := func(host, dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			simplelog.Warningf("jvm-collect: cannot read dir %s for tracking: %v", dir, err)
			return
		}
		jvmFilesMu.Lock()
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			fi, err := e.Info()
			if err != nil {
				continue
			}
			jvmFilesByHost[host] = append(jvmFilesByHost[host], helpers.CollectedFile{Path: filepath.Join(dir, e.Name()), Size: fi.Size()})
		}
		jvmFilesMu.Unlock()
	}

	simplelog.Infof("jvm-collect: starting on %d node(s)", len(nodes))

	// ---- Pre-distribution: upload async-profiler binary to all nodes in parallel ----
	// This runs before the synchronized start so upload time doesn't eat into profiling time.
	asprofByHost := make(map[string]bool)      // tracks which nodes have asprof ready
	asprofDirByHost := make(map[string]string) // remote directory where asprof is installed
	if args.CollectAsyncProfiler {
		consoleprint.UpdateResult("Distributing async-profiler to nodes...")
		var distWg sync.WaitGroup
		var distMu sync.Mutex

		for _, n := range nodes {
			distWg.Add(1)
			go func(host string, pid int, nodeType string) {
				defer distWg.Done()

				consoleprint.UpdateNodeState(consoleprint.NodeState{
					Node:     host,
					StatusUX: "Uploading async-profiler",
				})

				// Check disk space
				avail, dfErr := CheckRemoteDiskSpace(c, host, "/tmp")
				if dfErr != nil {
					simplelog.Warningf("jvm-collect: disk space check failed on %s, proceeding: %v", host, dfErr)
				} else if avail < minDiskSpaceBytes {
					simplelog.Warningf("jvm-collect: insufficient disk on %s for async-profiler: %d < %d bytes, skipping", host, avail, minDiskSpaceBytes)
					return
				}

				// Detect architecture
				archStr, archErr := c.HostExecute(false, host, "uname", "-m")
				if archErr != nil {
					simplelog.Warningf("jvm-collect: uname -m failed on %s, skipping async-profiler: %v", host, archErr)
					return
				}
				archStr = strings.TrimSpace(archStr)

				// Get binary + .so
				asprofFiles, binErr := getAsprofFilesFn(archStr)
				if binErr != nil {
					simplelog.Warningf("jvm-collect: async-profiler unavailable on %s (arch=%s): %v", host, archStr, binErr)
					consoleprint.UpdateNodeState(consoleprint.NodeState{
						Node:       host,
						ToolErrors: []string{fmt.Sprintf("async-profiler: unavailable (arch=%s): %v", archStr, binErr)},
					})
					return
				}

				// Upload asprof binary + libasyncProfiler.so, then restructure into
				// the directory layout async-profiler requires: <base>/bin/asprof + <base>/lib/libasyncProfiler.so.
				// Upload as temp flat files first, then mv into the directory structure.
				localTmp, tdErr := os.MkdirTemp("", "ddc-asprof-dist-*")
				if tdErr != nil {
					simplelog.Warningf("jvm-collect: temp dir creation failed: %v", tdErr)
					return
				}
				defer os.RemoveAll(localTmp)

				binLocal := filepath.Join(localTmp, "ddc-asprof-bin")
				if err := os.WriteFile(binLocal, asprofFiles.Binary, 0o600); err != nil {
					simplelog.Warningf("jvm-collect: write asprof binary failed: %v", err)
					return
				}
				soLocal := filepath.Join(localTmp, "ddc-asprof-lib.so")
				if err := os.WriteFile(soLocal, asprofFiles.LibSO, 0600); err != nil {
					simplelog.Warningf("jvm-collect: write libasyncProfiler.so failed: %v", err)
					return
				}

				// Try /tmp first, fall back to /opt/dremio/data if noexec
				remoteDir := "/tmp"
				remoteBinTmp := remoteDir + "/ddc-asprof-bin"
				remoteSOTmp := remoteDir + "/ddc-asprof-lib.so"

				if _, err := c.CopyToHost(host, binLocal, remoteBinTmp); err != nil {
					simplelog.Warningf("jvm-collect: asprof upload failed on %s: %v", host, err)
					consoleprint.UpdateNodeState(consoleprint.NodeState{
						Node: host, ToolErrors: []string{fmt.Sprintf("async-profiler: upload failed: %v", err)},
					})
					return
				}
				if _, err := c.CopyToHost(host, soLocal, remoteSOTmp); err != nil {
					simplelog.Warningf("jvm-collect: .so upload failed on %s: %v", host, err)
					consoleprint.UpdateNodeState(consoleprint.NodeState{
						Node: host, ToolErrors: []string{fmt.Sprintf("async-profiler: .so upload failed: %v", err)},
					})
					return
				}

				// Restructure into bin/asprof + lib/libasyncProfiler.so and test execution.
				remoteBase := remoteDir + "/ddc-asprof"
				setupCmd := fmt.Sprintf(
					"rm -rf %s && mkdir -p %s/bin %s/lib && mv %s %s/bin/asprof && chmod +x %s/bin/asprof && mv %s %s/lib/libasyncProfiler.so",
					remoteBase, remoteBase, remoteBase, remoteBinTmp, remoteBase, remoteBase, remoteSOTmp, remoteBase,
				)
				if _, err := c.HostExecute(false, host, setupCmd); err != nil {
					simplelog.Warningf("jvm-collect: setup in /tmp failed on %s, trying /opt/dremio/data: %v", host, err)
					// Fallback: try /opt/dremio/data
					remoteDir = "/opt/dremio/data"
					remoteBase = remoteDir + "/ddc-asprof"
					// Re-upload to fallback location
					remoteBinTmp = remoteDir + "/ddc-asprof-bin"
					remoteSOTmp = remoteDir + "/ddc-asprof-lib.so"
					if _, err := c.CopyToHost(host, binLocal, remoteBinTmp); err != nil {
						simplelog.Warningf("jvm-collect: fallback upload failed on %s: %v", host, err)
						return
					}
					if _, err := c.CopyToHost(host, soLocal, remoteSOTmp); err != nil {
						simplelog.Warningf("jvm-collect: fallback .so upload failed on %s: %v", host, err)
						return
					}
					setupCmd = fmt.Sprintf(
						"rm -rf %s && mkdir -p %s/bin %s/lib && mv %s %s/bin/asprof && chmod +x %s/bin/asprof && mv %s %s/lib/libasyncProfiler.so",
						remoteBase, remoteBase, remoteBase, remoteBinTmp, remoteBase, remoteBase, remoteSOTmp, remoteBase,
					)
					if _, err := c.HostExecute(false, host, setupCmd); err != nil {
						simplelog.Warningf("jvm-collect: fallback setup failed on %s: %v", host, err)
						consoleprint.UpdateNodeState(consoleprint.NodeState{
							Node: host, ToolErrors: []string{fmt.Sprintf("async-profiler: setup failed: %v", err)},
						})
						return
					}
				}

				distMu.Lock()
				asprofByHost[host] = true
				asprofDirByHost[host] = remoteBase // e.g. /tmp/ddc-asprof or /opt/dremio/data/ddc-asprof
				distMu.Unlock()

				consoleprint.UpdateNodeState(consoleprint.NodeState{
					Node:     host,
					StatusUX: "async-profiler ready",
				})
				simplelog.Infof("jvm-collect: async-profiler distributed to %s (dir=%s)", host, remoteDir)
			}(n.host, n.pid, n.nodeType)
		}
		distWg.Wait()
		simplelog.Infof("jvm-collect: async-profiler distribution complete (%d/%d nodes)", len(asprofByHost), len(nodes))
	}

	// Signal N/A for Threads/Queued during JVM phases (no semaphore used).
	consoleprint.UpdateThreadStatus(-1, maxThreads, 0)
	consoleprint.UpdateResult("Waiting for synchronized JVM tool start...")

	// ---- Phase 2: Parallel diagnostic tools (JFR, jstack, top, async-profiler) ----
	var jvmWg sync.WaitGroup
	startBarrier := make(chan struct{})
	maxDuration := computeMaxToolDuration(args)

	for _, n := range nodes {
		jvmWg.Add(1)
		go func(host string, pid int, nodeType string) {
			defer jvmWg.Done()
			<-startBarrier

			var toolMu sync.Mutex
			var jvmToolErrors []string
			activeTools := make(map[string]bool)
			var atMu sync.Mutex

			// Count enabled tools to compute stagger offset for deadline.
			var numTools int
			if args.CollectJFR {
				numTools++
			}
			if args.CollectJStack {
				numTools++
			}
			if args.CollectTop {
				numTools++
			}
			if args.CollectAsyncProfiler && asprofByHost[host] {
				numTools++
			}
			totalStagger := time.Duration(numTools) * time.Second
			deadline := time.Now().Add(maxDuration + totalStagger)

			updateToolStatus := func() {
				atMu.Lock()
				names := make([]string, 0, len(activeTools))
				for name := range activeTools {
					names = append(names, name)
				}
				sort.Strings(names)
				atMu.Unlock()

				var statusUX string
				if len(names) > 0 {
					statusUX = "Running: " + strings.Join(names, ", ")
				} else {
					statusUX = "JVM diagnostic tools completed"
				}
				consoleprint.UpdateNodeState(consoleprint.NodeState{
					Node:        host,
					StatusUX:    statusUX,
					JVMDeadline: deadline,
				})
			}

			var toolWg sync.WaitGroup
			stagger := time.Duration(0)

			startTool := func(name string, fn func() error) {
				toolWg.Add(1)
				delay := stagger
				stagger += time.Second // 1s stagger to reduce JVM attach contention
				go func() {
					defer toolWg.Done()
					if delay > 0 {
						time.Sleep(delay)
					}
					atMu.Lock()
					activeTools[name] = true
					atMu.Unlock()
					updateToolStatus()

					if err := fn(); err != nil {
						simplelog.Warningf("jvm-collect: %s failed on %s: %v", name, host, err)
						toolMu.Lock()
						jvmToolErrors = append(jvmToolErrors, fmt.Sprintf("%s: %v", name, err))
						toolMu.Unlock()
					}

					atMu.Lock()
					delete(activeTools, name)
					atMu.Unlock()
					updateToolStatus()
				}()
			}

			if args.CollectJFR {
				startTool("JFR", func() error {
					catDir, hostPrefix, err := createFlatPath(s, "jfr", host, nodeType)
					if err != nil {
						return fmt.Errorf("create path: %w", err)
					}
					if err := CollectJFR(c, host, pid, args.DiagTimeSeconds, catDir, hostPrefix, time.Sleep); err != nil {
						return err
					}
					trackJVMFile(host, filepath.Join(catDir, hostPrefix+".jfr"))
					return nil
				})
			}

			if args.CollectJStack {
				startTool("jstack", func() error {
					jstackDir, err := s.CreatePath("jstack", host, nodeType)
					if err != nil {
						return fmt.Errorf("create path: %w", err)
					}
					if err := CollectJStack(c, host, pid, args.DiagTimeSeconds, jstackDir, host, time.Sleep); err != nil {
						return err
					}
					trackJVMDir(host, jstackDir)
					return nil
				})
			}

			if args.CollectTop {
				startTool("top", func() error {
					catDir, hostPrefix, err := createFlatPath(s, "top", host, nodeType)
					if err != nil {
						return fmt.Errorf("create path: %w", err)
					}
					if err := CollectTop(c, host, pid, args.DiagTimeSeconds, catDir, hostPrefix); err != nil {
						return err
					}
					trackJVMFile(host, filepath.Join(catDir, hostPrefix+"_top.txt"))
					return nil
				})
			}

			if args.CollectAsyncProfiler && asprofByHost[host] {
				startTool("async-profiler", func() error {
					remoteBase := asprofDirByHost[host] // e.g. /tmp/ddc-asprof
					remoteBin := remoteBase + "/bin/asprof"
					remoteJFR := remoteBase + "/out.jfr"
					durSeconds := args.DiagTimeSeconds
					catDir, hostPrefix, err := createFlatPath(s, "async-profiler", host, nodeType)
					if err != nil {
						return fmt.Errorf("create path: %w", err)
					}
					pidStr := strconv.Itoa(pid)

					// Run async-profiler producing JFR output.
					execCmd := fmt.Sprintf("%s -e itimer,nativemem --nativemem 500m -d %d -f %s -o jfr %s", remoteBin, durSeconds, remoteJFR, pidStr)
					if _, execErr := c.HostExecute(false, host, execCmd); execErr != nil {
						cleanCmd := fmt.Sprintf("rm -rf %s", remoteBase)
						_, _ = c.HostExecute(false, host, cleanCmd)
						return fmt.Errorf("execution failed: %w", execErr)
					}

					// Stream JFR back
					localJFR := filepath.Join(catDir, hostPrefix+"-asprof.jfr")
					if err := streamRemoteFile(c, host, remoteJFR, localJFR); err != nil {
						simplelog.Warningf("asprof: JFR stream failed on %s: %v", host, err)
					} else {
						trackJVMFile(host, localJFR)
					}

					// Cleanup entire asprof directory
					cleanCmd := fmt.Sprintf("rm -rf %s", remoteBase)
					if _, err := c.HostExecute(false, host, cleanCmd); err != nil {
						simplelog.Warningf("asprof: cleanup on %s failed: %v", host, err)
					}
					return nil
				})
			}

			toolWg.Wait()

			// Clear the JVM deadline now that tools are done.
			consoleprint.UpdateNodeState(consoleprint.NodeState{
				Node:        host,
				StatusUX:    "JVM diagnostic tools completed",
				JVMDeadline: time.Time{},
			})

			if len(jvmToolErrors) > 0 {
				consoleprint.UpdateNodeState(consoleprint.NodeState{
					Node:       host,
					ToolErrors: jvmToolErrors,
				})
			}
			simplelog.Infof("jvm-collect: %s — tools complete, %d errors", host, len(jvmToolErrors))
		}(n.host, n.pid, n.nodeType)
	}

	close(startBarrier)
	consoleprint.UpdateResult("Running JVM diagnostic tools...")
	jvmWg.Wait()
	simplelog.Infof("jvm-collect: Phase 2 (diagnostic tools) completed on %d node(s)", len(nodes))
	if args.CollectJFR || args.CollectJStack || args.CollectTop || args.CollectAsyncProfiler {
		consoleprint.IncrementTransfersComplete()
	}

	// ---- Phase 3: Heap dump (synchronized across nodes, after diagnostic tools) ----
	if args.CollectHeapDump {
		simplelog.Infof("jvm-collect: starting heap dump phase on %d node(s)", len(nodes))
		consoleprint.UpdateResult("Waiting for synchronized heap dump start...")

		var heapWg sync.WaitGroup
		heapBarrier := make(chan struct{})

		for _, n := range nodes {
			heapWg.Add(1)
			go func(host string, pid int, nodeType string) {
				defer heapWg.Done()
				<-heapBarrier

				consoleprint.UpdateNodeState(consoleprint.NodeState{
					Node:     host,
					StatusUX: "Running: heap-dump",
				})

				skipHeapDump := false
				pidStr := strconv.Itoa(pid)
				vmFlagsOut, vmErr := jcmdExec(c, host, pidStr, "VM.flags")
				if vmErr != nil {
					simplelog.Warningf("jvm-collect: VM.flags probe failed on %s, proceeding: %v", host, vmErr)
				} else {
					maxHeap, parseErr := ParseXmxBytes(vmFlagsOut)
					if parseErr != nil {
						simplelog.Warningf("jvm-collect: could not parse -Xmx on %s, proceeding: %v", host, parseErr)
					} else {
						avail, dfErr := CheckRemoteDiskSpace(c, host, "/tmp")
						if dfErr != nil {
							simplelog.Warningf("jvm-collect: disk space check failed on %s, proceeding: %v", host, dfErr)
						} else if avail < maxHeap {
							simplelog.Warningf("jvm-collect: insufficient disk on %s: %d < -Xmx %d, skipping heap dump", host, avail, maxHeap)
							skipHeapDump = true
						}
					}
				}

				if !skipHeapDump {
					catDir, hostPrefix, err := createFlatPath(s, "heap-dump", host, nodeType)
					if err != nil {
						simplelog.Errorf("jvm-collect: failed to create heap-dump path for %s: %v", host, err)
					} else {
						if err := CollectHeapDump(c, host, pid, catDir, hostPrefix); err != nil {
							simplelog.Warningf("jvm-collect: heap-dump failed on %s: %v", host, err)
							consoleprint.UpdateNodeState(consoleprint.NodeState{
								Node:       host,
								ToolErrors: []string{fmt.Sprintf("heap-dump: %v", err)},
							})
						} else {
							trackJVMFile(host, filepath.Join(catDir, hostPrefix+".hprof"))
						}
					}
				}

				consoleprint.UpdateNodeState(consoleprint.NodeState{
					Node:     host,
					StatusUX: "Heap dump complete",
				})
			}(n.host, n.pid, n.nodeType)
		}

		close(heapBarrier)
		consoleprint.UpdateResult("Running heap dumps...")
		heapWg.Wait()
		simplelog.Infof("jvm-collect: Phase 3 (heap dump) completed on %d node(s)", len(nodes))
		consoleprint.IncrementTransfersComplete()
	}

	// Restore thread status for log streaming phase.
	consoleprint.UpdateThreadStatus(0, maxThreads, 0)
	consoleprint.UpdateResult("JVM collection complete, starting log streaming...")
	return jvmFilesByHost
}
