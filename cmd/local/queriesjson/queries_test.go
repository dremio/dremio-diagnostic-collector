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
	"os"
	"path"
	"testing"
)

func TestGetSlowExecJobs_empty(t *testing.T) {
	queriesRowsEmpty := []QueriesRow{}
	numSlowExecJobsEmpty := 10
	slowExecQueriesRowsEmpty := GetSlowExecJobs(queriesRowsEmpty, numSlowExecJobsEmpty)
	if len(slowExecQueriesRowsEmpty) != 0 {
		t.Errorf("Error")
	}
}

func TestGetSlowExecJobs_small(t *testing.T) {
	row1 := new(QueriesRow)
	row1.QueryID = "Row1"
	row1.QueryType = "REST"
	row1.QueryCost = 500
	row1.PlanningTime = 5
	row1.RunningTime = 100
	row1.Start = 11111
	row1.Outcome = "FAILED"

	row2 := new(QueriesRow)
	row2.QueryID = "Row2"
	row2.QueryType = "ODBC"
	row2.QueryCost = 10
	row2.PlanningTime = 500
	row2.RunningTime = 1
	row2.Start = 22222
	row2.Outcome = "FAILED"

	row3 := new(QueriesRow)
	row3.QueryID = "Row3"
	row3.QueryType = "META"
	row3.QueryCost = 1000
	row3.PlanningTime = 1
	row3.RunningTime = 50
	row3.Start = 33333
	row3.Outcome = "CANCELLED"

	row4 := new(QueriesRow)
	row4.QueryID = "Row4"
	row4.QueryType = "REFLECTION"
	row4.QueryCost = 10
	row4.PlanningTime = 100
	row4.RunningTime = 10
	row4.Start = 44444
	row4.Outcome = "FINISHED"

	row5 := new(QueriesRow)
	row5.QueryID = "Row5"
	row5.QueryType = "UI"
	row5.QueryCost = 99
	row5.PlanningTime = 1000
	row5.RunningTime = 25
	row5.Start = 55555
	row5.Outcome = "FAILED"
	queriesrows := []QueriesRow{*row1, *row2, *row3, *row4, *row5}

	// Slow Planning
	numSlowPlanningJobs := 10
	slowplanqueriesrows := GetSlowPlanningJobs(queriesrows, numSlowPlanningJobs)
	if len(slowplanqueriesrows) != 5 {
		t.Errorf("Error")
	}

	numSlowPlanningJobs = 3
	slowplanqueriesrows = GetSlowPlanningJobs(queriesrows, numSlowPlanningJobs)
	if len(slowplanqueriesrows) != 3 {
		t.Errorf("Error")
	}
	if slowplanqueriesrows[0] != *row5 {
		t.Errorf("Error")
	}
	if slowplanqueriesrows[1] != *row2 {
		t.Errorf("Error")
	}
	if slowplanqueriesrows[2] != *row4 {
		t.Errorf("Error")
	}

	// Slow Execution
	numSlowExecJobs := 10
	slowexecqueriesrows := GetSlowExecJobs(queriesrows, numSlowExecJobs)
	if len(slowexecqueriesrows) != 5 {
		t.Errorf("Error")
	}

	numSlowExecJobs = 3
	slowexecqueriesrows = GetSlowExecJobs(queriesrows, numSlowExecJobs)
	if len(slowexecqueriesrows) != 3 {
		t.Errorf("Error")
	}
	if slowexecqueriesrows[0] != *row1 {
		t.Errorf("Error")
	}
	if slowexecqueriesrows[1] != *row3 {
		t.Errorf("Error")
	}
	if slowexecqueriesrows[2] != *row5 {
		t.Errorf("Error")
	}

	// High Cost
	numHighQueryCostJobs := 10
	highcostqueriesrows := GetHighCostJobs(queriesrows, numHighQueryCostJobs)
	if len(highcostqueriesrows) != 5 {
		t.Errorf("Error")
	}

	numHighQueryCostJobs = 3
	highcostqueriesrows = GetHighCostJobs(queriesrows, numHighQueryCostJobs)
	if len(highcostqueriesrows) != 3 {
		t.Errorf("Error")
	}
	if highcostqueriesrows[0] != *row3 {
		t.Errorf("Error")
	}
	if highcostqueriesrows[1] != *row1 {
		t.Errorf("Error")
	}
	if highcostqueriesrows[2] != *row5 {
		t.Errorf("Error")
	}

	// Recent Errors
	numRecentErrorJobs := 10
	errorqueriesrows := GetRecentErrorJobs(queriesrows, numRecentErrorJobs)
	if len(errorqueriesrows) != 3 {
		t.Errorf("Error")
	}

	numRecentErrorJobs = 2
	errorqueriesrows = GetRecentErrorJobs(queriesrows, numRecentErrorJobs)
	if len(errorqueriesrows) != 2 {
		t.Errorf("Error")
	}
	if errorqueriesrows[0] != *row5 {
		t.Errorf("Error")
	}
	if errorqueriesrows[1] != *row2 {
		t.Errorf("Error")
	}
}

