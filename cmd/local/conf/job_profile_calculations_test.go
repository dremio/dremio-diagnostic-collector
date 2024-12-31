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
	"testing"
)

func TestCalculateDefaultJobProfileSettingsWithLessThan4Jobs(t *testing.T) {
	c := &CollectConf{
		dremioPATToken:             "abc",
		numberJobProfilesToCollect: 3,
	}
	// put them all in slow exec since we are dealing with fractions here
	numSlowExec, numErrors, numSlowPlan, numHighCost := calculateDefaultJobProfileNumbers(c)

	if numHighCost != 0 {
		t.Errorf("expected 0 but got %v", numHighCost)
	}
	if numSlowExec != 3 {
		t.Errorf("expected 3 but got %v", numSlowExec)
	}
	if numErrors != 0 {
		t.Errorf("expected 0 but got %v", numErrors)
	}
	if numSlowPlan != 0 {
		t.Errorf("expected 0 but got %v", numSlowPlan)
	}
}

func TestCalculateDefaultJobProfileSettingsWith0Jobs(t *testing.T) {
	c := &CollectConf{
		dremioPATToken:             "abc",
		numberJobProfilesToCollect: 0,
	}
	// put them all in slow exec since we are dealing with fractions here
	numSlowExec, numErrors, numSlowPlan, numHighCost := calculateDefaultJobProfileNumbers(c)

	if numHighCost != 0 {
		t.Errorf("expected 0 but got %v", numHighCost)
	}
	if numSlowExec != 0 {
		t.Errorf("expected 0 but got %v", numSlowExec)
	}
	if numErrors != 0 {
		t.Errorf("expected 0 but got %v", numErrors)
	}
	if numSlowPlan != 0 {
		t.Errorf("expected 0 but got %v", numSlowPlan)
	}
}

func TestCalculateDefaultJobProfileSettingsWith10Jobs(t *testing.T) {
	c := &CollectConf{
		dremioPATToken:             "abc",
		numberJobProfilesToCollect: 10,
	}
	// put them all in slow exec since we are dealing with fractions here
	numSlowExec, numErrors, numSlowPlan, numHighCost := calculateDefaultJobProfileNumbers(c)
	if numHighCost != 2 {
		t.Errorf("expected 2 but got %v", numHighCost)
	}
	if numSlowExec != 4 {
		t.Errorf("expected 2 but got %v", numSlowExec)
	}
	if numErrors != 2 {
		t.Errorf("expected 2 but got %v", numErrors)
	}
	if numSlowPlan != 2 {
		t.Errorf("expected 4 but got %v", numSlowPlan)
	}
}

func TestCalculateDefaultJobProfileSettingsWith25000Jobs(t *testing.T) {
	c := &CollectConf{
		dremioPATToken:             "abc",
		numberJobProfilesToCollect: 25000,
	}
	// put them all in slow exec since we are dealing with fractions here
	numSlowExec, numErrors, numSlowPlan, numHighCost := calculateDefaultJobProfileNumbers(c)
	if numHighCost != 5000 {
		t.Errorf("expected 5000 but got %v", numHighCost)
	}
	if numSlowExec != 10000 {
		t.Errorf("expected 10000 but got %v", numSlowExec)
	}
	if numErrors != 5000 {
		t.Errorf("expected 5000 but got %v", numErrors)
	}
	if numSlowPlan != 5000 {
		t.Errorf("expected 5000 but got %v", numSlowPlan)
	}
}

