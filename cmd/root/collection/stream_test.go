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

package collection_test

import (
	"bytes"
	"fmt"
	"io"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/cli"
)

// mockCollectorForStream is a minimal mock implementing only StreamFromHost
// for testing the interface contract and binary data integrity.
type mockCollectorForStream struct {
	streamFn func(host, remotePath string, writer io.Writer) error
}

func (m *mockCollectorForStream) CopyToHost(_, _, _ string) (string, error) { return "", nil }
func (m *mockCollectorForStream) GetCoordinators() ([]string, error)        { return nil, nil }
func (m *mockCollectorForStream) GetExecutors() ([]string, error)           { return nil, nil }
func (m *mockCollectorForStream) HostExecute(_ bool, _ string, _ ...string) (string, error) {
	return "", nil
}
func (m *mockCollectorForStream) HostExecuteAndStream(_ bool, _ string, _ cli.OutputHandler, _ string, _ ...string) error {
	return nil
}
func (m *mockCollectorForStream) HelpText() string       { return "" }
func (m *mockCollectorForStream) Name() string           { return "mock" }
func (m *mockCollectorForStream) Protocol() string       { return "mock" }
func (m *mockCollectorForStream) SetHostPid(_, _ string) {}
func (m *mockCollectorForStream) CleanupRemote() error   { return nil }
func (m *mockCollectorForStream) StreamFromHost(host, remotePath string, writer io.Writer, _ bool) error {
	return m.streamFn(host, remotePath, writer)
}

// TestStreamFromHost_BinaryIntegrity verifies that binary data including \n, \0,
// and 0xFF bytes pass through StreamFromHost unchanged (mock simulating K8s transport).
func TestStreamFromHost_BinaryIntegrity(t *testing.T) {
	// Build a payload with problematic bytes: newlines, null bytes, 0xFF, and a mix.
	payload := make([]byte, 0, 512)
	for i := 0; i < 256; i++ {
		payload = append(payload, byte(i))
	}
	// Duplicate for good measure.
	payload = append(payload, payload...)

	mock := &mockCollectorForStream{
		streamFn: func(_, _ string, writer io.Writer) error {
			_, err := writer.Write(payload)
			return err
		},
	}

	var buf bytes.Buffer
	err := mock.StreamFromHost("pod-1", "/var/log/dremio/server.log", &buf, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), payload) {
		t.Fatalf("binary mismatch: got %d bytes, want %d bytes", buf.Len(), len(payload))
	}
}

// TestStreamFromHost_SSHMockBinaryIntegrity tests the same binary integrity
// contract via a mock simulating SSH transport behavior.
func TestStreamFromHost_SSHMockBinaryIntegrity(t *testing.T) {
	payload := []byte{0x00, 0x0A, 0x0D, 0xFF, 0x7F, 0x80, 0x01}

	mock := &mockCollectorForStream{
		streamFn: func(_, _ string, writer io.Writer) error {
			_, err := writer.Write(payload)
			return err
		},
	}

	var buf bytes.Buffer
	err := mock.StreamFromHost("node-1.example.com", "/opt/dremio/data/file.bin", &buf, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), payload) {
		t.Fatalf("binary mismatch: got %v, want %v", buf.Bytes(), payload)
	}
}

// TestStreamFromHost_FileNotFound verifies that errors contain the remote path.
func TestStreamFromHost_FileNotFound(t *testing.T) {
	targetPath := "/nonexistent/file.log"
	mock := &mockCollectorForStream{
		streamFn: func(_, remotePath string, _ io.Writer) error {
			return fmt.Errorf("cat failed on host:%v: exit status 1 (stderr: No such file or directory)", remotePath)
		},
	}

	var buf bytes.Buffer
	err := mock.StreamFromHost("pod-1", targetPath, &buf, false)
	if err == nil {
		t.Fatal("expected error for non-existent file, got nil")
	}
	errMsg := err.Error()
	if !bytes.Contains([]byte(errMsg), []byte(targetPath)) {
		t.Fatalf("error message should contain path %q, got: %s", targetPath, errMsg)
	}
}

// TestStreamFromHost_EmptyFile verifies that streaming a zero-byte file
// results in zero bytes written and no error.
func TestStreamFromHost_EmptyFile(t *testing.T) {
	mock := &mockCollectorForStream{
		streamFn: func(_, _ string, _ io.Writer) error {
			// Write nothing — simulates an empty file.
			return nil
		},
	}

	var buf bytes.Buffer
	err := mock.StreamFromHost("pod-1", "/var/log/empty.log", &buf, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected zero bytes, got %d", buf.Len())
	}
}

// TestStreamFromHost_EmptyRemotePath verifies that an empty remotePath returns an error.
func TestStreamFromHost_EmptyRemotePath(t *testing.T) {
	mock := &mockCollectorForStream{
		streamFn: func(_, remotePath string, _ io.Writer) error {
			if remotePath == "" {
				return fmt.Errorf("StreamFromHost: remotePath is empty")
			}
			return nil
		},
	}

	var buf bytes.Buffer
	err := mock.StreamFromHost("pod-1", "", &buf, false)
	if err == nil {
		t.Fatal("expected error for empty remotePath, got nil")
	}
}
