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

package configui

import (
	"strings"
	"testing"
)

// allTools is the default selected tools list used by CLI command tests.
var allTools = []string{"JFR", "jstack", "top", "async-profiler"}

func TestBuildStandardCLICommand_WithContext(t *testing.T) {
	cfg := &StandardConfig{
		Transport:         "k8s",
		Namespace:         "dremio-ns",
		K8sContext:        "prod-cluster",
		CoordinatorLogDir: "/var/log/dremio",
		ExecutorLogDir:    "/var/log/dremio",
		DremioConfDir:     "/opt/dremio/conf",
		DremioRocksDBDir:  "/opt/dremio/data/db",
	}
	cmd := buildStandardCLICommand(cfg, 0, 0, 0, 0, 0)
	if !strings.Contains(cmd, "collect") || !strings.Contains(cmd, "standard") {
		t.Errorf("expected 'collect' and 'standard' verb in CLI command, got:\n%s", cmd)
	}
	if strings.Contains(cmd, "--mode") {
		t.Errorf("should not contain --mode flag; mode is a subcommand verb, got:\n%s", cmd)
	}
	if !strings.Contains(cmd, "--context=prod-cluster") {
		t.Errorf("expected --context=prod-cluster in CLI command, got:\n%s", cmd)
	}
	if !strings.Contains(cmd, "--namespace=dremio-ns") {
		t.Errorf("expected --namespace=dremio-ns in CLI command, got:\n%s", cmd)
	}
	// PAT/endpoint should not appear in standard mode CLI command
	if strings.Contains(cmd, "--dremio-pat-token") {
		t.Errorf("should not contain --dremio-pat-token, got:\n%s", cmd)
	}
	if strings.Contains(cmd, "--dremio-endpoint") {
		t.Errorf("should not contain --dremio-endpoint, got:\n%s", cmd)
	}
	// queries-perf with 0 days should show false
	if !strings.Contains(cmd, "--collect-queries-perf-json=false") {
		t.Errorf("expected --collect-queries-perf-json=false when QueriesPerfDays=0, got:\n%s", cmd)
	}
}

func TestBuildStandardCLICommand_QueriesPerfEnabled(t *testing.T) {
	cfg := &StandardConfig{
		Transport:         "k8s",
		Namespace:         "dremio-ns",
		CoordinatorLogDir: "/var/log/dremio",
		ExecutorLogDir:    "/var/log/dremio",
		DremioConfDir:     "/opt/dremio/conf",
		DremioRocksDBDir:  "/opt/dremio/data/db",
		QueriesPerfDays:   14,
		CollectWLM:        true,
		SystemTables:      []string{"options", "roles"},
	}
	cmd := buildStandardCLICommand(cfg, 30, 14, 1, 1, 1)
	if !strings.Contains(cmd, "--collect-queries-perf-json=true") {
		t.Errorf("expected --collect-queries-perf-json=true, got:\n%s", cmd)
	}
	if !strings.Contains(cmd, "--queries-perf-num-days=14") {
		t.Errorf("expected --queries-perf-num-days=14, got:\n%s", cmd)
	}
	if !strings.Contains(cmd, "--queries-json-num-days=30") {
		t.Errorf("expected --queries-json-num-days=30, got:\n%s", cmd)
	}
	if !strings.Contains(cmd, "--collect-wlm=true") {
		t.Errorf("expected --collect-wlm=true, got:\n%s", cmd)
	}
	if !strings.Contains(cmd, "--system-tables=options,roles") {
		t.Errorf("expected --system-tables=options,roles, got:\n%s", cmd)
	}
	// No PAT-related flags
	if strings.Contains(cmd, "--dremio-pat-token") {
		t.Errorf("should not contain --dremio-pat-token, got:\n%s", cmd)
	}
	if strings.Contains(cmd, "--collect-kvstore-report") {
		t.Errorf("should not contain --collect-kvstore-report, got:\n%s", cmd)
	}
}