func TestParseLine(t *testing.T) {
	s := "123"
	actual, err := parseLine(s, 1)
	if err == nil {
		t.Errorf("There should be an error here")
	}
	expected := *new(QueriesRow)
	if expected != actual {
		t.Errorf("ERROR")
	}
}

func TestParseLine_Empty(t *testing.T) {
	s := ""
	actual, err := parseLine(s, 1)
	if err == nil {
		t.Errorf("There should be an error here")
	}
	expected := *new(QueriesRow)
	if expected != actual {
		t.Errorf("ERROR")
	}
}

func TestParseLine_ValidJson(t *testing.T) {
	s := `{
		"queryId":"1b9b9629-8289-b46c-c765-455d24da7800",
		"start":100,
		"outcome":"COMPLETED",
		"queryType":"METADATA_REFRESH",
		"queryCost":5.1003501E7,
		"planningTime":340,
		"runningTime":4785
	}`
	actual, err := parseLine(s, 1)
	if err != nil {
		t.Errorf("There should not be an error here")
	}
	expected := new(QueriesRow)
	expected.QueryID = "1b9b9629-8289-b46c-c765-455d24da7800"
	expected.QueryType = "METADATA_REFRESH"
	expected.QueryCost = 5.1003501e7
	expected.PlanningTime = 340
	expected.RunningTime = 4785
	expected.Start = 100
	expected.Outcome = "COMPLETED"
	if *expected != actual {
		t.Errorf("ERROR")
	}
}

func TestParseLine_EmptyJson(t *testing.T) {
	s := "{}"
	actual, err := parseLine(s, 1)
	if err == nil {
		t.Errorf("There should be an error here")
	}
	expected := *new(QueriesRow)
	if expected != actual {
		t.Errorf("ERROR")
	}
}

func TestParseLine_ValidJsonWithMissingFields(t *testing.T) {
	s := `{
		"queryId":"1b9b9629-8289-b46c-c765-455d24da7800"
	}`
	actual, err := parseLine(s, 1)
	if err == nil {
		t.Errorf("There should be an error here")
	}
	expected := *new(QueriesRow)
	if expected != actual {
		t.Errorf("ERROR")
	}
}

func isField(i int, field int) int {
	if i == field {
		return 1
	}
	return 0
}

func TestParseLine_IncorrectTypes(t *testing.T) {
	fields := [][]string{
		{`"1b9b9629-8289-b46c-c765-455d24da7800"`, `123`},
		{`100`, `"now"`},
		{`"COMPLETED"`, `null`},
		{`"METADATA_REFRESH"`, `[1,2,3]`},
		{`5.1003501E7`, `"5.1003501E7"`},
		{`340`, `null`},
		{`4785`, `{"value":"4785"}`},
	}
	numfields := len(fields)
	for i := 0; i < numfields; i++ {
		s := `{
		"queryId":` + fields[0][isField(i, 0)] + `,
		"start":` + fields[1][isField(i, 1)] + `,
		"outcome":` + fields[2][isField(i, 2)] + `,
		"queryType":` + fields[3][isField(i, 3)] + `,
		"queryCost":` + fields[4][isField(i, 4)] + `,
		"planningTime":` + fields[5][isField(i, 5)] + `,
		"runningTime":` + fields[6][isField(i, 6)] + `
		}`
		_, err := parseLine(s, 1)
		if err == nil {
			t.Errorf("There should be an error here")
		}
	}
}

func TestMin(t *testing.T) {
	actual := min(1, 2)
	expected := 1
	if expected != actual {
		t.Errorf("ERROR")
	}
	actual = min(2, 1)
	if expected != actual {
		t.Errorf("ERROR")
	}
	actual = min(1, 1)
	if expected != actual {
		t.Errorf("ERROR")
	}
}

func TestReadJSONFile(t *testing.T) {
	filename := "../../testdata/queries/bad_queries.json"
	actual, err := ReadJSONFile(filename)
	if err != nil {
		t.Errorf("There should not be an error here")
	}
	if len(actual) != 0 {
		t.Errorf("The bad_queries.json should produce 0 valid entries")
	}
}

