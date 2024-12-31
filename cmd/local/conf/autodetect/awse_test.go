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
	"os"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/conf/autodetect"
)

func TestIsAWSEExecutorUsingDir(t *testing.T) {
	var (
		testDir  string
		nodeName string
		err      error
	)

	beforeEach := func() {
		testDir, err = os.MkdirTemp("", "example")
		if err != nil {
			t.Errorf("unexpected error %v", err)
		}
		nodeName = "TestNode"

		subDir := testDir + "/SubDirectory"
		err = os.Mkdir(subDir, 0o755)
		if err != nil {
			t.Errorf("unexpected error %v", err)
		}
	}

	afterEach := func() {
		os.RemoveAll(testDir)
	}

	// should return true when node name is found
	beforeEach()

	nodeDir := testDir + "/" + nodeName
	err = os.Mkdir(nodeDir, 0o755)
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}

	isAWSE, err := autodetect.IsAWSEExecutorUsingDir(testDir, nodeName)
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	if !isAWSE {
		t.Error("expected to be AWSE but was detected as not AWSE")
	}
	afterEach()
	// end scenario

	// should return false when node name is not found
	beforeEach()
	isAWSE, err = autodetect.IsAWSEExecutorUsingDir(testDir, nodeName)
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	if isAWSE {
		t.Error("expected to not be AWSE but was detected as AWSE")
	}
	afterEach()
	// end scenario
}
