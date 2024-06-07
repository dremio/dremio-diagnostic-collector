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
	"os"
	"path/filepath"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/output"
)

// TestTree will create a temporary directory, create some files and directories in it
// and then compare the output of Tree function with the expected string.
func TestTree(t *testing.T) {
	// Creating a temporary directory
	tempDir, err := os.MkdirTemp("", "TestTree")
	if err != nil {
		t.Fatalf("Cannot create temporary directory: %v", err)
	}

	defer func() {
		err := os.RemoveAll(tempDir) // clean up
		if err != nil {
			t.Errorf("Unexpected error removing temp directory: %v", err)
		}
	}()

	// Creating a dummy structure
	dirPath := filepath.Join(tempDir, "dir1/dir2")
	err = os.MkdirAll(dirPath, 0755)
	if err != nil {
		t.Fatalf("Cannot create directories: %v", err)
	}

	file1 := filepath.Join(tempDir, "file1")
	err = os.WriteFile(file1, []byte("file1"), 0644)
	if err != nil {
		t.Fatalf("Cannot write to file: %v", err)
	}

	file2 := filepath.Join(tempDir, "dir1/file2")
	err = os.WriteFile(file2, []byte("file2"), 0644)
	if err != nil {
		t.Fatalf("Cannot write to file: %v", err)
	}

	expected := TreeToString(tempDir)
	out, err := output.CaptureOutput(func() {
		Tree(tempDir)
	})

	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if out != expected {
		t.Fatalf("expected %q, got %q", expected, out)
	}
}
