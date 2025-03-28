//go:build windows
// +build windows

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

package dirs

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

func GetFreeSpaceOnFileSystem(folder string) (uint64, error) {
	var freeBytesAvailable uint64
	var totalNumberOfBytes uint64
	var totalNumberOfFreeBytes uint64
	abs, err := filepath.Abs(folder)
	if err != nil {
		return 0, err
	}
	if err := windows.GetDiskFreeSpaceEx(windows.StringToUTF16Ptr(abs),
		&freeBytesAvailable, &totalNumberOfBytes, &totalNumberOfFreeBytes); err != nil {
		return 0, err
	}
	return freeBytesAvailable, nil
}
