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

// package nodeinfocollect has all the methods for collecting the information for nodeinfo
package nodeinfocollect

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/ddcio"
	"github.com/dremio/dremio-diagnostic-collector/pkg/shutdown"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
)

// RunCollectDiskUsage collects disk usage information and writes it to files.
// It takes a pointer to a CollectConf struct (c)
// It returns an error if any operation fails.
func RunCollectDiskUsage(c *conf.CollectConf, hook shutdown.CancelHook) error {

	diskWriter, err := os.Create(path.Clean(filepath.Join(c.NodeInfoOutDir(), "diskusage.txt")))
	if err != nil {
		return fmt.Errorf("unable to create diskusage.txt due to error %v", err)
	}
	defer func() {
		if err := diskWriter.Sync(); err != nil {
			simplelog.Warningf("unable to sync the os_info.txt file due to error: %v", err)
		}
		if err := diskWriter.Close(); err != nil {
			simplelog.Warningf("unable to close the os_info.txt file due to error: %v", err)
		}
	}()
	err = ddcio.Shell(hook, diskWriter, "df -h")
	if err != nil {
		simplelog.Warningf("unable to read df -h due to error %v", err)
	}

	// this detection only makes sense in kubernetes TODO fix this to work with more than just kubernetes
	if strings.Contains(c.NodeName(), "dremio-master") {
		rocksDbDiskUsageWriter, err := os.Create(path.Clean(filepath.Join(c.NodeInfoOutDir(), "rocksdb_disk_allocation.txt")))
		if err != nil {
			return fmt.Errorf("unable to create rocksdb_disk_allocation.txt due to error %v", err)
		}
		defer func() {
			if err := rocksDbDiskUsageWriter.Close(); err != nil {
				simplelog.Warningf("unable to close rocksdb usage writer the file maybe incomplete %v", err)
			}
		}()
		err = ddcio.Shell(hook, rocksDbDiskUsageWriter, "du -sh /opt/dremio/data/db/*")
		if err != nil {
			simplelog.Warningf("unable to write du -sh to rocksdb_disk_allocation.txt due to error %v", err)
		}

	}
	simplelog.Debugf("... Collecting Disk Usage from %v COMPLETED", c.NodeName())

	return nil
}
