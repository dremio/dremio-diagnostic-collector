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

		if err := tarWriter.WriteHeader(header); err != nil {
			return err
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
				return fmt.Errorf("unable to copy file %v to tar due to error %v", filePath, err)
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
		return fmt.Errorf("failed close to tar file %v", err)
	}
	if err := gzWriter.Close(); err != nil {
		return fmt.Errorf("failed close to gz file %v", err)
	}
	if err := tarGzFile.Close(); err != nil {
		return fmt.Errorf("failed close to tgz file %v", err)
	}
	return nil
}
