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
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/simplelog"
)

// jcmdTimeoutSeconds is the per-command timeout for remote jcmd calls.
// Stale jcmd processes can hold the JVM attach socket, causing new calls
// to hang indefinitely. The timeout ensures we fail fast.
const jcmdTimeoutSeconds = 30

// jcmdExec runs a jcmd subcommand on a remote host with a timeout wrapper.
// args are the jcmd arguments after the PID (e.g. "VM.flags", "JFR.start", ...).
// Note: HostExecute already wraps args in "sh -c <joined args>", so we pass
// the timeout+jcmd as a single pre-quoted string to avoid double sh -c wrapping.
func jcmdExec(c Collector, host string, pidStr string, args ...string) (string, error) {
	cmd := fmt.Sprintf("timeout %d jcmd %s %s", jcmdTimeoutSeconds, pidStr, strings.Join(args, " "))
	return c.HostExecute(false, host, cmd)
}

// CollectJStack runs jcmd Thread.print on a remote host until the duration
// elapses, writing each thread dump to a separate timestamped file. Iterations
// are paced by sleepFn (tests pass a no-op to skip the wait).
func CollectJStack(c Collector, host string, pid int, durationSeconds int, outDir string, nodeName string, sleepFn func(time.Duration)) error {
	simplelog.Infof("jstack: starting collection on %s (pid=%d, duration=%ds)", host, pid, durationSeconds)

	if err := os.MkdirAll(outDir, DirPerms); err != nil {
		return fmt.Errorf("jstack: failed to create output dir %s: %w", outDir, err)
	}

	pidStr := strconv.Itoa(pid)
	deadline := time.Now().Add(time.Duration(durationSeconds) * time.Second)
	for i := 0; time.Now().Before(deadline); i++ {
		out, err := jcmdExec(c, host, pidStr, "Thread.print", "-l")
		if err != nil {
			simplelog.Warningf("jstack: iteration %d failed on %s: %v", i, host, err)
			return fmt.Errorf("jstack: HostExecute failed on %s iteration %d: %w", host, i, err)
		}

		ts := time.Now().Format("2006-01-02_15_04_05")
		filename := filepath.Join(outDir, fmt.Sprintf("threadDump-%s-%s-%d.txt", nodeName, ts, i))
		if err := os.WriteFile(filename, []byte(out), 0o600); err != nil {
			return fmt.Errorf("jstack: failed to write %s: %w", filename, err)
		}
		simplelog.Debugf("jstack: saved %s", filename)
		sleepFn(time.Second)
	}

	simplelog.Infof("jstack: completed collection on %s", host)
	return nil
}

// CollectTop runs top -H on a remote host to capture thread-level CPU usage.
// The LINES=100 environment variable is set via a sh -c wrapper so the remote
// shell expands it properly.
func CollectTop(c Collector, host string, pid int, durationSeconds int, outDir string, nodeName string) error {
	simplelog.Infof("top: starting collection on %s (pid=%d, duration=%ds)", host, pid, durationSeconds)

	if err := os.MkdirAll(outDir, DirPerms); err != nil {
		return fmt.Errorf("top: failed to create output dir %s: %w", outDir, err)
	}

	// Run top with per-thread view of the Dremio process, one iteration per second.
	cmd := fmt.Sprintf("top -H -p %d -d 1 -n %d -bw 512", pid, durationSeconds)
	out, err := c.HostExecute(false, host, cmd)
	if err != nil {
		simplelog.Warningf("top: HostExecute failed on %s: %v", host, err)
		return fmt.Errorf("top: HostExecute failed on %s: %w", host, err)
	}

	filename := filepath.Join(outDir, nodeName+"_top.txt")
	if err := os.WriteFile(filename, []byte(out), 0o600); err != nil {
		return fmt.Errorf("top: failed to write %s: %w", filename, err)
	}

	simplelog.Infof("top: completed on %s, wrote %s", host, filename)
	return nil
}

