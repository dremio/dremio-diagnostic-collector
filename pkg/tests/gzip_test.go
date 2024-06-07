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
	"path/filepath"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/tests"
)

func TestGzipMatchers_ContainsFileInGzip(t *testing.T) {
	//should contain the expected file
	gzipFile := filepath.Join("testdata", "file1.txt.gz")
	expectedFile := filepath.Join("testdata", "file1.txt")

	// Expect the gzip file to contain the expected file
	isValid, err := tests.ContainThisFileInTheGzip(expectedFile, gzipFile)
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	if !isValid {
		t.Error("expected file to be in gzip but was not")
	}

	//should not contain a different file"
	gzipFile = filepath.Join("testdata", "file1.txt.gz")
	expectedFile = filepath.Join("testdata", "file3.txt")

	// Expect the gzip file not to contain the different file
	isValid, err = tests.ContainThisFileInTheGzip(expectedFile, gzipFile)
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	if isValid {
		t.Error("expected file to NOT be in gzip but was present")
	}
}