// TestBuildStandardCLICommand_NoSystemTables verifies that deselecting all
// system tables in the TUI produces a CLI command with --system-tables=
// (empty value). Without this, re-running the generated command would fall
// back to the package-level default (a non-empty list of tables) and collect
// system tables despite the user's deselection.
func TestBuildStandardCLICommand_NoSystemTables(t *testing.T) {
	cfg := &StandardConfig{
		Transport:         "k8s",
		Namespace:         "dremio-ns",
		CoordinatorLogDir: "/var/log/dremio",
		ExecutorLogDir:    "/var/log/dremio",
		DremioConfDir:     "/opt/dremio/conf",
		DremioRocksDBDir:  "/opt/dremio/data/db",
		CollectWLM:        true,
		SystemTables:      nil, // user deselected all tables
	}
	cmd := buildStandardCLICommand(cfg, 7, 7, 1, 1, 1)
	if !strings.Contains(cmd, "--system-tables=") {
		t.Errorf("expected --system-tables= flag to be emitted even when SystemTables is empty, got:\n%s", cmd)
	}
	// Reject any non-empty system-tables value (e.g. --system-tables=options,...).
	for _, line := range strings.Split(cmd, "\n") {
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "`"))
		trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, "\\"))
		if strings.HasPrefix(trimmed, "--system-tables=") && trimmed != "--system-tables=" {
			t.Errorf("expected --system-tables= with empty value, got %q in:\n%s", trimmed, cmd)
		}
	}
}

func TestBuildStandardCLICommand_WithoutContext(t *testing.T) {
	cfg := &StandardConfig{
		Transport:         "k8s",
		Namespace:         "dremio-ns",
		K8sContext:        "",
		CoordinatorLogDir: "/var/log/dremio",
		ExecutorLogDir:    "/var/log/dremio",
		DremioConfDir:     "/opt/dremio/conf",
		DremioRocksDBDir:  "/opt/dremio/data/db",
	}
	cmd := buildStandardCLICommand(cfg, 0, 0, 0, 0, 0)
	if strings.Contains(cmd, "--context") {
		t.Errorf("expected no --context in CLI command when K8sContext is empty, got:\n%s", cmd)
	}
}

func TestBuildDiagnosisCLICommand_WithContext(t *testing.T) {
	days := "3"
	dur := "60"
	cfg := &DiagnosisConfig{
		Transport:         "k8s",
		Namespace:         "dremio-ns",
		K8sContext:        "staging-ctx",
		CoordinatorLogDir: "/var/log/dremio",
		ExecutorLogDir:    "/var/log/dremio",
		DremioConfDir:     "/opt/dremio/conf",
		DremioRocksDBDir:  "/opt/dremio/data/db",
	}
	cmd := buildDiagnosisCLICommand(cfg, &allTools, nil, &days, &dur, new(string))
	if !strings.Contains(cmd, "collect") || !strings.Contains(cmd, "diagnosis") {
		t.Errorf("expected 'collect' and 'diagnosis' verb in CLI command, got:\n%s", cmd)
	}
	if strings.Contains(cmd, "--mode") {
		t.Errorf("should not contain --mode flag; mode is a subcommand verb, got:\n%s", cmd)
	}
	if !strings.Contains(cmd, "--context=staging-ctx") {
		t.Errorf("expected --context=staging-ctx in CLI command, got:\n%s", cmd)
	}
	if !strings.Contains(cmd, "--namespace=dremio-ns") {
		t.Errorf("expected --namespace=dremio-ns in CLI command, got:\n%s", cmd)
	}
	// queries-perf-json should always be present
	if !strings.Contains(cmd, "--collect-queries-perf-json=false") {
		t.Errorf("expected --collect-queries-perf-json=false when CollectQueriesPerf is default false, got:\n%s", cmd)
	}
}

