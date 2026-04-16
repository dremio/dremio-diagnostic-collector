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
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/simplelog"
)

// configAllowlistPatterns lists glob patterns for known Dremio config files.
// Only files matching at least one pattern are returned for fileType "config".
var configAllowlistPatterns = []string{
	"dremio.conf",
	"dremio-env",
	"logback*.xml",
	"*.json",
	"core-site.xml",
	"*-site.xml",
	"*.jks",
	"*.keytab",
}

// RemoteFileInfo describes a single file discovered on a remote node.
type RemoteFileInfo struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	FileType string `json:"file_type"` // "log", "config", "gc-log", or "queries"
	ModTime  int64  `json:"mod_time"`  // last modification time as Unix epoch seconds (0 if unknown)
}

// RemoteNodeInfo holds the result of remote node discovery. Fields are
// populated on a best-effort basis — individual command failures leave
// fields at zero-values and are logged as warnings.
type RemoteNodeInfo struct {
	LogDir        string           `json:"log_dir"`
	ConfDir       string           `json:"conf_dir"`
	RocksDBDir    string           `json:"rocksdb_dir"`
	DremioPID     int              `json:"dremio_pid"`
	ChecksumTool  string           `json:"checksum_tool"`
	GzipAvailable bool             `json:"gzip_available"`
	Files         []RemoteFileInfo `json:"files"`
}

// HostExecutor runs a shell command on a remote host and returns combined
// stdout. Implementations wrap Collector.HostExecute with mask=false.
type HostExecutor func(host string, args ...string) (string, error)

// Well-known Dremio directory candidates. Discovery tries each in order
// and uses the first that exists.
var (
	logDirCandidates  = []string{"/var/log/dremio", "/opt/dremio/log", "/opt/dremio/data/log"}
	confDirCandidates = []string{"/opt/dremio/conf", "/etc/dremio"}
)

// runDiscovery executes a sequence of lightweight shell commands on a remote
// host to enumerate log files, config files, GC logs, and the Dremio PID.
// Individual command failures are logged and do not abort the overall
// discovery — partial results are always returned.
func RunDiscovery(executor HostExecutor, host, logDir, confDir string) (*RemoteNodeInfo, error) {
	if host == "" {
		return nil, fmt.Errorf("runDiscovery: host is empty")
	}
	info := &RemoteNodeInfo{}
	var anySuccess bool

	// 1. Find the log directory — user-provided path overrides probing.
	if logDir != "" {
		info.LogDir = logDir
	} else {
		info.LogDir = probeDir(executor, host, logDirCandidates)
	}
	if info.LogDir != "" {
		anySuccess = true
		files, err := listFiles(executor, host, info.LogDir, "log", "2")
		if err != nil {
			simplelog.Warningf("DiscoverFiles: failed to list log files on %v: %v", host, err)
		} else {
			// Reclassify well-known log files into specific types so they
			// are routed to the correct output directories.
			for i := range files {
				base := baseName(files[i].Path)
				switch {
				case strings.HasPrefix(base, "gc") && strings.Contains(base, ".log"):
					files[i].FileType = "gc-log"
				case strings.Contains(base, ".gc") || strings.HasSuffix(base, ".gc"):
					files[i].FileType = "gc-log"
				case strings.HasPrefix(base, "queries."):
					files[i].FileType = "queries"
				}
			}
			info.Files = append(info.Files, files...)
		}
	}

	// 2. Find the config directory — user-provided path overrides probing.
	if confDir != "" {
		info.ConfDir = confDir
	} else {
		info.ConfDir = probeDir(executor, host, confDirCandidates)
	}
	if info.ConfDir != "" {
		anySuccess = true
		files, err := listFiles(executor, host, info.ConfDir, "config", "1")
		if err != nil {
			simplelog.Warningf("DiscoverFiles: failed to list config files on %v: %v", host, err)
		} else {
			info.Files = append(info.Files, files...)
		}
	}

	// 3. Detect RocksDB path from dremio.conf (paths.local + /db).
	if info.ConfDir != "" {
		info.RocksDBDir = detectRocksDBDir(executor, host, info.ConfDir)
	}

	// 4. Find Dremio PID.
	pid, err := discoverPID(executor, host)
	if err != nil {
		simplelog.Warningf("DiscoverFiles: failed to find Dremio PID on %v: %v", host, err)
	} else {
		info.DremioPID = pid
		if pid > 0 {
			anySuccess = true
		}
	}

	// 5. Probe for a checksum tool (sha256sum preferred, md5sum fallback).
	info.ChecksumTool = probeChecksumTool(executor, host)
	if info.ChecksumTool != "" {
		simplelog.Infof("RunDiscovery: checksum tool on %v: %v", host, info.ChecksumTool)
	} else {
		simplelog.Infof("RunDiscovery: no checksum tool found on %v", host)
	}

	// 6. Probe for gzip availability (used for compressed streaming).
	info.GzipAvailable = probeGzip(executor, host)
	if info.GzipAvailable {
		simplelog.Infof("RunDiscovery: gzip available on %v", host)
	} else {
		simplelog.Infof("RunDiscovery: gzip not available on %v", host)
	}

	if !anySuccess {
		return info, fmt.Errorf("DiscoverFiles: all discovery commands failed on host %v", host)
	}
	return info, nil
}

