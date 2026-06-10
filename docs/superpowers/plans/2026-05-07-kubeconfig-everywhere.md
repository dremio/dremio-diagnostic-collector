# Kubeconfig Everywhere Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `--kubeconfig` actually flow through every `kubectl` shellout in DDC's production code (path discovery, RBAC pre-flight, the full `--enable-kubectl` collector). The earlier commit `f9ffdca` only fixed the K8s API client path; this one closes the gap so a WSL user (or anyone with a non-default kubeconfig) can use DDC end-to-end.

**Architecture:** Per-module helper, no new abstractions. The kubectl-based collector (`CliK8sActions`) gains a `kubeconfigPath` field and a `k8sFlags()` method returning the conditional `--kubeconfig`/`--context` flag pair. `runPathDiscovery` and `CheckRBAC` are small enough to inline the conditional pair directly. `CanRetryTransfers` switches to `kubectl version --client` (semantically correct — it only parses ClientVersion) so kubeconfig state doesn't gate it.

**Tech Stack:** Go 1.24, Cobra (CLI), client-go (kubernetes API), `kubectl` shellouts.

**Commit strategy:** Per the user's request, **DO NOT make intermediate commits**. Accumulate all changes in the working tree across tasks. The final task folds the work into the existing squash commit `f9ffdca` via `git commit --amend --no-edit` (no message change — the original message already covers "make `--kubeconfig` work end-to-end"). Each task uses `go build` + `go test -short ./...` as its checkpoint.

**Test runner:** `go test -short ./...` on Windows. The pre-existing `integrationtest/kube` failure (no live cluster) should be ignored — confirm no NEW failures.

---

## Reference: spec

Full design: `docs/superpowers/specs/2026-05-07-kubeconfig-everywhere-design.md`. The spec has the full inventory table and use-site sketches.

## Reference: file map

```
EDIT  cmd/root.go                                  — runPathDiscovery sig (+kubeconfig param), inner kubectl args, caller at :997
EDIT  cmd/root/kubernetes/kubernetes.go            — CheckRBAC sig (+kubeconfigPath param), caller in cmd/root.go:483
EDIT  cmd/root/kubectl/kubectl.go                  — CliK8sActions.kubeconfigPath field, k8sFlags() helper, all 8+ shellout sites, NewKubectlK8sActions current-context fix, CanRetryTransfers --client fix
EDIT  cmd/root/kubectl/kubectl_test.go             — TestCliK8sActions_k8sFlags
```

---

## Task 1: Add `kubeconfigPath` field + `k8sFlags()` helper to `CliK8sActions`

**Files:**
- Modify: `cmd/root/kubectl/kubectl.go` (struct field, helper method, populate field in `NewKubectlK8sActions`)
- Modify: `cmd/root/kubectl/kubectl_test.go` (add `TestCliK8sActions_k8sFlags`)

**Why:** Foundation for Task 2 (which uses the helper at every shellout). TDD: write the helper test first, then implement.

- [ ] **Step 1.1: Write the failing test**

Append to `cmd/root/kubectl/kubectl_test.go`:

```go
func TestCliK8sActions_k8sFlags(t *testing.T) {
	tests := []struct {
		name       string
		kubeconfig string
		context    string
		want       []string
	}{
		{"both empty", "", "", nil},
		{"only kubeconfig", "/tmp/cfg", "", []string{"--kubeconfig", "/tmp/cfg"}},
		{"only context", "", "prod", []string{"--context", "prod"}},
		{"both", "/tmp/cfg", "prod", []string{"--kubeconfig", "/tmp/cfg", "--context", "prod"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := &CliK8sActions{kubeconfigPath: tc.kubeconfig, k8sContext: tc.context}
			got := c.k8sFlags()
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
```

If `"reflect"` isn't already imported in the test file, add it.

- [ ] **Step 1.2: Run the test to verify it fails**

```
go test -short -run TestCliK8sActions_k8sFlags ./cmd/root/kubectl/...
```

Expected: FAIL — `unknown field kubeconfigPath in struct literal of type CliK8sActions` and/or `c.k8sFlags undefined`.

- [ ] **Step 1.3: Add `kubeconfigPath` field to the struct**