func TestBuildDiagnosisCLICommand_QueriesPerfEnabled(t *testing.T) {
	days := "3"
	dur := "60"
	cfg := &DiagnosisConfig{
		Transport:          "k8s",
		Namespace:          "dremio-ns",
		CoordinatorLogDir:  "/var/log/dremio",
		ExecutorLogDir:     "/var/log/dremio",
		DremioConfDir:      "/opt/dremio/conf",
		DremioRocksDBDir:   "/opt/dremio/data/db",
		CollectQueriesPerf: true,
	}
	cmd := buildDiagnosisCLICommand(cfg, &allTools, nil, &days, &dur, new(string))
	if !strings.Contains(cmd, "--collect-queries-perf-json=true") {
		t.Errorf("expected --collect-queries-perf-json=true when CollectQueriesPerf is true, got:\n%s", cmd)
	}
}

// TestBuildDiagnosisCLICommand_NoSystemTables verifies that deselecting all
// system tables in the TUI produces a diagnosis CLI command with --system-tables=
// (empty value). Same rationale as the standard-mode counterpart: without this,
// re-running the generated command would silently collect system tables.
func TestBuildDiagnosisCLICommand_NoSystemTables(t *testing.T) {
	days := "3"
	dur := "60"
	cfg := &DiagnosisConfig{
		Transport:         "k8s",
		Namespace:         "dremio-ns",
		CoordinatorLogDir: "/var/log/dremio",
		ExecutorLogDir:    "/var/log/dremio",
		DremioConfDir:     "/opt/dremio/conf",
		DremioRocksDBDir:  "/opt/dremio/data/db",
		CollectWLM:        true,
		SystemTables:      nil, // user deselected all tables
	}
	cmd := buildDiagnosisCLICommand(cfg, &allTools, nil, &days, &dur, new(string))
	if !strings.Contains(cmd, "--system-tables=") {
		t.Errorf("expected --system-tables= flag to be emitted even when SystemTables is empty, got:\n%s", cmd)
	}
	for _, line := range strings.Split(cmd, "\n") {
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "`"))
		trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, "\\"))
		if strings.HasPrefix(trimmed, "--system-tables=") && trimmed != "--system-tables=" {
			t.Errorf("expected --system-tables= with empty value, got %q in:\n%s", trimmed, cmd)
		}
	}
}

func TestBuildDiagnosisCLICommand_WithoutContext(t *testing.T) {
	days := "3"
	dur := "60"
	cfg := &DiagnosisConfig{
		Transport:         "k8s",
		Namespace:         "dremio-ns",
		K8sContext:        "",
		CoordinatorLogDir: "/var/log/dremio",
		ExecutorLogDir:    "/var/log/dremio",
		DremioConfDir:     "/opt/dremio/conf",
		DremioRocksDBDir:  "/opt/dremio/data/db",
	}
	cmd := buildDiagnosisCLICommand(cfg, &allTools, nil, &days, &dur, new(string))
	if strings.Contains(cmd, "--context") {
		t.Errorf("expected no --context in CLI command when K8sContext is empty, got:\n%s", cmd)
	}
}

