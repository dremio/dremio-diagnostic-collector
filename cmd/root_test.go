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

// cmd package contains all the command line flag and initialization logic for commands
package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/collection"
	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/ssh"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/collects"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/output"
	"github.com/spf13/cobra"
)

func TestSSHDefault(t *testing.T) {
	sshPath, err := sshDefault()
	if err != nil {
		t.Fatalf("unexpected exception %v", err)
	}

	expectedPath := filepath.Join(".ssh", "id_rsa")
	if !strings.HasSuffix(sshPath, expectedPath) {
		t.Errorf("expected %v but was %v", expectedPath, sshPath)
	}
}

func TestValidateParameters(t *testing.T) {
	err := validateSSHParameters(ssh.Args{
		SSHKeyLoc: "/home/dremio/.ssh",
		SSHUser:   "dremio",
		SudoUser:  "",
	})
	if err != nil {
		t.Errorf("expected: nil but was %v", err.Error())
	}

	err = validateSSHParameters(ssh.Args{
		SSHKeyLoc: "",
		SSHUser:   "dremio",
	})
	if err == nil {
		t.Fatal("expected an error for empty SSH key location but got nil")
	}
	expectedError := "the ssh private key location was empty, pass --ssh-key or -s with the key to get past this error. Example --ssh-key ~/.ssh/id_rsa"
	if expectedError != err.Error() {
		t.Errorf("expected: %v but was %v", expectedError, err.Error())
	}

	err = validateSSHParameters(ssh.Args{
		SSHKeyLoc: "/home/dremio/.ssh",
		SSHUser:   "",
	})
	if err == nil {
		t.Fatal("expected an error for empty SSH user but got nil")
	}
	expectedError = "the ssh user was empty, pass --ssh-user or -u with the user name you want to use to get past this error. Example --ssh-user ubuntu"

	if expectedError != err.Error() {
		t.Errorf("expected: %v but was %v", expectedError, err.Error())
	}
}

func TestExecute(t *testing.T) {
	_ = makeTestCollection()
	actual, err := captureAllOutput(checkstds)
	// message, err := captureAllOutput(Execute)
	expected := "This is stdout\nThis is stderr\n"
	if expected != actual {
		t.Errorf("\nERROR: stdout : \nexpected:\t%v\nactual:\t\t%v\n", expected, actual)
	}
	if err != nil {
		t.Errorf("\nERROR: stderr : \nexpected:\t%v\nactual:\t\t%v\n", expected, err)
	}
}

// Set of args for other tests
func makeTestCollection() collection.Args {
	testCollection := collection.Args{
		OutputLoc: "/tmp/diags",
	}
	return testCollection
}

func captureAllOutput(f func()) (string, error) {
	var err error
	old := os.Stdout
	olderr := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		return "", err
	}
	os.Stdout = writer
	os.Stderr = writer

	f()

	_ = writer.Close()
	os.Stdout = old
	os.Stderr = olderr

	var buf bytes.Buffer
	_, err = io.Copy(&buf, reader)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

func checkstds() {
	_, _ = os.Stdout.Write([]byte("This is stdout\n"))
	_, _ = os.Stderr.Write([]byte("This is stderr\n"))
}

func TestAllSubCommandsAreWiredUp(t *testing.T) {
	helpText, err := output.CaptureOutput(func() {
		if err := RootCmd.Help(); err != nil {
			t.Errorf("unable to process help text with error %v", err)
		}
	})
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	expected := "Available Commands:\n  collect     Run non-interactive collection with provided flags\n  version     Print the version number of DDC\n"
	if !strings.Contains(helpText, expected) {
		t.Errorf("missing command text in `%q`", helpText)
	}
}

func TestCollectSubcommandHelp(t *testing.T) {
	usage := CollectCmd.UsageString()
	if !strings.Contains(usage, "ssh") {
		t.Errorf("collect usage should list 'ssh' subcommand, got:\n%s", usage)
	}
	if !strings.Contains(usage, "k8s") {
		t.Errorf("collect usage should list 'k8s' subcommand, got:\n%s", usage)
	}
}

func TestBareCollectShowsHelp(t *testing.T) {
	// "ddc collect" with no subcommand should return nil (help printed) and not error
	err := Execute([]string{"ddc", "collect"})
	if err != nil {
		t.Errorf("bare 'ddc collect' should show help without error, got: %v", err)
	}
}

func TestBareSSHShowsHelp(t *testing.T) {
	err := Execute([]string{"ddc", "collect", "ssh"})
	if err != nil {
		t.Errorf("bare 'ddc collect ssh' should show help without error, got: %v", err)
	}
}

func TestBareK8sShowsHelp(t *testing.T) {
	err := Execute([]string{"ddc", "collect", "k8s"})
	if err != nil {
		t.Errorf("bare 'ddc collect k8s' should show help without error, got: %v", err)
	}
}

