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
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/ddcio"
	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/queriesjson"
	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/restclient"
	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/threading"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/shutdown"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/simplelog"
)

func GetNumberOfJobProfilesCollected(c *conf.CollectConf, hook shutdown.Hook) (tried, collected int, err error) {
	var files []fs.DirEntry
	var queriesrows []queriesjson.QueriesRow

	files, err = os.ReadDir(c.SystemTablesOutDir())
	if err != nil {
		return 0, 0, err
	}
	jobhistoryjsons := []string{}
	for _, file := range files {
		if strings.Contains(file.Name(), "project.history.jobs") || strings.Contains(file.Name(), "jobs_recent") {
			jobhistoryjsons = append(jobhistoryjsons, path.Join(c.SystemTablesOutDir(), file.Name()))
		}
	}

	if len(jobhistoryjsons) == 0 {
		// Attempt to read job history from queries.json, if not Dremio Cloud or REST collect
		if !c.IsRESTCollect() {
			files, err = os.ReadDir(c.QueriesOutDir())
			if err != nil {
				return 0, 0, err
			}
			queriesjsons := []string{}
			for _, file := range files {
				queriesjsons = append(queriesjsons, path.Join(c.QueriesOutDir(), file.Name()))
			}

			if len(queriesjsons) == 0 {
				simplelog.Warning("no queries.json or jobs.json files found. This is probably an executor, so we are skipping collection of Job Profiles")
				return
			}

			queriesrows = queriesjson.CollectQueriesJSON(queriesjsons)
		} else {
			simplelog.Warning("no valid records or jobs.json files found. Therefore, we are skipping collection of Job Profiles")
			return
		}
	} else {
		queriesrows = queriesjson.CollectJobHistoryJSON(jobhistoryjsons)
	}

	profilesToCollect := map[string]string{}

	simplelog.Debugf("searching job history for %v of jobProfilesNumSlowPlanning", c.JobProfilesNumSlowPlanning())
	slowplanqueriesrows := queriesjson.GetSlowPlanningJobs(queriesrows, c.JobProfilesNumSlowPlanning())
	queriesjson.AddRowsToSet(slowplanqueriesrows, profilesToCollect)

	simplelog.Debugf("searching job history for %v of jobProfilesNumSlowExec", c.JobProfilesNumSlowExec())
	slowexecqueriesrows := queriesjson.GetSlowExecJobs(queriesrows, c.JobProfilesNumSlowExec())
	queriesjson.AddRowsToSet(slowexecqueriesrows, profilesToCollect)

	simplelog.Debugf("searching job history for profiles %v of jobProfilesNumHighQueryCost", c.JobProfilesNumHighQueryCost())
	highcostqueriesrows := queriesjson.GetHighCostJobs(queriesrows, c.JobProfilesNumHighQueryCost())
	queriesjson.AddRowsToSet(highcostqueriesrows, profilesToCollect)

	simplelog.Debugf("searching job history for %v of jobProfilesNumRecentErrors", c.JobProfilesNumRecentErrors())
	errorqueriesrows := queriesjson.GetRecentErrorJobs(queriesrows, c.JobProfilesNumRecentErrors())
	queriesjson.AddRowsToSet(errorqueriesrows, profilesToCollect)

	tried = len(profilesToCollect)
	var m sync.Mutex
	if len(profilesToCollect) > 0 {
		fmt.Println("JOB START - JOB PROFILES COLLECTION")
		simplelog.Debugf("Downloading %v job profiles...", len(profilesToCollect))
		downloadThreadPool, err := threading.NewThreadPoolWithJobQueue(c.NumberThreads(), len(profilesToCollect), 10, false, true)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid thread pool: %w", err)
		}
		for key := range profilesToCollect {
			// because we are looping
			keyToDownload := key
			downloadThreadPool.AddJob(threading.Job{
				Name: keyToDownload,
				Process: func() error {
					err := DownloadJobProfile(c, hook, keyToDownload)
					if err != nil {
						simplelog.Errorf("unable to download %v, err: %v", keyToDownload, err) // Print instead of Error
						return nil
					}
					m.Lock()
					collected++
					m.Unlock()
					return nil
				},
			})
		}
		if err = downloadThreadPool.ProcessAndWait(); err != nil {
			simplelog.Errorf("job profile download thread pool wait error %v", err)
			fmt.Printf("JOB FAILED - JOB PROFILES COLLECTION - %v\n", err)
		} else {
			fmt.Println("JOB COMPLETED - JOB PROFILES COLLECTION")
		}
	} else {
		simplelog.Info("No job profiles to collect exiting...")
		fmt.Println("JOB FAILED - JOB PROFILES COLLECTION - no profiles to collect")
	}
	return tried, collected, nil
}

func RunCollectJobProfiles(c *conf.CollectConf, hook shutdown.Hook) error {
	simplelog.Info("Collecting Job Profiles...")
	tried, collected, err := GetNumberOfJobProfilesCollected(c, hook)
	if err != nil {
		return err
	}
	simplelog.Debugf("After eliminating duplicates we attempted to collect %v profiles", tried)
	simplelog.Infof("Downloaded %v job profiles", collected)
	return nil
}

func DownloadJobProfile(c *conf.CollectConf, hook shutdown.Hook, jobid string) error {
	var url, apipath string
	if !c.IsDremioCloud() {
		apipath = "/apiv2/support/" + jobid + "/download"
		url = c.DremioEndpoint() + apipath
	} else {
		apipath = "/ui/projects/" + c.DremioCloudProjectID() + "/support/" + jobid + "/download"
		url = c.DremioCloudAppEndpoint() + apipath
	}
	filename := jobid + ".zip"
	headers := map[string]string{"Accept": "application/octet-stream"}
	body, err := restclient.APIRequest(hook, url, c.DremioPATToken(), "POST", headers)
	if err != nil {
		return err
	}
	bodyString := string(body)
	jobProfileFile := path.Clean(path.Join(c.JobProfilesOutDir(), filename))
	file, err := os.Create(path.Clean(jobProfileFile))
	if err != nil {
		return fmt.Errorf("unable to create file %s: %w", filename, err)
	}
	defer ddcio.EnsureClose(filepath.Clean(jobProfileFile), file.Close)
	_, err = fmt.Fprint(file, bodyString)
	if err != nil {
		return fmt.Errorf("unable to write file %s: %w", filename, err)
	}
	return nil
}