func TestSortNodesMasterFirst_Mixed(t *testing.T) {
	input := []string{
		"dremio-executor-1",
		"dremio-coordinator-0",
		"dremio-master-0",
		"dremio-executor-0",
		"dremio-master-1",
		"dremio-coordinator-1",
	}
	got := SortNodesMasterFirst(input)
	want := []string{
		"dremio-master-0",
		"dremio-master-1",
		"dremio-coordinator-0",
		"dremio-coordinator-1",
		"dremio-executor-0",
		"dremio-executor-1",
	}
	if len(got) != len(want) {
		t.Fatalf("length mismatch: got %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSortNodesMasterFirst_Empty(t *testing.T) {
	got := SortNodesMasterFirst(nil)
	if got != nil {
		t.Errorf("expected nil for empty input, got %v", got)
	}
	got = SortNodesMasterFirst([]string{})
	if got != nil {
		t.Errorf("expected nil for zero-length input, got %v", got)
	}
}

func TestSortNodesMasterFirst_SSHIPs(t *testing.T) {
	// SSH-style IPs have no master/coordinator keywords — all land in "others" tier.
	input := []string{"10.0.0.3", "10.0.0.1", "10.0.0.2"}
	got := SortNodesMasterFirst(input)
	want := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}
	if len(got) != len(want) {
		t.Fatalf("length mismatch: got %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildDiagnosisCLICommand_WithNodeSelection(t *testing.T) {
	days := "3"
	dur := "60"
	cfg := &DiagnosisConfig{
		Namespace:         "dremio-ns",
		CoordinatorLogDir: "/var/log/dremio",
		ExecutorLogDir:    "/var/log/dremio",
		DremioConfDir:     "/opt/dremio/conf",
		DremioRocksDBDir:  "/opt/dremio/data/db",
	}
	nodes := []string{"dremio-master-0", "dremio-coordinator-0", "dremio-executor-0", "dremio-executor-1"}
	cmd := buildDiagnosisCLICommand(cfg, &allTools, &nodes, &days, &dur, new(string))
	if !strings.Contains(cmd, "--nodes=dremio-master-0,dremio-coordinator-0,dremio-executor-0,dremio-executor-1") {
		t.Errorf("expected --nodes flag with all selected nodes, got:\n%s", cmd)
	}
}

func TestBuildDiagnosisCLICommand_WithoutNodeSelection(t *testing.T) {
	days := "3"
	dur := "60"
	cfg := &DiagnosisConfig{
		Namespace:         "dremio-ns",
		CoordinatorLogDir: "/var/log/dremio",
		ExecutorLogDir:    "/var/log/dremio",
		DremioConfDir:     "/opt/dremio/conf",
		DremioRocksDBDir:  "/opt/dremio/data/db",
	}
	cmd := buildDiagnosisCLICommand(cfg, &allTools, nil, &days, &dur, new(string))
	if strings.Contains(cmd, "--coordinators") {
		t.Errorf("expected no --coordinators flag when no nodes selected, got:\n%s", cmd)
	}
	if strings.Contains(cmd, "--executors") {
		t.Errorf("expected no --executors flag when no nodes selected, got:\n%s", cmd)
	}
}

func TestBuildDiagnosisCLICommand_DateMode(t *testing.T) {
	days := "3"
	dur := "30"
	ds := "2026-04-01"
	cfg := &DiagnosisConfig{
		Namespace:         "default",
		CoordinatorLogDir: "/var/log/dremio",
		ExecutorLogDir:    "/var/log/dremio",
		DremioConfDir:     "/opt/dremio/conf",
		DremioRocksDBDir:  "/opt/dremio/data/db",
	}
	cmd := buildDiagnosisCLICommand(cfg, &allTools, nil, &days, &dur, &ds)
	if !strings.Contains(cmd, "--start-date=2026-04-01") {
		t.Errorf("expected --start-date flag, got:\n%s", cmd)
	}
	if !strings.Contains(cmd, "--days=3") {
		t.Errorf("expected --days=3 alongside --start-date, got:\n%s", cmd)
	}
	if strings.Contains(cmd, "--date-end") {
		t.Errorf("expected no --date-end flag, got:\n%s", cmd)
	}
}

func TestBuildDiagnosisCLICommand_DaysMode(t *testing.T) {
	days := "3"
	dur := "30"
	ds := ""
	cfg := &DiagnosisConfig{
		Namespace:         "default",
		CoordinatorLogDir: "/var/log/dremio",
		ExecutorLogDir:    "/var/log/dremio",
		DremioConfDir:     "/opt/dremio/conf",
		DremioRocksDBDir:  "/opt/dremio/data/db",
	}
	cmd := buildDiagnosisCLICommand(cfg, &allTools, nil, &days, &dur, &ds)
	if !strings.Contains(cmd, "--days=3") {
		t.Errorf("expected --days=3 in days mode, got:\n%s", cmd)
	}
	if strings.Contains(cmd, "--start-date") {
		t.Errorf("expected no --start-date in days mode, got:\n%s", cmd)
	}
}

func TestValidateDateOnly(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"empty string (auto)", "", false},
		{"valid date", "2026-04-01", false},
		{"valid date end of year", "2026-12-31", false},
		{"datetime with time", "2026-04-01T10:00:00", true},
		{"garbage string", "not-a-date", true},
		{"partial date", "2026-04", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateDateOnly(tc.input)
			if tc.wantErr && err == nil {
				t.Errorf("validateDateOnly(%q) expected error, got nil", tc.input)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("validateDateOnly(%q) unexpected error: %v", tc.input, err)
			}
		})
	}
}

