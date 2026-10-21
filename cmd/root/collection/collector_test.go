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

// collection package provides the interface for collection implementation and the actual collection execution
package collection

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/collects"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/simplelog"
)

func TestFilterCoordinators(t *testing.T) {
	t.Log("testing filtering duplicates")
	firstItem := "192.168.1.20"
	secondItem := "192.168.1.120"
	filtered := FilterCoordinators([]string{firstItem, firstItem, secondItem})
	expectedItems := 2
	if len(filtered) != expectedItems {
		t.Errorf("expected %v but got %v items", expectedItems, len(filtered))
	}
	t.Log("items are sorted in desc order by default")
	if filtered[0] != firstItem {
		t.Errorf("expected %v but got %v", firstItem, filtered[1])
	}
	if filtered[1] != secondItem {
		t.Errorf("expected %v but got %v", secondItem, filtered[0])
	}
}

func TestFilterExecutors(t *testing.T) {
	t.Log("testing filtering duplicates")
	firstItem := "192.168.1.20"
	secondItem := "192.168.1.120"
	filtered := FilterExecutors([]string{firstItem, firstItem, secondItem}, []string{})
	expectedItems := 2
	if len(filtered) != expectedItems {
		t.Errorf("expected %v but got %v items", expectedItems, len(filtered))
	}
	t.Log("items are sorted in desc order by default")
	if filtered[0] != firstItem {
		t.Errorf("expected %v but got %v", firstItem, filtered[1])
	}
	if filtered[1] != secondItem {
		t.Errorf("expected %v but got %v", secondItem, filtered[0])
	}

	t.Logf("now verify filter out coordinators")
	filtered = FilterExecutors([]string{firstItem, firstItem, secondItem}, []string{firstItem, secondItem})
	expectedItems = 0
	if len(filtered) != expectedItems {
		t.Errorf("expected %v but got %v items", expectedItems, len(filtered))
	}
}

func TestSuccessRate(t *testing.T) {
	tests := []struct {
		collected, failed, skipped int
		wantRate                   float64
		wantAttempts               int
	}{
		{collected: 125, failed: 0, skipped: 27, wantRate: 82.23684210526315, wantAttempts: 152},
		{collected: 10, failed: 2, skipped: 0, wantRate: 83.33333333333333, wantAttempts: 12},
		{collected: 0, failed: 0, skipped: 0, wantRate: 0, wantAttempts: 0},
	}
	for _, tt := range tests {
		rate, attempts := successRate(tt.collected, tt.failed, tt.skipped)
		if attempts != tt.wantAttempts || math.Abs(rate-tt.wantRate) > 1e-9 {
			t.Errorf("successRate(%d,%d,%d) = %v,%d want %v,%d", tt.collected, tt.failed, tt.skipped, rate, attempts, tt.wantRate, tt.wantAttempts)
		}
	}
}

