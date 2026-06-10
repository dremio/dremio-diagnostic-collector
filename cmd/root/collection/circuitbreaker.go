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
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultBackoffBase       = 1 * time.Second
	defaultBackoffMax        = 30 * time.Second
	consecutiveFailuresPause = 3
)

// CircuitBreaker implements rate limiting and backoff for kubectl exec / SSH calls.
type CircuitBreaker struct {
	maxConnections int
	activeConns    int64 // atomic counter
	backoffBase    time.Duration
	backoffMax     time.Duration

	mu          sync.Mutex
	cond        *sync.Cond
	podFailures map[string]int // consecutive failures per pod
	closed      bool
}

// NewCircuitBreaker creates a CircuitBreaker with the given maximum concurrent connections.
func NewCircuitBreaker(maxConnections int) *CircuitBreaker {
	cb := &CircuitBreaker{
		maxConnections: maxConnections,
		backoffBase:    defaultBackoffBase,
		backoffMax:     defaultBackoffMax,
		podFailures:    make(map[string]int),
	}
	cb.cond = sync.NewCond(&cb.mu)
	return cb
}

// Acquire blocks until a connection slot is available. Returns an error if the
// circuit breaker has been closed.
func (cb *CircuitBreaker) Acquire() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	for {
		if cb.closed {
			return fmt.Errorf("circuit breaker is closed")
		}
		if int(atomic.LoadInt64(&cb.activeConns)) < cb.maxConnections {
			atomic.AddInt64(&cb.activeConns, 1)
			return nil
		}
		cb.cond.Wait()
	}
}

// Release releases a connection slot and wakes one waiting Acquire call.
func (cb *CircuitBreaker) Release() {
	atomic.AddInt64(&cb.activeConns, -1)
	cb.cond.Signal()
}

// RecordSuccess resets the consecutive failure count for the given node.
func (cb *CircuitBreaker) RecordSuccess(node string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	delete(cb.podFailures, node)
}

// RecordFailure increments the consecutive failure count for the given node
// and returns the backoff duration that should be waited before retrying.
func (cb *CircuitBreaker) RecordFailure(node string) time.Duration {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.podFailures[node]++

	shift := cb.podFailures[node] - 1
	if shift < 0 {
		shift = 0
	} else if shift > 30 {
		shift = 30
	}
	backoff := cb.backoffBase * time.Duration(1<<shift) // #nosec G115 -- shift clamped to [0,30]
	if backoff > cb.backoffMax {
		backoff = cb.backoffMax
	}
	return backoff
}

// ShouldPauseNode returns true if the given node has had 3 or more consecutive
// failures, indicating it should be temporarily skipped.
func (cb *CircuitBreaker) ShouldPauseNode(node string) bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.podFailures[node] >= consecutiveFailuresPause
}

// SetMaxConnections dynamically adjusts the maximum concurrent connections.
// Used by AdaptiveConcurrency to respond to API server load.
func (cb *CircuitBreaker) SetMaxConnections(max int) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.maxConnections = max
	// Wake all waiters so they can re-evaluate with the new limit
	cb.cond.Broadcast()
}

// Close shuts down the circuit breaker and unblocks any waiting Acquire calls.
func (cb *CircuitBreaker) Close() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.closed = true
	cb.cond.Broadcast()
}
