//go:build !linux || !amd64

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

package ddcbinary

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteOutDDC(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()
	defer os.RemoveAll(tempDir) // clean up

	// Call the WriteOutDDC function
	ddcFilePath, err := WriteOutDDC(tempDir)
	if err != nil {
		t.Fatalf("WriteOutDDC failed: %v", err)
	}

	// Verify that the zip file was deleted
	zipFilePath := ddcFilePath + ".zip"
	if _, err := os.Stat(zipFilePath); !os.IsNotExist(err) {
		t.Errorf("zip file was not deleted: %v", err)
	}

	// Verify that the ddc file exists and can be opened
	if _, err := os.Stat(ddcFilePath); os.IsNotExist(err) {
		t.Errorf("ddc file does not exist: %v", err)
	}
}

func TestWriteOutDDCToInvalidFile(t *testing.T) {
	// 1. Test with an invalid directory
	if _, err := WriteOutDDC("/invalid/directory"); err == nil {
		t.Errorf("expected an error but got nil")
	}
}

func TestUnzipWithNonZipFile(t *testing.T) {
	// 1. Test with a non-zip file
	// Create a temp file
	f, err := os.CreateTemp("", "test")
	if err != nil {
		t.Fatalf("unable to create temp file: %v", err)
	}
	defer os.Remove(f.Name())

	_, err = f.WriteString("not a zip content")
	if err != nil {
		t.Fatalf("unable to write to temp file: %v", err)
	}
	f.Close()

	if err := Unzip(f.Name()); err == nil {
		t.Errorf("expected an error but got nil")
	}

	// 3. Test with a zip file whose content size is too large
	// Note: You'll need to create a zip file that contains a file larger than maxSize
}

func TestUnzipWithTooManyFilesInZip(t *testing.T) {
	entriesBefore, err := os.ReadDir("testdata")
	if err != nil {
		t.Errorf("unexpected error reading testdata %v", err)
	}
	if err := Unzip(filepath.Join("testdata", "test-too-many-files.zip")); err == nil {
		t.Errorf("expected an error but got nil")
	}
	entriesAfter, err := os.ReadDir("testdata")
	if err != nil {
		t.Errorf("unexpected error reading testdata %v", err)
	}
	if len(entriesBefore) != len(entriesAfter) {
		t.Errorf("the total file count changed by %v but we were expecting no change, we have some bad cleanup behavior to fix", len(entriesAfter)-len(entriesBefore))
	}
}
