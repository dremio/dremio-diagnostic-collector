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
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/local/ddcio"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/shutdown"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/simplelog"
)

const (
	jdk8GCLoggingFLag        = "-Xloggc:"
	jdk9UnifiedGCLoggingFlag = "-Xlog:"
)

// FindGCLogLocation retrieves the gc log location from ps eww <pid> output
func FindGCLogLocation(hook shutdown.Hook, pid int) (gcLogPattern string, gcLogLoc string, err error) {
	var psEWW bytes.Buffer

	// remove the header with tail -n 1
	err = ddcio.Shell(hook, &psEWW, fmt.Sprintf("ps eww %v | tail -n 1", pid))
	if err != nil {
		return "", "", fmt.Errorf("unable to find gc logs: %w", err)
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
		return "", "", fmt.Errorf("unable to find gc logs: %w", err)
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
			return "", "", fmt.Errorf("unable to parse gc flags: '%w' and '%w'", errorFromPost25, err)
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

// gcFallbackPatterns are file patterns that match GC log files across JDK versions.
var gcFallbackPatterns = []string{"gc*.log*", "*.gc.log*", "gc-*.log*", "server*.gc*"}

// gcMarkers are strings found in the first few lines of GC log files across JDK versions.
var gcMarkers = []string{
	"[gc",         // JDK 11+ unified logging: [gc, [gc,start
	"GC(",         // JDK 8: GC(0), GC(1)
	"Heap",        // JDK 8/11: Heap region size, Heap before GC
	"CommandLine", // JDK 11+: CommandLine flags
	"Using G1",    // JDK 8+: Using G1
	"Memory:",     // JDK 11+: Memory: 4k page
	"CPUs:",       // JDK 11+: CPUs: 4 total
	"[Times:",     // JDK 8: [Times: user=0.01 sys=0.00, real=0.00 secs]
	"-XX:",        // JDK 8/11: CommandLineFlags: -XX:InitialHeapSize=...
	"Total time",  // JDK 8: Total time for which application threads were stopped
}

// FindGCLogsFallback scans common directories for GC log files when JVM flag
// detection fails. It restricts results to files matching known GC log patterns,
// modified within the given number of days, and whose first lines contain GC markers.
func FindGCLogsFallback(dremioLogDir string, searchDirs []string, collectionDays int) (gcLogPattern string, gcLogDir string) {
	if dremioLogDir != "" {
		// Prepend dremio log dir as the highest-priority search location
		searchDirs = append([]string{dremioLogDir}, searchDirs...)
	}

	cutoff := time.Now().AddDate(0, 0, -collectionDays)

	for _, dir := range searchDirs {
		if dir == "" {
			continue
		}
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}

		for _, pattern := range gcFallbackPatterns {
			matches, err := filepath.Glob(filepath.Join(dir, pattern))
			if err != nil || len(matches) == 0 {
				continue
			}

			// Filter by modification time and GC content
			for _, match := range matches {
				fi, err := os.Stat(match)
				if err != nil || fi.IsDir() {
					continue
				}
				if fi.ModTime().Before(cutoff) {
					continue
				}
				if !looksLikeGCLog(match) {
					continue
				}

				// Found a valid GC log file
				simplelog.Infof("GC log fallback: found GC log at %v using pattern %v", match, pattern)
				return pattern, dir
			}
		}
	}

	simplelog.Warningf("GC log fallback: no GC logs found in any of the searched directories")
	return "", ""
}

// looksLikeGCLog reads the first few lines of a file and checks for GC log markers.
func looksLikeGCLog(filePath string) bool {
	f, err := os.Open(filepath.Clean(filePath))
	if err != nil {
		return false
	}
	defer f.Close() //nolint:errcheck // read-only file; close error is non-fatal

	scanner := bufio.NewScanner(f)
	linesToCheck := 10
	for i := 0; i < linesToCheck && scanner.Scan(); i++ {
		line := strings.ToLower(scanner.Text())
		for _, marker := range gcMarkers {
			if strings.Contains(line, strings.ToLower(marker)) {
				return true
			}
		}
	}
	return false
}
