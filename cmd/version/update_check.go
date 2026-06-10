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

// cmd package contains all the command line flag and initialization logic for commands
package cmd

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	githubReleasesURL = "https://api.github.com/repos/dremio/dremio-diagnostic-collector/releases/latest"
	cacheTTL          = 24 * time.Hour
	httpTimeout       = 3 * time.Second
	cacheFileName     = "version-check-cache.json"
	ddcConfigDir      = ".ddc"
)

// UpdateInfo holds the result of a version update check.
type UpdateInfo struct {
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version"`
	ReleaseURL     string `json:"release_url"`
	IsNewer        bool   `json:"is_newer"`
}

// versionCache represents the on-disk cache structure.
type versionCache struct {
	LatestVersion string    `json:"latest_version"`
	ReleaseURL    string    `json:"release_url"`
	CheckedAt     time.Time `json:"checked_at"`
}

// githubRelease is the subset of the GitHub API response we care about.
type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

// CheckForUpdate checks whether a newer version of DDC is available.
// It uses a local cache with a 24-hour TTL to avoid excessive GitHub API calls.
func CheckForUpdate(currentVersion string) (*UpdateInfo, error) {
	cacheDir, err := cacheDirectory()
	if err != nil {
		return nil, fmt.Errorf("unable to determine cache directory: %w", err)
	}
	cachePath := filepath.Join(cacheDir, cacheFileName)

	// Try to load a valid (non-expired) cache entry first.
	if cached, ok := loadCache(cachePath); ok {
		return buildUpdateInfo(currentVersion, cached.LatestVersion, cached.ReleaseURL), nil
	}

	// Cache is missing or stale; fetch from GitHub.
	latest, fetchErr := fetchLatestRelease()
	if fetchErr != nil {
		// If GitHub is unreachable, try to return stale cache without warning the user.
		if stale, ok := loadStaleCache(cachePath); ok {
			log.Printf("DEBUG: GitHub unreachable, using stale cache: %v", fetchErr)
			return buildUpdateInfo(currentVersion, stale.LatestVersion, stale.ReleaseURL), nil
		}
		return nil, fmt.Errorf("unable to check for updates: %w", fetchErr)
	}

	// Save the fresh result to cache.
	cache := &versionCache{
		LatestVersion: latest.TagName,
		ReleaseURL:    latest.HTMLURL,
		CheckedAt:     time.Now(),
	}
	if writeErr := saveCache(cachePath, cache); writeErr != nil {
		log.Printf("DEBUG: failed to write version cache: %v", writeErr)
	}

	return buildUpdateInfo(currentVersion, latest.TagName, latest.HTMLURL), nil
}

// buildUpdateInfo constructs an UpdateInfo by comparing the current and latest versions.
func buildUpdateInfo(currentVersion, latestVersion, releaseURL string) *UpdateInfo {
	return &UpdateInfo{
		CurrentVersion: currentVersion,
		LatestVersion:  latestVersion,
		ReleaseURL:     releaseURL,
		IsNewer:        isNewerVersion(latestVersion, currentVersion),
	}
}

// fetchLatestRelease calls the GitHub releases API with a 3-second timeout.
func fetchLatestRelease() (*githubRelease, error) {
	client := &http.Client{Timeout: httpTimeout}
	req, err := http.NewRequest(http.MethodGet, githubReleasesURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to decode GitHub response: %w", err)
	}
	return &release, nil
}

// cacheDirectory returns the path to ~/.ddc/, creating it if necessary.
func cacheDirectory() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ddcConfigDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	return dir, nil
}

// loadCache loads the cache file and returns it only if it is within the TTL.
func loadCache(path string) (*versionCache, bool) {
	cache, err := readCacheFile(path)
	if err != nil {
		return nil, false
	}
	if time.Since(cache.CheckedAt) > cacheTTL {
		return nil, false
	}
	return cache, true
}

// loadStaleCache loads the cache file regardless of TTL expiry.
func loadStaleCache(path string) (*versionCache, bool) {
	cache, err := readCacheFile(path)
	if err != nil {
		return nil, false
	}
	return cache, true
}

// readCacheFile reads and parses the cache JSON file.
func readCacheFile(path string) (*versionCache, error) {
	data, err := os.ReadFile(filepath.Clean(path)) // #nosec G304 -- path is DDC's own cache file
	if err != nil {
		return nil, err
	}
	var cache versionCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}
	return &cache, nil
}

// saveCache writes the cache to disk, creating the parent directory if needed.
func saveCache(path string, cache *versionCache) error {
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// isNewerVersion returns true if latest is semantically greater than current.
// Both versions may optionally have a leading "v" prefix.
func isNewerVersion(latest, current string) bool {
	latestParts := parseVersion(latest)
	currentParts := parseVersion(current)
	if latestParts == nil || currentParts == nil {
		return false
	}
	for i := 0; i < 3; i++ {
		if latestParts[i] > currentParts[i] {
			return true
		}
		if latestParts[i] < currentParts[i] {
			return false
		}
	}
	return false
}

// parseVersion strips a leading "v", splits on ".", and converts up to 3
// segments into integers. Returns nil if parsing fails.
func parseVersion(v string) []int {
	v = strings.TrimPrefix(v, "v")
	// Strip any pre-release suffix (e.g., "-rc.1").
	if idx := strings.Index(v, "-"); idx != -1 {
		v = v[:idx]
	}
	parts := strings.Split(v, ".")
	result := make([]int, 3)
	for i := 0; i < 3 && i < len(parts); i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return nil
		}
		result[i] = n
	}
	return result
}
