// Copyright 2023 Dremio Corporation
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
	"fmt"
	"strings"
	"testing"
)

// mockExecutor returns a function that maps "host arg1 arg2 ..." keys to
// predefined (output, error) pairs. Commands not in the map return an error.
func mockExecutor(responses map[string]struct {
	out string
	err error
}) HostExecutor {
	return func(host string, args ...string) (string, error) {
		key := host + " " + strings.Join(args, " ")
		for pattern, resp := range responses {
			if strings.Contains(key, pattern) {
				return resp.out, resp.err
			}
		}
		return "", fmt.Errorf("mock: unmatched command %q", key)
	}
}

func TestDiscoverFiles_FullInfo(t *testing.T) {
	responses := map[string]struct {
		out string
		err error
	}{
		// probeDir for log dir
		"test -d /var/log/dremio": {out: "exists", err: nil},
		// probeDir non-empty check for log dir
		"find -L /var/log/dremio -maxdepth 1 -type f -print -quit": {out: "/var/log/dremio/server.log\n", err: nil},
		// listFiles stat for log dir (includes a gc log file)
		"find -L /var/log/dremio -maxdepth 2 -type f -exec stat": {
			out: "1711929600 1234 /var/log/dremio/server.log\n1711929600 5678 /var/log/dremio/server.out\n1711929600 9012 /var/log/dremio/gc.log.0\n",
			err: nil,
		},
		// probeDir for conf dir
		"test -d /opt/dremio/conf": {out: "exists", err: nil},
		// probeDir non-empty check for conf dir
		"find -L /opt/dremio/conf -maxdepth 1 -type f -print -quit": {out: "/opt/dremio/conf/dremio.conf\n", err: nil},
		// listFiles stat for conf dir
		"find -L /opt/dremio/conf -maxdepth 1 -type f -exec stat": {
			out: "1711929600 512 /opt/dremio/conf/dremio.conf\n1711929600 256 /opt/dremio/conf/dremio-env\n",
			err: nil,
		},
		// PID cascade
		"jcmd -l":               {out: "", err: fmt.Errorf("not found")},
		"pgrep -x java":         {out: "", err: fmt.Errorf("exit status 1")},
		"pgrep -f dremio.*java": {out: "42\n", err: nil},
	}

	info, err := RunDiscovery(mockExecutor(responses), "node1", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.LogDir != "/var/log/dremio" {
		t.Errorf("LogDir = %q, want /var/log/dremio", info.LogDir)
	}
	if info.ConfDir != "/opt/dremio/conf" {
		t.Errorf("ConfDir = %q, want /opt/dremio/conf", info.ConfDir)
	}
	if info.DremioPID != 42 {
		t.Errorf("DremioPID = %d, want 42", info.DremioPID)
	}
	// Should have 5 files: 2 log + 2 config + 1 gc-log
	if len(info.Files) != 5 {
		t.Errorf("len(Files) = %d, want 5; files: %+v", len(info.Files), info.Files)
	}

	// Check file types
	var logCount, configCount, gcLogCount int
	for _, f := range info.Files {
		switch f.FileType {
		case "log":
			logCount++
		case "config":
			configCount++
		case "gc-log":
			gcLogCount++
		}
	}
	if logCount != 2 {
		t.Errorf("log file count = %d, want 2", logCount)
	}
	if configCount != 2 {
		t.Errorf("config file count = %d, want 2", configCount)
	}
	if gcLogCount != 1 {
		t.Errorf("gc-log file count = %d, want 1", gcLogCount)
	}
}

func TestDiscoverFiles_PartialFailure(t *testing.T) {
	// Log dir found, config dir not found, PID found.
	responses := map[string]struct {
		out string
		err error
	}{
		"test -d /var/log/dremio":                                  {out: "exists", err: nil},
		"find -L /var/log/dremio -maxdepth 1 -type f -print -quit": {out: "/var/log/dremio/server.log\n", err: nil},
		"find -L /var/log/dremio -maxdepth 2 -type f -exec stat": {
			out: "1711929600 1000 /var/log/dremio/server.log\n",
			err: nil,
		},
		// All conf dir candidates fail
		"test -d /opt/dremio/conf": {out: "", err: fmt.Errorf("not found")},
		"test -d /etc/dremio":      {out: "", err: fmt.Errorf("not found")},
		// PID cascade
		"jcmd -l":               {out: "", err: fmt.Errorf("not found")},
		"pgrep -x java":         {out: "", err: fmt.Errorf("exit status 1")},
		"pgrep -f dremio.*java": {out: "100\n", err: nil},
	}

	info, err := RunDiscovery(mockExecutor(responses), "node1", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.LogDir != "/var/log/dremio" {
		t.Errorf("LogDir = %q, want /var/log/dremio", info.LogDir)
	}
	if info.ConfDir != "" {
		t.Errorf("ConfDir = %q, want empty", info.ConfDir)
	}
	if info.DremioPID != 100 {
		t.Errorf("DremioPID = %d, want 100", info.DremioPID)
	}
	if len(info.Files) != 1 {
		t.Errorf("len(Files) = %d, want 1", len(info.Files))
	}
}

func TestDiscoverFiles_NoDremioProcess(t *testing.T) {
	responses := map[string]struct {
		out string
		err error
	}{
		"test -d /var/log/dremio":                                  {out: "exists", err: nil},
		"find -L /var/log/dremio -maxdepth 1 -type f -print -quit": {out: "/var/log/dremio/server.log\n", err: nil},
		"find -L /var/log/dremio -maxdepth 2 -type f -exec stat": {
			out: "1711929600 100 /var/log/dremio/server.log\n",
			err: nil,
		},
		"test -d /opt/dremio/conf": {out: "", err: fmt.Errorf("not found")},
		"test -d /etc/dremio":      {out: "", err: fmt.Errorf("not found")},
		// PID cascade — all fail
		"jcmd -l":               {out: "", err: fmt.Errorf("not found")},
		"pgrep -x java":         {out: "", err: fmt.Errorf("exit status 1")},
		"pgrep -f dremio.*java": {out: "", err: fmt.Errorf("exit status 1")},
	}

	info, err := RunDiscovery(mockExecutor(responses), "node1", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.DremioPID != 0 {
		t.Errorf("DremioPID = %d, want 0", info.DremioPID)
	}
}

func TestDiscoverFiles_EmptyDirectory(t *testing.T) {
	responses := map[string]struct {
		out string
		err error
	}{
		// /var/log/dremio exists but is empty — probeDir skips it
		"test -d /var/log/dremio":                                  {out: "exists", err: nil},
		"find -L /var/log/dremio -maxdepth 1 -type f -print -quit": {out: "", err: nil},
		// next candidate also doesn't exist
		"test -d /opt/dremio/log":      {out: "", err: fmt.Errorf("not found")},
		"test -d /opt/dremio/data/log": {out: "", err: fmt.Errorf("not found")},
		// conf candidates fail
		"test -d /opt/dremio/conf": {out: "", err: fmt.Errorf("not found")},
		"test -d /etc/dremio":      {out: "", err: fmt.Errorf("not found")},
		"jcmd -l":                  {out: "", err: fmt.Errorf("not found")},
		"pgrep -x java":            {out: "", err: fmt.Errorf("exit status 1")},
		"pgrep -f dremio.*java":    {out: "1\n", err: nil},
	}

	info, err := RunDiscovery(mockExecutor(responses), "node1", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Empty dir is skipped — LogDir should be empty since no other candidate exists
	if info.LogDir != "" {
		t.Errorf("LogDir = %q, want empty (empty dir should be skipped)", info.LogDir)
	}
	// No log files
	logFiles := 0
	for _, f := range info.Files {
		if f.FileType == "log" {
			logFiles++
		}
	}
	if logFiles != 0 {
		t.Errorf("expected 0 log files in empty dir, got %d", logFiles)
	}
}

func TestDiscoverFiles_EmptyHost(t *testing.T) {
	_, err := RunDiscovery(func(_ string, _ ...string) (string, error) {
		return "", nil
	}, "", "", "")
	if err == nil {
		t.Fatal("expected error for empty host")
	}
}

func TestDiscoverFiles_AllCommandsFail(t *testing.T) {
	// Every command fails → should return error.
	executor := func(_ string, _ ...string) (string, error) {
		return "", fmt.Errorf("connection refused")
	}
	info, err := RunDiscovery(executor, "badhost", "", "")
	if err == nil {
		t.Fatal("expected error when all commands fail")
	}
	// info should still be non-nil with zero-values
	if info == nil {
		t.Fatal("expected non-nil info even on total failure")
	}
	if info.DremioPID != 0 {
		t.Errorf("expected PID=0, got %d", info.DremioPID)
	}
}

func TestParseStatOutput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		fileType string
		want     int
		wantSize int64
	}{
		{
			name:     "normal output",
			input:    "1711929600 1234 /var/log/dremio/server.log\n1711929600 5678 /var/log/dremio/server.out\n",
			fileType: "log",
			want:     2,
			wantSize: 1234, // first file's size
		},
		{
			name:     "quoted output from shell",
			input:    "'1711929600 1234 /var/log/dremio/server.log'\n'1711929600 5678 /var/log/dremio/server.out'\n",
			fileType: "log",
			want:     2,
			wantSize: 1234,
		},
		{
			name:     "empty output",
			input:    "",
			fileType: "log",
			want:     0,
		},
		{
			name:     "malformed line",
			input:    "not-a-number /some/path\n",
			fileType: "log",
			want:     0,
		},
		{
			name:     "two field line (missing mod time)",
			input:    "1234 /some/path\n",
			fileType: "log",
			want:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := parseStatOutput(tt.input, tt.fileType)
			if len(files) != tt.want {
				t.Errorf("parseStatOutput() returned %d files, want %d", len(files), tt.want)
			}
			if tt.want > 0 && files[0].Size != tt.wantSize {
				t.Errorf("first file size = %d, want %d", files[0].Size, tt.wantSize)
			}
			for _, f := range files {
				if f.FileType != tt.fileType {
					t.Errorf("file type = %q, want %q", f.FileType, tt.fileType)
				}
			}
		})
	}
}

func TestParseLsOutput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		dir      string
		fileType string
		want     int
	}{
		{
			name: "normal ls -la output",
			input: `total 12345
-rw-r--r-- 1 dremio dremio 1234 Jul 10 14:30 server.log
-rw-r--r-- 1 dremio dremio 5678 Jul 10 14:30 server.out
drwxr-xr-x 2 dremio dremio 4096 Jul 10 14:30 archive`,
			dir:      "/var/log/dremio",
			fileType: "log",
			want:     2, // only regular files, not directories
		},
		{
			name:     "empty output",
			input:    "",
			dir:      "/var/log/dremio",
			fileType: "log",
			want:     0,
		},
		{
			name:     "only total line",
			input:    "total 0\n",
			dir:      "/var/log/dremio",
			fileType: "log",
			want:     0,
		},
		{
			name: "file with spaces",
			input: `total 100
-rw-r--r-- 1 dremio dremio 999 Jul 10 14:30 file with spaces.log`,
			dir:      "/var/log",
			fileType: "log",
			want:     1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := parseLsOutput(tt.input, tt.dir, tt.fileType)
			if len(files) != tt.want {
				t.Errorf("parseLsOutput() returned %d files, want %d", len(files), tt.want)
			}
			for _, f := range files {
				if f.FileType != tt.fileType {
					t.Errorf("file type = %q, want %q", f.FileType, tt.fileType)
				}
				if !strings.HasPrefix(f.Path, tt.dir+"/") {
					t.Errorf("file path %q doesn't start with dir %q", f.Path, tt.dir)
				}
			}
		})
	}
}

