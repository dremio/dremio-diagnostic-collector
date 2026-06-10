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
	"os"
	"path/filepath"
	"testing"
)

func TestValidateKubeconfigPath(t *testing.T) {
	tmpDir := t.TempDir()

	missing := filepath.Join(tmpDir, "does-not-exist")

	malformed := filepath.Join(tmpDir, "malformed")
	if err := os.WriteFile(malformed, []byte("not: valid: yaml: ::"), 0600); err != nil {
		t.Fatalf("write malformed: %v", err)
	}

	noContexts := filepath.Join(tmpDir, "no-contexts")
	if err := os.WriteFile(noContexts, []byte(`apiVersion: v1
kind: Config
clusters: []
contexts: []
users: []
`), 0600); err != nil {
		t.Fatalf("write no-contexts: %v", err)
	}

	oneContext := filepath.Join(tmpDir, "one-context")
	if err := os.WriteFile(oneContext, []byte(`apiVersion: v1
kind: Config
current-context: only
clusters:
- cluster:
    server: https://only.example.com
  name: c
contexts:
- context:
    cluster: c
    user: u
  name: only
users:
- name: u
`), 0600); err != nil {
		t.Fatalf("write one-context: %v", err)
	}

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"empty path", "", true},
		{"missing file", missing, true},
		{"malformed yaml", malformed, true},
		{"zero contexts", noContexts, true},
		{"valid one context", oneContext, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateKubeconfigPath(tc.input)
			if tc.wantErr && err == nil {
				t.Errorf("expected error for %q, got nil", tc.input)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error for %q: %v", tc.input, err)
			}
		})
	}
}
