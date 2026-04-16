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

package autodetect_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/local/conf/autodetect"
)

func TestFindGCLogsFallback_FindsGCLogInDir(t *testing.T) {
	dir := t.TempDir()
	// Create a file that matches gc pattern and has GC content
	gcFile := filepath.Join(dir, "gc.log")
	if err := os.WriteFile(gcFile, []byte("[gc] GC(0) Pause Young 100M->50M\nHeap region size: 1M\n"), 0644); err != nil {
		t.Fatal(err)
	}

	pattern, foundDir := autodetect.FindGCLogsFallback("", []string{dir}, 7)
	if foundDir != dir {
		t.Errorf("expected dir %v, got %v", dir, foundDir)
	}
	if pattern == "" {
		t.Error("expected non-empty pattern")
	}
}

func TestFindGCLogsFallback_SkipsNonGCFiles(t *testing.T) {
	dir := t.TempDir()
	// Create a file that matches pattern but doesn't have GC content
	gcFile := filepath.Join(dir, "gc.log")
	if err := os.WriteFile(gcFile, []byte("this is not a gc log file\njust some random text\n"), 0644); err != nil {
		t.Fatal(err)
	}

	_, foundDir := autodetect.FindGCLogsFallback("", []string{dir}, 7)
	if foundDir != "" {
		t.Errorf("expected empty dir for non-GC file, got %v", foundDir)
	}
}

func TestFindGCLogsFallback_ReturnsEmptyWhenNoGCLogs(t *testing.T) {
	dir := t.TempDir()

	_, foundDir := autodetect.FindGCLogsFallback("", []string{dir}, 7)
	if foundDir != "" {
		t.Errorf("expected empty dir when no GC logs, got %v", foundDir)
	}
}

func TestFindGCLogsFallback_PrefersLogDir(t *testing.T) {
	logDir := t.TempDir()
	otherDir := t.TempDir()

	// Create GC log in both
	for _, d := range []string{logDir, otherDir} {
		gcFile := filepath.Join(d, "gc.log")
		if err := os.WriteFile(gcFile, []byte("[gc] GC(0) Pause Young\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	_, foundDir := autodetect.FindGCLogsFallback(logDir, []string{otherDir}, 7)
	if foundDir != logDir {
		t.Errorf("expected logDir %v to be preferred, got %v", logDir, foundDir)
	}
}

func TestFindGCLogsFallback_JDK8Format(t *testing.T) {
	dir := t.TempDir()
	gcFile := filepath.Join(dir, "server.gc.log")
	content := `2025-07-10T14:30:22.456+0000: 123.456: [GC (Allocation Failure) [PSYoungGen: 100M->50M(200M)]
2025-07-10T14:30:22.500+0000: 123.500: [Times: user=0.01 sys=0.00, real=0.04 secs]
`
	if err := os.WriteFile(gcFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	pattern, foundDir := autodetect.FindGCLogsFallback("", []string{dir}, 7)
	if foundDir == "" {
		t.Error("expected to find JDK 8 GC log")
	}
	if pattern == "" {
		t.Errorf("expected non-empty pattern, got empty")
	}
}

func TestFindGCLogsFallback_JDK11Format(t *testing.T) {
	dir := t.TempDir()
	gcFile := filepath.Join(dir, "gc-2025-07-10.log")
	content := `[0.123s][info][gc] Using G1
[0.124s][info][gc,init] CPUs: 4 total, 4 available
[0.125s][info][gc,init] Memory: 16384M
[1.234s][info][gc,start] GC(0) Pause Young (Normal)
`
	if err := os.WriteFile(gcFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, foundDir := autodetect.FindGCLogsFallback("", []string{dir}, 7)
	if foundDir == "" {
		t.Error("expected to find JDK 11 GC log")
	}
}
