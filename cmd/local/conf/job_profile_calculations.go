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

package conf

import (
	"github.com/dremio/dremio-diagnostic-collector/cmd/simplelog"
	"github.com/spf13/viper"
)

func CalculateJobProfileSettings(c *CollectConf) (numberJobProfilesToCollect, jobProfilesNumHighQueryCost, jobProfilesNumSlowExec, jobProfilesNumRecentErrors, jobProfilesNumSlowPlanning int) {
	// don't bother doing any of the calculation if personal access token is not present in fact zero out everything
	if c.DremioPATToken() == "" {
		return
	}
	// check if job profile is set
	var defaultJobProfilesNumSlowExec int
	var defaultJobProfilesNumRecentErrors int
	var defaultJobProfilesNumSlowPlanning int
	var defaultJobProfilesNumHighQueryCost int
	if c.NumberJobProfilesToCollect() > 0 {
		if c.NumberJobProfilesToCollect() < 4 {
			//so few that it is not worth being clever
			defaultJobProfilesNumSlowExec = c.NumberJobProfilesToCollect()
		} else {
			defaultJobProfilesNumSlowExec = int(float64(c.NumberJobProfilesToCollect()) * 0.4)
			defaultJobProfilesNumRecentErrors = int(float64(defaultJobProfilesNumRecentErrors) * 0.2)
			defaultJobProfilesNumSlowPlanning = int(float64(defaultJobProfilesNumSlowPlanning) * 0.2)
			defaultJobProfilesNumHighQueryCost = int(float64(defaultJobProfilesNumHighQueryCost) * 0.2)
			//grab the remainder and drop on top of defaultJobProfilesNumSlowExec
			totalAllocated := defaultJobProfilesNumSlowExec + defaultJobProfilesNumRecentErrors + defaultJobProfilesNumSlowPlanning + defaultJobProfilesNumHighQueryCost
			diff := c.NumberJobProfilesToCollect() - totalAllocated
			defaultJobProfilesNumSlowExec += diff
		}
		simplelog.Infof("setting default values for slow execution profiles: %v, recent error profiles %v, slow planning profiles %v, high query cost profiles %v",
			defaultJobProfilesNumSlowExec,
			defaultJobProfilesNumRecentErrors,
			defaultJobProfilesNumSlowPlanning,
			defaultJobProfilesNumHighQueryCost)
	}

	// job profile specific numbers
	jobProfilesNumHighQueryCost = viper.GetInt(KeyJobProfilesNumHighQueryCost)
	if c.JobProfilesNumHighQueryCost() == 0 {
		//nothing is set, so go ahead and set a default value based on the calculations above
		jobProfilesNumHighQueryCost = defaultJobProfilesNumHighQueryCost
	} else if c.JobProfilesNumHighQueryCost() != defaultJobProfilesNumHighQueryCost {
		simplelog.Warningf("%s changed to %v by configuration", KeyJobProfilesNumHighQueryCost, jobProfilesNumHighQueryCost)
	}

	jobProfilesNumSlowExec = viper.GetInt(KeyJobProfilesNumSlowExec)
	if c.JobProfilesNumSlowExec() == 0 {
		//nothing is set, so go ahead and set a default value based on the calculations above
		jobProfilesNumSlowExec = defaultJobProfilesNumSlowExec
	} else if c.JobProfilesNumSlowExec() != defaultJobProfilesNumSlowExec {
		simplelog.Warningf("%s changed to %v by configuration", KeyJobProfilesNumSlowExec, c.JobProfilesNumSlowExec())
	}

	jobProfilesNumRecentErrors = viper.GetInt(KeyJobProfilesNumRecentErrors)
	if c.JobProfilesNumRecentErrors() == 0 {
		//nothing is set, so go ahead and set a default value based on the calculations above
		jobProfilesNumRecentErrors = defaultJobProfilesNumRecentErrors
	} else if c.JobProfilesNumRecentErrors() != defaultJobProfilesNumRecentErrors {
		simplelog.Warningf("%s changed to %v by configuration", KeyJobProfilesNumRecentErrors, c.JobProfilesNumRecentErrors())
	}

	jobProfilesNumSlowPlanning = viper.GetInt(KeyJobProfilesNumSlowPlanning)
	if c.JobProfilesNumSlowPlanning() == 0 {
		jobProfilesNumSlowPlanning = defaultJobProfilesNumSlowPlanning
	} else if c.JobProfilesNumSlowPlanning() != defaultJobProfilesNumSlowPlanning {
		simplelog.Warningf("%s changed to %v by configuration", KeyJobProfilesNumSlowPlanning, c.JobProfilesNumSlowPlanning())
	}

	totalAllocated := defaultJobProfilesNumSlowExec + defaultJobProfilesNumRecentErrors + defaultJobProfilesNumSlowPlanning + defaultJobProfilesNumHighQueryCost
	if totalAllocated > 0 && totalAllocated != c.NumberJobProfilesToCollect() {
		numberJobProfilesToCollect = totalAllocated
		simplelog.Warningf("due to configuration parameters new total jobs profiles collected has been adjusted to %v", totalAllocated)
	}
	return
}
