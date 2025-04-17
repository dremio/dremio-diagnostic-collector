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

// dirs provides helpers for working with directories on the filesystem
package dirs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// CheckDirectory checks if a directory exists and contains files.
// It returns an error if the directory is empty, doesn't exist, isn't a directory,
// or if there's an error reading it.
func CheckDirectory(dirPath string, fileCheck func([]fs.DirEntry) error) error {
	// Check if the directory exists
	fileInfo, err := os.Stat(dirPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("directory does not exist")
		}
		return fmt.Errorf("error checking directory: %w", err)
	}

	// Check if the path is a directory
	if !fileInfo.IsDir() {
		return errors.New("the path is not a directory")
	}

	// Read the contents of the directory
	files, err := os.ReadDir(dirPath)
	if err != nil {
		return fmt.Errorf("error reading directory: %w", err)
	}

	// Check if the directory is empty
	if len(files) == 0 {
		return errors.New("directory is empty")
	}

	if err := fileCheck(files); err != nil {
		return fmt.Errorf("file check function failed: %w", err)
	}
	return nil
}

func CheckFreeSpace(folder string, minGB uint64) error {
	var gb uint64 = 1024 * 1024 * 1024
	minBytes := minGB * gb
	b, err := GetFreeSpaceOnFileSystem(folder)
	if err != nil {
		return err
	}
	// mac uses 1024 base here, not sure if this is the right thing, but will go forward with this all the same
	if b < minBytes {
		var freeGB float64
		if b > 0 {
			freeGB = float64(b) / float64(gb)
		}
		return fmt.Errorf("there are only %.2f GB free on %v and %v GB is the minimum", freeGB, folder, minGB)
	}
	return nil
}

// FormatFreeSpaceError formats an error message for free space issues based on the collection mode.
// It provides a more specific error message suggesting to try a lighter collection mode if appropriate.
func FormatFreeSpaceError(nonDefaultFreeSpace bool, err error, collectionMode, fallbackMode string) error {
	if collectionMode == fallbackMode || nonDefaultFreeSpace {
		// If already using the lightest mode, just return the original error
		return fmt.Errorf("%w", err)
	}
	// Suggest trying a lighter collection mode
	return fmt.Errorf("%w for %v mode, try %v mode instead", err, collectionMode, fallbackMode)
}
