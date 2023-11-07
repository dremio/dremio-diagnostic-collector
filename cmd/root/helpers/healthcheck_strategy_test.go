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

// helpers package provides different functionality
package helpers

import (
	"path/filepath"
	"testing"
	"time"
)

type MockTimeService struct {
	Time time.Time
}

func (m *MockTimeService) GetNow() time.Time {
	return m.Time
}

// Tests the constructor is setting a basedir dir
func TestBaseDirHC(t *testing.T) {
	ddcfs := NewFakeFileSystem()
	now := time.Now()
	timeService := MockTimeService{
		Time: now,
	}
	testStrat, err := NewHCCopyStrategy(ddcfs, &timeService)
	if err != nil {
		t.Errorf("error when creating copy strategy: %v", err)
	}
	expected := now.Format("20060102-150405-DDC")
	actual := testStrat.BaseDir
	// Check the base dir is set on creation
	if expected != actual {
		t.Errorf("ERROR: base directory name on create: \nexpected:\t%v\nactual:\t\t%v\n", expected, actual)
	}
}

// Tests the constructor is setting a temp dir
func TestTmpDirHC(t *testing.T) {
	ddcfs := NewFakeFileSystem()
	testStrat, err := NewHCCopyStrategy(ddcfs, &MockTimeService{
		Time: time.Now(),
	})
	if err != nil {
		t.Errorf("error when creating copy strategy: %v", err)
	}
	expected := filepath.Join("tmp", "dir1", "random")
	actual := testStrat.TmpDir
	// Check the base dir is set on creation
	if expected != actual {
		t.Errorf("ERROR: tmp directory on create: \nexpected:\t%v\nactual:\t\t%v\n", expected, actual)
	}
}

// Tests the method returns the correct path
func TestGetPathHC(t *testing.T) {
	ddcfs := NewFakeFileSystem()
	testStrat, err := NewHCCopyStrategy(ddcfs, &MockTimeService{Time: time.Now()})
	if err != nil {
		t.Errorf("error when creating copy strategy: %v", err)
	}
	// Test path for coordinators
	expected := filepath.Join("tmp", "dir1", "random", testStrat.BaseDir, "log", "node1-C")
	actual, _ := testStrat.CreatePath("log", "node1", "coordinator")
	if expected != actual {
		t.Errorf("\nERROR: returned path: \nexpected:\t%v\nactual:\t\t%v\n", expected, actual)
	}
	// Test path for executors
	expected = filepath.Join("tmp", "dir1", "random", testStrat.BaseDir, "log", "node1-E")
	actual, _ = testStrat.CreatePath("log", "node1", "executors")
	if expected != actual {
		t.Errorf("\nERROR: returned path: \nexpected:\t%v\nactual:\t\t%v\n", expected, actual)
	}
}

// Test archiving of a file (which is also tested elsewhere) but in addition
// it tests the call via the selected strategy
func TestArchiveDiagHC(t *testing.T) {
	ddcfs := NewRealFileSystem()
	testStrat, err := NewHCCopyStrategy(ddcfs, &MockTimeService{Time: time.Now()})
	if err != nil {
		t.Errorf("error when creating copy strategy: %v", err)
	}
	tmpDir := t.TempDir()
	testFileRaw := filepath.Join("testdata", "test.txt")
	if testFile, err := filepath.Abs(testFileRaw); err != nil {
		t.Fatalf("not able to get absolute path for test file %v", err)
	} else {
		if _, err := ddcfs.Stat(testFile); err != nil {
			t.Fatalf("unexpected error getting file size for file %v due to error %v", testFile, err)
		}
		archiveFile := tmpDir + ".tgz"
		if err != nil {
			t.Fatalf("not able to get absolute path for testdata dir %v", err)
		}

		// Test Archive, pushes a teal test file into a zip archive
		err = testStrat.ArchiveDiag("test", archiveFile)
		if err != nil {
			t.Errorf("\nERROR: gzip file: \nexpected:\t%v\nactual:\t\t%v\n", nil, err)
		}
	}

}
