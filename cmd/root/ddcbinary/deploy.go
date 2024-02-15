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
	"archive/zip"
	"embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
)

//go:embed output/ddc.zip
var binaryData embed.FS

func WriteOutDDC(targetDir string) (ddcFilePath string, err error) {
	data, err := binaryData.ReadFile("output/ddc.zip")
	if err != nil {
		// Handle error
		return "", err
	}
	outFileName := filepath.Join(targetDir, "ddc.zip")
	if err := os.WriteFile(outFileName, data, 0600); err != nil {
		return "", fmt.Errorf("unable to write file %v due to error %v", outFileName, err)
	}
	if err := Unzip(outFileName); err != nil {
		return "", fmt.Errorf("unable to unzip file %v: '%v'", outFileName, err)
	}
	//the extracted ddc file should be where the zip was, and the zip should be deleted
	return strings.TrimSuffix(outFileName, ".zip"), nil
}

// Unzip a file to a target directory.
func Unzip(src string) error {
	dest := filepath.Dir(src) // Use the directory of the zip file as the destination

	// Open the zip file
	r, err := zip.OpenReader(src)
	if err != nil {
		return fmt.Errorf("failed to open archive: %v", err)
	}
	defer func() {
		if err := r.Close(); err != nil {
			simplelog.Debugf("optional close of file failed %v failed: %v", src, err)
		}
	}()
	maxFiles := 1
	maxSize := uint64(1024 * 1024 * 50)
	totalSize := uint64(0)
	if len(r.File) > maxFiles {
		return fmt.Errorf("too many files in zip %v which are %#v", len(r.File), r.File)
	}
	// Extract all files from the zip
	for _, f := range r.File {
		// Check max total size
		totalSize += f.UncompressedSize64
		if totalSize > maxSize {
			return fmt.Errorf("total size of files in zip is too large")
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}

		// Ignore directory structure in zip - get only the file name
		_, fileName := filepath.Split(f.Name)
		fpath := filepath.Join(dest, fileName)

		// Don't create directory entries
		if !f.FileInfo().IsDir() {
			// Create a file to write the decompressed data to
			outFile, err := os.OpenFile(filepath.Clean(fpath), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
			if err != nil {
				return err
			}

			// Write the decompressed data to the file
			_, err = io.CopyN(outFile, rc, int64(f.UncompressedSize64))
			if err != nil {
				return err
			}
			if err := outFile.Close(); err != nil {
				return err
			}
		}
		if err := rc.Close(); err != nil {
			return err
		}
	}
	// release for windows
	if err := r.Close(); err != nil {
		return fmt.Errorf("unable to close zip reader %v", err)
	}

	// Delete the zip file
	err = os.Remove(src)
	if err != nil {
		return fmt.Errorf("failed to remove zip file: %v", err)
	}

	return nil
}
