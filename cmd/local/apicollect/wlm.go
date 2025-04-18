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
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/ddcio"
	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/restclient"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/shutdown"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/simplelog"
)

// RunCollectWLM is a function that collects Workload Management (WLM) data from a Dremio cluster.
// It interacts with Dremio's WLM API endpoints, and collects WLM Queue and Rule information.
func RunCollectWLM(c *conf.CollectConf, hook shutdown.CancelHook) error {
	// Check if the configuration pointer is nil
	if c == nil {
		// Return an error if 'c' is nil
		return errors.New("config pointer is nil")
	}

	// Define the API objects (queues and rules) to be fetched
	var apiobjects [][]string
	if !c.IsDremioCloud() {
		apiobjects = [][]string{
			{"/api/v3/wlm/queue", "queues.json"},
			{"/api/v3/wlm/rule", "rules.json"},
			{"/apiv2/provision/clusters", "awse_engines.json"},
			{"/apiv2/stats/jobsandusers", "cluster_usage.json"},
			// {"/api/v3/cluster/stats", "cluster_stats.json"},
			// {"/api/v3/cluster/jobstats", "cluster_jobstats.json"},
			// {"/api/v3/stats/user", "cluster_userstats.json"},
		}
	} else {
		apiobjects = [][]string{
			{"/v0/projects/" + c.DremioCloudProjectID() + "/engines", "engines.json"},
			{"/v0/projects/" + c.DremioCloudProjectID() + "/rules", "rules.json"},
		}
	}

	// Iterate over each API object
	for _, apiobject := range apiobjects {
		apipath := apiobject[0]
		filename := apiobject[1]

		// Create the URL for the API request
		url := c.DremioEndpoint() + apipath
		headers := map[string]string{"Content-Type": "application/json"}

		// Make a GET request to the respective API endpoint
		body, err := restclient.APIRequest(hook, url, c.DremioPATToken(), "GET", headers)
		// Log and return if there was an error with the API request
		if err != nil {
			return fmt.Errorf("unable to retrieve WLM from %s: %w", url, err)
		}

		// Prepare the output directory and filename
		bodyString := string(body)
		wlmFile := filepath.Join(c.WLMOutDir(), filename)

		// Create a new file in the output directory
		file, err := os.Create(filepath.Clean(wlmFile))
		// Log and return if there was an error with file creation
		if err != nil {
			return fmt.Errorf("unable to create file %s: %w", filename, err)
		}

		defer ddcio.EnsureClose(filepath.Clean(wlmFile), file.Close)

		// Write the API response into the newly created file
		_, err = fmt.Fprint(file, bodyString)
		// Log and return if there was an error with writing to the file
		if err != nil {
			return fmt.Errorf("unable to write to file %s: %w", filename, err)
		}

		// Log a success message upon successful creation of the file
		simplelog.Debugf("SUCCESS - Created %v", filename)
	}

	// Return nil if the entire operation completes successfully
	return nil
}
