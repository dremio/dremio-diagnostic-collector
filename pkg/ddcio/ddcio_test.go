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
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/pkg/ddcio"
)

func TestCompareFiles_WhenCompareingFilesWithTheSameContent(t *testing.T) {
	//It("should return true", func() {
	file1 := "testdata/file1.txt"
	file2 := "testdata/file1_copy.txt"
	areSame, err := ddcio.CompareFiles(file1, file2)
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	if !areSame {
		t.Error("the files don't match but they should")
	}
}

func TestCompareFiles_WhenComparingFilesWithDifferentContent(t *testing.T) {
	//It("should return false", func() {
	file1 := "testdata/file1.txt"
	file2 := "testdata/file2.txt"

	areSame, err := ddcio.CompareFiles(file1, file2)
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	if areSame {
		t.Error("the files match but they should not")
	}
}

func TestCompareFiles_WhenComparingNonExistentFiles(t *testing.T) {
	//It("should return an error", func() {
	file1 := "testdata/nonexistent1.txt"
	file2 := "testdata/nonexistent2.txt"

	areSame, err := ddcio.CompareFiles(file1, file2)
	if err == nil {
		t.Errorf("expected error but there was none")
	}
	if areSame {
		t.Error("the files match but they should not")
	}
}
