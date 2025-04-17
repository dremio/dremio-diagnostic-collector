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

// apicollect provides all the methods that collect via the API, this is a substantial part of the activities of DDC so it gets it's own package
package apicollect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/ddcio"
	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/restclient"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/shutdown"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/simplelog"
)

type Stat struct {
	Date             string `json:"date"`
	JobCount         int    `json:"jobCount"`
	UniqueUsersCount int    `json:"uniqueUsersCount"`
}

type ClusterUsage struct {
	Edition string `json:"edition"`
	Stats   []Stat `json:"stats"`
}

func RunCollectDremioSystemTables(c *conf.CollectConf, hook shutdown.CancelHook) error {
	simplelog.Debugf("Collecting results from Export System Tables...")
	var systables []string
	if !c.IsDremioCloud() {
		systables = c.Systemtables()
		// sys.jobs_recent only to be collected in REST-only mode
		if c.IsRESTCollect() {
			// sys.jobs_recent only to be collected for small to medium sized clusters
			err := validateClusterUsage(c)
			if err != nil {
				simplelog.Warningf("Skipping sys.jobs_recent: %v", err)
			} else {
				simplelog.Debugf("Adding sys.jobs_recent to collection")
				systables = append(systables, "jobs_recent")
			}
		}
	} else {
		systables = c.SystemtablesDremioCloud()
	}
	timeoutDuration := time.Duration(c.CollectSystemTablesTimeoutSeconds()) * time.Second
	// wrap context with a timeout
	ctx, cancel := context.WithTimeoutCause(context.Background(), timeoutDuration, fmt.Errorf("unable to complete collection of sys tables in %v, raise %v in the ddc.yaml", timeoutDuration, conf.KeyCollectSystemTablesTimeoutSeconds))
	defer cancel() // avoid leaks
	for _, systable := range systables {
		// check to see if context is cancelled
		if err := ctx.Err(); err != nil {
			return err
		}
		err := downloadSysTable(ctx, c, hook, systable)
		if err != nil {
			simplelog.Errorf("%v", err) // Print instead of Error
		}
	}
	return nil
}

func validateClusterUsage(c *conf.CollectConf) (err error) {
	clusterusagefile := "cluster_usage.json"
	// Check if cluster usage is within limits, as sys.jobs_recent table performance can suffer on high volume clusters
	var files []fs.DirEntry
	files, err = os.ReadDir(c.WLMOutDir())
	if err != nil {
		return err
	}
	for _, file := range files {
		if strings.Contains(file.Name(), clusterusagefile) {
			filename := path.Join(c.WLMOutDir(), file.Name())
			data, err := openJSON(filename)
			if err != nil {
				return err
			}
			averageDailyJobCount, err := calculateJobCount(data)
			if err != nil {
				return err
			}
			if averageDailyJobCount > c.RestCollectDailyJobsLimit() {
				return fmt.Errorf("average daily job count %v above allowed limit of %v", averageDailyJobCount, c.RestCollectDailyJobsLimit())
			}
			return nil
		}
	}
	return fmt.Errorf("unable to find %v in wlm dir", clusterusagefile)
}

func openJSON(filename string) ([]byte, error) {
	var bytedata []byte
	data, err := os.Open(path.Clean(filename))
	if err != nil {
		simplelog.Errorf("can't open %v: %v", filename, err)
		return bytedata, err
	}
	defer errCheck(data.Close)

	bytedata, err = io.ReadAll(data)
	if err != nil {
		simplelog.Errorf("can't read data of %v: %v", filename, err)
		return bytedata, err
	}
	return bytedata, nil
}

func calculateJobCount(data []byte) (int, error) {
	var stats ClusterUsage
	err := json.Unmarshal(data, &stats)
	if err != nil {
		return -1, err
	}

	sum := 0
	daycount := 1 // Avoid divide by zero
	for _, stat := range stats.Stats {
		sum += stat.JobCount
		daycount++
	}
	averageDailyJobCount := sum / daycount

	return averageDailyJobCount, nil
}

func downloadSysTable(ctx context.Context, c *conf.CollectConf, hook shutdown.CancelHook, systable string) error {
	tablerowlimit := strconv.Itoa(c.SystemTablesRowLimit())

	headers := map[string]string{"Content-Type": "application/json"}
	var joburl, sqlurl, jobresultsurl string
	if !c.IsDremioCloud() {
		sqlurl = c.DremioEndpoint() + "/api/v3/sql"
		joburl = c.DremioEndpoint() + "/api/v3/job/"
	} else {
		sqlurl = c.DremioEndpoint() + "/v0/projects/" + c.DremioCloudProjectID() + "/sql"
		joburl = c.DremioEndpoint() + "/v0/projects/" + c.DremioCloudProjectID() + "/job/"
	}

	sql := "SELECT * FROM sys." + systable
	// job history is limited by the number of days, all other sys tables are limited by the number of rows
	if strings.Contains(systable, "project.history.jobs") || strings.Contains(systable, "jobs_recent") {
		maxdaylimit := 7
		if c.DremioQueriesJSONNumDays() < maxdaylimit {
			maxdaylimit = c.DremioQueriesJSONNumDays()
		}
		daylimit := strconv.Itoa(maxdaylimit)

		sql += " WHERE submitted_ts > DATE_SUB(CAST(NOW() AS DATE), CAST(" + daylimit + " AS INTERVAL DAY))"
		sql += " ORDER BY submitted_ts DESC"
		simplelog.Debugf("Collecting sys.%v (Limit: Last %v days)", systable, daylimit)
	} else {
		sql += " LIMIT " + tablerowlimit
		simplelog.Debugf("Collecting sys.%v (Limit: %v rows)", systable, tablerowlimit)
	}
	simplelog.Debug(sql)
	sqlbody := "{\"sql\": \"" + sql + "\"}"

	jobid, err := restclient.PostQuery(hook, sqlurl, c.DremioPATToken(), headers, sqlbody)
	if err != nil {
		return err
	}
	jobstateurl := joburl + jobid
	err = checkJobState(ctx, c, hook, jobstateurl, headers)
	if err != nil {
		return fmt.Errorf("unable to retrieve sys.%v: %w", systable, err)
	}
	jobresultsurl = joburl + jobid + "/results"
	simplelog.Debugf("Retrieving job results ...")
	err = retrieveJobResults(c, hook, jobresultsurl, headers, systable)
	if err != nil {
		return fmt.Errorf("unable to retrieve job results: %w", err)
	}
	return nil
}