Locate `type CliK8sActions struct` in `cmd/root/kubectl/kubectl.go` (around line 73). Replace:

```go
type CliK8sActions struct {
	cli            cli.CmdExecutor
	labelSelector  string
	kubectlPath    string
	namespace      string
	k8sContext     string
	pidHosts       map[string]string
	m              sync.Mutex
	retriesEnabled bool
}
```

with:

```go
type CliK8sActions struct {
	cli            cli.CmdExecutor
	labelSelector  string
	kubectlPath    string
	namespace      string
	k8sContext     string
	kubeconfigPath string
	pidHosts       map[string]string
	m              sync.Mutex
	retriesEnabled bool
}
```

- [ ] **Step 1.4: Add the `k8sFlags()` helper method**

Add this method anywhere in `cmd/root/kubectl/kubectl.go` (a natural spot is right after the struct definition or near the `addRetries` helper):

```go
// k8sFlags returns the cluster-routing flags (--kubeconfig, --context)
// that must precede any kubectl subcommand. Empty values are omitted.
// Order matches kubectl convention: global flags before subcommand.
func (c *CliK8sActions) k8sFlags() []string {
	var flags []string
	if c.kubeconfigPath != "" {
		flags = append(flags, "--kubeconfig", c.kubeconfigPath)
	}
	if c.k8sContext != "" {
		flags = append(flags, "--context", c.k8sContext)
	}
	return flags
}
```

- [ ] **Step 1.5: Populate the field in `NewKubectlK8sActions`**

Locate the `return &CliK8sActions{...}` literal at the end of `NewKubectlK8sActions` (around line 62). Add `kubeconfigPath: kubeArgs.KubeconfigPath` next to the other field assignments:

```go
return &CliK8sActions{
    cli:            cliInstance,
    kubectlPath:    kubectl,
    labelSelector:  kubeArgs.LabelSelector,
    namespace:      kubeArgs.Namespace,
    k8sContext:     k8sContext,
    kubeconfigPath: kubeArgs.KubeconfigPath,
    pidHosts:       make(map[string]string),
    retriesEnabled: retriesEnabled,
}, nil
```

- [ ] **Step 1.6: Run the test to verify it passes**

```
go test -short -run TestCliK8sActions_k8sFlags ./cmd/root/kubectl/...
```

Expected: PASS for all 4 subtests.

- [ ] **Step 1.7: Build + full test checkpoint**

```
go build -o bin/ddc.exe .
go test -short ./...
```

Expected: build succeeds, all unit tests pass. (`integrationtest/kube` is pre-existing — confirm no NEW failures.) **Do not commit.**

---

## Task 2: Wire `k8sFlags()` into every shellout in `cmd/root/kubectl/kubectl.go`

**Files:**
- Modify: `cmd/root/kubectl/kubectl.go` (8 shellout sites + `NewKubectlK8sActions` early call + `CanRetryTransfers`)

**Why:** With the helper available, every `kubectl` shellout in this file should use it, plus the two early sites that don't have a `CliK8sActions` instance handy (`NewKubectlK8sActions` itself, `CanRetryTransfers`).

- [ ] **Step 2.1: Update `NewKubectlK8sActions` early `kubectl config current-context` call**

Locate (around line 50-57):

```go
k8sContext := kubeArgs.K8SContext
if k8sContext == "" {
    k8sContextRaw, err := cliInstance.Execute(false, kubectl, "config", "current-context")
    if err != nil {
        return &CliK8sActions{}, fmt.Errorf("unable to retrieve context: %w", err)
    }
    k8sContext = strings.TrimSpace(k8sContextRaw)
}
```

Replace with:

```go
k8sContext := kubeArgs.K8SContext
if k8sContext == "" {
    ctxArgs := []string{}
    if kubeArgs.KubeconfigPath != "" {
        ctxArgs = append(ctxArgs, "--kubeconfig", kubeArgs.KubeconfigPath)
    }
    ctxArgs = append(ctxArgs, "config", "current-context")
    k8sContextRaw, err := cliInstance.Execute(false, kubectl, ctxArgs...)
    if err != nil {
        return &CliK8sActions{}, fmt.Errorf("unable to retrieve context: %w", err)
    }
    k8sContext = strings.TrimSpace(k8sContextRaw)
}
```

