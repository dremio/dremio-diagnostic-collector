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

// queriesjson package contains the logic for collecting queries.json information
package queriesjson

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/simplelog"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/strutils"
)

type QueriesRow struct {
	QueryID string `json:"queryId"`
	// Context       string `json:"context"`
	QueryText string  `json:"queryText"`
	Start     float64 `json:"start"`
	// Finish        int64  `json:"finish"`
	Outcome string `json:"outcome"`
	// OutcomeReason string `json:"outcomeReason"`
	// Username      string `json:"username"`
	// InputRecords  int    `json:"inputRecords"`
	// InputBytes    int    `json:"inputBytes"`
	// OutputRecords int    `json:"outputRecords"`
	// OutputBytes   int    `json:"outputBytes"`
	// RequestType   string `json:"requestType"`
	QueryType string `json:"queryType"`
	// ParentsList             []any  `json:"parentsList"`
	// Accelerated             bool   `json:"accelerated"`
	// ReflectionRelationships []any  `json:"reflectionRelationships"`
	QueryCost float64 `json:"queryCost"`
	// QueueName               string `json:"queueName"`
	// PoolWaitTime            int    `json:"poolWaitTime"`
	// PendingTime             int    `json:"pendingTime"`
	// MetadataRetrievalTime   int    `json:"metadataRetrievalTime"`
	PlanningTime float64 `json:"planningTime"`
	// EngineStartTime         int    `json:"engineStartTime"`
	// QueuedTime              int    `json:"queuedTime"`
	// ExecutionPlanningTime float64 `json:"executionPlanningTime"`
	// StartingTime            int    `json:"startingTime"`
	RunningTime float64 `json:"runningTime"`
	// EngineName              string `json:"engineName"`
	// AttemptCount            int    `json:"attemptCount"`
	// Submitted               int64  `json:"submitted"`
	// MetadataRetrieval       int64  `json:"metadataRetrieval"`
	// PlanningStart           int    `json:"planningStart"`
	// QueryEnqueued           int64  `json:"queryEnqueued"`
	// EngineStart             int64  `json:"engineStart"`
	// ExecutionPlanningStart  int64  `json:"executionPlanningStart"`
	// ExecutionStart          int64  `json:"executionStart"`
	// ScannedDatasets         []any  `json:"scannedDatasets"`
	// ExecutionNodes          []struct {
	// 	NodeID     string `json:"nodeId"`
	// 	Hostname   string `json:"hostname"`
	// 	MaxMemUsed int    `json:"maxMemUsed"`
	// } `json:"executionNodes"`
	// ExecutionCPUTimeNs   int   `json:"executionCpuTimeNs"`
	// SetupTimeNs          int   `json:"setupTimeNs"`
	// WaitTimeNs           int   `json:"waitTimeNs"`
	// MemoryAllocated      int   `json:"memoryAllocated"`
	// StartingStart        int64 `json:"startingStart"`
	// IsTruncatedQueryText bool  `json:"isTruncatedQueryText"`
}

type HistoryJobs struct {
	RowCount int   `json:"rowCount"`
	Schema   []any `json:"schema"`
	Rows     []Row `json:"rows"`
}

