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

package archive_test

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/pkg/archive"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
)

func TestTarGzDir(t *testing.T) {
	src := filepath.Join("testdata", "targz")
	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "output.tgz")
	if err := archive.TarGzDir(src, dest); err != nil {
		t.Fatalf("unable to archive file due to error %v", err)
	}
	f, err := os.Open(dest)
	if err != nil {
		t.Fatalf("unable to continue due to error %v", err)
	}
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	outf, err := os.Create(filepath.Join(tmpDir, "output.tar"))
	if err != nil {
		t.Fatalf("unable to open file for writing with error %v", err)
	}
	if _, err = io.Copy(outf, zr); err != nil {
		t.Fatalf("unable to copy file out %v", err)
	}

	if err := zr.Close(); err != nil {
		t.Fatalf("unable to close zip read %v", err)
	}

	if err := f.Close(); err != nil {
		t.Fatalf("unable to close destination tar %v", err)
	}
	if err := outf.Close(); err != nil {
		t.Fatalf("unable to close output tar %v", err)
	}

	tarFile, err := os.Open(filepath.Join(tmpDir, "output.tar"))
	if err != nil {
		t.Fatalf("unable to read output tar file")
	}
	// Open and iterate through the files in the archive.
	tr := tar.NewReader(tarFile)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break // End of archive
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag == tar.TypeDir {
			continue
		}
		outPath := filepath.Join(tmpDir, hdr.Name)
		f, err := os.Create(outPath)
		if err != nil {
			t.Fatalf("unable to create path %v: %v", outPath, err)
		}
		if _, err := io.Copy(f, tr); err != nil {
			t.Fatalf("unable to copy file %v out: %v", hdr.Name, err)
		}
		if err := f.Close(); err != nil {
			t.Fatalf("unable to close file %v", err)
		}
	}
	if err := tarFile.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("unable to read test dir for logging %v", err)
	}
	for i, e := range entries {
		t.Logf("entry #%v - %v", i, e)
	}
	_, err = os.Stat(filepath.Join(tmpDir, "file1.txt"))
	if err != nil {
		t.Fatalf("file missing due to error %v", err)
	}

	_, err = os.Stat(filepath.Join(tmpDir, "file2.txt"))
	if err != nil {
		t.Fatalf("file missing due to error %v", err)
	}

	copied1, err := os.ReadFile(filepath.Join(tmpDir, "file1.txt"))
	if err != nil {
		t.Fatalf("not able to read coped file1.txt: %v", err)
	}
	original1, err := os.ReadFile(filepath.Join("testdata", "targz", "file1.txt"))
	if err != nil {
		t.Fatalf("unable to read origina file1.txt file: %v", err)
	}
	if !reflect.DeepEqual(copied1, original1) {
		t.Errorf("expected '%q' but got '%q'", string(original1), string(copied1))
	}
	copied2, err := os.ReadFile(filepath.Join(tmpDir, "file2.txt"))
	if err != nil {
		t.Fatalf("not able to read coped file2.txt: %v", err)
	}
	original2, err := os.ReadFile(filepath.Join("testdata", "targz", "file2.txt"))
	if err != nil {
		t.Fatalf("unable to read original file2.txt file: %v", err)
	}
	if !reflect.DeepEqual(copied2, original2) {
		t.Errorf("expected '%q' but got '%q'", string(original2), string(copied2))
	}
}