func TestParseLsOutput_FileWithSpaces(t *testing.T) {
	input := `total 100
-rw-r--r-- 1 dremio dremio 999 Jul 10 14:30 file with spaces.log`
	files := parseLsOutput(input, "/var/log", "log")
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].Path != "/var/log/file with spaces.log" {
		t.Errorf("path = %q, want /var/log/file with spaces.log", files[0].Path)
	}
	if files[0].Size != 999 {
		t.Errorf("size = %d, want 999", files[0].Size)
	}
}

// --- New tests for T01 fixes ---

func TestRunDiscovery_UserProvidedLogDir(t *testing.T) {
	// User provides logDir — probing should be skipped, files listed from given dir.
	responses := map[string]struct {
		out string
		err error
	}{
		// listFiles stat for user-provided log dir
		"find -L /custom/logs -maxdepth 2 -type f -exec stat": {
			out: "1711929600 2048 /custom/logs/dremio.log\n",
			err: nil,
		},
		// conf dir probing
		"test -d /opt/dremio/conf":                                  {out: "exists", err: nil},
		"find -L /opt/dremio/conf -maxdepth 1 -type f -print -quit": {out: "/opt/dremio/conf/dremio.conf\n", err: nil},
		"find -L /opt/dremio/conf -maxdepth 1 -type f -exec stat": {
			out: "1711929600 512 /opt/dremio/conf/dremio.conf\n",
			err: nil,
		},
		// PID cascade
		"jcmd -l":               {out: "", err: fmt.Errorf("not found")},
		"pgrep -x java":         {out: "", err: fmt.Errorf("exit status 1")},
		"pgrep -f dremio.*java": {out: "99\n", err: nil},
	}

	info, err := RunDiscovery(mockExecutor(responses), "node1", "/custom/logs", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.LogDir != "/custom/logs" {
		t.Errorf("LogDir = %q, want /custom/logs", info.LogDir)
	}
	if len(info.Files) < 1 {
		t.Fatalf("expected at least 1 file, got %d", len(info.Files))
	}
	if info.Files[0].Path != "/custom/logs/dremio.log" {
		t.Errorf("first file path = %q, want /custom/logs/dremio.log", info.Files[0].Path)
	}
}

func TestRunDiscovery_UserProvidedConfDir(t *testing.T) {
	// User provides confDir — probing should be skipped.
	responses := map[string]struct {
		out string
		err error
	}{
		// log dir probing
		"test -d /var/log/dremio":                                  {out: "exists", err: nil},
		"find -L /var/log/dremio -maxdepth 1 -type f -print -quit": {out: "/var/log/dremio/server.log\n", err: nil},
		"find -L /var/log/dremio -maxdepth 2 -type f -exec stat": {
			out: "1711929600 1000 /var/log/dremio/server.log\n",
			err: nil,
		},
		// listFiles stat for user-provided conf dir
		"find -L /custom/conf -maxdepth 1 -type f -exec stat": {
			out: "1711929600 256 /custom/conf/dremio.conf\n1711929600 128 /custom/conf/dremio-env\n",
			err: nil,
		},
		// PID cascade
		"jcmd -l":               {out: "", err: fmt.Errorf("not found")},
		"pgrep -x java":         {out: "", err: fmt.Errorf("exit status 1")},
		"pgrep -f dremio.*java": {out: "55\n", err: nil},
	}

	info, err := RunDiscovery(mockExecutor(responses), "node1", "", "/custom/conf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.ConfDir != "/custom/conf" {
		t.Errorf("ConfDir = %q, want /custom/conf", info.ConfDir)
	}
	// Should have config files from user-provided dir
	var configCount int
	for _, f := range info.Files {
		if f.FileType == "config" {
			configCount++
		}
	}
	if configCount != 2 {
		t.Errorf("config file count = %d, want 2", configCount)
	}
}

func TestProbeDir_SkipsEmptyDir(t *testing.T) {
	// First candidate exists but is empty, second has files.
	responses := map[string]struct {
		out string
		err error
	}{
		// First dir exists
		"test -d /opt/dremio/log": {out: "exists", err: nil},
		// But is empty
		"find -L /opt/dremio/log -maxdepth 1 -type f -print -quit": {out: "", err: nil},
		// Second dir exists and has files
		"test -d /opt/dremio/data/log":                                  {out: "exists", err: nil},
		"find -L /opt/dremio/data/log -maxdepth 1 -type f -print -quit": {out: "/opt/dremio/data/log/server.log\n", err: nil},
	}

	executor := mockExecutor(responses)
	candidates := []string{"/opt/dremio/log", "/opt/dremio/data/log"}
	result := probeDir(executor, "node1", candidates)
	if result != "/opt/dremio/data/log" {
		t.Errorf("probeDir = %q, want /opt/dremio/data/log (should skip empty dir)", result)
	}
}

func TestDiscoverPID_FiltersPID1(t *testing.T) {
	// All three cascade steps eventually reach pgrep -f which returns PID 1 and 42.
	responses := map[string]struct {
		out string
		err error
	}{
		"jcmd -l":               {out: "", err: fmt.Errorf("jcmd not found")},
		"pgrep -x java":         {out: "", err: fmt.Errorf("exit status 1")},
		"pgrep -f dremio.*java": {out: "1\n42\n", err: nil},
	}
	executor := mockExecutor(responses)
	pid, err := discoverPID(executor, "node1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pid != 42 {
		t.Errorf("discoverPID = %d, want 42 (PID 1 should be filtered)", pid)
	}
}

func TestDiscoverPID_FallbackToPID1(t *testing.T) {
	// pgrep -f returns only PID 1 — should fall back to it.
	responses := map[string]struct {
		out string
		err error
	}{
		"jcmd -l":               {out: "", err: fmt.Errorf("jcmd not found")},
		"pgrep -x java":         {out: "", err: fmt.Errorf("exit status 1")},
		"pgrep -f dremio.*java": {out: "1\n", err: nil},
	}
	executor := mockExecutor(responses)
	pid, err := discoverPID(executor, "node1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pid != 1 {
		t.Errorf("discoverPID = %d, want 1 (fallback to PID 1 when it's the only match)", pid)
	}
}

func TestDiscoverPID_JcmdReturnsDremioPID(t *testing.T) {
	// jcmd -l returns a dremio PID → returned directly, pgrep never called.
	responses := map[string]struct {
		out string
		err error
	}{
		"jcmd -l": {out: "99 com.dremio.dac.daemon.DremioDaemon\n12345 sun.tools.jcmd.JCmd\n", err: nil},
	}
	executor := mockExecutor(responses)
	pid, err := discoverPID(executor, "node1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pid != 99 {
		t.Errorf("discoverPID = %d, want 99 (jcmd should find dremio PID)", pid)
	}
}

func TestDiscoverPID_JcmdNoDremioFallsThroughToPgrepX(t *testing.T) {
	// jcmd -l returns no dremio match → falls through to pgrep -x java.
	responses := map[string]struct {
		out string
		err error
	}{
		"jcmd -l":       {out: "999 some.other.JavaApp\n", err: nil},
		"pgrep -x java": {out: "42\n", err: nil},
	}
	executor := mockExecutor(responses)
	pid, err := discoverPID(executor, "node1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pid != 42 {
		t.Errorf("discoverPID = %d, want 42 (pgrep -x java fallback)", pid)
	}
}

func TestDiscoverPID_PgrepXReturnsPID(t *testing.T) {
	// jcmd fails, pgrep -x java returns a PID.
	responses := map[string]struct {
		out string
		err error
	}{
		"jcmd -l":       {out: "", err: fmt.Errorf("jcmd not found")},
		"pgrep -x java": {out: "55\n", err: nil},
	}
	executor := mockExecutor(responses)
	pid, err := discoverPID(executor, "node1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pid != 55 {
		t.Errorf("discoverPID = %d, want 55", pid)
	}
}

func TestDiscoverPID_AllThreeFail(t *testing.T) {
	// All three cascade steps fail → returns 0.
	responses := map[string]struct {
		out string
		err error
	}{
		"jcmd -l":               {out: "", err: fmt.Errorf("jcmd not found")},
		"pgrep -x java":         {out: "", err: fmt.Errorf("exit status 1")},
		"pgrep -f dremio.*java": {out: "", err: fmt.Errorf("exit status 1")},
	}
	executor := mockExecutor(responses)
	pid, err := discoverPID(executor, "node1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pid != 0 {
		t.Errorf("discoverPID = %d, want 0 (all steps failed)", pid)
	}
}

func TestDiscoverPID_JcmdPID1AndPID42(t *testing.T) {
	// jcmd -l returns PID 1 + PID 42 for dremio → selectPID returns 42.
	responses := map[string]struct {
		out string
		err error
	}{
		"jcmd -l": {out: "1 com.dremio.dac.daemon.DremioDaemon\n42 com.dremio.dac.daemon.DremioDaemon\n", err: nil},
	}
	executor := mockExecutor(responses)
	pid, err := discoverPID(executor, "node1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pid != 42 {
		t.Errorf("discoverPID = %d, want 42 (selectPID should prefer non-1)", pid)
	}
}

func TestDiscoverPID_JcmdFiltersOwnProcess(t *testing.T) {
	// jcmd -l output includes jcmd's own line → filtered out.
	responses := map[string]struct {
		out string
		err error
	}{
		"jcmd -l": {out: "12345 jdk.jcmd/sun.tools.jcmd.JCmd\n", err: nil},
		// No dremio match from jcmd, falls through
		"pgrep -x java":         {out: "", err: fmt.Errorf("exit status 1")},
		"pgrep -f dremio.*java": {out: "77\n", err: nil},
	}
	executor := mockExecutor(responses)
	pid, err := discoverPID(executor, "node1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pid != 77 {
		t.Errorf("discoverPID = %d, want 77 (jcmd should filter its own PID)", pid)
	}
}

func TestSelectPID(t *testing.T) {
	tests := []struct {
		name string
		pids []int
		want int
	}{
		{"empty", nil, 0},
		{"single non-1", []int{42}, 42},
		{"single PID 1", []int{1}, 1},
		{"PID 1 and others", []int{1, 42, 99}, 42},
		{"no PID 1", []int{50, 60}, 50},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectPID(tt.pids)
			if got != tt.want {
				t.Errorf("selectPID(%v) = %d, want %d", tt.pids, got, tt.want)
			}
		})
	}
}

