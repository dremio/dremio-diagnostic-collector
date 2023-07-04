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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/nodeinfocollect/metrics"
)

type MockCollector struct {
	ioCounters    []map[string]metrics.IOCountersStat
	times         []metrics.TimesStat
	virtualMemory []*metrics.VirtualMemoryStat
	counter       int
}

func (m *MockCollector) IOCounters() (map[string]metrics.IOCountersStat, error) {
	return m.ioCounters[m.counter], nil
}

func (m *MockCollector) Times() (metrics.TimesStat, error) {
	return m.times[m.counter], nil
}

func (m *MockCollector) VirtualMemory() (*metrics.VirtualMemoryStat, error) {
	return m.virtualMemory[m.counter], nil
}

func (m *MockCollector) Tick() {
	m.counter++
}

func TestCollectSystemMetricsForEachInterval(t *testing.T) {
	called := 0
	sleeper := func(d time.Duration) {
		called++
	}
	var rows []SystemMetricsRow
	writer := func(row SystemMetricsRow) error {
		rows = append(rows, row)
		return nil
	}
	params := CollectionParams{
		IntervalSeconds: 1,
		DurationSeconds: 60,
		RowWriter:       writer,
	}
	ioCounters := make(map[string]metrics.IOCountersStat)
	ioCounters["Disk1"] = metrics.IOCountersStat{
		Name: "Disk1",
	}
	mockCollector := MockCollector{
		ioCounters:    []map[string]metrics.IOCountersStat{ioCounters},
		virtualMemory: []*metrics.VirtualMemoryStat{{}},
		times:         []metrics.TimesStat{{}},
	}
	err := CollectSystemMetrics(params, sleeper, &mockCollector)
	if err != nil {
		t.Fatalf("unable to collect metrics: %v", err)
	}

	if called != 60 {
		t.Errorf("expected 60 iterations but got %v", called)
	}

	if len(rows) != 60 {
		t.Errorf("expected 60 rows but got %v", len(rows))
	}
}

func TestCollectSystemMetricsWithNoDuration(t *testing.T) {
	called := 0
	sleeper := func(d time.Duration) {
		called++
	}
	var rows []SystemMetricsRow
	writer := func(row SystemMetricsRow) error {
		rows = append(rows, row)
		return nil
	}
	params := CollectionParams{
		IntervalSeconds: 1,
		DurationSeconds: 0,
		RowWriter:       writer,
	}
	ioCounters := make(map[string]metrics.IOCountersStat)
	ioCounters["Disk1"] = metrics.IOCountersStat{
		Name: "Disk1",
	}
	mockCollector := MockCollector{
		ioCounters:    []map[string]metrics.IOCountersStat{ioCounters},
		virtualMemory: []*metrics.VirtualMemoryStat{{}},
		times:         []metrics.TimesStat{{}},
	}
	err := CollectSystemMetrics(params, sleeper, &mockCollector)
	if err == nil {
		t.Fatal("expected an error collecting")
	}

	if called != 0 {
		t.Errorf("expected 0 iterations but got %v", called)
	}

	if len(rows) != 0 {
		t.Errorf("expected 0 rows but got %v", len(rows))
	}
}

