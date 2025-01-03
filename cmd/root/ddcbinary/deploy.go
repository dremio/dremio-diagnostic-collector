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

// package ddcbinary is responsible for extracting the DDC binaries to the target directory.
package ddcbinary

import (
	"archive/zip"
	"embed"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/simplelog"
)

//go:embed output/*.zip
var binaryData embed.FS

// BinaryInfo provides location of ddc binaries
type BinaryInfo struct {
	IntelBinaryLocation string // location to find DDC x86_64 binary
	ArmBinaryLocation   string // location to find DDC aarch64 binary
}

// writeBinary writes the binary for the given architecture to the target directory.
// It returns the location of the extracted binary.
func writeBinary(arch, targetDir string) (string, error) {
	data, err := binaryData.ReadFile(fmt.Sprintf("output/ddc-%v.zip", arch))
	if err != nil {
		return "", err
	}
	// write out the zip file to the appropriate directory that matches the architecture.
	outFileDir := filepath.Join(targetDir, arch)
	if err := os.MkdirAll(outFileDir, 0o700); err != nil {
		return "", err
	}
	// write the zip file to the target directory
	outFileName := filepath.Join(outFileDir, "ddc.zip")
	if err := os.WriteFile(outFileName, data, 0o600); err != nil {
		return "", fmt.Errorf("unable to write file %v: %w", outFileName, err)
	}
	// unzip the file and remove the zip
	if err := UnzipAndRemoveZip(outFileName); err != nil {
		return "", fmt.Errorf("unable to unzip file %v: '%w'", outFileName, err)
	}
	// the extracted ddc file should be where the zip was, and the zip should be deleted
	return strings.TrimSuffix(outFileName, ".zip"), nil
}

// WriteOutDDC extracts the DDC binaries to the target directory
// for both x86_64 and aarch64 architectures.
// It returns the location of the extracted binaries.
// The correct binary is later used to copy DDC to matching
// architecture target machines.
func WriteOutDDC(targetDir string) (BinaryInfo, error) {
	// extract x86_64 binary
	intelBinary, err := writeBinary("amd64", targetDir)
	if err != nil {
		return BinaryInfo{}, err
	}
	// extract arm64 binary
	arm64Binary, err := writeBinary("arm64", targetDir)
	if err != nil {
		return BinaryInfo{}, err
	}
	return BinaryInfo{
		IntelBinaryLocation: intelBinary,
		ArmBinaryLocation:   arm64Binary,
	}, nil
}

// UnzipAndRemoveZip a file to a target directory.
func UnzipAndRemoveZip(src string) error {
	dest := filepath.Dir(src) // Use the directory of the zip file as the destination

	// Open the zip file
	r, err := zip.OpenReader(src)
	if err != nil {
		return fmt.Errorf("failed to open archive: %w", err)
	}
	defer func() {
		if err := r.Close(); err != nil {
			simplelog.Debugf("optional close of file failed %v failed: %v", src, err)
		}
	}()
	// Check the number of files in the zip, we assume one as this is only to support the DDC zip
	maxFiles := 1
	var totalSize uint64
	if len(r.File) > maxFiles {
		return fmt.Errorf("too many files in zip %v which are %#v", len(r.File), r.File)
	}
	// Set a max size to prevent zip bombs
	maxSize := uint64(1024 * 1024 * 100)
	// Extract all files from the zip
	for _, file := range r.File {
		// Check max total size
		totalSize += file.UncompressedSize64
		if totalSize > maxSize {
			return errors.New("total size of files in zip is too large")
		}

		// Open the file inside the zip
		fileReader, err := file.Open()
		if err != nil {
			return err
		}

		// Ignore directory structure in zip - get only the file name
		_, fileName := filepath.Split(file.Name)
		fpath := filepath.Join(dest, fileName)

		// Don't create directory entries
		if !file.FileInfo().IsDir() {
			// Create a file to write the decompressed data to
			outFile, err := os.OpenFile(filepath.Clean(fpath), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode())
			if err != nil {
				return err
			}
			// Close the file in case we exit prematurely
			defer func() {
				if err := outFile.Close(); err != nil {
					simplelog.Debugf("auto close for file %v failed, this is likely a non issue: %v", fpath, err)
				}
			}()

			// guard against invalid conversion
			if file.UncompressedSize64 > math.MaxInt64 {
				return fmt.Errorf("beyond max size we can deploy %v where max is %v", file.UncompressedSize64, math.MaxInt64)
			}

			// Write the decompressed data to the file
			_, err = io.CopyN(outFile, fileReader, int64(file.UncompressedSize64)) // #nosec G115
			if err != nil {
				return err
			}
			// Close the file
			if err := outFile.Close(); err != nil {
				return err
			}
		}
		// Close the file reader
		if err := fileReader.Close(); err != nil {
			return err
		}
	}
	// release for windows
	if err := r.Close(); err != nil {
		return fmt.Errorf("unable to close zip reader %w", err)
	}

	// Delete the zip file
	err = os.Remove(src)
	if err != nil {
		return fmt.Errorf("failed to remove zip file: %w", err)
	}

	return nil
}