// TestParseStatOutput_MultiLineFromHostExecute simulates output as assembled
// by HostExecute's writer callback (each scanner line gets \n appended).
func TestParseStatOutput_MultiLineFromHostExecute(t *testing.T) {
	// Simulate 3 files coming through HostExecute — each line terminated with \n.
	input := "1711929600 1024 /var/log/dremio/server.log\n" +
		"1711929600 2048 /var/log/dremio/server.out\n" +
		"1711929600 4096 /var/log/dremio/queries.json\n"
	files := parseStatOutput(input, "log")
	if len(files) != 3 {
		t.Fatalf("parseStatOutput() returned %d files, want 3", len(files))
	}
	if files[0].Size != 1024 || files[0].Path != "/var/log/dremio/server.log" {
		t.Errorf("file[0] = {%d, %q}, want {1024, /var/log/dremio/server.log}", files[0].Size, files[0].Path)
	}
	if files[2].Size != 4096 || files[2].Path != "/var/log/dremio/queries.json" {
		t.Errorf("file[2] = {%d, %q}, want {4096, /var/log/dremio/queries.json}", files[2].Size, files[2].Path)
	}
}

// TestParseLsOutput_MultiLineFromHostExecute simulates a full ls -la output
// with multiple files as it arrives from HostExecute.
func TestParseLsOutput_MultiLineFromHostExecute(t *testing.T) {
	input := "total 8192\n" +
		"-rw-r--r-- 1 dremio dremio 1024 Jul 10 14:30 server.log\n" +
		"-rw-r--r-- 1 dremio dremio 2048 Jul 10 14:30 server.out\n" +
		"drwxr-xr-x 2 dremio dremio 4096 Jul 10 14:30 archive\n" +
		"-rw-r--r-- 1 dremio dremio 512 Jul 10 14:30 queries.json\n"
	files := parseLsOutput(input, "/var/log/dremio", "log")
	if len(files) != 3 {
		t.Fatalf("parseLsOutput() returned %d files, want 3 (dirs excluded)", len(files))
	}
	if files[0].Path != "/var/log/dremio/server.log" || files[0].Size != 1024 {
		t.Errorf("file[0] = {%d, %q}, want {1024, /var/log/dremio/server.log}", files[0].Size, files[0].Path)
	}
	if files[2].Path != "/var/log/dremio/queries.json" || files[2].Size != 512 {
		t.Errorf("file[2] = {%d, %q}, want {512, /var/log/dremio/queries.json}", files[2].Size, files[2].Path)
	}
}

