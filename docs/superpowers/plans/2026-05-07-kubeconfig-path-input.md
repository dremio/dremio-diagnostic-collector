# Kubeconfig Path Input Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When DDC's TUI cannot auto-detect a Kubernetes config, prompt the user for a path; expose a `--kubeconfig` CLI flag for non-interactive use; emit it in the generated CLI preview when the user chose a non-default path. Apply to k8s standard, k8s diagnosis, and local-k8s.

**Architecture:** Single new path-resolution helper in `cmd/root/kubernetes/` with precedence (explicit → env → home). Threaded as an extra parameter through `GetClientset`, `ListContexts`, `GetClusters`, `DiscoverPods` and as a new field on `kubernetes.KubeArgs`. New `huh` input step in `cmd/root.go` with two-layer validation (sync `Validate` for file/parse/contexts, async spinner for connectivity). Local-k8s gets a flag-only fallback (no TUI input). CLI generator gets a new field, emitted only when the path was user-supplied.

**Tech Stack:** Go 1.24, Cobra (CLI), charmbracelet/huh (TUI), k8s.io/client-go (kubeconfig parsing).

**Commit strategy:** Per the user's request, **DO NOT make intermediate commits**. Accumulate all changes in the working tree across tasks. The final task creates a single squashed commit covering the spec, this plan, and all implementation work. Each task uses `go build -o bin/ddc.exe .` plus `go test -short ./...` as its checkpoint instead of `git commit`.

**Test runner:** Use `go test -short ./...` on Windows (CGO is unavailable by default; `-race` requires CGO). On Linux/macOS, prefer `go test -race -short ./...`.

---

## Reference: spec

Full design spec: `docs/superpowers/specs/2026-05-07-kubeconfig-path-input-design.md`. Read it before starting.

## Reference: file map

```
EDIT  pkg/dirs/dirs.go                                    — add ExpandTilde
EDIT  pkg/dirs/dirs_test.go                               — add TestExpandTilde
EDIT  cmd/root/kubernetes/kubernetes.go                   — resolveKubeconfigPath, signatures, KubeArgs.KubeconfigPath, VerifyConnectivity
EDIT  cmd/root/kubernetes/kubernetes_test.go              — TestResolveKubeconfigPath, TestGetClientset_ExplicitPathOverridesEnv, TestVerifyConnectivity_*
EDIT  cmd/root/kubernetes/list_contexts_test.go           — update calls + add explicit-path test
NEW   cmd/configui/kubeconfig.go                          — ValidateKubeconfigPath
NEW   cmd/configui/kubeconfig_test.go                     — table-driven validation tests
EDIT  cmd/configui/configui.go                            — StandardConfig.Kubeconfig, DiagnosisConfig.Kubeconfig, appendTransportAndPathFlags emits --kubeconfig
EDIT  cmd/configui/configui_test.go                       — emit-position + emit-omitted tests for buildStandardCLICommand
EDIT  cmd/cli_generator.go                                — Kubeconfig field + emit logic (parity with configui builder, even though no production caller today)
EDIT  cmd/cli_generator_test.go                           — three new test cases
EDIT  cmd/root.go                                         — global var, flag registrations, TUI input step (promptKubeconfigPath), plumbing, local-k8s fallback chain (with readNamespaceFromKubeconfig helper), Standard/DiagnosisConfig.Kubeconfig population
EDIT  CHANGELOG.md                                        — one-line entry
```

---

## Task 1: Add `ExpandTilde` helper to `pkg/dirs`

**Files:**
- Modify: `pkg/dirs/dirs.go` (add new function)
- Modify: `pkg/dirs/dirs_test.go` (add new test — file already exists)

**Why:** Two later consumers (`resolveKubeconfigPath` in the kubernetes package, `ValidateKubeconfigPath` in configui) need tilde expansion. Putting it in `pkg/dirs` keeps it shared without circular imports.

- [ ] **Step 1.1: Write the failing test**

Add this test function to the END of `pkg/dirs/dirs_test.go`:

```go
func TestExpandTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir failed: %v", err)
	}
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty string", "", ""},
		{"no tilde", "/etc/passwd", "/etc/passwd"},
		{"tilde alone", "~", home},
		{"tilde slash", "~/.kube/config", filepath.Join(home, ".kube", "config")},
		{"tilde backslash (windows-style)", `~\.kube\config`, filepath.Join(home, ".kube", "config")},
		{"tilde user (unsupported, untouched)", "~someone/file", "~someone/file"},
		{"tilde in middle (untouched)", "/foo/~bar", "/foo/~bar"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ExpandTilde(tc.in)
			if got != tc.want {
				t.Errorf("ExpandTilde(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
```

You will also need to add `"path/filepath"` and `"testing"` to the imports if not already present (`os` is already imported).

- [ ] **Step 1.2: Run the test to verify it fails**

```
go test -short ./pkg/dirs/...
```

Expected: FAIL with `undefined: ExpandTilde` or similar.

- [ ] **Step 1.3: Implement `ExpandTilde` in `pkg/dirs/dirs.go`**

Append this function to `pkg/dirs/dirs.go`:

```go
// ExpandTilde replaces a leading "~" or "~/" / "~\" token in path with
// the current user's home directory. Returns the input unchanged when
// no leading tilde token is present, when the home directory cannot be
// determined, or for "~user" forms (we don't resolve other users' homes).
//
// Examples:
//
//	ExpandTilde("~/.kube/config")  -> "/Users/foo/.kube/config"
//	ExpandTilde(`~\.kube\config`)  -> "C:\\Users\\foo\\.kube\\config"
//	ExpandTilde("~")               -> "/Users/foo"
//	ExpandTilde("/etc/passwd")     -> "/etc/passwd"
//	ExpandTilde("~someone/file")   -> "~someone/file"   (untouched)
func ExpandTilde(path string) string {
	if path == "" || path[0] != '~' {
		return path
	}
	// "~" alone or "~/..." or "~\..."
	if len(path) == 1 || path[1] == '/' || path[1] == '\\' {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		if len(path) == 1 {
			return home
		}
		// Strip the leading "~" then join — filepath.Join normalises separators.
		return filepath.Join(home, path[2:])
	}
	// "~user/..." — leave alone.
	return path
}
```

Add `"path/filepath"` to the import block in `pkg/dirs/dirs.go` if not already present.

- [ ] **Step 1.4: Run the test to verify it passes**

```
go test -short ./pkg/dirs/...
```

Expected: PASS for all `TestExpandTilde/*` subtests.

- [ ] **Step 1.5: Build and run full unit test suite as checkpoint**

```
go build -o bin/ddc.exe .
go test -short ./...
```

Expected: build succeeds, all tests pass. **Do not commit.**

---

## Task 2: Add `resolveKubeconfigPath` helper to `cmd/root/kubernetes/`

**Files:**
- Modify: `cmd/root/kubernetes/kubernetes.go` (add new function, no signature changes yet)
- Modify: `cmd/root/kubernetes/kubernetes_test.go` (add `TestResolveKubeconfigPath`)

**Why:** Centralised precedence (explicit → env → home) used by all later signature changes. Tilde expansion lives here so every call site benefits.

- [ ] **Step 2.1: Write the failing test**

Append this test to `cmd/root/kubernetes/kubernetes_test.go`:

```go
func TestResolveKubeconfigPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir failed: %v", err)
	}
	defaultHomePath := filepath.Join(home, ".kube", "config")

	tests := []struct {
		name      string
		explicit  string
		envValue  string
		envSet    bool // distinguishes env-unset from env-empty
		want      string
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
```

Add the imports `"os"`, `"path/filepath"`, `"testing"` if not already present in the test file.

- [ ] **Step 2.2: Run the test to verify it fails**

```
go test -short -run TestResolveKubeconfigPath ./cmd/root/kubernetes/...
```

