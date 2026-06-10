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

package collection

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCircuitBreaker_AcquireRelease(t *testing.T) {
	cb := NewCircuitBreaker(2)
	defer cb.Close()

	// Acquire two slots — should succeed immediately.
	if err := cb.Acquire(); err != nil {
		t.Fatalf("first Acquire failed: %v", err)
	}
	if err := cb.Acquire(); err != nil {
		t.Fatalf("second Acquire failed: %v", err)
	}

	// Third Acquire should block. Use a goroutine with a timeout to verify.
	var acquired int32
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := cb.Acquire(); err != nil {
			return
		}
		atomic.StoreInt32(&acquired, 1)
		cb.Release()
	}()

	// Give the goroutine a moment — it should NOT have acquired yet.
	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&acquired) == 1 {
		t.Error("third Acquire should have blocked, but it succeeded immediately")
	}

	// Release one slot — the blocked goroutine should now proceed.
	cb.Release()
	wg.Wait()

	if atomic.LoadInt32(&acquired) != 1 {
		t.Error("third Acquire did not succeed after Release")
	}

	// Release the remaining slot.
	cb.Release()
}

func TestCircuitBreaker_BackoffAfterFailure(t *testing.T) {
	cb := NewCircuitBreaker(5)
	defer cb.Close()

	node := "pod-abc"

	// Record 3 consecutive failures.
	for i := 0; i < 3; i++ {
		_ = cb.RecordFailure(node)
	}

	if !cb.ShouldPauseNode(node) {
		t.Error("expected ShouldPauseNode to be true after 3 consecutive failures")
	}

	// Verify backoff increases.
	cb2 := NewCircuitBreaker(5)
	defer cb2.Close()
	d1 := cb2.RecordFailure("n1")
	d2 := cb2.RecordFailure("n1")
	if d2 <= d1 {
		t.Errorf("expected increasing backoff: d1=%v d2=%v", d1, d2)
	}
}

func TestCircuitBreaker_SuccessResetsFailures(t *testing.T) {
	cb := NewCircuitBreaker(5)
	defer cb.Close()

	node := "pod-xyz"

	// Record 2 failures.
	cb.RecordFailure(node)
	cb.RecordFailure(node)

	// Record a success — should reset.
	cb.RecordSuccess(node)

	if cb.ShouldPauseNode(node) {
		t.Error("expected ShouldPauseNode to be false after RecordSuccess")
	}

	// One more failure should not trigger pause (count is back to 1).
	cb.RecordFailure(node)
	if cb.ShouldPauseNode(node) {
		t.Error("expected ShouldPauseNode to be false after 1 failure post-reset")
	}
}