func TestMapSelectedTools(t *testing.T) {
	tests := []struct {
		name          string
		selectedTools []string
		wantJFR       bool
		wantJStack    bool
		wantTop       bool
		wantAsync     bool
		wantHeapDump  bool
	}{
		{
			name:          "all tools selected",
			selectedTools: []string{"JFR", "jstack", "top", "async-profiler"},
			wantJFR:       true,
			wantJStack:    true,
			wantTop:       true,
			wantAsync:     true,
			wantHeapDump:  false, // heap dump is a separate Confirm, not in tools multi-select
		},
		{
			name:          "no tools selected",
			selectedTools: []string{},
			wantJFR:       false,
			wantJStack:    false,
			wantTop:       false,
			wantAsync:     false,
			wantHeapDump:  false,
		},
		{
			name:          "partial selection JFR and top only",
			selectedTools: []string{"JFR", "top"},
			wantJFR:       true,
			wantJStack:    false,
			wantTop:       true,
			wantAsync:     false,
			wantHeapDump:  false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &DiagnosisConfig{}
			mapSelectedTools(tc.selectedTools, cfg)
			if cfg.CollectJFR != tc.wantJFR {
				t.Errorf("CollectJFR = %v, want %v", cfg.CollectJFR, tc.wantJFR)
			}
			if cfg.CollectJStack != tc.wantJStack {
				t.Errorf("CollectJStack = %v, want %v", cfg.CollectJStack, tc.wantJStack)
			}
			if cfg.CollectTop != tc.wantTop {
				t.Errorf("CollectTop = %v, want %v", cfg.CollectTop, tc.wantTop)
			}
			if cfg.CollectAsyncProfiler != tc.wantAsync {
				t.Errorf("CollectAsyncProfiler = %v, want %v", cfg.CollectAsyncProfiler, tc.wantAsync)
			}
			if cfg.CollectHeapDump != tc.wantHeapDump {
				t.Errorf("CollectHeapDump = %v, want %v", cfg.CollectHeapDump, tc.wantHeapDump)
			}
		})
	}
}

func TestBuildDiagnosisCLICommand_DeselectedToolsHidden(t *testing.T) {
	days := "3"
	dur := "60"
	// Only JFR and top selected — jstack and async-profiler deselected
	tools := []string{"JFR", "top"}
	cfg := &DiagnosisConfig{
		Namespace:         "default",
		CoordinatorLogDir: "/var/log/dremio",
		ExecutorLogDir:    "/var/log/dremio",
		DremioConfDir:     "/opt/dremio/conf",
		DremioRocksDBDir:  "/opt/dremio/data/db",
	}
	cmd := buildDiagnosisCLICommand(cfg, &tools, nil, &days, &dur, new(string))
	// Selected tools should be true
	if !strings.Contains(cmd, "--diag-jfr=true") {
		t.Errorf("expected --diag-jfr=true, got:\n%s", cmd)
	}
	if !strings.Contains(cmd, "--diag-top=true") {
		t.Errorf("expected --diag-top=true, got:\n%s", cmd)
	}
	// Deselected tools should be false
	if !strings.Contains(cmd, "--diag-jstack=false") {
		t.Errorf("expected --diag-jstack=false, got:\n%s", cmd)
	}
	if !strings.Contains(cmd, "--diag-async-profiler=false") {
		t.Errorf("expected --diag-async-profiler=false, got:\n%s", cmd)
	}
	if !strings.Contains(cmd, "--diag-heap-dump=false") {
		t.Errorf("expected --diag-heap-dump=false, got:\n%s", cmd)
	}
	// Unified duration should always appear
	if !strings.Contains(cmd, "--diag-time-seconds=60") {
		t.Errorf("expected --diag-time-seconds=60 in command, got:\n%s", cmd)
	}
	// queries-perf-json should always be present
	if !strings.Contains(cmd, "--collect-queries-perf-json=") {
		t.Errorf("expected --collect-queries-perf-json flag in command, got:\n%s", cmd)
	}
}

