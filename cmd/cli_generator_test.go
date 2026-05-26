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

package cmd

import (
	"strings"
	"testing"
)

func TestGenerateCLICommand_K8sDiagnosis(t *testing.T) {
	cmd := GenerateCLICommand(CLICommandConfig{
		Transport: "k8s",
		Mode:      "diagnosis",
		Namespace: "dremio",
	})
	if !strings.Contains(cmd, "collect") {
		t.Error("expected command to contain collect subcommand")
	}
	if !strings.Contains(cmd, "k8s") {
		t.Error("expected k8s transport subcommand")
	}
	if !strings.Contains(cmd, "diagnosis") {
		t.Error("expected diagnosis subcommand verb")
	}
	if strings.Contains(cmd, "--mode") {
		t.Error("should not contain --mode flag; mode is a subcommand verb")
	}
	if !strings.Contains(cmd, "--namespace=dremio") {
		t.Error("expected --namespace=dremio")
	}
}

func TestGenerateCLICommand_SSHStandard(t *testing.T) {
	cmd := GenerateCLICommand(CLICommandConfig{
		Transport:   "ssh",
		Mode:        "standard",
		Coordinator: "10.0.0.1,10.0.0.2",
		Executors:   "10.0.0.3,10.0.0.4",
		SSHUser:     "dremio",
		SSHKey:      "/home/user/.ssh/id_rsa",
		SudoUser:    "root",
	})
	if !strings.Contains(cmd, "collect") || !strings.Contains(cmd, "ssh") || !strings.Contains(cmd, "standard") {
		t.Error("expected collect ssh standard subcommand")
	}
	if strings.Contains(cmd, "--mode") {
		t.Error("should not contain --mode flag; mode is a subcommand verb")
	}
	if !strings.Contains(cmd, "--coordinator=10.0.0.1,10.0.0.2") {
		t.Error("expected --coordinator")
	}
	if !strings.Contains(cmd, "--executors=10.0.0.3,10.0.0.4") {
		t.Error("expected --executors")
	}
	if !strings.Contains(cmd, "--ssh-user=dremio") {
		t.Error("expected --ssh-user")
	}
	if !strings.Contains(cmd, "--ssh-key=/home/user/.ssh/id_rsa") {
		t.Error("expected --ssh-key")
	}
	if !strings.Contains(cmd, "--sudo-user=root") {
		t.Error("expected --sudo-user")
	}
	// Should NOT contain K8s flags
	if strings.Contains(cmd, "--namespace") {
		t.Error("SSH mode should not include --namespace")
	}
}

func TestGenerateCLICommand_TransportRequired(t *testing.T) {
	cmd := GenerateCLICommand(CLICommandConfig{
		Transport: "k8s",
		Mode:      "standard",
		Namespace: "dremio",
	})
	if !strings.Contains(cmd, "k8s") {
		t.Error("expected k8s transport in output")
	}

	cmd = GenerateCLICommand(CLICommandConfig{
		Transport:   "ssh",
		Mode:        "standard",
		Coordinator: "10.0.0.1",
	})
	if !strings.Contains(cmd, "ssh") {
		t.Error("expected ssh transport in output")
	}
}

func TestGenerateCLICommand_RedactsPAT(t *testing.T) {
	cmd := GenerateCLICommand(CLICommandConfig{
		Transport:        "k8s",
		Mode:             "diagnosis",
		Namespace:        "dremio",
		PatTokenProvided: true,
	})
	if !strings.Contains(cmd, "--dremio-pat-token=<REDACTED>") {
		t.Error("expected PAT to be redacted")
	}
	// Should never contain actual token
	if strings.Contains(cmd, "actual-token") {
		t.Error("PAT should be redacted, not actual value")
	}
}

func TestGenerateCLICommand_OmitsDefaults(t *testing.T) {
	cmd := GenerateCLICommand(CLICommandConfig{
		Transport: "k8s",
		Mode:      "standard",
		Namespace: "dremio",
	})
	// No PAT → no PAT flag
	if strings.Contains(cmd, "--dremio-pat-token") {
		t.Error("should not include PAT when not provided")
	}
	// No heap dump → no heap dump flag
	if strings.Contains(cmd, "--diag-heap-dump") {
		t.Error("should not include heap dump when disabled")
	}
}

