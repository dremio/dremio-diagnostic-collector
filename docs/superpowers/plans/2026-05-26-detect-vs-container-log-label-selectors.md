# Separate detection and container-log label selectors — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split `--label-selector` into `--detect-label-selector` (Dremio pod discovery, default `role=dremio-cluster-pod`) and `--container-log-label-selector` / `-l` (container log filter, default empty).

**Architecture:** Two cobra flags with distinct defaults and consumers. Detection path continues as today under the renamed flag. Container log functions (`GetClusterLogs`, `GetPreviousLogsForRestartedPods`) gain a `labelSelector string` parameter — empty → namespace-wide listing, non-empty → filtered listing. Function signatures switch from `*k8sapi.Clientset` to `kubernetes.Interface` to unblock real unit tests with `fake.NewSimpleClientset`. Clean break for `--label-selector` (v4 RC).

**Tech Stack:** Go, cobra, client-go, kubernetes fake client.

**Spec:** `docs/superpowers/specs/2026-05-26-detect-vs-container-log-label-selectors-design.md`

---

## Task 1: Refactor cluster.go signatures and add label selector filter (TDD)

**Files:**
- Modify: `cmd/root/collection/cluster.go` (signatures of `GetClusterLogs` and `GetPreviousLogsForRestartedPods`)
- Create: `cmd/root/collection/cluster_test.go`
- Modify: `cmd/root.go` (call sites at lines 436, 440, 523, 528 — pass `""` temporarily; the new flag is wired in Task 4)

- [ ] **Step 1: Write the failing tests**

Create `cmd/root/collection/cluster_test.go` with the following content:

```go
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

package collection

import (
	"context"
	"os"
	"path/filepath"
	"sort"
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
	want := []string{"dremio-master-0-main.txt", "opensearch-0-main.txt"}
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
		if filepath.Base(e.Name()) == "opensearch-0-main.txt" {
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
}
```

- [ ] **Step 2: Run tests to verify compilation failure**

Run: `go test ./cmd/root/collection/... -run TestGetClusterLogs -count=1`
Expected: build failure — `GetClusterLogs` and `GetPreviousLogsForRestartedPods` don't accept the test signature yet (wrong arity, concrete type vs interface).

- [ ] **Step 3: Update cluster.go signatures and behavior**

In `cmd/root/collection/cluster.go`:

1. Replace the import `k8sapi "k8s.io/client-go/kubernetes"` usage — the package is still imported but the parameter types change to `kubernetes.Interface`. Adjust the import alias if needed (likely already imported as `k8sapi`; refer to it as `k8sapi.Interface`).

2. Change `GetPreviousLogsForRestartedPods` signature from:
```go
func GetPreviousLogsForRestartedPods(hook shutdown.CancelHook, namespace string, clientSet *k8sapi.Clientset, cs CopyStrategy, ddfs helpers.Filesystem) error {
```
to:
```go
func GetPreviousLogsForRestartedPods(hook shutdown.CancelHook, namespace string, clientSet k8sapi.Interface, cs CopyStrategy, ddfs helpers.Filesystem, labelSelector string) error {
```

And replace the body's `pods.List(ctx, metav1.ListOptions{})` with:
```go
listOpts := metav1.ListOptions{}
if labelSelector != "" {
    listOpts.LabelSelector = labelSelector
}
pods, err := clientSet.CoreV1().Pods(namespace).List(ctx, listOpts)
```

Update the doc comment to reflect: `labelSelector` empty → all namespace pods, non-empty → filtered. Same separation-of-concerns note as before.

3. Change `GetClusterLogs` signature from:
```go
func GetClusterLogs(hook shutdown.CancelHook, namespace string, clientSet *k8sapi.Clientset, cs CopyStrategy, ddfs helpers.Filesystem) error {
```
to:
```go
func GetClusterLogs(hook shutdown.CancelHook, namespace string, clientSet k8sapi.Interface, cs CopyStrategy, ddfs helpers.Filesystem, labelSelector string) error {
```

Same listOpts pattern in the body, replacing the current `metav1.ListOptions{}`.

4. Update the internal helpers reachable from the two renamed functions:
   - `saveLogsFromPod(... c *k8sapi.Clientset ...)` → `c k8sapi.Interface`
   - `savePreviousLogsFromPod(... c *k8sapi.Clientset ...)` → `c k8sapi.Interface`
   - `copyContainerLog(... client *k8sapi.Clientset ...)` → `client k8sapi.Interface`

