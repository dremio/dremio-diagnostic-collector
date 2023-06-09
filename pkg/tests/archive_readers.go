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
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func ZipContainsFile(t *testing.T, expectedFile, zipArchive string) {
	t.Helper()
	cleanedExpectedFile := filepath.Clean(expectedFile)

	tr, err := zip.OpenReader(zipArchive)
	if err != nil {
		t.Fatalf("unexpected error opening zip file %v due to error %v", cleanedExpectedFile, err)
	}
	defer func() {
		err := tr.Close()
		if err != nil {
			t.Logf("WARN: unable to close zip archive due to %v", err)
		}
	}()

	var found bool
	var buf bytes.Buffer
	var names []string
	var matchingFileModTime time.Time
	//var zero bytes.Buffer

	for _, f := range tr.File {
		//buf.Reset()
		names = append(names, f.Name)
		if string(filepath.Separator)+filepath.Base(f.Name) == string(filepath.Separator)+filepath.Base(cleanedExpectedFile) {
			found = true
			//using utc for comparison later
			matchingFileModTime = f.ModTime().UTC()

			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				err := rc.Close()
				if err != nil {
					log.Printf("WARN: unable to close zip file %v", err)
				}
			}()
			for {
				_, err := io.CopyN(&buf, rc, 1024)
				if err != nil {
					if err == io.EOF {
						break
					}
					t.Fatal(err)
				}

			}
			break
		}
	}

	if !found {
		t.Errorf("expected to find the newly archived file %v but did not in %v", expectedFile, names)
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
		t.Errorf("expected content to have '%v' but was %v", string(expectedText), row)
	}

}

func GzipContainsFile(t *testing.T, expectedFile, gzipArchive string) {
	t.Helper()
	cleanedExpectedFile := filepath.Clean(expectedFile)
	cleanedArchiveFile := filepath.Clean(gzipArchive)
	f, err := os.Open(cleanedArchiveFile)
	if err != nil {
		t.Fatalf("unexpected error ungziping file %v due to error %v", cleanedExpectedFile, err)
	}
	defer func() {
		err := f.Close()
		if err != nil {
			t.Logf("WARN unable to close gzip with error %v", err)
		}
	}()
	tr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("unexpected error reading gzip format from file %v due to error %v", cleanedExpectedFile, err)
	}
	defer func() {
		err := tr.Close()
		if err != nil {
			t.Logf("WARN unable to close gzip reader with error %v", err)
		}
	}()
	var buf bytes.Buffer
	for {
		_, err := io.CopyN(&buf, tr, 1024)
		if err != nil {
			if err == io.EOF {
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

func extraGZip(t *testing.T, gzipArchive string) string {
	t.Helper()
	cleanedArchiveFile := filepath.Clean(gzipArchive)
	f, err := os.Open(cleanedArchiveFile)
	if err != nil {
		t.Fatalf("unexpected error ungziping file %v due to error %v", cleanedArchiveFile, err)
	}
	defer func() {
		err := f.Close()
		if err != nil {
			t.Logf("WARN unable to close gzip file due to error %v", err)
		}
	}()
	tr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("unexpected error reading tar.gz file %v due to error %v", cleanedArchiveFile, err)
	}
	tarFile := filepath.Clean(cleanedArchiveFile + "tar")
	newFile, err := os.Create(tarFile)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err = newFile.Close()
		if err != nil {
			t.Logf("WARN unable to close %v to due error %v", tarFile, err)
		}
	}()

	for {
		_, err := io.CopyN(newFile, tr, 1024)
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatal(err)
		}
	}
	return tarFile
}

func TgzContainsFile(t *testing.T, expectedFile, archiveFile string) {
	t.Helper()
	tarFile := extraGZip(t, archiveFile)
	TarContainsFile(t, expectedFile, tarFile)
}

func TarContainsFile(t *testing.T, expectedFile, archiveFile string) {
	t.Helper()
	cleanedExpectedFile := filepath.Clean(expectedFile)
	cleanedArchiveFile := filepath.Clean(archiveFile)
	f, err := os.Open(cleanedArchiveFile)
	if err != nil {
		t.Fatalf("unexpected error untaring file %v due to error %v", archiveFile, err)
	}
	defer func() {
		err := f.Close()
		if err != nil {
			t.Logf("WARN unable to close tar file due to error %v", err)
		}
	}()

	tr := tar.NewReader(f)
	var found bool
	var buf bytes.Buffer
	var names []string
	var matchingFileModTime time.Time
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break // End of archive
		}
		if err != nil {
			t.Fatal(err)
		}
		matchingFileModTime = hdr.ModTime.UTC()
		fmt.Printf("Contents of %s:\n", hdr.Name)
		names = append(names, hdr.Name)
		if string(filepath.Separator)+filepath.Base(hdr.Name) == string(filepath.Separator)+filepath.Base(cleanedExpectedFile) {
			found = true
		}
		for {
			_, err := io.CopyN(&buf, tr, 1024)
			if err != nil {
				if err == io.EOF {
					break
				}
				t.Fatal(err)
			}
		}
		fmt.Println()
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
		t.Errorf("expected content to have %v but was %v", string(expectedText), row)
	}
}
