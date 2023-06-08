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

// ddcio include helper code for io operations common to ddc
package ddcio

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"

	"github.com/dremio/dremio-diagnostic-collector/cmd/simplelog"
)

func DeleteDirContents(dir string) error {
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip the directory itself
		if path == dir {
			return nil
		}

		// Delete the file or directory
		err = os.RemoveAll(path)
		if err != nil {
			return err
		}

		return nil
	})

	return err
}

// CopyDir recursively copies a source directory to a destination.
// It does not copy file attributes, but does maintain directory structure.
// If the destination directory does not exist, CopyDir creates it.
// If a file with the same name exists at the destination, CopyDir overwrites it.
func CopyDir(src, dst string) error {
	src = filepath.Clean(src)
	dst = filepath.Clean(dst)

	si, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !si.IsDir() {
		return fmt.Errorf("source is not a directory: %s", src)
	}

	err = os.MkdirAll(dst, si.Mode())
	if err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err = CopyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err = CopyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}

func CopyFile(srcPath, dstPath string) error {
	// Open the source file
	srcFile, err := os.Open(path.Clean(srcPath))
	if err != nil {
		return fmt.Errorf("unable to find file %v due to error %v", srcFile, err)
	}
	defer func() {
		if err := srcFile.Close(); err != nil {
			simplelog.Warningf("unable to close %v due to error %v", path.Clean(srcPath), err)
		}
	}()

	// Create the destination file
	dstFile, err := os.Create(path.Clean(dstPath))
	if err != nil {
		return err
	}
	defer func() {
		if err := dstFile.Close(); err != nil {
			simplelog.Errorf("unable to close file %v due to error %v", path.Clean(dstPath), err)
			os.Exit(1)
		}
	}()

	// Copy the contents of the source file to the destination file
	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return err
	}

	// Flush the written data to disk
	err = dstFile.Sync()
	if err != nil {
		return err
	}

	return nil
}

// CompareFiles checks if two files have the same content by comparing their hash values.
// It returns true if the files have the same content, or false otherwise.
// An error is returned if there is a problem reading the files or calculating the hashes.
func CompareFiles(file1, file2 string) (bool, error) {
	hash1, err := CalculateFileHash(file1)
	if err != nil {
		return false, err
	}

	hash2, err := CalculateFileHash(file2)
	if err != nil {
		return false, err
	}

	return bytes.Equal(hash1, hash2), nil

}

// CalculateFileHash calculates the MD5 hash value for the given file.
// It opens the file, reads its contents, and computes the hash value.
// The calculated hash value is returned as a slice of bytes.
// An error is returned if there is a problem opening or reading the file.
func CalculateFileHash(file string) ([]byte, error) {
	f, err := os.Open(filepath.Clean(file))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return nil, err
	}

	return hash.Sum(nil), nil
}

// GetFilesInDir retrieves a list of directory entries for the given directory.
// It returns a slice of os.DirEntry representing the files and subdirectories in the directory.
// An error is returned if there is a problem reading the directory.
func GetFilesInDir(dir string) ([]os.DirEntry, error) {
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	return dirEntries, nil
}

// Shell executes a shell command with shell expansion and appends its output to the provided io.Writer.
func Shell(writer io.Writer, commandLine string) error {
	cmd := exec.Command("bash", "-c", commandLine)
	cmd.Stdout = writer
	cmd.Stderr = writer

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("command execution failed: %w", err)
	}

	return nil
}

// EnsureClose logs a failure when the close does not succeed this should not be used with "just in case closes" and should indeed signal an error
func EnsureClose(fileName string, f func() error) {
	if err := f(); err != nil {
		simplelog.Errorf("unable to finish writing file %v due to error %v", fileName, err)
	}
}
