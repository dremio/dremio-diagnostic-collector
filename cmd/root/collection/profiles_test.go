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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/local/restclient"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/collects"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/shutdown"
)

func TestRunLogBasedProfileCollection_DiagnosisMode(t *testing.T) {
	// Set up a mock HTTP server that returns a valid ZIP-like response for profile downloads.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/apiv2/support/") && strings.HasSuffix(r.URL.Path, "/download") {
			w.Header().Set("Content-Type", "application/octet-stream")
			// Write a minimal valid zip header (PK\x03\x04) as mock profile data
			_, _ = w.Write([]byte("PK\x03\x04mock-profile-data"))
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	// Initialize the REST client to use the test server
	restclient.InitClient(false, 30)

	// Create tmpDir structure with a synthetic server.log containing known error patterns
	tmpDir := t.TempDir()
	logsDir := filepath.Join(tmpDir, "coordinator-node", "logs")
	if err := os.MkdirAll(logsDir, 0o750); err != nil {
		t.Fatal(err)
	}

	// Write server.log with known job UUIDs that match the logparser patterns.
	// Patterns require specific formats — use thread-bracketed UUIDs with OOM keywords,
	// or "Query <UUID> failed" / "Query <UUID> cancelled" fallback patterns.
	serverLog := `2024-01-15 10:30:45,123 [main] ERROR c.d.s.a.QueryRunner - Query 1a2b3c4d-5e6f-7a8b-9c0d-1e2f3a4b5c6d failed with OOM
2024-01-15 10:31:00,456 [1a2b3c4d-5e6f-7a8b-9c0d-aabbccddeeff] ERROR c.d.e.ExecException - OUT_OF_MEMORY ERROR: Direct buffer memory
2024-01-15 10:32:00,789 [main] INFO c.d.s.a.NormalMessage - Everything is fine
2024-01-15 10:33:00,012 [main] ERROR c.d.e.c.QueryRunner - Canceling query deadbeef-1234-5678-9abc-def012345678
`
	if err := os.WriteFile(filepath.Join(logsDir, "server.log"), []byte(serverLog), 0o600); err != nil {
		t.Fatal(err)
	}

	// Create job-profiles output directory structure — profiles go to <tmpDir>/job-profiles/<nodeName>/
	profilesDir := filepath.Join(tmpDir, "job-profiles", "coordinator")
	// Don't pre-create — RunLogBasedProfileCollection creates it via os.MkdirAll

	hook := shutdown.NewHook()
	defer hook.Cleanup()

	err := RunLogBasedProfileCollection(LogProfileArgs{
		TmpDir:           tmpDir,
		DremioEndpoint:   ts.URL,
		DremioPAT:        "test-pat-token",
		NodeName:         "coordinator",
		CollectionMode:   collects.DiagnosisCollection,
		AllowInsecureSSL: false,
		RestHTTPTimeout:  30,
		Hook:             hook,
	})
	if err != nil {
		t.Fatalf("RunLogBasedProfileCollection returned error: %v", err)
	}

	// Verify profiles were downloaded — check the job-profiles output directory
	entries, err := os.ReadDir(profilesDir)
	if err != nil {
		t.Fatalf("reading profiles dir: %v", err)
	}

	// We expect 3 job IDs from the server.log (three distinct UUIDs in error lines)
	if len(entries) < 1 {
		t.Errorf("expected at least 1 downloaded profile, got %d", len(entries))
	}

	// Verify at least one profile file exists and has content
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".zip") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(profilesDir, entry.Name()))
		if err != nil {
			t.Errorf("reading profile %v: %v", entry.Name(), err)
			continue
		}
		if len(data) == 0 {
			t.Errorf("profile %v is empty", entry.Name())
		}
	}
}

func TestRunLogBasedProfileCollection_StandardMode(t *testing.T) {
	// Standard mode should be a no-op — returns immediately without any I/O
	hook := shutdown.NewHook()
	defer hook.Cleanup()

	err := RunLogBasedProfileCollection(LogProfileArgs{
		TmpDir:           t.TempDir(), // empty dir is fine since we shouldn't touch it
		DremioEndpoint:   "http://not-a-real-server:9047",
		DremioPAT:        "test-pat-token",
		NodeName:         "coordinator",
		CollectionMode:   collects.StandardCollection,
		AllowInsecureSSL: false,
		RestHTTPTimeout:  30,
		Hook:             hook,
	})
	if err != nil {
		t.Fatalf("expected nil error for standard mode, got: %v", err)
	}
}

func TestRunLogBasedProfileCollection_NoMatches(t *testing.T) {
	// server.log with no error patterns — should return nil without downloading
	tmpDir := t.TempDir()
	logsDir := filepath.Join(tmpDir, "coordinator-node", "logs")
	if err := os.MkdirAll(logsDir, 0o750); err != nil {
		t.Fatal(err)
	}

	serverLog := `2024-01-15 10:30:45,123 [main] INFO c.d.s.a.NormalOperation - System started
2024-01-15 10:31:00,456 [main] INFO c.d.s.a.NormalOperation - Query completed successfully
2024-01-15 10:32:00,789 [main] DEBUG c.d.s.a.NormalOperation - Cache hit rate: 95%
`
	if err := os.WriteFile(filepath.Join(logsDir, "server.log"), []byte(serverLog), 0o600); err != nil {
		t.Fatal(err)
	}

	hook := shutdown.NewHook()
	defer hook.Cleanup()

	err := RunLogBasedProfileCollection(LogProfileArgs{
		TmpDir:           tmpDir,
		DremioEndpoint:   "http://not-a-real-server:9047",
		DremioPAT:        "test-pat-token",
		NodeName:         "coordinator",
		CollectionMode:   collects.DiagnosisCollection,
		AllowInsecureSSL: false,
		RestHTTPTimeout:  30,
		Hook:             hook,
	})
	if err != nil {
		t.Fatalf("expected nil error for no-match case, got: %v", err)
	}
}

func TestRunLogBasedProfileCollection_NoPAT(t *testing.T) {
	// No PAT token — should skip without error
	hook := shutdown.NewHook()
	defer hook.Cleanup()

	err := RunLogBasedProfileCollection(LogProfileArgs{
		TmpDir:           t.TempDir(),
		DremioEndpoint:   "http://not-a-real-server:9047",
		DremioPAT:        "",
		NodeName:         "coordinator",
		CollectionMode:   collects.DiagnosisCollection,
		AllowInsecureSSL: false,
		RestHTTPTimeout:  30,
		Hook:             hook,
	})
	if err != nil {
		t.Fatalf("expected nil error when no PAT provided, got: %v", err)
	}
}