- [ ] **Step 2.2: Update `CanRetryTransfers` to use `--client`**

Locate (around line 85-87):

```go
func CanRetryTransfers(kubectlPath string) (bool, error) {
    kubectlExec := exec.Command(kubectlPath, "version", "-o", "json")
    out, err := kubectlExec.Output()
```

Replace with:

```go
func CanRetryTransfers(kubectlPath string) (bool, error) {
    // Use --client so the call doesn't depend on cluster reachability or
    // a valid kubeconfig — we only parse ClientVersion from the JSON.
    kubectlExec := exec.Command(kubectlPath, "version", "--client", "-o", "json")
    out, err := kubectlExec.Output()
```

(Just adds `"--client"` between `"version"` and `"-o"`. No signature change.)

- [ ] **Step 2.3: Update `getContainerName` (line ~130)**

Locate:

```go
conts, err := c.cli.Execute(false, c.kubectlPath, "-n", c.namespace, "--context", c.k8sContext, "get", "pods", string(podName), "-o", `jsonpath={.spec.containers[*].name}`)
```

Replace with:

```go
args := []string{c.kubectlPath}
args = append(args, c.k8sFlags()...)
args = append(args, "-n", c.namespace, "get", "pods", string(podName), "-o", `jsonpath={.spec.containers[*].name}`)
conts, err := c.cli.Execute(false, args...)
```

- [ ] **Step 2.4: Update `HostExecuteAndStream` (lines ~184/186)**

Locate:

```go
var kubectlArgs []string
if pat == "" {
    kubectlArgs = []string{c.kubectlPath, "exec", "-n", c.namespace, "--context", c.k8sContext, "-c", container, hostString, "--"}
} else {
    kubectlArgs = []string{c.kubectlPath, "exec", "-i", "-n", c.namespace, "--context", c.k8sContext, "-c", container, hostString, "--"}
}

kubectlArgs = append(kubectlArgs, "sh", "-c", strings.Join(args, " "))
```

Replace with:

```go
kubectlArgs := []string{c.kubectlPath}
kubectlArgs = append(kubectlArgs, c.k8sFlags()...)
kubectlArgs = append(kubectlArgs, "exec")
if pat != "" {
    kubectlArgs = append(kubectlArgs, "-i")
}
kubectlArgs = append(kubectlArgs, "-n", c.namespace, "-c", container, hostString, "--", "sh", "-c", strings.Join(args, " "))
```

- [ ] **Step 2.5: Update `CopyToHost` (line ~214)**

Locate:

```go
args := []string{c.kubectlPath, "cp", "-n", c.namespace, "--context", c.k8sContext, "-c", container}
args = c.addRetries(args)
args = append(args, c.cleanLocal(source), fmt.Sprintf("%v:%v", hostString, destination))
return c.cli.Execute(false, args...)
```

Replace with:

```go
args := []string{c.kubectlPath}
args = append(args, c.k8sFlags()...)
args = append(args, "cp", "-n", c.namespace, "-c", container)
args = c.addRetries(args)
args = append(args, c.cleanLocal(source), fmt.Sprintf("%v:%v", hostString, destination))
return c.cli.Execute(false, args...)
```

- [ ] **Step 2.6: Update `SearchPods` (line ~227)**

Locate:

```go
out, err := c.cli.Execute(false, c.kubectlPath, "get", "pods", "-n", c.namespace, "--context", c.k8sContext, "-l", c.labelSelector, "--field-selector", "status.phase=Running", "-o", "name")
```

Replace with:

```go
args := []string{c.kubectlPath}
args = append(args, c.k8sFlags()...)
args = append(args, "get", "pods", "-n", c.namespace, "-l", c.labelSelector, "--field-selector", "status.phase=Running", "-o", "name")
out, err := c.cli.Execute(false, args...)
```

- [ ] **Step 2.7: Update `CleanupRemote` — both calls (lines ~289 and ~302)**

Locate the first kubectlArgs slice (cat call):

```go
kubectlArgs := []string{"exec", "-n", c.namespace, "--context", c.k8sContext, "-c", container, host, "--"}
kubectlArgs = append(kubectlArgs, "cat")
kubectlArgs = append(kubectlArgs, pidFile)
```

