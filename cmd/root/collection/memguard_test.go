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
	"runtime"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/cli"
	utilexec "k8s.io/client-go/util/exec"
)

const (
	guardRefuseLine = "error: memory guard: refusing to start: source=cgroup2 used_mib=11 limit_mib=1024 reserve_mib=1024"
	guardAbortLine  = "error: memory guard: aborting: source=cgroup2 used_mib=11 limit_mib=1024 reserve_mib=1024"
	refuseDetail    = "refusing to start: source=cgroup2 used_mib=11 limit_mib=1024 reserve_mib=1024"
	abortDetail     = "aborting: source=cgroup2 used_mib=11 limit_mib=1024 reserve_mib=1024"
)

func realExitError(t *testing.T) *exec.ExitError {
	t.Helper()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd.exe", "/c", "exit 3")
	} else {
		cmd = exec.Command("sh", "-c", "exit 3")
	}
	var ee *exec.ExitError
	if err := cmd.Run(); !errors.As(err, &ee) {
		t.Fatalf("expected *exec.ExitError, got %v", err)
	}
	return ee
}

func TestRemoteExitError_Message(t *testing.T) {
	if got := (&RemoteExitError{Code: 3}).Error(); got != "remote command exited with code 3" {
		t.Errorf("got %q", got)
	}
}

func TestRemoteExitCode(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		code   int
		wantOK bool
	}{
		{"remote", &RemoteExitError{Code: 3}, 3, true},
		{"wrapped remote", fmt.Errorf("w: %w", &RemoteExitError{Code: 137}), 137, true},
		{"client-go", fmt.Errorf("w: %w", utilexec.CodeExitError{Err: errors.New("command terminated with exit code 3"), Code: 3}), 3, true},
		{"exec exit error", cli.UnableToStartErr{Err: realExitError(t)}, 3, true},
		{"other", errors.New("x"), 0, false},
		{"nil", nil, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, ok := remoteExitCode(tt.err)
			if code != tt.code || ok != tt.wantOK {
				t.Errorf("got (%d, %v), want (%d, %v)", code, ok, tt.code, tt.wantOK)
			}
		})
	}
}

func TestMemoryGuardDetail(t *testing.T) {
	if d, ok := memoryGuardDetail(guardRefuseLine); !ok || d != refuseDetail {
		t.Errorf("got (%q, %v)", d, ok)
	}
	for _, l := range []string{`{"a":1}`, "warning: slow"} {
		if d, ok := memoryGuardDetail(l); ok || d != "" {
			t.Errorf("%q: got (%q, %v)", l, d, ok)
		}
	}
}

func TestMemoryGuardDetailIn(t *testing.T) {
	if got := memoryGuardDetailIn("x\n" + guardRefuseLine + "\n"); got != refuseDetail {
		t.Errorf("got %q", got)
	}
	if got := memoryGuardDetailIn("x\ny"); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestAsMemoryGuardError(t *testing.T) {
	const item = "sys.jobs_recent"
	tests := []struct {
		name     string
		err      error
		captured string
		wantOK   bool
		detail   string
	}{
		{"captured", &RemoteExitError{Code: 3}, refuseDetail, true, refuseDetail},
		{"from stderr", &RemoteExitError{Code: 3, Stderr: guardAbortLine}, "", true, abortDetail},
		{"no detail", &RemoteExitError{Code: 3}, "", true, ""},
		{"other code", &RemoteExitError{Code: 137}, "", false, ""},
		{"nil", nil, "", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, ok := asMemoryGuardError(item, tt.err, tt.captured)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				if g != nil {
					t.Errorf("want nil error, got %v", g)
				}
				return
			}
			if g.Detail != tt.detail {
				t.Errorf("Detail = %q, want %q", g.Detail, tt.detail)
			}
			if tt.name == "no detail" {
				if got := g.Summary(); got != "sys.jobs_recent (memory guard, exit 3)" {
					t.Errorf("Summary = %q", got)
				}
				if got := g.Error(); got != "sys.jobs_recent skipped by memory guard (exit 3)" {
					t.Errorf("Error = %q", got)
				}
			}
		})
	}
}

func TestMemoryGuardError_Text(t *testing.T) {
	g := &MemoryGuardError{Item: "sys.jobs_recent", Detail: refuseDetail}
	if got := g.Error(); got != "sys.jobs_recent skipped by memory guard: "+refuseDetail {
		t.Errorf("Error = %q", got)
	}
	if got := g.Summary(); got != "sys.jobs_recent ("+refuseDetail+")" {
		t.Errorf("Summary = %q", got)
	}
}
