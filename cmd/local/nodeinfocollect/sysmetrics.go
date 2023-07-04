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
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"strings"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/nodeinfocollect/gopsmetrics"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/nodeinfocollect/metrics"
)

type Args struct {
	IntervalSeconds int
	DurationSeconds int
	OutFile         string
}

// SystemMetricsRow represents a row of system metrics data.
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
	FreeRAMMB           int64     `json:"freeRAMMB"`
	CachedRAMMB         int64     `json:"cachedRAMMB"`
}

// CollectionParams includes all the necessary parameters to complete a collection
type CollectionParams struct {
	IntervalSeconds int
	DurationSeconds int
	RowWriter       func(SystemMetricsRow) error
}

func CollectSystemMetrics(params CollectionParams, sleeper func(time.Duration), metrics metrics.Collector) error {
	if params.DurationSeconds < 1 {
		return fmt.Errorf("duration must be at least 1 second %v", params.DurationSeconds)
	}
	if params.IntervalSeconds < 1 {
		return fmt.Errorf("interval must be at least 1 second %v", params.IntervalSeconds)
	}
	interval := time.Second * time.Duration(params.IntervalSeconds)
	iterations := params.DurationSeconds / params.IntervalSeconds
	if iterations < 1 {
		return fmt.Errorf("interval of %v cannot be greater than the duration of %v", params.IntervalSeconds, params.DurationSeconds)
	}

	prevDiskIO, err := metrics.IOCounters()
	if err != nil {
		return err
	}
	prevCPUTimes, err := metrics.Times()
	if err != nil {
		return err
	}
	for i := 0; i < iterations; i++ {
		// Sleep
		sleeper(interval)

		// CPU Times
		cpuTimes, err := metrics.Times()
		if err != nil {
			return err
		}

		// Memory
		memoryInfo, err := metrics.VirtualMemory()
		if err != nil {
			return err
		}

		// Disk I/O
		diskIO, err := metrics.IOCounters()
		if err != nil {
			return err
		}

		var weightedIOTime, totalIOs uint64
		var readBytes, writeBytes float64
		for i, io := range diskIO {
			p := prevDiskIO[i]
			weightedIOTime += io.WeightedIO - p.WeightedIO
			totalIOs += io.IoTime - p.IoTime

			if prev, ok := prevDiskIO[io.Name]; ok {
				readBytes += float64(io.ReadBytes - prev.ReadBytes)
				writeBytes += float64(io.WriteBytes - prev.WriteBytes)
			}
		}
		prevDiskIO = diskIO
		total := getTotalTime(cpuTimes, prevCPUTimes)
		var queueDepth float64
		var diskLatency float64
		if weightedIOTime > 0 {
			queueDepth = round(float64(weightedIOTime) / 1000)
			diskLatency = round(float64(weightedIOTime) / float64(totalIOs))
		}

		row := SystemMetricsRow{}
		row.CollectionTimeStamp = time.Now()
		user := cpuTimes.User - prevCPUTimes.User
		if user > 0 {
			row.UserCPUPercent = round((user / total) * 100)
		}
		system := cpuTimes.System - prevCPUTimes.System
		if system > 0 {
			row.SystemCPUPercent = round((system / total) * 100)
		}
		idle := cpuTimes.Idle - prevCPUTimes.Idle
		if idle > 0 {
			row.IdleCPUPercent = round((idle / total) * 100)
		}
		nice := cpuTimes.Nice - prevCPUTimes.Nice
		if nice > 0 {
			row.NiceCPUPercent = round((nice / total) * 100)
		}
		iowait := cpuTimes.Iowait - prevCPUTimes.Iowait
		if iowait > 0 {
			row.IOWaitCPUPercent = round((iowait / total) * 100)
		}

		irq := cpuTimes.Irq - prevCPUTimes.Irq
		if irq > 0 {
			row.IRQCPUPercent = round((irq / total) * 100)
		}

		softIRQ := cpuTimes.Softirq - prevCPUTimes.Softirq
		if softIRQ > 0 {
			row.SoftIRQCPUPercent = round((softIRQ / total) * 100)
		}
		steal := cpuTimes.Steal - prevCPUTimes.Steal
		if steal > 0 {
			row.StealCPUPercent = round((steal / total) * 100)
		}

		guestCPU := cpuTimes.Guest - prevCPUTimes.Guest
		if guestCPU > 0 {
			row.GuestCPUPercent = round((guestCPU / total) * 100)
		}
		guestCPUNice := cpuTimes.GuestNice - prevCPUTimes.GuestNice
		if guestCPUNice > 0 {
			row.GuestNiceCPUPercent = round((guestCPUNice / total) * 100)
		}

		prevCPUTimes = cpuTimes
		row.DiskLatency = diskLatency
		row.QueueDepth = queueDepth

		var memoryFreeMB float64
		if memoryInfo.Available > 0 {
			memoryFreeMB = round(float64(memoryInfo.Available) / (1024 * 1024))
		}
		row.FreeRAMMB = int64(memoryFreeMB)

		var memoryCachedMB float64
		if memoryCachedMB > 0 {
			memoryCachedMB = round(float64(memoryInfo.Cached) / (1024 * 1024))
		}
		row.CachedRAMMB = int64(memoryCachedMB)

		if err := params.RowWriter(row); err != nil {
			return err
		}
	}
	return nil
}

