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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- CollectOSInfo tests ---

func TestCollectOSInfo_HappyPath(t *testing.T) {
	outDir := t.TempDir()
	mock := &mockJVMCollector{
		hostExecuteFn: func(_ bool, _ string, args ...string) (string, error) {
			// Return identifiable output for each command
			if len(args) > 0 && args[0] == "uname" {
				return "5.15.0-generic\n", nil
			}
			if len(args) > 0 && args[0] == "lscpu" {
				return "Architecture:        x86_64\nCPU(s):              8\n", nil
			}
			if len(args) == 1 && strings.Contains(args[0], "cat /etc/*-release") {
				return "NAME=\"Ubuntu\"\nVERSION=\"22.04\"\n", nil
			}
			return "output\n", nil
		},
	}

	err := CollectOSInfo(mock, "node-1", outDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(outDir, "os_info.txt"))
	if err != nil {
		t.Fatalf("failed to read os_info.txt: %v", err)
	}

	text := string(content)
	// Verify header format
	if !strings.Contains(text, "___\n>>> cat /etc/*-release\n") {
		t.Error("missing cat /etc/*-release header")
	}
	if !strings.Contains(text, "___\n>>> uname -r\n") {
		t.Error("missing uname -r header")
	}
	if !strings.Contains(text, "___\n>>> lscpu\n") {
		t.Error("missing lscpu header")
	}
	// Verify content was captured
	if !strings.Contains(text, "Ubuntu") {
		t.Error("missing /etc/*-release content")
	}
	if !strings.Contains(text, "5.15.0") {
		t.Error("missing uname output")
	}
	if !strings.Contains(text, "x86_64") {
		t.Error("missing lscpu output")
	}
}

