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
	"errors"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/dirs"
)

func TestCheckDirectoryFull(t *testing.T) {
	if err := dirs.CheckDirectory(filepath.Join("testdata", "full"), func(de []fs.DirEntry) error {
		if len(de) > 0 {
			return nil
		} else {
			return errors.New("failed")
		}
	}); err != nil {
		t.Errorf("expected no error %v", err)
	}
}

func TestCheckDirectoryCustomChecker(t *testing.T) {
	if err := dirs.CheckDirectory(filepath.Join("testdata", "full"), func([]fs.DirEntry) error { return errors.New("failed") }); err == nil {
		t.Error("expected an error")
	}
}

func TestCheckDirectoryEmpty(t *testing.T) {
	if err := dirs.CheckDirectory(filepath.Join("testdata", "empty"), func(de []fs.DirEntry) error {
		if len(de) > 0 {
			return nil
		} else {
			return errors.New("failed")
		}
	}); err == nil {
		t.Error("expected an error")
	}
}

func TestCheckDirectoryNotPresent(t *testing.T) {
	if err := dirs.CheckDirectory(filepath.Join("testdata", "fdljk"), func([]fs.DirEntry) error { return nil }); err == nil {
		t.Error("expected an error")
	}
}

func TestFormatFreeSpaceError(t *testing.T) {
	testCases := []struct {
		name           string
		isNonDefault   bool
		baseErr        error
		collectionMode string
		fallbackMode   string
		expectedMsg    string
	}{
		{
			name:           "Same mode",
			isNonDefault:   false,
			baseErr:        errors.New("there are only 3.50 GB free on /tmp and 5 GB is the minimum"),
			collectionMode: "light",
			fallbackMode:   "light",
			expectedMsg:    "there are only 3.50 GB free on /tmp and 5 GB is the minimum",
		},
		{
			name:           "Higher mode with fallback suggestion",
			isNonDefault:   false,
			baseErr:        errors.New("there are only 10.75 GB free on /tmp and 25 GB is the minimum"),
			collectionMode: "standard",
			fallbackMode:   "light",
			expectedMsg:    "there are only 10.75 GB free on /tmp and 25 GB is the minimum for standard mode, try light mode instead",
		},
		{
			name:           "Health check with fallback",
			isNonDefault:   false,
			baseErr:        errors.New("there are only 15.25 GB free on /tmp and 40 GB is the minimum"),
			collectionMode: "health-check",
			fallbackMode:   "light",
			expectedMsg:    "there are only 15.25 GB free on /tmp and 40 GB is the minimum for health-check mode, try light mode instead",
		},
		{
			name:           "Health check custom size",
			isNonDefault:   true,
			baseErr:        errors.New("there are only 15.25 GB free on /tmp and 40 GB is the minimum"),
			collectionMode: "health-check",
			fallbackMode:   "light",
			expectedMsg:    "there are only 15.25 GB free on /tmp and 40 GB is the minimum",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := dirs.FormatFreeSpaceError(tc.isNonDefault, tc.baseErr, tc.collectionMode, tc.fallbackMode)
			if result.Error() != tc.expectedMsg {
				t.Errorf("Expected error message: %q, got: %q", tc.expectedMsg, result.Error())
			}
		})
	}
}
