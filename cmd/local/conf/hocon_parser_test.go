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

package conf

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHOCONParser(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "hocon-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a test dremio.conf file
	dremioConfContent := `
paths: {
  # the local path for dremio to store data.
  local: ${DREMIO_HOME}"/data"

  # the distributed path Dremio data including job results, downloads, uploads, etc
  dist: "pdfs://"${paths.local}"/pdfs"
}

# custom db path
db: "/opt/dremio/data/custom-db"

services: {
  coordinator.enabled: true,
  coordinator.master.enabled: true,
  executor.enabled: false,
  flight.use_session_service: true
}

debug: {
    addDefaultUser: true
}
`
	dremioConfPath := filepath.Join(tmpDir, "dremio.conf")
	err = os.WriteFile(dremioConfPath, []byte(dremioConfContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test dremio.conf: %v", err)
	}

	// Test parsing the file
	dremioHome := "/opt/dremio"
	config, err := ParseDremioConf(dremioConfPath, dremioHome)
	if err != nil {
		t.Fatalf("Failed to parse dremio.conf: %v", err)
	}

	// Test GetRocksDBPath
	expectedDBPath := "/opt/dremio/data/custom-db"
	dbPath := config.GetRocksDBPath(dremioHome)
	if dbPath != expectedDBPath {
		t.Errorf("Expected RocksDB path %s, got %s", expectedDBPath, dbPath)
	}

	// Test GetString
	expectedLocalPath := "/opt/dremio/data"
	localPath := config.GetString("paths.local")
	if localPath != expectedLocalPath {
		t.Errorf("Expected local path %s, got %s", expectedLocalPath, localPath)
	}

	// Test GetBool
	if !config.GetBool("debug.addDefaultUser") {
		t.Errorf("Expected debug.addDefaultUser to be true, got false")
	}

	// Test HasPath
	if !config.HasPath("services.coordinator.enabled") {
		t.Errorf("Expected HasPath(services.coordinator.enabled) to be true, got false")
	}
	if config.HasPath("nonexistent.path") {
		t.Errorf("Expected HasPath(nonexistent.path) to be false, got true")
	}
}

func TestHOCONParserWithDefaultValues(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "hocon-test-defaults")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a minimal dremio.conf file without many settings
	dremioConfContent := `
paths: {
  # the local path for dremio to store data.
  local: ${DREMIO_HOME}"/data"
}
`
	dremioConfPath := filepath.Join(tmpDir, "dremio.conf")
	err = os.WriteFile(dremioConfPath, []byte(dremioConfContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test dremio.conf: %v", err)
	}

	// Test parsing the file
	dremioHome := "/opt/dremio"
	config, err := ParseDremioConf(dremioConfPath, dremioHome)
	if err != nil {
		t.Fatalf("Failed to parse dremio.conf: %v", err)
	}

	// Test GetRocksDBPath with default value
	expectedDBPath := "/opt/dremio/data/db"
	dbPath := config.GetRocksDBPath(dremioHome)
	if dbPath != expectedDBPath {
		t.Errorf("Expected default RocksDB path %s, got %s", expectedDBPath, dbPath)
	}

}

func TestNewDremioHOCONConfigFromString_WithPathsDB(t *testing.T) {
	content := `
paths: {
  local: "/data/dremio"
  db: "/data/dremio/rocksdb"
}
`
	config, err := NewDremioHOCONConfigFromString(content, "/opt/dremio")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := config.GetRocksDBPath("/opt/dremio")
	if got != "/data/dremio/rocksdb" {
		t.Errorf("expected /data/dremio/rocksdb, got %s", got)
	}
}

func TestNewDremioHOCONConfigFromString_WithDremioHomePlaceholder(t *testing.T) {
	content := `
paths: {
  local: ${DREMIO_HOME}"/data"
}
`
	config, err := NewDremioHOCONConfigFromString(content, "/opt/dremio")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := config.GetRocksDBPath("/opt/dremio")
	if got != "/opt/dremio/data/db" {
		t.Errorf("expected /opt/dremio/data/db, got %s", got)
	}
}

func TestNewDremioHOCONConfigFromString_InvalidContent(t *testing.T) {
	_, err := NewDremioHOCONConfigFromString("{{invalid hocon", "/opt/dremio")
	if err == nil {
		t.Fatal("expected error for invalid HOCON content")
	}
}

func TestNewDremioHOCONConfigFromString_EmptyContent(t *testing.T) {
	config, err := NewDremioHOCONConfigFromString("", "/opt/dremio")
	if err != nil {
		t.Fatalf("unexpected error for empty content: %v", err)
	}
	// With no paths configured, GetRocksDBPath falls back to default
	got := config.GetRocksDBPath("/opt/dremio")
	if got != "/opt/dremio/data/db" {
		t.Errorf("expected /opt/dremio/data/db, got %s", got)
	}
}