func checkJobState(ctx context.Context, c *conf.CollectConf, hook shutdown.CancelHook, jobstateurl string, headers map[string]string) error {
	sleepms := 200 // Consider moving to config
	jobstate := "RUNNING"
	for jobstate != "COMPLETED" {
		// check for timeout before
		if err := ctx.Err(); err != nil {
			return err
		}
		time.Sleep(time.Duration(sleepms) * time.Millisecond)
		// check for timeout after
		if err := ctx.Err(); err != nil {
			return err
		}
		body, err := restclient.APIRequest(hook, jobstateurl, c.DremioPATToken(), "GET", headers)
		if err != nil {
			return fmt.Errorf("unable to retrieve job state from %s: %w", jobstateurl, err)
		}
		dat := make(map[string]interface{})
		err = json.Unmarshal(body, &dat)
		if err != nil {
			return fmt.Errorf("unable to unmarshall JSON response: %w", err)
		}
		if val, ok := dat["jobState"]; ok {
			jobstate, ok = val.(string)
			if !ok {
				return errors.New("returned field 'jobState' does not have expected type string")
			}
		} else {
			return errors.New("returned json does not contain required field 'jobState'")
		}
		simplelog.Debugf("job state: %s", jobstate)
		if jobstate == "FAILED" || jobstate == "CANCELLED" || jobstate == "CANCELLATION_REQUESTED" || jobstate == "INVALID_STATE" {
			return fmt.Errorf("unable to retrieve job results - job state: %v", jobstate)
		}
	}
	return nil
}

func retrieveJobResults(c *conf.CollectConf, hook shutdown.CancelHook, jobresultsurl string, headers map[string]string, systable string) error {
	apilimit := 500 // Consider moving to config
	tablerowlimit := c.SystemTablesRowLimit()

	offset := 0

	for {
		urlsuffix := "?offset=" + strconv.Itoa(offset) + "&limit=" + strconv.Itoa(apilimit)
		resultsurl := jobresultsurl + urlsuffix
		body, err := restclient.APIRequest(hook, resultsurl, c.DremioPATToken(), "GET", headers)
		if err != nil {
			return fmt.Errorf("unable to retrieve job results from %s: %w", resultsurl, err)
		}

		dat := make(map[string]interface{})
		err = json.Unmarshal(body, &dat)
		var rowcount float64
		if err != nil {
			return fmt.Errorf("unable to unmarshall JSON response: %w", err)
		}
		if val, ok := dat["rowCount"]; ok {
			rowcount, ok = val.(float64)
			if !ok {
				rowcount = 0
				simplelog.Warning("returned field 'rowCount' does not have expected type float64")
			}
		} else {
			rowcount = 0
			simplelog.Warning("returned json does not contain expected field 'rowCount'")
		}
		sb := string(body)
		filename := getSystemTableName(systable, urlsuffix)
		systemTableFile := path.Join(c.SystemTablesOutDir(), filename)
		simplelog.Debugf("Creating %v ...", filename)
		file, err := os.Create(path.Clean(systemTableFile))
		if err != nil {
			return fmt.Errorf("unable to create file %v: %w", filename, err)
		}
		defer ddcio.EnsureClose(filepath.Clean(systemTableFile), file.Close)
		_, err = fmt.Fprint(file, sb)
		if err != nil {
			return fmt.Errorf("unable to create file %s: %w", filename, err)
		}
		simplelog.Debugf("SUCCESS - Created %v", filename)

		offset = offset + apilimit
		if offset > int(rowcount) || offset >= tablerowlimit {
			if offset >= tablerowlimit {
				simplelog.Warningf("table results have been limited to %v records", tablerowlimit)
			}
			break
		}
	}

	return nil
}

func getSystemTableName(systable, urlsuffix string) string {
	filename := strings.Join([]string{"sys.", systable, urlsuffix, ".json"}, "")
	// the ? will not work on windows
	filename = strings.ReplaceAll(filename, "?", "_")
	// the = will not work on windows
	filename = strings.ReplaceAll(filename, "=", "_")
	// go ahead and remove & because it will look weird by itself in the file name
	filename = strings.ReplaceAll(filename, "&", "_")
	return strings.ReplaceAll(filename, "\\\"", "")
}

func errCheck(f func() error) {
	err := f()
	if err != nil {
		simplelog.Errorf("received error: %v", err)
	}
}
