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

package shutdown_test

import (
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/pkg/shutdown"
)

func TestShutdownRunsAllTasksInOrder(t *testing.T) {
	hook := shutdown.NewHook()
	items := []int{}
	hook.Add(func() {
		items = append(items, 1)
	}, "")
	hook.Add(func() {
		items = append(items, 2)
	}, "")
	hook.Add(func() {
		items = append(items, 3)
	}, "")
	hook.Cleanup()
	if items[0] != 1 {
		t.Errorf("expected 1 but was %v", items[0])
	}
	if items[1] != 2 {
		t.Errorf("expected 2 but was %v", items[1])
	}
	if items[2] != 3 {
		t.Errorf("expected 3 but was %v", items[2])
	}
}
