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
	"testing"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

func TestIOCounters(t *testing.T) {
	g := &Collector{}
	_, err := g.IOCounters()
	if err != nil {
		t.Fatal(err)
	}
}

func TestIOCountersMapping(t *testing.T) {

	g := &Collector{}
	result := make(map[string]disk.IOCountersStat)
	result["Disk1"] = disk.IOCountersStat{
		ReadBytes:  100,
		WriteBytes: 200,
		IoTime:     300,
		WeightedIO: 400,
		Name:       "Disk1",
	}
	counters := g.mapIOCounters(result)
	for k, v := range counters {
		e := result[k]
		if v.ReadBytes != e.ReadBytes {
			t.Errorf("IOCounters did not perform mapping correctly on read bytes '%v'", v.ReadBytes)
		}
		if v.WriteBytes != e.WriteBytes {
			t.Errorf("IOCounters did not perform mapping correctly on write bytes '%v'", v.WriteBytes)
		}

		if v.IoTime != e.IoTime {
			t.Errorf("IOCounters did not perform mapping correctly on iotime '%v'", v.IoTime)
		}

		if v.WeightedIO != e.WeightedIO {
			t.Errorf("IOCounters did not perform mapping correctly on weightedIO '%v'", v.WeightedIO)
		}

		if v.Name != e.Name {
			t.Errorf("IOCounters did not perform mapping correctly on name '%v'", v.Name)
		}
	}
}

func TestTimes(t *testing.T) {
	g := &Collector{}
	_, err := g.Times()

	if err != nil {
		t.Errorf("Times method failed: %v", err)
	}

}

func TestTimesMap(t *testing.T) {
	g := &Collector{}
	expected := cpu.TimesStat{
		User:      1.0,
		System:    2.0,
		Idle:      3.0,
		Nice:      4.0,
		Iowait:    5.0,
		Irq:       6.0,
		Softirq:   7.0,
		Steal:     8.0,
		Guest:     9.0,
		GuestNice: 10.0,
	}
	times := g.mapTimes(cpu.TimesStat{})

	// Check that all fields are not their zero values
	if times.User == expected.User {
		t.Errorf("Times did not perform mapping correctly on system %v", expected.User)
	}

	if times.System == expected.System {
		t.Errorf("Times did not perform mapping correctly on system %v", expected.System)
	}

	if times.Idle == expected.Idle {
		t.Errorf("Times did not perform mapping correctly on idle %v", expected.Idle)
	}

	if times.Nice == expected.Nice {
		t.Errorf("Times did not perform mapping correctly on nice %v", expected.Nice)
	}

	if times.Iowait == expected.Iowait {
		t.Errorf("Times did not perform mapping correctly on iowait %v", expected.Iowait)
	}

	if times.Irq == expected.Irq {
		t.Errorf("Times did not perform mapping correctly on irq %v", expected.Irq)
	}

	if times.Softirq == expected.Softirq {
		t.Errorf("Times did not perform mapping correctly on soft irq %v", expected.Softirq)
	}

	if times.Steal == expected.Steal {
		t.Errorf("Times did not perform mapping correctly on steal %v", expected.Steal)
	}

	if times.Guest == expected.Guest {
		t.Errorf("Times did not perform mapping correctly on guest %v", expected.Guest)
	}

	if times.GuestNice == expected.GuestNice {
		t.Errorf("Times did not perform mapping correctly on guest nice %v", expected.GuestNice)
	}
}

func TestVirtualMemory(t *testing.T) {
	g := &Collector{}
	_, err := g.VirtualMemory()

	if err != nil {
		t.Errorf("VirtualMemory method failed: %v", err)
	}

}

func TestVirtualMemoryMapping(t *testing.T) {
	g := &Collector{}
	e := &mem.VirtualMemoryStat{
		Cached:    100,
		Available: 200,
	}
	mem := g.mapVirtualMemory(e)
	if mem.Available != e.Available {
		t.Errorf("VirtualMemory did not perform mapping correctly on mem available %v", mem.Available)
	}

	if mem.Cached != e.Cached {
		t.Errorf("VirtualMemory did not perform mapping correctly on mem cached %v", mem.Cached)
	}
}