func TestCollectOSInfo_GlobPassedAsSingleArg(t *testing.T) {
	outDir := t.TempDir()
	var globCalls [][]string
	mock := &mockJVMCollector{
		hostExecuteFn: func(_ bool, _ string, args ...string) (string, error) {
			if len(args) == 1 && strings.Contains(args[0], "*") {
				globCalls = append(globCalls, args)
			}
			return "ok\n", nil
		},
	}

	err := CollectOSInfo(mock, "node-1", outDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// cat /etc/*-release should be a single arg (no sh -c wrapping)
	// so the remote SSH shell handles glob expansion.
	found := false
	for _, call := range globCalls {
		if strings.Contains(call[0], "cat /etc/*-release") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected single-arg glob call for cat /etc/*-release, got calls: %v", globCalls)
	}
}

func TestCollectOSInfo_PartialFailure(t *testing.T) {
	outDir := t.TempDir()
	mock := &mockJVMCollector{
		hostExecuteFn: func(_ bool, _ string, args ...string) (string, error) {
			if len(args) > 0 && args[0] == "lsblk" {
				return "", errors.New("lsblk not found")
			}
			if len(args) > 0 && args[0] == "uname" {
				return "5.15.0\n", nil
			}
			return "ok\n", nil
		},
	}

	// Should succeed despite lsblk failure (K013)
	err := CollectOSInfo(mock, "node-1", outDir)
	if err != nil {
		t.Fatalf("expected success despite partial failure: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(outDir, "os_info.txt"))
	if err != nil {
		t.Fatalf("failed to read os_info.txt: %v", err)
	}

	text := string(content)
	// Successful commands still present
	if !strings.Contains(text, "5.15.0") {
		t.Error("uname output should be present despite lsblk failure")
	}
	// lsblk header present but no output after it
	if !strings.Contains(text, ">>> lsblk") {
		t.Error("lsblk header should still be present")
	}
}

func TestCollectOSInfo_AllCommandsFail(t *testing.T) {
	outDir := t.TempDir()
	mock := &mockJVMCollector{
		hostExecuteFn: func(_ bool, _ string, _ ...string) (string, error) {
			return "", errors.New("host unreachable")
		},
	}

	// Should still succeed — file is written with headers only
	err := CollectOSInfo(mock, "node-1", outDir)
	if err != nil {
		t.Fatalf("expected success even when all commands fail: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(outDir, "os_info.txt"))
	if err != nil {
		t.Fatalf("os_info.txt should exist: %v", err)
	}
	if len(content) == 0 {
		t.Error("os_info.txt should contain at least headers")
	}
}

// --- CollectDiskUsage tests ---

func TestCollectDiskUsage_HappyPath(t *testing.T) {
	outDir := t.TempDir()
	dfOutput := "Filesystem      Size  Used Avail Use% Mounted on\n/dev/sda1       100G   50G   50G  50% /\n"
	mock := &mockJVMCollector{
		hostExecuteFn: func(_ bool, _ string, args ...string) (string, error) {
			if len(args) >= 2 && args[0] == "df" && args[1] == "-h" {
				return dfOutput, nil
			}
			return "", nil
		},
	}

	err := CollectDiskUsage(mock, "node-1", outDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(outDir, "diskusage.txt"))
	if err != nil {
		t.Fatalf("failed to read diskusage.txt: %v", err)
	}
	if string(content) != dfOutput {
		t.Errorf("unexpected content: %s", content)
	}
}

func TestCollectDiskUsage_HostExecuteError(t *testing.T) {
	outDir := t.TempDir()
	mock := &mockJVMCollector{
		hostExecuteFn: func(_ bool, _ string, _ ...string) (string, error) {
			return "", errors.New("df command failed")
		},
	}

	err := CollectDiskUsage(mock, "node-1", outDir)
	if err == nil {
		t.Fatal("expected error when df fails")
	}
	if !strings.Contains(err.Error(), "df") {
		t.Errorf("error should mention df: %v", err)
	}
	if !strings.Contains(err.Error(), "node-1") {
		t.Errorf("error should mention host: %v", err)
	}
}

// --- CollectRocksDBDiskUsage tests ---

// rocksDBMock returns a hostExecuteFn that responds to the test -d probe with "exists"
// and delegates du commands to duHandler.
func rocksDBMock(duHandler func(args ...string) (string, error)) func(bool, string, ...string) (string, error) {
	return func(_ bool, _ string, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "test" {
			return "exists\n", nil
		}
		return duHandler(args...)
	}
}

func TestCollectRocksDBDiskUsage_HappyPath(t *testing.T) {
	outDir := t.TempDir()
	duOutput := "4.0G\t/opt/dremio/data/db/catalog\n2.0G\t/opt/dremio/data/db/search\n"
	mock := &mockJVMCollector{
		hostExecuteFn: rocksDBMock(func(args ...string) (string, error) {
			return duOutput, nil
		}),
	}

	err := CollectRocksDBDiskUsage(mock, "coord-1", outDir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(outDir, "rocksdb_disk_allocation.txt"))
	if err != nil {
		t.Fatalf("failed to read rocksdb_disk_allocation.txt: %v", err)
	}
	if string(content) != duOutput {
		t.Errorf("unexpected content: %s", content)
	}
}

func TestCollectRocksDBDiskUsage_SingleArgGlob(t *testing.T) {
	outDir := t.TempDir()
	var capturedDuArgs []string
	mock := &mockJVMCollector{
		hostExecuteFn: rocksDBMock(func(args ...string) (string, error) {
			capturedDuArgs = args
			return "ok\n", nil
		}),
	}

	err := CollectRocksDBDiskUsage(mock, "coord-1", outDir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(capturedDuArgs) != 1 {
		t.Fatalf("expected single arg, got: %v", capturedDuArgs)
	}
	if !strings.Contains(capturedDuArgs[0], "du -sh /opt/dremio/data/db/*") {
		t.Errorf("expected du -sh glob command, got: %s", capturedDuArgs[0])
	}
}

func TestCollectRocksDBDiskUsage_DirNotFound(t *testing.T) {
	outDir := t.TempDir()
	mock := &mockJVMCollector{
		hostExecuteFn: func(_ bool, _ string, args ...string) (string, error) {
			// test -d returns empty (dir not found)
			return "", nil
		},
	}

	err := CollectRocksDBDiskUsage(mock, "coord-1", outDir, "")
	if err != nil {
		t.Fatalf("expected nil error when dir doesn't exist, got: %v", err)
	}
	// No file should be written
	if _, statErr := os.Stat(filepath.Join(outDir, "rocksdb_disk_allocation.txt")); !os.IsNotExist(statErr) {
		t.Error("expected no output file when dir doesn't exist")
	}
}

func TestCollectRocksDBDiskUsage_HostExecuteError(t *testing.T) {
	outDir := t.TempDir()
	mock := &mockJVMCollector{
		hostExecuteFn: rocksDBMock(func(_ ...string) (string, error) {
			return "", errors.New("du: cannot access '/opt/dremio/data/db/*'")
		}),
	}

	err := CollectRocksDBDiskUsage(mock, "coord-1", outDir, "")
	if err == nil {
		t.Fatal("expected error when du fails")
	}
	if !strings.Contains(err.Error(), "du") {
		t.Errorf("error should mention du: %v", err)
	}
}

func TestCollectRocksDBDiskUsage_CustomPath(t *testing.T) {
	outDir := t.TempDir()
	var capturedDuArgs []string
	mock := &mockJVMCollector{
		hostExecuteFn: rocksDBMock(func(args ...string) (string, error) {
			capturedDuArgs = args
			return "1.0G\t/custom/rocksdb/catalog\n", nil
		}),
	}

	err := CollectRocksDBDiskUsage(mock, "coord-1", outDir, "/custom/rocksdb")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(capturedDuArgs) != 1 || !strings.Contains(capturedDuArgs[0], "du -sh /custom/rocksdb/*") {
		t.Errorf("expected du -sh /custom/rocksdb/*, got: %v", capturedDuArgs)
	}
}

func TestCollectRocksDBDiskUsage_EmptyPathUsesDefault(t *testing.T) {
	outDir := t.TempDir()
	var capturedDuArgs []string
	mock := &mockJVMCollector{
		hostExecuteFn: rocksDBMock(func(args ...string) (string, error) {
			capturedDuArgs = args
			return "2.0G\t/opt/dremio/data/db/catalog\n", nil
		}),
	}

	err := CollectRocksDBDiskUsage(mock, "coord-1", outDir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(capturedDuArgs) != 1 || !strings.Contains(capturedDuArgs[0], "du -sh /opt/dremio/data/db/*") {
		t.Errorf("expected du -sh /opt/dremio/data/db/*, got: %v", capturedDuArgs)
	}
}
