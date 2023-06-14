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
	"path"
	"path/filepath"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/ddcio"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/restclient"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
)

// RunCollectWLM is a function that collects Workload Management (WLM) data from a Dremio cluster.
// It interacts with Dremio's WLM API endpoints, and collects WLM Queue and Rule information.
func RunCollectWLM(c *conf.CollectConf) error {
	// Check if the configuration pointer is nil
	if c == nil {
		// Return an error if 'c' is nil
		return errors.New("config pointer is nil")
	}

	// Validate the Dremio API credentials
	err := ValidateAPICredentials(c)
	if err != nil {
		// Return if the API credentials are not valid
		return err
	}

	// Define the API objects (queues and rules) to be fetched
	apiobjects := [][]string{
		{"/api/v3/wlm/queue", "queues.json"},
		{"/api/v3/wlm/rule", "rules.json"},
	}

	// Iterate over each API object
	for _, apiobject := range apiobjects {
		apipath := apiobject[0]
		filename := apiobject[1]

		// Create the URL for the API request
		url := c.DremioEndpoint() + apipath
		headers := map[string]string{"Content-Type": "application/json"}

		// Make a GET request to the respective API endpoint
		body, err := restclient.APIRequest(url, c.DremioPATToken(), "GET", headers)

		// Log and return if there was an error with the API request
		if err != nil {
			return fmt.Errorf("unable to retrieve WLM from %s due to error %v", url, err)
		}

		// Prepare the output directory and filename
		sb := string(body)
		wlmFile := path.Clean(filepath.Join(c.WLMOutDir(), filename))

		// Create a new file in the output directory
		file, err := os.Create(filepath.Clean(wlmFile))

		// Log and return if there was an error with file creation
		if err != nil {
			return fmt.Errorf("unable to create file %s due to error %v", filename, err)
		}

		defer ddcio.EnsureClose(filepath.Clean(wlmFile), file.Close)

		// Write the API response into the newly created file
		_, err = fmt.Fprint(file, sb)

		// Log and return if there was an error with writing to the file
		if err != nil {
			return fmt.Errorf("unable to write to file %s due to error %v", filename, err)
		}

		// Log a success message upon successful creation of the file
		simplelog.Infof("SUCCESS - Created " + filename)
	}

	// Return nil if the entire operation completes successfully
	return nil
}
