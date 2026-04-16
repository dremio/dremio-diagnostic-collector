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
	"fmt"
	"runtime"
	"strings"
)

// CLIShellFormat returns OS-appropriate command formatting:
// binary name, line continuation character, and env var syntax.
func CLIShellFormat() (binary, continuation, patEnvVar string) {
	if runtime.GOOS == "windows" {
		return ".\\ddc.exe", " `", "$env:DDC_PAT_TOKEN"
	}
	return "ddc", " \\", "$DDC_PAT_TOKEN"
}

// CLICommandConfig holds the state needed to generate a reproducible CLI command.
type CLICommandConfig struct {
	Transport        string // "ssh", "k8s", "local", or "local-k8s"
	Mode             string
	Namespace        string // K8s transport
	Coordinator      string // SSH transport
	Executors        string
	SSHUser          string
	SSHKey           string
	SudoUser         string
	DremioEndpoint   string
	PatTokenProvided bool
	Days             int
	StartDate        string
	CollectHeapDump  bool
	Nodes            string
	ExcludeNodes     string
}

// GenerateCLICommand produces a reproducible ddc command from the config.
// Only flags that differ from defaults are included.
// Transport flags are always included.
// PAT token is shown as <REDACTED>.
func GenerateCLICommand(c CLICommandConfig) string {
	binary, cont, _ := CLIShellFormat()
	var parts []string
	parts = append(parts, binary)
	parts = append(parts, "collect")
	parts = append(parts, c.Transport)

	// Mode is a subcommand verb, not a flag
	parts = append(parts, c.Mode)

	// Transport flags — always included
	if c.Namespace != "" {
		parts = append(parts, fmt.Sprintf("--namespace=%s", c.Namespace))
	}
	if c.Coordinator != "" {
		parts = append(parts, fmt.Sprintf("--coordinator=%s", c.Coordinator))
	}
	if c.Executors != "" {
		parts = append(parts, fmt.Sprintf("--executors=%s", c.Executors))
	}
	if c.SSHUser != "" {
		parts = append(parts, fmt.Sprintf("--ssh-user=%s", c.SSHUser))
	}
	if c.SSHKey != "" {
		parts = append(parts, fmt.Sprintf("--ssh-key=%s", c.SSHKey))
	}
	if c.SudoUser != "" {
		parts = append(parts, fmt.Sprintf("--sudo-user=%s", c.SudoUser))
	}

	// Endpoint — include if non-default
	if c.DremioEndpoint != "" && c.DremioEndpoint != "http://localhost:9047" {
		parts = append(parts, fmt.Sprintf("--dremio-endpoint=%s", c.DremioEndpoint))
	}

	// PAT token — redacted
	if c.PatTokenProvided {
		parts = append(parts, "--dremio-pat-token=<REDACTED>")
	}

	// Non-default flags only
	if c.Days > 0 {
		parts = append(parts, fmt.Sprintf("--days=%d", c.Days))
	}
	if c.StartDate != "" {
		parts = append(parts, fmt.Sprintf("--start-date=%s", c.StartDate))
	}
	if c.CollectHeapDump {
		parts = append(parts, "--diag-heap-dump")
	}
	if c.Nodes != "" {
		parts = append(parts, fmt.Sprintf("--nodes=%s", c.Nodes))
	}
	if c.ExcludeNodes != "" {
		parts = append(parts, fmt.Sprintf("--exclude-nodes=%s", c.ExcludeNodes))
	}

	return strings.Join(parts, cont+"\n  ")
}
