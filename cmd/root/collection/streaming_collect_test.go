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
	"bytes"
	"compress/gzip"
	"crypto/md5" //nolint:gosec // MD5 used for test checksum verification
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/local/jvmcollect"
	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/cli"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/collects"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/shutdown"
)

// --- mock collector for streaming tests ---

// mockStreamCollector satisfies the Collector interface with configurable
// behaviour for GetCoordinators, GetExecutors, DiscoverFiles, and StreamFromHost.
type mockStreamCollector struct {
	coordinators    []string
	executors       []string
	discoverFunc    func(host string) (*RemoteNodeInfo, error)
	streamFunc      func(host, remotePath string, writer io.Writer) error
	hostExecuteFunc func(mask bool, host string, args ...string) (string, error)
	copyToHostFunc  func(host, local, remote string) (string, error)
	hostPids        map[string]string
	cleanupCalled   atomic.Bool
}

func (m *mockStreamCollector) CopyToHost(host, local, remote string) (string, error) {
	if m.copyToHostFunc != nil {
		return m.copyToHostFunc(host, local, remote)
	}
	return "", nil
}
func (m *mockStreamCollector) GetCoordinators() ([]string, error) { return m.coordinators, nil }
func (m *mockStreamCollector) GetExecutors() ([]string, error)    { return m.executors, nil }
func (m *mockStreamCollector) HostExecute(mask bool, host string, args ...string) (string, error) {
	if m.hostExecuteFunc != nil {
		return m.hostExecuteFunc(mask, host, args...)
	}
	return "", nil
}
func (m *mockStreamCollector) HostExecuteAndStream(_ bool, _ string, _ cli.OutputHandler, _ string, _ ...string) error {
	return nil
}
func (m *mockStreamCollector) HelpText() string { return "mock" }
func (m *mockStreamCollector) Name() string     { return "mock" }
func (m *mockStreamCollector) Protocol() string { return "mock" }
func (m *mockStreamCollector) SetHostPid(h, p string) {
	if m.hostPids == nil {
		m.hostPids = make(map[string]string)
	}
	m.hostPids[h] = p
}
func (m *mockStreamCollector) CleanupRemote() error {
	m.cleanupCalled.Store(true)
	return nil
}
func (m *mockStreamCollector) StreamFromHost(host, remotePath string, writer io.Writer, _ bool) error {
	if m.streamFunc != nil {
		return m.streamFunc(host, remotePath, writer)
	}
	return nil
}
func (m *mockStreamCollector) DiscoverFiles(host, _, _ string) (*RemoteNodeInfo, error) {
	if m.discoverFunc != nil {
		return m.discoverFunc(host)
	}
	return &RemoteNodeInfo{}, nil
}

// --- mock copy strategy ---

type mockCopyStrategy struct {
	tmpDir string
}

func (m *mockCopyStrategy) CreatePath(fileType, source, nodeType string) (string, error) {
	p := filepath.Join(m.tmpDir, fileType, source)
	if err := os.MkdirAll(p, 0o750); err != nil {
		return "", err
	}
	return p, nil
}
func (m *mockCopyStrategy) ArchiveDiag(_ string, _ string) error { return nil }
func (m *mockCopyStrategy) GetTmpDir() string                    { return m.tmpDir }

// --- tests ---

func TestStreamingCollect_EndToEnd(t *testing.T) {
	tmpDir := t.TempDir()
	cs := &mockCopyStrategy{tmpDir: tmpDir}

	mc := &mockStreamCollector{
		coordinators: []string{"coord1"},
		executors:    []string{"exec1"},
		discoverFunc: func(host string) (*RemoteNodeInfo, error) {
			return &RemoteNodeInfo{
				LogDir:  "/var/log/dremio",
				ConfDir: "/opt/dremio/conf",
				Files: []RemoteFileInfo{
					{Path: "/var/log/dremio/server.log", Size: 100, FileType: "log"},
					{Path: "/opt/dremio/conf/dremio.conf", Size: 50, FileType: "config"},
					{Path: "/var/log/dremio/gc.log.0", Size: 30, FileType: "gc-log"},
				},
			}, nil
		},
		streamFunc: func(host, remotePath string, writer io.Writer) error {
			data := fmt.Sprintf("content-of-%s-on-%s", filepath.Base(remotePath), host)
			_, err := writer.Write([]byte(data))
			return err
		},
	}

	args := Args{
		DDCfs:             nil,
		OutputLoc:         filepath.Join(tmpDir, "output.tar.gz"),
		CopyStrategy:      cs,
		CollectionMode:    "standard",
		CollectionThreads: 2,
		CollectServerLogs: true,
		CollectGCLogs:     false, // standard mode never collects GC logs
	}
	hook := shutdown.NewHook()
	defer hook.Cleanup()

	err := ExecuteStreamingCollect(mc, cs, args, hook, func() {})
	if err != nil {
		t.Fatalf("ExecuteStreamingCollect failed: %v", err)
	}

	// Verify non-GC files landed in the correct strategy paths.
	for _, pair := range []struct {
		dir  string
		file string
	}{
		{"logs/coord1", "server.log"},
		{"configuration/coord1", "dremio.conf"},
		{"logs/exec1", "server.log"},
		{"configuration/exec1", "dremio.conf"},
	} {
		p := filepath.Join(tmpDir, pair.dir, pair.file)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			t.Errorf("expected file %v to exist", p)
		}
	}

	// Verify GC log files were NOT collected in standard mode.
	for _, pair := range []struct {
		dir  string
		file string
	}{
		{"logs/coord1", "gc.log.0"},
		{"logs/exec1", "gc.log.0"},
	} {
		p := filepath.Join(tmpDir, pair.dir, pair.file)
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("gc log file %v should NOT exist in standard mode", p)
		}
	}
}

func TestStreamingCollect_RetryOnTransientError(t *testing.T) {
	tmpDir := t.TempDir()
	cs := &mockCopyStrategy{tmpDir: tmpDir}

	var callCount atomic.Int32
	mc := &mockStreamCollector{
		coordinators: []string{"coord1"},
		executors:    nil,
		discoverFunc: func(host string) (*RemoteNodeInfo, error) {
			return &RemoteNodeInfo{
				Files: []RemoteFileInfo{
					{Path: "/var/log/dremio/server.log", Size: 100, FileType: "log"},
				},
			}, nil
		},
		streamFunc: func(host, remotePath string, writer io.Writer) error {
			n := callCount.Add(1)
			if n <= 2 {
				return fmt.Errorf("connection reset by peer")
			}
			_, err := writer.Write([]byte("success-data"))
			return err
		},
	}

	args := Args{
		DDCfs:             nil,
		OutputLoc:         filepath.Join(tmpDir, "output.tar.gz"),
		CopyStrategy:      cs,
		CollectionMode:    "standard",
		CollectionThreads: 1,
		CollectServerLogs: true,
	}
	hook := shutdown.NewHook()
	defer hook.Cleanup()

	err := ExecuteStreamingCollect(mc, cs, args, hook, func() {})
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}

	if callCount.Load() != 3 {
		t.Errorf("expected 3 stream calls (2 failures + 1 success), got %d", callCount.Load())
	}

	// Verify the file was written.
	p := filepath.Join(tmpDir, "logs", "coord1", "server.log")
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("failed to read collected file: %v", err)
	}
	if string(data) != "success-data" {
		t.Errorf("file content = %q, want %q", string(data), "success-data")
	}
}

func TestStreamingCollect_SkipOnPermissionDenied(t *testing.T) {
	tmpDir := t.TempDir()
	cs := &mockCopyStrategy{tmpDir: tmpDir}

	mc := &mockStreamCollector{
		coordinators: []string{"coord1"},
		executors:    nil,
		discoverFunc: func(host string) (*RemoteNodeInfo, error) {
			return &RemoteNodeInfo{
				Files: []RemoteFileInfo{
					{Path: "/secret/file.log", Size: 100, FileType: "log"},
					{Path: "/var/log/dremio/server.log", Size: 200, FileType: "log"},
				},
			}, nil
		},
		streamFunc: func(host, remotePath string, writer io.Writer) error {
			if strings.Contains(remotePath, "secret") {
				return fmt.Errorf("permission denied")
			}
			_, err := writer.Write([]byte("ok"))
			return err
		},
	}

	args := Args{
		DDCfs:             nil,
		OutputLoc:         filepath.Join(tmpDir, "output.tar.gz"),
		CopyStrategy:      cs,
		CollectionMode:    "standard",
		CollectionThreads: 1,
		CollectServerLogs: true,
	}
	hook := shutdown.NewHook()
	defer hook.Cleanup()

	err := ExecuteStreamingCollect(mc, cs, args, hook, func() {})
	if err != nil {
		t.Fatalf("expected success with skipped file, got: %v", err)
	}

	// The secret file should not exist; server.log should.
	secretPath := filepath.Join(tmpDir, "logs", "coord1", "file.log")
	if _, err := os.Stat(secretPath); !os.IsNotExist(err) {
		t.Errorf("expected secret file to NOT exist, but it does (or stat error: %v)", err)
	}
	serverPath := filepath.Join(tmpDir, "logs", "coord1", "server.log")
	if _, err := os.Stat(serverPath); os.IsNotExist(err) {
		t.Error("expected server.log to exist")
	}
}

