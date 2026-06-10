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

package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newTestServer returns an httptest.Server that responds with a fake GitHub release JSON.
func newTestServer(tagName, htmlURL string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := githubRelease{
			TagName: tagName,
			HTMLURL: htmlURL,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

// withTestGitHubURL temporarily overrides the GitHub releases URL for testing.
// It returns a cleanup function that restores the original URL.
func withTestCacheDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return dir
}

func TestCheckForUpdate_ParsesVersion(t *testing.T) {
	tests := []struct {
		name    string
		latest  string
		current string
		isNewer bool
	}{
		{"newer major", "v4.0.0", "v3.0.0", true},
		{"newer minor", "v3.2.0", "v3.1.0", true},
		{"newer patch", "v3.1.2", "v3.1.1", true},
		{"same version", "v3.1.0", "v3.1.0", false},
		{"older version", "v2.0.0", "v3.0.0", false},
		{"no v prefix", "4.0.0", "3.0.0", true},
		{"mixed prefix", "v4.0.0", "3.0.0", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			info := buildUpdateInfo(tc.current, tc.latest, "https://example.com")
			if info.IsNewer != tc.isNewer {
				t.Errorf("isNewer: got %v, want %v (latest=%s, current=%s)",
					info.IsNewer, tc.isNewer, tc.latest, tc.current)
			}
			if info.CurrentVersion != tc.current {
				t.Errorf("CurrentVersion: got %s, want %s", info.CurrentVersion, tc.current)
			}
			if info.LatestVersion != tc.latest {
				t.Errorf("LatestVersion: got %s, want %s", info.LatestVersion, tc.latest)
			}
		})
	}
}

func TestCheckForUpdate_CacheCreated(t *testing.T) {
	// Set up a fake GitHub server.
	server := newTestServer("v4.2.0", "https://github.com/dremio/dremio-diagnostic-collector/releases/tag/v4.2.0")
	defer server.Close()

	// Use a temp directory for cache.
	cacheDir := withTestCacheDir(t)
	cachePath := filepath.Join(cacheDir, cacheFileName)

	// Simulate what CheckForUpdate does: fetch from the test server, then save cache.
	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("failed to fetch from test server: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	cache := &versionCache{
		LatestVersion: release.TagName,
		ReleaseURL:    release.HTMLURL,
		CheckedAt:     time.Now(),
	}
	if err := saveCache(cachePath, cache); err != nil {
		t.Fatalf("failed to save cache: %v", err)
	}

	// Verify cache file exists and is valid.
	if _, err := os.Stat(cachePath); os.IsNotExist(err) {
		t.Fatal("cache file was not created")
	}

	loaded, ok := loadCache(cachePath)
	if !ok {
		t.Fatal("loadCache returned false for a freshly written cache")
	}
	if loaded.LatestVersion != "v4.2.0" {
		t.Errorf("cached LatestVersion: got %s, want v4.2.0", loaded.LatestVersion)
	}
}

func TestCheckForUpdate_CacheTTL(t *testing.T) {
	cacheDir := withTestCacheDir(t)
	cachePath := filepath.Join(cacheDir, cacheFileName)

	// Write a cache entry that is 25 hours old (past the 24h TTL).
	staleCache := &versionCache{
		LatestVersion: "v3.0.0",
		ReleaseURL:    "https://example.com/releases/v3.0.0",
		CheckedAt:     time.Now().Add(-25 * time.Hour),
	}
	if err := saveCache(cachePath, staleCache); err != nil {
		t.Fatalf("failed to save stale cache: %v", err)
	}

	// loadCache should reject the stale entry.
	if _, ok := loadCache(cachePath); ok {
		t.Error("loadCache should have rejected a cache entry older than 24 hours")
	}

	// loadStaleCache should still return it.
	if _, ok := loadStaleCache(cachePath); !ok {
		t.Error("loadStaleCache should have returned the stale cache entry")
	}

	// Write a fresh cache entry (1 hour old).
	freshCache := &versionCache{
		LatestVersion: "v4.0.0",
		ReleaseURL:    "https://example.com/releases/v4.0.0",
		CheckedAt:     time.Now().Add(-1 * time.Hour),
	}
	if err := saveCache(cachePath, freshCache); err != nil {
		t.Fatalf("failed to save fresh cache: %v", err)
	}

	// loadCache should accept the fresh entry.
	loaded, ok := loadCache(cachePath)
	if !ok {
		t.Fatal("loadCache should have accepted a cache entry within 24 hours")
	}
	if loaded.LatestVersion != "v4.0.0" {
		t.Errorf("cached LatestVersion: got %s, want v4.0.0", loaded.LatestVersion)
	}
}

func TestCheckForUpdate_SemanticComparison(t *testing.T) {
	tests := []struct {
		name    string
		latest  string
		current string
		isNewer bool
	}{
		{"v4.1.0 > v4.0.0", "v4.1.0", "v4.0.0", true},
		{"v4.0.0 = v4.0.0", "v4.0.0", "v4.0.0", false},
		{"v4.0.0 < v4.1.0", "v4.0.0", "v4.1.0", false},
		{"v10.0.0 > v9.0.0", "v10.0.0", "v9.0.0", true},
		{"v3.0.10 > v3.0.9", "v3.0.10", "v3.0.9", true},
		{"v3.0.9 < v3.0.10", "v3.0.9", "v3.0.10", false},
		{"pre-release stripped", "v4.1.0-rc.1", "v4.0.0", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isNewerVersion(tc.latest, tc.current)
			if got != tc.isNewer {
				t.Errorf("isNewerVersion(%q, %q): got %v, want %v",
					tc.latest, tc.current, got, tc.isNewer)
			}
		})
	}
}
