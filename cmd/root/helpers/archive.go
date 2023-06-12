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

// This module controls archiving files that are collected
package helpers

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/dremio/dremio-diagnostic-collector/cli"
	"github.com/dremio/dremio-diagnostic-collector/cmd/simplelog"
)

// archiveDiagDirectory supports .tgz
func ArchiveDiagDirectory(outputFile, outputDir string) error {

	// Make a complete list of collected files
	found, err := findAllFiles(outputDir)
	if err != nil {
		return fmt.Errorf("uanble to find files while archiving due to error %v", err)
	}
	// Get all file sizes
	files, err := createFileList(found)
	if err != nil {
		return fmt.Errorf("uanble to create file list of files to archive due to error %v", err)
	}
	for _, f := range files {
		simplelog.Debugf("archving %v", f)
	}
	ext := filepath.Ext(outputFile)
	if ext == ".tgz" {
		tempFile := strings.Join([]string{strings.TrimSuffix(outputFile, ext), "tar"}, ".")
		if err := TarDiag(tempFile, outputDir, files); err != nil {
			return fmt.Errorf("unable to write tar file %v due to error %v", outputFile, err)
		}
		defer func() {
			if err := os.Remove(tempFile); err != nil {
				simplelog.Warningf("unable to delete file '%v' due to '%v'", tempFile, err)
			}
		}()
		if err := GZipDiag(outputFile, outputDir, tempFile); err != nil {
			return fmt.Errorf("unable to write gz file %v due to error %v", outputFile, err)
		}
	} else {
		return fmt.Errorf("unsupported file extension %v for archival only support .tgz", ext)
	}
	return nil
}

func TarDiag(tarFileName string, baseDir string, files []CollectedFile) error {
	// Create a buffer to write our archive to.
	tarFile, err := os.Create(filepath.Clean(tarFileName))
	if err != nil {
		return err
	}
	defer func() {
		err := tarFile.Close()
		if err != nil {
			simplelog.Warningf("unable to close file %v due to error %v", tarFileName, err)
		}
	}()
	// Create a new tar archive.
	tw := tar.NewWriter(tarFile)
	defer func() {
		err := tw.Close()
		if err != nil {
			simplelog.Warningf("unable to close file %v due to error %v", tarFileName, err)
		}
	}()
	for _, collectedFile := range files {
		file := collectedFile.Path
		fi, err := os.Stat(filepath.Clean(file))
		if err != nil {
			return err
		}
		if fi.IsDir() {
			continue
		}
		simplelog.Infof("taring file %v", file)
		rf, err := os.Open(filepath.Clean(file))
		if err != nil {
			return err
		}
		defer func() {
			err := rf.Close()
			if err != nil {
				simplelog.Warningf("unable to close file %v due to error %v", tarFileName, err)
			}
		}()
		fileWithoutDir := file[len(baseDir):]
		fileWithoutDir = strings.Trim(fileWithoutDir, string(filepath.Separator))
		hdr := &tar.Header{
			Name:    fileWithoutDir,
			Mode:    0600,
			Size:    fi.Size(),
			ModTime: fi.ModTime().UTC(),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		fileBytes, err := os.ReadFile(collectedFile.Path)

		if err != nil {
			return err
		}
		_, err = tw.Write(fileBytes)
		if err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("unable to close the archive %v due to error %v", tarFile, err)
	}
	return nil
}

func GZipDiag(zipFileName string, _ string, file string) error {
	// Create a buffer to write our archive to.
	zipFile, err := os.Create(filepath.Clean(zipFileName))
	if err != nil {
		return err
	}
	defer func() {
		err := zipFile.Close()
		if err != nil {
			simplelog.Warningf("unable to close file %v due to error %v", zipFileName, err)
		}

	}()
	// Create a new gzip archive.
	w := gzip.NewWriter(zipFile)
	defer func() {
		err := w.Close()
		if err != nil {
			simplelog.Warningf("unable to close file %v due to error %v", zipFileName, err)
		}
	}()
	simplelog.Infof("gzipping file %v into %v", file, zipFileName)
	rf, err := os.Open(filepath.Clean(file))
	if err != nil {
		return err
	}
	defer func() {
		err := rf.Close()
		if err != nil {
			simplelog.Warningf("unable to close file %v due to error %v", zipFileName, err)
		}
	}()
	_, err = io.Copy(w, rf)
	if err != nil {
		return err
	}
	return nil
}

func findAllFiles(path string) ([]string, error) {
	cmd := cli.Cli{}
	f := []string{}
	out, err := cmd.Execute("find", path, "-type", "f")
	if err != nil {
		return f, err
	}
	f = strings.Split(out, "\n")
	return f, nil
}

func createFileList(foundFiles []string) (files []CollectedFile, err error) {
	for _, file := range foundFiles {
		if file == "" {
			break
		}
		g, err := os.Stat(file)
		if err != nil {
			return nil, err
		}
		files = append(files, CollectedFile{
			Path: file,
			Size: g.Size(),
		})
	}
	return files, err
}
