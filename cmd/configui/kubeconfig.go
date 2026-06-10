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
	"fmt"
	"os"

	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/dirs"
	"k8s.io/client-go/tools/clientcmd"
)

// ValidateKubeconfigPath returns nil if s names a readable file that
// parses as a kubeconfig with at least one defined context. Tilde-prefixed
// paths are expanded against the current user's home directory before any
// filesystem check.
//
// Used as the synchronous Validate callback for the TUI kubeconfig-path
// input step (cmd/root.go). Connectivity checks are intentionally NOT
// performed here — they happen in a separate spinner step after the form
// returns.
func ValidateKubeconfigPath(s string) error {
	s = dirs.ExpandTilde(s)
	if s == "" {
		return fmt.Errorf("path is required")
	}
	if _, err := os.Stat(s); err != nil { // #nosec G703 -- s is user-supplied kubeconfig path being validated
		return fmt.Errorf("file not found")
	}
	cfg, err := clientcmd.LoadFromFile(s)
	if err != nil {
		return fmt.Errorf("invalid kubeconfig: %v", err)
	}
	if len(cfg.Contexts) == 0 {
		return fmt.Errorf("kubeconfig has no contexts")
	}
	return nil
}
