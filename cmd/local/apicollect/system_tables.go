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
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/ddcio"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/restclient"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
)

func RunCollectDremioSystemTables(c *conf.CollectConf) error {
	simplelog.Debugf("Collecting results from Export System Tables...")
	var systables []string
	if !c.IsDremioCloud() {
		systables = c.Systemtables()
	} else {
		systables = c.SystemtablesDremioCloud()
	}

	for _, systable := range systables {
		err := downloadSysTable(c, systable)
		if err != nil {
			simplelog.Errorf("%v", err) // Print instead of Error
		}

	}

	return nil
}

func downloadSysTable(c *conf.CollectConf, systable string) error {
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
		daylimit := strconv.Itoa(c.DremioQueriesJSONNumDays())
		sql += " WHERE submitted_ts > DATE_SUB(CAST(NOW() AS DATE), CAST(" + daylimit + " AS INTERVAL DAY))"
		sql += " ORDER BY submitted_ts DESC"
		simplelog.Debugf("Collecting sys." + systable + " (Limit: Last " + daylimit + " days)")
	} else {
		sql += " LIMIT " + tablerowlimit
		simplelog.Debugf("Collecting sys." + systable + " (Limit: " + tablerowlimit + " rows)")
	}
	simplelog.Debugf(sql)
	sqlbody := "{\"sql\": \"" + sql + "\"}"

	jobid, err := restclient.PostQuery(sqlurl, c.DremioPATToken(), headers, sqlbody)
	if err != nil {
		return err
	}
	jobstateurl := joburl + jobid
	err = checkJobState(c, jobstateurl, headers)
	if err != nil {
		return fmt.Errorf("unable to retrieve sys.%v due to error %v", systable, err)
	}
	jobresultsurl = joburl + jobid + "/results"
	simplelog.Debugf("Retrieving job results ...")
	err = retrieveJobResults(c, jobresultsurl, headers, systable)
	if err != nil {
		return fmt.Errorf("unable to retrieve job results due to error %v", err)
	}
	return nil
}

func checkJobState(c *conf.CollectConf, jobstateurl string, headers map[string]string) error {
	sleepms := 200 // Consider moving to config
	jobstate := "RUNNING"
	for jobstate != "COMPLETED" {
		time.Sleep(time.Duration(sleepms) * time.Millisecond)
		body, err := restclient.APIRequest(jobstateurl, c.DremioPATToken(), "GET", headers)
		if err != nil {
			return fmt.Errorf("unable to retrieve job state from %s due to error %v", jobstateurl, err)
		}
		dat := make(map[string]interface{})
		err = json.Unmarshal(body, &dat)
		if err != nil {
			return fmt.Errorf("unable to unmarshall JSON response - %w", err)
		}
		if val, ok := dat["jobState"]; ok {
			jobstate, ok = val.(string)
			if !ok {
				return fmt.Errorf("returned field 'jobState' does not have expected type string")
			}
		} else {
			return fmt.Errorf("returned json does not contain required field 'jobState'")
		}
		simplelog.Debugf("job state: %s", jobstate)
		if jobstate == "FAILED" || jobstate == "CANCELED" || jobstate == "CANCELLATION_REQUESTED" || jobstate == "INVALID_STATE" {
			return fmt.Errorf("unable to retrieve job results - job state: " + jobstate)
		}
	}
	return nil
}

func retrieveJobResults(c *conf.CollectConf, jobresultsurl string, headers map[string]string, systable string) error {
	apilimit := 500 // Consider moving to config
	tablerowlimit := c.SystemTablesRowLimit()

	offset := 0

	for {
		urlsuffix := "?offset=" + strconv.Itoa(offset) + "&limit=" + strconv.Itoa(apilimit)
		resultsurl := jobresultsurl + urlsuffix
		body, err := restclient.APIRequest(resultsurl, c.DremioPATToken(), "GET", headers)
		if err != nil {
			return fmt.Errorf("unable to retrieve job results from %s due to error %v", resultsurl, err)
		}

		dat := make(map[string]interface{})
		err = json.Unmarshal(body, &dat)
		var rowcount float64
		if err != nil {
			return fmt.Errorf("unable to unmarshall JSON response - %w", err)
		}
		if val, ok := dat["rowCount"]; ok {
			rowcount, ok = val.(float64)
			if !ok {
				rowcount = 0
				simplelog.Warningf("returned field 'rowCount' does not have expected type float64")
			}
		} else {
			rowcount = 0
			simplelog.Warningf("returned json does not contain expected field 'rowCount'")
		}
		sb := string(body)
		filename := getSystemTableName(systable, urlsuffix)
		systemTableFile := path.Join(c.SystemTablesOutDir(), filename)
		simplelog.Debugf("Creating " + filename + " ...")
		file, err := os.Create(path.Clean(systemTableFile))
		if err != nil {
			return fmt.Errorf("unable to create file %v due to error %v", filename, err)
		}
		defer ddcio.EnsureClose(filepath.Clean(systemTableFile), file.Close)
		_, err = fmt.Fprint(file, sb)
		if err != nil {
			return fmt.Errorf("unable to create file %s due to error %v", filename, err)
		}
		simplelog.Debugf("SUCCESS - Created " + filename)

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
	filename = strings.Replace(filename, "?", "_", -1)
	// the = will not work on windows
	filename = strings.Replace(filename, "=", "_", -1)
	// go ahead and remove & because it will look weird by itself in the file name
	filename = strings.Replace(filename, "&", "_", -1)
	return strings.Replace(filename, "\\\"", "", -1)
}
