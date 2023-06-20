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

// gopsmetrics implements metrics collection using gopsutil
package gopsmetrics

import (
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/nodeinfocollect/metrics"
)

type Collector struct {
}

func (g *Collector) IOCounters() (map[string]metrics.IOCountersStat, error) {
	counters, err := disk.IOCounters()
	if err != nil {
		return make(map[string]metrics.IOCountersStat), err
	}
	return g.mapIOCounters(counters), nil
}

func (g *Collector) mapIOCounters(counters map[string]disk.IOCountersStat) map[string]metrics.IOCountersStat {
	result := make(map[string]metrics.IOCountersStat)
	for k, v := range counters {
		result[k] = metrics.IOCountersStat{
			ReadBytes:  v.ReadBytes,
			WriteBytes: v.WriteBytes,
			IoTime:     v.IoTime,
			WeightedIO: v.WeightedIO,
			Name:       v.Name,
		}
	}
	return result
}

func (g *Collector) Times() (metrics.TimesStat, error) {
	c, err := cpu.Times(false)
	if err != nil {
		return metrics.TimesStat{}, err
	}
	first := c[0]
	return g.mapTimes(first), nil
}

func (g *Collector) mapTimes(cpuTime cpu.TimesStat) metrics.TimesStat {
	return metrics.TimesStat{
		User:      cpuTime.User,
		System:    cpuTime.System,
		Idle:      cpuTime.Idle,
		Nice:      cpuTime.Nice,
		Iowait:    cpuTime.Iowait,
		Irq:       cpuTime.Irq,
		Softirq:   cpuTime.Softirq,
		Steal:     cpuTime.Steal,
		Guest:     cpuTime.Guest,
		GuestNice: cpuTime.GuestNice,
	}
}

func (g *Collector) VirtualMemory() (*metrics.VirtualMemoryStat, error) {
	virt, err := mem.VirtualMemory()
	if err != nil {
		return &metrics.VirtualMemoryStat{}, err
	}
	return g.mapVirtualMemory(virt), nil
}

func (g Collector) mapVirtualMemory(virt *mem.VirtualMemoryStat) *metrics.VirtualMemoryStat {
	return &metrics.VirtualMemoryStat{
		Available: virt.Available,
		Cached:    virt.Cached,
	}
}