// CollectJVMFlags captures VM.flags and VM.system_properties from a remote host.
// If one subcommand fails the other is still attempted — partial results are written.
func CollectJVMFlags(c Collector, host string, pid int, outDir string) error {
	simplelog.Infof("jvm-flags: starting collection on %s (pid=%d)", host, pid)

	if err := os.MkdirAll(outDir, DirPerms); err != nil {
		return fmt.Errorf("jvm-flags: failed to create output dir %s: %w", outDir, err)
	}

	pidStr := strconv.Itoa(pid)
	var combined string
	var firstErr error

	// Wrap jcmd calls with a timeout via jcmdExec. Stale jcmd processes on the
	// target JVM can hold the attach socket, causing new jcmd calls to hang
	// indefinitely.
	// Collect VM.flags
	flagsOut, err := jcmdExec(c, host, pidStr, "VM.flags")
	if err != nil {
		simplelog.Warningf("jvm-flags: VM.flags failed on %s: %v", host, err)
		firstErr = fmt.Errorf("jvm-flags: VM.flags failed on %s: %w", host, err)
		combined += fmt.Sprintf("=== VM.flags (ERROR: %v) ===\n", err)
	} else {
		combined += "=== VM.flags ===\n" + flagsOut + "\n"
	}

	// Collect VM.system_properties
	propsOut, err := jcmdExec(c, host, pidStr, "VM.system_properties")
	if err != nil {
		simplelog.Warningf("jvm-flags: VM.system_properties failed on %s: %v", host, err)
		if firstErr == nil {
			firstErr = fmt.Errorf("jvm-flags: VM.system_properties failed on %s: %w", host, err)
		}
		combined += fmt.Sprintf("=== VM.system_properties (ERROR: %v) ===\n", err)
	} else {
		combined += "=== VM.system_properties ===\n" + propsOut + "\n"
	}

	// Always write whatever we collected
	filename := filepath.Join(outDir, "jvm_settings.txt")
	if writeErr := os.WriteFile(filename, []byte(combined), 0o600); writeErr != nil {
		return fmt.Errorf("jvm-flags: failed to write %s: %w", filename, writeErr)
	}

	if firstErr != nil {
		simplelog.Infof("jvm-flags: partial collection on %s (some commands failed), wrote %s", host, filename)
		return firstErr
	}

	simplelog.Infof("jvm-flags: completed on %s, wrote %s", host, filename)
	return nil
}

// CollectJFR runs a Java Flight Recording on a remote host, streams the
// resulting .jfr file back, and cleans up the remote temp file.
// sleepFn is injected so tests can skip the real wait.
func CollectJFR(c Collector, host string, pid int, durationSeconds int, outDir string, nodeName string, sleepFn func(time.Duration)) error {
	simplelog.Infof("jfr: starting collection on %s (pid=%d, duration=%ds)", host, pid, durationSeconds)

	if err := os.MkdirAll(outDir, DirPerms); err != nil {
		return fmt.Errorf("jfr: failed to create output dir %s: %w", outDir, err)
	}

	pidStr := strconv.Itoa(pid)
	remotePath := fmt.Sprintf("/tmp/ddc-jfr-%s.jfr", host)

	// Best-effort: unlock commercial features (needed on older JVMs)
	if _, err := jcmdExec(c, host, pidStr, "VM.unlock_commercial_features"); err != nil {
		simplelog.Debugf("jfr: unlock_commercial_features failed on %s (may be unnecessary): %v", host, err)
	}

	// Best-effort: stop any prior recording with this name
	if _, err := jcmdExec(c, host, pidStr, "JFR.stop", "name=DREMIO_JFR"); err != nil {
		simplelog.Debugf("jfr: prior JFR.stop on %s (expected if no prior recording): %v", host, err)
	}

	// Start JFR recording
	startCmd := fmt.Sprintf("name=DREMIO_JFR settings=profile maxage=%ds filename=%s dumponexit=true", durationSeconds, remotePath)
	if _, err := jcmdExec(c, host, pidStr, "JFR.start", startCmd); err != nil {
		return fmt.Errorf("jfr: JFR.start failed on %s: %w", host, err)
	}

	// Wait for the recording duration
	sleepFn(time.Duration(durationSeconds) * time.Second)

	// Dump and stop
	if _, err := jcmdExec(c, host, pidStr, "JFR.dump", "name=DREMIO_JFR"); err != nil {
		simplelog.Warningf("jfr: JFR.dump failed on %s: %v", host, err)
	}
	if _, err := jcmdExec(c, host, pidStr, "JFR.stop", "name=DREMIO_JFR"); err != nil {
		simplelog.Warningf("jfr: JFR.stop failed on %s: %v", host, err)
	}

	// Stream the file back
	localPath := filepath.Join(outDir, nodeName+".jfr")
	streamErr := streamRemoteFile(c, host, remotePath, localPath)

	// Cleanup remote temp file regardless of stream outcome
	if _, err := c.HostExecute(false, host, "rm", "-f", remotePath); err != nil {
		simplelog.Warningf("jfr: cleanup of %s on %s failed: %v", remotePath, host, err)
	}

	if streamErr != nil {
		return fmt.Errorf("jfr: stream failed on %s: %w", host, streamErr)
	}

	simplelog.Infof("jfr: completed on %s, wrote %s", host, localPath)
	return nil
}

