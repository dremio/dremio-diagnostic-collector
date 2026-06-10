package cmd

import (
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
