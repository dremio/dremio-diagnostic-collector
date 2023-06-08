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

// package autodetect looks at the system configuration and file names and tries to guess at the correct configuration
package autodetect

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/dremio/dremio-diagnostic-collector/cmd/simplelog"
	"github.com/dremio/dremio-diagnostic-collector/pkg/ddcio"
)

func IsAWSEFromJPSOutput(jpsText string) (bool, error) {
	if strings.Contains(jpsText, "DremioDaemon") && strings.Contains(jpsText, "preview") {
		return true, nil
	} else if strings.Contains(jpsText, "AwsDremioDaemon") {
		return true, nil
	}
	return false, nil
}

func IsAWSEExecutorUsingDir(efsFolder, nodeName string) (bool, error) {
	dir, err := os.ReadDir(efsFolder)
	if err != nil {
		return false, err
	}
	simplelog.Debugf("searching for node name %v in %v", nodeName, efsFolder)
	// Iterate over the directory entries
	for _, entry := range dir {
		// Check if the entry is a directory
		if entry.IsDir() {
			simplelog.Debugf("found node named %v in %v", entry.Name(), efsFolder)
			// match the directory name this assumes aws and the node believe they have the same name
			if entry.Name() == nodeName {
				return true, nil
			}
		}
	}
	return false, nil
}

func IsAWSE() (bool, error) {
	var dremioPIDOutput bytes.Buffer
	if err := ddcio.Shell(&dremioPIDOutput, "jps -v"); err != nil {
		return false, fmt.Errorf("grepping from Dremio from jps -v failed %v with output %v", err, dremioPIDOutput.String())
	}
	dremioPIDString := dremioPIDOutput.String()
	return IsAWSEFromJPSOutput(dremioPIDString)
}

func IsAWSEExecutor(nodeName string) (bool, error) {
	//search EFS folder
	// Open the directory
	efsFolder := "/var/dremio_efs/log/executor"
	return IsAWSEExecutorUsingDir(efsFolder, nodeName)
}
