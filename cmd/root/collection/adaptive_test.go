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
	"testing"
	"time"
)

func TestAdaptiveConcurrency_NoReductionBelowThreshold(t *testing.T) {
	cb := NewCircuitBreaker(10)
	ac := NewAdaptiveConcurrency(cb, 10)

	// Record fast responses
	for i := 0; i < 10; i++ {
		ac.RecordLatency(500 * time.Millisecond)
	}

	if ac.IsReduced() {
		t.Error("concurrency should not be reduced when p95 < threshold")
	}
	if ac.CurrentMax() != 10 {
		t.Errorf("expected max 10, got %d", ac.CurrentMax())
	}
}

func TestAdaptiveConcurrency_ReducesOnHighLatency(t *testing.T) {
	cb := NewCircuitBreaker(10)
	ac := NewAdaptiveConcurrency(cb, 10)

	// Record slow responses (p95 > 2s)
	for i := 0; i < 10; i++ {
		ac.RecordLatency(3 * time.Second)
	}

	if !ac.IsReduced() {
		t.Error("concurrency should be reduced when p95 > 2s")
	}
	if ac.CurrentMax() != 5 {
		t.Errorf("expected max 5 (halved from 10), got %d", ac.CurrentMax())
	}
}

func TestAdaptiveConcurrency_RestoresOnLowLatency(t *testing.T) {
	cb := NewCircuitBreaker(10)
	ac := NewAdaptiveConcurrency(cb, 10)

	// Trigger reduction
	for i := 0; i < 10; i++ {
		ac.RecordLatency(3 * time.Second)
	}
	if !ac.IsReduced() {
		t.Fatal("should be reduced")
	}

	// Clear samples by waiting (simulate time passing)
	ac.mu.Lock()
	ac.samples = nil
	ac.mu.Unlock()

	// Record fast responses
	for i := 0; i < 10; i++ {
		ac.RecordLatency(200 * time.Millisecond)
	}

	if ac.IsReduced() {
		t.Error("concurrency should be restored when p95 < 1s")
	}
	if ac.CurrentMax() != 10 {
		t.Errorf("expected max restored to 10, got %d", ac.CurrentMax())
	}
}

func TestAdaptiveConcurrency_MinConcurrency(t *testing.T) {
	cb := NewCircuitBreaker(3)
	ac := NewAdaptiveConcurrency(cb, 3)

	// Record very slow responses
	for i := 0; i < 10; i++ {
		ac.RecordLatency(5 * time.Second)
	}

	if ac.CurrentMax() < minConcurrency {
		t.Errorf("concurrency should not drop below %d, got %d", minConcurrency, ac.CurrentMax())
	}
}

func TestAdaptiveConcurrency_NeedsMinSamples(t *testing.T) {
	cb := NewCircuitBreaker(10)
	ac := NewAdaptiveConcurrency(cb, 10)

	// Only 3 samples — not enough for p95
	for i := 0; i < 3; i++ {
		ac.RecordLatency(5 * time.Second)
	}

	if ac.IsReduced() {
		t.Error("should not reduce with fewer than 5 samples")
	}
}