Expected: FAIL with `undefined: resolveKubeconfigPath`.

- [ ] **Step 2.3: Implement `resolveKubeconfigPath` in `cmd/root/kubernetes/kubernetes.go`**

Append this function near the bottom of `cmd/root/kubernetes/kubernetes.go` (after `GetClusters` is fine):

```go
// resolveKubeconfigPath returns the effective kubeconfig file path with
// precedence:
//
//  1. explicit (e.g. --kubeconfig flag or TUI input), tilde-expanded
//  2. $KUBECONFIG env var, tilde-expanded
//  3. $HOME/.kube/config
//
// Returns "" only if all three are unavailable (e.g. no home dir).
// Matches `kubectl --kubeconfig` precedence so flag overrides env.
func resolveKubeconfigPath(explicit string) string {
	if explicit != "" {
		return dirs.ExpandTilde(explicit)
	}
	if env := os.Getenv("KUBECONFIG"); env != "" {
		return dirs.ExpandTilde(env)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".kube", "config")
}
```

Add `"github.com/dremio/dremio-diagnostic-collector/v4/pkg/dirs"` to the import block of `cmd/root/kubernetes/kubernetes.go`. The other imports (`os`, `path/filepath`) are already present.

- [ ] **Step 2.4: Run the test to verify it passes**

```
go test -short -run TestResolveKubeconfigPath ./cmd/root/kubernetes/...
```

Expected: PASS for all subtests.

- [ ] **Step 2.5: Build and full-test checkpoint**

```
go build -o bin/ddc.exe .
go test -short ./...
```

Expected: build succeeds, all tests pass. **Do not commit.**

---

## Task 3: Refactor `ListContexts()` → `ListContexts(kubeconfigPath string)`

**Files:**
- Modify: `cmd/root/kubernetes/kubernetes.go` (function signature + body)
- Modify: `cmd/root/kubernetes/list_contexts_test.go` (update existing tests + add new explicit-path test)
- Modify: `cmd/root.go:897` (single call site)

**Why:** Allow the TUI to re-enumerate contexts from a user-supplied kubeconfig file after the input step.

- [ ] **Step 3.1: Update existing tests to call with explicit empty string**

Edit `cmd/root/kubernetes/list_contexts_test.go`. There are three calls to `ListContexts()` at lines 63, 89, 124. Change all three to:

```go
contexts, currentContext, err := ListContexts("")
```

(Empty string means "use $KUBECONFIG env / home", preserving the original behavior of those tests.)

- [ ] **Step 3.2: Add a new test `TestListContexts_ExplicitPath`**

Append to `cmd/root/kubernetes/list_contexts_test.go`:

```go
func TestListContexts_ExplicitPath(t *testing.T) {
	kubeconfig := `apiVersion: v1
kind: Config
current-context: explicit-ctx
clusters:
- cluster:
    server: https://explicit.example.com
  name: explicit-cluster
contexts:
- context:
    cluster: explicit-cluster
    user: explicit-user
  name: explicit-ctx
users:
- name: explicit-user
`
	tmpDir := t.TempDir()
	explicitPath := filepath.Join(tmpDir, "explicit-config")
	if err := os.WriteFile(explicitPath, []byte(kubeconfig), 0600); err != nil {
		t.Fatalf("write explicit kubeconfig: %v", err)
	}

	// Set KUBECONFIG to a different (nonexistent) path to prove the explicit
	// path takes precedence.
	t.Setenv("KUBECONFIG", filepath.Join(tmpDir, "does-not-exist"))

	contexts, currentContext, err := ListContexts(explicitPath)
	if err != nil {
		t.Fatalf("ListContexts(explicitPath) returned error: %v", err)
	}
	if currentContext != "explicit-ctx" {
		t.Errorf("currentContext = %q, want %q", currentContext, "explicit-ctx")
	}
	if len(contexts) != 1 || contexts[0] != "explicit-ctx" {
		t.Errorf("contexts = %v, want [explicit-ctx]", contexts)
	}
}
```

- [ ] **Step 3.3: Run the new test to verify it fails**

```
go test -short -run TestListContexts_ExplicitPath ./cmd/root/kubernetes/...
```

Expected: FAIL — either won't compile (existing tests now use the new signature) or fails because `ListContexts` doesn't accept a parameter yet.

- [ ] **Step 3.4: Update `ListContexts` in `cmd/root/kubernetes/kubernetes.go`**

Replace the existing function (currently at line 743 — find the comment `// ListContexts parses the kubeconfig and returns all context names`) with:

```go
// ListContexts parses the kubeconfig and returns all context names (sorted)
// and the current-context. If kubeconfigPath is non-empty, it is used as
// the explicit kubeconfig source (overriding $KUBECONFIG and ~/.kube/config).
// If no kubeconfig file exists (e.g. running in-cluster), it returns
// (nil, "", nil) so the caller can skip the context picker.
func ListContexts(kubeconfigPath string) (contexts []string, currentContext string, err error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if resolved := resolveKubeconfigPath(kubeconfigPath); resolved != "" {
		loadingRules.ExplicitPath = resolved
	}
	config, err := loadingRules.Load()
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", nil
		}
		// If the default kubeconfig path simply doesn't exist, Load returns
		// a not-found error — treat it as a graceful skip.
		if strings.Contains(err.Error(), "no such file or directory") ||
			strings.Contains(err.Error(), "The system cannot find the file specified") ||
			strings.Contains(err.Error(), "The system cannot find the path specified") {
			return nil, "", nil
		}
		return nil, "", fmt.Errorf("failed to load kubeconfig: %w", err)
	}
	for name := range config.Contexts {
		contexts = append(contexts, name)
	}
	sort.Strings(contexts)
	return contexts, config.CurrentContext, nil
}
```

- [ ] **Step 3.5: Update the single call site in `cmd/root.go`**

Find this line (around `cmd/root.go:897`):

```go
contexts, currentCtx, ctxErr := kubernetes.ListContexts()
```

Change to:

```go
contexts, currentCtx, ctxErr := kubernetes.ListContexts(kubeconfigPath)
```

