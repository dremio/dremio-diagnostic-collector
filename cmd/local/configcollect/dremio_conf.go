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

package configcollect

import (
	"path/filepath"

	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/ddcio"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/masking"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/shutdown"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/simplelog"
)

func RunCollectDremioConfig(c *conf.CollectConf, _ shutdown.CancelHook) error {
	simplelog.Debugf("Collecting Configuration Information from %v ...", c.NodeName())

	dremioConfDest := filepath.Join(c.ConfigurationOutDir(), "dremio.conf")
	err := ddcio.CopyFile(filepath.Join(c.DremioConfDir(), "dremio.conf"), dremioConfDest)
	if err != nil {
		simplelog.Warningf("unable to copy dremio.conf: %v", err)
	}
	simplelog.Debugf("masking passwords in dremio.conf")
	if err := masking.RemoveSecretsFromDremioConf(dremioConfDest); err != nil {
		simplelog.Warningf("UNABLE TO MASK SECRETS in dremio.conf: %v", err)
	}
	err = ddcio.CopyFile(filepath.Join(c.DremioConfDir(), "dremio-env"), filepath.Join(c.ConfigurationOutDir(), "dremio-env"))
	if err != nil {
		simplelog.Warningf("unable to copy dremio-env: %v", err)
	}
	err = ddcio.CopyFile(filepath.Join(c.DremioConfDir(), "logback.xml"), filepath.Join(c.ConfigurationOutDir(), "logback.xml"))
	if err != nil {
		simplelog.Warningf("unable to copy logback.xml: %v", err)
	}
	err = ddcio.CopyFile(filepath.Join(c.DremioConfDir(), "logback-access.xml"), filepath.Join(c.ConfigurationOutDir(), "logback-access.xml"))
	if err != nil {
		simplelog.Warningf("unable to copy logback-access.xml: %v", err)
	}
	simplelog.Debugf("... Collecting Configuration Information from %v COMPLETED", c.NodeName())

	return nil
}