// --- Symlink and config allowlist tests (T01) ---

func TestParseLsOutput_SymlinkStripsTarget(t *testing.T) {
	input := `total 100
lrwxrwxrwx 1 root root 24 Jul 10 14:30 dremio.conf -> ..data/dremio.conf
lrwxrwxrwx 1 root root 20 Jul 10 14:30 dremio-env -> ..data/dremio-env
-rw-r--r-- 1 dremio dremio 512 Jul 10 14:30 regular-file.conf`
	files := parseLsOutput(input, "/opt/dremio/conf", "config")
	if len(files) != 3 {
		t.Fatalf("parseLsOutput() returned %d files, want 3; got %+v", len(files), files)
	}
	// Symlink names should have " -> target" stripped
	if files[0].Path != "/opt/dremio/conf/dremio.conf" {
		t.Errorf("file[0].Path = %q, want /opt/dremio/conf/dremio.conf", files[0].Path)
	}
	if files[1].Path != "/opt/dremio/conf/dremio-env" {
		t.Errorf("file[1].Path = %q, want /opt/dremio/conf/dremio-env", files[1].Path)
	}
	if files[2].Path != "/opt/dremio/conf/regular-file.conf" {
		t.Errorf("file[2].Path = %q, want /opt/dremio/conf/regular-file.conf", files[2].Path)
	}
}