func TestModeArgMigrationError(t *testing.T) {
	err := Execute([]string{"ddc", "collect", "ssh", "standard", "--mode", "standard", "--coordinator", "10.0.0.1"})
	if err == nil {
		t.Fatal("expected error when --mode is passed, got nil")
	}
	if !strings.Contains(err.Error(), "--mode flag has been removed") {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}

func TestModeArgMigrationErrorEquals(t *testing.T) {
	err := Execute([]string{"ddc", "collect", "ssh", "standard", "--mode=standard", "--coordinator", "10.0.0.1"})
	if err == nil {
		t.Fatal("expected error when --mode= is passed, got nil")
	}
	if !strings.Contains(err.Error(), "--mode flag has been removed") {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}

func TestOldSyntaxMigrationError(t *testing.T) {
	err := Execute([]string{"ddc", "collect", "standard", "--namespace", "test"})
	if err == nil || !strings.Contains(err.Error(), "command structure has changed") {
		t.Errorf("expected migration error for old syntax, got: %v", err)
	}
}

func TestStandardSubcommandSetsMode(t *testing.T) {
	oldMode := collectionMode
	defer func() { collectionMode = oldMode }()
	collectionMode = ""

	err := Execute([]string{"ddc", "collect", "ssh", "standard"})
	if collectionMode != collects.StandardCollection {
		t.Errorf("expected collectionMode=%q after 'collect ssh standard', got %q", collects.StandardCollection, collectionMode)
	}
	if err == nil || !strings.Contains(err.Error(), "--coordinator is required") {
		t.Errorf("expected SSH transport validation error, got: %v", err)
	}
}

func TestDiagnosisSubcommandSetsMode(t *testing.T) {
	oldMode := collectionMode
	defer func() { collectionMode = oldMode }()
	collectionMode = ""

	err := Execute([]string{"ddc", "collect", "k8s", "diagnosis"})
	if collectionMode != collects.DiagnosisCollection {
		t.Errorf("expected collectionMode=%q after 'collect k8s diagnosis', got %q", collects.DiagnosisCollection, collectionMode)
	}
	if err == nil || !strings.Contains(err.Error(), "--namespace is required") {
		t.Errorf("expected K8s transport validation error, got: %v", err)
	}
}

func TestExecutorPodSelection(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "picks dremio-executor pod",
			input:    "pod/dremio-master-0\npod/dremio-executor-0\npod/dremio-executor-1\n",
			expected: "dremio-executor-0",
		},
		{
			name:     "ignores healthcheck and other non-executor pods",
			input:    "pod/dremio-master-0\npod/healthcheck-aks-abc123\npod/dremio-executor-0\n",
			expected: "dremio-executor-0",
		},
		{
			name:     "returns empty when no executor pods exist",
			input:    "pod/dremio-master-0\npod/healthcheck-aks-abc123\n",
			expected: "",
		},
		{
			name:     "handles empty input",
			input:    "",
			expected: "",
		},
		{
			name:     "handles pod names without pod/ prefix",
			input:    "dremio-master-0\ndremio-executor-0\n",
			expected: "dremio-executor-0",
		},
		{
			name:     "old filter bug: healthcheck pod would have been selected",
			input:    "pod/dremio-master-0\npod/healthcheck-aks-xyz\n",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findExecutorPod(tt.input)
			if result != tt.expected {
				t.Errorf("findExecutorPod(%q) = %q; want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// ── Flag scoping and deletion tests ──

func TestDiagnosisOnlyFlagRejectedOnStandard(t *testing.T) {
	err := Execute([]string{"ddc", "collect", "ssh", "standard", "--diag-jfr"})
	if err == nil {
		t.Fatal("expected error when diagnosis-only flag --diag-jfr is used on standard, got nil")
	}
	if !strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("expected 'unknown flag' error, got: %v", err)
	}
}

func TestStandardFlagAcceptedOnStandard(t *testing.T) {
	err := Execute([]string{"ddc", "collect", "ssh", "standard", "--collect-server-logs=false"})
	if err != nil && strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("--collect-server-logs should be accepted on standard, got: %v", err)
	}
}

func TestDiagnosisFlagAcceptedOnDiagnosis(t *testing.T) {
	err := Execute([]string{"ddc", "collect", "ssh", "diagnosis", "--diag-jfr=false"})
	if err != nil && strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("--diag-jfr should be accepted on diagnosis, got: %v", err)
	}
}

func TestSharedFlagWorksOnBothSubcommands(t *testing.T) {
	for _, args := range [][]string{
		{"ddc", "collect", "k8s", "standard", "--namespace", "test"},
		{"ddc", "collect", "k8s", "diagnosis", "--namespace", "test"},
	} {
		err := Execute(args)
		if err != nil && strings.Contains(err.Error(), "unknown flag") {
			t.Errorf("--namespace should be accepted on %v, got: %v", args, err)
		}
	}
}

func TestSSHFlagRejectedOnK8s(t *testing.T) {
	err := Execute([]string{"ddc", "collect", "k8s", "standard", "--coordinator", "10.0.0.1"})
	if err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("expected --coordinator to be rejected on k8s, got: %v", err)
	}
}

func TestBareLocalK8sShowsHelp(t *testing.T) {
	err := Execute([]string{"ddc", "collect", "local-k8s"})
	if err != nil {
		t.Errorf("bare 'ddc collect local-k8s' should show help without error, got: %v", err)
	}
}

func TestLocalK8sStandardSetsMode(t *testing.T) {
	oldMode := collectionMode
	defer func() { collectionMode = oldMode }()
	collectionMode = ""

	// local-k8s standard is zero-config — should proceed to collection (which will
	// fail in test since no Dremio process is running, but mode must be set).
	_ = Execute([]string{"ddc", "collect", "local-k8s", "standard"})
	if collectionMode != collects.StandardCollection {
		t.Errorf("expected collectionMode=%q after 'collect local-k8s standard', got %q", collects.StandardCollection, collectionMode)
	}
}

func TestLocalK8sDiagnosisSetsMode(t *testing.T) {
	oldMode := collectionMode
	defer func() { collectionMode = oldMode }()
	collectionMode = ""

	_ = Execute([]string{"ddc", "collect", "local-k8s", "diagnosis"})
	if collectionMode != collects.DiagnosisCollection {
		t.Errorf("expected collectionMode=%q after 'collect local-k8s diagnosis', got %q", collects.DiagnosisCollection, collectionMode)
	}
}

func TestCollectSubcommandHelp_IncludesLocalK8s(t *testing.T) {
	usage := CollectCmd.UsageString()
	if !strings.Contains(usage, "local-k8s") {
		t.Errorf("collect usage should list 'local-k8s' subcommand, got:\n%s", usage)
	}
}

func TestK8sFlagRejectedOnSSH(t *testing.T) {
	err := Execute([]string{"ddc", "collect", "ssh", "standard", "--namespace", "test"})
	if err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("expected --namespace to be rejected on ssh, got: %v", err)
	}
}

