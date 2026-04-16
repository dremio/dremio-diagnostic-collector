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

package kubernetes

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestListContexts_MultipleContexts(t *testing.T) {
	kubeconfig := `apiVersion: v1
kind: Config
current-context: staging
clusters:
- cluster:
    server: https://prod.example.com
  name: prod-cluster
- cluster:
    server: https://staging.example.com
  name: staging-cluster
- cluster:
    server: https://dev.example.com
  name: dev-cluster
contexts:
- context:
    cluster: prod-cluster
    user: prod-user
  name: production
- context:
    cluster: staging-cluster
    user: staging-user
  name: staging
- context:
    cluster: dev-cluster
    user: dev-user
  name: development
users:
- name: prod-user
- name: staging-user
- name: dev-user
`
	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "config")
	if err := os.WriteFile(kubeconfigPath, []byte(kubeconfig), 0600); err != nil {
		t.Fatalf("failed to write temp kubeconfig: %v", err)
	}
	t.Setenv("KUBECONFIG", kubeconfigPath)

	contexts, currentContext, err := ListContexts()
	if err != nil {
		t.Fatalf("ListContexts() returned unexpected error: %v", err)
	}
	if currentContext != "staging" {
		t.Errorf("expected current context %q, got %q", "staging", currentContext)
	}
	expected := []string{"development", "production", "staging"}
	if !sort.StringsAreSorted(contexts) {
		t.Errorf("contexts are not sorted: %v", contexts)
	}
	if len(contexts) != len(expected) {
		t.Fatalf("expected %d contexts, got %d: %v", len(expected), len(contexts), contexts)
	}
	for i, name := range expected {
		if contexts[i] != name {
			t.Errorf("contexts[%d] = %q, want %q", i, contexts[i], name)
		}
	}
}

func TestListContexts_NoKubeconfig(t *testing.T) {
	tmpDir := t.TempDir()
	nonexistent := filepath.Join(tmpDir, "does-not-exist", "config")
	t.Setenv("KUBECONFIG", nonexistent)

	contexts, currentContext, err := ListContexts()
	if err != nil {
		t.Fatalf("ListContexts() returned unexpected error: %v", err)
	}
	if contexts != nil {
		t.Errorf("expected nil contexts, got %v", contexts)
	}
	if currentContext != "" {
		t.Errorf("expected empty current context, got %q", currentContext)
	}
}

func TestListContexts_SingleContext(t *testing.T) {
	kubeconfig := `apiVersion: v1
kind: Config
current-context: only-ctx
clusters:
- cluster:
    server: https://only.example.com
  name: only-cluster
contexts:
- context:
    cluster: only-cluster
    user: only-user
  name: only-ctx
users:
- name: only-user
`
	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "config")
	if err := os.WriteFile(kubeconfigPath, []byte(kubeconfig), 0600); err != nil {
		t.Fatalf("failed to write temp kubeconfig: %v", err)
	}
	t.Setenv("KUBECONFIG", kubeconfigPath)

	contexts, currentContext, err := ListContexts()
	if err != nil {
		t.Fatalf("ListContexts() returned unexpected error: %v", err)
	}
	if currentContext != "only-ctx" {
		t.Errorf("expected current context %q, got %q", "only-ctx", currentContext)
	}
	if len(contexts) != 1 {
		t.Fatalf("expected 1 context, got %d: %v", len(contexts), contexts)
	}
	if contexts[0] != "only-ctx" {
		t.Errorf("contexts[0] = %q, want %q", contexts[0], "only-ctx")
	}
}