func TestTarDDC(t *testing.T) {
	src := filepath.Join("testdata", "ddctgz")
	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "output.tgz")
	if err := archive.TarDDC(src, dest, "2050101011-DDC"); err != nil {
		t.Fatalf("unable to archive file due to error %v", err)
	}
	f, err := os.Open(dest)
	if err != nil {
		t.Fatalf("unable to continue due to error %v", err)
	}
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	outf, err := os.Create(filepath.Join(tmpDir, "output.tar"))
	if err != nil {
		t.Fatalf("unable to open file for writing with error %v", err)
	}
	if _, err = io.Copy(outf, zr); err != nil {
		t.Fatalf("unable to copy file out %v", err)
	}

	if err := zr.Close(); err != nil {
		t.Fatalf("unable to close zip read %v", err)
	}

	if err := f.Close(); err != nil {
		t.Fatalf("unable to close destination tar %v", err)
	}
	if err := outf.Close(); err != nil {
		t.Fatalf("unable to close output tar %v", err)
	}

	tarFile, err := os.Open(filepath.Join(tmpDir, "output.tar"))
	if err != nil {
		t.Fatalf("unable to read output tar file")
	}
	// Open and iterate through the files in the archive.
	tr := tar.NewReader(tarFile)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break // End of archive
		}
		if err != nil {
			t.Fatal(err)
		}
		outPath := filepath.Join(tmpDir, hdr.Name)

		if hdr.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(outPath, 0700); err != nil {
				t.Fatalf("unable to create dir path %v: %v", outPath, err)
			}
			continue
		}
		f, err := os.Create(outPath)
		if err != nil {
			t.Fatalf("unable to create path %v: %v", outPath, err)
		}
		if _, err := io.Copy(f, tr); err != nil {
			t.Fatalf("unable to copy file %v out: %v", hdr.Name, err)
		}
		if err := f.Close(); err != nil {
			t.Fatalf("unable to close file %v", err)
		}
	}
	if err := tarFile.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("unable to read test dir for logging %v", err)
	}
	for i, e := range entries {
		t.Logf("entry #%v - %v", i, e)
	}
	_, err = os.Stat(filepath.Join(tmpDir, "2050101011-DDC", "file1.txt"))
	if err != nil {
		t.Fatalf("file missing due to error %v", err)
	}

	_, err = os.Stat(filepath.Join(tmpDir, "2050101011-DDC", "file2.txt"))
	if err != nil {
		t.Fatalf("file missing due to error %v", err)
	}

	copied1, err := os.ReadFile(filepath.Join(tmpDir, "2050101011-DDC", "file1.txt"))
	if err != nil {
		t.Fatalf("not able to read coped file1.txt: %v", err)
	}
	original1, err := os.ReadFile(filepath.Join("testdata", "ddctgz", "2050101011-DDC", "file1.txt"))
	if err != nil {
		t.Fatalf("unable to read origina file1.txt file: %v", err)
	}
	if !reflect.DeepEqual(copied1, original1) {
		t.Errorf("expected '%q' but got '%q'", string(original1), string(copied1))
	}
	copied2, err := os.ReadFile(filepath.Join(tmpDir, "2050101011-DDC", "file2.txt"))
	if err != nil {
		t.Fatalf("not able to read coped file2.txt: %v", err)
	}
	original2, err := os.ReadFile(filepath.Join("testdata", "ddctgz", "2050101011-DDC", "file2.txt"))
	if err != nil {
		t.Fatalf("unable to read original file2.txt file: %v", err)
	}
	if !reflect.DeepEqual(copied2, original2) {
		t.Errorf("expected '%q' but got '%q'", string(original2), string(copied2))
	}
}

func TestCopyLog(t *testing.T) {
	simplelog.InitLogger(2)
	simplelog.Infof("test for copy")
	currLog := simplelog.GetLogLoc()
	destLog := filepath.Join("testdata", "ddc.log")
	err := simplelog.CopyLog(destLog)
	if err != nil {
		t.Errorf("error copying log\n%v", err)
	}

	expected, err := os.Stat(currLog)
	if err != nil {
		t.Errorf("error opening file:\n%v", err)
	}
	actual, err := os.Stat(destLog)
	if err != nil {
		t.Errorf("error opening file:\n%v", err)
	}
	if actual.Size() != expected.Size() {
		t.Errorf("expected logs to be equal size but they were not:\nFile: %v\nSize: %v\nFile: %v\nSize: %v", currLog, expected.Size(), destLog, actual.Size())
	}
}
