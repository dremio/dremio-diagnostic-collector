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

// threading package provides support for simple concurrency and threading
package threading

import (
	"errors"
	"fmt"
	"sync"

	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
)

type ThreadPool struct {
	wg               sync.WaitGroup
	numberThreads    int
	jobs             chan func() error
	pendingJobs      int
	totalJobs        int
	loggingFrequency int
	mut              sync.Mutex
}

func NewThreadPool(numberThreads int, loggingFrequency int) (*ThreadPool, error) {
	if numberThreads == 0 {
		return &ThreadPool{}, errors.New("invalid number of threads at 0")
	}

	//by default support 4 million jobs
	jobs := make(chan func() error, 4000000)
	return &ThreadPool{
		numberThreads:    numberThreads,
		jobs:             jobs,
		loggingFrequency: loggingFrequency,
	}, nil
}

func NewThreadPoolWithJobQueue(numberThreads, jobQueueSize int, loggingFrequency int) (*ThreadPool, error) {
	if numberThreads == 0 {
		return &ThreadPool{}, errors.New("invalid number of threads at 0")
	}
	jobs := make(chan func() error, jobQueueSize)

	return &ThreadPool{
		numberThreads:    numberThreads,
		jobs:             jobs,
		loggingFrequency: loggingFrequency,
	}, nil
}

// AddJob adds a job to the thread pool. It increases the wait group counter and sends the job to the jobs channel.
func (t *ThreadPool) AddJob(job func() error) {
	t.mut.Lock()
	t.pendingJobs++
	t.totalJobs++
	t.mut.Unlock()
	t.wg.Add(1)
	t.jobs <- job
}

// worker listens for jobs on the jobs channel and executes them. Each job runs on its own goroutine.
func (t *ThreadPool) worker() {
	for job := range t.jobs {
		err := job()
		if err != nil {
			fmt.Print("x")
			simplelog.Errorf("Failed to execute job: %v", err)
		} else {
			fmt.Print(".")
		}
		t.mut.Lock()
		t.pendingJobs--
		jobsCompleted := t.totalJobs - t.pendingJobs
		if jobsCompleted%t.loggingFrequency == 0 {
			simplelog.Infof("%v/%v tasks completed", jobsCompleted, t.totalJobs)
		}
		t.mut.Unlock()
		t.wg.Done()
	}
}

// ProcessAndWait blocks until all jobs have finished. If no jobs were added, it returns an error.
func (t *ThreadPool) ProcessAndWait() error {
	t.mut.Lock()
	if t.pendingJobs == 0 {
		t.mut.Unlock()
		return fmt.Errorf("thread pool wait called with no pending jobs this is unexpected")
	}
	t.mut.Unlock()
	//start processing jobs
	for i := 0; i < t.numberThreads; i++ {
		go t.worker()
	}
	//then wait for them
	t.wg.Wait()
	close(t.jobs)
	t.mut.Lock()
	simplelog.Infof("%v/%v tasks completed", t.totalJobs, t.totalJobs)
	t.totalJobs = 0
	t.mut.Unlock()
	return nil
}

// PendingJobs returns the number of jobs that are pending.
func (t *ThreadPool) PendingJobs() int {
	t.mut.Lock()
	defer t.mut.Unlock()
	return t.pendingJobs
}