// probeDir checks candidate directories and returns the first that exists
// and contains at least one regular file. Empty directories are skipped.
func probeDir(executor HostExecutor, host string, candidates []string) string {
	for _, dir := range candidates {
		out, err := executor(host, "test", "-d", dir, "&&", "echo", "exists")
		if err != nil || !strings.Contains(out, "exists") {
			continue
		}
		// Dir exists — check it contains at least one file.
		filesOut, err := executor(host, "find", "-L", dir, "-maxdepth", "1", "-type", "f", "-print", "-quit")
		if err != nil || strings.TrimSpace(filesOut) == "" {
			simplelog.Infof("probeDir: %v exists but is empty on %v, skipping", dir, host)
			continue
		}
		return dir
	}
	return ""
}

// listFiles runs stat/ls on a directory and parses the output into RemoteFileInfo
// entries. maxDepth controls how deep find recurses ("1" for top-level only,
// "2" to include one level of subdirectories like archive/).
func listFiles(executor HostExecutor, host, dir, fileType, maxDepth string) ([]RemoteFileInfo, error) {
	// Try stat first for accurate sizes and modification times. Use -L to
	// dereference symlinks so we get the target file metadata.
	// Format: <mod_epoch> <size> <path>
	out, err := executor(host, "find", "-L", dir, "-maxdepth", maxDepth, "-type", "f", "-exec", "stat", "-L", "-c", "'%Y %s %n'", "{}", "\\;")
	if err == nil && strings.TrimSpace(out) != "" {
		files := parseStatOutput(out, fileType)
		if len(files) > 0 {
			return filterFiles(files, fileType), nil
		}
	}

	// Fallback to ls -laL. The -L flag dereferences symlinks so sizes
	// reflect the target file, not the symlink path length.
	out, err = executor(host, "ls", "-laL", dir)
	if err != nil {
		return nil, fmt.Errorf("ls -laL %v failed: %w", dir, err)
	}
	return filterFiles(parseLsOutput(out, dir, fileType), fileType), nil
}

// parseStatOutput parses output from: stat -c '%Y %s %n' <files>
// Each line: <mod_epoch> <size> <full-path>
func parseStatOutput(output, fileType string) []RemoteFileInfo {
	var files []RemoteFileInfo
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		// Strip surrounding single-quotes that some shells add from the format string.
		line = strings.Trim(line, "'")
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 3)
		if len(parts) != 3 {
			simplelog.Warningf("parseStatOutput: unexpected line format: %q", line)
			continue
		}
		modTime, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
		if err != nil {
			simplelog.Warningf("parseStatOutput: failed to parse mod time from %q: %v", line, err)
			continue
		}
		size, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
		if err != nil {
			simplelog.Warningf("parseStatOutput: failed to parse size from %q: %v", line, err)
			continue
		}
		path := strings.TrimSpace(parts[2])
		if path == "" {
			continue
		}
		files = append(files, RemoteFileInfo{
			Path:     path,
			Size:     size,
			FileType: fileType,
			ModTime:  modTime,
		})
	}
	return files
}

