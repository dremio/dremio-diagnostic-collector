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
	"os"
	"os/exec"
	"path/filepath"

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/local/ddcio"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/shutdown"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/simplelog"
)

// AsyncProfilerCommand builds the asprof command string for the given parameters.
// Exported for testing.
func AsyncProfilerCommand(pid int, durationSeconds int, outputFile string) string {
	return fmt.Sprintf("asprof -d %d -e cpu -f %v %v", durationSeconds, outputFile, pid)
}

// resolveAsprof locates the asprof binary, trying the embedded binary first and
// falling back to PATH lookup. It returns the full path to the binary or an
// error if neither source is available.
func resolveAsprof() (asprofPath string, tmpDir string, err error) {
	// Try extracting the embedded binary to a temp directory first.
	tmpDir, err = os.MkdirTemp("", "ddc-asprof-*")
	if err != nil {
		simplelog.Warningf("failed to create temp dir for embedded asprof: %v, trying PATH", err)
	} else {
		asprofPath, err = ExtractAsprof(tmpDir)
		if err == nil {
			simplelog.Debugf("using embedded asprof binary: %s", asprofPath)
			return asprofPath, tmpDir, nil
		}
		simplelog.Debugf("embedded asprof not available: %v, falling back to PATH lookup", err)
		// Clean up the empty temp dir since we won't use it.
		_ = os.RemoveAll(tmpDir)
	}

	// Fall back to PATH lookup (original behavior).
	asprofPath, err = exec.LookPath("asprof")
	if err != nil {
		return "", "", fmt.Errorf("asprof not available (embedded or PATH): %w", err)
	}
	simplelog.Debugf("using asprof from PATH: %s", asprofPath)
	return asprofPath, "", nil
}

// RunAsyncProfiler runs async-profiler against the Dremio JVM to capture
// a CPU profile in JFR format. It first tries to use the embedded asprof binary
// and falls back to PATH lookup. If neither is available, a warning is logged
// and nil is returned.
func RunAsyncProfiler(c *conf.CollectConf, hook shutdown.CancelHook) error {
	asprofPath, tmpDir, err := resolveAsprof()
	if err != nil {
		simplelog.Warningf("skipping async-profiler collection: %v", err)
		return fmt.Errorf("skipping async-profiler collection: %w", err)
	}
	// Clean up extracted binary when done.
	if tmpDir != "" {
		defer func() {
			if removeErr := os.RemoveAll(tmpDir); removeErr != nil {
				simplelog.Warningf("failed to clean up temp asprof dir %s: %v", tmpDir, removeErr)
			}
		}()
	}

	pid := c.DremioPID()
	duration := c.DiagTimeSeconds()
	outputFile := filepath.Join(c.JFROutDir(), c.NodeName()+"-async-profile.jfr")

	command := fmt.Sprintf("%s -d %d -e cpu -f %v %v", asprofPath, duration, outputFile, pid)
	simplelog.Debugf("node: %v - starting async-profiler collection: %v", c.NodeName(), command)

	var w bytes.Buffer
	if err := ddcio.Shell(hook, &w, command); err != nil {
		return fmt.Errorf("unable to run async-profiler: %w", err)
	}
	simplelog.Debugf("node: %v - async-profiler output: %v", c.NodeName(), w.String())

	return nil
}
