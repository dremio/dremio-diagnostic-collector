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

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/ddcio"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/queriesjson"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/restclient"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/threading"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
)

func GetNumberOfJobProfilesCollected(c *conf.CollectConf) (tried, collected int, err error) {
	var files []fs.DirEntry
	var queriesrows []queriesjson.QueriesRow
	if !c.IsDremioCloud() {
		files, err = os.ReadDir(c.QueriesOutDir())
		if err != nil {
			return 0, 0, err
		}
		queriesjsons := []string{}
		for _, file := range files {
			queriesjsons = append(queriesjsons, path.Join(c.QueriesOutDir(), file.Name()))
		}

		if len(queriesjsons) == 0 {
			simplelog.Warning("no queries.json files found. This is probably an executor, so we are skipping collection of Job Profiles")
			return
		}

		queriesrows = queriesjson.CollectQueriesJSON(queriesjsons)
	} else {
		files, err = os.ReadDir(c.SystemTablesOutDir())
		if err != nil {
			return 0, 0, err
		}
		jobhistoryjsons := []string{}
		for _, file := range files {
			if strings.Contains(file.Name(), "project.history.jobs") {
				jobhistoryjsons = append(jobhistoryjsons, path.Join(c.SystemTablesOutDir(), file.Name()))
			}
		}

		if len(jobhistoryjsons) == 0 {
			simplelog.Warning("no valid records or sys.project.history.jobs.json files found. Therefore, we are skipping collection of Job Profiles")
			return
		}

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
	if len(profilesToCollect) > 0 {
		simplelog.Debugf("Downloading %v job profiles...", len(profilesToCollect))
		downloadThreadPool := threading.NewThreadPoolWithJobQueue(c.NumberThreads(), len(profilesToCollect), 100)
		for key := range profilesToCollect {
			//because we are looping
			keyToDownload := key
			downloadThreadPool.AddJob(func() error {
				err := DownloadJobProfile(c, keyToDownload)
				if err != nil {
					simplelog.Errorf("unable to download job profile %v, due to error %v", keyToDownload, err) // Print instead of Error
				}
				return nil
			})
			collected++
		}
		if err = downloadThreadPool.ProcessAndWait(); err != nil {
			simplelog.Errorf("job profile download thread pool wait error %v", err)
		}
	} else {
		simplelog.Info("No job profiles to collect exiting...")
	}
	return tried, collected, nil
}

func RunCollectJobProfiles(c *conf.CollectConf) error {
	simplelog.Info("Collecting Job Profiles...")
	err := ValidateAPICredentials(c)
	if err != nil {
		return err
	}
	tried, collected, err := GetNumberOfJobProfilesCollected(c)
	if err != nil {
		return err
	}
	simplelog.Debugf("After eliminating duplicates we are tried to collect %v profiles", tried)
	simplelog.Infof("Downloaded %v job profiles", collected)
	return nil
}

func DownloadJobProfile(c *conf.CollectConf, jobid string) error {
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
	body, err := restclient.APIRequest(url, c.DremioPATToken(), "POST", headers)
	if err != nil {
		return err
	}
	sb := string(body)
	jobProfileFile := path.Clean(path.Join(c.JobProfilesOutDir(), filename))
	file, err := os.Create(path.Clean(jobProfileFile))
	if err != nil {
		return fmt.Errorf("unable to create file %s due to error %v", filename, err)
	}
	defer ddcio.EnsureClose(filepath.Clean(jobProfileFile), file.Close)
	_, err = fmt.Fprint(file, sb)
	if err != nil {
		return fmt.Errorf("unable to create file %s due to error %v", filename, err)
	}
	return nil
}