// CollectHeapDump captures a heap dump from a remote host via jmap,
// streams the raw .hprof back, and cleans up remote temp files.
// No remote compression is performed — the transfer layer (SPDY) already
// applies compression, and the final tar archive compresses the output.
func CollectHeapDump(c Collector, host string, pid int, outDir string, nodeName string) error {
	simplelog.Infof("heap-dump: starting collection on %s (pid=%d)", host, pid)

	if err := os.MkdirAll(outDir, DirPerms); err != nil {
		return fmt.Errorf("heap-dump: failed to create output dir %s: %w", outDir, err)
	}

	pidStr := strconv.Itoa(pid)
	remoteRaw := fmt.Sprintf("/tmp/ddc-heapdump-%s.hprof", host)

	// Capture heap dump
	if _, err := c.HostExecute(false, host, "jmap", fmt.Sprintf("-dump:format=b,file=%s", remoteRaw), pidStr); err != nil {
		return fmt.Errorf("heap-dump: jmap failed on %s: %w", host, err)
	}

	localPath := filepath.Join(outDir, nodeName+".hprof")
	streamErr := streamRemoteFile(c, host, remoteRaw, localPath)

	// Best-effort cleanup of remote temp file
	if _, err := c.HostExecute(false, host, "rm", "-f", remoteRaw); err != nil {
		simplelog.Warningf("heap-dump: cleanup on %s failed: %v", host, err)
	}

	if streamErr != nil {
		return fmt.Errorf("heap-dump: stream failed on %s: %w", host, streamErr)
	}

	simplelog.Infof("heap-dump: completed on %s, wrote %s", host, localPath)
	return nil
}

