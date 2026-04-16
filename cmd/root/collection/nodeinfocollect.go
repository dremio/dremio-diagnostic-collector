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

	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/simplelog"
)

// CollectOSInfo runs OS-level shell commands on a remote host and writes the
// combined output to os_info.txt in outDir. Individual command failures are
// logged and skipped (K013) — partial results are always written.
func CollectOSInfo(c Collector, host string, outDir string) error {
	simplelog.Infof("os-info: starting collection on %s", host)

	if err := os.MkdirAll(outDir, DirPerms); err != nil {
		return fmt.Errorf("os-info: failed to create output dir %s: %w", outDir, err)
	}

	// Commands match the local OSConfigCollector format.
	// Glob commands use a single string arg — SSH passes it to the remote
	// shell which handles glob expansion. Do NOT wrap in sh -c: the remote
	// sshd already runs the command via /bin/sh -c, so an extra sh -c layer
	// mis-splits the argument (sh -c only takes the NEXT token as the cmd).
	commands := []struct {
		header string
		args   []string
	}{
		{"cat /etc/*-release", []string{"cat /etc/*-release"}},
		{"uname -r", []string{"uname", "-r"}},
		{"cat /etc/issue", []string{"cat", "/etc/issue"}},
		{"cat /proc/sys/kernel/hostname", []string{"cat", "/proc/sys/kernel/hostname"}},
		{"cat /proc/meminfo", []string{"cat", "/proc/meminfo"}},
		{"lscpu", []string{"lscpu"}},
		{"mount", []string{"mount"}},
		{"lsblk", []string{"lsblk"}},
		{"df -h", []string{"df", "-h"}},
		{"cat /proc/loadavg", []string{"cat", "/proc/loadavg"}},
	}

	var b strings.Builder
	for _, cmd := range commands {
		fmt.Fprintf(&b, "___\n>>> %s\n", cmd.header)
		out, err := c.HostExecute(false, host, cmd.args...)
		if err != nil {
			simplelog.Warningf("os-info: %s failed on %s: %v", cmd.header, host, err)
			continue
		}
		b.WriteString(out)
		if !strings.HasSuffix(out, "\n") {
			b.WriteByte('\n')
		}
	}

	filename := filepath.Join(outDir, "os_info.txt")
	if err := os.WriteFile(filename, []byte(b.String()), 0o600); err != nil {
		return fmt.Errorf("os-info: failed to write %s: %w", filename, err)
	}

	simplelog.Infof("os-info: completed on %s, wrote %s", host, filename)
	return nil
}

// CollectDiskUsage runs df -h on a remote host and writes the output to
// diskusage.txt in outDir. Errors are advisory per K013.
func CollectDiskUsage(c Collector, host string, outDir string) error {
	simplelog.Infof("disk-usage: starting collection on %s", host)

	if err := os.MkdirAll(outDir, DirPerms); err != nil {
		return fmt.Errorf("disk-usage: failed to create output dir %s: %w", outDir, err)
	}

	out, err := c.HostExecute(false, host, "df", "-h")
	if err != nil {
		simplelog.Warningf("disk-usage: df -h failed on %s: %v", host, err)
		return fmt.Errorf("disk-usage: df -h failed on %s: %w", host, err)
	}

	filename := filepath.Join(outDir, "diskusage.txt")
	if err := os.WriteFile(filename, []byte(out), 0o600); err != nil {
		return fmt.Errorf("disk-usage: failed to write %s: %w", filename, err)
	}

	simplelog.Infof("disk-usage: completed on %s, wrote %s", host, filename)
	return nil
}

// CollectRocksDBDiskUsage runs du -sh on the RocksDB data directory on a
// remote coordinator host. This is coordinator-only — executors don't have
// a RocksDB store. The glob requires sh -c wrapping for HostExecute.
// If rocksDBDir is empty, the default /opt/dremio/data/db is used.
func CollectRocksDBDiskUsage(c Collector, host string, outDir string, rocksDBDir string) error {
	if rocksDBDir == "" {
		rocksDBDir = "/opt/dremio/data/db"
	}
	simplelog.Infof("rocksdb-disk: starting collection on %s (path: %s)", host, rocksDBDir)

	// Check if the directory exists before running du — the default path may
	// not match the actual deployment layout and that is not an error.
	probe, _ := c.HostExecute(false, host, "test", "-d", rocksDBDir, "&&", "echo", "exists")
	if !strings.Contains(probe, "exists") {
		simplelog.Infof("rocksdb-disk: directory %s does not exist on %s, skipping", rocksDBDir, host)
		return nil
	}

	if err := os.MkdirAll(outDir, DirPerms); err != nil {
		return fmt.Errorf("rocksdb-disk: failed to create output dir %s: %w", outDir, err)
	}

	out, err := c.HostExecute(false, host, fmt.Sprintf("du -sh %s/*", rocksDBDir))
	if err != nil {
		simplelog.Warningf("rocksdb-disk: du failed on %s: %v", host, err)
		return fmt.Errorf("rocksdb-disk: du failed on %s: %w", host, err)
	}

	filename := filepath.Join(outDir, "rocksdb_disk_allocation.txt")
	if err := os.WriteFile(filename, []byte(out), 0o600); err != nil {
		return fmt.Errorf("rocksdb-disk: failed to write %s: %w", filename, err)
	}

	simplelog.Infof("rocksdb-disk: completed on %s, wrote %s", host, filename)
	return nil
}
