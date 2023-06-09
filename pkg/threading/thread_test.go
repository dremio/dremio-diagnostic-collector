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

package threading_test

import (
	"sync"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/pkg/threading"
)

var (
	tp *threading.ThreadPool
)

var setupThreadPool = func() {
	tp = threading.NewThreadPool(10)
}

func TestThreadPool_WhenWaitWithOneJob(t *testing.T) {
	var waitErr error
	var executed bool
	setupThreadPool()
	executed = false
	jobFunc := func() error {
		executed = true
		return nil
	}

	tp.AddJob(jobFunc)
	waitErr = tp.ProcessAndWait()

	//		It("should execute all jobs", func() {
	if !executed {
		t.Errorf("did not execute all jobs")
	}

	//It("should wait successfully", func() {
	if waitErr != nil {
		t.Errorf("unexpected error %v", waitErr)
	}
}

func TestThreadPool_WhenWaitWithNoJobs(t *testing.T) {
	err := tp.ProcessAndWait()
	if err == nil {
		t.Error("expected an error but received none")
	}
}

func TestThreadPool_When(t *testing.T) {
	var executed []bool
	var mut sync.RWMutex
	var waitErr error
	setupThreadPool()
	jobFunc := func() error {
		mut.Lock()
		defer mut.Unlock()
		executed = append(executed, true)
		return nil
	}
	for i := 0; i < 100; i++ {
		tp.AddJob(jobFunc)
	}
	waitErr = tp.ProcessAndWait()

	//It("should execute all jobs", func() {
	if len(executed) != 100 {
		t.Errorf("expected 100 jobs executed but had only %v", len(executed))
	}

	//It("should wait successfully", func() {
	if waitErr != nil {
		t.Errorf("unexpected error %v", waitErr)
	}
}

func TestThreadPool_WhenWait(t *testing.T) {
	var executed []bool
	var mut sync.RWMutex
	var waitErr error
	setupThreadPool()
	jobFunc := func() error {
		mut.Lock()
		defer mut.Unlock()
		executed = append(executed, true)
		return nil
	}

	for i := 0; i < 10; i++ {
		tp.AddJob(jobFunc)
	}
	waitErr = tp.ProcessAndWait()

	//It("should execute all jobs", func() {
	if len(executed) != 10 {
		t.Errorf("expected 10 jobs executed but had only %v", len(executed))
	}
	if waitErr != nil {
		t.Errorf("unexpected error %v", waitErr)
	}
}
