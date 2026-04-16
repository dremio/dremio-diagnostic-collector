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
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/cli"
)

// mockJVMCollector implements Collector with configurable HostExecute, StreamFromHost, and CopyToHost callbacks.
type mockJVMCollector struct {
	hostExecuteFn    func(mask bool, host string, args ...string) (string, error)
	streamFromHostFn func(host, remotePath string, writer io.Writer) error
	copyToHostFn     func(host, source, destination string) (string, error)
	calls            [][]string // records each HostExecute call's args
}

func (m *mockJVMCollector) CopyToHost(host, source, destination string) (string, error) {
	if m.copyToHostFn != nil {
		return m.copyToHostFn(host, source, destination)
	}
	return "", nil
}
func (m *mockJVMCollector) GetCoordinators() ([]string, error) { return nil, nil }
func (m *mockJVMCollector) GetExecutors() ([]string, error)    { return nil, nil }
func (m *mockJVMCollector) HostExecute(mask bool, host string, args ...string) (string, error) {
	m.calls = append(m.calls, args)
	if m.hostExecuteFn != nil {
		return m.hostExecuteFn(mask, host, args...)
	}
	return "", nil
}
func (m *mockJVMCollector) HostExecuteAndStream(_ bool, _ string, _ cli.OutputHandler, _ string, _ ...string) error {
	return nil
}
func (m *mockJVMCollector) HelpText() string       { return "" }
func (m *mockJVMCollector) Name() string           { return "mock-jvm" }
func (m *mockJVMCollector) Protocol() string       { return "mock" }
func (m *mockJVMCollector) SetHostPid(_, _ string) {}
func (m *mockJVMCollector) CleanupRemote() error   { return nil }
func (m *mockJVMCollector) StreamFromHost(host string, remotePath string, writer io.Writer, _ bool) error {
	if m.streamFromHostFn != nil {
		return m.streamFromHostFn(host, remotePath, writer)
	}
	return nil
}
func (m *mockJVMCollector) DiscoverFiles(_, _, _ string) (*RemoteNodeInfo, error) {
	return &RemoteNodeInfo{}, nil
}

// noSleep is a sleepFn that doesn't wait — used to keep tests fast.
func noSleep(_ time.Duration) {}

// --- jstack tests ---

func TestCollectJStack_CapturesIterations(t *testing.T) {
	outDir := t.TempDir()
	callCount := 0
	mock := &mockJVMCollector{
		hostExecuteFn: func(_ bool, _ string, args ...string) (string, error) {
			callCount++
			return fmt.Sprintf("thread dump iteration %d\njcmd %s", callCount, strings.Join(args, " ")), nil
		},
	}

	err := CollectJStack(mock, "node-1", 12345, 2, outDir, "coord-1", noSleep)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if callCount < 1 {
		t.Errorf("expected at least 1 HostExecute call, got %d", callCount)
	}

	// Verify files were created
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("failed to read output dir: %v", err)
	}
	if len(entries) < 1 {
		t.Fatalf("expected at least 1 thread dump file, got %d", len(entries))
	}

	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "threadDump-coord-1-") || !strings.HasSuffix(e.Name(), ".txt") {
			t.Errorf("unexpected filename pattern: %s", e.Name())
		}
		content, err := os.ReadFile(filepath.Join(outDir, e.Name()))
		if err != nil {
			t.Fatalf("failed to read %s: %v", e.Name(), err)
		}
		if !strings.Contains(string(content), "thread dump iteration") {
			t.Errorf("file %s missing expected content", e.Name())
		}
	}

	// Verify HostExecute was called with jcmd via the timeout wrapper
	for _, call := range mock.calls {
		joined := strings.Join(call, " ")
		if !strings.Contains(joined, "jcmd") || !strings.Contains(joined, "Thread.print") {
			t.Errorf("unexpected args: %v", call)
		}
	}
}

