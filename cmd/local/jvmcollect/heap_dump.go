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

// package jvmcollect handles parsing of the jvm information
package jvmcollect

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/ddcio"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
)

func RunCollectHeapDump(c *conf.CollectConf) error {
	simplelog.Debug("Capturing Java Heap Dump")
	dremioPID := c.DremioPID()
	baseName := fmt.Sprintf("%v.hprof", c.NodeName())

	hprofFile := filepath.Join(c.OutputDir(), baseName)
	hprofGzFile := fmt.Sprintf("%v.gz", hprofFile)
	if err := os.Remove(path.Clean(hprofGzFile)); err != nil {
		simplelog.Warningf("unable to remove hprof.gz file with error %v", err)
	}
	if err := os.Remove(path.Clean(hprofFile)); err != nil {
		simplelog.Warningf("unable to remove hprof file with error %v", err)
	}
	var w bytes.Buffer
	if err := ddcio.Shell(&w, fmt.Sprintf("jmap -dump:format=b,file=%v %v", hprofFile, dremioPID)); err != nil {
		return fmt.Errorf("unable to capture heap dump %v", err)
	}
	simplelog.Debugf("heap dump output %v", w.String())
	if err := ddcio.GzipFile(hprofFile, hprofGzFile); err != nil {
		return fmt.Errorf("unable to gzip heap dump file")
	}
	if err := os.Remove(path.Clean(hprofFile)); err != nil {
		simplelog.Warningf("unable to remove old hprof file, must remove manually %v", err)
	}

	dest := filepath.Join(c.HeapDumpsOutDir(), baseName+".gz")
	if err := os.Rename(path.Clean(hprofGzFile), path.Clean(dest)); err != nil {
		return fmt.Errorf("unable to move heap dump to %v due to error %v", dest, err)
	}
	return nil
}