func TestReadGzippedJSONFile(t *testing.T) {
	filename := "../../testdata/queries/queries.json.gz"
	actual, err := ReadGzFile(filename)
	if err != nil {
		t.Errorf("There should not be an error here")
	}
	if len(actual) != 3 {
		t.Errorf("The zipped queries.json should produce 3 entries")
	}
	expectedStartOfIndex0 := 100.0
	if actual[0].Start != expectedStartOfIndex0 {
		t.Errorf("ERROR")
	}
}

func TestReadHistoryJobsJSONFile(t *testing.T) {
	filename := "../../testdata/queries/sys.project.history.jobs.json"
	actual, err := ReadHistoryJobsJSONFile(filename)
	if err != nil {
		t.Errorf("%v", err)
		t.Errorf("There should not be an error here")
	}
	if len(actual) != 2 {
		t.Errorf("%v", len(actual))
		t.Errorf("The sys.project.history.jobs.json should produce 2 valid entries")
	}
}

func TestReadJobsRecentJSONFile(t *testing.T) {
	filename := "../../testdata/queries/sys.jobs_recent.json"
	actual, err := ReadHistoryJobsJSONFile(filename)
	if err != nil {
		t.Errorf("%v", err)
		t.Errorf("There should not be an error here")
	}
	if len(actual) != 2 {
		t.Errorf("%v", len(actual))
		t.Errorf("The sys.jobs_recent.json should produce 2 valid entries")
	} else if actual[1].QueryID != "Query2" ||
		actual[1].QueryType != "REST" ||
		actual[1].QueryCost != 3.8154000035e9 ||
		actual[1].Start != 1714033458006 ||
		actual[1].PlanningTime != 34 ||
		actual[1].RunningTime != 55 ||
		actual[1].Outcome != "COMPLETED" {
		t.Errorf("The second sys.jobs_recent.json entry was not parsed correctly")
	}
}

func TestReadBadJobsRecentJSONFile(t *testing.T) {
	filename := "../../testdata/queries/bad_sys.jobs_recent.json"
	actual, err := ReadHistoryJobsJSONFile(filename)
	if err != nil {
		t.Errorf("%v", err)
		t.Errorf("There should not be an error here")
	}
	if len(actual) != 0 {
		t.Errorf("%v", len(actual))
		t.Errorf("The bad_sys.jobs_recent.json should produce 0 valid entries")
	}
}

func TestCollectQueriesJSON(t *testing.T) {
	queriesDir := "../../testdata/queries/"
	files, err := os.ReadDir(queriesDir)
	if err != nil {
		t.Errorf("There should not be an error here")
	}
	queriesjsons := []string{}
	for _, file := range files {
		queriesjsons = append(queriesjsons, path.Join(queriesDir, file.Name()))
	}
	numValidEntries := 6
	queriesrows := CollectQueriesJSON(queriesjsons)
	if len(queriesrows) != numValidEntries {
		t.Errorf("The queries files in testdata should produce %v entries", numValidEntries)
	}

	// Testing AddRowsToSet with the data
	profilesToCollect := map[string]string{}
	AddRowsToSet(queriesrows, profilesToCollect)
	if len(profilesToCollect) != numValidEntries {
		t.Errorf("The profiles to collect should be %v entries", numValidEntries)
	}
	if _, ok := profilesToCollect["1b9b9629-8289-b46c-c765-455d24da7800"]; !ok {
		t.Errorf("The profile ID is missing")
	}
	if _, ok := profilesToCollect["123456"]; !ok {
		t.Errorf("The profile ID is missing")
	}
}

func TestCollectJobHistoryJSON(t *testing.T) {
	queriesDir := "../../testdata/queries/"
	files, err := os.ReadDir(queriesDir)
	if err != nil {
		t.Errorf("There should not be an error here")
	}
	queriesjsons := []string{}
	for _, file := range files {
		queriesjsons = append(queriesjsons, path.Join(queriesDir, file.Name()))
	}
	numValidEntries := 4
	queriesrows := CollectJobHistoryJSON(queriesjsons)
	if len(queriesrows) != numValidEntries {
		t.Errorf("The queries files in testdata should produce %v entries", numValidEntries)
	}

	// Testing AddRowsToSet with the data
	profilesToCollect := map[string]string{}
	AddRowsToSet(queriesrows, profilesToCollect)
	if len(profilesToCollect) != numValidEntries {
		t.Errorf("The profiles to collect should be %v entries", numValidEntries)
	}
	if _, ok := profilesToCollect["1c38de7b-1028-54d9-e238-87eae52bc200"]; !ok {
		t.Errorf("The profile ID is missing")
	}
	if _, ok := profilesToCollect["1d956036-df02-a335-a573-c4b281926000"]; !ok {
		t.Errorf("The profile ID is missing")
	}
}
