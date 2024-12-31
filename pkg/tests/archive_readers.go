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

// package tests provides helper functions and mocks for running tests
package tests

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func GzipContainsFile(t *testing.T, expectedFile, gzipArchive string) {
	t.Helper()
	cleanedExpectedFile := filepath.Clean(expectedFile)
	cleanedArchiveFile := filepath.Clean(gzipArchive)
	f, err := os.Open(cleanedArchiveFile)
	if err != nil {
		t.Fatalf("unexpected error ungziping file %v: %v", cleanedExpectedFile, err)
	}
	defer func() {
		err := f.Close()
		if err != nil {
			t.Logf("WARN unable to close gzip with error %v", err)
		}
	}()
	gzipReader, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("unexpected error reading gzip format from file %v: %v", cleanedExpectedFile, err)
	}
	defer func() {
		err := gzipReader.Close()
		if err != nil {
			t.Logf("WARN unable to close gzip reader: %v", err)
		}
	}()
	var buf bytes.Buffer
	for {
		_, err := io.CopyN(&buf, gzipReader, 1024)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatal(err)
		}
	}
	if err != nil {
		t.Errorf("cannot read stat to get accurate comparison %s", err)
	}

	expectedText, err := os.ReadFile(cleanedExpectedFile)
	if err != nil {
		t.Fatal(err)
	}
	row := buf.String()
	if row != string(expectedText) {
		t.Errorf("expected content to have '%v' but was %v", string(expectedText), row)
	}
}

func ExtractGZip(t *testing.T, gzipArchive, fileName string) string {
	t.Helper()
	cleanedArchiveFile := filepath.Clean(gzipArchive)
	f, err := os.Open(cleanedArchiveFile)
	if err != nil {
		t.Fatalf("unexpected error ungziping file %v: %v", cleanedArchiveFile, err)
	}
	defer func() {
		err := f.Close()
		if err != nil {
			t.Logf("WARN unable to close gzip file: %v", err)
		}
	}()
	gzipReader, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("unexpected error reading tar.gz file %v: %v", cleanedArchiveFile, err)
	}

	newFile, err := os.Create(fileName)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err = newFile.Close()
		if err != nil {
			t.Logf("WARN unable to close %v to due error %v", fileName, err)
		}
	}()

	bufioReader := bufio.NewReader(gzipReader)
	for {
		b, err := bufioReader.Peek(1024)
		if err != nil && !errors.Is(err, io.EOF) {
			t.Fatal(err)
		}

		n, err := io.CopyN(newFile, bufioReader, int64(len(b)))
		if err != nil && !errors.Is(err, io.EOF) {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}
	return fileName
}

func ExtractGZipToTar(t *testing.T, gzipArchive string) string {
	t.Helper()
	cleanedArchiveFile := filepath.Clean(gzipArchive)
	tarFile := filepath.Clean(cleanedArchiveFile + "tar")
	return ExtractGZip(t, cleanedArchiveFile, tarFile)
}

func TgzContainsFile(t *testing.T, expectedFile, archiveFile, internalPath string) {
	t.Helper()
	tarFile := ExtractGZipToTar(t, archiveFile)
	defer os.Remove(tarFile)
	TarContainsFile(t, expectedFile, tarFile, internalPath)
}

func TarContainsFile(t *testing.T, expectedFile, archiveFile, internalPath string) {
	t.Helper()
	cleanedExpectedFile := filepath.Clean(expectedFile)
	cleanedArchiveFile := filepath.Clean(archiveFile)
	f, err := os.Open(cleanedArchiveFile)
	if err != nil {
		t.Fatalf("unexpected error untaring file %v: %v", archiveFile, err)
	}
	defer func() {
		err := f.Close()
		if err != nil {
			t.Logf("WARN unable to close tar file: %v", err)
		}
	}()

	tarReader := tar.NewReader(f)
	var found bool
	var buf bytes.Buffer
	var names []string
	var matchingFileModTime time.Time
	for {
		hdr, err := tarReader.Next()
		if err == io.EOF {
			break // End of archive
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Name == internalPath {
			found = true
		} else {
			continue
		}
		matchingFileModTime = hdr.ModTime.UTC()
		fmt.Printf("Contents of %s:\n", hdr.Name)
		names = append(names, hdr.Name)

		for {
			_, err := io.CopyN(&buf, tarReader, 1024)
			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				t.Fatal(err)
			}
		}
	}
	if !found {
		t.Errorf("expected to find the newly archived %v file but did not, inside was %v", cleanedExpectedFile, names)
	}
	// validating the mod time is not ancient
	if matchingFileModTime.Year() < 2011 {
		t.Errorf("mod time is older than 2011 for zipped up file %v, this is a bug as we expect them to be modern", matchingFileModTime)
	}
	expectedText, err := os.ReadFile(cleanedExpectedFile)
	if err != nil {
		t.Fatal(err)
	}
	row := buf.String()
	if row != string(expectedText) {
		t.Errorf("expected content to have '%v' but was '%v'", string(expectedText), row)
	}
}