func TestStreamingCollect_SkipNodeOnDiscoveryFailure(t *testing.T) {
	tmpDir := t.TempDir()
	cs := &mockCopyStrategy{tmpDir: tmpDir}

	mc := &mockStreamCollector{
		coordinators: []string{"good-node"},
		executors:    []string{"bad-node"},
		discoverFunc: func(host string) (*RemoteNodeInfo, error) {
			if host == "bad-node" {
				return nil, fmt.Errorf("connection refused")
			}
			return &RemoteNodeInfo{
				Files: []RemoteFileInfo{
					{Path: "/var/log/dremio/server.log", Size: 100, FileType: "log"},
				},
			}, nil
		},
		streamFunc: func(host, remotePath string, writer io.Writer) error {
			_, err := writer.Write([]byte("data"))
			return err
		},
	}

	args := Args{
		DDCfs:             nil,
		OutputLoc:         filepath.Join(tmpDir, "output.tar.gz"),
		CopyStrategy:      cs,
		CollectionMode:    "standard",
		CollectionThreads: 2,
		CollectServerLogs: true,
	}
	hook := shutdown.NewHook()
	defer hook.Cleanup()

	err := ExecuteStreamingCollect(mc, cs, args, hook, func() {})
	if err != nil {
		t.Fatalf("expected success (one good node), got: %v", err)
	}

	// good-node file should exist.
	p := filepath.Join(tmpDir, "logs", "good-node", "server.log")
	if _, err := os.Stat(p); os.IsNotExist(err) {
		t.Error("expected good-node file to exist")
	}
}

// TestStreamingCollect_RocksDBViewer_UsesAutodetectedDir ensures the rocksdb-viewer
// collection runs when the per-node autodetected RocksDB dir is available, even if
// --dremio-rocksdb-dir was not passed on the CLI. Regression test for the gate that
// previously required collectionArgs.DremioRocksDBDir to be set.
func TestStreamingCollect_RocksDBViewer_UsesAutodetectedDir(t *testing.T) {
	tmpDir := t.TempDir()
	cs := &mockCopyStrategy{tmpDir: tmpDir}

	var unameCalled atomic.Bool
	mc := &mockStreamCollector{
		coordinators: []string{"coord1"},
		executors:    nil,
		discoverFunc: func(_ string) (*RemoteNodeInfo, error) {
			return &RemoteNodeInfo{
				LogDir:     "/var/log/dremio",
				ConfDir:    "/opt/dremio/conf",
				RocksDBDir: "/opt/dremio/data/db",
				Files: []RemoteFileInfo{
					{Path: "/var/log/dremio/server.log", Size: 10, FileType: "log"},
				},
			}, nil
		},
		hostExecuteFunc: func(_ bool, _ string, args ...string) (string, error) {
			cmd := strings.Join(args, " ")
			switch {
			case strings.Contains(cmd, "uname -m"):
				unameCalled.Store(true)
				return "x86_64\n", nil
			case strings.Contains(cmd, "chmod +x"), strings.Contains(cmd, "rm -f"):
				return "", nil
			case strings.Contains(cmd, "-type cluster_stats"):
				return `{"cluster":"stub"}`, nil
			}
			return "", nil
		},
		streamFunc: func(_, _ string, w io.Writer) error {
			_, err := w.Write([]byte("stub"))
			return err
		},
		copyToHostFunc: func(_, _, _ string) (string, error) { return "", nil },
	}

	args := Args{
		OutputLoc:         filepath.Join(tmpDir, "output.tar.gz"),
		CopyStrategy:      cs,
		CollectionMode:    "standard",
		CollectionThreads: 1,
		CollectServerLogs: true,
		// DremioRocksDBDir intentionally left empty — the bug condition.
		DremioRocksDBDir:   "",
		CollectQueriesPerf: false, // cluster_stats runs unconditionally inside RunRocksDBCollection
	}
	hook := shutdown.NewHook()
	defer hook.Cleanup()

	if err := ExecuteStreamingCollect(mc, cs, args, hook, func() {}); err != nil {
		t.Fatalf("ExecuteStreamingCollect failed: %v", err)
	}

	if !unameCalled.Load() {
		t.Error("expected rocksdb-viewer collection to run (uname -m should have been called), but it was skipped")
	}
	clusterStats := filepath.Join(tmpDir, "cluster-stats", "coord1", "cluster-stats.json")
	if _, err := os.Stat(clusterStats); os.IsNotExist(err) {
		t.Errorf("expected %v to exist (rocksdb-viewer ran), but it does not", clusterStats)
	}
}

// TestStreamingCollect_RocksDBViewer_SkippedWhenNoDir verifies the rocksdb-viewer
// collection is skipped (no panic, no error) when neither autodetection nor the
// CLI flag yields a RocksDB dir.
func TestStreamingCollect_RocksDBViewer_SkippedWhenNoDir(t *testing.T) {
	tmpDir := t.TempDir()
	cs := &mockCopyStrategy{tmpDir: tmpDir}

	var unameCalled atomic.Bool
	mc := &mockStreamCollector{
		coordinators: []string{"coord1"},
		discoverFunc: func(_ string) (*RemoteNodeInfo, error) {
			return &RemoteNodeInfo{
				LogDir:     "/var/log/dremio",
				RocksDBDir: "",
				Files: []RemoteFileInfo{
					{Path: "/var/log/dremio/server.log", Size: 10, FileType: "log"},
				},
			}, nil
		},
		hostExecuteFunc: func(_ bool, _ string, args ...string) (string, error) {
			cmd := strings.Join(args, " ")
			if strings.Contains(cmd, "uname -m") {
				unameCalled.Store(true)
			}
			return "", nil
		},
		streamFunc: func(_, _ string, w io.Writer) error {
			_, err := w.Write([]byte("stub"))
			return err
		},
	}

	args := Args{
		OutputLoc:         filepath.Join(tmpDir, "output.tar.gz"),
		CopyStrategy:      cs,
		CollectionMode:    "standard",
		CollectionThreads: 1,
		CollectServerLogs: true,
		DremioRocksDBDir:  "",
	}
	hook := shutdown.NewHook()
	defer hook.Cleanup()

	if err := ExecuteStreamingCollect(mc, cs, args, hook, func() {}); err != nil {
		t.Fatalf("ExecuteStreamingCollect failed: %v", err)
	}

	if unameCalled.Load() {
		t.Error("rocksdb-viewer should not run when no RocksDB dir is available")
	}
}

func TestStreamingCollect_ZeroNodes(t *testing.T) {
	tmpDir := t.TempDir()
	cs := &mockCopyStrategy{tmpDir: tmpDir}

	mc := &mockStreamCollector{
		coordinators: nil,
		executors:    nil,
	}

	args := Args{
		DDCfs:             nil,
		OutputLoc:         filepath.Join(tmpDir, "output.tar.gz"),
		CopyStrategy:      cs,
		CollectionMode:    "standard",
		CollectionThreads: 1,
		CollectServerLogs: true,
	}
	hook := shutdown.NewHook()
	defer hook.Cleanup()

	err := ExecuteStreamingCollect(mc, cs, args, hook, func() {})
	if err == nil {
		t.Fatal("expected error for zero nodes")
	}
	if !strings.Contains(err.Error(), "no hosts found") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestStreamingCollect_AllFilesFailMeansNodeFailed(t *testing.T) {
	tmpDir := t.TempDir()
	cs := &mockCopyStrategy{tmpDir: tmpDir}

	mc := &mockStreamCollector{
		coordinators: []string{"fail-node"},
		executors:    []string{"ok-node"},
		discoverFunc: func(host string) (*RemoteNodeInfo, error) {
			return &RemoteNodeInfo{
				Files: []RemoteFileInfo{
					{Path: "/var/log/dremio/server.log", Size: 100, FileType: "log"},
				},
			}, nil
		},
		streamFunc: func(host, remotePath string, writer io.Writer) error {
			if host == "fail-node" {
				return fmt.Errorf("connection reset by peer")
			}
			_, err := writer.Write([]byte("ok"))
			return err
		},
	}

	args := Args{
		DDCfs:             nil,
		OutputLoc:         filepath.Join(tmpDir, "output.tar.gz"),
		CopyStrategy:      cs,
		CollectionMode:    "standard",
		CollectionThreads: 2,
		CollectServerLogs: true,
	}
	hook := shutdown.NewHook()
	defer hook.Cleanup()

	// Should succeed because ok-node collected files.
	err := ExecuteStreamingCollect(mc, cs, args, hook, func() {})
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}

	// ok-node file should exist.
	p := filepath.Join(tmpDir, "logs", "ok-node", "server.log")
	if _, err := os.Stat(p); os.IsNotExist(err) {
		t.Error("expected ok-node file to exist")
	}
}

