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
	"sync"

	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
)

// Hook is a thread safe queue of cleanup work to be run.
// this is to be used for things that need to be cleaned up if the process
// receives an interrupt (as defers would not be run)
type Hook struct {
	mu              sync.Mutex
	cleanups        []cleanupTask
	priorityCleanup []cleanupTask
}

type cleanupTask struct {
	name string
	p    func()
}

// Add will add a function call to a list to be cleaned up later
// Is thread safe.
func (h *Hook) Add(p func(), name string) {
	defer h.mu.Unlock()
	h.mu.Lock()
	h.cleanups = append(h.cleanups, cleanupTask{name: name, p: p})
}

// AddPriorityCancel are run first as their order is important
func (h *Hook) AddPriorityCancel(p func(), name string) {
	defer h.mu.Unlock()
	h.mu.Lock()
	h.priorityCleanup = append(h.priorityCleanup, cleanupTask{name: name, p: p})
}

// Cleanup runs in order all cleanup tasks that have been added
// Is thread safe
func (h *Hook) Cleanup() {
	defer h.mu.Unlock()
	simplelog.Debugf("%v tasks to run on cleanup", len(h.cleanups)+len(h.priorityCleanup))
	for _, j := range h.priorityCleanup {
		simplelog.Debugf("cleaning up %v", j.name)
		j.p()
	}
	h.mu.Lock()
	for _, j := range h.cleanups {
		simplelog.Debugf("cleaning up %v", j.name)
		j.p()
	}

	//blank
	h.priorityCleanup = []cleanupTask{}
	h.cleanups = []cleanupTask{}

}
