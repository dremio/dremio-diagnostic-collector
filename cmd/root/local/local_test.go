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

package local

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	osexec "os/exec"
	"path/filepath"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/collection"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/shutdown"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLocalCollectorImplementsCollector is a compile-time check that
// LocalCollector satisfies the Collector interface.
func TestLocalCollectorImplementsCollector(t *testing.T) {
	// This test exists to document the compile-time check at package level.
	// If LocalCollector doesn't implement Collector, the package won't compile.
	var _ collection.Collector = (*LocalCollector)(nil)
}

func TestRoleDetection(t *testing.T) {
	tests := []struct {
		name           string
		confContent    string
		wantCoord      bool
		description    string
		createConfFile bool // false = unreadable/missing path
	}{
		{
			name:           "coordinator conf (coord=true, exec=false)",
			confContent:    `services { coordinator.enabled: true, executor.enabled: false }`,
			wantCoord:      true,
			description:    "explicit coordinator",
			createConfFile: true,
		},
		{
			name:           "executor conf (coord=false, exec=true)",
			confContent:    `services { coordinator.enabled: false, executor.enabled: true }`,
			wantCoord:      false,
			description:    "explicit executor",
			createConfFile: true,
		},
		{
			name:           "both true conf",
			confContent:    `services { coordinator.enabled: true, executor.enabled: true }`,
			wantCoord:      true,
			description:    "both true defaults to coordinator",
			createConfFile: true,
		},
		{
			name:           "missing keys conf",
			confContent:    `paths { local: "/tmp/dremio" }`,
			wantCoord:      true,
			description:    "no role keys defaults to coordinator",
			createConfFile: true,
		},
		{
			name:           "unreadable conf path",
			confContent:    "",
			wantCoord:      true,
			description:    "unreadable file defaults to coordinator",
			createConfFile: false,
		},
		{
			name:           "only coordinator enabled set to false",
			confContent:    `services { coordinator.enabled: false }`,
			wantCoord:      true,
			description:    "coord=false without exec=true defaults to coordinator",
			createConfFile: true,
		},
		{
			name:           "only executor enabled set to true",
			confContent:    `services { executor.enabled: true }`,
			wantCoord:      false,
			description:    "exec=true without coord key → executor (coord defaults false-ish)",
			createConfFile: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var confPath string
			if tc.createConfFile {
				tmpDir := t.TempDir()
				confPath = filepath.Join(tmpDir, "dremio.conf")
				err := os.WriteFile(confPath, []byte(tc.confContent), 0o600)
				require.NoError(t, err)
			} else {
				confPath = "/nonexistent/path/dremio.conf"
			}

			got := detectRole(confPath, "/opt/dremio")
			assert.Equal(t, tc.wantCoord, got, tc.description)
		})
	}
}

func TestStreamFromHostPlain(t *testing.T) {
	tmpDir := t.TempDir()
	content := []byte("hello world\nthis is test data\n")
	srcPath := filepath.Join(tmpDir, "testfile.txt")
	require.NoError(t, os.WriteFile(srcPath, content, 0o600))

	hook := shutdown.NewHook()
	lc := NewLocalCollector(hook, "/nonexistent/dremio.conf", "/opt/dremio")

	var buf bytes.Buffer
	err := lc.StreamFromHost("", srcPath, &buf, false)
	require.NoError(t, err)
	assert.Equal(t, content, buf.Bytes())
}

func TestStreamFromHostGzip(t *testing.T) {
	// Skip if gzip is not available on PATH
	if _, err := osexec.LookPath("gzip"); err != nil {
		t.Skip("gzip not on PATH, skipping gzip streaming test")
	}

	tmpDir := t.TempDir()
	content := []byte("hello world\nthis is gzip test data\nwith multiple lines\n")
	srcPath := filepath.Join(tmpDir, "testfile.txt")
	require.NoError(t, os.WriteFile(srcPath, content, 0o600))

	hook := shutdown.NewHook()
	lc := NewLocalCollector(hook, "/nonexistent/dremio.conf", "/opt/dremio")

	var buf bytes.Buffer
	err := lc.StreamFromHost("", srcPath, &buf, true)
	require.NoError(t, err)

	// Decompress and verify
	gz, err := gzip.NewReader(&buf)
	require.NoError(t, err)
	defer gz.Close()

	decompressed, err := io.ReadAll(gz)
	require.NoError(t, err)
	assert.Equal(t, content, decompressed)
}

func TestStreamFromHostEmptyPath(t *testing.T) {
	hook := shutdown.NewHook()
	lc := NewLocalCollector(hook, "/nonexistent/dremio.conf", "/opt/dremio")

	var buf bytes.Buffer
	err := lc.StreamFromHost("", "", &buf, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "remotePath is empty")
}

func TestGetCoordinatorsExecutors(t *testing.T) {
	hostname, err := os.Hostname()
	require.NoError(t, err)

	t.Run("coordinator role returns hostname in coordinators", func(t *testing.T) {
		tmpDir := t.TempDir()
		confPath := filepath.Join(tmpDir, "dremio.conf")
		require.NoError(t, os.WriteFile(confPath, []byte(`services { coordinator.enabled: true, executor.enabled: false }`), 0o600))

		hook := shutdown.NewHook()
		lc := NewLocalCollector(hook, confPath, "/opt/dremio")

		coords, err := lc.GetCoordinators()
		require.NoError(t, err)
		assert.Equal(t, []string{hostname}, coords)

		execs, err := lc.GetExecutors()
		require.NoError(t, err)
		assert.Empty(t, execs)
	})

	t.Run("executor role returns hostname in executors", func(t *testing.T) {
		tmpDir := t.TempDir()
		confPath := filepath.Join(tmpDir, "dremio.conf")
		require.NoError(t, os.WriteFile(confPath, []byte(`services { coordinator.enabled: false, executor.enabled: true }`), 0o600))

		hook := shutdown.NewHook()
		lc := NewLocalCollector(hook, confPath, "/opt/dremio")

		coords, err := lc.GetCoordinators()
		require.NoError(t, err)
		assert.Empty(t, coords)

		execs, err := lc.GetExecutors()
		require.NoError(t, err)
		assert.Equal(t, []string{hostname}, execs)
	})
}