func TestFilterFiles_ConfigAllowlist(t *testing.T) {
	input := []RemoteFileInfo{
		{Path: "/conf/dremio.conf", Size: 100, FileType: "config"},
		{Path: "/conf/dremio-env", Size: 200, FileType: "config"},
		{Path: "/conf/logback.xml", Size: 300, FileType: "config"},
		{Path: "/conf/logback-access.xml", Size: 300, FileType: "config"},
		{Path: "/conf/sso.json", Size: 400, FileType: "config"},
		{Path: "/conf/core-site.xml", Size: 500, FileType: "config"},
		{Path: "/conf/hive-site.xml", Size: 600, FileType: "config"},
		{Path: "/conf/truststore.jks", Size: 700, FileType: "config"},
		{Path: "/conf/dremio.keytab", Size: 800, FileType: "config"},
		{Path: "/conf/random-unknown-file.txt", Size: 900, FileType: "config"},
		{Path: "/conf/.hidden-file", Size: 50, FileType: "config"},
	}
	result := filterFiles(input, "config")
	// random-unknown-file.txt and .hidden-file should be excluded
	expected := map[string]bool{
		"/conf/dremio.conf":        true,
		"/conf/dremio-env":         true,
		"/conf/logback.xml":        true,
		"/conf/logback-access.xml": true,
		"/conf/sso.json":           true,
		"/conf/core-site.xml":      true,
		"/conf/hive-site.xml":      true,
		"/conf/truststore.jks":     true,
		"/conf/dremio.keytab":      true,
	}
	if len(result) != len(expected) {
		t.Fatalf("filterFiles returned %d files, want %d; got paths: %v", len(result), len(expected), filePaths(result))
	}
	for _, f := range result {
		if !expected[f.Path] {
			t.Errorf("unexpected file in result: %q", f.Path)
		}
	}
}

