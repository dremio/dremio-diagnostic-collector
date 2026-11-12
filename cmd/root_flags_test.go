package cmd

import (
	"strings"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/local/conf"
)

var perLogNumDaysFlags = []string{
	"queries-json-num-days",
	"queries-perf-num-days",
	"server-logs-num-days",
	"tracker-json-num-days",
	"vacuum-log-num-days",
}

func TestDiagnosisCmdDoesNotHavePerLogNumDaysFlags(t *testing.T) {
	for _, name := range perLogNumDaysFlags {
		if SSHDiagnosisCmd.Flags().Lookup(name) != nil {
			t.Errorf("SSHDiagnosisCmd should not have flag --%s", name)
		}
		if K8sDiagnosisCmd.Flags().Lookup(name) != nil {
			t.Errorf("K8sDiagnosisCmd should not have flag --%s", name)
		}
	}
}

func TestStandardCmdHasPerLogNumDaysFlags(t *testing.T) {
	for _, name := range perLogNumDaysFlags {
		if SSHStandardCmd.Flags().Lookup(name) == nil {
			t.Errorf("SSHStandardCmd should have flag --%s", name)
		}
		if K8sStandardCmd.Flags().Lookup(name) == nil {
			t.Errorf("K8sStandardCmd should have flag --%s", name)
		}
	}
}

func TestDiagnosisTimeFlagOnlyOnDiagnosis(t *testing.T) {
	if SSHDiagnosisCmd.Flags().Lookup("diag-time-seconds") == nil {
		t.Error("SSHDiagnosisCmd should have --diag-time-seconds")
	}
	if SSHStandardCmd.Flags().Lookup("diag-time-seconds") != nil {
		t.Error("SSHStandardCmd should not have --diag-time-seconds")
	}
}

func TestNodesOnlyOnK8s(t *testing.T) {
	if K8sCmd.PersistentFlags().Lookup("nodes") == nil {
		t.Error("K8sCmd should have --nodes flag")
	}
	if SSHCmd.PersistentFlags().Lookup("nodes") != nil {
		t.Error("SSHCmd should not have --nodes flag")
	}
}

func TestHSErrOnlyOnDiagnosis(t *testing.T) {
	if SSHDiagnosisCmd.Flags().Lookup("collect-hs-err-files") == nil {
		t.Error("SSHDiagnosisCmd should have --collect-hs-err-files")
	}
	if SSHStandardCmd.Flags().Lookup("collect-hs-err-files") != nil {
		t.Error("SSHStandardCmd should not have --collect-hs-err-files")
	}
}

func TestQueriesPerfOnBothModes(t *testing.T) {
	if SSHStandardCmd.Flags().Lookup("collect-queries-perf-json") == nil {
		t.Error("SSHStandardCmd should have --collect-queries-perf-json")
	}
	if SSHDiagnosisCmd.Flags().Lookup("collect-queries-perf-json") == nil {
		t.Error("SSHDiagnosisCmd should have --collect-queries-perf-json")
	}
}

func TestQueriesPerfNumDaysOnlyOnStandard(t *testing.T) {
	if SSHStandardCmd.Flags().Lookup("queries-perf-num-days") == nil {
		t.Error("SSHStandardCmd should have --queries-perf-num-days")
	}
	if SSHDiagnosisCmd.Flags().Lookup("queries-perf-num-days") != nil {
		t.Error("SSHDiagnosisCmd should not have --queries-perf-num-days")
	}
}

func TestHiveDeprecatedOnlyOnDiagnosis(t *testing.T) {
	if SSHDiagnosisCmd.Flags().Lookup("collect-hive-deprecated-log") == nil {
		t.Error("SSHDiagnosisCmd should have --collect-hive-deprecated-log")
	}
	if K8sDiagnosisCmd.Flags().Lookup("collect-hive-deprecated-log") == nil {
		t.Error("K8sDiagnosisCmd should have --collect-hive-deprecated-log")
	}
	if SSHStandardCmd.Flags().Lookup("collect-hive-deprecated-log") != nil {
		t.Error("SSHStandardCmd should not have --collect-hive-deprecated-log")
	}
}

func TestMetaRefreshLogOnBothModes(t *testing.T) {
	if SSHStandardCmd.Flags().Lookup("collect-meta-refresh-log") == nil {
		t.Error("SSHStandardCmd should have --collect-meta-refresh-log")
	}
	if SSHDiagnosisCmd.Flags().Lookup("collect-meta-refresh-log") == nil {
		t.Error("SSHDiagnosisCmd should have --collect-meta-refresh-log")
	}
	if K8sStandardCmd.Flags().Lookup("collect-meta-refresh-log") == nil {
		t.Error("K8sStandardCmd should have --collect-meta-refresh-log")
	}
	if K8sDiagnosisCmd.Flags().Lookup("collect-meta-refresh-log") == nil {
		t.Error("K8sDiagnosisCmd should have --collect-meta-refresh-log")
	}
}

func TestResolveMetaRefresh(t *testing.T) {
	stdDefault := map[string]interface{}{conf.KeyCollectMetaRefreshLog: false}
	diagDefault := map[string]interface{}{conf.KeyCollectMetaRefreshLog: true}

	// CLI standard, flag unset: confData=false must win over the leaked global=true.
	if resolveMetaRefresh(true, true, stdDefault) {
		t.Error("CLI standard default must resolve false despite leaked global=true")
	}
	// CLI, user enabled (confData=true): resolves true.
	if !resolveMetaRefresh(true, false, diagDefault) {
		t.Error("CLI must honor confData=true")
	}
	// TUI: trust the global, ignore confData.
	if !resolveMetaRefresh(false, true, stdDefault) {
		t.Error("TUI must trust global=true even when confData=false")
	}
	if resolveMetaRefresh(false, false, diagDefault) {
		t.Error("TUI must trust global=false even when confData=true")
	}
}

func TestNumDaysFlagsDocumentZero(t *testing.T) {
	const suffix = "(0 = no limit)"
	for _, name := range []string{"queries-json-num-days", "server-logs-num-days", "tracker-json-num-days", "vacuum-log-num-days"} {
		f := K8sStandardCmd.Flags().Lookup(name)
		if f == nil {
			t.Fatalf("flag --%s missing on K8sStandardCmd", name)
		}
		if !strings.HasSuffix(f.Usage, suffix) {
			t.Errorf("--%s usage %q does not end with %q", name, f.Usage, suffix)
		}
	}
	const perfSuffix = "(0 = no limit: exports the full job history, which can take hours on large catalogs)"
	f := K8sStandardCmd.Flags().Lookup("queries-perf-num-days")
	if f == nil {
		t.Fatal("flag --queries-perf-num-days missing on K8sStandardCmd")
	}
	if !strings.HasSuffix(f.Usage, perfSuffix) {
		t.Errorf("--queries-perf-num-days usage %q does not end with %q", f.Usage, perfSuffix)
	}
}

func TestCollectorTimeoutDeprecated(t *testing.T) {
	f := CollectCmd.PersistentFlags().Lookup("collector-timeout")
	if f == nil {
		t.Fatal("flag --collector-timeout missing on CollectCmd")
	}
	if f.Deprecated != "it has no effect and will be removed in a future release" {
		t.Errorf("unexpected Deprecated message %q", f.Deprecated)
	}
	if !f.Hidden {
		t.Error("--collector-timeout should be hidden")
	}
	if err := K8sStandardCmd.ParseFlags([]string{"--collector-timeout=5m"}); err != nil {
		t.Errorf("deprecated flag must still be accepted, got %v", err)
	}
}
