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

// dirs_test tests the dirs package
package dirs_test

import (
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/pkg/dirs"
)

func TestCheckDirectoryFull(t *testing.T) {
	if err := dirs.CheckDirectory(filepath.Join("testdata", "full"), func(de []fs.DirEntry) bool { return len(de) > 0 }); err != nil {
		t.Errorf("expected no error %v", err)
	}
}

func TestCheckDirectoryCustomCecker(t *testing.T) {
	if err := dirs.CheckDirectory(filepath.Join("testdata", "full"), func(de []fs.DirEntry) bool { return false }); err == nil {
		t.Error("expected an error")
	}
}

func TestCheckDirectoryEmpty(t *testing.T) {
	if err := dirs.CheckDirectory(filepath.Join("testdata", "empty"), func(de []fs.DirEntry) bool { return len(de) > 0 }); err == nil {
		t.Error("expected an error")
	}
}

func TestCheckDirectoryNotPresent(t *testing.T) {
	if err := dirs.CheckDirectory(filepath.Join("testdata", "fdljk"), func(de []fs.DirEntry) bool { return true }); err == nil {
		t.Error("expected an error")
	}
}
