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

package tests_test

import (
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/tests"
)

func TestMatchFile(t *testing.T) {
	//should match files with the same content
	file1 := "testdata/file1.txt"
	file2 := "testdata/file2.txt"
	expectedFile := "testdata/expected.txt"
	// Expect file1 to match the expected file
	if match, err := tests.MatchFile(file1, expectedFile); !match && err != nil {
		t.Errorf("expected %v to equal %v", file1, expectedFile)
	}

	// Expect file2 to not match the expected file
	if match, err := tests.MatchFile(file2, expectedFile); match && err != nil {
		t.Errorf("expected %v to equal %v", file2, expectedFile)
	}

	//should not match files with different content"
	file3 := "testdata/file3.txt"

	// Expect file3 to match the expected file
	if match, err := tests.MatchFile(file3, expectedFile); !match && err != nil {
		t.Errorf("expected %v to equal %v", file3, expectedFile)
	}
}
func TestFileContents(t *testing.T) {
	//should match files with the same content
	testFile := "testdata/test_os_info.txt"
	expectedText := []string{">>> mount", ">>> lsblk"}

	var match bool
	// Expect testFile to contain the expected lines
	match, err := tests.MatchLines(t, expectedText, testFile)
	if !match {
		t.Errorf("expected %v to contain %v", testFile, expectedText)
	}
	if err != nil {
		t.Errorf("error matching lines in  %v error was %v", testFile, err)
	}
}