type Row struct {
	JobID     string `json:"job_id"`
	Status    string `json:"status"`
	QueryType string `json:"query_type"`
	// UserName                     string  `json:"user_name"`
	// QueriedDatasets              any     `json:"queried_datasets"`
	// ScannedDatasets              any     `json:"scanned_datasets"`
	// ExecutionCPUTimeNs           int     `json:"execution_cpu_time_ns"`
	// AttemptCount                 int     `json:"attempt_count"`
	// SubmittedTs                  string  `json:"submitted_ts"`
	// AttemptStartedTs             string  `json:"attempt_started_ts"`
	// MetadataRetrievalTs          any     `json:"metadata_retrieval_ts"`
	// PlanningStartTs              any     `json:"planning_start_ts"`
	// QueryEnqueuedTs              any     `json:"query_enqueued_ts"`
	// EngineStartTs                any     `json:"engine_start_ts"`
	// ExecutionPlanningStartTs     any     `json:"execution_planning_start_ts"`
	// ExecutionStartTs             any     `json:"execution_start_ts"`
	// FinalStateTs                 string  `json:"final_state_ts"`
	SubmittedEpoch int64 `json:"submitted_epoch"`
	// AttemptStartedEpoch          int64   `json:"attempt_started_epoch"`
	// MetadataRetrievalEpoch       int     `json:"metadata_retrieval_epoch"`
	PlanningStartEpoch int `json:"planning_start_epoch"`
	// QueryEnqueuedEpoch           int     `json:"query_enqueued_epoch"`
	// EngineStartEpoch             int     `json:"engine_start_epoch"`
	// ExecutionPlanningStartEpoch  int     `json:"execution_planning_start_epoch,omitempty"`
	ExecutionStartEpoch  int     `json:"execution_start_epoch"`
	FinalStateEpoch      int64   `json:"final_state_epoch"`
	PlannerEstimatedCost float64 `json:"planner_estimated_cost"`
	// RowsScanned                  int     `json:"rows_scanned"`
	// BytesScanned                 int     `json:"bytes_scanned"`
	// RowsReturned                 int     `json:"rows_returned"`
	// BytesReturned                int     `json:"bytes_returned"`
	// Accelerated                  bool    `json:"accelerated"`
	// QueueName                    any     `json:"queue_name"`
	// Engine                       any     `json:"engine"`
	// ExecutionNodes               any     `json:"execution_nodes"`
	// MemoryAvailable              int     `json:"memory_available"`
	// ErrorMsg                     string  `json:"error_msg"`
	// Query                        string  `json:"query"`
	// QueryChunks                  any     `json:"query_chunks"`
	// ReflectionMatches            any     `json:"reflection_matches"`
	// StartingTs                   any     `json:"starting_ts"`
	// StartingEpoch                int     `json:"starting_epoch"`
	// ExecutionPlanningStart       int64   `json:"execution_planning_start_,omitempty"`
	SubmittedEpochMillis      int64 `json:"submitted_epoch_millis"`       // Used in sys.jobs_recent
	ExecutionStartEpochMillis int64 `json:"execution_start_epoch_millis"` // Used in sys.jobs_recent
	PlanningStartEpochMillis  int64 `json:"planning_start_epoch_millis"`  // Used in sys.jobs_recent
	FinalStateEpochMillis     int64 `json:"final_state_epoch_millis"`     // Used in sys.jobs_recent
}

func ReadGzFile(filename string) ([]QueriesRow, error) {
	queriesrows := []QueriesRow{}
	file, err := os.Open(path.Clean(filename))
	if err != nil {
		return queriesrows, err
	}
	defer errCheck(file.Close)

	fz, err := gzip.NewReader(file)
	if err != nil {
		return queriesrows, err
	}
	defer errCheck(fz.Close)

	scanner := *bufio.NewScanner(fz)
	maxFileSize := 100 * 1024 * 1024
	buffer := make([]byte, 0, maxFileSize)
	scanner.Buffer(buffer, maxFileSize)

	i := 0
	for scanner.Scan() {
		line := scanner.Text()
		row, err := parseLine(line, i)
		if err != nil {
			simplelog.Errorf("%v: %v", err.Error(), filename)
		} else {
			queriesrows = append(queriesrows, row)
		}
		i++
	}
	return queriesrows, err
}

func ReadJSONFile(filename string) ([]QueriesRow, error) {
	// Source: https://gist.github.com/kendellfab/7417164
	queriesrows := []QueriesRow{}
	file, err := os.Open(path.Clean(filename))
	if err != nil {
		simplelog.Errorf("can't open %v: %v", filename, err)
		return queriesrows, err
	}
	defer errCheck(file.Close)
	scanner := *bufio.NewScanner(file)
	maxFileSize := 100 * 1024 * 1024
	buffer := make([]byte, 0, maxFileSize)
	scanner.Buffer(buffer, maxFileSize)

	i := 0
	for scanner.Scan() {
		line := scanner.Text()
		row, err := parseLine(line, i)
		if err != nil {
			simplelog.Errorf("can't parse line %v from file %v: %v", line, filename, err)
		} else {
			queriesrows = append(queriesrows, row)
		}
		i++
	}
	return queriesrows, err
}