func TestCollectJStack_HostExecuteError(t *testing.T) {
	outDir := t.TempDir()
	mock := &mockJVMCollector{
		hostExecuteFn: func(_ bool, _ string, _ ...string) (string, error) {
			return "", errors.New("ssh connection refused")
		},
	}

	err := CollectJStack(mock, "node-1", 12345, 1, outDir, "coord-1", noSleep)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "ssh connection refused") {
		t.Errorf("error should wrap original: %v", err)
	}
	if !strings.Contains(err.Error(), "node-1") {
		t.Errorf("error should mention host: %v", err)
	}
}

// --- top tests ---

func TestCollectTop_CapturesOutput(t *testing.T) {
	outDir := t.TempDir()
	mock := &mockJVMCollector{
		hostExecuteFn: func(_ bool, _ string, args ...string) (string, error) {
			cmd := strings.Join(args, " ")
			if !strings.Contains(cmd, "top -H -p 12345 -d 1 -n 3 -bw 512") {
				t.Errorf("expected top -H -p 12345 -d 1 -n 3 -bw 512 in command, got: %s", cmd)
			}
			return "PID USER\n12345 dremio\nPID USER\n12345 dremio\n", nil
		},
	}

	err := CollectTop(mock, "node-1", 12345, 3, outDir, "node-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(outDir, "node-1_top.txt"))
	if err != nil {
		t.Fatalf("failed to read top.txt: %v", err)
	}
	if !strings.Contains(string(content), "12345") {
		t.Error("top.txt missing expected PID in output")
	}
	if !strings.Contains(string(content), "PID USER") {
		t.Error("top.txt missing expected header")
	}
}

func TestCollectTop_HostExecuteError(t *testing.T) {
	outDir := t.TempDir()
	mock := &mockJVMCollector{
		hostExecuteFn: func(_ bool, _ string, _ ...string) (string, error) {
			return "", errors.New("top not found")
		},
	}

	err := CollectTop(mock, "node-1", 12345, 30, outDir, "node-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "top not found") {
		t.Errorf("error should wrap original: %v", err)
	}
}

// --- JVM flags tests ---

func TestCollectJVMFlags_CapturesBoth(t *testing.T) {
	outDir := t.TempDir()
	mock := &mockJVMCollector{
		hostExecuteFn: func(_ bool, _ string, args ...string) (string, error) {
			joined := strings.Join(args, " ")
			if strings.Contains(joined, "VM.flags") {
				return "-XX:+UseG1GC -XX:MaxHeapSize=8589934592", nil
			}
			if strings.Contains(joined, "VM.system_properties") {
				return "java.version=11.0.20\nuser.dir=/opt/dremio", nil
			}
			return "", fmt.Errorf("unexpected args: %v", args)
		},
	}

	err := CollectJVMFlags(mock, "node-1", 12345, outDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(outDir, "jvm_settings.txt"))
	if err != nil {
		t.Fatalf("failed to read jvm_settings.txt: %v", err)
	}

	text := string(content)
	if !strings.Contains(text, "VM.flags") {
		t.Error("output missing VM.flags section")
	}
	if !strings.Contains(text, "UseG1GC") {
		t.Error("output missing VM.flags content")
	}
	if !strings.Contains(text, "VM.system_properties") {
		t.Error("output missing VM.system_properties section")
	}
	if !strings.Contains(text, "java.version") {
		t.Error("output missing VM.system_properties content")
	}
}

func TestCollectJVMFlags_PartialFailure(t *testing.T) {
	outDir := t.TempDir()
	mock := &mockJVMCollector{
		hostExecuteFn: func(_ bool, _ string, args ...string) (string, error) {
			joined := strings.Join(args, " ")
			if strings.Contains(joined, "VM.flags") {
				return "-XX:+UseG1GC", nil
			}
			if strings.Contains(joined, "VM.system_properties") {
				return "", errors.New("jcmd not responding")
			}
			return "", fmt.Errorf("unexpected args: %v", args)
		},
	}

	err := CollectJVMFlags(mock, "node-1", 12345, outDir)
	if err == nil {
		t.Fatal("expected error for partial failure")
	}
	if !strings.Contains(err.Error(), "VM.system_properties") {
		t.Errorf("error should mention failed subcommand: %v", err)
	}

	// File should still be written with partial content
	content, err := os.ReadFile(filepath.Join(outDir, "jvm_settings.txt"))
	if err != nil {
		t.Fatalf("failed to read jvm_settings.txt after partial failure: %v", err)
	}

	text := string(content)
	if !strings.Contains(text, "UseG1GC") {
		t.Error("partial output should contain successful VM.flags content")
	}
	if !strings.Contains(text, "ERROR") {
		t.Error("partial output should note the error for the failed subcommand")
	}
}

func TestCollectJVMFlags_BothFail(t *testing.T) {
	outDir := t.TempDir()
	mock := &mockJVMCollector{
		hostExecuteFn: func(_ bool, _ string, _ ...string) (string, error) {
			return "", errors.New("host unreachable")
		},
	}

	err := CollectJVMFlags(mock, "node-1", 12345, outDir)
	if err == nil {
		t.Fatal("expected error when both subcommands fail")
	}

	// File should still be written with error markers
	content, err := os.ReadFile(filepath.Join(outDir, "jvm_settings.txt"))
	if err != nil {
		t.Fatalf("jvm_settings.txt should exist even on full failure: %v", err)
	}
	if !strings.Contains(string(content), "ERROR") {
		t.Error("output should contain error markers")
	}
}

// --- JFR tests ---

func TestCollectJFR_HappyPath(t *testing.T) {
	outDir := t.TempDir()
	mock := &mockJVMCollector{
		hostExecuteFn: func(_ bool, _ string, _ ...string) (string, error) {
			return "", nil
		},
		streamFromHostFn: func(_ string, _ string, w io.Writer) error {
			_, err := w.Write([]byte("fake-jfr-data"))
			return err
		},
	}

	err := CollectJFR(mock, "node-1", 12345, 10, outDir, "coord-1", noSleep)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify local file written
	content, err := os.ReadFile(filepath.Join(outDir, "coord-1.jfr"))
	if err != nil {
		t.Fatalf("failed to read .jfr file: %v", err)
	}
	if string(content) != "fake-jfr-data" {
		t.Errorf("unexpected .jfr content: %s", content)
	}

	// Verify command sequence: unlock, stop(prior), start, dump, stop, stat (file size probe), rm
	expectedSubcmds := []string{
		"VM.unlock_commercial_features",
		"JFR.stop",
		"JFR.start",
		"JFR.dump",
		"JFR.stop",
		"stat",
		"rm",
	}
	if len(mock.calls) != len(expectedSubcmds) {
		t.Fatalf("expected %d HostExecute calls, got %d: %v", len(expectedSubcmds), len(mock.calls), mock.calls)
	}
	for i, expect := range expectedSubcmds {
		found := false
		for _, arg := range mock.calls[i] {
			if strings.Contains(arg, expect) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("call %d: expected arg containing %q, got %v", i, expect, mock.calls[i])
		}
	}
}

func TestCollectJFR_StartFailure(t *testing.T) {
	outDir := t.TempDir()
	mock := &mockJVMCollector{
		hostExecuteFn: func(_ bool, _ string, args ...string) (string, error) {
			for _, a := range args {
				if strings.Contains(a, "JFR.start") {
					return "", errors.New("cannot start JFR")
				}
			}
			return "", nil
		},
	}

	err := CollectJFR(mock, "node-1", 12345, 10, outDir, "coord-1", noSleep)
	if err == nil {
		t.Fatal("expected error when JFR.start fails")
	}
	if !strings.Contains(err.Error(), "JFR.start") {
		t.Errorf("error should mention JFR.start: %v", err)
	}
}

func TestCollectJFR_StreamFailure(t *testing.T) {
	outDir := t.TempDir()
	mock := &mockJVMCollector{
		hostExecuteFn: func(_ bool, _ string, _ ...string) (string, error) {
			return "", nil
		},
		streamFromHostFn: func(_ string, _ string, _ io.Writer) error {
			return errors.New("stream broken")
		},
	}

	err := CollectJFR(mock, "node-1", 12345, 10, outDir, "coord-1", noSleep)
	if err == nil {
		t.Fatal("expected error when StreamFromHost fails")
	}
	if !strings.Contains(err.Error(), "stream") {
		t.Errorf("error should mention stream: %v", err)
	}

	// Verify cleanup still ran (rm -f should be the last call)
	lastCall := mock.calls[len(mock.calls)-1]
	if lastCall[0] != "rm" {
		t.Errorf("cleanup should run even on stream failure, last call was: %v", lastCall)
	}
}

// --- Heap dump tests ---

func TestCollectHeapDump_HappyPath(t *testing.T) {
	outDir := t.TempDir()
	mock := &mockJVMCollector{
		hostExecuteFn: func(_ bool, _ string, _ ...string) (string, error) {
			return "", nil
		},
		streamFromHostFn: func(_ string, _ string, w io.Writer) error {
			_, err := w.Write([]byte("fake-heap-data"))
			return err
		},
	}

	err := CollectHeapDump(mock, "node-1", 12345, outDir, "coord-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No gzip — raw .hprof streamed directly
	content, err := os.ReadFile(filepath.Join(outDir, "coord-1.hprof"))
	if err != nil {
		t.Fatalf("failed to read .hprof file: %v", err)
	}
	if string(content) != "fake-heap-data" {
		t.Errorf("unexpected content: %s", content)
	}

	// Verify command sequence: jmap, stat (file size probe), rm
	if len(mock.calls) != 3 {
		t.Fatalf("expected 3 HostExecute calls, got %d: %v", len(mock.calls), mock.calls)
	}
	if mock.calls[0][0] != "jmap" {
		t.Errorf("first call should be jmap, got %v", mock.calls[0])
	}
	if mock.calls[1][0] != "stat" {
		t.Errorf("second call should be stat (file size probe), got %v", mock.calls[1])
	}
	if mock.calls[2][0] != "rm" {
		t.Errorf("third call should be rm, got %v", mock.calls[2])
	}
}

func TestCollectHeapDump_JmapFailure(t *testing.T) {
	outDir := t.TempDir()
	mock := &mockJVMCollector{
		hostExecuteFn: func(_ bool, _ string, args ...string) (string, error) {
			if len(args) > 0 && args[0] == "jmap" {
				return "", errors.New("jmap: process not found")
			}
			return "", nil
		},
	}

	err := CollectHeapDump(mock, "node-1", 12345, outDir, "coord-1")
	if err == nil {
		t.Fatal("expected error when jmap fails")
	}
	if !strings.Contains(err.Error(), "jmap") {
		t.Errorf("error should mention jmap: %v", err)
	}
}

// --- Async-profiler tests ---

func TestCollectAsyncProfiler_HappyPath(t *testing.T) {
	outDir := t.TempDir()
	mock := &mockJVMCollector{
		hostExecuteFn: func(_ bool, _ string, _ ...string) (string, error) {
			return "", nil
		},
		streamFromHostFn: func(_ string, _ string, w io.Writer) error {
			_, err := w.Write([]byte("fake-asprof-jfr"))
			return err
		},
	}

	err := CollectAsyncProfiler(mock, "node-1", 12345, 30, outDir, "coord-1", []byte("fake-binary"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify output file
	content, err := os.ReadFile(filepath.Join(outDir, "coord-1-asprof.jfr"))
	if err != nil {
		t.Fatalf("failed to read asprof output: %v", err)
	}
	if string(content) != "fake-asprof-jfr" {
		t.Errorf("unexpected content: %s", content)
	}

	// Verify command sequence: chmod, execute, rm
	if len(mock.calls) < 3 {
		t.Fatalf("expected at least 3 HostExecute calls, got %d: %v", len(mock.calls), mock.calls)
	}
	if mock.calls[0][0] != "chmod" {
		t.Errorf("first call should be chmod, got %v", mock.calls[0])
	}
	// Last call should be cleanup rm
	lastCall := mock.calls[len(mock.calls)-1]
	if lastCall[0] != "rm" {
		t.Errorf("last call should be rm (cleanup), got %v", lastCall)
	}
}

func TestCollectAsyncProfiler_EmptyBinarySkips(t *testing.T) {
	outDir := t.TempDir()
	mock := &mockJVMCollector{}

	err := CollectAsyncProfiler(mock, "node-1", 12345, 30, outDir, "coord-1", nil)
	if err != nil {
		t.Fatalf("expected nil error for empty binary, got: %v", err)
	}

	err = CollectAsyncProfiler(mock, "node-1", 12345, 30, outDir, "coord-1", []byte{})
	if err != nil {
		t.Fatalf("expected nil error for zero-length binary, got: %v", err)
	}

	// No HostExecute calls should have been made
	if len(mock.calls) != 0 {
		t.Errorf("expected 0 HostExecute calls for empty binary, got %d", len(mock.calls))
	}
}

func TestCollectAsyncProfiler_CopyToHostFailure(t *testing.T) {
	outDir := t.TempDir()
	mock := &mockJVMCollector{
		copyToHostFn: func(_, _, _ string) (string, error) {
			return "", errors.New("scp failed")
		},
	}

	err := CollectAsyncProfiler(mock, "node-1", 12345, 30, outDir, "coord-1", []byte("fake-binary"))
	if err == nil {
		t.Fatal("expected error when CopyToHost fails")
	}
	if !strings.Contains(err.Error(), "CopyToHost") {
		t.Errorf("error should mention CopyToHost: %v", err)
	}
}

func TestCollectAsyncProfiler_ChmodFailure(t *testing.T) {
	outDir := t.TempDir()
	rmCalled := false
	mock := &mockJVMCollector{
		hostExecuteFn: func(_ bool, _ string, args ...string) (string, error) {
			if len(args) > 0 && args[0] == "chmod" {
				return "", errors.New("chmod: permission denied")
			}
			if len(args) > 0 && args[0] == "rm" {
				rmCalled = true
			}
			return "", nil
		},
	}

	err := CollectAsyncProfiler(mock, "node-1", 12345, 30, outDir, "coord-1", []byte("fake-binary"))
	if err == nil {
		t.Fatal("expected error when chmod fails")
	}
	if !strings.Contains(err.Error(), "chmod") {
		t.Errorf("error should mention chmod: %v", err)
	}
	if !rmCalled {
		t.Error("cleanup should run after chmod failure")
	}
}

func TestCollectAsyncProfiler_ExecutionFailure(t *testing.T) {
	outDir := t.TempDir()
	rmCalled := false
	mock := &mockJVMCollector{
		hostExecuteFn: func(_ bool, _ string, args ...string) (string, error) {
			if len(args) > 0 && args[0] == "chmod" {
				return "", nil
			}
			if len(args) > 0 && args[0] == "rm" {
				rmCalled = true
				return "", nil
			}
			// The asprof execution call — args[0] is the remote binary path
			if len(args) > 0 && strings.Contains(args[0], "ddc-asprof") {
				return "", errors.New("asprof: failed to attach to process")
			}
			return "", nil
		},
	}

	err := CollectAsyncProfiler(mock, "node-1", 12345, 30, outDir, "coord-1", []byte("fake-binary"))
	if err == nil {
		t.Fatal("expected error when asprof execution fails")
	}
	if !strings.Contains(err.Error(), "execution failed") {
		t.Errorf("error should mention execution: %v", err)
	}
	if !rmCalled {
		t.Error("cleanup should run after execution failure")
	}
}

func TestCollectAsyncProfiler_StreamFailure(t *testing.T) {
	outDir := t.TempDir()
	rmCalled := false
	mock := &mockJVMCollector{
		hostExecuteFn: func(_ bool, _ string, args ...string) (string, error) {
			if len(args) > 0 && args[0] == "rm" {
				rmCalled = true
			}
			return "", nil
		},
		streamFromHostFn: func(_ string, _ string, _ io.Writer) error {
			return errors.New("stream broken")
		},
	}

	err := CollectAsyncProfiler(mock, "node-1", 12345, 30, outDir, "coord-1", []byte("fake-binary"))
	if err == nil {
		t.Fatal("expected error when stream fails")
	}
	if !strings.Contains(err.Error(), "stream") {
		t.Errorf("error should mention stream: %v", err)
	}
	if !rmCalled {
		t.Error("cleanup should run after stream failure")
	}
}

func TestCollectAsyncProfiler_CleanupAfterFailure(t *testing.T) {
	outDir := t.TempDir()
	var rmArgs []string
	mock := &mockJVMCollector{
		hostExecuteFn: func(_ bool, _ string, args ...string) (string, error) {
			if len(args) > 0 && args[0] == "rm" {
				rmArgs = args
				return "", nil
			}
			if len(args) > 0 && args[0] == "chmod" {
				return "", nil
			}
			// asprof execution — fail
			if len(args) > 0 && strings.Contains(args[0], "ddc-asprof") {
				return "", errors.New("segfault")
			}
			return "", nil
		},
	}

	_ = CollectAsyncProfiler(mock, "node-1", 12345, 30, outDir, "coord-1", []byte("fake-binary"))

	// Cleanup should remove both the binary and output file
	if len(rmArgs) < 3 {
		t.Fatalf("expected rm with at least 3 args (rm -f bin out), got: %v", rmArgs)
	}
	rmJoined := strings.Join(rmArgs, " ")
	if !strings.Contains(rmJoined, "ddc-asprof-node-1") {
		t.Errorf("cleanup should remove remote binary, args: %v", rmArgs)
	}
	if !strings.Contains(rmJoined, "ddc-asprof-out-node-1") {
		t.Errorf("cleanup should remove remote output, args: %v", rmArgs)
	}
}

// --- CheckRemoteDiskSpace tests ---

func TestCheckRemoteDiskSpace_HappyPath(t *testing.T) {
	mock := &mockJVMCollector{
		hostExecuteFn: func(_ bool, _ string, _ ...string) (string, error) {
			// Typical df -P output
			return "Filesystem           1024-blocks      Used Available Capacity Mounted on\n/dev/sda1            102400000  51200000  51200000      50% /\n", nil
		},
	}

	avail, err := CheckRemoteDiskSpace(mock, "node-1", "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 51200000 KB * 1024 = 52428800000 bytes
	expected := uint64(51200000) * 1024
	if avail != expected {
		t.Errorf("expected %d bytes, got %d", expected, avail)
	}
}

func TestCheckRemoteDiskSpace_UnparseableOutput(t *testing.T) {
	mock := &mockJVMCollector{
		hostExecuteFn: func(_ bool, _ string, _ ...string) (string, error) {
			return "garbage output", nil
		},
	}

	_, err := CheckRemoteDiskSpace(mock, "node-1", "/tmp")
	if err == nil {
		t.Fatal("expected error for unparseable output")
	}
}

func TestCheckRemoteDiskSpace_HostExecuteError(t *testing.T) {
	mock := &mockJVMCollector{
		hostExecuteFn: func(_ bool, _ string, _ ...string) (string, error) {
			return "", errors.New("host unreachable")
		},
	}

	_, err := CheckRemoteDiskSpace(mock, "node-1", "/tmp")
	if err == nil {
		t.Fatal("expected error when HostExecute fails")
	}
	if !strings.Contains(err.Error(), "host unreachable") {
		t.Errorf("error should wrap original: %v", err)
	}
}

func TestCheckRemoteDiskSpace_NonNumericAvailable(t *testing.T) {
	mock := &mockJVMCollector{
		hostExecuteFn: func(_ bool, _ string, _ ...string) (string, error) {
			return "Filesystem           1024-blocks      Used Available Capacity Mounted on\n/dev/sda1            102400000  51200000  NOPE      50% /\n", nil
		},
	}

	_, err := CheckRemoteDiskSpace(mock, "node-1", "/tmp")
	if err == nil {
		t.Fatal("expected error for non-numeric available field")
	}
}

// --- ParseXmxBytes tests ---

func TestParseXmxBytes_Typical(t *testing.T) {
	input := "12345:\n-XX:CICompilerCount=4 -XX:ConcGCThreads=2 -XX:G1ConcRefinementThreads=8 -XX:G1HeapRegionSize=4194304 -XX:InitialHeapSize=268435456 -XX:MaxHeapSize=8589934592 -XX:+UseG1GC"
	val, err := ParseXmxBytes(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 8589934592 {
		t.Errorf("expected 8589934592, got %d", val)
	}
}

func TestParseXmxBytes_NotFound(t *testing.T) {
	input := "-XX:+UseG1GC -XX:CICompilerCount=4"
	_, err := ParseXmxBytes(input)
	if err == nil {
		t.Fatal("expected error when MaxHeapSize not found")
	}
}

func TestParseXmxBytes_MultipleFlags(t *testing.T) {
	// Only MaxHeapSize should be extracted
	input := "-XX:InitialHeapSize=268435456 -XX:MaxHeapSize=4294967296 -XX:MinHeapSize=268435456"
	val, err := ParseXmxBytes(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 4294967296 {
		t.Errorf("expected 4294967296, got %d", val)
	}
}

// --- probeRemoteFileSize tests ---

func TestProbeRemoteFileSize_GNUStatSuccess(t *testing.T) {
	mock := &mockJVMCollector{
		hostExecuteFn: func(_ bool, _ string, args ...string) (string, error) {
			if len(args) >= 3 && args[0] == "stat" && args[1] == "-c" {
				return "1048576\n", nil // 1MB
			}
			return "", nil
		},
	}

	size := probeRemoteFileSize(mock, "node-1", "/tmp/test.hprof.gz")
	if size != 1048576 {
		t.Errorf("expected 1048576, got %d", size)
	}

	// Should only call stat once (GNU stat succeeded)
	if len(mock.calls) != 1 {
		t.Errorf("expected 1 stat call, got %d: %v", len(mock.calls), mock.calls)
	}
}

func TestProbeRemoteFileSize_BSDFallback(t *testing.T) {
	mock := &mockJVMCollector{
		hostExecuteFn: func(_ bool, _ string, args ...string) (string, error) {
			if len(args) >= 3 && args[0] == "stat" && args[1] == "-c" {
				return "", errors.New("stat: illegal option -- c")
			}
			if len(args) >= 3 && args[0] == "stat" && args[1] == "-f" {
				return "2097152\n", nil // 2MB
			}
			return "", nil
		},
	}

	size := probeRemoteFileSize(mock, "node-1", "/tmp/test.hprof.gz")
	if size != 2097152 {
		t.Errorf("expected 2097152, got %d", size)
	}

	// Should call stat twice (GNU failed, BSD succeeded)
	if len(mock.calls) != 2 {
		t.Errorf("expected 2 stat calls, got %d: %v", len(mock.calls), mock.calls)
	}
}

func TestProbeRemoteFileSize_BothFail(t *testing.T) {
	mock := &mockJVMCollector{
		hostExecuteFn: func(_ bool, _ string, args ...string) (string, error) {
			if len(args) > 0 && args[0] == "stat" {
				return "", errors.New("stat not found")
			}
			return "", nil
		},
	}

	size := probeRemoteFileSize(mock, "node-1", "/tmp/test.hprof.gz")
	if size != 0 {
		t.Errorf("expected 0 when stat fails, got %d", size)
	}
}

func TestProbeRemoteFileSize_UnparseableOutput(t *testing.T) {
	mock := &mockJVMCollector{
		hostExecuteFn: func(_ bool, _ string, _ ...string) (string, error) {
			return "not-a-number", nil
		},
	}

	size := probeRemoteFileSize(mock, "node-1", "/tmp/test.hprof.gz")
	if size != 0 {
		t.Errorf("expected 0 for unparseable output, got %d", size)
	}
}