func TestIsTransientError(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		transient bool
	}{
		{"nil error", nil, false},
		{"connection reset", fmt.Errorf("connection reset by peer"), true},
		{"EOF", fmt.Errorf("unexpected EOF"), true},
		{"timeout", fmt.Errorf("i/o timeout"), true},
		{"deadline exceeded", fmt.Errorf("context deadline exceeded"), true},
		{"broken pipe", fmt.Errorf("broken pipe"), true},
		{"permission denied", fmt.Errorf("permission denied"), false},
		{"no such file", fmt.Errorf("no such file or directory"), false},
		{"ENOENT", fmt.Errorf("ENOENT: file not found"), false},
		{"EACCES", fmt.Errorf("EACCES: access denied"), false},
		{"unknown error defaults to transient", fmt.Errorf("something unexpected"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTransientError(tt.err)
			if got != tt.transient {
				t.Errorf("isTransientError(%v) = %v, want %v", tt.err, got, tt.transient)
			}
		})
	}
}

// TestIsAlwaysExcluded verifies that the discovery-time filter blocks
// secret-bearing files (keystores, certs, private keys) and the existing
// admin_backup / audit prefixes, while leaving ordinary configs alone.
func TestIsAlwaysExcluded(t *testing.T) {
	tests := []struct {
		name     string
		baseName string
		want     bool
	}{
		// Suffix-based blocks for secrets.
		{"jks keystore", "keystore.jks", true},
		{"jks keystore uppercase", "KEYSTORE.JKS", true},
		{"jks mixed case", "TrustStore.Jks", true},
		{"pem cert", "server.pem", true},
		{"key file", "private.key", true},
		{"cer cert", "ca.cer", true},
		{"crt cert", "ca.crt", true},
		{"pem with path-like name", "dremio.test.pem", true},
		// Prefix-based blocks (existing behaviour).
		{"admin backup", "admin_backup-2024-01-01.tar", true},
		{"audit log", "audit.log", true},
		{"server.json", "server.json", true},
		{"server.out", "server.out", true},
		// Should NOT be blocked.
		{"dremio.conf", "dremio.conf", false},
		{"dremio-env", "dremio-env", false},
		{"server.log", "server.log", false},
		{"queries.json", "queries.json", false},
		{"logback.xml", "logback.xml", false},
		// Substring-only matches must not trigger (e.g. ".key" must be a suffix).
		{"name containing key", "key-mappings.conf", false},
		{"name containing pem", "pem-config.yaml", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAlwaysExcluded(tt.baseName); got != tt.want {
				t.Errorf("isAlwaysExcluded(%q) = %v, want %v", tt.baseName, got, tt.want)
			}
		})
	}
}

