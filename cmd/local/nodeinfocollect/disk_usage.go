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
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
	"github.com/shirou/gopsutil/v3/disk"
)

// RunCollectDiskUsaget collects disk usage information and writes it to files.
// It takes a pointer to a CollectConf struct (c)
// It returns an error if any operation fails.
func RunCollectDiskUsageFromPartitionList(c *conf.CollectConf, partitions []disk.PartitionStat) error {
	// Create a file to write disk usage information
	diskWriter, err := os.Create(path.Clean(filepath.Join(c.NodeInfoOutDir(), "diskusage.txt")))
	if err != nil {
		return fmt.Errorf("unable to create diskusage.txt due to error %w", err)
	}
	defer func() {
		// Sync and close the diskWriter file
		if err := diskWriter.Sync(); err != nil {
			simplelog.Warningf("unable to sync the diskusage.txt file due to error: %v", err)
		}
		if err := diskWriter.Close(); err != nil {
			simplelog.Warningf("unable to close the diskusage.txt file due to error: %v", err)
		}
	}()

	if duTxt, err := GetDiskUsageAllFromPartitions(partitions); err != nil {
		simplelog.Warningf("unable to write disk usage: %v", err)
	} else {
		if _, err := diskWriter.WriteString(duTxt); err != nil {
			simplelog.Warningf("unable to write disk usage: %v", err)
		}
	}

	//make sure ROCKSDBDIR ends in slash
	rocksDBDir := c.DremioRocksDBDir()
	if !strings.HasSuffix(rocksDBDir, fmt.Sprintf("%c", filepath.Separator)) {
		rocksDBDir = fmt.Sprintf("%v%c", rocksDBDir, filepath.Separator)
	}
	// get entries count and make sure the dir is not empty

	entries, err := os.ReadDir(rocksDBDir)
	if err != nil {
		return fmt.Errorf("unable to read rockdbdir: %w", err)
	}
	if len(entries) > 0 {
		// Create a file to write rocksdb disk allocation information
		rocksDbDiskUsageWriter, err := os.Create(path.Clean(filepath.Join(c.NodeInfoOutDir(), "rocksdb_disk_allocation.txt")))
		if err != nil {
			return fmt.Errorf("unable to create rocksdb_disk_allocation.txt due to error %w", err)
		}
		defer func() {
			// Close the rocksDbDiskUsageWriter file
			if err := rocksDbDiskUsageWriter.Close(); err != nil {
				simplelog.Warningf("unable to close rocksdb usage writer the file maybe incomplete %v", err)
			}
		}()
		size, err := CalculateDiskUsage(rocksDBDir)
		if err != nil {
			return fmt.Errorf("unable to calculate rocksdb usage: %w", err)
		}
		if _, err := rocksDbDiskUsageWriter.WriteString(fmt.Sprintf("%v\t%v", HumanReadableSize(uint64(size)), rocksDBDir)); err != nil {
			return fmt.Errorf("unable to write rocksdb usage: %w", err)
		}
	}
	simplelog.Debugf("... Collecting Disk Usage from %v COMPLETED", c.NodeName())
	return nil
}

func RunCollectDiskUsage(c *conf.CollectConf) error {
	partitions, err := disk.Partitions(false)
	if err != nil {
		return err
	}
	return RunCollectDiskUsageFromPartitionList(c, partitions)
}

func GetDiskUsageAllFromPartitions(partitions []disk.PartitionStat) (string, error) {
	usage := "Filesystem\tSize\tUsed\tAvailable\tUse%\tMounted on\n"
	for _, partition := range partitions {
		usageStat, err := disk.Usage(partition.Mountpoint)
		if err != nil {
			return "", err
		}

		total := usageStat.Total
		used := usageStat.Used
		available := usageStat.Free
		usedPercent := usageStat.UsedPercent

		usage += fmt.Sprintf("%s\t%s\t%s\t%s\t%.1f%%\t%s\n", partition.Device, HumanReadableSize(total), HumanReadableSize(used), HumanReadableSize(available), usedPercent, partition.Mountpoint)
	}

	return usage, nil
}

func CalculateDiskUsage(directory string) (int64, error) {
	var totalSize int64

	err := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			totalSize += info.Size()
		}

		return nil
	})

	if err != nil {
		return 0, err
	}

	return totalSize, nil
}

// HumanReadableSize is matching df -h output which tends to be base 2 and not base 10
// this will create a disconnect between the vendor disk sizes and what this reports
func HumanReadableSize(size uint64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%dB", size)
	}
	div, exp := uint64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%c", float64(size)/float64(div), "KMGTPE"[exp])
}