// CollectAsyncProfiler runs async-profiler (asprof) on a remote host against
// the Dremio JVM. It detects the remote architecture via uname -m, uploads
// the matching binary via CopyToHost, executes it, streams the result back,
// and cleans up all remote artifacts. asprofBinary is the pre-resolved binary
// bytes (caller obtains via jvmcollect.GetAsprofBinary) so tests can inject
// synthetic content without touching embed vars.
func CollectAsyncProfiler(c Collector, host string, pid int, durationSeconds int, outDir string, nodeName string, asprofBinary []byte) error {
	simplelog.Infof("asprof: starting collection on %s (pid=%d, duration=%ds)", host, pid, durationSeconds)

	if len(asprofBinary) == 0 {
		simplelog.Warningf("asprof: empty binary provided, skipping collection on %s", host)
		return nil
	}

	if err := os.MkdirAll(outDir, DirPerms); err != nil {
		return fmt.Errorf("asprof: failed to create output dir %s: %w", outDir, err)
	}

	pidStr := strconv.Itoa(pid)
	remoteBinBase := fmt.Sprintf("ddc-asprof-%s", host)
	remoteBin := "/tmp/" + remoteBinBase
	remoteOut := fmt.Sprintf("/tmp/ddc-asprof-out-%s.jfr", host)

	// Write binary to a local temp file whose basename matches the expected
	// remote path. CopyToHost (K8s impl) uses tar which preserves the source
	// filename — the basename must match so chmod/exec target the right file.
	tmpDir, err := os.MkdirTemp("", "ddc-asprof-*")
	if err != nil {
		return fmt.Errorf("asprof: failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tmpPath := filepath.Join(tmpDir, remoteBinBase)
	if err := os.WriteFile(tmpPath, asprofBinary, 0o600); err != nil {
		return fmt.Errorf("asprof: failed to write temp file: %w", err)
	}

	// Upload binary to remote host
	if _, err := c.CopyToHost(host, tmpPath, remoteBin); err != nil {
		return fmt.Errorf("asprof: CopyToHost failed on %s: %w", host, err)
	}

	// Make it executable (needed even though local perms are 0700,
	// because tar extraction may not preserve them on all transports)
	if _, err := c.HostExecute(false, host, "chmod", "+x", remoteBin); err != nil {
		if _, rmErr := c.HostExecute(false, host, "rm", "-f", remoteBin); rmErr != nil {
			simplelog.Warningf("asprof: cleanup of %s on %s failed: %v", remoteBin, host, rmErr)
		}
		return fmt.Errorf("asprof: chmod failed on %s: %w", host, err)
	}

	// Execute asprof: CPU profile for the given duration, output as JFR format
	_, execErr := c.HostExecute(false, host, remoteBin, "-d", strconv.Itoa(durationSeconds), "-f", remoteOut, "-o", "jfr", pidStr)

	// Always attempt cleanup of binary and output regardless of exec result
	defer func() {
		if _, err := c.HostExecute(false, host, "rm", "-f", remoteBin, remoteOut); err != nil {
			simplelog.Warningf("asprof: cleanup on %s failed: %v", host, err)
		}
	}()

	if execErr != nil {
		return fmt.Errorf("asprof: execution failed on %s: %w", host, execErr)
	}

	// Stream results back
	localPath := filepath.Join(outDir, nodeName+"-asprof.jfr")
	if err := streamRemoteFile(c, host, remoteOut, localPath); err != nil {
		return fmt.Errorf("asprof: stream failed on %s: %w", host, err)
	}

	simplelog.Infof("asprof: completed on %s, wrote %s", host, localPath)
	return nil
}

// probeRemoteFileSize returns the size in bytes of a remote file using stat.
// It tries GNU stat first (-c %s), then BSD stat (-f %z) as a fallback.
// Returns 0 if the size cannot be determined (best-effort, never fatal).
func probeRemoteFileSize(c Collector, host, remotePath string) int64 {
	out, err := c.HostExecute(false, host, "stat", "-c", "%s", remotePath)
	if err != nil {
		out, err = c.HostExecute(false, host, "stat", "-f", "%z", remotePath)
	}
	if err != nil {
		return 0
	}
	n, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// streamRemoteFile streams a remote file to a local path via the collector's
// StreamFromHost method, reporting transfer progress (including percentage
// when the remote file size can be probed) to the TUI.
func streamRemoteFile(c Collector, host, remotePath, localPath string) error {
	expectedSize := probeRemoteFileSize(c, host, remotePath)

	f, err := os.Create(localPath) // #nosec G304 -- localPath is derived from controlled output dir
	if err != nil {
		return fmt.Errorf("failed to create local file %s: %w", localPath, err)
	}
	defer f.Close()

	filename := filepath.Base(localPath)
	pw := &progressWriter{w: f, expectedSize: expectedSize, host: host, filename: filename}

	if err := c.StreamFromHost(host, remotePath, pw, false); err != nil {
		_ = os.Remove(localPath)
		return fmt.Errorf("StreamFromHost %s: %w", remotePath, err)
	}
	return nil
}

// CheckRemoteDiskSpace probes available disk space on a remote host at the given
// path using `df -P`. Returns available bytes. Errors are advisory (K011) — callers
// should warn and proceed, never block collection on failure.
func CheckRemoteDiskSpace(c Collector, host string, path string) (uint64, error) {
	out, err := c.HostExecute(false, host, "df", "-P", path)
	if err != nil {
		return 0, fmt.Errorf("df -P failed on %s: %w", host, err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 {
		return 0, fmt.Errorf("df -P on %s: expected at least 2 lines, got %d", host, len(lines))
	}
	// POSIX df -P second line: Filesystem 1024-blocks Used Available Capacity Mounted
	fields := strings.Fields(lines[1])
	if len(fields) < 4 {
		return 0, fmt.Errorf("df -P on %s: expected at least 4 fields in data line, got %d: %q", host, len(fields), lines[1])
	}
	availKB, err := strconv.ParseUint(fields[3], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("df -P on %s: failed to parse available blocks %q: %w", host, fields[3], err)
	}
	return availKB * 1024, nil
}

// xmxRegex matches the resolved MaxHeapSize in jcmd VM.flags output.
// jcmd prints the resolved value in bytes, e.g. -XX:MaxHeapSize=8589934592.
var xmxRegex = regexp.MustCompile(`-XX:MaxHeapSize=(\d+)`)

// ParseXmxBytes extracts the -XX:MaxHeapSize value (in bytes) from jcmd VM.flags output.
// Returns an error if the pattern is not found.
func ParseXmxBytes(vmFlagsOutput string) (uint64, error) {
	m := xmxRegex.FindStringSubmatch(vmFlagsOutput)
	if m == nil {
		return 0, fmt.Errorf("MaxHeapSize not found in VM.flags output")
	}
	val, err := strconv.ParseUint(m[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse MaxHeapSize value %q: %w", m[1], err)
	}
	return val, nil
}
