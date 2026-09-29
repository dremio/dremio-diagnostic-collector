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
	"strings"
	"testing"
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
	tests := []struct {
		name       string
		skipped    []string
		incomplete []*IncompleteCollectionError
		want       string
	}{
		{name: "clean", want: "Done: 3 files (1.0KB), 0 skipped"},
		{name: "skipped", skipped: []string{"/opt/dremio/log/archive/queries.2026-08-23.0.json.gz"},
			want: "Done: 3 files (1.0KB), 1 skipped (queries.2026-08-23.0.json.gz)"},
		{name: "incomplete", incomplete: inc,
			want: "Done: 3 files (1.0KB), 0 skipped, INCOMPLETE: queries-perf (187442 records)"},
		{name: "skipped and incomplete", skipped: []string{"/opt/dremio/log/server.log"}, incomplete: inc,
			want: "Done: 3 files (1.0KB), 1 skipped (server.log), INCOMPLETE: queries-perf (187442 records)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nodeDoneStatus(3, 1024, tt.skipped, tt.incomplete); got != tt.want {
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