func ReadHistoryJobsJSONFile(filename string) ([]QueriesRow, error) {
	queriesrows := []QueriesRow{}
	file, err := os.Open(path.Clean(filename))
	if err != nil {
		simplelog.Errorf("can't open %v: %v", filename, err)
		return queriesrows, err
	}
	defer errCheck(file.Close)

	var bytedata []byte
	bytedata, err = io.ReadAll(file)
	if err != nil {
		simplelog.Errorf("can't read data of %v: %v", filename, err)
		return queriesrows, err
	}

	var dat HistoryJobs
	err = json.Unmarshal(bytedata, &dat)
	if err != nil {
		return queriesrows, fmt.Errorf("can't JSON unmarshall %v: %w", filename, err)
	}
	for _, line := range dat.Rows {
		row, err := parseLineJobsJSON(line)
		if err != nil {
			simplelog.Errorf("can't parse line %v from file %v: %v", row, filename, err)
		} else {
			queriesrows = append(queriesrows, row)
		}
	}
	return queriesrows, err
}

func parseLine(line string, i int) (QueriesRow, error) {
	dat := make(map[string]interface{})
	err := json.Unmarshal([]byte(line), &dat)
	if err != nil {
		return *new(QueriesRow), fmt.Errorf("queries.json line #%v: %v[...] - error: %w", i, strutils.GetEndOfString(line, 50), err)
	}
	row := new(QueriesRow)
	if val, ok := dat["queryId"]; ok {
		qid, ok := val.(string)
		if ok {
			row.QueryID = qid
		} else {
			return *new(QueriesRow), errors.New("incorrect type for 'queryId'")
		}
	} else {
		return *new(QueriesRow), errors.New("missing field 'queryId'")
	}
	if val, ok := dat["queryType"]; ok {
		qt, ok := val.(string)
		if ok {
			row.QueryType = qt
		} else {
			return *new(QueriesRow), errors.New("incorrect type for 'queryType'")
		}
	} else {
		simplelog.Warningf("queries.json is missing field 'queryType'")
	}
	if val, ok := dat["queryCost"]; ok {
		qc, ok := val.(float64)
		if ok {
			row.QueryCost = qc
		} else {
			return *new(QueriesRow), errors.New("incorrect type for 'queryCost'")
		}
	} else {
		simplelog.Warningf("queries.json is missing field 'queryCost'")
	}
	if val, ok := dat["planningTime"]; ok {
		pt, ok := val.(float64)
		if ok {
			row.PlanningTime = pt
		} else {
			return *new(QueriesRow), errors.New("incorrect type for 'planningTime'")
		}
	} else {
		simplelog.Warningf("queries.json is missing field 'planningTime'")
	}
	if val, ok := dat["runningTime"]; ok {
		rt, ok := val.(float64)
		if ok {
			row.RunningTime = rt
		} else {
			return *new(QueriesRow), errors.New("incorrect type for 'runningTime'")
		}
	} else {
		simplelog.Warningf("queries.json is missing field 'runningTime'")
	}
	if val, ok := dat["start"]; ok {
		s, ok := val.(float64)
		if ok {
			row.Start = s
		} else {
			return *new(QueriesRow), errors.New("incorrect type for 'start'")
		}
	} else {
		return *new(QueriesRow), errors.New("missing field 'start'")
	}
	if val, ok := dat["outcome"]; ok {
		o, ok := val.(string)
		if ok {
			row.Outcome = o
		} else {
			return *new(QueriesRow), errors.New("incorrect type for 'outcome'")
		}
	} else {
		return *new(QueriesRow), errors.New("missing field 'outcome'")
	}
	queriesrow := *row
	return queriesrow, err
}

func parseLineJobsJSON(line Row) (QueriesRow, error) {
	row := new(QueriesRow)
	if line.JobID == "" {
		return *new(QueriesRow), errors.New("no job ID found")
	}
	row.QueryID = line.JobID
	row.QueryType = line.QueryType
	row.QueryCost = line.PlannerEstimatedCost
	// Column names used for Dremio Cloud sys.project.history.jobs
	if line.SubmittedEpoch != 0 {
		row.Start = float64(line.SubmittedEpoch)
		row.PlanningTime = float64(line.ExecutionStartEpoch) - max(float64(line.PlanningStartEpoch), float64(line.SubmittedEpoch))
		row.RunningTime = float64(line.FinalStateEpoch) - float64(line.SubmittedEpoch)
	} else {
		// Column names used for Dremio Software sys.jobs_recent
		row.Start = float64(line.SubmittedEpochMillis)
		row.PlanningTime = float64(line.ExecutionStartEpochMillis) - max(float64(line.PlanningStartEpochMillis), float64(line.SubmittedEpochMillis))
		row.RunningTime = float64(line.FinalStateEpochMillis) - float64(line.SubmittedEpochMillis)
	}
	row.Outcome = line.Status
	queriesrow := *row
	return queriesrow, nil
}