Do NOT touch `ClusterK8sExecute` or `clusterExecuteBytes` — they are out of scope for this change. The concrete `*k8sapi.Clientset` passed from root.go satisfies any unchanged signature.

Use Grep first to confirm every `*k8sapi.Clientset` reference in the file before editing — only update the three helpers listed above.

- [ ] **Step 4: Update call sites in cmd/root.go**

In `cmd/root.go`, update the four call sites to pass `""` for the new `labelSelector` parameter. The new flag wiring comes in Task 4 — this step just makes the project compile.

Line 436 area (local-k8s mode):
```go
if err := collection.GetPreviousLogsForRestartedPods(hook, detectedNS, clientSet, cs, collectionArgs.DDCfs, ""); err != nil {
```

Line 440 area (local-k8s mode):
```go
if err := collection.GetClusterLogs(hook, detectedNS, clientSet, cs, collectionArgs.DDCfs, ""); err != nil {
```

Line 523 area (standard k8s mode):
```go
err = collection.GetPreviousLogsForRestartedPods(hook, kubeArgs.Namespace, clientSet, cs, collectionArgs.DDCfs, "")
```

Line 528 area (standard k8s mode):
```go
err = collection.GetClusterLogs(hook, kubeArgs.Namespace, clientSet, cs, collectionArgs.DDCfs, "")
```

`ClusterK8sExecute` calls (lines 433 and 519 area) need no argument change — only the parameter type changes internally.

- [ ] **Step 5: Build and run new tests**

Run: `go build ./...`
Expected: clean build.

Run: `go test ./cmd/root/collection/... -run "TestGetClusterLogs|TestGetPreviousLogsForRestartedPods" -count=1 -v`
Expected: all four tests PASS.

- [ ] **Step 6: Run full collection package tests**

Run: `go test ./cmd/root/collection/... -count=1`
Expected: all tests PASS (existing tests should continue to pass since callers still pass `""` and behavior with empty selector is unchanged from the post-commit-585380a state).

- [ ] **Step 7: Commit**

```bash
git add cmd/root/collection/cluster.go cmd/root/collection/cluster_test.go cmd/root.go
git commit -m "Add labelSelector parameter to container log collection

GetClusterLogs and GetPreviousLogsForRestartedPods now accept a
labelSelector string parameter (empty = namespace-wide, non-empty =
filtered). Signatures switched from *k8sapi.Clientset to
kubernetes.Interface to enable real unit tests via fake clientset.
Callers in cmd/root.go pass empty string; the new
--container-log-label-selector flag is wired in a later commit."
```

---

## Task 2: Rename --label-selector flag to --detect-label-selector

**Files:**
- Modify: `cmd/root.go`

- [ ] **Step 1: Rename the package variable**

At `cmd/root.go:64`, rename:
```go
labelSelector  string
```
to:
```go
detectLabelSelector string
```

- [ ] **Step 2: Update all references to the variable in root.go**

Use Grep first to enumerate all references:
Run: `grep -n "labelSelector" cmd/root.go`

Then rename each occurrence of the bare `labelSelector` identifier to `detectLabelSelector`. Notable known sites (line numbers approximate, will shift):

- Line 475 area (UpdateCollectionArgs string): keep the human-readable text `"label selector"` but reference `kubeArgs.DetectLabelSelector` (Task 3 handles the field rename — for now, this still uses `kubeArgs.LabelSelector`).
- Line 975 area (GetClusters call): `kubernetes.GetClusters(k8sContext, detectLabelSelector, kubeconfigPath)`.
- Line 1224 (KubeArgs population): `LabelSelector: detectLabelSelector,` (field rename in Task 3).
- Line 1597 area (DiscoverPods call): `kubernetes.DiscoverPods(k8sContext, namespace, detectLabelSelector, kubeconfigPath)`.

Do NOT touch `kubeArgs.LabelSelector` references yet — those become `kubeArgs.DetectLabelSelector` in Task 3.

- [ ] **Step 3: Rename the cobra flag registration**