func TestGenerateCLICommand_IncludesNonDefaults(t *testing.T) {
	cmd := GenerateCLICommand(CLICommandConfig{
		Transport:       "k8s",
		Mode:            "diagnosis",
		Namespace:       "dremio",
		Days:            7,
		CollectHeapDump: true,
		Nodes:           "dremio-master-0,dremio-executor-0",
	})
	if !strings.Contains(cmd, "--days=7") {
		t.Error("expected --days=7")
	}
	if !strings.Contains(cmd, "--diag-heap-dump") {
		t.Error("expected --diag-heap-dump")
	}
	if !strings.Contains(cmd, "--nodes=dremio-master-0,dremio-executor-0") {
		t.Error("expected --nodes")
	}
}

func TestGenerateCLICommand_EndpointOmittedWhenDefault(t *testing.T) {
	cmd := GenerateCLICommand(CLICommandConfig{
		Transport:      "k8s",
		Mode:           "standard",
		Namespace:      "dremio",
		DremioEndpoint: "http://localhost:9047",
	})
	if strings.Contains(cmd, "--dremio-endpoint") {
		t.Error("default endpoint should be omitted")
	}
}

func TestGenerateCLICommand_EndpointIncludedWhenNonDefault(t *testing.T) {
	cmd := GenerateCLICommand(CLICommandConfig{
		Transport:      "k8s",
		Mode:           "standard",
		Namespace:      "dremio",
		DremioEndpoint: "https://dremio.internal:9047",
	})
	if !strings.Contains(cmd, "--dremio-endpoint=https://dremio.internal:9047") {
		t.Error("non-default endpoint should be included")
	}
}

func TestGenerateCLICommand_LocalK8sStandard(t *testing.T) {
	cmd := GenerateCLICommand(CLICommandConfig{
		Transport: "local-k8s",
		Mode:      "standard",
	})
	if !strings.Contains(cmd, "collect") {
		t.Error("expected command to contain collect subcommand")
	}
	if !strings.Contains(cmd, "local-k8s") {
		t.Error("expected local-k8s transport subcommand")
	}
	if !strings.Contains(cmd, "standard") {
		t.Error("expected standard subcommand verb")
	}
	// Should NOT contain K8s-specific flags
	if strings.Contains(cmd, "--namespace") {
		t.Error("local-k8s mode should not include --namespace")
	}
	if strings.Contains(cmd, "--detect-label-selector") {
		t.Error("local-k8s mode should not include --detect-label-selector")
	}
	if strings.Contains(cmd, "--container-log-label-selector") {
		t.Error("local-k8s mode should not include --container-log-label-selector")
	}
	if strings.Contains(cmd, "--context") {
		t.Error("local-k8s mode should not include --context")
	}
}

func TestGenerateCLICommand_LocalK8sDiagnosis(t *testing.T) {
	cmd := GenerateCLICommand(CLICommandConfig{
		Transport: "local-k8s",
		Mode:      "diagnosis",
		Days:      3,
	})
	if !strings.Contains(cmd, "local-k8s") {
		t.Error("expected local-k8s transport")
	}
	if !strings.Contains(cmd, "diagnosis") {
		t.Error("expected diagnosis mode")
	}
	if !strings.Contains(cmd, "--days=3") {
		t.Error("expected --days=3")
	}
}

func TestGenerateCLICommand_KubeconfigEmitted(t *testing.T) {
	cmd := GenerateCLICommand(CLICommandConfig{
		Transport:  "k8s",
		Mode:       "diagnosis",
		Namespace:  "dremio",
		Kubeconfig: "/home/user/.kube/config",
	})
	if !strings.Contains(cmd, "--kubeconfig=/home/user/.kube/config") {
		t.Errorf("expected --kubeconfig flag in output, got: %s", cmd)
	}
}

func TestGenerateCLICommand_KubeconfigOmittedWhenEmpty(t *testing.T) {
	cmd := GenerateCLICommand(CLICommandConfig{
		Transport: "k8s",
		Mode:      "standard",
		Namespace: "dremio",
	})
	if strings.Contains(cmd, "--kubeconfig") {
		t.Errorf("expected NO --kubeconfig flag (empty Kubeconfig field), got: %s", cmd)
	}
}

func TestGenerateCLICommand_KubeconfigPositionedAboveNamespace(t *testing.T) {
	cmd := GenerateCLICommand(CLICommandConfig{
		Transport:  "k8s",
		Mode:       "standard",
		Namespace:  "dremio",
		Kubeconfig: "/path/to/kc",
	})
	kIdx := strings.Index(cmd, "--kubeconfig=")
	nIdx := strings.Index(cmd, "--namespace=")
	if kIdx < 0 || nIdx < 0 {
		t.Fatalf("missing flags in output: %s", cmd)
	}
	if kIdx > nIdx {
		t.Errorf("--kubeconfig must appear before --namespace; got: %s", cmd)
	}
}
