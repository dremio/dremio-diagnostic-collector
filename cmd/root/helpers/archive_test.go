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

// collection package provides the interface for collection implementation and the actual collection execution
package helpers

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/pkg/tests"
)

var expectedOutput string

func TestTar(t *testing.T) {
	tmpDir := t.TempDir()
	testFileRaw := filepath.Join("testdata", "test.txt")
	testFile, err := filepath.Abs(testFileRaw)
	if err != nil {
		t.Fatalf("not able to get absolute path for test file %v", err)
	}
	archiveFile := tmpDir + ".tar"
	baseDir, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatalf("not able to get absolute path for base dir %v", err)
	}
	fi, err := os.Stat(testFile)
	if err != nil {
		t.Fatalf("unable to get os stat for file %v due to error %v", testFile, err)
	}
	err = TarDiag(archiveFile, baseDir, []CollectedFile{
		{
			Path: testFile,
			Size: fi.Size(),
		},
	})
	if err != nil {
		t.Fatalf("unexpected error taring file %v due to error %v", testFile, err)
	}
	tests.TarContainsFile(t, testFile, archiveFile)

	fakePath := tmpDir + "/does-not-exist/test-tar.tar"
	err = TarDiag(fakePath, tmpDir, []CollectedFile{
		{
			Path: testFile,
			Size: fi.Size(),
		},
	})
	if runtime.GOOS == "windows" {
		expectedOutput = "open " + filepath.Clean(fakePath) + ": The system cannot find the path specified."
	} else {
		expectedOutput = "open " + filepath.Clean(fakePath) + ": no such file or directory"
	}
	if err.Error() != expectedOutput {
		t.Fatalf("unmatched error response\nexpected: %v\nresponse: %v", expectedOutput, err)
	}

}

func TestGZip(t *testing.T) {
	tmpDir := t.TempDir()
	testFileRaw := filepath.Join("testdata", "test.txt")
	testFile, err := filepath.Abs(testFileRaw)
	if err != nil {
		t.Fatalf("not able to get absolute path for test file %v", err)
	}
	archiveFile := tmpDir + ".gzip"
	err = GZipDiag(archiveFile, tmpDir, testFile)
	if err != nil {
		t.Fatalf("unexpected error zipping file %v due to error %v", testFile, err)
	}

	tests.GzipContainsFile(t, testFile, archiveFile)

	fakePath := tmpDir + "/does-not-exist/test.gzip"
	err = GZipDiag(fakePath, tmpDir, testFile)
	if runtime.GOOS == "windows" {
		expectedOutput = "open " + filepath.Clean(fakePath) + ": The system cannot find the path specified."
	} else {
		expectedOutput = "open " + filepath.Clean(fakePath) + ": no such file or directory"
	}
	if err.Error() != expectedOutput {
		t.Fatalf("unmatched error response\nexpected: %v\nresponse: %v", expectedOutput, err)
	}
}
