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

package shutdown

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/consoleprint"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/simplelog"
)

type CancelHook interface {
	GetContext() context.Context
}

type Hook interface {
	GetContext() context.Context
	AddFinalSteps(p func(), name string)
	Add(p func(), name string)
	AddCancelOnlyTasks(p func(), name string)
	Cleanup()
	Interrupt()
	AddUIStop(func())
}

// hookImpl is a thread safe queue of cleanup work to be run.
// this is to be used for things that need to be cleaned up if the process
// receives an interrupt (as defers would not be run)
type hookImpl struct {
	mu           sync.Mutex
	cleanups     []cleanupTask
	cancelOnly   []cleanupTask
	finalSteps   []cleanupTask
	ctx          context.Context
	stopUIThread func()
}

func NewHook() Hook {
	ctx, cancel := context.WithCancel(context.Background())
	hook := &hookImpl{
		ctx:          ctx,
		stopUIThread: func() {},
	}
	hook.Add(cancel, "cancelling all cancellable executions")
	return hook
}

type cleanupTask struct {
	name string
	p    func()
}

// GetContext provides a cancel context for everyone to share
func (h *hookImpl) GetContext() context.Context {
	return h.ctx
}

// AddUIStop sets the function that stops the ui thread
func (h *hookImpl) AddUIStop(f func()) {
	defer h.mu.Unlock()
	h.mu.Lock()
	h.stopUIThread = f
}

// Add will add a function call to a list to be cleaned up later
// Is thread safe.
func (h *hookImpl) Add(p func(), name string) {
	defer h.mu.Unlock()
	h.mu.Lock()
	h.cleanups = append(h.cleanups, cleanupTask{name: name, p: p})
}

// AddCancelOnlyTasks are run first as their order is important
func (h *hookImpl) AddCancelOnlyTasks(p func(), name string) {
	defer h.mu.Unlock()
	h.mu.Lock()
	h.cancelOnly = append(h.cancelOnly, cleanupTask{name: name, p: p})
}

// AddFinalSteps run last after everything has stopped
func (h *hookImpl) AddFinalSteps(p func(), name string) {
	defer h.mu.Unlock()
	h.mu.Lock()
	h.finalSteps = append(h.finalSteps, cleanupTask{name: name, p: p})
}

// Cleanup runs in order all cleanup tasks that have been added
// Is thread safe
func (h *hookImpl) Interrupt() {
	h.mu.Lock()
	defer h.mu.Unlock()
	totalTasks := len(h.cleanups) + len(h.cancelOnly) + len(h.finalSteps)
	if totalTasks == 0 {
		return
	}
	consoleprint.UpdateResult("CLEANUP TASKS")
	var counter int
	simplelog.Debugf("%v tasks to run on cleanup", totalTasks)
	for _, j := range h.cancelOnly {
		counter++
		consoleprint.UpdateResult(fmt.Sprintf("CLEANUP TASKS - %v/%v. %v", counter, totalTasks, j.name))
		simplelog.Debugf("shutdown initial stage: %v", j.name)
		j.p()
	}
	h.cancelOnly = []cleanupTask{}
	for _, j := range h.cleanups {
		counter++
		consoleprint.UpdateResult(fmt.Sprintf("CLEANUP TASKS - %v/%v. %v", counter, totalTasks, j.name))
		simplelog.Debugf("shutdown task: %v", j.name)
		j.p()
	}
	// blank
	h.cleanups = []cleanupTask{}
	for _, j := range h.finalSteps {
		counter++
		consoleprint.UpdateResult(fmt.Sprintf("CLEANUP TASKS - %v/%v. %v", counter, totalTasks, j.name))
		simplelog.Debugf("shutdown task final stage: %v", j.name)
		j.p()
	}
	// blank
	h.finalSteps = []cleanupTask{}
	consoleprint.UpdateResult(fmt.Sprintf("COMPLETE AT %v", time.Now().Format(time.RFC1123)))
	h.stopUIThread()
}

// Cleanup runs in order all cleanup tasks that have been added
// Is thread safe
func (h *hookImpl) Cleanup() {
	h.mu.Lock()
	defer h.mu.Unlock()
	totalTasks := len(h.cleanups) + len(h.finalSteps)
	if totalTasks == 0 {
		return
	}
	consoleprint.UpdateResult("CLEANUP TASKS")
	var counter int
	simplelog.Debugf("%v tasks to run on cleanup", totalTasks)

	for _, j := range h.cleanups {
		counter++
		consoleprint.UpdateResult(fmt.Sprintf("CLEANUP TASKS - %v/%v. %v", counter, totalTasks, j.name))
		simplelog.Debugf("shutdown task: %v", j.name)
		j.p()
	}
	// blank
	h.cleanups = []cleanupTask{}
	for _, j := range h.finalSteps {
		counter++
		consoleprint.UpdateResult(fmt.Sprintf("CLEANUP TASKS - %v/%v. %v", counter, totalTasks, j.name))
		simplelog.Debugf("shutdown task final stage: %v", j.name)
		j.p()
	}
	// blank
	h.finalSteps = []cleanupTask{}
	consoleprint.UpdateResult(fmt.Sprintf("COMPLETE AT %v", time.Now().Format(time.RFC1123)))
	time.Sleep(2 * time.Second) // pause 2 seconds to allow the UI to update
	h.stopUIThread()
}
