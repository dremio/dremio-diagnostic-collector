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
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
	"github.com/spf13/viper"
)

func calculateDefaultJobProfileNumbers(c *CollectConf) (defaultJobProfilesNumSlowExec, defaultJobProfilesNumRecentErrors, defaultJobProfilesNumSlowPlanning, defaultJobProfilesNumHighQueryCost int) {
	// check if job profile is set
	if c.NumberJobProfilesToCollect() > 0 {
		if c.NumberJobProfilesToCollect() < 4 {
			//so few that it is not worth being clever
			defaultJobProfilesNumSlowExec = c.NumberJobProfilesToCollect()
		} else {
			defaultJobProfilesNumSlowExec = int(float64(c.NumberJobProfilesToCollect()) * 0.4)
			defaultJobProfilesNumRecentErrors = int(float64(c.NumberJobProfilesToCollect()) * 0.2)
			defaultJobProfilesNumSlowPlanning = int(float64(c.NumberJobProfilesToCollect()) * 0.2)
			defaultJobProfilesNumHighQueryCost = int(float64(c.NumberJobProfilesToCollect()) * 0.2)
			//grab the remainder and drop on top of defaultJobProfilesNumSlowExec
			totalAllocated := defaultJobProfilesNumSlowExec + defaultJobProfilesNumRecentErrors + defaultJobProfilesNumSlowPlanning + defaultJobProfilesNumHighQueryCost
			diff := c.NumberJobProfilesToCollect() - totalAllocated
			defaultJobProfilesNumSlowExec += diff
		}
		simplelog.Debugf("setting default values for slow execution profiles: %v, recent error profiles %v, slow planning profiles %v, high query cost profiles %v",
			defaultJobProfilesNumSlowExec,
			defaultJobProfilesNumRecentErrors,
			defaultJobProfilesNumSlowPlanning,
			defaultJobProfilesNumHighQueryCost)
	}
	return
}

func CalculateJobProfileSettingsWithViperConfig(c *CollectConf) (numberJobProfilesToCollect, jobProfilesNumHighQueryCost, jobProfilesNumSlowExec, jobProfilesNumRecentErrors, jobProfilesNumSlowPlanning int) {
	// don't bother doing any of the calculation if personal access token is not present in fact zero out everything
	if c.DremioPATToken() == "" {
		return
	}
	defaultJobProfilesNumSlowExec, defaultJobProfilesNumRecentErrors, defaultJobProfilesNumSlowPlanning, defaultJobProfilesNumHighQueryCost := calculateDefaultJobProfileNumbers(c)
	// job profile specific numbers
	jobProfilesNumHighQueryCost = viper.GetInt(KeyJobProfilesNumHighQueryCost)
	if jobProfilesNumHighQueryCost == 0 {
		//nothing is set, so go ahead and set a default value based on the calculations above
		jobProfilesNumHighQueryCost = defaultJobProfilesNumHighQueryCost
	} else if jobProfilesNumHighQueryCost != defaultJobProfilesNumHighQueryCost {
		simplelog.Warningf("%s changed to %v by configuration", KeyJobProfilesNumHighQueryCost, jobProfilesNumHighQueryCost)
	}

	jobProfilesNumSlowExec = viper.GetInt(KeyJobProfilesNumSlowExec)
	if jobProfilesNumSlowExec == 0 {
		//nothing is set, so go ahead and set a default value based on the calculations above
		jobProfilesNumSlowExec = defaultJobProfilesNumSlowExec
	} else if jobProfilesNumSlowExec != defaultJobProfilesNumSlowExec {
		simplelog.Warningf("%s changed to %v by configuration", KeyJobProfilesNumSlowExec, jobProfilesNumSlowExec)
	}

	jobProfilesNumRecentErrors = viper.GetInt(KeyJobProfilesNumRecentErrors)
	if jobProfilesNumRecentErrors == 0 {
		//nothing is set, so go ahead and set a default value based on the calculations above
		jobProfilesNumRecentErrors = defaultJobProfilesNumRecentErrors
	} else if jobProfilesNumRecentErrors != defaultJobProfilesNumRecentErrors {
		simplelog.Warningf("%s changed to %v by configuration", KeyJobProfilesNumRecentErrors, jobProfilesNumRecentErrors)
	}

	jobProfilesNumSlowPlanning = viper.GetInt(KeyJobProfilesNumSlowPlanning)
	if jobProfilesNumSlowPlanning == 0 {
		jobProfilesNumSlowPlanning = defaultJobProfilesNumSlowPlanning
	} else if jobProfilesNumSlowPlanning != defaultJobProfilesNumSlowPlanning {
		simplelog.Warningf("%s changed to %v by configuration", KeyJobProfilesNumSlowPlanning, jobProfilesNumSlowPlanning)
	}

	numberJobProfilesToCollect = jobProfilesNumSlowExec + jobProfilesNumRecentErrors + jobProfilesNumSlowPlanning + jobProfilesNumHighQueryCost
	if numberJobProfilesToCollect != c.NumberJobProfilesToCollect() {
		simplelog.Warningf("due to configuration parameters new total jobs profiles collected has been adjusted to %v", numberJobProfilesToCollect)
	}
	return
}
