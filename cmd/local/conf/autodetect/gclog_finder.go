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
	"bufio"
	"bytes"
	"fmt"
	"path"
	"strings"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/ddcio"
)

// findGCLogLocation retrieves the gc log location with a search string to greedily retrieve everything by prefix
func FindGCLogLocation() (gcLogLoc string, err error) {

	var jpsVerbose bytes.Buffer
	err = ddcio.Shell(&jpsVerbose, "jps -v")
	if err != nil {
		return "", fmt.Errorf("unable to find gc logs due to error '%v'", err)
	}
	pid, err := GetDremioPID()
	if err != nil {
		return "", fmt.Errorf("unable to find gc logs due to error '%v'", err)
	}
	var startupFlags string
	scanner := bufio.NewScanner(&jpsVerbose)
	for scanner.Scan() {
		line := scanner.Text()
		tokens := strings.Split(line, " ")
		if len(tokens) > 0 {
			potentialPid := strings.TrimSpace(tokens[0])
			if potentialPid == fmt.Sprintf("%d", pid) {
				startupFlags = strings.Join(tokens[1:], " ")
			}
		}
	}
	logLocation, err := ParseGCLogFromFlags(startupFlags)
	if err != nil {
		return "", fmt.Errorf("unable to find gc logs due to error '%v'", err)
	}
	if logLocation != "" {
		return logLocation, nil
	}
	return "", nil
}

// ParseGCLogFromFlags takes a given string with java startup flags and finds the gclog directive
func ParseGCLogFromFlags(startupFlagsStr string) (gcLogLocation string, err error) {
	logDir, errorFromPost25 := ParseGCLogFromFlagsPost25(startupFlagsStr)
	if logDir == "" {
		logDir, err := ParseGCLogFromFlagsPre25(startupFlagsStr)
		if err != nil {
			return "", fmt.Errorf("uanble to parse gc flags due the following errors: '%v' and '%v'", errorFromPost25, err)
		}
		return logDir, nil
	}
	return logDir, nil
}

// ParseGCLogFromFlags takes a given string with java startup flags and finds the gclog directive
func ParseGCLogFromFlagsPost25(startupFlagsStr string) (gcLogLocation string, err error) {
	tokens := strings.Split(startupFlagsStr, " ")
	var found []int
	for i, token := range tokens {
		if strings.HasPrefix(token, "-Xlog:") {
			found = append(found, i)
		}
	}
	if len(found) == 0 {
		return "", nil
	}
	lastIndex := found[len(found)-1]
	last := tokens[lastIndex]
	gcLogLocationTokens := strings.Split(last, "-Xlog:")
	if len(gcLogLocationTokens) != 2 {
		return "", fmt.Errorf("unexpected items in string '%v', expected only 2 items but found %v", last, len(gcLogLocationTokens))
	}
	tokens = strings.Split(gcLogLocationTokens[1], ":")
	for _, t := range tokens {
		if strings.HasPrefix(t, "file=") {
			return path.Dir(strings.Split(t, "file=")[1]), nil
		}
	}
	return "", fmt.Errorf("could not find an Xlog parameter with file= in the string %v", startupFlagsStr)
}

// ParseGCLogFromFlags takes a given string with java startup flags and finds the gclog directive
func ParseGCLogFromFlagsPre25(startupFlagsStr string) (gcLogLocation string, err error) {
	tokens := strings.Split(startupFlagsStr, " ")
	var found []int
	for i, token := range tokens {
		if strings.HasPrefix(token, "-Xloggc:") {
			found = append(found, i)
		}
	}
	if len(found) == 0 {
		return "", nil
	}
	lastIndex := found[len(found)-1]
	last := tokens[lastIndex]
	gcLogLocationTokens := strings.Split(last, "-Xloggc:")
	if len(gcLogLocationTokens) != 2 {
		return "", fmt.Errorf("unexpected items in string '%v', expected only 2 items but found %v", last, len(gcLogLocationTokens))
	}
	return path.Dir(gcLogLocationTokens[1]), nil
}