func TestNodeDoneStatus(t *testing.T) {
	inc := []*IncompleteCollectionError{{Item: "queries-perf", Records: 187442, Cause: ErrStreamIncomplete}}
	g1 := &MemoryGuardError{Item: "sys.jobs_recent"}
	g2 := &MemoryGuardError{Item: "sys.views"}
	tests := []struct {
		name       string
		skipped    []string
		incomplete []*IncompleteCollectionError
		guard      []*MemoryGuardError
		want       string
	}{
		{name: "clean", want: "Done: 3 files (1.0KB), 0 skipped"},
		{name: "skipped", skipped: []string{"/opt/dremio/log/archive/queries.2026-08-23.0.json.gz"},
			want: "Done: 3 files (1.0KB), 1 skipped (queries.2026-08-23.0.json.gz)"},
		{name: "incomplete", incomplete: inc,
			want: "Done: 3 files (1.0KB), 0 skipped, INCOMPLETE: queries-perf (187442 records)"},
		{name: "skipped and incomplete", skipped: []string{"/opt/dremio/log/server.log"}, incomplete: inc,
			want: "Done: 3 files (1.0KB), 1 skipped (server.log), INCOMPLETE: queries-perf (187442 records)"},
		{name: "guard", guard: []*MemoryGuardError{g1},
			want: "Done: 3 files (1.0KB), 0 skipped, MEMORY GUARD: sys.jobs_recent"},
		{name: "incomplete and guard", incomplete: inc, guard: []*MemoryGuardError{g1, g2},
			want: "Done: 3 files (1.0KB), 0 skipped, INCOMPLETE: queries-perf (187442 records), MEMORY GUARD: sys.jobs_recent, sys.views"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nodeDoneStatus(3, 1024, tt.skipped, tt.incomplete, tt.guard); got != tt.want {
				t.Errorf("got %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestSummaryInfo_IncompleteCollectionsOmittedWhenEmpty(t *testing.T) {
	s, err := SummaryInfo{}.String()
	if err != nil {
		t.Fatalf("String: %v", err)
	}
	if strings.Contains(s, "incompleteCollections") {
		t.Error("incompleteCollections must be omitted when empty")
	}
	s, err = SummaryInfo{IncompleteCollections: []string{"dremio-master-0: queries-perf incomplete after 2 records (x)"}}.String()
	if err != nil || !strings.Contains(s, `"incompleteCollections"`) {
		t.Errorf("err=%v, want incompleteCollections in %s", err, s)
	}
}

func TestSummaryInfo_MemoryGuardSkipsOmittedWhenEmpty(t *testing.T) {
	s, err := SummaryInfo{}.String()
	if err != nil {
		t.Fatalf("String: %v", err)
	}
	if strings.Contains(s, "memoryGuardSkips") {
		t.Error("memoryGuardSkips must be omitted when empty")
	}
	s, err = SummaryInfo{MemoryGuardSkips: []string{"dremio-master-0: sys.jobs_recent (memory guard, exit 3)"}}.String()
	if err != nil || !strings.Contains(s, `"memoryGuardSkips"`) {
		t.Errorf("err=%v, want memoryGuardSkips in %s", err, s)
	}
}

func TestLogDistributedCollectionSummary_GuardSection(t *testing.T) {
	dir := t.TempDir()
	simplelog.InitLoggerWithOutputDir(dir)
	t.Cleanup(func() { _ = simplelog.Close(); simplelog.InitLogger() })

	readLog := func() string {
		b, err := os.ReadFile(filepath.Join(dir, "ddc.log"))
		if err != nil {
			t.Fatalf("read ddc.log: %v", err)
		}
		return string(b)
	}
	call := func(guard []string) {
		logDistributedCollectionSummary(collects.StandardCollection, []string{"m0"}, nil, nil,
			nil, nil, nil, []string{"m0: queries-perf incomplete"}, guard, 1, time.Second)
	}

	call([]string{"m0: sys.jobs_recent (x)"})
	got := readLog()
	for _, want := range []string{
		"Incomplete Collections: 1",
		"Memory Guard Skips: 1",
		"MEMORY GUARD SKIPS:",
		"  - m0: sys.jobs_recent (x)",
		"INCOMPLETE COLLECTIONS:",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("ddc.log missing %q:\n%s", want, got)
		}
	}

	if err := simplelog.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "ddc.log")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	simplelog.InitLoggerWithOutputDir(dir)
	call(nil)
	if got := readLog(); strings.Contains(got, "Memory Guard Skips") {
		t.Errorf("unexpected Memory Guard Skips line:\n%s", got)
	}
}

// TestFindClusterID_ParsesStreamedClusterStats: the streamed cluster-stats.json
// ends with a newline, which FindClusterID must still parse.
func TestFindClusterID_ParsesStreamedClusterStats(t *testing.T) {
	dir := t.TempDir()
	mc := streamCollector(`{"dremioVersion":"26.1.7","clusterID":"abc","nodeName":"m0"}`, nil)
	if _, err := collectRocksType(mc, &mockCopyStrategy{tmpDir: dir}, "dremio-master-0", "coordinator",
		"/opt/dremio/data/db/catalog", "cluster_stats", "", "cluster-stats", "cluster-stats.json"); err != nil {
		t.Fatalf("collectRocksType: %v", err)
	}
	stats, err := FindClusterID(dir)
	if err != nil {
		t.Fatalf("FindClusterID: %v", err)
	}
	if len(stats) != 1 || stats[0].ClusterID != "abc" {
		t.Errorf("stats = %+v, want one entry with ClusterID abc", stats)
	}
}
