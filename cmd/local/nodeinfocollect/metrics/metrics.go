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

// metrics provides the interface to implement metrics
package metrics

type Collector interface {
	IOCounters() (map[string]IOCountersStat, error)
	Times() (TimesStat, error)
	VirtualMemory() (*VirtualMemoryStat, error)
}

type VirtualMemoryStat struct {
	// RAM available for programs to allocate
	//
	// This value is computed from the kernel specific values.
	Available uint64
	Cached    uint64
}

type TimesStat struct {
	User      float64
	System    float64
	Idle      float64
	Nice      float64
	Iowait    float64
	Irq       float64
	Softirq   float64
	Steal     float64
	Guest     float64
	GuestNice float64
}

type IOCountersStat struct {
	ReadBytes  uint64
	WriteBytes uint64
	IoTime     uint64
	WeightedIO uint64
	Name       string
}
