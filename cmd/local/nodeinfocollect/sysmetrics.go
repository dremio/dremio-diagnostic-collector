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
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"text/tabwriter"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

type SystemMetricsRow struct {
	CollectionTimeStamp time.Time `json:"collectionTimestamp"`
	UserCPUPercent      float64   `json:"userCPUPercent"`
	SystemCPUPercent    float64   `json:"systmeCPUPercent"`
	IdleCPUPercent      float64   `json:"idleCPUPercent"`
	NiceCPUPercent      float64   `json:"niceCPUPercent"`
	IOWaitCPUPercent    float64   `json:"ioWaitCPUPercent"`
	IRQCPUPercent       float64   `json:"irqCPUPercent"`
	SoftIRQCPUPercent   float64   `json:"softIRQCPUPercent"`
	StealCPUPercent     float64   `json:"stealCPUPercent"`
	GuestCPUPercent     float64   `json:"guestCPUPercent"`
	GuestNiceCPUPercent float64   `json:"guestCPUNicePercent"`
	QueueDepth          float64   `json:"queueDepth"`
	DiskLatency         float64   `json:"diskLatency"`
	ReadBytes           int64     `json:"readBytes"`
	WriteBytes          int64     `json:"writeBytes"`
	FreeRAMMB           float64   `json:"freeRAMMB"`
	CachedRAMMB         float64   `json:"cachedRAMMB"`
}

func collectSystemMetrics(seconds int) (rows []SystemMetricsRow, err error) {
	iterations := seconds
	interval := time.Second

	prevDiskIO, _ := disk.IOCounters()
	for i := 0; i < iterations; i++ {
		// Sleep
		if i > 0 {
			time.Sleep(interval)
		}

		// CPU Times
		cpuTimes, _ := cpu.Times(false)

		// Memory
		memoryInfo, _ := mem.VirtualMemory()

		// Disk I/O
		diskIO, _ := disk.IOCounters()
		var weightedIOTime, totalIOs uint64
		var readBytes, writeBytes float64
		for _, io := range diskIO {
			weightedIOTime += io.WeightedIO
			totalIOs += io.IoTime

			if prev, ok := prevDiskIO[io.Name]; ok {
				readBytes += float64(io.ReadBytes-prev.ReadBytes) / 1024
				writeBytes += float64(io.WriteBytes-prev.WriteBytes) / 1024
			}
		}
		prevDiskIO = diskIO
		total := getTotalTime(cpuTimes[0])
		queueDepth := float64(weightedIOTime) / 1000
		diskLatency := float64(weightedIOTime) / float64(totalIOs)
		memoreFreeMB := float64(memoryInfo.Free) / (1024 * 1024)
		memoryCachedMB := float64(memoryInfo.Cached) / (1024 * 1024)

		row := SystemMetricsRow{
			CollectionTimeStamp: time.Now(),
			UserCPUPercent:      (cpuTimes[0].User / total) * 100,
			SystemCPUPercent:    (cpuTimes[0].System / total) * 100,
			IdleCPUPercent:      (cpuTimes[0].Idle / total) * 100,
			NiceCPUPercent:      (cpuTimes[0].Nice / total) * 100,
			IOWaitCPUPercent:    (cpuTimes[0].Iowait / total) * 100,
			IRQCPUPercent:       (cpuTimes[0].Irq / total) * 100,
			SoftIRQCPUPercent:   (cpuTimes[0].Softirq / total) * 100,
			StealCPUPercent:     (cpuTimes[0].Steal / total) * 100,
			GuestCPUPercent:     (cpuTimes[0].Guest / total) * 100,
			GuestNiceCPUPercent: (cpuTimes[0].GuestNice / total) * 100,

			DiskLatency: diskLatency,
			QueueDepth:  queueDepth,
			FreeRAMMB:   memoreFreeMB,
			CachedRAMMB: memoryCachedMB,
		}
		rows = append(rows, row)
	}
	return
}

