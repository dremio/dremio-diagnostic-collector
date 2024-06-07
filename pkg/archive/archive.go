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

package archive

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/simplelog"
)

func TarGzDir(srcDir, dest string) error {
	return TarGzDirFiltered(srcDir, dest, func(s string) bool { return true })
}

func TarDDC(srcDir, dest, baseDDC string) error {
	summaryJSON := filepath.Join(srcDir, "summary.json")
	ddcFolder := filepath.Join(srcDir, baseDDC)
	err := simplelog.CopyLog(filepath.Join(baseDDC, "ddc.log"))
	if err != nil {
		simplelog.Warningf("unable to copy ddc.log: \n%v", err)
	}

	return TarGzDirFiltered(srcDir, dest, func(name string) bool {
		switch name {
		case summaryJSON, ddcFolder:
			return true
		}

		// Check if it's a file under tarballDir
		if strings.HasPrefix(name, ddcFolder) {
			return true
		}
		simplelog.Infof("skipping %v", name)
		return false
	})
}

func TarGzDirFiltered(srcDir, dest string, filterList func(string) bool) error {
	tarGzFile, err := os.Create(filepath.Clean(dest))
	if err != nil {
		return err
	}
	defer func() {
		if err := tarGzFile.Close(); err != nil {
			simplelog.Debugf("failed extra close to tgz file %v", err)
		}
	}()
	if err := TarGzDirFilteredStream(srcDir, tarGzFile, filterList); err != nil {
		return err
	}
	if err := tarGzFile.Close(); err != nil {
		return fmt.Errorf("failed close to tgz file %w", err)
	}
	return nil
}

func TarGzDirFilteredStream(srcDir string, w io.Writer, filterList func(string) bool) error {
	gzWriter := gzip.NewWriter(w)
	defer func() {
		if err := gzWriter.Close(); err != nil {
			simplelog.Debugf("failed extra close to gz file %v", err)
		}
	}()

	tarWriter := tar.NewWriter(gzWriter)
	defer func() {
		if err := tarWriter.Close(); err != nil {
			simplelog.Debugf("failed extra close to tar file %v", err)
		}
	}()

	srcDir = strings.TrimSuffix(srcDir, string(os.PathSeparator))

	if err := filepath.Walk(srcDir, func(filePath string, fileInfo os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		//don't try and archive the tarball itself
		//if filePath == dest {
		//		return nil
		//	}

		if !filterList(filePath) {
			return nil
		}

		// Get the relative path of the file
		relativePath, err := filepath.Rel(srcDir, filePath)
		if err != nil {
			return err
		}

		header, err := tar.FileInfoHeader(fileInfo, relativePath)
		if err != nil {
			return err
		}

		// Convert path to use forward slashes
		header.Name = filepath.ToSlash(relativePath)

		header.Size = fileInfo.Size()

		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}

		if !fileInfo.Mode().IsRegular() { //nothing more to do for non-regular
			return nil
		}

		if !fileInfo.IsDir() {
			file, err := os.Open(filepath.Clean(filePath))
			if err != nil {
				return err
			}

			defer func() {
				if err := file.Close(); err != nil {
					simplelog.Debugf("optional file close for file %v failed %v", filePath, err)
				}
			}()
			if _, err := io.Copy(tarWriter, file); err != nil {
				return fmt.Errorf("unable to copy file %v to tar due to error %w", filePath, err)
			}
			// if err := file.Close(); err != nil {
			// 	return fmt.Errorf("failed closing file %v: %v", filePath, err)
			// }
			return nil
		}

		return nil
	}); err != nil {
		return err
	}
	if err := tarWriter.Close(); err != nil {
		return fmt.Errorf("failed close to tar file %w", err)
	}
	if err := gzWriter.Close(); err != nil {
		return fmt.Errorf("failed close to gz file %w", err)
	}

	return nil
}

// Sanitize archive file pathing from "G305: Zip Slip vulnerability"
func SanitizeArchivePath(d, t string) (v string, err error) {
	v = filepath.Join(d, t)
	if strings.HasPrefix(v, filepath.Clean(d)) {
		return v, nil
	}
	return "", fmt.Errorf("%s: %s", "content filepath is tainted", t)
}

func ExtractTarGz(gzFilePath, dest string) error {
	reader, err := os.Open(path.Clean(gzFilePath))
	if err != nil {
		return err
	}
	defer reader.Close()
	return ExtractTarGzStream(reader, dest, "")
}

func ExtractTarStream(reader io.Reader, dest, pathToStrip string) error {
	tarReader := tar.NewReader(reader)

	var totalCopied int64
	for {
		header, err := tarReader.Next()
		switch {
		case err == io.EOF:
			simplelog.Infof("extraction complete %v: %v bytes", dest, totalCopied)
			return nil
		case err != nil:
			return err
		case header == nil:
			continue
		}
		var headerName = header.Name
		if pathToStrip != "" {
			simplelog.Infof("stripping %v with %v", headerName, pathToStrip)
			headerName = strings.TrimPrefix("/"+headerName, pathToStrip)
		}
		target, err := SanitizeArchivePath(dest, headerName)
		if err != nil {
			return err
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if _, err := os.Stat(target); err != nil {
				if err := os.MkdirAll(path.Clean(target), 0750); err != nil {
					return err
				}
			}
		case tar.TypeReg:
			file, err := os.OpenFile(path.Clean(target), os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil {
				simplelog.Errorf("skipping file %v due to error %v", file, err)
				continue
			}
			defer file.Close()
			for {
				copied, err := io.CopyN(file, tarReader, 1024)
				if err != nil {
					if err == io.EOF {
						break
					}
					return err
				}
				totalCopied += copied
			}
		}
	}
}

func ExtractTarGzStream(reader io.Reader, dest, pathToStrip string) error {
	gzReader, err := gzip.NewReader(reader)
	if err != nil {
		return err
	}
	defer gzReader.Close()
	return ExtractTarStream(gzReader, dest, pathToStrip)
}
