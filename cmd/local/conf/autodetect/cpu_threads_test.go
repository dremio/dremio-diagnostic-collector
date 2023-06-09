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

package autodetect_test

import (
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf/autodetect"
)

func TestGetDefaultThreadsFromCPUs(t *testing.T) {
	cases := []struct {
		name     string
		numCPUs  int
		expected int
	}{
		{"CPUs is 1", 1, 2},
		{"CPUs is 4", 4, 2},
		{"CPUs is 6", 6, 3},
		{"CPUs is 5", 5, 2},
		{"CPUs is 1000", 1000, 500},
		{"CPUs is 0", 0, 2},
		{"CPUs is -4", -4, 2},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := autodetect.GetDefaultThreadsFromCPUs(c.numCPUs); got != c.expected {
				t.Errorf("GetDefaultThreadsFromCPUs(%d) = %d; want %d", c.numCPUs, got, c.expected)
			}
		})
	}
}
