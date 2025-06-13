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
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/archive"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/simplelog"
)

func TestTarGzDir(t *testing.T) {
	src := filepath.Join("testdata", "targz")
	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "output.tgz")
	if err := archive.TarGzDir(src, dest); err != nil {
		t.Fatalf("unable to archive file: %v", err)
	}
	f, err := os.Open(dest)
	if err != nil {
		t.Fatalf("unable to continue: %v", err)
	}
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	outf, err := os.Create(filepath.Join(tmpDir, "output.tar"))
	if err != nil {
		t.Fatalf("unable to open file for writing with error %v", err)
	}

	if _, err = io.CopyN(outf, zr, 4096); err != nil {
		if !errors.Is(err, io.EOF) {
			t.Fatalf("unable to copy file out %v", err)
		}
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
	tarReader := tar.NewReader(tarFile)
	for {
		hdr, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break // End of archive
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag == tar.TypeDir {
			continue
		}
		outPath := filepath.Join(tmpDir, filepath.Clean(hdr.Name))
		f, err := os.Create(outPath)
		if err != nil {
			t.Fatalf("unable to create path %v: %v", outPath, err)
		}
		if _, err := io.CopyN(f, tarReader, 4096); err != nil {
			if !errors.Is(err, io.EOF) {
				t.Fatalf("unable to copy file %v out: %v", hdr.Name, err)
			}
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
		t.Fatalf("file missing: %v", err)
	}

	_, err = os.Stat(filepath.Join(tmpDir, "file2.txt"))
	if err != nil {
		t.Fatalf("file missing: %v", err)
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
		t.Fatalf("unable to archive file: %v", err)
	}
	f, err := os.Open(dest)
	if err != nil {
		t.Fatalf("unable to continue: %v", err)
	}
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	outf, err := os.Create(filepath.Join(tmpDir, "output.tar"))
	if err != nil {
		t.Fatalf("unable to open file for writing with error %v", err)
	}
	if _, err = io.CopyN(outf, zr, 4096); err != nil {
		if !errors.Is(err, io.EOF) {
			t.Fatalf("unable to copy file out %v", err)
		}
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
		if errors.Is(err, io.EOF) {
			break // End of archive
		}
		if err != nil {
			t.Fatal(err)
		}

		outPath := filepath.Join(tmpDir, filepath.Clean(hdr.Name))
		if hdr.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(outPath, 0o700); err != nil {
				t.Fatalf("unable to create dir path %v: %v", outPath, err)
			}
			continue
		}
		f, err := os.Create(outPath)
		if err != nil {
			t.Fatalf("unable to create path %v: %v", outPath, err)
		}
		if _, err := io.CopyN(f, tr, 4096); err != nil {
			if !errors.Is(err, io.EOF) {
				t.Fatalf("unable to copy file %v out: %v", hdr.Name, err)
			}
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
		t.Fatalf("file missing: %v", err)
	}

	_, err = os.Stat(filepath.Join(tmpDir, "2050101011-DDC", "file2.txt"))
	if err != nil {
		t.Fatalf("file missing: %v", err)
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
	tempDir := t.TempDir()
	simplelog.InitLoggerWithOutputDir(tempDir)
	defer simplelog.InitLoggerWithOutputDir(tempDir)
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

func TestTarGzDirWithSizeLimit(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "archive_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test source directory
	srcDir := filepath.Join(tempDir, "source")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("Failed to create source dir: %v", err)
	}

	// Use fixed seed for reproducible test results
	source := rand.NewSource(12345)
	rd := rand.New(source)

	// Create test files with known sizes
	testFiles := []struct {
		name    string
		content int
	}{
		{"file1.txt", 1000}, // 1KB
		{"file2.txt", 2000}, // 2KB
		{"file3.txt", 3000}, // 3KB
		{"file4.txt", 4000}, // 4KB
	}

	for _, tf := range testFiles {
		// Add some randomness to the content to make it more realistic
		content := strings.Repeat(fmt.Sprintf("%c", rd.Intn(26)+'a'), tf.content)

		filePath := filepath.Join(srcDir, tf.name)
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", tf.name, err)
		}
	}

	// Test with a small size limit to force splitting
	destPrefix := filepath.Join(tempDir, "test_archive")
	maxSizeBytes := int64(5000) // 5KB limit

	createdFiles, err := archive.TarGzDirWithSizeLimit(srcDir, destPrefix, maxSizeBytes)
	if err != nil {
		t.Fatalf("TarGzDirWithSizeLimit failed: %v", err)
	}

	// With 5KB limit and files totaling ~10KB content, we expect 2 archive parts
	// The exact number depends on compression and tar overhead, but should be consistent with fixed seed
	expectedFiles := 2 // Based on empirical testing with this seed and file sizes
	if len(createdFiles) != expectedFiles {
		t.Errorf("Expected exactly %d archive parts, got %d: %v", expectedFiles, len(createdFiles), createdFiles)
	}

	// Verify all created files exist and have reasonable sizes
	for i, archiveFile := range createdFiles {
		if _, err := os.Stat(archiveFile); err != nil {
			t.Errorf("Archive file %s does not exist: %v", archiveFile, err)
		}

		// Check file naming convention
		expectedName := fmt.Sprintf("%s.part%03d.tar.gz", destPrefix, i+1)
		if archiveFile != expectedName {
			t.Errorf("Expected archive name %s, got %s", expectedName, archiveFile)
		}

		// Verify file size is reasonable (not empty, not too large)
		fileInfo, err := os.Stat(archiveFile)
		if err != nil {
			t.Errorf("Failed to stat archive file %s: %v", archiveFile, err)
			continue
		}

		if fileInfo.Size() == 0 {
			t.Errorf("Archive file %s is empty", archiveFile)
		}

		// The compressed size should be less than our limit (with some tolerance for compression overhead)
		if fileInfo.Size() > maxSizeBytes+2000 { // Allow 2KB overhead for compression/headers
			t.Errorf("Archive file %s size %d exceeds limit %d by too much", archiveFile, fileInfo.Size(), maxSizeBytes)
		}
	}

	// Test extraction to verify archive integrity
	for i, archiveFile := range createdFiles {
		extractDir := filepath.Join(tempDir, fmt.Sprintf("extract_%d", i))
		if err := os.MkdirAll(extractDir, 0755); err != nil {
			t.Fatalf("Failed to create extract dir: %v", err)
		}

		if err := archive.ExtractTarGz(archiveFile, extractDir); err != nil {
			t.Errorf("Failed to extract archive %s: %v", archiveFile, err)
		}

		// Verify extracted files exist (at least some files should be in each archive)
		extractedFiles, err := os.ReadDir(extractDir)
		if err != nil {
			t.Errorf("Failed to read extracted directory: %v", err)
		}

		if len(extractedFiles) == 0 {
			t.Errorf("No files extracted from archive %s", archiveFile)
		}
	}
}

func TestTarGzDirWithSizeLimitSingleFile(t *testing.T) {
	// Test with a size limit that accommodates all files in one archive
	tempDir, err := os.MkdirTemp("", "archive_test_single")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test source directory
	srcDir := filepath.Join(tempDir, "source")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("Failed to create source dir: %v", err)
	}

	// Create a small test file
	testFile := filepath.Join(srcDir, "small.txt")
	if err := os.WriteFile(testFile, []byte("small content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	destPrefix := filepath.Join(tempDir, "single_archive")
	maxSizeBytes := int64(1024 * 1024) // 1MB limit (much larger than our content)

	createdFiles, err := archive.TarGzDirWithSizeLimit(srcDir, destPrefix, maxSizeBytes)
	if err != nil {
		t.Fatalf("TarGzDirWithSizeLimit failed: %v", err)
	}

	// Should create only one file
	if len(createdFiles) != 1 {
		t.Errorf("Expected 1 archive file, got %d: %v", len(createdFiles), createdFiles)
	}

	// Verify the file exists and can be extracted
	if len(createdFiles) > 0 {
		if _, err := os.Stat(createdFiles[0]); err != nil {
			t.Errorf("Archive file does not exist: %v", err)
		}

		extractDir := filepath.Join(tempDir, "extract")
		if err := os.MkdirAll(extractDir, 0755); err != nil {
			t.Fatalf("Failed to create extract dir: %v", err)
		}

		if err := archive.ExtractTarGz(createdFiles[0], extractDir); err != nil {
			t.Errorf("Failed to extract archive: %v", err)
		}

		// Verify the extracted file
		extractedFile := filepath.Join(extractDir, "small.txt")
		content, err := os.ReadFile(extractedFile)
		if err != nil {
			t.Errorf("Failed to read extracted file: %v", err)
		}

		if string(content) != "small content" {
			t.Errorf("Extracted content mismatch: expected 'small content', got '%s'", string(content))
		}
	}
}

func TestTarGzDirWithSizeLimitInvalidInput(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "archive_test_invalid")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	srcDir := filepath.Join(tempDir, "source")
	destPrefix := filepath.Join(tempDir, "archive")

	// Test with invalid size limit
	_, err = archive.TarGzDirWithSizeLimit(srcDir, destPrefix, 0)
	if err == nil {
		t.Error("Expected error for zero size limit")
	}

	_, err = archive.TarGzDirWithSizeLimit(srcDir, destPrefix, -1)
	if err == nil {
		t.Error("Expected error for negative size limit")
	}
}

func TestTarGzDirWithSizeLimitMB(t *testing.T) {
	// Test the MB convenience function
	tempDir, err := os.MkdirTemp("", "archive_test_mb")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test source directory
	srcDir := filepath.Join(tempDir, "source")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("Failed to create source dir: %v", err)
	}

	// Create a test file
	testFile := filepath.Join(srcDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	destPrefix := filepath.Join(tempDir, "mb_archive")
	maxSizeMB := int64(1) // 1MB limit

	createdFiles, err := archive.TarGzDirWithSizeLimitMB(srcDir, destPrefix, maxSizeMB)
	if err != nil {
		t.Fatalf("TarGzDirWithSizeLimitMB failed: %v", err)
	}

	// Should create at least one file
	if len(createdFiles) == 0 {
		t.Error("Expected at least one archive file")
	}

	// Verify the file exists
	if len(createdFiles) > 0 {
		if _, err := os.Stat(createdFiles[0]); err != nil {
			t.Errorf("Archive file does not exist: %v", err)
		}
	}
}

func TestTarGzDirWithSizeLimitLargeFile(t *testing.T) {
	// Test with one large file that exceeds the size limit
	tempDir, err := os.MkdirTemp("", "archive_test_large")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test source directory
	srcDir := filepath.Join(tempDir, "source")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("Failed to create source dir: %v", err)
	}

	// Use fixed seed for reproducible test results
	source := rand.NewSource(12345)
	rd := rand.New(source)

	// Create a large file (3KB) that exceeds our 1KB limit
	largeFileData := make([]byte, 3*1024) // 3KB
	for i := range largeFileData {
		largeFileData[i] = byte(rd.Intn(256))
	}

	largeFilePath := filepath.Join(srcDir, "large_file.bin")
	if err := os.WriteFile(largeFilePath, largeFileData, 0644); err != nil {
		t.Fatalf("Failed to create large test file: %v", err)
	}

	// Archive with 1KB limit to force splitting
	destPrefix := filepath.Join(tempDir, "test_archive")
	maxSizeBytes := int64(1024) // 1KB limit

	createdFiles, err := archive.TarGzDirWithSizeLimit(srcDir, destPrefix, maxSizeBytes)
	if err != nil {
		t.Fatalf("TarGzDirWithSizeLimit failed: %v", err)
	}

	// With 3KB file and 1KB limit, should create exactly 2 archive parts
	// (based on empirical testing with this seed and compression)
	expectedFiles := 2
	if len(createdFiles) != expectedFiles {
		t.Errorf("Expected exactly %d archive parts, got %d: %v", expectedFiles, len(createdFiles), createdFiles)
	}

	// Extract all parts and verify the large file is reconstructed correctly
	extractDir := filepath.Join(tempDir, "extracted")
	if err := os.MkdirAll(extractDir, 0755); err != nil {
		t.Fatalf("Failed to create extract dir: %v", err)
	}

	for _, archiveFile := range createdFiles {
		if err := archive.ExtractTarGz(archiveFile, extractDir); err != nil {
			t.Errorf("Failed to extract archive %s: %v", archiveFile, err)
		}
	}

	// Verify the extracted file matches the original
	extractedData, err := os.ReadFile(filepath.Join(extractDir, "large_file.bin"))
	if err != nil {
		t.Fatalf("Failed to read extracted file: %v", err)
	}

	if !reflect.DeepEqual(extractedData, largeFileData) {
		t.Error("Extracted file data does not match original")
	}
}