At `cmd/root.go:1306`, change:
```go
K8sCmd.PersistentFlags().StringVarP(&labelSelector, "label-selector", "l", "role=dremio-cluster-pod", "select which pods to collect: follows kubernetes label syntax see https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#label-selectors")
```
to:
```go
K8sCmd.PersistentFlags().StringVar(&detectLabelSelector, "detect-label-selector", "role=dremio-cluster-pod", "label selector used to identify Dremio coordinator/executor pods for file streaming; follows kubernetes label syntax (https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#label-selectors)")
```

Note: `StringVar` not `StringVarP` (no short flag). The `-l` short form will be reattached to the new container-log flag in Task 4.

- [ ] **Step 4: Build**

Run: `go build ./...`
Expected: clean build.

- [ ] **Step 5: Run cmd tests**

Run: `go test ./cmd/... -short -count=1`
Expected: all PASS except the pre-existing `cmd/local/rockscollect` Windows-AV failure (unrelated).

- [ ] **Step 6: Commit**

```bash
git add cmd/root.go
git commit -m "Rename --label-selector to --detect-label-selector

Removes the short form -l (will be reattached to a new
container-log filter flag in a later commit). Variable
labelSelector renamed to detectLabelSelector. Help text updated to
state the flag's specific purpose: Dremio pod detection."
```

---

## Task 3: Rename KubeArgs.LabelSelector and internal fields to DetectLabelSelector

**Files:**
- Modify: `cmd/root/kubernetes/kubernetes.go`
- Modify: `cmd/root/kubectl/kubectl.go`
- Modify: `cmd/root/kubernetes/kubernetes_test.go`
- Modify: `cmd/root/kubectl/kubectl_test.go`
- Modify: `cmd/root.go` (the `kubeArgs := kubernetes.KubeArgs{...}` literal and the `UpdateCollectionArgs` line)

- [ ] **Step 1: Rename the exported struct field**

In `cmd/root/kubernetes/kubernetes.go:54`, rename:
```go
LabelSelector  string
```
to:
```go
DetectLabelSelector  string
```

- [ ] **Step 2: Rename the internal field on KubeCtlAPIActions**

In `cmd/root/kubernetes/kubernetes.go`:

Line 69 (constructor body):
```go
labelSelector:  kubeArgs.LabelSelector,
```
becomes:
```go
detectLabelSelector:  kubeArgs.DetectLabelSelector,
```

Line 135 (struct field):
```go
labelSelector  string
```
becomes:
```go
detectLabelSelector  string
```

Line 459 and line 564 (usages of `c.labelSelector`) — rename to `c.detectLabelSelector`.

- [ ] **Step 3: Update kubectl.go**

In `cmd/root/kubectl/kubectl.go`:

Line 70 (constructor body):
```go
labelSelector:  kubeArgs.LabelSelector,
```
becomes:
```go
detectLabelSelector:  kubeArgs.DetectLabelSelector,
```

Line 82 (struct field):
```go
labelSelector  string
```
becomes:
```go
detectLabelSelector  string
```

Line 256 (usage):
```go
args = append(args, "get", "pods", "-n", c.namespace, "-l", c.labelSelector, ...)
```
becomes:
```go
args = append(args, "get", "pods", "-n", c.namespace, "-l", c.detectLabelSelector, ...)
```

- [ ] **Step 4: Update root.go references to kubeArgs.LabelSelector**

In `cmd/root.go`:

Line 475 area:
```go
consoleprint.UpdateCollectionArgs(fmt.Sprintf("namespace: '%v', label selector: '%v'", kubeArgs.Namespace, kubeArgs.LabelSelector))
```
becomes:
```go
consoleprint.UpdateCollectionArgs(fmt.Sprintf("namespace: '%v', detect-label-selector: '%v'", kubeArgs.Namespace, kubeArgs.DetectLabelSelector))
```

Line 1224 area:
```go
LabelSelector:  detectLabelSelector,
```
becomes:
```go
DetectLabelSelector:  detectLabelSelector,
```

- [ ] **Step 5: Update tests for renamed internal field**

In `cmd/root/kubernetes/kubernetes_test.go:248`:
```go
labelSelector: "app=dremio",
```
becomes:
```go
detectLabelSelector: "app=dremio",
```

In `cmd/root/kubectl/kubectl_test.go:78`:
```go
labelSelector: "role=dremio-pods",
```
becomes:
```go
detectLabelSelector: "role=dremio-pods",
```