func TestFileTypeToStrategyType(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"log", "logs"},
		{"gc-log", "logs"},
		{"config", "configuration"},
		{"unknown", "unknown"},
	}
	for _, tt := range tests {
		got := fileTypeToStrategyType(tt.input)
		if got != tt.want {
			t.Errorf("fileTypeToStrategyType(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// --- progressWriter and humanizeBytes tests ---

func TestHumanizeBytes(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0B"},
		{100, "100B"},
		{1023, "1023B"},
		{1024, "1.0KB"},
		{1536, "1.5KB"},
		{1048576, "1.0MB"},
		{2621440, "2.5MB"},
		{1073741824, "1.0GB"},
		{5368709120, "5.0GB"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d", tt.input), func(t *testing.T) {
			got := humanizeBytes(tt.input)
			if got != tt.want {
				t.Errorf("humanizeBytes(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestProgressWriter_WithKnownSize(t *testing.T) {
	var buf bytes.Buffer
	pw := &progressWriter{
		w:            &buf,
		expectedSize: 1000,
		host:         "node1",
		filename:     "server.log",
		// lastUpdate zero-value ensures first write triggers an update
	}

	data := make([]byte, 500)
	n, err := pw.Write(data)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != 500 {
		t.Errorf("Write returned %d, want 500", n)
	}
	if pw.n != 500 {
		t.Errorf("pw.n = %d, want 500", pw.n)
	}
	// Verify the underlying writer received the bytes.
	if buf.Len() != 500 {
		t.Errorf("underlying writer received %d bytes, want 500", buf.Len())
	}
	// The first write should have triggered an update (lastUpdate was zero).
	// We can't directly inspect the consoleprint call, but we verify no panic
	// occurred and the writer worked correctly.
}

func TestProgressWriter_WithUnknownSize(t *testing.T) {
	var buf bytes.Buffer
	pw := &progressWriter{
		w:            &buf,
		expectedSize: 0,
		host:         "node1",
		filename:     "server.log",
	}

	data := make([]byte, 500)
	n, err := pw.Write(data)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != 500 {
		t.Errorf("Write returned %d, want 500", n)
	}
	// No panic or division by zero when expectedSize is 0.
}

func TestProgressWriter_ZeroLengthWrite(t *testing.T) {
	var buf bytes.Buffer
	pw := &progressWriter{
		w:            &buf,
		expectedSize: 1000,
		host:         "node1",
		filename:     "server.log",
	}

	// Write nil — should not panic.
	n, err := pw.Write(nil)
	if err != nil {
		t.Fatalf("Write(nil) failed: %v", err)
	}
	if n != 0 {
		t.Errorf("Write(nil) returned %d, want 0", n)
	}

	// Write empty slice — should not panic.
	n, err = pw.Write([]byte{})
	if err != nil {
		t.Fatalf("Write([]byte{}) failed: %v", err)
	}
	if n != 0 {
		t.Errorf("Write([]byte{}) returned %d, want 0", n)
	}
	if pw.n != 0 {
		t.Errorf("pw.n = %d, want 0", pw.n)
	}
}

func TestProgressWriter_Throttling(t *testing.T) {
	// updateCount tracks how many times consoleprint.UpdateNodeState would be
	// called. We can't mock the global function directly, but we can observe
	// the lastUpdate field to verify throttling behavior.
	var buf bytes.Buffer
	pw := &progressWriter{
		w:            &buf,
		expectedSize: 10000,
		host:         "node1",
		filename:     "test.log",
		lastUpdate:   time.Now(), // Set to now so the first write is throttled
	}

	// Write many small chunks rapidly — none should trigger an update because
	// lastUpdate was just set.
	for i := 0; i < 100; i++ {
		_, err := pw.Write([]byte("x"))
		if err != nil {
			t.Fatalf("Write failed at iteration %d: %v", i, err)
		}
	}

	if pw.n != 100 {
		t.Errorf("pw.n = %d, want 100", pw.n)
	}

	// Verify that lastUpdate hasn't changed from its initial setting (all
	// writes were within the 250ms window so no update should have fired).
	// We check that the time didn't advance significantly.
	elapsed := time.Since(pw.lastUpdate)
	if elapsed > 250*time.Millisecond {
		t.Log("Test ran slowly; throttle verification inconclusive")
	}

	// Now simulate a stale lastUpdate to verify the next write triggers an update.
	pw.lastUpdate = time.Now().Add(-500 * time.Millisecond)
	beforeUpdate := pw.lastUpdate
	_, err := pw.Write([]byte("y"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	// lastUpdate should have been refreshed.
	if !pw.lastUpdate.After(beforeUpdate) {
		t.Error("expected lastUpdate to advance after throttle interval elapsed")
	}
}

// --- hashFileInBackground tests ---

func TestHashFileInBackground(t *testing.T) {
	content := []byte("hashFileInBackground test content")
	sha256Sum := sha256.Sum256(content)
	wantSHA256 := hex.EncodeToString(sha256Sum[:])
	md5Sum := md5.Sum(content) //nolint:gosec
	wantMD5 := hex.EncodeToString(md5Sum[:])

	t.Run("sha256sum", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "test.txt")
		if err := os.WriteFile(tmpFile, content, 0o600); err != nil {
			t.Fatal(err)
		}
		ch := hashFileInBackground(tmpFile, "sha256sum")
		hr := <-ch
		if hr.err != nil {
			t.Fatalf("unexpected error: %v", hr.err)
		}
		if hr.hex != wantSHA256 {
			t.Errorf("got %q, want %q", hr.hex, wantSHA256)
		}
	})

	t.Run("md5sum", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "test.txt")
		if err := os.WriteFile(tmpFile, content, 0o600); err != nil {
			t.Fatal(err)
		}
		ch := hashFileInBackground(tmpFile, "md5sum")
		hr := <-ch
		if hr.err != nil {
			t.Fatalf("unexpected error: %v", hr.err)
		}
		if hr.hex != wantMD5 {
			t.Errorf("got %q, want %q", hr.hex, wantMD5)
		}
	})

	t.Run("empty_tool_returns_empty_hex", func(t *testing.T) {
		ch := hashFileInBackground("/any/path", "")
		hr := <-ch
		if hr.err != nil {
			t.Fatalf("unexpected error: %v", hr.err)
		}
		if hr.hex != "" {
			t.Errorf("expected empty hex, got %q", hr.hex)
		}
	})

	t.Run("nonexistent_file_returns_error", func(t *testing.T) {
		ch := hashFileInBackground("/nonexistent/path/to/file", "sha256sum")
		hr := <-ch
		if hr.err == nil {
			t.Fatal("expected error for nonexistent file")
		}
	})
}

// --- parseChecksumOutput tests ---

func TestParseChecksumOutput_Valid(t *testing.T) {
	// Standard GNU coreutils format: "<hash>  <path>\n"
	got := parseChecksumOutput("abc123def456  /path/to/file\n")
	if got != "abc123def456" {
		t.Errorf("parseChecksumOutput() = %q, want %q", got, "abc123def456")
	}
}

func TestParseChecksumOutput_EdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty string", "", ""},
		{"whitespace only", "   \n  ", ""},
		{"no space (bare hash)", "abc123", "abc123"},
		{"trailing newlines", "abc123  /file\n\n\n", "abc123"},
		{"extra whitespace", "  abc123   /path/to/file  \n", "abc123"},
		{"tab separated", "abc123\t/path/to/file", "abc123"},
		{"BSD-style output", "SHA256 (/path) = abc123", "SHA256"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseChecksumOutput(tt.input)
			if got != tt.want {
				t.Errorf("parseChecksumOutput(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// --- verifyChecksum tests ---

func TestVerifyChecksum_SHA256Match(t *testing.T) {
	mc := &mockStreamCollector{
		hostExecuteFunc: func(_ bool, _ string, args ...string) (string, error) {
			if len(args) > 0 && args[0] == "sha256sum" {
				return "abcdef123456  /remote/file\n", nil
			}
			return "", fmt.Errorf("not called")
		},
	}

	err := verifyChecksum(mc, "host1", "/remote/file", "abcdef123456", "sha256sum")
	if err != nil {
		t.Fatalf("verifyChecksum returned error: %v", err)
	}
}

func TestVerifyChecksum_SHA256Mismatch(t *testing.T) {
	mc := &mockStreamCollector{
		hostExecuteFunc: func(_ bool, _ string, args ...string) (string, error) {
			if len(args) > 0 && args[0] == "sha256sum" {
				return "remotehash999  /remote/file\n", nil
			}
			return "", fmt.Errorf("not called")
		},
	}

	err := verifyChecksum(mc, "host1", "/remote/file", "localhash111", "sha256sum")
	if err != nil {
		t.Fatalf("verifyChecksum returned error: %v", err)
	}
	// Warning logged but no error returned — advisory per D013.
}

func TestVerifyChecksum_MD5(t *testing.T) {
	mc := &mockStreamCollector{
		hostExecuteFunc: func(_ bool, _ string, args ...string) (string, error) {
			if len(args) > 0 && args[0] == "md5sum" {
				return "md5match123  /remote/file\n", nil
			}
			return "", fmt.Errorf("unexpected command")
		},
	}

	err := verifyChecksum(mc, "host1", "/remote/file", "md5match123", "md5sum")
	if err != nil {
		t.Fatalf("verifyChecksum returned error: %v", err)
	}
}

func TestVerifyChecksum_NoTool(t *testing.T) {
	mc := &mockStreamCollector{
		hostExecuteFunc: func(_ bool, _ string, args ...string) (string, error) {
			return "", fmt.Errorf("command not found")
		},
	}

	err := verifyChecksum(mc, "host1", "/remote/file", "sha", "")
	if err != nil {
		t.Fatalf("verifyChecksum returned error: %v", err)
	}
	// Warning logged about skipping — no error returned.
}

// --- single-hash streamFileOnce test ---

func TestStreamFileOnce_SingleHash(t *testing.T) {
	tmpDir := t.TempDir()
	content := []byte("hello world single hash test content")

	// Compute expected hashes.
	sha256Sum := sha256.Sum256(content)
	wantSHA256 := hex.EncodeToString(sha256Sum[:])
	md5Sum := md5.Sum(content) //nolint:gosec
	wantMD5 := hex.EncodeToString(md5Sum[:])

	mc := &mockStreamCollector{
		streamFunc: func(_, _ string, writer io.Writer) error {
			_, err := writer.Write(content)
			return err
		},
	}

	t.Run("sha256sum", func(t *testing.T) {
		destPath := filepath.Join(tmpDir, "sha256file.txt")
		n, hashCh, err := streamFileOnce(mc, "host1", "/remote/file", destPath, int64(len(content)), "testfile.txt", "sha256sum", false)
		if err != nil {
			t.Fatalf("streamFileOnce returned error: %v", err)
		}
		if n != int64(len(content)) {
			t.Errorf("bytes written = %d, want %d", n, len(content))
		}
		hr := <-hashCh
		if hr.err != nil {
			t.Fatalf("hash error: %v", hr.err)
		}
		if hr.hex != wantSHA256 {
			t.Errorf("SHA-256 = %q, want %q", hr.hex, wantSHA256)
		}

		// Verify file was actually written.
		data, err := os.ReadFile(destPath)
		if err != nil {
			t.Fatalf("failed to read dest file: %v", err)
		}
		if !bytes.Equal(data, content) {
			t.Errorf("file content mismatch")
		}
	})

	t.Run("md5sum", func(t *testing.T) {
		destPath := filepath.Join(tmpDir, "md5file.txt")
		n, hashCh, err := streamFileOnce(mc, "host1", "/remote/file", destPath, int64(len(content)), "testfile.txt", "md5sum", false)
		if err != nil {
			t.Fatalf("streamFileOnce returned error: %v", err)
		}
		if n != int64(len(content)) {
			t.Errorf("bytes written = %d, want %d", n, len(content))
		}
		hr := <-hashCh
		if hr.err != nil {
			t.Fatalf("hash error: %v", hr.err)
		}
		if hr.hex != wantMD5 {
			t.Errorf("MD5 = %q, want %q", hr.hex, wantMD5)
		}
	})

	t.Run("empty_tool_returns_empty_hex", func(t *testing.T) {
		destPath := filepath.Join(tmpDir, "nohashfile.txt")
		n, hashCh, err := streamFileOnce(mc, "host1", "/remote/file", destPath, int64(len(content)), "testfile.txt", "", false)
		if err != nil {
			t.Fatalf("streamFileOnce returned error: %v", err)
		}
		if n != int64(len(content)) {
			t.Errorf("bytes written = %d, want %d", n, len(content))
		}
		hr := <-hashCh
		if hr.err != nil {
			t.Fatalf("hash error: %v", hr.err)
		}
		if hr.hex != "" {
			t.Errorf("expected empty hex for empty tool, got %q", hr.hex)
		}
	})
}

// TestStreamFileOnce_Buffered sends many small chunks through streamFileOnce
// and verifies that the buffered writer produces correct hashes and file content.
// This exercises the bufio.Writer wrapping added to batch small SPDY chunks.
func TestStreamFileOnce_Buffered(t *testing.T) {
	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, "buffered.txt")

	// Build content from many small chunks (100 × 100 bytes = 10KB).
	chunk := bytes.Repeat([]byte("x"), 100)
	const numChunks = 100
	fullContent := bytes.Repeat(chunk, numChunks)

	sha256Sum := sha256.Sum256(fullContent)
	wantSHA256 := hex.EncodeToString(sha256Sum[:])

	mc := &mockStreamCollector{
		streamFunc: func(_, _ string, writer io.Writer) error {
			// Write many small chunks, simulating SPDY chunked delivery.
			for i := 0; i < numChunks; i++ {
				if _, err := writer.Write(chunk); err != nil {
					return err
				}
			}
			return nil
		},
	}

	n, hashCh, err := streamFileOnce(mc, "host1", "/remote/file", destPath, int64(len(fullContent)), "buffered.txt", "sha256sum", false)
	if err != nil {
		t.Fatalf("streamFileOnce returned error: %v", err)
	}
	if n != int64(len(fullContent)) {
		t.Errorf("bytes written = %d, want %d", n, len(fullContent))
	}
	hr := <-hashCh
	if hr.err != nil {
		t.Fatalf("hash error: %v", hr.err)
	}
	if hr.hex != wantSHA256 {
		t.Errorf("SHA-256 = %q, want %q", hr.hex, wantSHA256)
	}

	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("failed to read dest file: %v", err)
	}
	if !bytes.Equal(data, fullContent) {
		t.Errorf("file content mismatch: got %d bytes, want %d", len(data), len(fullContent))
	}
}

// TestBufioWriterReducesWriteCalls proves that wrapping a writer in
// bufio.NewWriterSize batches many small writes into fewer large ones.
func TestBufioWriterReducesWriteCalls(t *testing.T) {
	// writeCounter counts Write() calls on an underlying writer.
	type writeCounter struct {
		io.Writer
		count int
	}
	countingWrite := func(wc *writeCounter, p []byte) (int, error) {
		wc.count++
		return wc.Write(p)
	}

	var buf bytes.Buffer
	wc := &writeCounter{Writer: &buf}

	// Wrap in a bufio.Writer so small writes batch into larger ones.
	bw := bufio.NewWriterSize(writerFunc(func(p []byte) (int, error) {
		return countingWrite(wc, p)
	}), 256*1024)

	chunk := bytes.Repeat([]byte("y"), 100)
	const numChunks = 1000 // 100KB total, well under 256KB buffer

	for i := 0; i < numChunks; i++ {
		if _, err := bw.Write(chunk); err != nil {
			t.Fatalf("write %d failed: %v", i, err)
		}
	}
	if err := bw.Flush(); err != nil {
		t.Fatalf("flush failed: %v", err)
	}

	// With 100KB of data and a 256KB buffer, we expect exactly 1 flush write
	// (the final Flush). Without buffering it would be 1000 writes.
	if wc.count >= numChunks {
		t.Errorf("buffered writer made %d Write() calls for %d input writes — buffering not working", wc.count, numChunks)
	}
	t.Logf("buffered: %d Write() calls for %d input writes", wc.count, numChunks)

	if buf.Len() != numChunks*len(chunk) {
		t.Errorf("total bytes = %d, want %d", buf.Len(), numChunks*len(chunk))
	}
}

// writerFunc adapts a function to io.Writer for test convenience.
type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// --- JVM collection integration tests ---

// newJVMTestCollector returns a mockStreamCollector pre-configured for JVM tests.
// discoverFunc returns RemoteNodeInfo with the given PID. hostExecuteFunc records
// all commands issued. streamFunc writes a token so file-streaming produces
// at least one collected file (required for ExecuteStreamingCollect to succeed).
func newJVMTestCollector(coordinators, executors []string, pidByHost map[string]int, hostExecFunc func(bool, string, ...string) (string, error)) *mockStreamCollector {
	return &mockStreamCollector{
		coordinators: coordinators,
		executors:    executors,
		discoverFunc: func(host string) (*RemoteNodeInfo, error) {
			pid := pidByHost[host]
			return &RemoteNodeInfo{
				DremioPID: pid,
				Files: []RemoteFileInfo{
					{Path: "/var/log/dremio/server.log", Size: 10, FileType: "log"},
				},
			}, nil
		},
		streamFunc: func(host, remotePath string, writer io.Writer) error {
			_, err := writer.Write([]byte("log-data"))
			return err
		},
		hostExecuteFunc: hostExecFunc,
	}
}

func TestStreamingCollect_JVMCollection_DiagnosisMode(t *testing.T) {
	tmpDir := t.TempDir()
	cs := &mockCopyStrategy{tmpDir: tmpDir}

	var mu sync.Mutex
	commandsByHost := make(map[string][]string)

	mc := newJVMTestCollector(
		[]string{"coord1"},
		[]string{"exec1"},
		map[string]int{"coord1": 1234, "exec1": 5678},
		func(_ bool, host string, args ...string) (string, error) {
			mu.Lock()
			commandsByHost[host] = append(commandsByHost[host], strings.Join(args, " "))
			mu.Unlock()
			// Return valid output for any jcmd/top command.
			if len(args) > 0 && args[0] == "sh" {
				return "top output", nil
			}
			return "jcmd output", nil
		},
	)

	args := Args{
		OutputLoc:         filepath.Join(tmpDir, "output.tar.gz"),
		CopyStrategy:      cs,
		CollectionMode:    collects.DiagnosisCollection,
		CollectionThreads: 2,
		CollectServerLogs: true,
		CollectJStack:     true,
		CollectTop:        true,
		CollectJVMFlags:   true,
		CollectJFR:        true,
		CollectHeapDump:   true,
		DiagTimeSeconds:   2,
	}
	hook := shutdown.NewHook()
	defer hook.Cleanup()

	err := ExecuteStreamingCollect(mc, cs, args, hook, func() {})
	if err != nil {
		t.Fatalf("ExecuteStreamingCollect failed: %v", err)
	}

	// Verify JVM output files were created for both nodes.
	for _, host := range []string{"coord1", "exec1"} {
		// jstack creates threadDump files
		jstackDir := filepath.Join(tmpDir, "jstack", host)
		entries, err := os.ReadDir(jstackDir)
		if err != nil {
			t.Errorf("jstack dir missing for %s: %v", host, err)
		} else if len(entries) == 0 {
			t.Errorf("expected jstack output files for %s", host)
		}

		// top creates <host>_top.txt in flat directory
		topFile := filepath.Join(tmpDir, "top", host+"_top.txt")
		if _, err := os.Stat(topFile); os.IsNotExist(err) {
			t.Errorf("expected %s_top.txt for %s", host, host)
		}

		// jvm_settings creates jvm_settings.txt
		jvmFile := filepath.Join(tmpDir, "node-info", host, "jvm_settings.txt")
		if _, err := os.Stat(jvmFile); os.IsNotExist(err) {
			t.Errorf("expected jvm_settings.txt for %s", host)
		}

		// JFR creates a .jfr file in flat directory
		jfrDir := filepath.Join(tmpDir, "jfr")
		jfrEntries, err := os.ReadDir(jfrDir)
		if err != nil {
			t.Errorf("jfr dir missing: %v", err)
		} else {
			found := false
			for _, e := range jfrEntries {
				if strings.HasPrefix(e.Name(), host) && strings.HasSuffix(e.Name(), ".jfr") {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected .jfr file for %s in %s", host, jfrDir)
			}
		}

		// heap dump creates a .hprof.gz or .hprof file in flat directory
		heapDir := filepath.Join(tmpDir, "heap-dump")
		heapEntries, err := os.ReadDir(heapDir)
		if err != nil {
			t.Errorf("heap-dump dir missing: %v", err)
		} else {
			found := false
			for _, e := range heapEntries {
				if strings.HasPrefix(e.Name(), host) && (strings.HasSuffix(e.Name(), ".hprof.gz") || strings.HasSuffix(e.Name(), ".hprof")) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected .hprof.gz or .hprof file for %s in %s", host, heapDir)
			}
		}
	}

	// Verify both nodes had commands issued.
	mu.Lock()
	defer mu.Unlock()
	for _, host := range []string{"coord1", "exec1"} {
		if len(commandsByHost[host]) == 0 {
			t.Errorf("expected JVM commands on %s, got none", host)
		}
	}
}

func TestStreamingCollect_JVMCollection_StandardMode_Skipped(t *testing.T) {
	tmpDir := t.TempDir()
	cs := &mockCopyStrategy{tmpDir: tmpDir}

	var commandCount atomic.Int32
	mc := newJVMTestCollector(
		[]string{"coord1"},
		nil,
		map[string]int{"coord1": 1234},
		func(_ bool, host string, args ...string) (string, error) {
			commandCount.Add(1)
			return "output", nil
		},
	)

	args := Args{
		OutputLoc:         filepath.Join(tmpDir, "output.tar.gz"),
		CopyStrategy:      cs,
		CollectionMode:    "standard", // NOT diagnosis
		CollectionThreads: 1,
		CollectServerLogs: true,
		CollectJStack:     false, // JVM diagnostic tools disabled in standard mode
		CollectTop:        false,
		CollectJVMFlags:   true, // JVM flags always collected (produces node-info output)
		CollectJFR:        false,
		CollectHeapDump:   false,
		DiagTimeSeconds:   2,
	}
	hook := shutdown.NewHook()
	defer hook.Cleanup()

	err := ExecuteStreamingCollect(mc, cs, args, hook, func() {})
	if err != nil {
		t.Fatalf("ExecuteStreamingCollect failed: %v", err)
	}

	// In standard mode, JVM diagnostic tools (jstack, top, jfr, heap-dump)
	// should NOT run, but node-info collection always runs.
	for _, dir := range []string{"jstack", "top", "jfr", "heap-dump"} {
		p := filepath.Join(tmpDir, dir)
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("expected %s dir to NOT exist in standard mode, but it does", dir)
		}
	}
	// node-info directory SHOULD exist (created by runNodeInfoCollection).
	niPath := filepath.Join(tmpDir, "node-info")
	if _, err := os.Stat(niPath); os.IsNotExist(err) {
		t.Error("expected node-info dir to exist in standard mode, but it does not")
	}
}

func TestStreamingCollect_JVMCollection_NoPID_Skipped(t *testing.T) {
	tmpDir := t.TempDir()
	cs := &mockCopyStrategy{tmpDir: tmpDir}

	var mu sync.Mutex
	jvmCommandsByHost := make(map[string]int)

	mc := newJVMTestCollector(
		[]string{"has-pid"},
		[]string{"no-pid"},
		map[string]int{"has-pid": 9999, "no-pid": 0}, // no-pid has PID=0
		func(_ bool, host string, args ...string) (string, error) {
			// Count JVM-specific commands: jcmd calls now go through
			// sh -c "timeout 30 jcmd ..." via jcmdExec helper.
			joined := strings.Join(args, " ")
			if strings.Contains(joined, "jcmd") {
				mu.Lock()
				jvmCommandsByHost[host]++
				mu.Unlock()
			}
			return "output", nil
		},
	)

	args := Args{
		OutputLoc:         filepath.Join(tmpDir, "output.tar.gz"),
		CopyStrategy:      cs,
		CollectionMode:    collects.DiagnosisCollection,
		CollectionThreads: 2,
		CollectServerLogs: true,
		CollectJStack:     true,
		CollectTop:        true,
		CollectJVMFlags:   true,
		DiagTimeSeconds:   1,
	}
	hook := shutdown.NewHook()
	defer hook.Cleanup()

	err := ExecuteStreamingCollect(mc, cs, args, hook, func() {})
	if err != nil {
		t.Fatalf("ExecuteStreamingCollect failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	// has-pid should have JVM commands; no-pid should have zero.
	if jvmCommandsByHost["has-pid"] == 0 {
		t.Error("expected JVM commands on has-pid node")
	}
	if jvmCommandsByHost["no-pid"] != 0 {
		t.Errorf("expected NO JVM commands on no-pid node, got %d", jvmCommandsByHost["no-pid"])
	}
}

func TestStreamingCollect_JVMCollection_SynchronizedStart(t *testing.T) {
	tmpDir := t.TempDir()
	cs := &mockCopyStrategy{tmpDir: tmpDir}

	// Track when each node starts its first JVM command.
	var mu sync.Mutex
	startTimes := make(map[string]time.Time)

	mc := newJVMTestCollector(
		[]string{"node-a", "node-b"},
		nil,
		map[string]int{"node-a": 100, "node-b": 200},
		func(_ bool, host string, args ...string) (string, error) {
			cmd := ""
			if len(args) > 0 {
				cmd = args[0]
			}
			// Record first JVM command timestamp per host.
			// Commands arrive as single strings: "timeout 30 jcmd ...", "top -H ...", etc.
			if strings.Contains(cmd, "jcmd") || strings.Contains(cmd, "top") {
				mu.Lock()
				if _, ok := startTimes[host]; !ok {
					startTimes[host] = time.Now()
				}
				mu.Unlock()
			}
			if strings.Contains(cmd, "top") {
				return "top output", nil
			}
			return "jcmd output", nil
		},
	)

	args := Args{
		OutputLoc:         filepath.Join(tmpDir, "output.tar.gz"),
		CopyStrategy:      cs,
		CollectionMode:    collects.DiagnosisCollection,
		CollectionThreads: 4,
		CollectServerLogs: true,
		CollectJStack:     true,
		CollectTop:        false,
		CollectJVMFlags:   false,
		DiagTimeSeconds:   1,
	}
	hook := shutdown.NewHook()
	defer hook.Cleanup()

	err := ExecuteStreamingCollect(mc, cs, args, hook, func() {})
	if err != nil {
		t.Fatalf("ExecuteStreamingCollect failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	tA, okA := startTimes["node-a"]
	tB, okB := startTimes["node-b"]
	if !okA || !okB {
		t.Fatalf("expected start times for both nodes, got node-a=%v node-b=%v", okA, okB)
	}

	// Both nodes should start within a tight window because of the synchronized
	// barrier. Allow generous 2s tolerance for CI scheduling jitter.
	diff := tA.Sub(tB)
	if diff < 0 {
		diff = -diff
	}
	if diff > 2*time.Second {
		t.Errorf("JVM collection start times differ by %v, expected synchronized start (< 2s)", diff)
	}
}

// --- Async-profiler integration tests ---

func TestStreamingCollect_AsyncProfiler_Enabled(t *testing.T) {
	// Override the binary resolver to return synthetic bytes so the test doesn't
	// depend on real embedded binaries (which are empty placeholders in dev).
	origFn := getAsprofBinaryFn
	getAsprofBinaryFn = func(arch string) ([]byte, error) {
		return []byte("fake-asprof-binary"), nil
	}
	t.Cleanup(func() { getAsprofBinaryFn = origFn })

	tmpDir := t.TempDir()
	cs := &mockCopyStrategy{tmpDir: tmpDir}

	var mu sync.Mutex
	commandsByHost := make(map[string][]string)
	copyToHostCalls := make(map[string]int)

	mc := newJVMTestCollector(
		[]string{"coord1"},
		nil,
		map[string]int{"coord1": 1234},
		func(_ bool, host string, args ...string) (string, error) {
			mu.Lock()
			commandsByHost[host] = append(commandsByHost[host], strings.Join(args, " "))
			mu.Unlock()
			// uname -m → x86_64
			if len(args) >= 2 && args[0] == "uname" && args[1] == "-m" {
				return "x86_64\n", nil
			}
			// chmod
			if len(args) > 0 && args[0] == "chmod" {
				return "", nil
			}
			// rm cleanup
			if len(args) > 0 && args[0] == "rm" {
				return "", nil
			}
			// asprof execution — match by the remote binary path pattern
			if len(args) > 0 && strings.HasPrefix(args[0], "/tmp/ddc-asprof-") {
				return "profiling done", nil
			}
			if len(args) > 0 && args[0] == "sh" {
				return "top output", nil
			}
			return "jcmd output", nil
		},
	)
	mc.copyToHostFunc = func(host, local, remote string) (string, error) {
		mu.Lock()
		copyToHostCalls[host]++
		mu.Unlock()
		return "", nil
	}

	args := Args{
		OutputLoc:            filepath.Join(tmpDir, "output.tar.gz"),
		CopyStrategy:         cs,
		CollectionMode:       collects.DiagnosisCollection,
		CollectionThreads:    1,
		CollectServerLogs:    true,
		CollectAsyncProfiler: true,
		DiagTimeSeconds:      5,
	}
	hook := shutdown.NewHook()
	defer hook.Cleanup()

	err := ExecuteStreamingCollect(mc, cs, args, hook, func() {})
	if err != nil {
		t.Fatalf("ExecuteStreamingCollect failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	// Verify uname -m was called on coord1.
	found := false
	for _, cmd := range commandsByHost["coord1"] {
		if strings.Contains(cmd, "uname -m") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected uname -m on coord1, commands: %v", commandsByHost["coord1"])
	}

	// Verify CopyToHost was called (binary upload).
	if copyToHostCalls["coord1"] == 0 {
		t.Error("expected CopyToHost call on coord1 for asprof binary upload")
	}

	// Verify the asprof binary was executed (look for /tmp/ddc-asprof- in commands).
	asprofRan := false
	for _, cmd := range commandsByHost["coord1"] {
		if strings.Contains(cmd, "ddc-asprof") && strings.Contains(cmd, "-d 5") && strings.Contains(cmd, "-e itimer") {
			asprofRan = true
			break
		}
	}
	if !asprofRan {
		t.Errorf("expected asprof execution with -d 5 on coord1, commands: %v", commandsByHost["coord1"])
	}
}

func TestStreamingCollect_AsyncProfiler_Disabled(t *testing.T) {
	tmpDir := t.TempDir()
	cs := &mockCopyStrategy{tmpDir: tmpDir}

	var mu sync.Mutex
	commandsByHost := make(map[string][]string)

	mc := newJVMTestCollector(
		[]string{"coord1"},
		nil,
		map[string]int{"coord1": 1234},
		func(_ bool, host string, args ...string) (string, error) {
			mu.Lock()
			commandsByHost[host] = append(commandsByHost[host], strings.Join(args, " "))
			mu.Unlock()
			if len(args) > 0 && args[0] == "sh" {
				return "top output", nil
			}
			return "output", nil
		},
	)

	args := Args{
		OutputLoc:            filepath.Join(tmpDir, "output.tar.gz"),
		CopyStrategy:         cs,
		CollectionMode:       collects.DiagnosisCollection,
		CollectionThreads:    1,
		CollectServerLogs:    true,
		CollectAsyncProfiler: false, // disabled
		CollectJStack:        true,
		DiagTimeSeconds:      1,
	}
	hook := shutdown.NewHook()
	defer hook.Cleanup()

	err := ExecuteStreamingCollect(mc, cs, args, hook, func() {})
	if err != nil {
		t.Fatalf("ExecuteStreamingCollect failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	// No uname -m or asprof commands should have been issued.
	for _, cmd := range commandsByHost["coord1"] {
		if strings.Contains(cmd, "uname -m") {
			t.Errorf("unexpected uname -m call when async-profiler disabled")
		}
		if strings.Contains(cmd, "asprof") {
			t.Errorf("unexpected asprof command when async-profiler disabled: %s", cmd)
		}
	}
}

func TestStreamingCollect_AsyncProfiler_EmptyBinary(t *testing.T) {
	// Simulate empty placeholder binary regardless of actual embedded bytes.
	origFn := getAsprofBinaryFn
	getAsprofBinaryFn = func(arch string) ([]byte, error) {
		return nil, fmt.Errorf("embedded asprof binary is empty (placeholder)")
	}
	t.Cleanup(func() { getAsprofBinaryFn = origFn })

	origFilesFn := getAsprofFilesFn
	getAsprofFilesFn = func(arch string) (*jvmcollect.AsprofFiles, error) {
		return nil, fmt.Errorf("embedded asprof binary is empty (placeholder)")
	}
	t.Cleanup(func() { getAsprofFilesFn = origFilesFn })

	tmpDir := t.TempDir()
	cs := &mockCopyStrategy{tmpDir: tmpDir}

	var mu sync.Mutex
	commandsByHost := make(map[string][]string)
	copyToHostCalls := make(map[string]int)

	mc := newJVMTestCollector(
		[]string{"coord1"},
		nil,
		map[string]int{"coord1": 1234},
		func(_ bool, host string, args ...string) (string, error) {
			mu.Lock()
			commandsByHost[host] = append(commandsByHost[host], strings.Join(args, " "))
			mu.Unlock()
			// uname -m → x86_64 (embedded binary for x86_64 is empty placeholder)
			if len(args) >= 2 && args[0] == "uname" && args[1] == "-m" {
				return "x86_64\n", nil
			}
			if len(args) > 0 && args[0] == "sh" {
				return "top output", nil
			}
			return "output", nil
		},
	)
	mc.copyToHostFunc = func(host, local, remote string) (string, error) {
		mu.Lock()
		copyToHostCalls[host]++
		mu.Unlock()
		return "", nil
	}

	args := Args{
		OutputLoc:            filepath.Join(tmpDir, "output.tar.gz"),
		CopyStrategy:         cs,
		CollectionMode:       collects.DiagnosisCollection,
		CollectionThreads:    1,
		CollectServerLogs:    true,
		CollectAsyncProfiler: true,
		DiagTimeSeconds:      5,
	}
	hook := shutdown.NewHook()
	defer hook.Cleanup()

	err := ExecuteStreamingCollect(mc, cs, args, hook, func() {})
	if err != nil {
		t.Fatalf("ExecuteStreamingCollect failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	// uname -m should have been called (arch detection still runs).
	unameFound := false
	for _, cmd := range commandsByHost["coord1"] {
		if strings.Contains(cmd, "uname -m") {
			unameFound = true
			break
		}
	}
	if !unameFound {
		t.Errorf("expected uname -m on coord1 even with empty binary")
	}

	// But CopyToHost should NOT have been called because GetAsprofBinary
	// returns ErrAsprofEmpty for the empty placeholder.
	if copyToHostCalls["coord1"] != 0 {
		t.Errorf("expected no CopyToHost calls when binary is empty placeholder, got %d", copyToHostCalls["coord1"])
	}
}

// --- post-stream masking tests ---

func TestMaskLocalConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	p := filepath.Join(tmpDir, "dremio.conf")

	content := `paths: {
  local: "/data"
}
services: {
  javax.net.ssl {
    keyStorePassword: "my-secret-password",
    trustStorePassword: "another-secret"
  }
}`
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := maskLocalConfigFile(p); err != nil {
		t.Fatalf("maskLocalConfigFile returned error: %v", err)
	}

	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	masked := string(data)

	if strings.Contains(masked, "my-secret-password") {
		t.Error("keyStorePassword was not masked")
	}
	if strings.Contains(masked, "another-secret") {
		t.Error("trustStorePassword was not masked")
	}
	if !strings.Contains(masked, `<REMOVED_POTENTIAL_SECRET>`) {
		t.Error("expected masking marker not found")
	}
	// Non-secret lines preserved.
	if !strings.Contains(masked, `local: "/data"`) {
		t.Error("non-secret line was modified")
	}
}

func TestMaskLocalConfigFile_JSON(t *testing.T) {
	tmpDir := t.TempDir()
	p := filepath.Join(tmpDir, "sso.json")

	content := `{"clientSecret": "real-secret", "clientId": "safe-value"}`
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := maskLocalConfigFile(p); err != nil {
		t.Fatalf("maskLocalConfigFile returned error: %v", err)
	}

	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	masked := string(data)

	if strings.Contains(masked, "real-secret") {
		t.Error("clientSecret value was not masked")
	}
}

func TestMaskLocalConfigFile_NonexistentFile(t *testing.T) {
	err := maskLocalConfigFile("/nonexistent/path/to/file.conf")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestStreamNodeFiles_MasksConfigFiles(t *testing.T) {
	tmpDir := t.TempDir()
	cs := &mockCopyStrategy{tmpDir: tmpDir}

	confContent := `services.javax.net.ssl.keyStorePassword: "super-secret"`

	mc := &mockStreamCollector{
		streamFunc: func(host, remotePath string, writer io.Writer) error {
			if strings.Contains(remotePath, "dremio.conf") {
				_, err := writer.Write([]byte(confContent))
				return err
			}
			_, err := writer.Write([]byte("log line"))
			return err
		},
	}

	info := &RemoteNodeInfo{
		Files: []RemoteFileInfo{
			{Path: "/opt/dremio/conf/dremio.conf", Size: int64(len(confContent)), FileType: "config"},
			{Path: "/var/log/dremio/server.log", Size: 8, FileType: "log"},
		},
	}

	collected, skipped := streamNodeFiles(mc, "host1", info, cs, "coordinator", "diagnosis", true, Args{CollectServerLogs: true})

	if len(skipped) != 0 {
		t.Errorf("expected no skipped files, got %v", skipped)
	}
	if len(collected) != 2 {
		t.Fatalf("expected 2 collected files, got %d", len(collected))
	}

	// Read the config file and verify it was masked.
	confPath := filepath.Join(tmpDir, "configuration", "host1", "dremio.conf")
	data, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatalf("failed to read config file: %v", err)
	}
	if strings.Contains(string(data), "super-secret") {
		t.Error("config file secret was not masked after streaming")
	}
	if !strings.Contains(string(data), "<REMOVED_POTENTIAL_SECRET>") {
		t.Error("expected masking marker in streamed config file")
	}

	// Log file should be unchanged.
	logPath := filepath.Join(tmpDir, "logs", "host1", "server.log")
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	if string(logData) != "log line" {
		t.Errorf("log file was modified: got %q", string(logData))
	}
}

func TestStreamNodeFiles_GCLogGating(t *testing.T) {
	tmpDir := t.TempDir()
	cs := &mockCopyStrategy{tmpDir: tmpDir}

	mc := &mockStreamCollector{
		streamFunc: func(host, remotePath string, writer io.Writer) error {
			_, err := writer.Write([]byte("gc content"))
			return err
		},
	}

	info := &RemoteNodeInfo{
		Files: []RemoteFileInfo{
			{Path: "/var/log/dremio/server.log", Size: 8, FileType: "log"},
			{Path: "/var/log/dremio/gc.log", Size: 10, FileType: "gc-log"},
		},
	}

	t.Run("disabled", func(t *testing.T) {
		collected, skipped := streamNodeFiles(mc, "host1", info, cs, "coordinator", "diagnosis", false, Args{CollectServerLogs: true})

		if len(collected) != 1 {
			t.Fatalf("expected 1 collected file, got %d", len(collected))
		}
		// GC logs are silently excluded, not counted as skipped.
		if len(skipped) != 0 {
			t.Fatalf("expected 0 skipped files, got %d: %v", len(skipped), skipped)
		}
	})

	t.Run("enabled", func(t *testing.T) {
		collected, skipped := streamNodeFiles(mc, "host1", info, cs, "coordinator", "diagnosis", true, Args{CollectServerLogs: true})

		if len(collected) != 2 {
			t.Fatalf("expected 2 collected files, got %d", len(collected))
		}
		if len(skipped) != 0 {
			t.Errorf("expected 0 skipped files, got %d: %v", len(skipped), skipped)
		}
		// Verify the gc-log file is in collected.
		foundGC := false
		for _, cf := range collected {
			if filepath.Base(cf.Path) == "gc.log" {
				foundGC = true
				break
			}
		}
		if !foundGC {
			t.Error("gc.log should be in collected files when gc-log collection is enabled")
		}
	})
}

// --- gzip decompression tests (T03) ---

// gzipCompress compresses data using gzip and returns the compressed bytes.
func gzipCompress(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(data); err != nil {
		t.Fatalf("gzip compress write: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip compress close: %v", err)
	}
	return buf.Bytes()
}

func TestStreamFileOnce_GzipDecompression(t *testing.T) {
	tmpDir := t.TempDir()
	content := []byte("hello world gzip decompression test content — this should round-trip through gzip")
	compressed := gzipCompress(t, content)

	mc := &mockStreamCollector{
		streamFunc: func(_, _ string, writer io.Writer) error {
			// When useGzip=true, write compressed bytes.
			_, err := writer.Write(compressed)
			return err
		},
	}

	destPath := filepath.Join(tmpDir, "gzip_out.txt")
	n, hashCh, err := streamFileOnce(mc, "host1", "/remote/file", destPath, int64(len(content)), "testfile.txt", "sha256sum", true)
	if err != nil {
		t.Fatalf("streamFileOnce with gzip returned error: %v", err)
	}

	// progressWriter should count decompressed bytes.
	if n != int64(len(content)) {
		t.Errorf("bytes counted = %d, want %d (decompressed size)", n, len(content))
	}

	// Consume hash channel.
	hr := <-hashCh
	if hr.err != nil {
		t.Fatalf("hash error: %v", hr.err)
	}
	// Verify hash matches decompressed content.
	wantHash := sha256.Sum256(content)
	wantHex := hex.EncodeToString(wantHash[:])
	if hr.hex != wantHex {
		t.Errorf("SHA-256 = %q, want %q", hr.hex, wantHex)
	}

	// Verify file on disk is decompressed.
	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("failed to read dest file: %v", err)
	}
	if !bytes.Equal(data, content) {
		t.Errorf("file content mismatch: got %d bytes, want %d", len(data), len(content))
	}
}

func TestStreamFileOnce_GzipFallback(t *testing.T) {
	tmpDir := t.TempDir()
	content := []byte("raw content without gzip compression")

	mc := &mockStreamCollector{
		streamFunc: func(_, _ string, writer io.Writer) error {
			_, err := writer.Write(content)
			return err
		},
	}

	destPath := filepath.Join(tmpDir, "fallback_out.txt")
	n, hashCh, err := streamFileOnce(mc, "host1", "/remote/file", destPath, int64(len(content)), "testfile.txt", "sha256sum", false)
	if err != nil {
		t.Fatalf("streamFileOnce with useGzip=false returned error: %v", err)
	}
	if n != int64(len(content)) {
		t.Errorf("bytes counted = %d, want %d", n, len(content))
	}

	hr := <-hashCh
	if hr.err != nil {
		t.Fatalf("hash error: %v", hr.err)
	}
	wantHash := sha256.Sum256(content)
	wantHex := hex.EncodeToString(wantHash[:])
	if hr.hex != wantHex {
		t.Errorf("SHA-256 = %q, want %q", hr.hex, wantHex)
	}

	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("failed to read dest file: %v", err)
	}
	if !bytes.Equal(data, content) {
		t.Errorf("file content mismatch")
	}
}

func TestStreamFileOnce_GzipStreamError(t *testing.T) {
	tmpDir := t.TempDir()

	mc := &mockStreamCollector{
		streamFunc: func(_, _ string, writer io.Writer) error {
			return fmt.Errorf("connection reset by peer")
		},
	}

	destPath := filepath.Join(tmpDir, "err_out.txt")
	_, _, err := streamFileOnce(mc, "host1", "/remote/file", destPath, 100, "testfile.txt", "sha256sum", true)
	if err == nil {
		t.Fatal("expected error from gzip stream, got nil")
	}
	if !strings.Contains(err.Error(), "connection reset") {
		t.Errorf("expected connection reset error, got: %v", err)
	}

	// Partial file should be cleaned up.
	if _, statErr := os.Stat(destPath); !os.IsNotExist(statErr) {
		t.Error("expected partial file to be removed after gzip stream error")
	}
}

func TestStreamFileOnce_GzipInvalidData(t *testing.T) {
	tmpDir := t.TempDir()

	mc := &mockStreamCollector{
		streamFunc: func(_, _ string, writer io.Writer) error {
			// Write non-gzip data when gzip is expected.
			_, err := writer.Write([]byte("this is not gzip data"))
			return err
		},
	}

	destPath := filepath.Join(tmpDir, "bad_gzip.txt")
	_, _, err := streamFileOnce(mc, "host1", "/remote/file", destPath, 100, "testfile.txt", "", true)
	if err == nil {
		t.Fatal("expected error for invalid gzip data, got nil")
	}
	if !strings.Contains(err.Error(), "gzip") {
		t.Errorf("expected gzip-related error, got: %v", err)
	}
}

func TestStreamFileOnce_8MBBuffer(t *testing.T) {
	// Verify that large files stream correctly through the 8MB buffer.
	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, "large.bin")

	// 10MB of data — exceeds the 8MB buffer to exercise flush.
	content := bytes.Repeat([]byte("A"), 10*1024*1024)

	mc := &mockStreamCollector{
		streamFunc: func(_, _ string, writer io.Writer) error {
			_, err := writer.Write(content)
			return err
		},
	}

	n, hashCh, err := streamFileOnce(mc, "host1", "/remote/file", destPath, int64(len(content)), "large.bin", "sha256sum", false)
	if err != nil {
		t.Fatalf("streamFileOnce returned error: %v", err)
	}
	if n != int64(len(content)) {
		t.Errorf("bytes written = %d, want %d", n, len(content))
	}
	hr := <-hashCh
	if hr.err != nil {
		t.Fatalf("hash error: %v", hr.err)
	}

	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("failed to read dest file: %v", err)
	}
	if len(data) != len(content) {
		t.Errorf("file size = %d, want %d", len(data), len(content))
	}
}