func TestCalculateJobProfileSettingsWith25000JobsAndEmptyViperConfigAndNoPat(t *testing.T) {
	c := &CollectConf{
		dremioPATToken:             "",
		numberJobProfilesToCollect: 25000,
	}
	// put them all in slow exec since we are dealing with fractions here
	numberProfiles, numHighCost, numSlowExec, numErrors, numSlowPlan := CalculateJobProfileSettingsWithViperConfig(c)
	if numberProfiles != 0 {
		t.Errorf("expected 0 but got %v", numberProfiles)
	}
	if numHighCost != 0 {
		t.Errorf("expected 0 but got %v", numHighCost)
	}
	if numSlowExec != 0 {
		t.Errorf("expected 0 but got %v", numSlowExec)
	}
	if numErrors != 0 {
		t.Errorf("expected 0 but got %v", numErrors)
	}
	if numSlowPlan != 0 {
		t.Errorf("expected 0 but got %v", numSlowPlan)
	}
}

func TestCalculateJobProfileSettingsWith25000JobsAndEmptyViperConfig(t *testing.T) {
	c := &CollectConf{
		dremioPATToken:             "abc",
		numberJobProfilesToCollect: 25000,
	}
	// put them all in slow exec since we are dealing with fractions here
	numberProfiles, numHighCost, numSlowExec, numErrors, numSlowPlan := CalculateJobProfileSettingsWithViperConfig(c)
	if numberProfiles != 25000 {
		t.Errorf("expected 25000 but got %v", numberProfiles)
	}
	if numHighCost != 5000 {
		t.Errorf("expected 5000 but got %v", numHighCost)
	}
	if numSlowExec != 10000 {
		t.Errorf("expected 10000 but got %v", numSlowExec)
	}
	if numErrors != 5000 {
		t.Errorf("expected 5000 but got %v", numErrors)
	}
	if numSlowPlan != 5000 {
		t.Errorf("expected 5000 but got %v", numSlowPlan)
	}
}

func TestCalculateJobProfileSettingsWith25000JobsAndWithViperOverride(t *testing.T) {
	c := &CollectConf{
		dremioPATToken:              "abc",
		numberJobProfilesToCollect:  25000,
		jobProfilesNumSlowExec:      50,
		jobProfilesNumRecentErrors:  150,
		jobProfilesNumSlowPlanning:  250,
		jobProfilesNumHighQueryCost: 350,
	}

	// put them all in slow exec since we are dealing with fractions here
	numberProfiles, numHighCost, numSlowExec, numErrors, numSlowPlan := CalculateJobProfileSettingsWithViperConfig(c)
	if numberProfiles != 800 {
		t.Errorf("expected 800 but got %v", numberProfiles)
	}
	if numHighCost != 350 {
		t.Errorf("expected 350 but got %v", numHighCost)
	}
	if numSlowExec != 50 {
		t.Errorf("expected 50 but got %v", numSlowExec)
	}
	if numErrors != 150 {
		t.Errorf("expected 150 but got %v", numErrors)
	}
	if numSlowPlan != 250 {
		t.Errorf("expected 250 but got %v", numSlowPlan)
	}
}

func TestCalculateJobProfileSettingsWith25000JobsAndWithViperOverrideAndNoPat(t *testing.T) {
	c := &CollectConf{
		dremioPATToken:              "",
		numberJobProfilesToCollect:  25000,
		jobProfilesNumSlowExec:      50,
		jobProfilesNumRecentErrors:  150,
		jobProfilesNumSlowPlanning:  250,
		jobProfilesNumHighQueryCost: 350,
	}
	// put them all in slow exec since we are dealing with fractions here
	numberProfiles, numHighCost, numSlowExec, numErrors, numSlowPlan := CalculateJobProfileSettingsWithViperConfig(c)
	if numberProfiles != 0 {
		t.Errorf("expected 0 but got %v", numberProfiles)
	}
	if numHighCost != 0 {
		t.Errorf("expected 0 but got %v", numHighCost)
	}
	if numSlowExec != 0 {
		t.Errorf("expected 0 but got %v", numSlowExec)
	}
	if numErrors != 0 {
		t.Errorf("expected 0 but got %v", numErrors)
	}
	if numSlowPlan != 0 {
		t.Errorf("expected 0 but got %v", numSlowPlan)
	}
}