In `cmd/root/kubectl/kubectl_test.go:305`:
```go
labelSelector: "app=dremio",
```
becomes:
```go
detectLabelSelector: "app=dremio",
```

- [ ] **Step 6: Build**

Run: `go build ./...`
Expected: clean build.

- [ ] **Step 7: Run tests**

Run: `go test ./cmd/... -short -count=1`
Expected: all PASS except pre-existing `cmd/local/rockscollect` flake.

- [ ] **Step 8: Commit**

```bash
git add cmd/root/kubernetes/kubernetes.go cmd/root/kubernetes/kubernetes_test.go cmd/root/kubectl/kubectl.go cmd/root/kubectl/kubectl_test.go cmd/root.go
git commit -m "Rename KubeArgs.LabelSelector to DetectLabelSelector

Mechanical rename across kubernetes.go, kubectl.go, and their tests.
Aligns the struct field name with the new flag --detect-label-selector
and makes the field's single purpose (Dremio pod detection) explicit
at every usage site. Internal labelSelector fields on KubeCtlAPIActions
and CliK8sActions renamed to detectLabelSelector for consistency."
```

---

## Task 4: Add --container-log-label-selector flag and wire it

**Files:**
- Modify: `cmd/root.go`

- [ ] **Step 1: Add the new package variable**

In `cmd/root.go` near line 64 (where `detectLabelSelector` now lives), add:
```go
containerLogLabelSelector string
```

- [ ] **Step 2: Register the new cobra flag**

In `cmd/root.go` immediately after the `--detect-label-selector` registration (around line 1306), add:
```go
K8sCmd.PersistentFlags().StringVarP(&containerLogLabelSelector, "container-log-label-selector", "l", "", "label selector to filter which pods' container logs are collected (default: empty = all pods in the namespace); follows kubernetes label syntax")
```

- [ ] **Step 3: Wire the new variable into the two standard-k8s call sites**

In `cmd/root.go` at lines 523 and 528 (the standard k8s mode block), change the empty-string arg to `containerLogLabelSelector`:

```go
err = collection.GetPreviousLogsForRestartedPods(hook, kubeArgs.Namespace, clientSet, cs, collectionArgs.DDCfs, containerLogLabelSelector)
```

```go
err = collection.GetClusterLogs(hook, kubeArgs.Namespace, clientSet, cs, collectionArgs.DDCfs, containerLogLabelSelector)
```

The local-k8s mode call sites at lines 436 and 440 keep passing `""` — the flag is on `K8sCmd` (remote k8s), and local-k8s preserves its existing behavior of no container-log filter (consistent with the existing assertion in `cmd/cli_generator_test.go:196`).

- [ ] **Step 4: Build**

Run: `go build ./...`
Expected: clean build.

- [ ] **Step 5: Manual smoke check of help text**

Run: `go run ./ collect k8s standard --help 2>&1 | grep -E "detect-label-selector|container-log-label-selector|label-selector"`
Expected: two lines — one for `--detect-label-selector` (no `-l` short), one for `-l, --container-log-label-selector`. No bare `--label-selector` entry.

- [ ] **Step 6: Run tests**

Run: `go test ./cmd/... -short -count=1`
Expected: all PASS except pre-existing rockscollect flake. The cli_generator_test at line 196 will FAIL because it asserts on the absence of `--label-selector` but the flag generation logic may now emit `--container-log-label-selector` for some modes. This is fixed in Task 5.

If the test passes, that's also fine (means the generated CLI for local-k8s mode legitimately doesn't include the new flag either).

- [ ] **Step 7: Commit**

```bash
git add cmd/root.go
git commit -m "Add --container-log-label-selector flag (-l)

New flag with empty default filters which pods' container logs are
collected by GetClusterLogs and GetPreviousLogsForRestartedPods.
Empty (default) preserves the post-#335 namespace-wide behavior;
non-empty scopes container log collection to matching pods.
Wired into standard k8s mode; local-k8s mode continues to pass
empty string."
```

---

## Task 5: Update cli_generator_test assertions

**Files:**
- Modify: `cmd/cli_generator_test.go` (around line 196)

- [ ] **Step 1: Read the current assertion**

Run: `grep -n -A3 "should not include.*label-selector" cmd/cli_generator_test.go`

The current test asserts local-k8s mode does not include `--label-selector`. Confirm the surrounding test name and structure.

- [ ] **Step 2: Update the assertion**