func TestFilterFiles_LogTypeNotFiltered(t *testing.T) {
	// Log files should not be filtered by config allowlist.
	input := []RemoteFileInfo{
		{Path: "/logs/server.log", Size: 100, FileType: "log"},
		{Path: "/logs/random-name.log", Size: 200, FileType: "log"},
	}
	result := filterFiles(input, "log")
	if len(result) != 2 {
		t.Errorf("filterFiles for log type returned %d files, want 2 (no filtering)", len(result))
	}
}

func TestFilterFiles_ExcludesDoubleDotPaths(t *testing.T) {
	input := []RemoteFileInfo{
		{Path: "/conf/dremio.conf", Size: 100, FileType: "config"},
		{Path: "/conf/..data/dremio.conf", Size: 100, FileType: "config"},
		{Path: "/conf/..2024_01_01/dremio.conf", Size: 100, FileType: "config"},
	}
	result := filterFiles(input, "config")
	if len(result) != 1 {
		t.Fatalf("filterFiles returned %d files, want 1; got paths: %v", len(result), filePaths(result))
	}
	if result[0].Path != "/conf/dremio.conf" {
		t.Errorf("expected /conf/dremio.conf, got %q", result[0].Path)
	}
}

func TestFilterFiles_ExcludesDoubleDotFromLogType(t *testing.T) {
	// ..data paths excluded even for non-config file types.
	input := []RemoteFileInfo{
		{Path: "/logs/server.log", Size: 100, FileType: "log"},
		{Path: "/logs/..data/server.log", Size: 100, FileType: "log"},
	}
	result := filterFiles(input, "log")
	if len(result) != 1 {
		t.Fatalf("filterFiles returned %d files, want 1; got paths: %v", len(result), filePaths(result))
	}
}

