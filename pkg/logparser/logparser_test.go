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

package logparser

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- helper ---

func scanLines(t *testing.T, lines ...string) *Result {
	t.Helper()
	s := NewScanner()
	r := strings.NewReader(strings.Join(lines, "\n"))
	res, err := s.ScanReader(r)
	if err != nil {
		t.Fatalf("ScanReader error: %v", err)
	}
	return res
}

func requireJobIDs(t *testing.T, res *Result, want ...string) {
	t.Helper()
	got := res.JobIDs()
	if len(got) != len(want) {
		t.Fatalf("JobIDs count: got %d %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("JobIDs[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}

// --- OOM patterns ---

func TestOOMError_Logback24x(t *testing.T) {
	res := scanLines(t,
		`2024-03-15 10:30:45,123 [1a2b3c4d-1234-5678-9abc-def012345678] ERROR c.d.s.e.f.SomeClass - OUT_OF_MEMORY ERROR: Node ran out of memory`,
	)
	requireJobIDs(t, res, "1a2b3c4d-1234-5678-9abc-def012345678")
	m := res.Matches["1a2b3c4d-1234-5678-9abc-def012345678"]
	if m.Source != "oom_error" {
		t.Errorf("Source: got %q, want oom_error", m.Source)
	}
	if m.Timestamp != "2024-03-15 10:30:45,123" {
		t.Errorf("Timestamp: got %q", m.Timestamp)
	}
}

func TestOOMException_ISO26x(t *testing.T) {
	res := scanLines(t,
		`2025-01-10T14:22:33.456Z [aabbccdd-1111-2222-3333-444455556666:frag:0:0] ERROR c.d.exec - OutOfMemoryException while executing query`,
	)
	requireJobIDs(t, res, "aabbccdd-1111-2222-3333-444455556666")
	m := res.Matches["aabbccdd-1111-2222-3333-444455556666"]
	if m.Source != "oom_exception" {
		t.Errorf("Source: got %q, want oom_exception", m.Source)
	}
	if m.Timestamp != "2025-01-10T14:22:33.456Z" {
		t.Errorf("Timestamp: got %q", m.Timestamp)
	}
}

func TestOOMExceededMemory(t *testing.T) {
	res := scanLines(t,
		`2024-06-01 09:00:00,000 [11111111-2222-3333-4444-555566667777] WARN c.d.s.exec - Query exceeded memory limits for allocation`,
	)
	requireJobIDs(t, res, "11111111-2222-3333-4444-555566667777")
	if res.Matches["11111111-2222-3333-4444-555566667777"].Source != "oom_exceeded_memory" {
		t.Error("wrong source pattern")
	}
}

// --- Query failure patterns ---

func TestQueryFailed_Logback(t *testing.T) {
	res := scanLines(t,
		`2024-05-20 12:00:00,100 [aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee] ERROR c.d.e.f.QueryTracker - ERROR Query aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee failed: RESOURCE`,
	)
	requireJobIDs(t, res, "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	if res.Matches["aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"].Source != "query_failed_fallback" {
		t.Error("wrong source")
	}
}

func TestFragmentTrackerFailed_ISO(t *testing.T) {
	res := scanLines(t,
		`2025-03-01T08:15:30.789+00:00 [12345678-abcd-ef01-2345-6789abcdef01] ERROR c.d.sabot - FragmentTracker query 12345678-abcd-ef01-2345-6789abcdef01 failed with status FAILED`,
	)
	requireJobIDs(t, res, "12345678-abcd-ef01-2345-6789abcdef01")
}

func TestSystemError(t *testing.T) {
	res := scanLines(t,
		`2024-11-11 11:11:11,111 [abcdef01-2345-6789-abcd-ef0123456789] ERROR c.d.s - SYSTEM ERROR: UnexpectedFailure Fragment abcdef01-2345-6789-abcd-ef0123456789:1:0`,
	)
	requireJobIDs(t, res, "abcdef01-2345-6789-abcd-ef0123456789")
}

// --- Cancellation patterns ---

func TestQueryCancelled(t *testing.T) {
	res := scanLines(t,
		`2024-09-09 09:09:09,009 [deadbeef-dead-beef-dead-beefdeadbeef] INFO c.d.s - Query deadbeef-dead-beef-dead-beefdeadbeef cancelled by user`,
	)
	requireJobIDs(t, res, "deadbeef-dead-beef-dead-beefdeadbeef")
}

func TestQueryCanceled_US(t *testing.T) {
	res := scanLines(t,
		`2025-02-14T00:00:00.000Z [cafebabe-cafe-babe-cafe-babecafebabe] INFO - Query cafebabe-cafe-babe-cafe-babecafebabe was canceled`,
	)
	requireJobIDs(t, res, "cafebabe-cafe-babe-cafe-babecafebabe")
}

// --- Heap monitor ---

func TestHeapMonitorDump(t *testing.T) {
	res := scanLines(t,
		`2024-04-04 04:04:04,404 [feed1234-feed-1234-feed-1234feed1234] WARN c.d.exec.heap - HeapMonitorThread: dumping heap for query feed1234-feed-1234-feed-1234feed1234`,
	)
	requireJobIDs(t, res, "feed1234-feed-1234-feed-1234feed1234")
	if res.Matches["feed1234-feed-1234-feed-1234feed1234"].Source != "heap_monitor_dump" {
		t.Error("wrong source")
	}
}

// --- Planning failure ---

func TestMemoryAllocationUtilities(t *testing.T) {
	res := scanLines(t,
		`2025-06-15T22:10:00.500Z [baadf00d-baad-f00d-baad-f00dbaadf00d] ERROR c.d.s - MemoryAllocationUtilities failed to plan query baadf00d-baad-f00d-baad-f00dbaadf00d`,
	)
	requireJobIDs(t, res, "baadf00d-baad-f00d-baad-f00dbaadf00d")
	if res.Matches["baadf00d-baad-f00d-baad-f00dbaadf00d"].Source != "memory_allocation_util" {
		t.Error("wrong source")
	}
}

// --- UUID extraction variants ---

func TestUUID_ThreadName(t *testing.T) {
	// UUID in thread-name brackets
	res := scanLines(t,
		`2024-01-01 00:00:00,000 [aabb0011-2233-4455-6677-8899aabbccdd] ERROR - OUT_OF_MEMORY ERROR`,
	)
	requireJobIDs(t, res, "aabb0011-2233-4455-6677-8899aabbccdd")
}

func TestUUID_FragmentRef(t *testing.T) {
	// UUID in Fragment reference
	res := scanLines(t,
		`2024-01-01 00:00:00,000 ERROR c.d.s - SYSTEM ERROR Fragment 11223344-aabb-ccdd-eeff-001122334455:2:1 blew up`,
	)
	requireJobIDs(t, res, "11223344-aabb-ccdd-eeff-001122334455")
}

func TestUUID_MessageBody(t *testing.T) {
	// UUID only in message text, not in brackets or Fragment ref
	res := scanLines(t,
		`2024-01-01 00:00:00,000 ERROR - Query 99887766-5544-3322-1100-ffeeddccbbaa failed with OOM; OUT_OF_MEMORY ERROR`,
	)
	requireJobIDs(t, res, "99887766-5544-3322-1100-ffeeddccbbaa")
}

// --- Deduplication ---

func TestDeduplication(t *testing.T) {
	// Same UUID from two different pattern matches on different lines
	res := scanLines(t,
		`2024-01-01 00:00:00,000 [aaaabbbb-cccc-dddd-eeee-ffffffffffff] ERROR - OUT_OF_MEMORY ERROR`,
		`2024-01-01 00:00:01,000 [aaaabbbb-cccc-dddd-eeee-ffffffffffff] ERROR - Query aaaabbbb-cccc-dddd-eeee-ffffffffffff failed`,
	)
	requireJobIDs(t, res, "aaaabbbb-cccc-dddd-eeee-ffffffffffff")
	// First match wins — should be from line 1 (oom_error)
	m := res.Matches["aaaabbbb-cccc-dddd-eeee-ffffffffffff"]
	if m.LineNumber != 1 {
		t.Errorf("LineNumber: got %d, want 1", m.LineNumber)
	}
	if m.Source != "oom_error" {
		t.Errorf("Source: got %q, want oom_error", m.Source)
	}
}

// --- Empty input ---

func TestEmptyReader(t *testing.T) {
	res := scanLines(t)
	requireJobIDs(t, res)
	if res.TotalLinesScanned != 0 {
		t.Errorf("TotalLinesScanned: got %d, want 0", res.TotalLinesScanned)
	}
}

// --- Gzip reader ---

func TestScanFile_Gzip(t *testing.T) {
	content := `2024-07-07 07:07:07,777 [face1234-face-1234-face-1234face1234] ERROR - OUT_OF_MEMORY ERROR`

	// Write gzipped content to temp file
	dir := t.TempDir()
	gzPath := filepath.Join(dir, "server.log.gz")

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gzPath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewScanner()
	res, err := s.ScanFile(gzPath)
	if err != nil {
		t.Fatalf("ScanFile error: %v", err)
	}
	requireJobIDs(t, res, "face1234-face-1234-face-1234face1234")
}

// --- Line count ---

func TestTotalLinesScanned(t *testing.T) {
	res := scanLines(t,
		"line one with no matches",
		"line two with no matches",
		`2024-01-01 00:00:00,000 [aaaa1111-bbbb-cccc-dddd-eeeeeeee1111] ERROR - OUT_OF_MEMORY ERROR`,
		"line four with no matches",
	)
	if res.TotalLinesScanned != 4 {
		t.Errorf("TotalLinesScanned: got %d, want 4", res.TotalLinesScanned)
	}
	requireJobIDs(t, res, "aaaa1111-bbbb-cccc-dddd-eeeeeeee1111")
}

// --- ScanFile plain text ---

func TestScanFile_Plain(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "server.log")
	content := `2024-01-01 00:00:00,000 [bbbb2222-cccc-dddd-eeee-ffffffffffff] ERROR - OutOfMemoryException`
	if err := os.WriteFile(logPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewScanner()
	res, err := s.ScanFile(logPath)
	if err != nil {
		t.Fatalf("ScanFile error: %v", err)
	}
	requireJobIDs(t, res, "bbbb2222-cccc-dddd-eeee-ffffffffffff")
}

// --- Multiple UUIDs on one line ---

func TestMultipleUUIDsOneLine(t *testing.T) {
	// Line has two distinct UUIDs: one in thread name, one in Fragment ref
	res := scanLines(t,
		`2024-01-01 00:00:00,000 [11110000-2222-3333-4444-555566667777] ERROR - SYSTEM ERROR Fragment 88880000-9999-aaaa-bbbb-ccccddddeeee:0:0 blew up`,
	)
	ids := res.JobIDs()
	if len(ids) != 1 {
		t.Fatalf("expected 1 explicit ID extracted under strict rules, got %d: %v", len(ids), ids)
	}
	if ids[0] != "88880000-9999-aaaa-bbbb-ccccddddeeee" {
		t.Fatalf("expected Fragment ID 88880000-9999-aaaa-bbbb-ccccddddeeee, got: %v", ids[0])
	}
}

// --- Timestamp extraction ---

func TestTimestamp_ISO8601(t *testing.T) {
	res := scanLines(t,
		`2025-12-25T23:59:59.999+05:30 [aaa00000-1111-2222-3333-444455556666] ERROR - OUT_OF_MEMORY ERROR`,
	)
	m := res.Matches["aaa00000-1111-2222-3333-444455556666"]
	if m.Timestamp != "2025-12-25T23:59:59.999+05:30" {
		t.Errorf("Timestamp: got %q", m.Timestamp)
	}
}

func TestTimestamp_Logback(t *testing.T) {
	res := scanLines(t,
		`2024-06-15 08:30:00,123 [bbb00000-1111-2222-3333-444455556666] ERROR - OUT_OF_MEMORY ERROR`,
	)
	m := res.Matches["bbb00000-1111-2222-3333-444455556666"]
	if m.Timestamp != "2024-06-15 08:30:00,123" {
		t.Errorf("Timestamp: got %q", m.Timestamp)
	}
}

// ---------- File-based fixture tests ----------

// The four consistent UUIDs used across all version fixtures:
var fixtureJobIDs = []string{
	"0a1b2c3d-4e5f-6a7b-8c9d-0e1f2a3b4c5d",
	"1b2c3d4e-5f6a-7b8c-9d0e-1f2a3b4c5d6e",
	"2c3d4e5f-6a7b-8c9d-0e1f-2a3b4c5d6e7f",
	"3d4e5f6a-7b8c-9d0e-1f2a-3b4c5d6e7f80",
}

func requireContainsAll(t *testing.T, res *Result, wantIDs []string) {
	t.Helper()
	got := res.JobIDs()
	gotSet := make(map[string]bool, len(got))
	for _, id := range got {
		gotSet[id] = true
	}
	for _, id := range wantIDs {
		if !gotSet[id] {
			t.Errorf("missing expected job ID %s in results %v", id, got)
		}
	}
}

func TestScanFile_Dremio24x(t *testing.T) {
	s := NewScanner()
	res, err := s.ScanFile(filepath.Join("testdata", "dremio24x", "server.log"))
	if err != nil {
		t.Fatalf("ScanFile error: %v", err)
	}
	requireContainsAll(t, res, fixtureJobIDs)
	if len(res.Matches) != 4 {
		t.Errorf("expected exactly 4 unique job IDs, got %d: %v", len(res.Matches), res.JobIDs())
	}
	// Verify the first UUID was categorised from OOM (first match wins)
	m := res.Matches["0a1b2c3d-4e5f-6a7b-8c9d-0e1f2a3b4c5d"]
	if m == nil {
		t.Fatal("OOM job ID not found")
	}
	if m.Source != "oom_error" {
		t.Errorf("expected oom_error source, got %q", m.Source)
	}
	if m.Timestamp != "2023-02-14 12:30:15,750" {
		t.Errorf("unexpected timestamp: %q", m.Timestamp)
	}
}

func TestScanFile_Dremio25x(t *testing.T) {
	s := NewScanner()
	res, err := s.ScanFile(filepath.Join("testdata", "dremio25x", "server.log"))
	if err != nil {
		t.Fatalf("ScanFile error: %v", err)
	}
	requireContainsAll(t, res, fixtureJobIDs)
	if len(res.Matches) != 4 {
		t.Errorf("expected exactly 4 unique job IDs, got %d: %v", len(res.Matches), res.JobIDs())
	}
	// 25.x uses different thread pool names (fabric-rpc-pool, maestro-pool)
	// but the patterns should still match identically
	m := res.Matches["1b2c3d4e-5f6a-7b8c-9d0e-1f2a3b4c5d6e"]
	if m == nil {
		t.Fatal("query failure job ID not found")
	}
	// The FragmentTracker line also matches query_failed (which is checked first
	// in pattern order), so the source is query_failed per first-pattern-wins.
	if m.Source != "query_failed" {
		t.Errorf("expected query_failed source (first pattern match), got %q", m.Source)
	}
}

func TestScanFile_Dremio26x(t *testing.T) {
	s := NewScanner()
	res, err := s.ScanFile(filepath.Join("testdata", "dremio26x", "server.log"))
	if err != nil {
		t.Fatalf("ScanFile error: %v", err)
	}
	requireContainsAll(t, res, fixtureJobIDs)
	if len(res.Matches) != 4 {
		t.Errorf("expected exactly 4 unique job IDs, got %d: %v", len(res.Matches), res.JobIDs())
	}
	// Validate ISO-8601 timestamp parsing for 26.x format
	m := res.Matches["0a1b2c3d-4e5f-6a7b-8c9d-0e1f2a3b4c5d"]
	if m == nil {
		t.Fatal("OOM job ID not found")
	}
	if m.Timestamp != "2025-07-10T14:32:05.750+0000" {
		t.Errorf("ISO-8601 timestamp: got %q, want 2025-07-10T14:32:05.750+0000", m.Timestamp)
	}
}

func TestScanFile_HeapMonitor(t *testing.T) {
	s := NewScanner()
	res, err := s.ScanFile(filepath.Join("testdata", "heap_monitor", "server.log"))
	if err != nil {
		t.Fatalf("ScanFile error: %v", err)
	}
	// Two job IDs from heap monitor context lines
	wantIDs := []string{
		"3d4e5f6a-7b8c-9d0e-1f2a-3b4c5d6e7f80",
		"4e5f6a7b-8c9d-0e1f-2a3b-4c5d6e7f8091",
	}
	requireContainsAll(t, res, wantIDs)
	// Both should be sourced from heap_monitor_dump
	for _, id := range wantIDs {
		m := res.Matches[id]
		if m == nil {
			t.Errorf("missing job ID %s", id)
			continue
		}
		if m.Source != "heap_monitor_dump" {
			t.Errorf("job %s: expected heap_monitor_dump source, got %q", id, m.Source)
		}
	}
}

func TestScanFile_GzipFixture(t *testing.T) {
	// Create gzip from 24x fixture programmatically
	plainPath := filepath.Join("testdata", "dremio24x", "server.log")
	content, err := os.ReadFile(plainPath)
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	dir := t.TempDir()
	gzPath := filepath.Join(dir, "server.log.gz")

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gzPath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewScanner()
	res, err := s.ScanFile(gzPath)
	if err != nil {
		t.Fatalf("ScanFile gzip error: %v", err)
	}
	// Same results as plain text 24x
	requireContainsAll(t, res, fixtureJobIDs)
	if len(res.Matches) != 4 {
		t.Errorf("expected 4 unique job IDs from gzip, got %d: %v", len(res.Matches), res.JobIDs())
	}
}

func TestScanFile_Empty(t *testing.T) {
	s := NewScanner()
	res, err := s.ScanFile(filepath.Join("testdata", "empty", "server.log"))
	if err != nil {
		t.Fatalf("ScanFile error: %v", err)
	}
	if len(res.Matches) != 0 {
		t.Errorf("expected 0 matches from empty file, got %d", len(res.Matches))
	}
	if res.TotalLinesScanned != 0 {
		t.Errorf("expected 0 lines scanned, got %d", res.TotalLinesScanned)
	}
}

func TestScanFile_NoMatches(t *testing.T) {
	s := NewScanner()
	res, err := s.ScanFile(filepath.Join("testdata", "no_matches", "server.log"))
	if err != nil {
		t.Fatalf("ScanFile error: %v", err)
	}
	if len(res.Matches) != 0 {
		t.Errorf("expected 0 matches from no_matches file, got %d: %v", len(res.Matches), res.JobIDs())
	}
	if res.TotalLinesScanned == 0 {
		t.Error("expected some lines scanned in no_matches fixture")
	}
}

func TestScanFile_Deduplication(t *testing.T) {
	// 24x fixture has UUID 0a1b2c3d... in both oom_error and oom_exception lines.
	// Result should have it once, sourced from the first match (oom_error).
	s := NewScanner()
	res, err := s.ScanFile(filepath.Join("testdata", "dremio24x", "server.log"))
	if err != nil {
		t.Fatalf("ScanFile error: %v", err)
	}
	id := "0a1b2c3d-4e5f-6a7b-8c9d-0e1f2a3b4c5d"
	m := res.Matches[id]
	if m == nil {
		t.Fatalf("dedup target ID %s not found", id)
	}
	if m.Source != "oom_error" {
		t.Errorf("expected first-match source oom_error, got %q", m.Source)
	}
	// Count occurrences in JobIDs() — should be exactly 1
	count := 0
	for _, jid := range res.JobIDs() {
		if jid == id {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected ID to appear once in JobIDs(), appeared %d times", count)
	}
}

func TestScanReader_LargeInput(t *testing.T) {
	// Generate 1M+ lines: mostly filler, with known job IDs at specific positions.
	const totalLines = 1_100_000
	const insertEvery = 250_000

	knownIDs := []string{
		"aa000001-0000-0000-0000-000000000001",
		"aa000002-0000-0000-0000-000000000002",
		"aa000003-0000-0000-0000-000000000003",
		"aa000004-0000-0000-0000-000000000004",
	}

	var sb strings.Builder
	for i := 1; i <= totalLines; i++ {
		if i%insertEvery == 0 {
			idx := (i / insertEvery) - 1
			if idx < len(knownIDs) {
				sb.WriteString(fmt.Sprintf(
					"2024-01-01 00:00:00,000 [%s:frag:0:0] ERROR com.dremio.exec - OUT_OF_MEMORY ERROR for query %s\n",
					knownIDs[idx], knownIDs[idx]))
				continue
			}
		}
		sb.WriteString("2024-01-01 00:00:00,000 [pool-1] INFO  com.dremio.exec - Normal operation line\n")
	}

	s := NewScanner()
	res, err := s.ScanReader(strings.NewReader(sb.String()))
	if err != nil {
		t.Fatalf("ScanReader large input error: %v", err)
	}

	if res.TotalLinesScanned != totalLines {
		t.Errorf("TotalLinesScanned: got %d, want %d", res.TotalLinesScanned, totalLines)
	}

	requireContainsAll(t, res, knownIDs)
	if len(res.Matches) != len(knownIDs) {
		t.Errorf("expected %d unique job IDs, got %d: %v", len(knownIDs), len(res.Matches), res.JobIDs())
	}
}