// parseLsOutput parses output from ls -la into RemoteFileInfo entries.
// Expected format: -rw-r--r-- 1 dremio dremio 12345 Jul 10 14:30 filename
// Directories (lines starting with 'd') and the total line are skipped.
func parseLsOutput(output, dir, fileType string) []RemoteFileInfo {
	var files []RemoteFileInfo
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "total") || strings.HasPrefix(line, "d") {
			continue
		}
		// ls -la fields: perms links owner group size month day time name
		fields := strings.Fields(line)
		if len(fields) < 9 {
			continue
		}
		size, err := strconv.ParseInt(fields[4], 10, 64)
		if err != nil {
			continue
		}
		// Filename is everything from field 8 onwards (handles spaces in names).
		name := strings.Join(fields[8:], " ")
		// Symlink entries show "filename -> target" — strip the target.
		if idx := strings.Index(name, " -> "); idx >= 0 {
			name = name[:idx]
		}
		if name == "." || name == ".." {
			continue
		}
		fullPath := dir + "/" + name
		files = append(files, RemoteFileInfo{
			Path:     fullPath,
			Size:     size,
			FileType: fileType,
		})
	}
	return files
}

// selectPID picks the best PID from a list: returns the first non-1 PID,
// falls back to PID 1 if it's the only match, returns 0 if empty.
func selectPID(pids []int) int {
	if len(pids) == 0 {
		return 0
	}
	for _, pid := range pids {
		if pid != 1 {
			return pid
		}
	}
	// Only PID 1 found — use it as fallback (container scenario).
	return pids[0]
}

// parsePIDLines parses newline-separated PID output into a slice of ints,
// logging warnings for unparseable lines.
func parsePIDLines(out, host, source string) []int {
	out = strings.TrimSpace(out)
	if out == "" {
		return nil
	}
	var pids []int
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		pid, err := strconv.Atoi(line)
		if err != nil {
			simplelog.Warningf("discoverPID(%s): failed to parse PID from %q on %v: %v", source, line, host, err)
			continue
		}
		pids = append(pids, pid)
	}
	return pids
}

// parseJcmdOutput parses "jcmd -l" output looking for Dremio processes.
// Each line has format: "<pid> <main-class-or-jar> [args...]"
// Filters out jcmd's own process (main class "jdk.jcmd/sun.tools.jcmd.JCmd"
// or "sun.tools.jcmd.JCmd").
func parseJcmdOutput(out, host string) []int {
	out = strings.TrimSpace(out)
	if out == "" {
		return nil
	}
	var pids []int
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Split into PID and remainder.
		parts := strings.SplitN(line, " ", 2)
		pid, err := strconv.Atoi(parts[0])
		if err != nil {
			simplelog.Warningf("discoverPID(jcmd): failed to parse PID from %q on %v: %v", line, host, err)
			continue
		}
		// Filter out jcmd's own process.
		if len(parts) > 1 {
			mainClass := parts[1]
			if strings.Contains(mainClass, "sun.tools.jcmd.JCmd") {
				continue
			}
		}
		// Check for dremio in the line (case-insensitive).
		if strings.Contains(strings.ToLower(line), "dremio") {
			pids = append(pids, pid)
		}
	}
	return pids
}

