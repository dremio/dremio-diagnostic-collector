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

// Package logparser scans Dremio server.log files for job IDs associated with
// failures, OOM events, and cancellations. It uses streaming line-by-line I/O
// so it can handle 100 GB+ log files without loading them into memory.
package logparser

import (
	"bufio"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Match records a single job ID occurrence from a log line.
type Match struct {
	JobID      string // lowercase UUID
	Source     string // pattern name that first matched
	LineNumber int64
	Timestamp  string // leading timestamp extracted from the line, or empty
}

// Result holds the accumulated output of a scan.
type Result struct {
	Matches           map[string]*Match // jobID → first match
	Exclusions        map[string]bool   // job IDs to exclude (e.g. globally cancelled queries)
	TotalLinesScanned int64
}

// JobIDs returns a sorted, deduplicated list of job IDs found,
// excluding any IDs in the Exclusions set.
func (r *Result) JobIDs() []string {
	ids := make([]string, 0, len(r.Matches))
	for id := range r.Matches {
		if r.Exclusions[id] {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// MostRecent returns up to limit job IDs sorted by timestamp descending
// (most recent first), excluding any IDs in the Exclusions set.
// If limit <= 0, all non-excluded IDs are returned.
func (r *Result) MostRecent(limit int) []string {
	// Collect non-excluded matches.
	type entry struct {
		id string
		ts string
	}
	entries := make([]entry, 0, len(r.Matches))
	for id, m := range r.Matches {
		if r.Exclusions[id] {
			continue
		}
		entries = append(entries, entry{id: id, ts: m.Timestamp})
	}

	// Sort by timestamp descending (most recent first), then by ID for stability.
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].ts != entries[j].ts {
			return entries[i].ts > entries[j].ts
		}
		return entries[i].id < entries[j].id
	})

	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}

	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.id
	}
	return ids
}

// Scanner holds compiled patterns and can scan readers/files for job IDs.
type Scanner struct {
	patterns []Pattern
}

// NewScanner compiles all patterns and returns a ready-to-use Scanner.
func NewScanner() *Scanner {
	return &Scanner{
		patterns: buildPatterns(),
	}
}

// ScanReader reads from r line-by-line, applying patterns and extracting
// job IDs. It never loads the full content into memory.
func (s *Scanner) ScanReader(r io.Reader) (*Result, error) {
	result := &Result{
		Matches:    make(map[string]*Match),
		Exclusions: make(map[string]bool),
	}

	scanner := bufio.NewScanner(r)
	var lineNum int64

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		for _, p := range s.patterns {
			matches := p.Regex.FindStringSubmatch(line)
			if matches == nil {
				continue
			}

			ids := extractJobIDsFromMatches(matches)
			if len(ids) == 0 {
				continue
			}

			if p.Exclude {
				for _, id := range ids {
					result.Exclusions[id] = true
				}
				continue
			}

			ts := extractTimestamp(line)

			for _, id := range ids {
				if _, exists := result.Matches[id]; !exists {
					result.Matches[id] = &Match{
						JobID:      id,
						Source:     p.Name,
						LineNumber: lineNum,
						Timestamp:  ts,
					}
				}
			}
			// A line can match multiple patterns, but we already extracted
			// all UUIDs — no need to re-extract for subsequent pattern matches
			// on the same line. However, we do want to record the first pattern
			// that surfaced each UUID, so we continue the pattern loop.
		}
	}

	result.TotalLinesScanned = lineNum
	if err := scanner.Err(); err != nil {
		return result, err
	}
	return result, nil
}

// ScanFile opens a file and delegates to ScanReader. Files ending in .gz
// are transparently decompressed.
func (s *Scanner) ScanFile(path string) (*Result, error) {
	f, err := os.Open(filepath.Clean(path)) // #nosec G304 -- path is from internal log directory walker
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck // read-only file; close error is non-fatal

	var reader io.Reader = f

	if strings.EqualFold(filepath.Ext(path), ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		defer gz.Close() //nolint:errcheck // read-only gzip reader; close error is non-fatal
		reader = gz
	}

	return s.ScanReader(reader)
}

// extractTimestamp pulls a leading ISO-8601 or logback-style timestamp from
// a log line. Returns empty string if none found.
func extractTimestamp(line string) string {
	// Dremio 26.x: 2024-01-15T10:30:45.123Z or 2024-01-15T10:30:45.123+00:00
	// Dremio 24.x: 2024-01-15 10:30:45,123
	// Both start with a YYYY-MM-DD prefix.
	if len(line) < 19 {
		return ""
	}
	// Quick check: must start with digit and have dashes at positions 4 and 7.
	if line[4] != '-' || line[7] != '-' {
		return ""
	}

	// Find the end of the timestamp region. We look for the first space or tab
	// after the date portion that isn't part of the timestamp.
	// For logback format "2024-01-15 10:30:45,123 ..." the timestamp ends at
	// the second space (after the time). For ISO "2024-01-15T10:30:45.123Z ..."
	// it ends at the first space.
	if len(line) >= 23 && line[10] == 'T' {
		// ISO-8601: find end — could be Z, +offset, or space
		end := 23 // minimum: YYYY-MM-DDTHH:MM:SS.sss
		for end < len(line) && line[end] != ' ' && line[end] != '\t' {
			end++
		}
		return line[:end]
	}
	if len(line) >= 23 && line[10] == ' ' && line[13] == ':' {
		// Logback: "YYYY-MM-DD HH:MM:SS,mmm"
		end := 23
		for end < len(line) && line[end] != ' ' && line[end] != '\t' {
			end++
		}
		return line[:end]
	}
	return ""
}
