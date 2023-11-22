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

// package jvmcollect handles parsing of the jvm information
package jvmcollect

import (
	"bytes"
	"fmt"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/ddcio"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
)

func RunCollectJFR(c *conf.CollectConf) error {
	var w bytes.Buffer
	w = bytes.Buffer{}
	if err := ddcio.Shell(&w, fmt.Sprintf("jcmd %v VM.unlock_commercial_features", c.DremioPID())); err != nil {
		simplelog.Warningf("Error trying to unlock commercial features %v. Note: newer versions of OpenJDK do not support the call VM.unlock_commercial_features. This is usually safe to ignore", err)
	}
	simplelog.Debugf("node: %v - jfr unlock commercial output - %v", c.NodeName(), w.String())

	w = bytes.Buffer{}
	if err := ddcio.Shell(&w, fmt.Sprintf("jcmd %v JFR.stop name=\"DREMIO_JFR\"", c.DremioPID())); err != nil {
		simplelog.Debugf("attempting to stop existing JFR failed, but this is usually expected: '%v' -- output: '%v'", err, w.String())
	} else {
		simplelog.Warningf("JFR named DREMIO_JFR was running on PID %v and has been stopped so we could get a fresh collection: '%v'", c.DremioPID(), w.String())
	}

	w = bytes.Buffer{}
	if err := ddcio.Shell(&w, fmt.Sprintf("jcmd %v JFR.start name=\"DREMIO_JFR\" settings=profile maxage=%vs  filename=%v/%v.jfr dumponexit=true", c.DremioPID(), c.DremioJFRTimeSeconds(), c.JFROutDir(), c.NodeName())); err != nil {
		return fmt.Errorf("unable to run JFR due to error %v", err)
	}
	simplelog.Debugf("node: %v - jfr start output - %v", c.NodeName(), w.String())
	secondsWaiting := c.DremioJFRTimeSeconds()
	time.Sleep(time.Duration(secondsWaiting) * time.Second)
	// do not "optimize". the recording first needs to be stopped for all processes before collecting the data.
	simplelog.Debugf("... stopping JFR %v", c.NodeName())
	w = bytes.Buffer{}
	if err := ddcio.Shell(&w, fmt.Sprintf("jcmd %v JFR.dump name=\"DREMIO_JFR\"", c.DremioPID())); err != nil {
		return fmt.Errorf("unable to dump JFR due to error %v", err)
	}
	simplelog.Debugf("node: %v - jfr dump output %v", c.NodeName(), w.String())
	w = bytes.Buffer{}
	if err := ddcio.Shell(&w, fmt.Sprintf("jcmd %v JFR.stop name=\"DREMIO_JFR\"", c.DremioPID())); err != nil {
		return fmt.Errorf("unable to dump JFR due to error %v", err)
	}
	simplelog.Debugf("node: %v - jfr stop output %v", c.NodeName(), w.String())

	return nil
}
