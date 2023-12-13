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
	"fmt"
	"io/fs"
	"os"
)

// CheckDirectory checks if a directory exists and contains files.
// It returns an error if the directory is empty, doesn't exist, isn't a directory,
// or if there's an error reading it.
func CheckDirectory(dirPath string, fileCheck func([]fs.DirEntry) bool) error {
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
		return fmt.Errorf("the path is not a directory")
	}

	// Read the contents of the directory
	files, err := os.ReadDir(dirPath)
	if err != nil {
		return fmt.Errorf("error reading directory: %w", err)
	}

	// Check if the directory is empty
	if len(files) == 0 {
		return fmt.Errorf("directory is empty")
	}

	if !fileCheck(files) {
		return fmt.Errorf("file check function failed")
	}
	return nil
}
