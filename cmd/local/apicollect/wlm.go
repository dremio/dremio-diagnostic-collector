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
	"os"
	"path"
	"path/filepath"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/ddcio"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/restclient"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
)

func RunCollectWLM(c *conf.CollectConf) error {
	err := ValidateAPICredentials(c)
	if err != nil {
		return err
	}
	apiobjects := [][]string{
		{"/api/v3/wlm/queue", "queues.json"},
		{"/api/v3/wlm/rule", "rules.json"},
	}
	for _, apiobject := range apiobjects {
		apipath := apiobject[0]
		filename := apiobject[1]
		url := c.DremioEndpoint() + apipath
		headers := map[string]string{"Content-Type": "application/json"}
		body, err := restclient.APIRequest(url, c.DremioPATToken(), "GET", headers)
		if err != nil {
			return fmt.Errorf("unable to retrieve WLM from %s due to error %v", url, err)
		}
		sb := string(body)
		wlmFile := path.Clean(path.Join(c.WLMOutDir(), filename))
		file, err := os.Create(path.Clean(wlmFile))
		if err != nil {
			return fmt.Errorf("unable to create file %s due to error %v", filename, err)
		}
		defer ddcio.EnsureClose(filepath.Clean(wlmFile), file.Close)
		_, err = fmt.Fprint(file, sb)
		if err != nil {
			return fmt.Errorf("unable to create file %s due to error %v", filename, err)
		}
		simplelog.Infof("SUCCESS - Created " + filename)
	}
	return nil
}
