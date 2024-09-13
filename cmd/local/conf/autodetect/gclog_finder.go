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
	"path"
	"strings"

	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/ddcio"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/shutdown"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/simplelog"
)

const jdk8GCLoggingFLag = "-Xloggc:"
const jdk9UnifiedGCLoggingFlag = "-Xlog:"

// FindGCLogLocation retrieves the gc log location from ps eww <pid> output
func FindGCLogLocation(hook shutdown.Hook, pid int) (gcLogPattern string, gcLogLoc string, err error) {
	var psEWW bytes.Buffer

	// remove the header with tail -n 1
	err = ddcio.Shell(hook, &psEWW, fmt.Sprintf("ps eww %v | tail -n 1", pid))
	if err != nil {
		return "", "", fmt.Errorf("unable to find gc logs due to error '%v'", err)
	}

	data := strings.TrimSpace(psEWW.String())
	lines := len(strings.Split(data, "\n"))
	if lines == 0 {
		return "", "", fmt.Errorf("empty ps eww %v output cannot find gc logs", pid)
	}
	if lines > 1 {
		return "", "", fmt.Errorf("to many results in the ps eww %v output cannot find gc logs: '%v'", pid, data)
	}
	var startupFlags string
	tokens := strings.Split(data, " ")
	if len(tokens) > 0 {
		startupFlags = strings.Join(tokens[1:], " ")
	}

	if startupFlags == "" {
		return "", "", fmt.Errorf("unable to find gc logs because there was no matching pid %v found in the jps -v output: '%v'", pid, psEWW)
	}
	logRegex, logLocation, err := ParseGCLogFromFlags(startupFlags)
	if err != nil {
		return "", "", fmt.Errorf("unable to find gc logs due to error '%v'", err)
	}
	if logLocation == "" {
		simplelog.Warningf("autodetection of gc logs location failed as no %v or %v flag was found in the startup flags: '%v'", jdk8GCLoggingFLag, jdk9UnifiedGCLoggingFlag, startupFlags)
		return "", "", nil
	}
	simplelog.Infof("detected gc log directory at '%v'", logLocation)
	if logRegex == "" {
		simplelog.Warningf("autodetection of gc logs location failed we were unable to determine gc log regex: '%v'", startupFlags)
		return "", "", nil
	}
	simplelog.Infof("detected gc log pattern at '%v'", logRegex)
	return logRegex, logLocation, nil
}

// ParseGCLogFromFlags takes a given string with java startup flags and finds the gclog directive
func ParseGCLogFromFlags(startupFlagsStr string) (logRegex string, gcLogLocation string, err error) {
	logRegex, logDir, errorFromPost25 := ParseGCLogFromFlagsPost25(startupFlagsStr)
	if logDir == "" {
		logRegex, logDir, err := ParseGCLogFromFlagsPre25(startupFlagsStr)
		if err != nil {
			return "", "", fmt.Errorf("uanble to parse gc flags due the following errors: '%v' and '%v'", errorFromPost25, err)
		}
		return logRegex, logDir, nil
	}
	return logRegex, logDir, nil
}

// ParseGCLogFromFlags takes a given string with java startup flags and finds the gclog directive
func ParseGCLogFromFlagsPost25(startupFlagsStr string) (logRegex string, gcLogLocation string, err error) {
	tokens := strings.Split(startupFlagsStr, " ")
	var found []int
	for i, token := range tokens {
		if strings.HasPrefix(token, jdk9UnifiedGCLoggingFlag) {
			found = append(found, i)
		}
	}
	if len(found) == 0 {
		return "", "", nil
	}
	lastIndex := found[len(found)-1]
	last := tokens[lastIndex]
	gcLogLocationTokens := strings.Split(last, jdk9UnifiedGCLoggingFlag)
	if len(gcLogLocationTokens) != 2 {
		return "", "", fmt.Errorf("unexpected items in string '%v', expected only 2 items but found %v", last, len(gcLogLocationTokens))
	}
	tokens = strings.Split(gcLogLocationTokens[1], ":")
	for _, t := range tokens {
		if strings.HasPrefix(t, "file=") {
			gcPath := strings.Split(t, "file=")[1]
			gcLogDir := path.Dir(gcPath)
			gcRegex := fmt.Sprintf("*%v*", path.Base(gcPath))
			// unified logging lets you add the timestamp, just doing a * here
			gcRegex = strings.ReplaceAll(gcRegex, "%t", "*")
			// unified logging lets you set the pid also just doing *
			gcRegex = strings.ReplaceAll(gcRegex, "%p", "*")
			return gcRegex, gcLogDir, nil
		}
	}

	return "", "", fmt.Errorf("could not find an %v parameter with file= in the string %v", jdk9UnifiedGCLoggingFlag, startupFlagsStr)
}

// ParseGCLogFromFlags takes a given string with java startup flags and finds the gclog directive
func ParseGCLogFromFlagsPre25(startupFlagsStr string) (logRegex string, gcLogLocation string, err error) {
	tokens := strings.Split(startupFlagsStr, " ")
	var found []int
	for i, token := range tokens {
		if strings.HasPrefix(token, jdk8GCLoggingFLag) {
			found = append(found, i)
		}
	}
	if len(found) == 0 {
		return "", "", nil
	}
	lastIndex := found[len(found)-1]
	last := tokens[lastIndex]
	gcLogLocationTokens := strings.Split(last, jdk8GCLoggingFLag)
	if len(gcLogLocationTokens) != 2 {
		return "", "", fmt.Errorf("unexpected items in string '%v', expected only 2 items but found %v", last, len(gcLogLocationTokens))
	}
	gcPath := gcLogLocationTokens[1]
	// get the file arg
	gcRegex := fmt.Sprintf("*%v*", path.Base(gcPath))
	// since jdk8 lets you add the timestamp, just doing a * here
	gcRegex = strings.ReplaceAll(gcRegex, "%t", "*")
	// since jdk8 lets you set the pid also just doing *
	gcRegex = strings.ReplaceAll(gcRegex, "%p", "*")
	return gcRegex, path.Dir(gcPath), nil
}
