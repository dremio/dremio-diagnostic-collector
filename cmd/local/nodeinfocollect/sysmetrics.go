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

	prevDiskIO, err := disk.IOCounters()
	if err != nil {
		return rows, err
	}
	prevCPUTimes, err := cpu.Times(false)
	if err != nil {
		return rows, err
	}
	for i := 0; i < iterations; i++ {
		// Sleep
		time.Sleep(interval)

		// CPU Times
		cpuTimes, err := cpu.Times(false)
		if err != nil {
			return rows, err
		}

		// Memory
		memoryInfo, err := mem.VirtualMemory()
		if err != nil {
			return rows, err
		}

		// Disk I/O
		diskIO, err := disk.IOCounters()
		if err != nil {
			return rows, err
		}

		var weightedIOTime, totalIOs uint64
		var readBytes, writeBytes float64
		for i, io := range diskIO {
			p := prevDiskIO[i]
			weightedIOTime += io.WeightedIO - p.WeightedIO
			totalIOs += io.IoTime - p.IoTime

			if prev, ok := prevDiskIO[io.Name]; ok {
				readBytes += float64(io.ReadBytes-prev.ReadBytes) / 1024
				writeBytes += float64(io.WriteBytes-prev.WriteBytes) / 1024
			}
		}
		prevDiskIO = diskIO
		total := getTotalTime(cpuTimes[0], prevCPUTimes[0])
		var queueDepth float64
		var diskLatency float64
		if weightedIOTime > 0 {
			queueDepth = float64(weightedIOTime) / 1000
			diskLatency = float64(weightedIOTime) / float64(totalIOs)
		}

		memoreFreeMB := float64(memoryInfo.Free) / (1024 * 1024)
		memoryCachedMB := float64(memoryInfo.Cached) / (1024 * 1024)

		row := SystemMetricsRow{}
		row.CollectionTimeStamp = time.Now()
		user := cpuTimes[0].User - prevCPUTimes[0].User
		if user > 0 {
			row.UserCPUPercent = (user / total) * 100
		}
		system := cpuTimes[0].System - prevCPUTimes[0].System
		if system > 0 {
			row.SystemCPUPercent = (system / total) * 100
		}
		idle := cpuTimes[0].Idle - prevCPUTimes[0].Idle
		if idle > 0 {
			row.IdleCPUPercent = (idle / total) * 100
		}
		nice := cpuTimes[0].Nice - prevCPUTimes[0].Nice
		if nice > 0 {
			row.NiceCPUPercent = (nice / total) * 100
		}
		iowait := cpuTimes[0].Iowait - prevCPUTimes[0].Iowait
		if iowait > 0 {
			row.IOWaitCPUPercent = (iowait / total) * 100
		}
		irq := cpuTimes[0].Irq - prevCPUTimes[0].Irq
		if irq > 0 {
			row.IRQCPUPercent = (irq / total) * 100
		}
		softIRQ := cpuTimes[0].Softirq - prevCPUTimes[0].Softirq
		if softIRQ > 0 {
			row.SoftIRQCPUPercent = (softIRQ / total) * 100
		}
		steal := cpuTimes[0].Steal - prevCPUTimes[0].Steal
		if steal > 0 {
			row.StealCPUPercent = (steal / total) * 100
		}
		guestCPU := cpuTimes[0].Guest - prevCPUTimes[0].Guest
		if guestCPU > 0 {
			row.GuestCPUPercent = (guestCPU / total) * 100
		}
		guestCPUNice := cpuTimes[0].GuestNice - prevCPUTimes[0].GuestNice
		if guestCPUNice > 0 {
			row.GuestNiceCPUPercent = (guestCPUNice / total) * 100
		}
		prevCPUTimes = cpuTimes

		row.DiskLatency = diskLatency
		row.QueueDepth = queueDepth
		row.FreeRAMMB = memoreFreeMB
		row.CachedRAMMB = memoryCachedMB

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

func getTotalTime(c cpu.TimesStat, p cpu.TimesStat) float64 {
	current := c.User + c.System + c.Idle + c.Nice + c.Iowait + c.Irq +
		c.Softirq + c.Steal + c.Guest + c.GuestNice
	prev := p.User + p.System + p.Idle + p.Nice + p.Iowait + p.Irq +
		p.Softirq + p.Steal + p.Guest + p.GuestNice
	return current - prev
}
