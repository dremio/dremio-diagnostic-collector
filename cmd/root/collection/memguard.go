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

package collection

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// viewerExitMemoryGuard is the dremio-rocksdb-viewer exit code for a memory guard stop.
const viewerExitMemoryGuard = 3

// memoryGuardPrefix starts the single stderr line the viewer prints on a guard stop.
const memoryGuardPrefix = "error: memory guard: "

// remoteExitCode extracts a remote command's exit code from err. Order:
// *RemoteExitError, client-go's ExitStatus() (CodeExitError), *exec.ExitError.
func remoteExitCode(err error) (code int, ok bool) {
	if err == nil {
		return 0, false
	}
	var rexit *RemoteExitError
	if errors.As(err, &rexit) {
		return rexit.Code, true
	}
	var es interface{ ExitStatus() int }
	if errors.As(err, &es) {
		return es.ExitStatus(), true
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), true
	}
	return 0, false
}

// memoryGuardDetail returns the text after memoryGuardPrefix when line is a guard line.
func memoryGuardDetail(line string) (detail string, ok bool) {
	detail, ok = strings.CutPrefix(strings.TrimSpace(line), memoryGuardPrefix)
	if !ok {
		return "", false
	}
	return detail, true
}

// memoryGuardDetailIn returns the detail of the last guard line in a multi-line
// output, or "" if there is none.
func memoryGuardDetailIn(output string) string {
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if d, ok := memoryGuardDetail(lines[i]); ok {
			return d
		}
	}
	return ""
}

// MemoryGuardError means the viewer refused to run, or aborted, on its memory guard.
type MemoryGuardError struct {
	Item   string // viewer item, e.g. "sys.jobs_recent", "queries-perf"
	Detail string // guard text without the prefix; "" if not captured
}

func (e *MemoryGuardError) Error() string {
	if e.Detail == "" {
		return fmt.Sprintf("%s skipped by memory guard (exit %d)", e.Item, viewerExitMemoryGuard)
	}
	return fmt.Sprintf("%s skipped by memory guard: %s", e.Item, e.Detail)
}

// Summary is the short form used in reports.
func (e *MemoryGuardError) Summary() string {
	if e.Detail == "" {
		return fmt.Sprintf("%s (memory guard, exit %d)", e.Item, viewerExitMemoryGuard)
	}
	return fmt.Sprintf("%s (%s)", e.Item, e.Detail)
}

// asMemoryGuardError returns a guard error when err carries exit code 3. captured
// wins; otherwise the detail comes from a *RemoteExitError's Stderr; otherwise "".
func asMemoryGuardError(item string, err error, captured string) (*MemoryGuardError, bool) {
	code, ok := remoteExitCode(err)
	if !ok || code != viewerExitMemoryGuard {
		return nil, false
	}
	detail := captured
	if detail == "" {
		var rexit *RemoteExitError
		if errors.As(err, &rexit) {
			detail, _ = memoryGuardDetail(rexit.Stderr)
		}
	}
	return &MemoryGuardError{Item: item, Detail: detail}, true
}