func TestDeletedFlagsAbsentFromHelp(t *testing.T) {
	deletedFlags := []string{"collect-audit-log", "dremio-gclogs-dir"}
	cmds := map[string]*cobra.Command{
		"CollectCmd":      CollectCmd,
		"SSHStandardCmd":  SSHStandardCmd,
		"SSHDiagnosisCmd": SSHDiagnosisCmd,
		"K8sStandardCmd":  K8sStandardCmd,
		"K8sDiagnosisCmd": K8sDiagnosisCmd,
	}
	for cmdName, cmd := range cmds {
		for _, flag := range deletedFlags {
			if cmd.Flags().Lookup(flag) != nil {
				t.Errorf("deleted flag --%s should not exist on %s", flag, cmdName)
			}
			if cmd.PersistentFlags().Lookup(flag) != nil {
				t.Errorf("deleted flag --%s should not exist on %s persistent flags", flag, cmdName)
			}
		}
	}
}

func TestDeletedFlagRejected(t *testing.T) {
	err := Execute([]string{"ddc", "collect", "ssh", "diagnosis", "--collect-audit-log"})
	if err == nil {
		t.Fatal("expected error for deleted flag --collect-audit-log, got nil")
	}
	if !strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("expected 'unknown flag' error for deleted flag, got: %v", err)
	}
}

