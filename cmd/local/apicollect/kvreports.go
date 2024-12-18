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

	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/ddcio"
	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/restclient"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/shutdown"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/simplelog"
)

func RunCollectKvReport(c *conf.CollectConf, hook shutdown.CancelHook) error {
	filename := "kvstore-report.zip"
	apipath := "/apiv2/kvstore/report"
	url := c.DremioEndpoint() + apipath
	headers := map[string]string{"Accept": "application/octet-stream"}
	body, err := restclient.APIRequest(hook, url, c.DremioPATToken(), "GET", headers)
	if err != nil {
		return fmt.Errorf("unable to retrieve KV store report from %s due to error %v", url, err)
	}
	sb := string(body)
	kvStoreReportFile := path.Join(c.KVstoreOutDir(), filename)
	file, err := os.Create(path.Clean(kvStoreReportFile))
	if err != nil {
		return fmt.Errorf("unable to create file %s due to error %v", filename, err)
	}
	defer ddcio.EnsureClose(filepath.Clean(kvStoreReportFile), file.Close)
	_, err = fmt.Fprint(file, sb)
	if err != nil {
		return fmt.Errorf("unable to create file %s due to error %v", filename, err)
	}
	simplelog.Debugf("SUCCESS - Created %v", filename)
	return nil
}