func writeSystemMetrics(useTabWriter bool, nodeMetricsFile string, header string, rows []SystemMetricsRow, addRow func(SystemMetricsRow) (string, error)) error {
	w, err := os.Create(path.Clean(nodeMetricsFile))
	if err != nil {
		return fmt.Errorf("unable to create file %v due to error '%v'", nodeMetricsFile, err)
	}
	defer func() {
		if err := w.Close(); err != nil {
			simplelog.Debugf("unable to close file %v due to error '%v'. This is probably ok because we manually close the file as well", nodeMetricsFile, err)
		}
	}()
	var writer io.Writer
	var cleanup func()
	if useTabWriter {
		tabWriter := tabwriter.NewWriter(w, 5, 0, 1, ' ', tabwriter.AlignRight)
		writer = tabWriter
		cleanup = func() {
			if err := tabWriter.Flush(); err != nil {
				simplelog.Warningf("unable to flush metrics file %v due to error %v", nodeMetricsFile, err)
			}
		}
	} else {
		bufWriter := bufio.NewWriter(w)
		writer = bufWriter
		cleanup = func() {
			if err := bufWriter.Flush(); err != nil {
				simplelog.Warningf("unable to flush metrics file %v due to error %v", nodeMetricsFile, err)
			}
		}
	}
	_, err = writer.Write([]byte(header))
	if err != nil {
		return fmt.Errorf("unable to write output string %v due to %v", header, err)
	}
	for _, row := range rows {
		rowString, err := addRow(row)
		if err != nil {
			return fmt.Errorf("unable to convert row %#v into string due to error %v", row, err)
		}
		// Output
		_, err = writer.Write([]byte(rowString + "\n"))
		if err != nil {
			return fmt.Errorf("unable to write output string %v due to %v", row, err)
		}
	}
	cleanup()
	return w.Close()
}

func SystemMetrics(secondsToCollect int, nodeMetricsFile, nodeMetricsJSONFile string) error {
	rows, err := collectSystemMetrics(secondsToCollect)
	if err != nil {
		return fmt.Errorf("unable to collect system metrics with error %v", err)
	}

	//write metrics.txt file
	txtHeader := fmt.Sprintf("Timestamp\tUser %%\tSystem %%\tIO Wait%%\tOther %%\tIdle %%\tQueue Depth\tDisk Latency (ms)\tDisk Read (MB/s)\tDisk Write (MB/s)\t\tFree Mem (GB)\n")
	if err := writeSystemMetrics(true, nodeMetricsFile, txtHeader, rows, func(row SystemMetricsRow) (string, error) {
		otherCPU := row.NiceCPUPercent + row.IRQCPUPercent + row.SoftIRQCPUPercent + row.StealCPUPercent + row.GuestCPUPercent + row.GuestNiceCPUPercent
		var readBytesMB, writeBytesMB, freeRAMGB float64
		if row.ReadBytes > 0 {
			readBytesMB = float64(row.ReadBytes) / (1024 * 1024)
		}
		if row.WriteBytes > 0 {
			writeBytesMB = float64(row.WriteBytes) / (1024 * 1024)
		}
		if row.FreeRAMMB > 0 {
			freeRAMGB = float64(row.FreeRAMMB) / 1024.0
		}
		return fmt.Sprintf("%s\t%.2f%%\t%.2f%%\t%.2f%%\t%.2f%%\t%.2f%%\t%.2f\t%.2f\t%.2f\t%.2f\t\t%.2f",
			row.CollectionTimeStamp.Format(time.RFC3339), row.UserCPUPercent, row.SystemCPUPercent, row.IOWaitCPUPercent, otherCPU, row.IdleCPUPercent, row.QueueDepth, row.DiskLatency, readBytesMB, writeBytesMB, freeRAMGB), nil
	}); err != nil {
		return fmt.Errorf("unable to write metrics file %v due to error %v", nodeMetricsFile, err)
	}

	//write json file
	if err := writeSystemMetrics(false, nodeMetricsJSONFile, "", rows, func(row SystemMetricsRow) (string, error) {
		str, err := json.Marshal(&row)
		if err != nil {
			return "", fmt.Errorf("unable to marshal row %#v due to error %v", row, err)
		}
		return string(str), nil
	}); err != nil {
		return fmt.Errorf("unable to write metrics file %v due to error %v", nodeMetricsFile, err)
	}
	return nil
}

func getTotalTime(c cpu.TimesStat) float64 {
	return c.User + c.System + c.Idle + c.Nice + c.Iowait + c.Irq +
		c.Softirq + c.Steal + c.Guest + c.GuestNice
}