// discoverPID finds the Dremio Java process PID using a three-step cascade:
//  1. jcmd -l — lists JVM processes; most precise, finds dremio by class/jar name
//  2. pgrep -x java — exact process name match; picks up java processes
//  3. pgrep -f dremio.*java — broadest match (original behavior, final fallback)
//
// Returns 0 if no Dremio process is found. PID 1 is filtered out unless it
// is the only match (some containers run dremio as PID 1).
func discoverPID(executor HostExecutor, host string) (int, error) {
	// Step 1: jcmd -l — most precise.
	if out, err := executor(host, "jcmd", "-l"); err == nil {
		if pids := parseJcmdOutput(out, host); len(pids) > 0 {
			simplelog.Debugf("discoverPID: jcmd -l found PIDs %v on %v", pids, host)
			return selectPID(pids), nil
		}
	}

	// Step 2: pgrep -x java — exact process name.
	if out, err := executor(host, "pgrep", "-x", "java"); err == nil {
		if pids := parsePIDLines(out, host, "pgrep -x java"); len(pids) > 0 {
			simplelog.Debugf("discoverPID: pgrep -x java found PIDs %v on %v", pids, host)
			return selectPID(pids), nil
		}
	}

	// Step 3: pgrep -f dremio.*java — broadest match.
	if out, err := executor(host, "pgrep", "-f", "dremio.*java"); err == nil {
		if pids := parsePIDLines(out, host, "pgrep -f"); len(pids) > 0 {
			simplelog.Debugf("discoverPID: pgrep -f found PIDs %v on %v", pids, host)
			return selectPID(pids), nil
		}
	}

	// No step found a PID.
	return 0, nil
}

// filterFiles applies fileType-specific filters to a list of RemoteFileInfo.
// For "config" files, only allowlisted base names are kept and paths with
// double-dot components (e.g. ..data/) are excluded.
func filterFiles(files []RemoteFileInfo, fileType string) []RemoteFileInfo {
	var result []RemoteFileInfo
	for _, f := range files {
		if hasDoubleDotComponent(f.Path) {
			continue
		}
		if fileType == "config" && !matchesConfigAllowlist(baseName(f.Path)) {
			continue
		}
		result = append(result, f)
	}
	return result
}

// matchesConfigAllowlist checks whether a filename matches any of the known
// Dremio config file patterns.
func matchesConfigAllowlist(name string) bool {
	for _, pattern := range configAllowlistPatterns {
		if matched, _ := filepath.Match(pattern, name); matched {
			return true
		}
	}
	return false
}

// hasDoubleDotComponent returns true if any path component starts with "..".
func hasDoubleDotComponent(path string) bool {
	for _, part := range strings.Split(path, "/") {
		if strings.HasPrefix(part, "..") {
			return true
		}
	}
	return false
}

// probeChecksumTool checks whether sha256sum or md5sum is available on the
// remote host using POSIX "command -v". Returns the tool name or "" if
// neither is found. sha256sum is preferred when both are available.
func probeChecksumTool(executor HostExecutor, host string) string {
	out, err := executor(host, "command", "-v", "sha256sum")
	if err == nil && strings.TrimSpace(out) != "" {
		return "sha256sum"
	}
	out, err = executor(host, "command", "-v", "md5sum")
	if err == nil && strings.TrimSpace(out) != "" {
		return "md5sum"
	}
	return ""
}

// probeGzip checks whether gzip is available on the remote host using POSIX
// "command -v gzip". Returns true if gzip is found, false otherwise. Probe
// failure is advisory — it does not abort discovery.
func probeGzip(executor HostExecutor, host string) bool {
	out, err := executor(host, "command", "-v", "gzip")
	return err == nil && strings.TrimSpace(out) != ""
}

// detectRocksDBDir reads dremio.conf from confDir on the remote host and
// extracts the RocksDB path via paths.local + /db. Returns "" if the
// config cannot be read or parsed — this is advisory, not fatal.
func detectRocksDBDir(executor HostExecutor, host, confDir string) string {
	out, err := executor(host, "cat", confDir+"/dremio.conf")
	if err != nil {
		simplelog.Infof("detectRocksDBDir: could not read dremio.conf on %s: %v", host, err)
		return ""
	}
	dremioHome := "/opt/dremio"
	hc, err := conf.NewDremioHOCONConfigFromString(out, dremioHome)
	if err != nil {
		simplelog.Infof("detectRocksDBDir: could not parse dremio.conf on %s: %v", host, err)
		return ""
	}
	rocksDir := hc.GetRocksDBPath(dremioHome)
	if rocksDir != "" {
		simplelog.Infof("detectRocksDBDir: detected RocksDB dir on %s: %s", host, rocksDir)
	}
	return rocksDir
}

// baseName returns the last component of a /-separated path.
func baseName(path string) string {
	idx := strings.LastIndex(path, "/")
	if idx < 0 {
		return path
	}
	return path[idx+1:]
}