func TestDetectNamespaceFromFile(t *testing.T) {
	tmpDir := t.TempDir()
	nsFile := filepath.Join(tmpDir, "namespace")
	if err := os.WriteFile(nsFile, []byte("dremio-prod"), 0600); err != nil {
		t.Fatal(err)
	}
	ns, err := detectK8sNamespace(nsFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ns != "dremio-prod" {
		t.Errorf("expected 'dremio-prod', got %q", ns)
	}
}

func TestDetectNamespaceFromFile_Missing(t *testing.T) {
	_, err := detectK8sNamespace("/nonexistent/path/namespace")
	if err == nil {
		t.Error("expected error for missing namespace file")
	}
}

func TestDetectNamespaceFromFile_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	nsFile := filepath.Join(tmpDir, "namespace")
	if err := os.WriteFile(nsFile, []byte(""), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := detectK8sNamespace(nsFile)
	if err == nil {
		t.Error("expected error for empty namespace file")
	}
}

func TestDetectNamespaceFromFile_Whitespace(t *testing.T) {
	tmpDir := t.TempDir()
	nsFile := filepath.Join(tmpDir, "namespace")
	if err := os.WriteFile(nsFile, []byte("  my-namespace\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ns, err := detectK8sNamespace(nsFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ns != "my-namespace" {
		t.Errorf("expected 'my-namespace', got %q", ns)
	}
}

func TestExtractEnvValue(t *testing.T) {
	tests := []struct {
		name string
		ps   string
		key  string
		want string
	}{
		{
			name: "single -D flag",
			ps:   "java -Ddremio.log.path=/opt/dremio/log -Xmx2g",
			key:  "-Ddremio.log.path=",
			want: "/opt/dremio/log",
		},
		{
			name: "duplicate -D flag — last wins (JVM semantics)",
			ps:   "java -Ddremio.log.path=/opt/dremio/log -Xmx2g -Ddremio.log.path=/opt/dremio/data/log -XX:+UseG1GC",
			key:  "-Ddremio.log.path=",
			want: "/opt/dremio/data/log",
		},
		{
			name: "trailing comma is stripped",
			ps:   "java -Ddremio.log.path=/opt/dremio/log, -Xmx2g",
			key:  "-Ddremio.log.path=",
			want: "/opt/dremio/log",
		},
		{
			name: "duplicate with first having trailing comma — last still wins",
			ps:   "java -Ddremio.log.path=/opt/dremio/log, -Ddremio.log.path=/opt/dremio/data/log",
			key:  "-Ddremio.log.path=",
			want: "/opt/dremio/data/log",
		},
		{
			name: "env-var style key",
			ps:   "DREMIO_HOME=/opt/dremio DREMIO_LOG_DIR=/opt/dremio/log PATH=/bin",
			key:  "DREMIO_LOG_DIR=",
			want: "/opt/dremio/log",
		},
		{
			name: "key not found",
			ps:   "java -Xmx2g",
			key:  "-Ddremio.log.path=",
			want: "",
		},
		{
			name: "value at end of string (no trailing space)",
			ps:   "java -Ddremio.log.path=/opt/dremio/data/log",
			key:  "-Ddremio.log.path=",
			want: "/opt/dremio/data/log",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractEnvValue(tc.ps, tc.key)
			if got != tc.want {
				t.Errorf("extractEnvValue(%q, %q) = %q, want %q", tc.ps, tc.key, got, tc.want)
			}
		})
	}
}

// oldExtractEnvValue is a verbatim copy of the pre-fix implementation, preserved
// here as a control so the regression test below can prove the old code failed
// on the diag-20260521-171640 bundle's input while the new code succeeds.
func oldExtractEnvValue(ps, key string) string {
	idx := strings.Index(ps, key)
	if idx < 0 {
		return ""
	}
	rest := ps[idx+len(key):]
	end := strings.IndexAny(rest, " \t\n\x00")
	if end < 0 {
		return strings.TrimSpace(rest)
	}
	return strings.TrimSpace(rest[:end])
}

// TestExtractEnvValue_RegressionDiagBundle pins down the exact failure mode from
// diag-20260521-171640: pods.json shows DREMIO_LOG_DIR=/opt/dremio/log and
// DREMIO_JAVA_SERVER_EXTRA_OPTS containing -Ddremio.log.path=/opt/dremio/data/log.
// The launcher derives -Ddremio.log.path=$DREMIO_LOG_DIR for its own defaults,
// then appends the extra opts — resulting in two -Ddremio.log.path= flags on the
// java command line. JVM resolves the last one (jvm_settings.txt confirmed
// dremio.log.path=/opt/dremio/data/log) but the old extractEnvValue took the
// first.
func TestExtractEnvValue_RegressionDiagBundle(t *testing.T) {
	// Simulates /proc/1/cmdline | tr '\0' ' ' — both -D flags present.
	// The trailing comma on the first reproduces the artifact seen in
	// ddc.log:34 ('dremio-log-dir':'/opt/dremio/log,').
	cmdline := "java -Xms2g -Xmx2g -Ddremio.log.path=/opt/dremio/log, -Djava.security.krb5.conf=/opt/dremio/krb/krb5.conf -Ddremio.log.path=/opt/dremio/data/log -XX:+UseG1GC DREMIO_LOG_DIR=/opt/dremio/log"

	oldGot := oldExtractEnvValue(cmdline, "-Ddremio.log.path=")
	if oldGot != "/opt/dremio/log," {
		t.Errorf("control: old strings.Index-based code should have produced the buggy %q, got %q — input may not faithfully reproduce the bundle", "/opt/dremio/log,", oldGot)
	}

	newGot := extractEnvValue(cmdline, "-Ddremio.log.path=")
	if newGot != "/opt/dremio/data/log" {
		t.Errorf("fix: new code should return JVM-resolved %q, got %q", "/opt/dremio/data/log", newGot)
	}
}
