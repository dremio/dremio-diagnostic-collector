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

package jvmcollect_test

import (
	"strings"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/local/jvmcollect"
)

func TestAsyncProfiler_CommandConstruction(t *testing.T) {
	cmd := jvmcollect.AsyncProfilerCommand(12345, 60, "/tmp/out/node1-async-profile.jfr")
	expected := "asprof -d 60 -e cpu -f /tmp/out/node1-async-profile.jfr 12345"
	if cmd != expected {
		t.Errorf("expected command %q but got %q", expected, cmd)
	}
}

func TestAsyncProfiler_UsesCPUMode(t *testing.T) {
	cmd := jvmcollect.AsyncProfilerCommand(1, 30, "/tmp/output.jfr")
	if !strings.Contains(cmd, "-e cpu") {
		t.Errorf("expected command to contain '-e cpu' but got %q", cmd)
	}
	if strings.Contains(cmd, "-e alloc") {
		t.Errorf("command must not use '-e alloc' (conflicts with JFR allocation sampling) but got %q", cmd)
	}
}

func TestAsyncProfiler_OutputFormatJFR(t *testing.T) {
	cmd := jvmcollect.AsyncProfilerCommand(999, 10, "/data/node1-async-profile.jfr")
	if !strings.HasSuffix(strings.Fields(cmd)[6], ".jfr") {
		t.Errorf("expected output file to end in .jfr but got command %q", cmd)
	}
}