Replace with:

```go
kubectlArgs := append([]string{}, c.k8sFlags()...)
kubectlArgs = append(kubectlArgs, "exec", "-n", c.namespace, "-c", container, host, "--")
kubectlArgs = append(kubectlArgs, "cat")
kubectlArgs = append(kubectlArgs, pidFile)
```

Locate the second kubectlArgs slice (kill call):

```go
kubectlArgs = []string{"exec", "-n", c.namespace, "--context", c.k8sContext, "-c", container, host, "--"}
kubectlArgs = append(kubectlArgs, "kill")
kubectlArgs = append(kubectlArgs, "-15")
```

Replace with:

```go
kubectlArgs = append([]string{}, c.k8sFlags()...)
kubectlArgs = append(kubectlArgs, "exec", "-n", c.namespace, "-c", container, host, "--")
kubectlArgs = append(kubectlArgs, "kill")
kubectlArgs = append(kubectlArgs, "-15")
```

(Both reset the slice via `append([]string{}, c.k8sFlags()...)` — this returns a new slice, never `nil`, even when `k8sFlags()` returns `nil`.)

- [ ] **Step 2.8: Update `StreamFromHost` (line ~396-399)**

Locate:

```go
escapedPath := strings.ReplaceAll(remotePath, "'", "'\\''")
args := []string{"exec", host, "-n", c.namespace, "-c", containerName, "--", "sh", "-c", fmt.Sprintf("%s '%s'", streamCmd, escapedPath)}

// #nosec G204 -- arguments are controlled by the caller
cmd := exec.Command(c.kubectlPath, args...)
```

Replace with:

```go
escapedPath := strings.ReplaceAll(remotePath, "'", "'\\''")
args := append([]string{}, c.k8sFlags()...)
args = append(args, "exec", host, "-n", c.namespace, "-c", containerName, "--", "sh", "-c", fmt.Sprintf("%s '%s'", streamCmd, escapedPath))

// #nosec G204 -- arguments are controlled by the caller
cmd := exec.Command(c.kubectlPath, args...)
```

(This site previously had no `--context`. The helper now adds both `--context` and `--kubeconfig`. Pre-existing bug fixed as a free side-effect.)

- [ ] **Step 2.9: Build + full test checkpoint**

```
go build -o bin/ddc.exe .
go test -short ./...
```

Expected: build succeeds, all unit tests pass. The kubectl_test.go tests that exercise `Execute` calls should still pass because their args don't depend on `k8sFlags()` ordering — but if any test asserts the exact arg list, you may need to update it.

If `kubectl_test.go` tests fail because they assert exact arg orders, they likely rely on the old `--context` placement. Update those tests to reflect the new front-loaded ordering. **Do not commit.**

---

## Task 3: Plumb `kubeconfigPath` through `runPathDiscovery`

**Files:**
- Modify: `cmd/root.go` (function signature, both internal kubectl shellouts, caller at line ~997)

**Why:** `runPathDiscovery` runs `kubectl exec ... ps eww` to autodetect the Dremio log dir on a target pod, and `kubectl get pods` to find an executor. Neither passes `--kubeconfig` today; the second also missed `--context`.

- [ ] **Step 3.1: Update the function signature**

Locate `runPathDiscovery` in `cmd/root.go` (around line 1790):

```go
func runPathDiscovery(ns, coordinator, sshUsr, sshKey, k8sCtx string) *configui.DetectedPaths {
```

Change to:

```go
func runPathDiscovery(ns, coordinator, sshUsr, sshKey, k8sCtx, kubeconfig string) *configui.DetectedPaths {
```

- [ ] **Step 3.2: Update `makeRemoteCmd`'s K8s branch to inject `--kubeconfig`**

Inside `runPathDiscovery`, locate the closure (around line 1795-1807):

```go
makeRemoteCmd := func(host string) func(args ...string) *exec.Cmd {
    if ns != "" {
        return func(args ...string) *exec.Cmd {
            var fullArgs []string
            if k8sCtx != "" {
                fullArgs = append(fullArgs, "--context", k8sCtx)
            }
            fullArgs = append(fullArgs, "exec", host, "-n", ns, "--")
            fullArgs = append(fullArgs, "sh", "-c", strings.Join(args, " "))
            return exec.Command("kubectl", fullArgs...)
        }
    }
    ...
```

