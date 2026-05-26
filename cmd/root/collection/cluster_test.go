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

// collection package provides the interface for collection implementation and the actual collection execution
package collection

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/helpers"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

type stubHook struct{ ctx context.Context }

func (s *stubHook) GetContext() context.Context { return s.ctx }

type stubCS struct{ dir string }

func (s *stubCS) CreatePath(_, _, _ string) (string, error) { return s.dir, nil }
func (s *stubCS) ArchiveDiag(_, _ string) error             { return nil }
func (s *stubCS) GetTmpDir() string                         { return s.dir }

func makePod(name string, labels map[string]string, restarts int32) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test-ns", Labels: labels},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "main", Image: "img"}}},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{{Name: "main", RestartCount: restarts}},
		},
	}
}

// listActionRestrictions returns the label restriction string from the first
// pods List action recorded against the fake clientset. Tests in this file
// expect exactly one List call per scenario.
func listActionRestrictions(t *testing.T, fc *fake.Clientset) string {
	t.Helper()
	for _, a := range fc.Actions() {
		if la, ok := a.(k8stesting.ListAction); ok && la.GetResource().Resource == "pods" {
			return la.GetListRestrictions().Labels.String()
		}
	}
	return "<no pods list action recorded>"
}

func TestGetClusterLogs_EmptySelector_ListsAllNamespacePods(t *testing.T) {
	dremio := makePod("dremio-master-0", map[string]string{"role": "dremio-cluster-pod"}, 0)
	other := makePod("opensearch-0", map[string]string{"app": "opensearch"}, 0)
	fc := fake.NewSimpleClientset(dremio, other)
	dir := t.TempDir()

	err := GetClusterLogs(&stubHook{ctx: context.Background()}, "test-ns", fc, &stubCS{dir: dir}, helpers.NewRealFileSystem(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := listActionRestrictions(t, fc); got != "" {
		t.Errorf("expected empty label restrictions (namespace-wide list), got %q", got)
	}

	entries, _ := os.ReadDir(dir)
	names := []string{}
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	// GetClusterLogs writes both current (-main.txt) and previous (-main-previous.txt)
	// files per container, so 2 pods × 2 files = 4 expected outputs.
	want := []string{"dremio-master-0-main-previous.txt", "dremio-master-0-main.txt", "opensearch-0-main-previous.txt", "opensearch-0-main.txt"}
	if len(names) != len(want) {
		t.Fatalf("expected %d log files, got %d: %v", len(want), len(names), names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("file[%d] = %q, want %q", i, names[i], want[i])
		}
	}
}

func TestGetClusterLogs_NonEmptySelector_FiltersPods(t *testing.T) {
	dremio := makePod("dremio-master-0", map[string]string{"role": "dremio-cluster-pod"}, 0)
	other := makePod("opensearch-0", map[string]string{"app": "opensearch"}, 0)
	fc := fake.NewSimpleClientset(dremio, other)
	dir := t.TempDir()

	err := GetClusterLogs(&stubHook{ctx: context.Background()}, "test-ns", fc, &stubCS{dir: dir}, helpers.NewRealFileSystem(), "role=dremio-cluster-pod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := listActionRestrictions(t, fc); got != "role=dremio-cluster-pod" {
		t.Errorf("expected label restriction %q, got %q", "role=dremio-cluster-pod", got)
	}

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() == "opensearch-0-main.txt" {
			t.Errorf("opensearch-0 log should not be collected when selector excludes it")
		}
	}
}

func TestGetPreviousLogsForRestartedPods_EmptySelector_ListsAllNamespacePods(t *testing.T) {
	dremio := makePod("dremio-master-0", map[string]string{"role": "dremio-cluster-pod"}, 1)
	other := makePod("opensearch-0", map[string]string{"app": "opensearch"}, 1)
	fc := fake.NewSimpleClientset(dremio, other)
	dir := t.TempDir()

	err := GetPreviousLogsForRestartedPods(&stubHook{ctx: context.Background()}, "test-ns", fc, &stubCS{dir: dir}, helpers.NewRealFileSystem(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := listActionRestrictions(t, fc); got != "" {
		t.Errorf("expected empty label restrictions, got %q", got)
	}
}

func TestGetPreviousLogsForRestartedPods_NonEmptySelector_FiltersPods(t *testing.T) {
	dremio := makePod("dremio-master-0", map[string]string{"role": "dremio-cluster-pod"}, 1)
	other := makePod("opensearch-0", map[string]string{"app": "opensearch"}, 1)
	fc := fake.NewSimpleClientset(dremio, other)
	dir := t.TempDir()

	err := GetPreviousLogsForRestartedPods(&stubHook{ctx: context.Background()}, "test-ns", fc, &stubCS{dir: dir}, helpers.NewRealFileSystem(), "role=dremio-cluster-pod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := listActionRestrictions(t, fc); got != "role=dremio-cluster-pod" {
		t.Errorf("expected label restriction %q, got %q", "role=dremio-cluster-pod", got)
	}

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() == "opensearch-0-main-previous.txt" {
			t.Errorf("opensearch-0 previous log should not be collected when selector excludes it")
		}
	}
}

type ExpectedJSON struct {
	APIVersion string
	Kind       string
	Value      int
}

func TestClusterCopyJSON(t *testing.T) {
	tmpDir := t.TempDir()
	// Read a file bytes
	testjson := filepath.Join("testdata", "test.json")
	actual, err := os.ReadFile(testjson)
	if err != nil {
		log.Printf("ERROR: when reading json file\n%v\nerror returned was:\n %v", actual, err)
	}

	afile := filepath.Join(tmpDir, "actual.json")
	// Write a file with the same bytes
	err = os.WriteFile(afile, actual, DirPerms)
	if err != nil {
		t.Errorf("ERROR: trying to write file %v, error was %v", afile, err)
	}

	expected := ExpectedJSON{
		APIVersion: "v1",
		Kind:       "Data",
		Value:      100,
	}

	// Create a model file
	efile := filepath.Join(tmpDir, "expected.json")
	edata, _ := json.MarshalIndent(expected, "", "    ")
	err = os.WriteFile(efile, edata, DirPerms)
	if err != nil {
		t.Errorf("ERROR: trying to write file %v, error was %v", efile, err)
	}
	// Read back files and compare
	acheck, err := os.ReadFile(afile)
	if err != nil {
		t.Errorf("ERROR: trying to read file %v, error was %v", afile, err)
	}
	echeck, err := os.ReadFile(efile)
	if err != nil {
		t.Errorf("ERROR: trying to read file %v, error was %v", efile, err)
	}

	expStr := strings.ReplaceAll((string(echeck)), "\r\n", "\n")
	actStr := strings.ReplaceAll((string(acheck)), "\r\n", "\n")

	if expStr != actStr {
		t.Errorf("\nERROR: \nexpected:\t%q\nactual:\t\t%q\n", expStr, actStr)
	}
}
