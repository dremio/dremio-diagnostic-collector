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

// kubernetes package provides access to log collections on k8s
package kubernetes

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/shutdown"
	v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

func TestNewKubectlK8sActions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testing in short mode")
	}
	namespace := "mynamespace"
	hook := shutdown.NewHook()
	defer hook.Cleanup()
	actions, err := NewK8sAPI(KubeArgs{
		Namespace: namespace,
	}, hook)
	if err != nil {
		t.Fatal(err)
	}
	if actions.namespace != namespace {
		t.Errorf("\nexpected \n%v\nbut got\n%v", namespace, actions.namespace)
	}
}

func TestK8SWriterChunkedWrites(t *testing.T) {
	tests := []struct {
		name   string
		chunks []string
		want   []string // expected lines delivered to output handler
	}{
		{
			name:   "single complete chunk",
			chunks: []string{"line1\nline2\n"},
			want:   []string{"line1", "line2"},
		},
		{
			name:   "split mid-line across two writes",
			chunks: []string{"1234 /var/lo", "g/file1\n5678 /var/log/file2\n"},
			want:   []string{"1234 /var/log/file1", "5678 /var/log/file2"},
		},
		{
			name:   "three chunks splitting one line",
			chunks: []string{"12", "34 /var/log", "/server.log\n"},
			want:   []string{"1234 /var/log/server.log"},
		},
		{
			name:   "trailing partial line flushed",
			chunks: []string{"line1\nline2-no-newline"},
			want:   []string{"line1", "line2-no-newline"},
		},
		{
			name:   "empty write",
			chunks: []string{""},
			want:   nil,
		},
		{
			name:   "preserves leading whitespace",
			chunks: []string{"  indented line\n"},
			want:   []string{"  indented line"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			w := &K8SWriter{
				Output: func(line string) { got = append(got, line) },
			}
			for _, chunk := range tt.chunks {
				_, err := w.Write([]byte(chunk))
				if err != nil {
					t.Fatalf("Write error: %v", err)
				}
			}
			w.Flush()
			if len(got) != len(tt.want) {
				t.Fatalf("got %d lines %v, want %d lines %v", len(got), got, len(tt.want), tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("line[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// --- Mock executor for newExecutor tests ---

// mockExecutor implements remotecommand.Executor with configurable StreamWithContext behavior.
type mockExecutor struct {
	streamErr error
	called    bool
}

func (m *mockExecutor) Stream(_ remotecommand.StreamOptions) error {
	m.called = true
	return m.streamErr
}

func (m *mockExecutor) StreamWithContext(_ context.Context, _ remotecommand.StreamOptions) error {
	m.called = true
	return m.streamErr
}

// helper to build a KubeCtlAPIActions with an injected SPDY factory (no real K8s client needed).
func newTestActions(spdyFn ExecutorFactory) *KubeCtlAPIActions {
	return &KubeCtlAPIActions{
		config:         &rest.Config{Host: "https://fake"},
		spdyExecutorFn: spdyFn,
		pidHosts:       make(map[string]string),
		protocol:       "SPDY",
	}
}

func TestNewExecutorSPDYSucceeds(t *testing.T) {
	spdyExec := &mockExecutor{}
	actions := newTestActions(
		func(_ *rest.Config, _ string, _ *url.URL) (remotecommand.Executor, error) {
			return spdyExec, nil
		},
	)

	u, _ := url.Parse("https://fake/api/v1/pods/test/exec")
	exec, err := actions.newExecutor("POST", u)
	if err != nil {
		t.Fatalf("newExecutor returned unexpected error: %v", err)
	}
	if exec == nil {
		t.Fatal("newExecutor returned nil executor")
	}
}

func TestNewExecutorSPDYFactoryFails(t *testing.T) {
	actions := newTestActions(
		func(_ *rest.Config, _ string, _ *url.URL) (remotecommand.Executor, error) {
			return nil, fmt.Errorf("spdy dial failed")
		},
	)

	u, _ := url.Parse("https://fake/api/v1/pods/test/exec")
	_, err := actions.newExecutor("POST", u)
	if err == nil {
		t.Fatal("expected error from newExecutor when SPDY factory fails")
	}
	if got := err.Error(); got != "spdy dial failed" {
		t.Errorf("unexpected error message: %s", got)
	}
}

func TestNewExecutorPassesConfigMethodURL(t *testing.T) {
	var gotMethod string
	var gotURL string

	actions := newTestActions(
		func(cfg *rest.Config, method string, u *url.URL) (remotecommand.Executor, error) {
			gotMethod = method
			gotURL = u.String()
			if cfg.Host != "https://fake" {
				t.Errorf("SPDY factory got wrong config host: %s", cfg.Host)
			}
			return &mockExecutor{}, nil
		},
	)

	u, _ := url.Parse("https://fake/api/v1/namespaces/ns/pods/pod/exec")
	_, err := actions.newExecutor("POST", u)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != "POST" {
		t.Errorf("SPDY factory got method %q, want POST", gotMethod)
	}
	expectedURL := "https://fake/api/v1/namespaces/ns/pods/pod/exec"
	if gotURL != expectedURL {
		t.Errorf("SPDY factory got URL %q, want %q", gotURL, expectedURL)
	}
}

func TestSearchPods_FiltersNonRunningPods(t *testing.T) {
	fakeClient := fake.NewSimpleClientset(
		&v1.Pod{
			ObjectMeta: meta_v1.ObjectMeta{
				Name:      "dremio-coordinator-0",
				Namespace: "dremio",
				Labels:    map[string]string{"app": "dremio"},
			},
			Status: v1.PodStatus{Phase: v1.PodRunning},
			Spec: v1.PodSpec{
				Containers: []v1.Container{{Name: "dremio-coordinator"}},
			},
		},
		&v1.Pod{
			ObjectMeta: meta_v1.ObjectMeta{
				Name:      "dremio-coordinator-1",
				Namespace: "dremio",
				Labels:    map[string]string{"app": "dremio"},
			},
			Status: v1.PodStatus{Phase: v1.PodPending},
			Spec: v1.PodSpec{
				Containers: []v1.Container{{Name: "dremio-coordinator"}},
			},
		},
		&v1.Pod{
			ObjectMeta: meta_v1.ObjectMeta{
				Name:      "dremio-executor-0",
				Namespace: "dremio",
				Labels:    map[string]string{"app": "dremio"},
			},
			Status: v1.PodStatus{Phase: v1.PodRunning},
			Spec: v1.PodSpec{
				Containers: []v1.Container{{Name: "dremio-executor"}},
			},
		},
	)

	actions := &KubeCtlAPIActions{
		namespace:           "dremio",
		detectLabelSelector: "app=dremio",
		client:              fakeClient,
	}

	// Search for coordinators — should return only the Running one
	pods, err := actions.SearchPods(func(container string) bool {
		return container == "dremio-coordinator"
	})
	if err != nil {
		t.Fatalf("SearchPods returned error: %v", err)
	}
	if len(pods) != 1 {
		t.Fatalf("expected 1 pod, got %d: %v", len(pods), pods)
	}
	if pods[0] != "dremio-coordinator-0" {
		t.Errorf("expected dremio-coordinator-0, got %v", pods[0])
	}

	// Search for executors — the Running executor should be returned
	pods, err = actions.SearchPods(func(container string) bool {
		return container == "dremio-executor"
	})
	if err != nil {
		t.Fatalf("SearchPods returned error: %v", err)
	}
	if len(pods) != 1 {
		t.Fatalf("expected 1 pod, got %d: %v", len(pods), pods)
	}
	if pods[0] != "dremio-executor-0" {
		t.Errorf("expected dremio-executor-0, got %v", pods[0])
	}
}

func TestGetClientset_ExplicitPathOverridesEnv(t *testing.T) {
	mkConfig := func(server string) string {
		return `apiVersion: v1
kind: Config
current-context: ctx
clusters:
- cluster:
    server: ` + server + `
  name: c
contexts:
- context:
    cluster: c
    user: u
  name: ctx
users:
- name: u
`
	}
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, "env-config")
	explicitPath := filepath.Join(tmpDir, "explicit-config")
	if err := os.WriteFile(envPath, []byte(mkConfig("https://env.example.com")), 0600); err != nil {
		t.Fatalf("write env config: %v", err)
	}
	if err := os.WriteFile(explicitPath, []byte(mkConfig("https://explicit.example.com")), 0600); err != nil {
		t.Fatalf("write explicit config: %v", err)
	}
	t.Setenv("KUBECONFIG", envPath)

	_, restCfg, err := GetClientset("", explicitPath)
	if err != nil {
		t.Fatalf("GetClientset error: %v", err)
	}
	if restCfg.Host != "https://explicit.example.com" {
		t.Errorf("explicit path lost — Host = %q, want https://explicit.example.com", restCfg.Host)
	}

	// Sanity: when explicit is empty, env wins.
	_, restCfg, err = GetClientset("", "")
	if err != nil {
		t.Fatalf("GetClientset (env-only) error: %v", err)
	}
	if restCfg.Host != "https://env.example.com" {
		t.Errorf("env path not used — Host = %q, want https://env.example.com", restCfg.Host)
	}
}

func TestResolveKubeconfigPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir failed: %v", err)
	}
	defaultHomePath := filepath.Join(home, ".kube", "config")

	tests := []struct {
		name     string
		explicit string
		envValue string
		envSet   bool // distinguishes env-unset from env-empty
		want     string
	}{
		{"explicit wins over env", "/explicit/path", "/env/path", true, "/explicit/path"},
		{"explicit wins over env (env unset)", "/explicit/path", "", false, "/explicit/path"},
		{"env used when explicit empty", "", "/env/path", true, "/env/path"},
		{"home used when both empty", "", "", false, defaultHomePath},
		{"home used when env empty-string", "", "", true, defaultHomePath},
		{"explicit tilde expanded", "~/.kube/conf", "", false, filepath.Join(home, ".kube", "conf")},
		{"env tilde expanded", "", "~/.kube/conf", true, filepath.Join(home, ".kube", "conf")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.envSet {
				t.Setenv("KUBECONFIG", tc.envValue)
			} else {
				// t.Setenv unsets at end, but we need it actually unset for this case.
				t.Setenv("KUBECONFIG", "")
				if err := os.Unsetenv("KUBECONFIG"); err != nil {
					t.Fatalf("unset KUBECONFIG: %v", err)
				}
			}
			got := resolveKubeconfigPath(tc.explicit)
			if got != tc.want {
				t.Errorf("resolveKubeconfigPath(%q) = %q, want %q", tc.explicit, got, tc.want)
			}
		})
	}
}

func TestVerifyConnectivity_Success(t *testing.T) {
	// Spin up a minimal fake apiserver that returns 200 on namespace list.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/namespaces") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"kind":"NamespaceList","apiVersion":"v1","items":[]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	// Build a kubeconfig pointing at the fake server.
	kubeconfig := `apiVersion: v1
kind: Config
current-context: fake
clusters:
- cluster:
    server: ` + srv.URL + `
    insecure-skip-tls-verify: true
  name: fake
contexts:
- context:
    cluster: fake
    user: fake
  name: fake
users:
- name: fake
`
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config")
	if err := os.WriteFile(cfgPath, []byte(kubeconfig), 0600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}

	if err := VerifyConnectivity(cfgPath, ""); err != nil {
		t.Errorf("VerifyConnectivity returned error: %v", err)
	}
}

func TestVerifyConnectivity_Failure(t *testing.T) {
	// Kubeconfig pointing at a port nothing listens on.
	kubeconfig := `apiVersion: v1
kind: Config
current-context: dead
clusters:
- cluster:
    server: http://127.0.0.1:1
  name: dead
contexts:
- context:
    cluster: dead
    user: dead
  name: dead
users:
- name: dead
`
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config")
	if err := os.WriteFile(cfgPath, []byte(kubeconfig), 0600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	if err := VerifyConnectivity(cfgPath, ""); err == nil {
		t.Error("VerifyConnectivity expected error, got nil")
	}
}