Replace the `if ns != ""` body with:

```go
if ns != "" {
    return func(args ...string) *exec.Cmd {
        var fullArgs []string
        if kubeconfig != "" {
            fullArgs = append(fullArgs, "--kubeconfig", kubeconfig)
        }
        if k8sCtx != "" {
            fullArgs = append(fullArgs, "--context", k8sCtx)
        }
        fullArgs = append(fullArgs, "exec", host, "-n", ns, "--")
        fullArgs = append(fullArgs, "sh", "-c", strings.Join(args, " "))
        return exec.Command("kubectl", fullArgs...)
    }
}
```

- [ ] **Step 3.3: Update the inner `kubectl get pods` (line ~1967)**

Locate:

```go
if ns != "" {
    // K8s: find first executor pod
    listCmd := exec.Command("kubectl", "get", "pods", "-n", ns, "-o", "name")
    if listOut, err := listCmd.Output(); err == nil {
        executorHost = findExecutorPod(string(listOut))
    }
}
```

Replace with:

```go
if ns != "" {
    // K8s: find first executor pod
    var listArgs []string
    if kubeconfig != "" {
        listArgs = append(listArgs, "--kubeconfig", kubeconfig)
    }
    if k8sCtx != "" {
        listArgs = append(listArgs, "--context", k8sCtx)
    }
    listArgs = append(listArgs, "get", "pods", "-n", ns, "-o", "name")
    listCmd := exec.Command("kubectl", listArgs...)
    if listOut, err := listCmd.Output(); err == nil {
        executorHost = findExecutorPod(string(listOut))
    }
}
```

(The variable `kubeconfig` is the new function parameter; `k8sCtx` was already in scope.)

- [ ] **Step 3.4: Update the caller at `cmd/root.go:997`**

Locate:

```go
detected = runPathDiscovery(namespace, coordinatorStr, sshUser, sshKeyLoc, k8sContext)
```

Change to:

```go
detected = runPathDiscovery(namespace, coordinatorStr, sshUser, sshKeyLoc, k8sContext, kubeconfigPath)
```

- [ ] **Step 3.5: Build + full test checkpoint**

```
go build -o bin/ddc.exe .
go test -short ./...
```

Expected: build succeeds, all unit tests pass. **Do not commit.**

---

## Task 4: Plumb `kubeconfigPath` through `CheckRBAC`

**Files:**
- Modify: `cmd/root/kubernetes/kubernetes.go` (`CheckRBAC` signature + body)
- Modify: `cmd/root.go` (single caller at line ~483)

**Why:** `CheckRBAC` runs three `kubectl auth can-i` calls before the K8s API client takes over. Currently it adds `--context` conditionally but never `--kubeconfig`.

- [ ] **Step 4.1: Update `CheckRBAC` signature and body**

Locate `CheckRBAC` in `cmd/root/kubernetes/kubernetes.go` (around line 715-743):

```go
func CheckRBAC(k8sContext, namespace string) error {
    checks := []struct {
        verb     string
        resource string
    }{
        {"get", "pods"},
        {"list", "pods"},
        {"create", "pods/exec"},
    }
    var missing []string
    for _, check := range checks {
        cmd := exec.Command("kubectl", "auth", "can-i", check.verb, check.resource, "-n", namespace)
        if k8sContext != "" {
            cmd = exec.Command("kubectl", "auth", "can-i", check.verb, check.resource, "-n", namespace, "--context", k8sContext)
        }
        output, err := cmd.CombinedOutput()
        result := strings.TrimSpace(string(output))
        if err != nil || result != "yes" {
            missing = append(missing, fmt.Sprintf("%s %s", check.verb, check.resource))
        }
    }
    if len(missing) > 0 {
        return fmt.Errorf("insufficient RBAC permissions in namespace %q: missing %s. Ensure your ServiceAccount has a Role/ClusterRole with these permissions", namespace, strings.Join(missing, ", "))
    }
    return nil
}
```

Replace with:

```go
func CheckRBAC(k8sContext, namespace, kubeconfigPath string) error {
    checks := []struct {
        verb     string
        resource string
    }{
        {"get", "pods"},
        {"list", "pods"},
        {"create", "pods/exec"},
    }
    var missing []string
    for _, check := range checks {
        var args []string
        if kubeconfigPath != "" {
            args = append(args, "--kubeconfig", kubeconfigPath)
        }
        if k8sContext != "" {
            args = append(args, "--context", k8sContext)
        }
        args = append(args, "auth", "can-i", check.verb, check.resource, "-n", namespace)
        cmd := exec.Command("kubectl", args...)
        output, err := cmd.CombinedOutput()
        result := strings.TrimSpace(string(output))
        if err != nil || result != "yes" {
            missing = append(missing, fmt.Sprintf("%s %s", check.verb, check.resource))
        }
    }
    if len(missing) > 0 {
        return fmt.Errorf("insufficient RBAC permissions in namespace %q: missing %s. Ensure your ServiceAccount has a Role/ClusterRole with these permissions", namespace, strings.Join(missing, ", "))
    }
    return nil
}
```

- [ ] **Step 4.2: Update the caller at `cmd/root.go:483`**

Locate:

```go
if err := kubernetes.CheckRBAC(kubeArgs.K8SContext, kubeArgs.Namespace); err != nil {
```

Change to:

```go
if err := kubernetes.CheckRBAC(kubeArgs.K8SContext, kubeArgs.Namespace, kubeArgs.KubeconfigPath); err != nil {
```

- [ ] **Step 4.3: Build + full test checkpoint**

```
go build -o bin/ddc.exe .
go test -short ./...
```

Expected: build succeeds, all unit tests pass. **Do not commit.**

---

## Task 5: Final verification + amend the squash commit

**Files:**
- Verify: all touched files build and tests pass
- Amend: `f9ffdca` (the previous squash commit) via `git commit --amend --no-edit`

**Why:** The user wants this work folded into the existing PR commit, not a separate fix-up commit. The original message ("Prompt for kubeconfig path when none is auto-detected") already covers "make `--kubeconfig` work end-to-end" in spirit — no message change needed.

- [ ] **Step 5.1: Final formatting + lint + tests**

```
go fmt ./...
go vet ./...
golangci-lint run
```

Expected: clean (`golangci-lint` should report 0 issues). Address any new findings.

```
go test -short ./...
```

Expected: only `integrationtest/kube` fails (pre-existing). Confirm no NEW failures.

- [ ] **Step 5.2: Final clean build**

```
go build -o bin/ddc.exe .
```

Expected: clean build, `bin/ddc.exe` produced.

- [ ] **Step 5.3: Restore CRLF noise files**

`go fmt ./...` rewrites files with LF endings on Windows; many show up as modified in `git status` despite no real content change. Restore them:

```bash
# Compute the set of files with real content changes (vs. f9ffdca's tree)
git diff --stat HEAD --name-only > /tmp/real_changes.txt

# All modified files
git status --short | grep -E "^ M" | awk '{print $2}' > /tmp/all_modified.txt

# CRLF-only noise = modified MINUS real-content-changes
comm -23 <(sort /tmp/all_modified.txt) <(sort /tmp/real_changes.txt) > /tmp/crlf_only.txt

# Restore the noise files
xargs -a /tmp/crlf_only.txt git checkout HEAD --
```

Verify `git status --short` now shows only the real-content-modified files (the four targeted in Tasks 1-4).

- [ ] **Step 5.4: Confirm the working-tree state**

```
git status --short
```

Expected output (in any order):

```
 M cmd/root.go
 M cmd/root/kubectl/kubectl.go
 M cmd/root/kubectl/kubectl_test.go
 M cmd/root/kubernetes/kubernetes.go
```

If anything else appears (other than untracked `docs/superpowers/specs/` or `docs/superpowers/plans/` files for the new spec/plan), investigate before continuing.

- [ ] **Step 5.5: Confirm `f9ffdca` is the current HEAD and is not pushed**

```
git log -1 --format="%h %s"
git rev-parse --abbrev-ref --symbolic-full-name HEAD@{upstream} 2>&1 || echo "no upstream"
```