(Note: `kubeconfigPath` is a global that will be declared in Task 9. For now this code won't compile until Task 9. That's OK — we'll build at the checkpoint, expect a known compile error here, and proceed. Alternatively, you can defer this single edit until Task 9. **Defer it.** Leave `cmd/root.go:897` as `kubernetes.ListContexts()` for now — but note: this means `cmd/root.go` won't compile yet because the signature changed. To keep the build green between tasks, change line 897 to a placeholder for the moment:

```go
contexts, currentCtx, ctxErr := kubernetes.ListContexts("")
```

This passes empty string until Task 10 wires the global through.

- [ ] **Step 3.6: Run tests to verify they pass**

```
go test -short ./cmd/root/kubernetes/...
go test -short ./cmd/...
```

Expected: PASS.

- [ ] **Step 3.7: Build checkpoint**

```
go build -o bin/ddc.exe .
go test -short ./...
```

Expected: build succeeds, all tests pass. **Do not commit.**

---

## Task 4: Refactor `GetClientset(ctx)` → `GetClientset(ctx, kubeconfigPath)`

**Files:**
- Modify: `cmd/root/kubernetes/kubernetes.go` (signature + internal callers `NewK8sAPI`, `DiscoverPods`, `GetClusters`)
- Modify: `cmd/root/kubernetes/kubernetes_test.go` (add `TestGetClientset_ExplicitPathOverridesEnv`)
- Modify: `cmd/root.go:490` (call site, will use empty placeholder; wired for real in Task 10)

**Why:** This is the core function — every other helper funnels through it. Making it accept an explicit path is what enables flag-driven and TUI-driven kubeconfig selection.

- [ ] **Step 4.1: Write the failing test**

Append this test to `cmd/root/kubernetes/kubernetes_test.go`. It uses two distinct kubeconfig files with different cluster servers, so we can prove which one was used by inspecting the resulting `*rest.Config`:

```go
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
```

- [ ] **Step 4.2: Run the test to verify it fails**

```
go test -short -run TestGetClientset_ExplicitPathOverridesEnv ./cmd/root/kubernetes/...
```

Expected: FAIL — `GetClientset` only takes one argument right now.

- [ ] **Step 4.3: Update `GetClientset` signature and body**

In `cmd/root/kubernetes/kubernetes.go`, replace the existing `GetClientset` function (at line 79) with:

```go
// GetClientset returns a clientset and rest.Config using the supplied
// kubeconfigPath if non-empty (with precedence: explicit → $KUBECONFIG →
// ~/.kube/config). Falls back to in-cluster config when no kubeconfig
// file exists.
func GetClientset(k8sContext, kubeconfigPath string) (*kubernetes.Clientset, *rest.Config, error) {
	resolved := resolveKubeconfigPath(kubeconfigPath)
	var config *rest.Config
	if resolved == "" {
		// No path determinable — try in-cluster.
		var err error
		config, err = rest.InClusterConfig()
		if err != nil {
			return nil, nil, err
		}
	} else if _, err := os.Stat(resolved); err != nil { // #nosec G703 -- resolved is user-supplied kubeconfig path
		// File does not exist — fall back to in-cluster.
		config, err = rest.InClusterConfig()
		if err != nil {
			return nil, nil, err
		}
	} else {
		clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: resolved},
			&clientcmd.ConfigOverrides{CurrentContext: k8sContext},
		)
		if k8sContext == "" {
			startConfig, err := clientConfig.ConfigAccess().GetStartingConfig()
			if err != nil {
				return nil, nil, err
			}
			simplelog.Infof("current kubernetes context is detected as %v", startConfig.CurrentContext)
		} else {
			simplelog.Infof("using kubernetes context of %v", k8sContext)
		}
		var err error
		config, err = clientConfig.ClientConfig()
		if err != nil {
			return nil, nil, err
		}
	}
	simplelog.Debugf("connection to kubernetes API: %v", config.Host)
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, nil, err
	}
	return clientset, config, nil
}
```

- [ ] **Step 4.4: Update internal callers in `cmd/root/kubernetes/kubernetes.go`**

Three internal callers of `GetClientset`. Locate and update each:

1. `NewK8sAPI` body (around line 59):
   ```go
   clientset, config, err := GetClientset(kubeArgs.K8SContext)
   ```
   Change to:
   ```go
   clientset, config, err := GetClientset(kubeArgs.K8SContext, kubeArgs.KubeconfigPath)
   ```
   (`KubeconfigPath` field will be added in Task 6. Until then, this won't compile — see Step 4.5.)

2. `DiscoverPods` body (around line 600):
   ```go
   clientset, _, err := GetClientset(k8sContext)
   ```
   Will be updated as part of Task 5 (DiscoverPods signature change). For now, change to:
   ```go
   clientset, _, err := GetClientset(k8sContext, "")
   ```

3. `GetClusters` body (around line 767):
   ```go
   clientset, _, err := GetClientset(k8sContext)
   ```
   Will be updated as part of Task 5 (GetClusters signature change). For now, change to:
   ```go
   clientset, _, err := GetClientset(k8sContext, "")
   ```

- [ ] **Step 4.5: Add `KubeconfigPath` field to `KubeArgs` (early to keep build green)**

Locate the `KubeArgs` struct in `cmd/root/kubernetes/kubernetes.go` (around line 50):

```go
type KubeArgs struct {
	Namespace     string
	K8SContext    string
	LabelSelector string
}
```

Change to:

```go
type KubeArgs struct {
	Namespace      string
	K8SContext     string
	LabelSelector  string
	KubeconfigPath string
}
```

(Task 6 plumbs the value into this field at construction time. The new field defaults to `""` and the build stays green.)

- [ ] **Step 4.6: Update the single external call site in `cmd/root.go:490`**

Find:

```go
clientSet, _, err := kubernetes.GetClientset(k8sContext)
```

Change to:

```go
clientSet, _, err := kubernetes.GetClientset(k8sContext, "")
```

(Will be wired to `kubeconfigPath` global in Task 10.)

- [ ] **Step 4.7: Run tests**

```
go test -short ./cmd/root/kubernetes/...
```

Expected: PASS, including the new `TestGetClientset_ExplicitPathOverridesEnv`.

- [ ] **Step 4.8: Build checkpoint**

```
go build -o bin/ddc.exe .
go test -short ./...
```

Expected: build succeeds, all tests pass. **Do not commit.**

---

## Task 5: Refactor `GetClusters` and `DiscoverPods` signatures

**Files:**
- Modify: `cmd/root/kubernetes/kubernetes.go` (signatures + bodies)
- Modify: `cmd/root.go` (two call sites: lines 929 and 1521; use empty string placeholders)

**Why:** Both helpers are called from the TUI flow with `kubeconfigPath` in scope. Threading the parameter through them lets a user-supplied path actually drive the namespace listing and pod discovery.

- [ ] **Step 5.1: Update `GetClusters` signature**

In `cmd/root/kubernetes/kubernetes.go` (around line 766), replace:

```go
func GetClusters(k8sContext, labelSelector string) ([]string, error) {
	clientset, _, err := GetClientset(k8sContext, "")
```

with:

```go
func GetClusters(k8sContext, labelSelector, kubeconfigPath string) ([]string, error) {
	clientset, _, err := GetClientset(k8sContext, kubeconfigPath)
```

- [ ] **Step 5.2: Update `DiscoverPods` signature**

In `cmd/root/kubernetes/kubernetes.go` (around line 599), replace:

```go
func DiscoverPods(k8sContext, namespace, labelSelector string) (coordinators []string, executors []string, err error) {
	clientset, _, err := GetClientset(k8sContext, "")
```

with:

```go
func DiscoverPods(k8sContext, namespace, labelSelector, kubeconfigPath string) (coordinators []string, executors []string, err error) {
	clientset, _, err := GetClientset(k8sContext, kubeconfigPath)
```

- [ ] **Step 5.3: Update call sites in `cmd/root.go`**

Two call sites — for now pass empty string placeholders (Task 10 wires the real global through):

Line 929 area:

```go
clustersToList, clusterErr = kubernetes.GetClusters(k8sContext, labelSelector)
```

→

```go
clustersToList, clusterErr = kubernetes.GetClusters(k8sContext, labelSelector, "")
```

Line 1521 area:

```go
discoveredCoordinators, discoveredExecutors, discoverErr = kubernetes.DiscoverPods(k8sContext, namespace, labelSelector)
```

→

```go
discoveredCoordinators, discoveredExecutors, discoverErr = kubernetes.DiscoverPods(k8sContext, namespace, labelSelector, "")
```

- [ ] **Step 5.4: Build checkpoint**

```
go build -o bin/ddc.exe .
go test -short ./...
```

Expected: build succeeds, all tests pass. **Do not commit.**

---

## Task 6: Add `VerifyConnectivity` helper

**Files:**
- Modify: `cmd/root/kubernetes/kubernetes.go` (new exported function near `GetClusters`)
- Modify: `cmd/root/kubernetes/kubernetes_test.go` (new test using a fake apiserver via httptest)

**Why:** The TUI input step (Task 12) calls this in a spinner after the form returns to verify the supplied kubeconfig actually reaches a cluster, before letting the user advance. Cheap API call (namespace list, no parsing of items needed beyond "did the call succeed").

- [ ] **Step 6.1: Write the failing test**

Append to `cmd/root/kubernetes/kubernetes_test.go`:

```go
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
```

Add imports `"net/http"`, `"net/http/httptest"`, `"strings"` to `kubernetes_test.go` if not already present.

- [ ] **Step 6.2: Run the test to verify it fails**

```
go test -short -run TestVerifyConnectivity ./cmd/root/kubernetes/...
```

Expected: FAIL with `undefined: VerifyConnectivity`.

- [ ] **Step 6.3: Implement `VerifyConnectivity`**

Append to `cmd/root/kubernetes/kubernetes.go` (after `GetClusters` is fine):

```go
// VerifyConnectivity issues a lightweight namespace-list call against the
// cluster identified by the given kubeconfigPath and k8sContext. Used by
// the TUI to confirm the user's kubeconfig actually reaches a cluster
// before moving on. The returned error wraps the underlying transport or
// auth error verbatim — callers should surface it to the user.
func VerifyConnectivity(kubeconfigPath, k8sContext string) error {
	clientset, _, err := GetClientset(k8sContext, kubeconfigPath)
	if err != nil {
		return fmt.Errorf("kubeconfig load: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := clientset.CoreV1().Namespaces().List(ctx, meta_v1.ListOptions{Limit: 1}); err != nil {
		return fmt.Errorf("cluster reachable check failed: %w", err)
	}
	return nil
}
```

The `time` import may need to be added to `kubernetes.go` (`context` and `meta_v1` are already present).

- [ ] **Step 6.4: Run the test to verify it passes**

```
go test -short -run TestVerifyConnectivity ./cmd/root/kubernetes/...
```

Expected: PASS.

- [ ] **Step 6.5: Build checkpoint**

```
go build -o bin/ddc.exe .
go test -short ./...
```

Expected: build succeeds, all tests pass. **Do not commit.**

---

## Task 7: Add `ValidateKubeconfigPath` to `cmd/configui/`

**Files:**
- Create: `cmd/configui/kubeconfig.go`
- Create: `cmd/configui/kubeconfig_test.go`

**Why:** Extracted from the `huh.Validate` callback so unit tests can cover it directly without driving a TUI form. Used by Task 12's input step.

- [ ] **Step 7.1: Write the failing test (file + tests at once)**

Create `cmd/configui/kubeconfig_test.go`:

```go
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
```

- [ ] **Step 7.2: Run the test to verify it fails**

```
go test -short ./cmd/configui/...
```

Expected: FAIL — `undefined: ValidateKubeconfigPath`.

- [ ] **Step 7.3: Implement `cmd/configui/kubeconfig.go`**

Create `cmd/configui/kubeconfig.go`:

```go
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
```

- [ ] **Step 7.4: Run the test to verify it passes**

```
go test -short ./cmd/configui/...
```

Expected: PASS for all subtests.

- [ ] **Step 7.5: Build checkpoint**

```
go build -o bin/ddc.exe .
go test -short ./...
```

Expected: build succeeds, all tests pass. **Do not commit.**

---

## Task 8: Add `Kubeconfig` field to `CLICommandConfig` + emit logic

**Files:**
- Modify: `cmd/cli_generator.go` (struct field + emit clause)
- Modify: `cmd/cli_generator_test.go` (three new test cases)

**Why:** The reproducible-CLI panel shown after the TUI must include `--kubeconfig=<path>` when (and only when) the user explicitly chose a non-default path. Position is **immediately after the mode word**, before any other flag including `--namespace`.

- [ ] **Step 8.1: Write the three failing tests**

Append to `cmd/cli_generator_test.go`:

```go
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
```

- [ ] **Step 8.2: Run the tests to verify they fail**

```
go test -short -run TestGenerateCLICommand_Kubeconfig ./cmd/...
```

Expected: FAIL — `Kubeconfig` field undefined.

- [ ] **Step 8.3: Add the field and emit logic to `cmd/cli_generator.go`**

In the `CLICommandConfig` struct, add `Kubeconfig string` between `Namespace` and `Coordinator`:

```go
type CLICommandConfig struct {
	Transport        string // "ssh", "k8s", "local", or "local-k8s"
	Mode             string
	Kubeconfig       string // K8s/local-k8s — emitted only if non-empty
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
```

In `GenerateCLICommand`, **insert the `--kubeconfig` emit immediately before the `--namespace` block** so it sits between the mode word and the existing `--namespace` flag in the output:

Find this block (currently right after `parts = append(parts, c.Mode)`):

```go
	// Transport flags — always included
	if c.Namespace != "" {
		parts = append(parts, fmt.Sprintf("--namespace=%s", c.Namespace))
	}
```

Change to:

```go
	// Transport flags — always included
	if c.Kubeconfig != "" {
		parts = append(parts, fmt.Sprintf("--kubeconfig=%s", c.Kubeconfig))
	}
	if c.Namespace != "" {
		parts = append(parts, fmt.Sprintf("--namespace=%s", c.Namespace))
	}
```

- [ ] **Step 8.4: Run the tests to verify they pass**

```
go test -short -run TestGenerateCLICommand ./cmd/...
```

Expected: PASS for all `TestGenerateCLICommand_*` tests including the existing ones.

- [ ] **Step 8.5: Build checkpoint**

```
go build -o bin/ddc.exe .
go test -short ./...
```

Expected: build succeeds, all tests pass. **Do not commit.**

---

## Task 9: Register `--kubeconfig` flag and declare the global

**Files:**
- Modify: `cmd/root.go` (declare `kubeconfigPath` global, register flag on `K8sCmd` and `LocalK8sCmd`)

**Why:** Makes `--kubeconfig` available on the command line for non-interactive use. The TUI input step (Task 12) writes into the same global.

- [ ] **Step 9.1: Declare the global variable**

In `cmd/root.go`, find the var block around line 79 (search for `// v4 CLI flags`):

```go
var (
	sudoUser              string
	namespace             string
	k8sContext            string
	disableFreeSpaceCheck bool
	enableKubeCtl         bool
	collectionMode        collects.CollectionMode
	transportCmd          string // "ssh", "k8s", "local", or "local-k8s", set from command path or TUI
	cliAuthToken          string
	pid                   string
	collectionThreads     int
	// v4 CLI flags
	skipVersionCheck  bool
	...
)
```

Add `kubeconfigPath string` next to `k8sContext`:

```go
var (
	sudoUser              string
	namespace             string
	k8sContext            string
	kubeconfigPath        string
	disableFreeSpaceCheck bool
	...
```

- [ ] **Step 9.2: Register the flag on `K8sCmd` and `LocalK8sCmd`**

Find the K8s flag registration block in `cmd/root.go` (around line 1255, search for `// ── K8s transport flags`). After the existing `--context` line (at 1257), add a new `--kubeconfig` line:

```go
	K8sCmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "", "namespace to use for kubernetes pods")
	K8sCmd.PersistentFlags().StringVarP(&k8sContext, "context", "x", "", "context to use for kubernetes pods")
	K8sCmd.PersistentFlags().StringVar(&kubeconfigPath, "kubeconfig", "", "path to kubeconfig file (overrides $KUBECONFIG and ~/.kube/config)")
	K8sCmd.PersistentFlags().StringVarP(&labelSelector, "label-selector", "l", "role=dremio-cluster-pod", ...)
	...
```

Find the LocalK8sCmd flag block (around line 1268, search for `// ── Local-K8s transport flags`):

```go
	// ── Local-K8s transport flags — on LocalK8sCmd.PersistentFlags() ──
	LocalK8sCmd.PersistentFlags().StringVar(&dremioHome, "dremio-home", "/opt/dremio", "Dremio installation directory")
	LocalK8sCmd.PersistentFlags().StringVar(&localLogDir, "local-log-dir", "", "Log directory on this node (autodetected if not specified)")
```

Add a new line registering the same global on LocalK8sCmd:

```go
	// ── Local-K8s transport flags — on LocalK8sCmd.PersistentFlags() ──
	LocalK8sCmd.PersistentFlags().StringVar(&dremioHome, "dremio-home", "/opt/dremio", "Dremio installation directory")
	LocalK8sCmd.PersistentFlags().StringVar(&localLogDir, "local-log-dir", "", "Log directory on this node (autodetected if not specified)")
	LocalK8sCmd.PersistentFlags().StringVar(&kubeconfigPath, "kubeconfig", "", "path to kubeconfig file used when in-cluster config is unavailable")
```

(Same global; cobra is fine with the same `*string` registered against two parent commands because only one parent runs per invocation.)

- [ ] **Step 9.3: Build checkpoint**

```
go build -o bin/ddc.exe .
go test -short ./...
```

Expected: build succeeds, all tests pass. Sanity-check the flag is wired:

```
.\bin\ddc.exe collect k8s standard --help
```

Expected: `--kubeconfig` appears in the listed flags with the help text from Step 9.2.

```
.\bin\ddc.exe collect local-k8s standard --help
```

Expected: `--kubeconfig` appears here too. **Do not commit.**

---

## Task 10: Wire `kubeconfigPath` global through all four call sites

**Files:**
- Modify: `cmd/root.go` (replace empty-string placeholders from Tasks 3-5 with the real global, plus the `KubeArgs` field)

**Why:** Now that the global exists (Task 9), every call site that was using `""` as a placeholder switches to the real value.

- [ ] **Step 10.1: Update `cmd/root.go:490` (GetClientset for cluster collect)**

Find:

```go
clientSet, _, err := kubernetes.GetClientset(k8sContext, "")
```

Change to:

```go
clientSet, _, err := kubernetes.GetClientset(k8sContext, kubeconfigPath)
```

- [ ] **Step 10.2: Update `cmd/root.go:897` area (ListContexts for context picker)**

Find:

```go
contexts, currentCtx, ctxErr := kubernetes.ListContexts("")
```

Change to:

```go
contexts, currentCtx, ctxErr := kubernetes.ListContexts(kubeconfigPath)
```

- [ ] **Step 10.3: Update `cmd/root.go:929` area (GetClusters for namespace listing)**

Find:

```go
clustersToList, clusterErr = kubernetes.GetClusters(k8sContext, labelSelector, "")
```

Change to:

```go
clustersToList, clusterErr = kubernetes.GetClusters(k8sContext, labelSelector, kubeconfigPath)
```

- [ ] **Step 10.4: Update `cmd/root.go:1521` area (DiscoverPods for pod discovery)**

Find:

```go
discoveredCoordinators, discoveredExecutors, discoverErr = kubernetes.DiscoverPods(k8sContext, namespace, labelSelector, "")
```

Change to:

```go
discoveredCoordinators, discoveredExecutors, discoverErr = kubernetes.DiscoverPods(k8sContext, namespace, labelSelector, kubeconfigPath)
```

- [ ] **Step 10.5: Update `KubeArgs` construction at `cmd/root.go:1176`**

Find:

```go
		kubeArgs := kubernetes.KubeArgs{
			Namespace:     namespace,
			LabelSelector: labelSelector,
			K8SContext:    k8sContext,
		}
```

Change to:

```go
		kubeArgs := kubernetes.KubeArgs{
			Namespace:      namespace,
			LabelSelector:  labelSelector,
			K8SContext:     k8sContext,
			KubeconfigPath: kubeconfigPath,
		}
```

- [ ] **Step 10.6: Verify there are no other `KubeArgs{` constructions missing the field**

Search for all struct literal constructions:

```
grep -rn "KubeArgs{" --include="*.go" .
```

(or use the Grep tool with pattern `KubeArgs\{` and type `go`)

Expected: one match in production code (`cmd/root.go` around 1176, the one updated in Step 10.5). Tests may construct `KubeArgs{}` partially — that's fine, the new `KubeconfigPath` field zero-value is `""` and matches today's implicit behavior.

- [ ] **Step 10.7: Build checkpoint**

```
go build -o bin/ddc.exe .
go test -short ./...
```

Expected: build succeeds, all tests pass. **Do not commit.**

Sanity-check non-interactively:

```
.\bin\ddc.exe collect k8s standard --kubeconfig=C:\nonexistent\kubeconfig --namespace=foo
```

Expected: an error from kubernetes API client about the file or in-cluster config — proves the flag is being threaded all the way through to `GetClientset`.

---

## Task 11: Add the new TUI input step

**Files:**
- Modify: `cmd/root.go` (the `case "k8s":` branch around lines 895-952)

**Why:** This is the user-visible part of the feature. When `ListContexts(kubeconfigPath)` returns no contexts, prompt the user, validate inline, then run a connectivity probe with retry.

- [ ] **Step 11.1: Locate the K8s branch and study the current shape**

Open `cmd/root.go` and find:

```go
case "k8s":
    // Step 3b: K8s context selection (if multiple contexts available)
    contexts, currentCtx, ctxErr := kubernetes.ListContexts(kubeconfigPath)
    if ctxErr != nil {
        simplelog.Warningf("could not list kubeconfig contexts: %v", ctxErr)
    }
    if len(contexts) > 0 {
        // ... existing context picker
    }

    // Step 3c: K8s namespace selection
    var clustersToList []string
    var clusterErr error
    _ = spinner.New().
        Title("Detecting Kubernetes namespaces...").
        Action(func() {
            clustersToList, clusterErr = kubernetes.GetClusters(k8sContext, labelSelector, kubeconfigPath)
        }).
        Run()
    ...
```

Read it through once. The new step inserts between `ListContexts` and the context-picker block.

- [ ] **Step 11.2: Insert the kubeconfig-path input step**

Replace the block:

```go
case "k8s":
    // Step 3b: K8s context selection (if multiple contexts available)
    contexts, currentCtx, ctxErr := kubernetes.ListContexts(kubeconfigPath)
    if ctxErr != nil {
        simplelog.Warningf("could not list kubeconfig contexts: %v", ctxErr)
    }
    if len(contexts) > 0 {
```

with:

```go
case "k8s":
    // Step 3a (new): If no kubeconfig auto-detected (file missing or zero contexts),
    // prompt for an explicit path with inline validation and a connectivity probe.
    contexts, currentCtx, ctxErr := kubernetes.ListContexts(kubeconfigPath)
    if ctxErr != nil {
        simplelog.Warningf("could not list kubeconfig contexts: %v", ctxErr)
    }
    if len(contexts) == 0 {
        if err := promptKubeconfigPath(); err != nil {
            return err
        }
        // Re-enumerate contexts now that the user has supplied a path.
        contexts, currentCtx, ctxErr = kubernetes.ListContexts(kubeconfigPath)
        if ctxErr != nil {
            simplelog.Warningf("could not list kubeconfig contexts after input: %v", ctxErr)
        }
    }

    // Step 3b: K8s context selection (if multiple contexts available)
    if len(contexts) > 0 {
```

(Note: the body of the original `if len(contexts) > 0 {` block stays unchanged — we're simply preceding it with the new logic and turning the original conditional into the second branch in this if-else chain.)

- [ ] **Step 11.3: Implement `promptKubeconfigPath` helper**

Add this helper function near the bottom of `cmd/root.go` (after the existing helpers, e.g. near `runPathDiscovery` or `sshDefault`). Include all imports it needs (likely `runtime`, `huh`, `huh/spinner`, `configui`, `simplelog`):

```go
// promptKubeconfigPath shows a TUI input step asking the user for a
// kubeconfig file path, validates it inline (file/parse/contexts), and then
// runs a connectivity probe in a spinner. On connectivity failure the form
// is re-shown up to 2 retries (3 attempts total). On success, the global
// kubeconfigPath is set. Returns a non-nil error only on user cancel or
// final retry exhaustion.
func promptKubeconfigPath() error {
    placeholder := "/home/you/.kube/config"
    if runtime.GOOS == "windows" {
        placeholder = `C:\Users\you\.kube\config`
    }

    const maxAttempts = 3
    var entered string
    var lastErr error
    for attempt := 0; attempt < maxAttempts; attempt++ {
        if attempt > 0 && lastErr != nil {
            fmt.Println()
            fmt.Println("─────────────────────────────────────────────────────────────")
            fmt.Printf("unable to reach cluster: %v\n", lastErr)
            fmt.Println("─────────────────────────────────────────────────────────────")
            fmt.Println()
        }

        form := huh.NewForm(
            huh.NewGroup(
                huh.NewInput().
                    Title("Kubeconfig file path").
                    Description("No Kubernetes config auto-detected. Enter the path to your kubeconfig file.").
                    Placeholder(placeholder).
                    Value(&entered).
                    Validate(configui.ValidateKubeconfigPath),
            ),
        ).WithTheme(huh.ThemeCharm())
        if err := form.Run(); err != nil {
            return fmt.Errorf("kubeconfig input cancelled: %w", err)
        }

        // Inline validate has passed (file/parse/contexts). Now probe connectivity.
        candidate := dirs.ExpandTilde(entered)
        var probeErr error
        _ = spinner.New().
            Title("Verifying cluster connectivity...").
            Action(func() {
                probeErr = kubernetes.VerifyConnectivity(candidate, "")
            }).
            Run()
        if probeErr == nil {
            kubeconfigPath = candidate
            return nil
        }
        simplelog.Warningf("kubeconfig connectivity probe failed (attempt %d/%d): %v", attempt+1, maxAttempts, probeErr)
        lastErr = probeErr
    }
    return fmt.Errorf("could not reach a Kubernetes cluster with any of the supplied kubeconfig paths after %d attempts", maxAttempts)
}
```

Add these imports to `cmd/root.go`'s import block if not already present:
- `"runtime"`
- `"github.com/dremio/dremio-diagnostic-collector/v4/pkg/dirs"`

(`huh`, `huh/spinner`, `configui`, `kubernetes`, `simplelog`, `fmt` are already imported per the existing file.)

- [ ] **Step 11.4: Build checkpoint**

```
go build -o bin/ddc.exe .
go test -short ./...
```

Expected: build succeeds, all tests pass.

- [ ] **Step 11.5: Manual smoke test (TUI input flow)**

If you have no `~/.kube/config` and `$KUBECONFIG` is unset, run:

```
.\bin\ddc.exe
```

Then in the TUI: pick Diagnosis (or Standard), Kubernetes. Expected:
1. The new "Kubeconfig file path" input form shows immediately.
2. Typing a non-existent path → inline error "file not found".
3. Typing a malformed file → inline error "invalid kubeconfig: ...".
4. Typing a valid kubeconfig path that points to an unreachable cluster (e.g. fabricate one with `server: http://127.0.0.1:1`) → spinner appears, then error "unable to reach cluster: ..." and the form re-shows.
5. Typing a valid path to a working cluster → flow continues to context/namespace picker.

If you can't easily reproduce all four cases, at minimum validate (1) (form shows up) and (5) (flow continues for a real cluster). **Do not commit.**

---

## Task 12: Wire `Kubeconfig` field into the actual TUI CLI command preview

**Background — important correction to the spec assumption:**

The actual CLI preview shown at the end of the TUI is **not** built by `cmd/cli_generator.go`'s `GenerateCLICommand` (Task 8 changes that — but it has no production callers in the current code). The preview is built by `cmd/configui/configui.go` via:

- `appendTransportAndPathFlags` (lines ~752-780) — emits `--namespace`, `--context`, etc.
- `buildStandardCLICommand` (line ~783) — orchestrates the standard preview
- `buildDiagnosisCLICommand` (line ~854) — orchestrates the diagnosis preview

We add `--kubeconfig` to the actual flow (configui) AND keep `cli_generator.go` updated for parity (so the orphan infrastructure stays consistent).

**Files:**
- Modify: `cmd/configui/configui.go` (`StandardConfig` and `DiagnosisConfig` structs; `appendTransportAndPathFlags` signature + body)
- Modify: `cmd/configui/configui_test.go` (add test for the kubeconfig emit in the preview)
- Modify: `cmd/root.go` (populate `Kubeconfig` field when constructing `StandardConfig` / `DiagnosisConfig` for the config screens, OR ensure it flows in through whatever assignment pattern is used today)

- [ ] **Step 12.1: Add `Kubeconfig string` field to `StandardConfig` and `DiagnosisConfig`**

In `cmd/configui/configui.go`, locate the two struct definitions (search for `type StandardConfig struct` and `type DiagnosisConfig struct`). For each, add a `Kubeconfig string` field next to the existing `Namespace` field — keep the doc comments parallel to the `Namespace` comment. Example (the actual struct may have more fields; match the existing style):

```go
type StandardConfig struct {
    Transport   string
    Namespace   string
    Kubeconfig  string // empty unless user supplied a non-default kubeconfig path
    Coordinator string
    ...
}
```

Do the same for `DiagnosisConfig`.

- [ ] **Step 12.2: Add `kubeconfig` parameter to `appendTransportAndPathFlags` and emit it for k8s**

Replace the existing function (around line 754):

```go
func appendTransportAndPathFlags(parts []string, transport, namespace, k8sContext, coordinator, executors, sshUser, dremioHome, coordinatorLogDir, executorLogDir, confDir, rocksdbDir, cont string) []string {
	switch transport {
	case "k8s":
		line := fmt.Sprintf("  --namespace=%s", namespace)
		if k8sContext != "" {
			line += fmt.Sprintf(" --context=%s", k8sContext)
		}
		parts = append(parts, line+cont)
```

with:

```go
func appendTransportAndPathFlags(parts []string, transport, namespace, k8sContext, kubeconfig, coordinator, executors, sshUser, dremioHome, coordinatorLogDir, executorLogDir, confDir, rocksdbDir, cont string) []string {
	switch transport {
	case "k8s", "local-k8s":
		// --kubeconfig appears on its own line ABOVE --namespace, only if user supplied one.
		if kubeconfig != "" {
			parts = append(parts, fmt.Sprintf("  --kubeconfig=%s"+cont, kubeconfig))
		}
		if transport == "k8s" {
			line := fmt.Sprintf("  --namespace=%s", namespace)
			if k8sContext != "" {
				line += fmt.Sprintf(" --context=%s", k8sContext)
			}
			parts = append(parts, line+cont)
		}
```

(The `local-k8s` arm only emits `--kubeconfig` because local-k8s today has no `--namespace` flag and we agreed not to add one. Keep the rest of the switch — `case "ssh":`, `case "local":` — unchanged.)

- [ ] **Step 12.3: Update both callers of `appendTransportAndPathFlags`**

In `buildStandardCLICommand` (around line 788), find:

```go
parts = appendTransportAndPathFlags(parts, cfg.Transport, cfg.Namespace, cfg.K8sContext, cfg.Coordinator, cfg.Executors, cfg.SSHUser, cfg.DremioHome, cfg.CoordinatorLogDir, cfg.ExecutorLogDir, cfg.DremioConfDir, cfg.DremioRocksDBDir, cont)
```

Change to:

```go
parts = appendTransportAndPathFlags(parts, cfg.Transport, cfg.Namespace, cfg.K8sContext, cfg.Kubeconfig, cfg.Coordinator, cfg.Executors, cfg.SSHUser, cfg.DremioHome, cfg.CoordinatorLogDir, cfg.ExecutorLogDir, cfg.DremioConfDir, cfg.DremioRocksDBDir, cont)
```

Make the equivalent change in `buildDiagnosisCLICommand` (around line 859).

- [ ] **Step 12.4: Populate the field when constructing the configs**

`cmd/configui` is consumed from `cmd/root.go`. Search for where `StandardConfig{` or `DiagnosisConfig{` literals are constructed (both initial construction in cmd/root.go and any place inside configui.go that builds the displayed value).

```
grep -rn "StandardConfig{\|DiagnosisConfig{" --include="*.go" cmd/
```

For every literal:
- If the literal is in `cmd/root.go` (or anywhere `kubeconfigPath` global is in scope), add `Kubeconfig: kubeconfigPath,` to the literal.
- If the literal is inside `cmd/configui/` and builds an internal/intermediate value, look at how the existing `Namespace` field flows and mirror it with `Kubeconfig`.

Note: in many cases the structs are populated via direct field assignment (`cfg.Namespace = namespace`) rather than struct literals. Search for `cfg.Namespace` and `.Namespace =` patterns and add a parallel `cfg.Kubeconfig = kubeconfigPath` (or equivalent for DiagnosisConfig) right next to it.

- [ ] **Step 12.5: Add a test for the new emit behaviour**

Append to `cmd/configui/configui_test.go`:

```go
func TestBuildStandardCLICommand_KubeconfigEmittedAboveNamespace(t *testing.T) {
	cfg := &StandardConfig{
		Transport:        "k8s",
		Namespace:        "dremio",
		Kubeconfig:       "/home/user/.kube/config",
		CoordinatorLogDir: "/var/log/dremio",
		ExecutorLogDir:    "/var/log/dremio",
		DremioConfDir:     "/etc/dremio",
		DremioRocksDBDir:  "/var/dremio/db",
	}
	out := buildStandardCLICommand(cfg, 0, 0, 0, 0, 0)
	if !strings.Contains(out, "--kubeconfig=/home/user/.kube/config") {
		t.Errorf("expected --kubeconfig in output, got:\n%s", out)
	}
	kIdx := strings.Index(out, "--kubeconfig=")
	nIdx := strings.Index(out, "--namespace=")
	if kIdx < 0 || nIdx < 0 {
		t.Fatalf("missing flags:\n%s", out)
	}
	if kIdx > nIdx {
		t.Errorf("--kubeconfig must appear before --namespace; got:\n%s", out)
	}
}

func TestBuildStandardCLICommand_KubeconfigOmittedWhenEmpty(t *testing.T) {
	cfg := &StandardConfig{
		Transport:         "k8s",
		Namespace:         "dremio",
		CoordinatorLogDir: "/var/log/dremio",
		ExecutorLogDir:    "/var/log/dremio",
		DremioConfDir:     "/etc/dremio",
		DremioRocksDBDir:  "/var/dremio/db",
	}
	out := buildStandardCLICommand(cfg, 0, 0, 0, 0, 0)
	if strings.Contains(out, "--kubeconfig") {
		t.Errorf("expected NO --kubeconfig (empty Kubeconfig field), got:\n%s", out)
	}
}
```

(Adjust the field names/values to match what `StandardConfig` actually requires — check the struct definition for any required fields.)

- [ ] **Step 12.6: Build checkpoint**

```
go build -o bin/ddc.exe .
go test -short ./...
```

Expected: build succeeds, all tests pass including the two new ones. **Do not commit.**

- [ ] **Step 12.7: Manual smoke test (CLI preview)**

If you have a working `~/.kube/config`:

```
.\bin\ddc.exe
```

Pick K8s, work through the TUI. Expected: the preview at the end does NOT contain `--kubeconfig` (auto-detection succeeded — global stays empty).

If you removed/renamed `~/.kube/config` and supplied a path via the TUI:

```
.\bin\ddc.exe
```

Expected: the preview **does** contain `--kubeconfig=<path>` on its own line, immediately above the `--namespace=...` line. **Do not commit.**

---

## Task 13: Implement local-k8s in-cluster → kubeconfig fallback

**Files:**
- Modify: `cmd/root.go:382-424` (the local-k8s branch inside `RemoteCollect`)

**Why:** When DDC's local-k8s mode runs outside a pod (laptop-against-remote-cluster), `rest.InClusterConfig()` fails. With this change, if the user passed `--kubeconfig=/path` (or has $KUBECONFIG / ~/.kube/config), DDC falls back to that for the optional cluster-resource and container-log collection.

- [ ] **Step 13.1: Update the local-k8s in-cluster branch**

In `cmd/root.go`, find the block around line 397-424:

```go
		// Auto-detect namespace and set up K8s cluster-level collection
		const nsPath = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
		detectedNS, nsErr := detectK8sNamespace(nsPath)
		if nsErr != nil {
			simplelog.Warningf("local-k8s: K8s API unavailable (namespace detection failed: %v) — skipping cluster resource and container log collection", nsErr)
		} else {
			restCfg, cfgErr := rest.InClusterConfig()
			if cfgErr != nil {
				simplelog.Warningf("local-k8s: K8s API unavailable (in-cluster config failed: %v) — skipping cluster resource and container log collection", cfgErr)
			} else {
				clientSet, csErr := k8sapi.NewForConfig(restCfg)
				if csErr != nil {
					simplelog.Warningf("local-k8s: K8s API unavailable (clientset creation failed: %v) — skipping cluster resource and container log collection", csErr)
				} else {
					...
```

Replace with the kubeconfig-aware fallback structure. The key change: when in-cluster config fails AND a kubeconfig path is resolvable, try `GetClientset("", kubeconfigPath)` instead. Also handle namespace detection from the kubeconfig's current-context as a secondary source:

```go
		// Auto-detect namespace; fall back to kubeconfig current-context if
		// no service-account file is present.
		const nsPath = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
		detectedNS, nsErr := detectK8sNamespace(nsPath)

		// Try in-cluster config first; if that fails, fall back to a
		// user-supplied kubeconfig (flag → env → home).
		var clientSet *k8sapi.Clientset
		restCfg, cfgErr := rest.InClusterConfig()
		if cfgErr == nil {
			cs, csErr := k8sapi.NewForConfig(restCfg)
			if csErr != nil {
				simplelog.Warningf("local-k8s: K8s API unavailable (clientset creation failed: %v) — skipping cluster resource and container log collection", csErr)
			} else {
				clientSet = cs
			}
		} else {
			cs, _, kcErr := kubernetes.GetClientset("", kubeconfigPath)
			if kcErr != nil {
				simplelog.Warningf("local-k8s: K8s API unavailable (in-cluster config failed: %v; kubeconfig fallback also failed: %v) — skipping cluster resource and container log collection", cfgErr, kcErr)
			} else {
				clientSet = cs
				simplelog.Info("local-k8s: using kubeconfig fallback for K8s API access")
				// If the namespace file wasn't readable, use the kubeconfig's current-context namespace.
				if nsErr != nil {
					if nsFromKubeconfig, kcNsErr := readNamespaceFromKubeconfig(kubeconfigPath); kcNsErr == nil && nsFromKubeconfig != "" {
						detectedNS = nsFromKubeconfig
						nsErr = nil
					}
				}
			}
		}
		if nsErr != nil {
			simplelog.Warningf("local-k8s: namespace detection failed (%v) and no fallback available — skipping cluster resource and container log collection", nsErr)
		} else if clientSet != nil {
			simplelog.Infof("local-k8s: K8s API available, namespace=%s — collecting cluster resources and container logs", detectedNS)
			clusterCollect = func() {
				if err := collection.ClusterK8sExecute(hook, detectedNS, clientSet, cs, collectionArgs.DDCfs); err != nil {
					simplelog.Errorf("local-k8s: error collecting K8s resources: %v", err)
				}
				if err := collection.GetPreviousLogsForRestartedPods(hook, detectedNS, clientSet, cs, collectionArgs.DDCfs, ""); err != nil {
					simplelog.Errorf("local-k8s: error collecting previous container logs for restarted pods: %v", err)
				}
				if collectContainerLogs {
					if err := collection.GetClusterLogs(hook, detectedNS, clientSet, cs, collectionArgs.DDCfs, ""); err != nil {
						simplelog.Errorf("local-k8s: error collecting container logs: %v", err)
					}
				} else {
					simplelog.Info("local-k8s: skipping container log collection (disabled)")
				}
			}
		}
```

Note: the original code only attempts cluster collection when `nsErr == nil` AND `cfgErr == nil` AND `csErr == nil`. The refactor above preserves that "all three must succeed" gate, just with the kubeconfig fallback path inserted.

- [ ] **Step 13.2: Implement `readNamespaceFromKubeconfig` helper**

Add this helper near the bottom of `cmd/root.go`:

```go
// readNamespaceFromKubeconfig returns the default namespace declared on the
// current-context of the supplied kubeconfig (after the standard precedence
// resolution explicit → $KUBECONFIG → ~/.kube/config). Returns "" with no
// error if the current-context has no default namespace declared.
func readNamespaceFromKubeconfig(explicit string) (string, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if explicit != "" {
		loadingRules.ExplicitPath = dirs.ExpandTilde(explicit)
	}
	cfg, err := loadingRules.Load()
	if err != nil {
		return "", err
	}
	currentCtx := cfg.CurrentContext
	if currentCtx == "" {
		return "", nil
	}
	ctx, ok := cfg.Contexts[currentCtx]
	if !ok || ctx == nil {
		return "", nil
	}
	return ctx.Namespace, nil
}
```

Add `"k8s.io/client-go/tools/clientcmd"` to `cmd/root.go`'s imports if not already present (it likely isn't — the `kubernetes` package wraps clientcmd today). Also confirm `dirs` is imported (it was added in Task 11).

- [ ] **Step 13.3: Build checkpoint**

```
go build -o bin/ddc.exe .
go test -short ./...
```

Expected: build succeeds, all tests pass. **Do not commit.**

- [ ] **Step 13.4: Manual smoke test (local-k8s flag-only)**

```
.\bin\ddc.exe collect local-k8s standard --kubeconfig=C:\path\to\config
```

(Run from a workstation, NOT inside a pod.) Expected: in the log file (`ddc.log` inside the produced archive, or stdout simplelog), see `local-k8s: using kubeconfig fallback for K8s API access` and either `K8s API available, namespace=...` (success) or a clear "kubeconfig fallback also failed" warning. Local file collection should still run regardless. **Do not commit.**

---

## Task 14: Final verification, doc, and squashed commit

**Files:**
- Verify: all of the above
- Update: `CHANGELOG.md` (one-line entry)

- [ ] **Step 14.1: Full test suite**

```
go fmt ./...
go test -short ./...
```

Expected: PASS for all packages.

If on Linux/macOS, also run:

```
go test -race -short ./...
```

- [ ] **Step 14.2: Lint**

```
golangci-lint run
```

Expected: no new findings introduced by this change. Address any reported issues inline.

- [ ] **Step 14.3: Final build of the binary**

```
go build -o bin/ddc.exe .
```

Expected: produces `bin/ddc.exe` cleanly.

- [ ] **Step 14.4: Update `CHANGELOG.md`**

Add a one-line entry to the top of `CHANGELOG.md` under the current/unreleased section (follow the existing style):

```
- TUI prompts for kubeconfig path when none is auto-detected; new --kubeconfig flag on k8s and local-k8s subcommands.
```

- [ ] **Step 14.5: Verify git status**

```
git status
```

Confirm the changed/new files match the file map at the top of this plan plus the spec, this plan, and CHANGELOG.md. There should be no unrelated files.

- [ ] **Step 14.6: Stage and create the single squashed commit**

Stage all the relevant paths explicitly (avoid `git add -A`):

```
git add ^
  docs/superpowers/specs/2026-05-07-kubeconfig-path-input-design.md ^
  docs/superpowers/plans/2026-05-07-kubeconfig-path-input.md ^
  pkg/dirs/dirs.go ^
  pkg/dirs/dirs_test.go ^
  cmd/root/kubernetes/kubernetes.go ^
  cmd/root/kubernetes/kubernetes_test.go ^
  cmd/root/kubernetes/list_contexts_test.go ^
  cmd/configui/kubeconfig.go ^
  cmd/configui/kubeconfig_test.go ^
  cmd/configui/configui.go ^
  cmd/configui/configui_test.go ^
  cmd/cli_generator.go ^
  cmd/cli_generator_test.go ^
  cmd/root.go ^
  CHANGELOG.md
```

(Adjust the list if any file was not actually modified, or if other files were changed.)

Then create the single commit (use a HEREDOC for clean formatting):

```
git commit -m "$(cat <<'EOF'
Prompt for kubeconfig path when none is auto-detected

When no kubeconfig is found via $KUBECONFIG or ~/.kube/config, DDC's TUI
now shows an input field with an OS-aware example placeholder. The path
is validated synchronously (file/parse/contexts) and asynchronously
(cluster connectivity probe in a spinner; up to 3 attempts).

Adds a --kubeconfig flag on K8sCmd and LocalK8sCmd. Precedence matches
kubectl: flag > $KUBECONFIG > ~/.kube/config. The flag is emitted in the
generated reproducible-CLI preview only when the user explicitly chose a
non-default path. Tilde expansion is centralised so PowerShell users can
pass --kubeconfig=~/.kube/config directly.

local-k8s mode now falls back to kubeconfig when in-cluster config is
unavailable (laptop-against-remote-cluster use case) — local file
collection is unaffected; only optional cluster-resource and container-
log collection is gated on a usable clientset.

Spec: docs/superpowers/specs/2026-05-07-kubeconfig-path-input-design.md
Plan: docs/superpowers/plans/2026-05-07-kubeconfig-path-input.md

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

- [ ] **Step 14.7: Verify**

```
git status
git log -1 --stat
```

Expected: working tree clean, single new commit on top of the previous tip with all changed files included.

---

## Self-review checklist

Run through this before declaring done:

1. ✅ Trigger condition (spec §"Trigger condition") — Task 11 Step 11.2 implements the `if len(contexts) == 0` branch.
2. ✅ TUI input step shape (spec §"TUI input step") — Task 11 Step 11.3 (title, description, OS-aware placeholder).
3. ✅ Layer 1 validation (file/parse/contexts) — Task 7 + Task 11 wires it via `configui.ValidateKubeconfigPath`.
4. ✅ Layer 2 connectivity probe with retry — Task 6 (`VerifyConnectivity`) + Task 11 Step 11.3 retry loop.
5. ✅ New CLI flag `--kubeconfig` — Task 9.
6. ✅ Resolution precedence (flag → env → home) with tilde expansion — Task 1 (`ExpandTilde`) + Task 2 (`resolveKubeconfigPath`).
7. ✅ Plumbing through `GetClientset`, `ListContexts`, `GetClusters`, `DiscoverPods` — Tasks 3-5.
8. ✅ `KubeconfigPath` field on `KubeArgs` — Task 4 Step 4.5 + Task 10 Step 10.5.
9. ✅ local-k8s integration (in-cluster → kubeconfig fallback) — Task 13.
10. ✅ CLI command preview with new flag positioned above `--namespace`, only emitted when explicit — Task 8 + Task 12.
11. ✅ Tests: precedence, ListContexts explicit path, GetClientset explicit-over-env, generator emit/omit/order, validator table, ExpandTilde, VerifyConnectivity (success + failure) — covered in Tasks 1, 2, 3, 4, 6, 7, 8.
12. ✅ Single squashed commit at the end — Task 14.

If any of the above doesn't hold true after implementation, fix before commit.
