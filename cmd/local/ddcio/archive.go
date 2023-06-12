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
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/dremio/dremio-diagnostic-collector/cmd/simplelog"
)

func GzipFile(src, dst string) error {
	sourceFile, err := os.Open(path.Clean(src))
	if err != nil {
		return err
	}
	defer func() {
		if err := sourceFile.Close(); err != nil {
			simplelog.Errorf("unable to close source file %v due to error %v", sourceFile, err)
		}
	}()

	destFile, err := os.Create(path.Clean(dst))
	if err != nil {
		return err
	}
	defer func() {
		if err := destFile.Close(); err != nil {
			simplelog.Errorf("unable to close gzip file %v due to error %v", dst, err)
		}
	}()

	gzipWriter := gzip.NewWriter(destFile)
	defer gzipWriter.Close()

	_, err = io.Copy(gzipWriter, sourceFile)
	if err != nil {
		return fmt.Errorf("unable to create gzip due to error %v", err)
	}

	return nil
}