Expected:
- HEAD is `f9ffdca Prompt for kubeconfig path when none is auto-detected`
- "no upstream" — branch hasn't been pushed yet (safe to amend)

If upstream IS configured and HEAD is pushed, **STOP** and ask the user before amending — force-push to a shared branch is dangerous.

- [ ] **Step 5.6: Stage the changed files and the new spec + plan**

```bash
git add \
  cmd/root.go \
  cmd/root/kubectl/kubectl.go \
  cmd/root/kubectl/kubectl_test.go \
  cmd/root/kubernetes/kubernetes.go \
  docs/superpowers/specs/2026-05-07-kubeconfig-everywhere-design.md \
  docs/superpowers/plans/2026-05-07-kubeconfig-everywhere.md
```

Verify with:

```
git status --short
```

Expected: all six files shown as `M` (modified) or `A` (added) with no `??` untracked entries for these paths, and no other entries.

- [ ] **Step 5.7: Amend the previous commit**

```
git commit --amend --no-edit
```

Expected: commit succeeds, no message editor opens, output shows the amended commit hash (will differ from `f9ffdca` since the tree changed).

- [ ] **Step 5.8: Verify the amended commit**

```
git log -1 --stat
```

Expected output:
- The commit message is unchanged ("Prompt for kubeconfig path when none is auto-detected")
- The stat lists the previously-included 15 files PLUS the 4 production files modified by this PR (some files may overlap if they were already in the prior commit, e.g. `cmd/root.go` and `cmd/root/kubernetes/kubernetes.go`)
- Two new files: `docs/superpowers/specs/2026-05-07-kubeconfig-everywhere-design.md` and `docs/superpowers/plans/2026-05-07-kubeconfig-everywhere.md`

Optionally:

```
git diff main..HEAD --stat
```

Shows the full PR footprint. Confirm it looks right.

- [ ] **Step 5.9: Sanity smoke test**

If you can run the binary, verify the path-discovery flow now uses `--kubeconfig`:

```
.\bin\ddc.exe collect k8s standard --kubeconfig=C:\nonexistent\config --namespace=foo
```

Expected: an error referencing the supplied kubeconfig path (e.g., "The system cannot find the path specified" from the Windows kubectl when the file doesn't exist) — **not** the in-cluster fallback message ("KUBERNETES_SERVICE_HOST and KUBERNETES_SERVICE_PORT must be defined"). This proves the flag is being threaded through to `runPathDiscovery`'s `kubectl exec` call.

---

## Self-review checklist

Run through this before declaring done:

1. ✅ Inventory site 1 (`runPathDiscovery` `kubectl exec`) — Task 3 Step 3.2
2. ✅ Inventory site 2 (`runPathDiscovery` inner `kubectl get pods`) — Task 3 Step 3.3
3. ✅ Inventory site 3 (`CheckRBAC` `kubectl auth can-i` ×3) — Task 4 Step 4.1
4. ✅ Inventory site 4 (`kubectl config current-context` early call) — Task 2 Step 2.1
5. ✅ Inventory site 5 (`CanRetryTransfers` `kubectl version`) — Task 2 Step 2.2
6. ✅ Inventory site 6 (`getContainerName` `kubectl ... get pods`) — Task 2 Step 2.3
7. ✅ Inventory site 7 (`HostExecuteAndStream` `kubectl exec`) — Task 2 Step 2.4
8. ✅ Inventory site 8 (`CopyToHost` `kubectl cp`) — Task 2 Step 2.5
9. ✅ Inventory site 9 (`SearchPods` `kubectl get pods`) — Task 2 Step 2.6
10. ✅ Inventory site 10 (`CleanupRemote` `kubectl exec ... cat / kill`) — Task 2 Step 2.7
11. ✅ Inventory site 11 (`StreamFromHost` `kubectl exec`) — Task 2 Step 2.8
12. ✅ `CliK8sActions.kubeconfigPath` field — Task 1 Step 1.3
13. ✅ `k8sFlags()` helper + test — Task 1 Steps 1.1-1.6
14. ✅ Squash commit amend — Task 5 Step 5.7

If any of the above doesn't hold true after implementation, fix before commit.