func GetRecentErrorJobs(queriesrows []QueriesRow, limit int) []QueriesRow {
	errorqueriesrows := []QueriesRow{}

	for i := range queriesrows {
		if queriesrows[i].Outcome == "FAILED" {
			errorqueriesrows = append(errorqueriesrows, queriesrows[i])
		}
	}

	totalrows := len(errorqueriesrows)
	sort.Slice(errorqueriesrows, func(i, j int) bool {
		return errorqueriesrows[i].Start > errorqueriesrows[j].Start
	})
	return errorqueriesrows[:min(totalrows, limit)]
}

func GetSlowExecJobs(queriesrows []QueriesRow, limit int) []QueriesRow {
	totalrows := len(queriesrows)
	sort.Slice(queriesrows, func(i, j int) bool {
		return queriesrows[i].RunningTime > queriesrows[j].RunningTime
	})
	return queriesrows[:min(totalrows, limit)]
}

func GetSlowPlanningJobs(queriesrows []QueriesRow, limit int) []QueriesRow {
	totalrows := len(queriesrows)
	sort.Slice(queriesrows, func(i, j int) bool {
		return queriesrows[i].PlanningTime > queriesrows[j].PlanningTime
	})
	return queriesrows[:min(totalrows, limit)]
}

func GetHighCostJobs(queriesrows []QueriesRow, limit int) []QueriesRow {
	totalrows := len(queriesrows)
	sort.Slice(queriesrows, func(i, j int) bool {
		return queriesrows[i].QueryCost > queriesrows[j].QueryCost
	})

	return queriesrows[:min(totalrows, limit)]
}

func AddRowsToSet(queriesrows []QueriesRow, profilesToCollect map[string]string) {
	for _, row := range queriesrows {
		jobid := row.QueryID
		profilesToCollect[jobid] = ""
	}
}

func CollectQueriesJSON(queriesjsons []string) []QueriesRow {
	queriesrows := []QueriesRow{}
	for _, queriesjson := range queriesjsons {
		simplelog.Debugf("Attempting to open queries.json file %v", queriesjson)
		rows := []QueriesRow{}
		var err error

		if strings.HasSuffix(queriesjson, ".gz") {
			rows, err = ReadGzFile(queriesjson)
			if err != nil {
				simplelog.Errorf("failed to read gunzip %v: %v", queriesjson, err)
				continue
			}
			queriesrows = append(queriesrows, rows...)
		} else if strings.HasSuffix(queriesjson, ".json") {
			rows, err = ReadJSONFile(queriesjson)
			if err != nil {
				simplelog.Errorf("failed to parse json file %v: %v", queriesjson, err)
				continue
			}
			queriesrows = append(queriesrows, rows...)
		} else {
			simplelog.Error("File is neither JSON or GZIP format.")
		}
		simplelog.Infof("Found %v new rows in %v", strconv.Itoa(len(rows)), queriesjson)
	}
	simplelog.Debugf("Collected a total of %v rows of queries.json", len(queriesrows))
	return queriesrows
}

func CollectJobHistoryJSON(jobhistoryjsons []string) []QueriesRow {
	queriesrows := []QueriesRow{}
	for _, jobhistoryjson := range jobhistoryjsons {
		simplelog.Infof("Attempting to open json file %v", jobhistoryjson)
		rows, err := ReadHistoryJobsJSONFile(jobhistoryjson)
		if err != nil {
			simplelog.Errorf("failed to parse json file %v: %v", jobhistoryjson, err)
			continue
		}
		queriesrows = append(queriesrows, rows...)
		simplelog.Infof("Found %v new rows in %v", strconv.Itoa(len(rows)), jobhistoryjson)
	}
	simplelog.Infof("Collected a total of %v rows of jobs history", len(queriesrows))
	return queriesrows
}

func errCheck(f func() error) {
	err := f()
	if err != nil {
		simplelog.Errorf("received error: %v", err)
	}
}