func TestHasDoubleDotComponent(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/opt/dremio/conf/dremio.conf", false},
		{"/opt/dremio/conf/..data/dremio.conf", true},
		{"/opt/dremio/conf/..2024_01_01/dremio.conf", true},
		{"..data/file", true},
		{"/normal/path/file.txt", false},
		{"/path/with.dots/file.txt", false},
		{"/path/.hidden/file.txt", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := hasDoubleDotComponent(tt.path)
			if got != tt.want {
				t.Errorf("hasDoubleDotComponent(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestMatchesConfigAllowlist(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"dremio.conf", true},
		{"dremio-env", true},
		{"logback.xml", true},
		{"logback-access.xml", true},
		{"sso.json", true},
		{"core-site.xml", true},
		{"hive-site.xml", true},
		{"truststore.jks", true},
		{"dremio.keytab", true},
		{"random.txt", false},
		{".hidden", false},
		{"README.md", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesConfigAllowlist(tt.name)
			if got != tt.want {
				t.Errorf("matchesConfigAllowlist(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestProbeDir_UsesFollowSymlinks(t *testing.T) {
	// Verify probeDir sends "find -L" to the executor.
	var capturedArgs []string
	executor := func(host string, args ...string) (string, error) {
		joined := strings.Join(args, " ")
		capturedArgs = append(capturedArgs, joined)
		if strings.Contains(joined, "test -d") {
			return "exists", nil
		}
		if strings.Contains(joined, "find -L") {
			return "/some/dir/file.txt\n", nil
		}
		return "", fmt.Errorf("unexpected: %s", joined)
	}
	result := probeDir(executor, "node1", []string{"/some/dir"})
	if result != "/some/dir" {
		t.Fatalf("probeDir returned %q, want /some/dir", result)
	}
	// Check that find -L was used
	foundFindL := false
	for _, args := range capturedArgs {
		if strings.Contains(args, "find -L /some/dir") {
			foundFindL = true
			break
		}
	}
	if !foundFindL {
		t.Errorf("probeDir did not use 'find -L'; captured commands: %v", capturedArgs)
	}
}

func TestListFiles_ConfigAllowlistApplied(t *testing.T) {
	// listFiles with fileType "config" should filter to allowlisted files.
	executor := func(host string, args ...string) (string, error) {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "find -L") && strings.Contains(joined, "stat") {
			return "1711929600 100 /conf/dremio.conf\n1711929600 200 /conf/random.txt\n1711929600 300 /conf/logback.xml\n1711929600 400 /conf/..data/dremio.conf\n", nil
		}
		return "", fmt.Errorf("unexpected: %s", joined)
	}
	files, err := listFiles(executor, "node1", "/conf", "config", "1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// random.txt should be excluded (not in allowlist), ..data path should be excluded
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(files), filePaths(files))
	}
	paths := filePaths(files)
	if paths[0] != "/conf/dremio.conf" {
		t.Errorf("file[0] = %q, want /conf/dremio.conf", paths[0])
	}
	if paths[1] != "/conf/logback.xml" {
		t.Errorf("file[1] = %q, want /conf/logback.xml", paths[1])
	}
}

func TestRunDiscovery_GCLogClassification(t *testing.T) {
	// Multiple GC filename patterns in log dir — verify each is classified correctly.
	responses := map[string]struct {
		out string
		err error
	}{
		// listFiles stat for user-provided log dir with mixed files
		"find -L /var/log/dremio -maxdepth 2 -type f -exec stat": {
			out: "1711929600 1000 /var/log/dremio/gc.log\n" +
				"1711929600 2000 /var/log/dremio/gc.log.0\n" +
				"1711929600 3000 /var/log/dremio/gc-2022-10-21_02-01-40.log\n" +
				"1711929600 4000 /var/log/dremio/gc.log.0.current\n" +
				"1711929600 5000 /var/log/dremio/server.log\n" +
				"1711929600 6000 /var/log/dremio/server.out\n" +
				"1711929600 7000 /var/log/dremio/access.log\n",
			err: nil,
		},
		// conf dir not found
		"test -d /opt/dremio/conf": {out: "", err: fmt.Errorf("not found")},
		"test -d /etc/dremio":      {out: "", err: fmt.Errorf("not found")},
		// PID cascade
		"jcmd -l":               {out: "", err: fmt.Errorf("not found")},
		"pgrep -x java":         {out: "", err: fmt.Errorf("exit status 1")},
		"pgrep -f dremio.*java": {out: "99\n", err: nil},
	}

	info, err := RunDiscovery(mockExecutor(responses), "node1", "/var/log/dremio", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Build a map of path → fileType for easy assertion.
	typeByPath := make(map[string]string)
	for _, f := range info.Files {
		typeByPath[f.Path] = f.FileType
	}

	// GC log files should be classified as "gc-log".
	gcLogFiles := []string{
		"/var/log/dremio/gc.log",
		"/var/log/dremio/gc.log.0",
		"/var/log/dremio/gc-2022-10-21_02-01-40.log",
		"/var/log/dremio/gc.log.0.current",
	}
	for _, path := range gcLogFiles {
		if ft, ok := typeByPath[path]; !ok {
			t.Errorf("expected file %q in results, not found", path)
		} else if ft != "gc-log" {
			t.Errorf("file %q has FileType %q, want gc-log", path, ft)
		}
	}

	// Regular log files should remain "log".
	regularLogFiles := []string{
		"/var/log/dremio/server.log",
		"/var/log/dremio/server.out",
		"/var/log/dremio/access.log",
	}
	for _, path := range regularLogFiles {
		if ft, ok := typeByPath[path]; !ok {
			t.Errorf("expected file %q in results, not found", path)
		} else if ft != "log" {
			t.Errorf("file %q has FileType %q, want log", path, ft)
		}
	}

	// Total file count.
	if len(info.Files) != 7 {
		t.Errorf("len(Files) = %d, want 7; files: %+v", len(info.Files), info.Files)
	}
}

func TestRunDiscovery_ChecksumProbe(t *testing.T) {
	// Base responses shared by all subtests — log dir, conf dir probing, pgrep.
	baseResponses := map[string]struct {
		out string
		err error
	}{
		"test -d /var/log/dremio":                                  {out: "exists", err: nil},
		"find -L /var/log/dremio -maxdepth 1 -type f -print -quit": {out: "/var/log/dremio/server.log\n", err: nil},
		"find -L /var/log/dremio -maxdepth 2 -type f -exec stat": {
			out: "1711929600 1000 /var/log/dremio/server.log\n",
			err: nil,
		},
		"test -d /opt/dremio/conf": {out: "", err: fmt.Errorf("not found")},
		"test -d /etc/dremio":      {out: "", err: fmt.Errorf("not found")},
		"jcmd -l":                  {out: "", err: fmt.Errorf("not found")},
		"pgrep -x java":            {out: "", err: fmt.Errorf("exit status 1")},
		"pgrep -f dremio.*java":    {out: "42\n", err: nil},
	}

	tests := []struct {
		name  string
		extra map[string]struct {
			out string
			err error
		}
		want string
	}{
		{
			name: "sha256sum available",
			extra: map[string]struct {
				out string
				err error
			}{
				"command -v sha256sum": {out: "/usr/bin/sha256sum\n", err: nil},
			},
			want: "sha256sum",
		},
		{
			name: "md5sum fallback",
			extra: map[string]struct {
				out string
				err error
			}{
				"command -v sha256sum": {out: "", err: fmt.Errorf("not found")},
				"command -v md5sum":    {out: "/usr/bin/md5sum\n", err: nil},
			},
			want: "md5sum",
		},
		{
			name: "neither available",
			extra: map[string]struct {
				out string
				err error
			}{
				"command -v sha256sum": {out: "", err: fmt.Errorf("not found")},
				"command -v md5sum":    {out: "", err: fmt.Errorf("not found")},
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Merge base and extra responses.
			merged := make(map[string]struct {
				out string
				err error
			})
			for k, v := range baseResponses {
				merged[k] = v
			}
			for k, v := range tt.extra {
				merged[k] = v
			}

			info, err := RunDiscovery(mockExecutor(merged), "node1", "", "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if info.ChecksumTool != tt.want {
				t.Errorf("ChecksumTool = %q, want %q", info.ChecksumTool, tt.want)
			}
		})
	}
}

// filePaths is a test helper that extracts paths from a slice of RemoteFileInfo.
func filePaths(files []RemoteFileInfo) []string {
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.Path
	}
	return paths
}

func TestRunDiscovery_GzipProbe(t *testing.T) {
	// Base responses shared by all subtests — log dir, conf dir probing, pgrep, checksum.
	baseResponses := map[string]struct {
		out string
		err error
	}{
		"test -d /var/log/dremio":                                  {out: "exists", err: nil},
		"find -L /var/log/dremio -maxdepth 1 -type f -print -quit": {out: "/var/log/dremio/server.log\n", err: nil},
		"find -L /var/log/dremio -maxdepth 2 -type f -exec stat": {
			out: "1711929600 1000 /var/log/dremio/server.log\n",
			err: nil,
		},
		"test -d /opt/dremio/conf": {out: "", err: fmt.Errorf("not found")},
		"test -d /etc/dremio":      {out: "", err: fmt.Errorf("not found")},
		"jcmd -l":                  {out: "", err: fmt.Errorf("not found")},
		"pgrep -x java":            {out: "", err: fmt.Errorf("exit status 1")},
		"pgrep -f dremio.*java":    {out: "42\n", err: nil},
		"command -v sha256sum":     {out: "/usr/bin/sha256sum\n", err: nil},
	}

	tests := []struct {
		name  string
		extra map[string]struct {
			out string
			err error
		}
		want bool
	}{
		{
			name: "gzip available",
			extra: map[string]struct {
				out string
				err error
			}{
				"command -v gzip": {out: "/usr/bin/gzip\n", err: nil},
			},
			want: true,
		},
		{
			name: "gzip unavailable",
			extra: map[string]struct {
				out string
				err error
			}{
				"command -v gzip": {out: "", err: fmt.Errorf("not found")},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Merge base and extra responses.
			merged := make(map[string]struct {
				out string
				err error
			})
			for k, v := range baseResponses {
				merged[k] = v
			}
			for k, v := range tt.extra {
				merged[k] = v
			}

			info, err := RunDiscovery(mockExecutor(merged), "node1", "", "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if info.GzipAvailable != tt.want {
				t.Errorf("GzipAvailable = %v, want %v", info.GzipAvailable, tt.want)
			}
		})
	}
}
