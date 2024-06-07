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

package tests_test

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/tests"
)

func TestGzipContainsFile(t *testing.T) {
	// create a gzip file
	content := []byte("temporary file's content")
	tmpfile, err := os.CreateTemp("", "example.*.txt")
	if err != nil {
		t.Fatalf("Unexpected error creating temp file: %v", err)
	}
	defer func() {
		err := os.Remove(tmpfile.Name()) // clean up
		if err != nil {
			t.Errorf("Unexpected error removing temp file: %v", err)
		}
	}()

	if _, err := tmpfile.Write(content); err != nil {
		t.Fatalf("Unexpected error writing to temp file: %v", err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatalf("Unexpected error closing temp file: %v", err)
	}

	gzipFileName := filepath.Join(t.TempDir(), "testfile.gz")
	gzipFile, err := os.Create(gzipFileName)
	if err != nil {
		t.Fatalf("Unexpected error creating gzip file: %v", err)
	}
	defer func() {
		err := os.Remove(gzipFileName) // clean up
		if err != nil {
			t.Errorf("Unexpected error removing gzip file: %v", err)
		}
	}()

	gw := gzip.NewWriter(gzipFile)
	if _, err := gw.Write(content); err != nil {
		t.Fatalf("Unexpected error writing to gzip file: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("Unexpected error closing gzip writer: %v", err)
	}
	if err := gzipFile.Close(); err != nil {
		t.Fatalf("Unexpected error closing gzip file: %v", err)
	}

	tests.GzipContainsFile(t, tmpfile.Name(), gzipFileName)
}
func TestTgzContainsFile(t *testing.T) {
	// create a tgz file
	content := []byte("temporary file's content")
	tmpfile, err := os.CreateTemp("", "example.*.txt")
	if err != nil {
		t.Fatalf("Unexpected error creating temp file: %v", err)
	}

	defer func() {
		err := os.Remove(tmpfile.Name()) // clean up
		if err != nil {
			t.Logf("Unexpected error removing temp file: %v", err)
		}
	}()

	if _, err := tmpfile.Write(content); err != nil {
		t.Fatalf("Unexpected error writing to temp file: %v", err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatalf("Unexpected error closing temp file: %v", err)
	}

	tgzFileName := filepath.Join(t.TempDir(), "testfile.tgz")
	tarFileName := filepath.Join(t.TempDir(), "testfile.tar")
	tarfile, err := os.Create(tarFileName)
	if err != nil {
		t.Fatalf("Unexpected error creating tar file: %v", err)
	}

	tw := tar.NewWriter(tarfile)
	hdr := &tar.Header{
		Name:    tmpfile.Name(),
		Mode:    0600,
		ModTime: time.Now(),
		Size:    int64(len(content)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("Unexpected error writing header for tar file: %v", err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatalf("Unexpected error writing to tar file: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("Unexpected error closing tar writer: %v", err)
	}
	if err := tarfile.Close(); err != nil {
		t.Fatalf("Unexpected error closing tar file: %v", err)
	}

	gzipfile, err := os.Create(tgzFileName)
	if err != nil {
		t.Fatalf("Unexpected error creating tgz file: %v", err)
	}

	gw := gzip.NewWriter(gzipfile)
	tarfileToRead, err := os.Open(tarFileName)
	if err != nil {
		t.Fatalf("Unexpected error opening tar file for gzip compression: %v", err)
	}

	defer func() {
		err := tarfileToRead.Close()
		if err != nil {
			t.Logf("Unexpected error closing tar file after gzip compression: %v", err)
		}
	}()

	if _, err := io.Copy(gw, tarfileToRead); err != nil {
		t.Fatalf("Unexpected error writing tar file to gzip writer: %v", err)
	}

	if err := gw.Close(); err != nil {
		t.Fatalf("Unexpected error closing gzip writer for tgz file: %v", err)
	}

	if err := gzipfile.Close(); err != nil {
		t.Fatalf("Unexpected error closing tgz file: %v", err)
	}

	defer func() {
		err := os.Remove(tgzFileName) // clean up
		if err != nil {
			t.Logf("Unexpected error removing tgz file: %v", err)
		}
	}()

	tests.TgzContainsFile(t, tmpfile.Name(), tgzFileName, tmpfile.Name())
}
