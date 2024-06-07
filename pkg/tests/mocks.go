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

// package tests provides helper functions and mocks for running tests
package tests

import (
	"sync"

	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/root/cli"
)

type MockCli struct {
	PatCalls       []string
	Calls          [][]string
	StoredResponse []string
	StoredErrors   []error
	lock           sync.RWMutex
}

func (m *MockCli) Execute(_ bool, args ...string) (out string, err error) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.Calls = append(m.Calls, args)
	length := len(m.Calls)

	return m.StoredResponse[length-1], m.StoredErrors[length-1]
}

func (m *MockCli) ExecuteAndStreamOutput(_ bool, output cli.OutputHandler, pat string, args ...string) (err error) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.PatCalls = append(m.PatCalls, pat)
	m.Calls = append(m.Calls, args)
	length := len(m.Calls)
	output(m.StoredResponse[length-1])
	return m.StoredErrors[length-1]
}
