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

package nodeinfocollect_test

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/nodeinfocollect"
	"github.com/shirou/gopsutil/v3/disk"
)

func createRandomFile(filename string, size int64) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	buffer := make([]byte, 1024)
	for written := int64(0); written < size; {
		n, err := rand.Read(buffer)
		if err != nil {
			return err
		}

		w, err := file.Write(buffer[:n])
		if err != nil {
			return err
		}

		written += int64(w)
	}

	return nil
}

func TestRunCollectDiskUsageFromPartitionListWithNoRocksDBDir(t *testing.T) {
	dirPath1 := filepath.Join(t.TempDir(), "my_partition1")
	if err := os.Mkdir(dirPath1, 0700); err != nil {
		t.Fatalf("unable to write dir: %v", err)
	}
	dirPath2 := filepath.Join(t.TempDir(), "my_partition2")
	if err := os.Mkdir(dirPath2, 0700); err != nil {
		t.Fatalf("unable to write dir: %v", err)
	}
	partitions := []disk.PartitionStat{
		{
			Device:     "part1",
			Mountpoint: dirPath1,
		},
		{
			Device:     "part2",
			Mountpoint: dirPath2,
		},
	}
	confPath := filepath.Join(t.TempDir(), "my_conf")
	if err := os.Mkdir(confPath, 0700); err != nil {
		t.Fatalf("unable to write dir: %v", err)
	}
	//we have it but we will not make it
	rocksDBDir := filepath.Join(dirPath1, "data")

	tmpOutDir := filepath.Join(t.TempDir(), "tmpOutDir")
	if err := os.Mkdir(tmpOutDir, 0700); err != nil {
		t.Fatalf("unable to write dir: %v", err)
	}
	nodeName := "node1"
	nodeInfo := filepath.Join(tmpOutDir, "node-info", nodeName)
	if err := os.MkdirAll(nodeInfo, 0700); err != nil {
		t.Fatalf("unable to write dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(confPath, "ddc.yaml"), []byte(fmt.Sprintf(`
dremio-rocksdb-dir: %v
tmp-output-dir: %v
node-name: %v
`,
		strings.ReplaceAll(rocksDBDir, "\\", "\\\\"),
		strings.ReplaceAll(tmpOutDir, "\\", "\\\\"),
		nodeName,
	)), 0700); err != nil {
		t.Fatalf("unable to write ddc.yaml for test %v", err)
	}
	overrides := make(map[string]string)
	c, err := conf.ReadConf(overrides, confPath)
	if err != nil {
		t.Fatalf("unable to read conf %v", err)
	}
	if err := nodeinfocollect.RunCollectDiskUsageFromPartitionList(c, partitions); err == nil {
		t.Error("expect an error when running collect")

	} else {
		if !strings.HasPrefix(err.Error(), "unable to read rockdbdir:") {
			t.Errorf("error did not match but was %v", err)
		}
	}

	//should have the partition report
	if b, err := os.ReadFile(filepath.Join(nodeInfo, "diskusage.txt")); err != nil {
		t.Errorf("unable to read df -h equivalent %v", err)
	} else {
		str := string(b)
		header := "Filesystem\tSize\tUsed\tAvailable\tUse%\tMounted on"
		tokens := strings.Split(str, "\n")
		if tokens[0] != header {
			t.Errorf("expected header of %v in string %v", header, tokens[0])
		}
		pattern := `^[^\t]+\t\d+(\.\d+)?[KMGT]?\t\d+(\.\d+)?[KMGT]?\t\d+(\.\d+)?[KMGT]?\t\d+(\.\d+)?%\t`

		match, err := regexp.MatchString(pattern, tokens[1])
		if err != nil {
			t.Fatalf("error on regex %v", err)
		}

		if !match {
			t.Errorf("expected line %v to match", tokens[1])
		}

		match, err = regexp.MatchString(pattern, tokens[2])
		if err != nil {
			t.Fatalf("error on regex %v", err)
		}

		if !match {
			t.Errorf("expected line %v to match", tokens[2])
		}

	}
}

func TestRunCollectDiskUsageFromPartitionListWithEmptyRocksDbDir(t *testing.T) {
	dirPath1 := filepath.Join(t.TempDir(), "my_partition1")
	if err := os.Mkdir(dirPath1, 0700); err != nil {
		t.Fatalf("unable to write dir: %v", err)
	}
	dirPath2 := filepath.Join(t.TempDir(), "my_partition2")
	if err := os.Mkdir(dirPath2, 0700); err != nil {
		t.Fatalf("unable to write dir: %v", err)
	}
	partitions := []disk.PartitionStat{
		{
			Device:     "part1",
			Mountpoint: dirPath1,
		},
		{
			Device:     "part2",
			Mountpoint: dirPath2,
		},
	}
	confPath := filepath.Join(t.TempDir(), "my_conf")
	if err := os.Mkdir(confPath, 0700); err != nil {
		t.Fatalf("unable to write dir: %v", err)
	}
	rocksDBDir := filepath.Join(dirPath1, "data")
	if err := os.Mkdir(rocksDBDir, 0700); err != nil {
		t.Fatalf("unable to write dir: %v", err)
	}
	tmpOutDir := filepath.Join(t.TempDir(), "tmpOutDir")
	if err := os.Mkdir(tmpOutDir, 0700); err != nil {
		t.Fatalf("unable to write dir: %v", err)
	}
	nodeName := "node1"
	nodeInfo := filepath.Join(tmpOutDir, "node-info", nodeName)
	if err := os.MkdirAll(nodeInfo, 0700); err != nil {
		t.Fatalf("unable to write dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(confPath, "ddc.yaml"), []byte(fmt.Sprintf(`
dremio-rocksdb-dir: %v
tmp-output-dir: %v
node-name: %v
`,
		strings.ReplaceAll(rocksDBDir, "\\", "\\\\"),
		strings.ReplaceAll(tmpOutDir, "\\", "\\\\"),
		nodeName,
	)), 0700); err != nil {
		t.Fatalf("unable to write ddc.yaml for test %v", err)
	}
	overrides := make(map[string]string)
	c, err := conf.ReadConf(overrides, confPath)
	if err != nil {
		t.Fatalf("unable to read conf %v", err)
	}
	if err := nodeinfocollect.RunCollectDiskUsageFromPartitionList(c, partitions); err != nil {
		t.Fatalf("expected no error when running diag but got %v", err)
	}

	//should have the partition report
	if b, err := os.ReadFile(filepath.Join(nodeInfo, "diskusage.txt")); err != nil {
		t.Errorf("unable to read df -h equivalent %v", err)
	} else {
		str := string(b)
		header := "Filesystem\tSize\tUsed\tAvailable\tUse%\tMounted on"
		tokens := strings.Split(str, "\n")
		if tokens[0] != header {
			t.Errorf("expected header of %v in string %v", header, tokens[0])
		}
		pattern := `^[^\t]+\t\d+(\.\d+)?[KMGT]?\t\d+(\.\d+)?[KMGT]?\t\d+(\.\d+)?[KMGT]?\t\d+(\.\d+)?%\t`

		match, err := regexp.MatchString(pattern, tokens[1])
		if err != nil {
			t.Fatalf("error on regex %v", err)
		}

		if !match {
			t.Errorf("expected line %v to match", tokens[1])
		}

		match, err = regexp.MatchString(pattern, tokens[2])
		if err != nil {
			t.Fatalf("error on regex %v", err)
		}

		if !match {
			t.Errorf("expected line %v to match", tokens[2])
		}

	}

	//should not have the rocksdb report
	if _, err := os.Stat(filepath.Join(nodeInfo, "rocksdb_disk_allocation.txt")); err == nil {
		t.Error("rocksdb file should not be present but we have no error which suggests it is")
		if !os.IsNotExist(err) {
			t.Errorf("expected error to be is not exist but was %v", err)
		}
	}
}

func TestRunCollectDiskUsageFromPartitionList(t *testing.T) {
	dirPath1 := filepath.Join(t.TempDir(), "my_partition1")
	if err := os.Mkdir(dirPath1, 0700); err != nil {
		t.Fatalf("unable to write dir: %v", err)
	}
	dirPath2 := filepath.Join(t.TempDir(), "my_partition2")
	if err := os.Mkdir(dirPath2, 0700); err != nil {
		t.Fatalf("unable to write dir: %v", err)
	}
	partitions := []disk.PartitionStat{
		{
			Device:     "part1",
			Mountpoint: dirPath1,
		},
		{
			Device:     "part2",
			Mountpoint: dirPath2,
		},
	}
	confPath := filepath.Join(t.TempDir(), "my_conf")
	if err := os.Mkdir(confPath, 0700); err != nil {
		t.Fatalf("unable to write dir: %v", err)
	}
	rocksDBDir := filepath.Join(dirPath1, "data")
	if err := os.Mkdir(rocksDBDir, 0700); err != nil {
		t.Fatalf("unable to write dir: %v", err)
	}
	if err := createRandomFile(filepath.Join(rocksDBDir, "my_data_file.db"), 1024); err != nil {
		t.Fatalf("error making test file: %v", filepath.ErrBadPattern)
	}
	tmpOutDir := filepath.Join(t.TempDir(), "tmpOutDir")
	if err := os.Mkdir(tmpOutDir, 0700); err != nil {
		t.Fatalf("unable to write dir: %v", err)
	}
	nodeName := "node1"
	nodeInfo := filepath.Join(tmpOutDir, "node-info", nodeName)
	if err := os.MkdirAll(nodeInfo, 0700); err != nil {
		t.Fatalf("unable to write dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(confPath, "ddc.yaml"), []byte(fmt.Sprintf(`
dremio-rocksdb-dir: %v
tmp-output-dir: %v
node-name: %v
`,
		strings.ReplaceAll(rocksDBDir, "\\", "\\\\"),
		strings.ReplaceAll(tmpOutDir, "\\", "\\\\"),
		nodeName,
	)), 0700); err != nil {
		t.Fatalf("unable to write ddc.yaml for test %v", err)
	}
	overrides := make(map[string]string)
	c, err := conf.ReadConf(overrides, confPath)
	if err != nil {
		t.Fatalf("unable to read conf %v", err)
	}
	if err := nodeinfocollect.RunCollectDiskUsageFromPartitionList(c, partitions); err != nil {
		t.Fatalf("expected no error when running diag but got %v", err)
	}

	//should have the partition report
	if b, err := os.ReadFile(filepath.Join(nodeInfo, "diskusage.txt")); err != nil {
		t.Errorf("unable to read df -h equivalent %v", err)
	} else {
		str := string(b)
		header := "Filesystem\tSize\tUsed\tAvailable\tUse%\tMounted on"
		tokens := strings.Split(str, "\n")
		if tokens[0] != header {
			t.Errorf("expected header of %v in string %v", header, tokens[0])
		}
		pattern := `^[^\t]+\t\d+(\.\d+)?[KMGT]?\t\d+(\.\d+)?[KMGT]?\t\d+(\.\d+)?[KMGT]?\t\d+(\.\d+)?%\t`

		match, err := regexp.MatchString(pattern, tokens[1])
		if err != nil {
			t.Fatalf("error on regex %v", err)
		}

		if !match {
			t.Errorf("expected line %v to match", tokens[1])
		}

		match, err = regexp.MatchString(pattern, tokens[2])
		if err != nil {
			t.Fatalf("error on regex %v", err)
		}

		if !match {
			t.Errorf("expected line %v to match", tokens[2])
		}

	}

	//should have the rocksdb report
	if b, err := os.ReadFile(filepath.Join(nodeInfo, "rocksdb_disk_allocation.txt")); err != nil {
		t.Errorf("unable to read df -h equivalent %v", err)
	} else {
		str := string(b)
		expected := fmt.Sprintf("1.0K\t%v%c", strings.ReplaceAll(rocksDBDir, "\\", "\\\\"), filepath.Separator)
		if str != expected {
			t.Errorf("expected %q but was %q", expected, str)
		}
	}
}

func TestCalculateDiskUsage(t *testing.T) {
	dirPath := filepath.Join(t.TempDir(), "my_dir_with_1k_file")
	if err := os.Mkdir(dirPath, 0700); err != nil {
		t.Fatalf("unable to write dir: %v", err)
	}
	if err := createRandomFile(filepath.Join(dirPath, "file.out"), 1024); err != nil {
		t.Fatalf("unable to write file: %v", err)
	}
	if size, err := nodeinfocollect.CalculateDiskUsage(dirPath); err != nil {
		t.Fatalf("unable to calculate disk usage: %v", err)
	} else {
		expected := int64(1024)
		if size != expected {
			t.Errorf("expected %v but was %v", expected, size)
		}
	}
}

func TestHumanSizeIsZero(t *testing.T) {
	expected := "5B"
	actual := nodeinfocollect.HumanReadableSize(5)
	if expected != actual {
		t.Errorf("expected %v actual %v", expected, actual)
	}
}

func TestHumanSizeIs1k(t *testing.T) {
	expected := "5.0K"
	actual := nodeinfocollect.HumanReadableSize(1024 * 5)
	if expected != actual {
		t.Errorf("expected %v actual %v", expected, actual)
	}
}

func TestHumanSizeIsInMB(t *testing.T) {
	expected := "5.0M"
	actual := nodeinfocollect.HumanReadableSize(1024 * 1024 * 5)
	if expected != actual {
		t.Errorf("expected %v actual %v", expected, actual)
	}
}

func TestHumanSizeIsInGB(t *testing.T) {
	expected := "5.0G"
	actual := nodeinfocollect.HumanReadableSize(1024 * 1024 * 1024 * 5)
	if expected != actual {
		t.Errorf("expected %v actual %v", expected, actual)
	}
}

func TestHumanSizeIsInTB(t *testing.T) {
	expected := "5.0T"
	actual := nodeinfocollect.HumanReadableSize(1024 * 1024 * 1024 * 1024 * 5)
	if expected != actual {
		t.Errorf("expected %v actual %v", expected, actual)
	}
}
