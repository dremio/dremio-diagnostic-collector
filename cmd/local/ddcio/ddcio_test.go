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

package ddcio_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/ddcio"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
)

func TestCopyFile(t *testing.T) {
	srcContent := []byte("This is the source file content")

	// Create a temporary source file
	srcFile, err := os.CreateTemp("", "source-file")
	if err != nil {
		t.Fatalf("Failed to create temporary source file: %v", err)
	}
	defer os.Remove(srcFile.Name())
	defer srcFile.Close()

	// Write content to the source file
	_, err = srcFile.Write(srcContent)
	if err != nil {
		t.Fatalf("Failed to write content to source file: %v", err)
	}

	// Create a temporary destination file
	dstFile, err := os.CreateTemp("", "destination-file")
	if err != nil {
		t.Fatalf("Failed to create temporary destination file: %v", err)
	}
	defer os.Remove(dstFile.Name())
	defer dstFile.Close()

	// Call the method under test
	err = ddcio.CopyFile(srcFile.Name(), dstFile.Name())
	if err != nil {
		t.Fatalf("CopyFile failed: %v", err)
	}

	// Read the content of the destination file
	dstContent, err := os.ReadFile(dstFile.Name())
	if err != nil {
		t.Fatalf("Failed to read destination file: %v", err)
	}

	// Compare the content of the source and destination files
	if string(dstContent) != string(srcContent) {
		t.Errorf("Copied content doesn't match source content")
	}
}

func TestCopyDir(t *testing.T) {
	srcDir, err := os.MkdirTemp("", "source-dir")
	if err != nil {
		t.Fatalf("Failed to create temporary source directory: %v", err)
	}
	defer os.RemoveAll(srcDir)

	// Create a subdirectory within the source directory
	subDir := filepath.Join(srcDir, "subdir")
	err = os.Mkdir(subDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create subdirectory: %v", err)
	}

	// Create a temporary file within the source directory
	srcFile := filepath.Join(srcDir, "file.txt")
	file, err := os.Create(srcFile)
	if err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}
	defer file.Close()

	_, err = file.WriteString("This is the source file content")
	if err != nil {
		t.Fatalf("Failed to write content to source file: %v", err)
	}

	dstDir, err := os.MkdirTemp("", "destination-dir")
	if err != nil {
		t.Fatalf("Failed to create temporary destination directory: %v", err)
	}
	defer os.RemoveAll(dstDir)

	// Call the method under test
	err = ddcio.CopyDir(srcDir, dstDir)
	if err != nil {
		t.Fatalf("CopyDir failed: %v", err)
	}

	// Verify the copied directory structure
	subDirExists, err := exists(filepath.Join(dstDir, "subdir"))
	if err != nil {
		t.Fatalf("Failed to check subdirectory existence: %v", err)
	}
	if !subDirExists {
		t.Errorf("Copied subdirectory does not exist")
	}

	dstFileExists, err := exists(filepath.Join(dstDir, "file.txt"))
	if err != nil {
		t.Fatalf("Failed to check destination file existence: %v", err)
	}
	if !dstFileExists {
		t.Errorf("Copied file does not exist")
	}
}

func exists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func TestEnsureClose(t *testing.T) {
	closed := false
	ddcio.EnsureClose("myTest", func() error {
		closed = true
		return nil
	})
	if !closed {
		t.Error("expected the close to be executed but it was not")
	}

	expectedText := "FAILED BADLY!!!!"
	failedClose := func() error {
		return errors.New(expectedText)
	}
	expectedFile := "my_long_file_name.txt"

	// so the simplelogger output will be captured
	simplelog.InitLogger(2)
	ddcio.EnsureClose(expectedFile, failedClose)

	raw, err := os.ReadFile(simplelog.GetLogLoc())
	if err != nil {
		t.Fatal(err)
	}
	out := string(raw)
	if !strings.Contains(out, expectedText) {
		t.Errorf("expected error text''%v' was not captured in %v", expectedFile, out)
	}

	if !strings.Contains(out, expectedFile) {
		t.Errorf("expected error text''%v' was not captured in %v", expectedFile, out)
	}
}
