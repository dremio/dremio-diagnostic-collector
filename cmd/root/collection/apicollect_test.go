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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/local/restclient"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/clusterstats"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/shutdown"
)

func TestRunCollectKVStore(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/apiv2/kvstore/report" {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte("fake-zip-content"))
		} else {
			http.Error(w, "Not Found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	restclient.InitClient(false, 30)
	hook := shutdown.NewHook()
	defer hook.Cleanup()

	tmpDir := t.TempDir()
	args := APICollectionArgs{
		TmpDir:           tmpDir,
		CoordinatorNode:  "dremio-master-0",
		DremioEndpoint:   server.URL,
		DremioPAT:        "test-token",
		AllowInsecureSSL: false,
		RestHTTPTimeout:  30,
		Hook:             hook,
	}

	err := RunCollectKVStore(args)
	if err != nil {
		t.Fatalf("RunCollectKVStore failed: %v", err)
	}

	kvFile := filepath.Join(tmpDir, "kvstore", "dremio-master-0", "kvstore-report.zip")
	data, err := os.ReadFile(kvFile)
	if err != nil {
		t.Fatalf("expected kvstore-report.zip to exist: %v", err)
	}
	if string(data) != "fake-zip-content" {
		t.Errorf("unexpected content: %v", string(data))
	}
}

func TestRunCollectKVStore_NoPAT(t *testing.T) {
	hook := shutdown.NewHook()
	defer hook.Cleanup()

	args := APICollectionArgs{
		TmpDir:          t.TempDir(),
		CoordinatorNode: "dremio-master-0",
		DremioPAT:       "",
		Hook:            hook,
	}

	err := RunCollectKVStore(args)
	if err != nil {
		t.Fatalf("expected no error when PAT is empty, got: %v", err)
	}
}

func TestFindClusterIDFromOrchestrator(t *testing.T) {
	tmpDir := t.TempDir()
	nodeName := "dremio-master-0"

	// Create the same directory structure the orchestrator produces.
	outDir := filepath.Join(tmpDir, "cluster-stats", nodeName)
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		t.Fatalf("failed to create test dir: %v", err)
	}

	stats := clusterstats.ClusterStats{
		ClusterID:     "test-cluster-123",
		DremioVersion: "",
		NodeName:      nodeName,
	}
	data, err := json.Marshal(stats)
	if err != nil {
		t.Fatalf("failed to marshal stats: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "cluster-stats.json"), data, 0o600); err != nil {
		t.Fatalf("failed to write cluster-stats.json: %v", err)
	}

	// FindClusterID walks the output dir looking for cluster-stats.json files.
	results, err := FindClusterID(tmpDir)
	if err != nil {
		t.Fatalf("FindClusterID failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ClusterID != "test-cluster-123" {
		t.Errorf("expected clusterID test-cluster-123, got %v", results[0].ClusterID)
	}
	if results[0].NodeName != nodeName {
		t.Errorf("expected nodeName %v, got %v", nodeName, results[0].NodeName)
	}
}
