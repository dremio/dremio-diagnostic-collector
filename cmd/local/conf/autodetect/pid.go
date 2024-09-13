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
	"strconv"
	"strings"

	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/ddcio"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/shutdown"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/simplelog"
)

// GetDremioPIDFromText takes the ouput from
// "ps aux | grep DremioDaemon | grep -v grep | grep -v /etc/dremio/preview"
// and retrieves the pid
func GetDremioPIDFromText(psOutput string) (int, error) {
	// should always trim trailing spaces
	cleanedOutput := strings.TrimSpace(psOutput)
	linesCount := len((strings.Split(cleanedOutput, "\n")))
	if linesCount > 1 {
		return -1, fmt.Errorf("to many lines in the ps outout, should only be one line '%v'", cleanedOutput)
	}
	if linesCount == 0 {
		return -1, fmt.Errorf("no lines in the ps output, should be one line '%v'", cleanedOutput)
	}

	tokens := strings.Split(cleanedOutput, " ")
	var cleaned []string
	for _, t := range tokens {
		if t == "" {
			continue
		}
		cleaned = append(cleaned, t)
	}
	if len(cleaned) < 2 {
		return -1, fmt.Errorf("no pid for dremio found in text '%v'", cleanedOutput)
	}
	pidText := cleaned[1]
	return strconv.Atoi(pidText)
}

// GetDremioPID calls ps aux and finds the DremioDaemon (filtering out the preview engine)
func GetDremioPID(hook shutdown.Hook) (int, error) {
	var psOutput bytes.Buffer
	if err := ddcio.Shell(hook, &psOutput, "ps aux | grep DremioDaemon | grep -v grep | grep -v /etc/dremio/preview"); err != nil {
		simplelog.Warningf("attempting to get full ps aux output failed: %v", err)
	}
	return GetDremioPIDFromText(psOutput.String())
}
