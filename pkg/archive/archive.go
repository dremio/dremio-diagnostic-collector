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
	"path/filepath"
	"strings"

	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
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

	gzWriter := gzip.NewWriter(tarGzFile)
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
		if filePath == dest {
			return nil
		}

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
	if err := tarGzFile.Close(); err != nil {
		return fmt.Errorf("failed close to tgz file %w", err)
	}
	return nil
}
