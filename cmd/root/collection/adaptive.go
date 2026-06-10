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
	"sort"
	"sync"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/simplelog"
)

const (
	// adaptiveWindowDuration is the sliding window for p95 calculation.
	adaptiveWindowDuration = 30 * time.Second

	// p95HighThreshold triggers concurrency reduction when exceeded.
	p95HighThreshold = 2 * time.Second

	// p95LowThreshold triggers concurrency restoration when response times drop below.
	p95LowThreshold = 1 * time.Second

	// minConcurrency is the floor for adaptive concurrency reduction.
	minConcurrency = 2
)

// AdaptiveConcurrency monitors response times from kubectl exec / SSH calls
// and dynamically adjusts the circuit breaker's max connections based on
// the p95 latency over a sliding window.
type AdaptiveConcurrency struct {
	mu             sync.Mutex
	samples        []timedSample
	maxConnections int // the original/configured max
	currentMax     int // the current effective max (may be halved)
	cb             *CircuitBreaker
	reduced        bool
}

type timedSample struct {
	timestamp time.Time
	duration  time.Duration
}

// NewAdaptiveConcurrency wraps a CircuitBreaker with adaptive concurrency control.
func NewAdaptiveConcurrency(cb *CircuitBreaker, maxConnections int) *AdaptiveConcurrency {
	return &AdaptiveConcurrency{
		samples:        make([]timedSample, 0, 256),
		maxConnections: maxConnections,
		currentMax:     maxConnections,
		cb:             cb,
	}
}

// RecordLatency records a response time sample and adjusts concurrency if needed.
func (ac *AdaptiveConcurrency) RecordLatency(d time.Duration) {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	now := time.Now()
	ac.samples = append(ac.samples, timedSample{timestamp: now, duration: d})

	// Prune samples outside the window
	cutoff := now.Add(-adaptiveWindowDuration)
	pruned := ac.samples[:0]
	for _, s := range ac.samples {
		if s.timestamp.After(cutoff) {
			pruned = append(pruned, s)
		}
	}
	ac.samples = pruned

	// Need at least 5 samples for a meaningful p95
	if len(ac.samples) < 5 {
		return
	}

	p95 := ac.computeP95()

	if p95 > p95HighThreshold && !ac.reduced {
		newMax := ac.currentMax / 2
		if newMax < minConcurrency {
			newMax = minConcurrency
		}
		simplelog.Warningf("adaptive concurrency: p95 response time %.1fs exceeds %.1fs threshold — reducing max connections from %d to %d",
			p95.Seconds(), p95HighThreshold.Seconds(), ac.currentMax, newMax)
		ac.currentMax = newMax
		ac.cb.SetMaxConnections(newMax)
		ac.reduced = true
	} else if p95 < p95LowThreshold && ac.reduced {
		simplelog.Infof("adaptive concurrency: p95 response time %.1fs below %.1fs threshold — restoring max connections to %d",
			p95.Seconds(), p95LowThreshold.Seconds(), ac.maxConnections)
		ac.currentMax = ac.maxConnections
		ac.cb.SetMaxConnections(ac.maxConnections)
		ac.reduced = false
	}
}

// computeP95 calculates the 95th percentile of response times in the current window.
// Must be called with ac.mu held.
func (ac *AdaptiveConcurrency) computeP95() time.Duration {
	if len(ac.samples) == 0 {
		return 0
	}

	durations := make([]time.Duration, len(ac.samples))
	for i, s := range ac.samples {
		durations[i] = s.duration
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })

	idx := int(float64(len(durations)) * 0.95)
	if idx >= len(durations) {
		idx = len(durations) - 1
	}
	return durations[idx]
}

// CurrentMax returns the current effective max connections.
func (ac *AdaptiveConcurrency) CurrentMax() int {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return ac.currentMax
}

// IsReduced returns true if concurrency has been reduced from its original value.
func (ac *AdaptiveConcurrency) IsReduced() bool {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return ac.reduced
}