func SystemMetrics(args Args) error {
	var w io.Writer
	var rowWriter func(SystemMetricsRow) error
	var cleanup func() error
	outputFile := args.OutFile

	if strings.HasSuffix(outputFile, ".json") {
		f, err := os.Create(path.Clean(outputFile))
		if err != nil {
			return fmt.Errorf("unable to create file %v due to error '%w'", outputFile, err)
		}
		w = f
		// we manually close this so we do not care that we are not handling the error
		defer f.Close()

		cleanup = func() error {
			if err := f.Close(); err != nil {
				return fmt.Errorf("unable to close metrics file %v due to error %w", outputFile, err)
			}
			return nil
		}
		//write json file
		rowWriter = func(row SystemMetricsRow) error {
			str, err := json.Marshal(&row)
			if err != nil {
				return fmt.Errorf("unable to marshal row %#v due to error %w", row, err)
			}
			txt := fmt.Sprintf("%v\n", string(str))
			_, err = f.Write([]byte(txt))
			if err != nil {
				return fmt.Errorf("unable to write to json file due to error %w", err)
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("unable to write metrics file %v due to error %w", outputFile, err)
		}
	} else {
		if outputFile == "" {
			cleanup = func() error { return nil }
			w = os.Stdout
		} else {
			f, err := os.Create(path.Clean(outputFile))
			if err != nil {
				return fmt.Errorf("unable to create file %v due to error '%w'", outputFile, err)
			}
			cleanup = func() error {
				if err := f.Close(); err != nil {
					return fmt.Errorf("unable to close metrics file %v due to error %w", outputFile, err)
				}
				return nil
			}
			w = f
			// we don't care as this is just an emergency cleanup we manually call "cleanup" which closes the file anyway
			defer f.Close()
		}

		//write metrics.txt file
		template := "%25s\t%10s\t%10s\t%10s\t%10s\t%10s\t%10s\t%10s\t%10s\t%10s\t%10s"
		floatTemplate := "%.2f"
		percentTemplate := "%.2f%%"
		txtHeader := fmt.Sprintf(template, "Timestamp", "usr %%", "sys %%", "iowait %%", "other %%", "idl %%", "Queue", "Latency (ms)", "Read (MB/s)", "Write (MB/s)", "Free Mem (GB)")
		if _, err := fmt.Fprintln(w, txtHeader); err != nil {
			return fmt.Errorf("unable to write metrics file %v due to error %w", outputFile, err)
		}
		rowWriter = func(row SystemMetricsRow) error {
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
			rowString := fmt.Sprintf(template,
				row.CollectionTimeStamp.Format(time.RFC3339),
				fmt.Sprintf(percentTemplate, row.UserCPUPercent),
				fmt.Sprintf(percentTemplate, row.SystemCPUPercent),
				fmt.Sprintf(percentTemplate, row.IOWaitCPUPercent),
				fmt.Sprintf(percentTemplate, otherCPU),
				fmt.Sprintf(percentTemplate, row.IdleCPUPercent),
				fmt.Sprintf(floatTemplate, row.QueueDepth),
				fmt.Sprintf(floatTemplate, row.DiskLatency),
				fmt.Sprintf(floatTemplate, readBytesMB),
				fmt.Sprintf(floatTemplate, writeBytesMB),
				fmt.Sprintf(floatTemplate, freeRAMGB))
			if _, err := fmt.Fprintln(w, rowString); err != nil {
				return fmt.Errorf("unable to write metrics file %v due to error %w", outputFile, err)
			}
			return nil
		}
	}
	params := CollectionParams{
		DurationSeconds: args.DurationSeconds,
		IntervalSeconds: args.IntervalSeconds,
		RowWriter:       rowWriter,
	}

	if err := CollectSystemMetrics(params, func(d time.Duration) {
		time.Sleep(d)
	}, &gopsmetrics.Collector{}); err != nil {
		return fmt.Errorf("unable to collect system metrics with error %v", err)
	}
	return cleanup()
}

func round(num float64) float64 {
	factor := math.Pow(10, float64(2))
	return math.Round(num*factor) / factor
}
func getTotalTime(c metrics.TimesStat, p metrics.TimesStat) float64 {
	current := c.User + c.System + c.Idle + c.Nice + c.Iowait + c.Irq +
		c.Softirq + c.Steal + c.Guest + c.GuestNice
	prev := p.User + p.System + p.Idle + p.Nice + p.Iowait + p.Irq +
		p.Softirq + p.Steal + p.Guest + p.GuestNice
	return current - prev
}