func TestCalculatesCPUUsageCorrectly(t *testing.T) {

	var rows []SystemMetricsRow
	writer := func(row SystemMetricsRow) error {
		rows = append(rows, row)
		return nil
	}
	params := CollectionParams{
		IntervalSeconds: 1,
		DurationSeconds: 2,
		RowWriter:       writer,
	}
	ioCounters := make(map[string]metrics.IOCountersStat)
	ioCounters["Disk1"] = metrics.IOCountersStat{
		Name: "Disk1",
	}
	ioCounters2 := make(map[string]metrics.IOCountersStat)
	ioCounters2["Disk1"] = metrics.IOCountersStat{
		Name: "Disk1",
	}
	ioCounters3 := make(map[string]metrics.IOCountersStat)
	ioCounters3["Disk1"] = metrics.IOCountersStat{
		Name: "Disk1",
	}
	mockCollector := &MockCollector{
		ioCounters:    []map[string]metrics.IOCountersStat{ioCounters, ioCounters2, ioCounters3},
		virtualMemory: []*metrics.VirtualMemoryStat{{}, {}, {}},
		times: []metrics.TimesStat{
			{
				User:      19.0,
				System:    8.0,
				Idle:      45.0,
				Nice:      7.0,
				Iowait:    6.0,
				Irq:       5.0,
				Softirq:   4.0,
				Steal:     3.0,
				Guest:     2.0,
				GuestNice: 1.0,
			},
			{
				User:      19.0,
				System:    8.0,
				Idle:      145.0,
				Nice:      7.0,
				Iowait:    6.0,
				Irq:       5.0,
				Softirq:   4.0,
				Steal:     3.0,
				Guest:     2.0,
				GuestNice: 1.0,
			},
			{
				User:      29.0,
				System:    17.0,
				Idle:      196.0,
				Nice:      15.0,
				Iowait:    13.0,
				Irq:       10.0,
				Softirq:   8.0,
				Steal:     6.0,
				Guest:     4.0,
				GuestNice: 2.0,
			},
		},
	}

	err := CollectSystemMetrics(params, func(d time.Duration) {
		mockCollector.Tick()
	}, mockCollector)
	if err != nil {
		t.Fatalf("unable to collect metrics: %v", err)
	}

	if len(rows) != 2 {
		t.Errorf("expected 2 rows but got %v", len(rows))
	}
	perc := "%.2f"
	expected := "0.00"
	actual := rows[0].UserCPUPercent
	if fmt.Sprintf(perc, actual) != expected {
		t.Errorf("user cpu %v but expected %v", actual, expected)
	}

	expected = "0.00"
	actual = rows[0].SystemCPUPercent
	if fmt.Sprintf(perc, actual) != expected {
		t.Errorf("system cpu %v but expected %v", actual, expected)
	}

	expected = "0.00"
	actual = rows[0].StealCPUPercent
	if fmt.Sprintf(perc, actual) != expected {
		t.Errorf("steal cpu %v but expected %v", actual, expected)
	}

	expected = "100.00"
	actual = rows[0].IdleCPUPercent
	if fmt.Sprintf(perc, actual) != expected {
		t.Errorf("idle cpu %v but expected %v", actual, expected)
	}

	expected = "0.00"
	actual = rows[0].GuestCPUPercent
	if fmt.Sprintf(perc, actual) != expected {
		t.Errorf("guest cpu %v but expected %v", actual, expected)
	}

	expected = "0.00"
	actual = rows[0].GuestNiceCPUPercent
	if fmt.Sprintf(perc, actual) != expected {
		t.Errorf("guest nice cpu %v but expected %v", actual, expected)
	}

	expected = "0.00"
	actual = rows[0].NiceCPUPercent
	if fmt.Sprintf(perc, actual) != expected {
		t.Errorf("nice cpu %v but expected %v", actual, expected)
	}

	expected = "0.00"
	actual = rows[0].IRQCPUPercent
	if fmt.Sprintf(perc, actual) != expected {
		t.Errorf("irq cpu %v but expected %v", actual, expected)
	}

	expected = "0.00"
	actual = rows[0].SoftIRQCPUPercent
	if fmt.Sprintf(perc, actual) != expected {
		t.Errorf("irq cpu %v but expected %v", actual, expected)
	}

	expected = "0.00"
	actual = rows[0].IOWaitCPUPercent
	if fmt.Sprintf(perc, actual) != expected {
		t.Errorf("irq cpu %v but expected %v", actual, expected)
	}

	//second row should have some measurements

	expected = "10.00"
	actual = rows[1].UserCPUPercent
	if fmt.Sprintf(perc, actual) != expected {
		t.Errorf("user cpu %v but expected %v", actual, expected)
	}

	expected = "9.00"
	actual = rows[1].SystemCPUPercent
	if fmt.Sprintf(perc, actual) != expected {
		t.Errorf("system cpu %v but expected %v", actual, expected)
	}

	expected = "3.00"
	actual = rows[1].StealCPUPercent
	if fmt.Sprintf(perc, actual) != expected {
		t.Errorf("steal cpu %v but expected %v", actual, expected)
	}

	expected = "51.00"
	actual = rows[1].IdleCPUPercent
	if fmt.Sprintf(perc, actual) != expected {
		t.Errorf("idle cpu %v but expected %v", actual, expected)
	}

	expected = "2.00"
	actual = rows[1].GuestCPUPercent
	if fmt.Sprintf(perc, actual) != expected {
		t.Errorf("guest cpu %v but expected %v", actual, expected)
	}

	expected = "1.00"
	actual = rows[1].GuestNiceCPUPercent
	if fmt.Sprintf(perc, actual) != expected {
		t.Errorf("guest nice cpu %v but expected %v", actual, expected)
	}

	expected = "8.00"
	actual = rows[1].NiceCPUPercent
	if fmt.Sprintf(perc, actual) != expected {
		t.Errorf("nice cpu %v but expected %v", actual, expected)
	}

	expected = "5.00"
	actual = rows[1].IRQCPUPercent
	if fmt.Sprintf(perc, actual) != expected {
		t.Errorf("irq cpu %v but expected %v", actual, expected)
	}

	expected = "4.00"
	actual = rows[1].SoftIRQCPUPercent
	if fmt.Sprintf(perc, actual) != expected {
		t.Errorf("irq cpu %v but expected %v", actual, expected)
	}

	expected = "7.00"
	actual = rows[1].IOWaitCPUPercent
	if fmt.Sprintf(perc, actual) != expected {
		t.Errorf("irq cpu %v but expected %v", actual, expected)
	}
}