func TestBuildStandardCLICommand_SSHTransportNoContext(t *testing.T) {
	cfg := &StandardConfig{
		Coordinator:       "192.168.1.10",
		SSHUser:           "dremio",
		K8sContext:        "some-ctx",
		CoordinatorLogDir: "/var/log/dremio",
		ExecutorLogDir:    "/var/log/dremio",
		DremioConfDir:     "/opt/dremio/conf",
		DremioRocksDBDir:  "/opt/dremio/data/db",
	}
	cmd := buildStandardCLICommand(cfg, 0, 0, 0, 0, 0)
	// --context should NOT appear for SSH transport (no namespace)
	if strings.Contains(cmd, "--context") {
		t.Errorf("expected no --context for SSH transport, got:\n%s", cmd)
	}
}

func TestBuildStandardCLICommand_LocalTransport(t *testing.T) {
	cfg := &StandardConfig{
		Transport:         "local",
		DremioHome:        "/opt/dremio",
		CoordinatorLogDir: "/var/log/dremio",
		ExecutorLogDir:    "/var/log/dremio",
		DremioConfDir:     "/opt/dremio/conf",
		DremioRocksDBDir:  "/opt/dremio/data/db",
	}
	cmd := buildStandardCLICommand(cfg, 0, 0, 0, 0, 0)
	if !strings.Contains(cmd, "collect local standard") {
		t.Errorf("expected 'collect local standard' in CLI command, got:\n%s", cmd)
	}
	if !strings.Contains(cmd, "--dremio-home=/opt/dremio") {
		t.Errorf("expected --dremio-home=/opt/dremio in CLI command, got:\n%s", cmd)
	}
	// Must NOT contain SSH or K8s transport flags (use = suffix to avoid matching --coordinator-log-dir)
	for _, flag := range []string{"--namespace=", "--coordinator=", "--ssh-user=", "--executors=", "--context="} {
		if strings.Contains(cmd, flag) {
			t.Errorf("local transport should not contain %s, got:\n%s", flag, cmd)
		}
	}
}

func TestBuildDiagnosisCLICommand_LocalTransport(t *testing.T) {
	days := "3"
	dur := "60"
	cfg := &DiagnosisConfig{
		Transport:         "local",
		DremioHome:        "/opt/dremio",
		CoordinatorLogDir: "/var/log/dremio",
		ExecutorLogDir:    "/var/log/dremio",
		DremioConfDir:     "/opt/dremio/conf",
		DremioRocksDBDir:  "/opt/dremio/data/db",
	}
	cmd := buildDiagnosisCLICommand(cfg, &allTools, nil, &days, &dur, new(string))
	if !strings.Contains(cmd, "collect local diagnosis") {
		t.Errorf("expected 'collect local diagnosis' in CLI command, got:\n%s", cmd)
	}
	if !strings.Contains(cmd, "--dremio-home=/opt/dremio") {
		t.Errorf("expected --dremio-home=/opt/dremio in CLI command, got:\n%s", cmd)
	}
	// Must NOT contain SSH or K8s transport flags (use = suffix to avoid matching --coordinator-log-dir)
	for _, flag := range []string{"--namespace=", "--coordinator=", "--ssh-user=", "--executors=", "--context="} {
		if strings.Contains(cmd, flag) {
			t.Errorf("local transport should not contain %s, got:\n%s", flag, cmd)
		}
	}
}
