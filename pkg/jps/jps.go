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

// jps package provides logic for extracting values from jps
package jps

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/ddcio"
)

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