func TestSystemMetricsIntegrationWithJson(t *testing.T) {
	outFile := filepath.Join(t.TempDir(), "metrics.json")
	args := Args{
		IntervalSeconds: 1,
		DurationSeconds: 2,
		OutFile:         outFile,
	}
	err := SystemMetrics(args)
	if err != nil {
		t.Fatal(err)
	}

	//verify each row is readable as json
	f, err := os.Open(outFile)
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(f)
	counter := 0
	for scanner.Scan() {
		counter++
		line := scanner.Text()
		obj := SystemMetricsRow{}
		err := json.Unmarshal([]byte(strings.TrimSpace(line)), &obj)
		if err != nil {
			t.Errorf("error unmarshalling line %v with error %v", line, err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatalf("unable to close test file %v", err)
	}
	if counter != 2 {
		t.Errorf("expected 2 iterations but got %v", counter)
	}

}

func TestSystemMetricsIntegrationWithTxt(t *testing.T) {
	outFile := filepath.Join(t.TempDir(), "metrics.txt")
	args := Args{
		IntervalSeconds: 1,
		DurationSeconds: 2,
		OutFile:         outFile,
	}
	err := SystemMetrics(args)
	if err != nil {
		t.Fatal(err)
	}

	//verify each has the right count of values
	f, err := os.Open(outFile)
	if err != nil {
		t.Fatal(err)
	}

	scanner := bufio.NewScanner(f)
	rowCounter := 0
	header := ""
	for scanner.Scan() {
		line := scanner.Text()
		if header == "" {
			header = line
			continue
		}
		rowCounter++
		//should have 10 tabs
		tabs := strings.Count(line, "\t")
		if tabs != 10 {
			t.Errorf("expected 10 tabs for line %v but had %v", line, tabs)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatalf("unable to close test file %v", err)
	}
	expectedHeader := "                Timestamp	    usr %%	    sys %%	 iowait %%	  other %%	    idl %%	     Queue	Latency (ms)	Read (MB/s)	Write (MB/s)	Free Mem (GB)"
	if header != expectedHeader {
		t.Errorf("expected header\n%q\nbut got\n%q", expectedHeader, header)
	}
	if rowCounter != 2 {
		t.Errorf("expected 2 rows but got %v", rowCounter)
	}
}