Replace the current assertion (around lines 196-198):

```go
if strings.Contains(cmd, "--label-selector") {
    t.Error("local-k8s mode should not include --label-selector")
}
```

with:

```go
if strings.Contains(cmd, "--detect-label-selector") {
    t.Error("local-k8s mode should not include --detect-label-selector")
}
if strings.Contains(cmd, "--container-log-label-selector") {
    t.Error("local-k8s mode should not include --container-log-label-selector")
}
```

- [ ] **Step 3: Run the test**

Run: `go test ./cmd/... -run "Generator" -count=1 -v`
Expected: all generator-related tests PASS.

- [ ] **Step 4: Commit**

```bash
git add cmd/cli_generator_test.go
git commit -m "Update cli_generator_test for renamed label selector flags

Local-k8s mode should not include either of the new selector flags."
```

---

## Task 6: Update README

**Files:**
- Modify: `README.md` (around line 154)

- [ ] **Step 1: Find the flag row**

Run: `grep -n "label-selector" README.md`
Expected: one match at line 154 in the flag-reference table.

- [ ] **Step 2: Replace the row**

Replace:
```
| `-l, --label-selector` | K8s label selector (default: `role=dremio-cluster-pod`) |
```

with:
```
| `--detect-label-selector` | K8s label selector to identify Dremio coordinator/executor pods (default: `role=dremio-cluster-pod`) |
| `-l, --container-log-label-selector` | K8s label selector to filter which pods' container logs are collected (default: empty = all namespace pods) |
```

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "README: document new --detect-label-selector and --container-log-label-selector"
```

---

## Task 7: Add CHANGELOG entry

**Files:**
- Modify: `CHANGELOG.md`

- [ ] **Step 1: Locate the next unreleased section**

Open `CHANGELOG.md`. Identify the section for the next release (or the top of the file if there's an unreleased section). Recent v4 entries should be near the top.

- [ ] **Step 2: Add a Breaking-changes entry**

Add the following block under the next release header (or create an "Unreleased" section if none exists):

```markdown
### Breaking changes

* `--label-selector` has been split into two flags with single, distinct purposes:
  * `--detect-label-selector` (no short form, default `role=dremio-cluster-pod`) — identifies Dremio coordinator/executor pods for file streaming.
  * `-l, --container-log-label-selector` (default empty) — filters which pods' container logs end up in `kubernetes/container-logs/`. Empty means all pods in the namespace, restoring the v3 behavior of capturing ecosystem pods (catalog-server, opensearch, mongodb, nats, operators) that issue #335 reported missing.
  
  Anyone passing `--label-selector` will see cobra's unknown-flag error. Migration:
  * To scope which Dremio pods get their files streamed: `--detect-label-selector <selector>`.
  * To scope which pods' container logs are collected: `-l <selector>` (same short form as before, but new semantics).
  * For the v3-style "scope everything to one pod" behavior, pass both flags with the same value.
```

- [ ] **Step 3: Commit**

```bash
git add CHANGELOG.md
git commit -m "CHANGELOG: document --label-selector split into detect + container-log flags"
```

---

## Final verification

After all tasks are committed:

- [ ] **Step 1: Full build**

Run: `go build ./...`
Expected: clean.

- [ ] **Step 2: Full unit test suite**

Run: `go test ./cmd/... -short -count=1`
Expected: all PASS except `cmd/local/rockscollect` (pre-existing Windows-AV flake).

- [ ] **Step 3: Confirm both flags appear in help**

Run: `go run ./ collect k8s standard --help`
Expected: `--detect-label-selector` and `-l, --container-log-label-selector` both listed; no bare `--label-selector`.

- [ ] **Step 4: Run vet**

Run: `go vet ./...`
Expected: clean.

- [ ] **Step 5: Confirm acceptance criteria from spec**

Walk the spec's "Acceptance criteria" section and confirm each item:
- Build green ✓
- New unit tests pass ✓
- `go test ./cmd/...` green ✓
- Default invocation (no `-l`) lists namespace-wide → verified by `TestGetClusterLogs_EmptySelector_ListsAllNamespacePods`
- `-l role=dremio-cluster-pod` filters → verified by `TestGetClusterLogs_NonEmptySelector_FiltersPods`
- `--label-selector` is unknown → cobra emits error (manual verification or add a test if desired)
- README/CHANGELOG updated ✓
