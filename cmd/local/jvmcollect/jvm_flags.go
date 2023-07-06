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
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/ddcio"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
)

// RunCollectJVM collects JVM flags from a java process
func RunCollectJVMFlags(c *conf.CollectConf) error {
	txt, err := CaptureFlagsFromPID(c.DremioPID())
	if err != nil {
		return err
	}
	jvmSettingsFile := filepath.Join(c.NodeInfoOutDir(), "jvm_settings.txt")
	jvmSettingsFileWriter, err := os.Create(filepath.Clean(jvmSettingsFile))
	if err != nil {
		return fmt.Errorf("unable to create file %v due to error %w", filepath.Clean(jvmSettingsFile), err)
	}
	defer func() {
		if err := jvmSettingsFileWriter.Close(); err != nil {
			simplelog.Debugf("This is an automatic close on file %v and safe to ignore this error: %v", filepath.Clean(jvmSettingsFile), err)
		}
	}()
	if _, err := jvmSettingsFileWriter.WriteString(txt); err != nil {
		return fmt.Errorf("unable to write to file %v due to error: %w", filepath.Clean(jvmSettingsFile), err)
	}
	if err := jvmSettingsFileWriter.Sync(); err != nil {
		return fmt.Errorf("unable to sync the jvm_settings.txt file due to error: %w", err)
	}
	if err := jvmSettingsFileWriter.Close(); err != nil {
		return fmt.Errorf("unable to close the jvm_settings.txt file due to error: %w", err)
	}
	return nil
}

func CaptureFlagsFromPID(pid int) (string, error) {
	var buf bytes.Buffer
	if err := ddcio.Shell(&buf, "jps -v"); err != nil {
		return "", fmt.Errorf("failed getting flags: '%w', output was: '%v'", err, buf.String())
	}
	scanner := bufio.NewScanner(&buf)
	//adjust the max line size capacity as the jpv output can be large
	const maxCapacity = 512 * 1024
	lineBuffer := make([]byte, maxCapacity)
	scanner.Buffer(lineBuffer, maxCapacity)
	jvmFlagsForPid := ""
	for scanner.Scan() {
		line := scanner.Text()
		pidPrefix := fmt.Sprintf("%v ", pid)
		if strings.HasPrefix(line, pidPrefix) {
			//matched now let's eliminate the pid part
			flagText := strings.TrimPrefix(line, pidPrefix)
			jvmFlagsForPid = strings.TrimSpace(flagText)
		}
	}
	if strings.TrimSpace(jvmFlagsForPid) == "" {
		return "", fmt.Errorf("pid %v not found in jps output", pid)
	}
	return jvmFlagsForPid, nil
}
