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

	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/simplelog"
)

type ThreadPool struct {
	wg               sync.WaitGroup
	numberThreads    int
	jobs             chan Job
	pendingJobs      int
	totalJobs        int
	loggingFrequency int
	output           bool
	outputProgress   bool
	mut              sync.Mutex
}

func NewThreadPool(numberThreads int, loggingFrequency int, output bool, outputProgress bool) (*ThreadPool, error) {
	if numberThreads == 0 {
		return &ThreadPool{}, errors.New("invalid number of threads at 0")
	}

	//by default support 4 million jobs
	jobs := make(chan Job, 4000000)
	return &ThreadPool{
		numberThreads:    numberThreads,
		jobs:             jobs,
		loggingFrequency: loggingFrequency,
		output:           output,
		outputProgress:   outputProgress,
	}, nil
}

func NewThreadPoolWithJobQueue(numberThreads, jobQueueSize int, loggingFrequency int, output bool, outputProgress bool) (*ThreadPool, error) {
	if numberThreads == 0 {
		return &ThreadPool{}, errors.New("invalid number of threads at 0")
	}
	jobs := make(chan Job, jobQueueSize)

	return &ThreadPool{
		numberThreads:    numberThreads,
		jobs:             jobs,
		loggingFrequency: loggingFrequency,
		output:           output,
		outputProgress:   outputProgress,
	}, nil
}

type Job struct {
	Name    string
	Process func() error
}

// AddJob adds a job to the thread pool. It increases the wait group counter and sends the job to the jobs channel.
func (t *ThreadPool) AddJob(job Job) {
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
		msg := fmt.Sprintf("JOB START - %v", job.Name)
		if t.output {
			fmt.Println(msg)
		}
		simplelog.Info(msg)
		err := job.Process()
		if err != nil {
			msg := fmt.Sprintf("JOB FAILED - %v - %v", job.Name, err)
			if t.output {
				fmt.Println(msg)
			}
			simplelog.Error(msg)
		} else {
			msg := fmt.Sprintf("JOB COMPLETE - %v", job.Name)
			if t.output {
				fmt.Println(msg)
			}
			simplelog.Info(msg)
		}
		t.mut.Lock()
		t.pendingJobs--
		jobsCompleted := t.totalJobs - t.pendingJobs
		if jobsCompleted%t.loggingFrequency == 0 {
			msg := fmt.Sprintf("JOB PROGRESS - %.2f%% COMPLETED", (float64(t.totalJobs-t.pendingJobs)*100.0)/float64(t.totalJobs))
			if t.outputProgress {
				fmt.Println(msg)
			}
			simplelog.Info(msg)
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
	msg := fmt.Sprintf("JOB PROGRESS - %.2f%% COMPLETED", (float64(t.totalJobs-t.pendingJobs)*100.0)/float64(t.totalJobs))
	if t.outputProgress {
		fmt.Println(msg)
	}
	simplelog.Info(msg)
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
