# Kubeconfig Everywhere — Design

**Date:** 2026-05-07
**Status:** Approved (pending spec review)
**Branch:** ddc_v4
**Follow-up to:** [2026-05-07-kubeconfig-path-input-design.md](2026-05-07-kubeconfig-path-input-design.md)

## Problem

The earlier kubeconfig PR (commit `f9ffdca`) added a `--kubeconfig` flag and
threaded it through DDC's K8s **API client** path (`GetClientset`,
`ListContexts`, `GetClusters`, `DiscoverPods`, etc.). It also added a TUI
input prompt and a CLI command preview entry.

What it missed: every place DDC shells out to the `kubectl` **binary**
still uses kubectl's default kubeconfig discovery. That breaks DDC for
WSL users (and anyone running with a non-default kubeconfig) in three
flows:

1. **Path discovery** — DDC probes a target pod with `kubectl exec ... ps eww`
   to autodetect Dremio's log dir before showing the config screen.
   Currently this `kubectl` call passes `--context` but not `--kubeconfig`,
   so it can't reach the cluster the user supplied.
2. **RBAC pre-flight** — `kubectl auth can-i ...` is run three times to
   verify the service account has `get/list pods` and `create pods/exec`.
3. **The full kubectl-based collector** — `--enable-kubectl` switches DDC
   from the embedded K8s API client to shelling out to `kubectl` for every
   operation (`get pods`, `exec`, `cp`, `cat`, `kill`, ...). Every single
   one of these forgets `--kubeconfig`.

A WSL user with a kubeconfig at the Windows path `C:\Users\you\.kube\config`
(typical setup) sees DDC fail despite passing `--kubeconfig` because the
kubectl shellouts above ignore the flag.

## Goals

- Every `kubectl` shellout in production code accepts the user-supplied
  `--kubeconfig` value when one is set.
- `--context` is also added at sites that today omit it (free side-effect
  fix — these are pre-existing bugs for non-default contexts).
- Behaviour preserved when `kubeconfigPath == ""` (existing default
  kubeconfig discovery still works).
- Single squash commit (`f9ffdca`) becomes "kubeconfig works end-to-end."

## Non-goals

- Not refactoring the kubectl-based collector beyond what's needed for
  flag plumbing.
- Not introducing a new `pkg/kubectlcmd` builder package — too much
  abstraction for ~12 call-sites in two files.
- Not changing the K8s API client path — already done in the prior PR.
- Not touching integration tests.

## Inventory of `kubectl` shellouts

Twelve sites across three files. Confirmed by full repo grep
(`exec\.Command\(.*kubectl|exec\.CommandContext\(.*kubectl|\.Execute\(.*kubectl`).

| # | File | Function | Current flags |
|---|---|---|---|
| 1 | `cmd/root.go:1805` | `runPathDiscovery` `kubectl exec` | `--context` only |
| 2 | `cmd/root.go:1967` | `runPathDiscovery` inner `kubectl get pods` | none |
| 3 | `cmd/root/kubernetes/kubernetes.go:729-731` | `CheckRBAC` `kubectl auth can-i` ×3 | `--context` (conditional) |
| 4 | `cmd/root/kubectl/kubectl.go:52` | `kubectl config current-context` (when context unset) | none |
| 5 | `cmd/root/kubectl/kubectl.go:86` | `CanRetryTransfers` `kubectl version -o json` | none |
| 6 | `cmd/root/kubectl/kubectl.go:130` | `getContainerName` `kubectl ... get pods <pod>` | `--context` |
| 7 | `cmd/root/kubectl/kubectl.go:184/186` | `HostExecuteAndStream` `kubectl exec` | `--context` |
| 8 | `cmd/root/kubectl/kubectl.go:214` | `CopyToHost` `kubectl cp` | `--context` |
| 9 | `cmd/root/kubectl/kubectl.go:227` | `SearchPods` `kubectl get pods` | `--context` |
| 10 | `cmd/root/kubectl/kubectl.go:289/302` | `CleanupRemote` `kubectl exec ... cat / kill` | `--context` |
| 11 | `cmd/root/kubectl/kubectl.go:399` | `StreamFromHost` `kubectl exec` | none |

## Approach (chosen)

**Per-module helper, not a global builder.** A small helper per file/struct
that produces the global flag pair (`--kubeconfig`, `--context`) when
non-empty:

- `cmd/root.go::runPathDiscovery` — extend `makeRemoteCmd`'s closure with
  the new `kubeconfig` parameter; inline the conditional `--kubeconfig` /
  `--context` pair where args are built.
- `cmd/root/kubernetes/kubernetes.go::CheckRBAC` — small enough to inline
  the conditional pair directly into the args slice.
- `cmd/root/kubectl/kubectl.go::CliK8sActions` — gain a `kubeconfigPath`
  field and a `k8sFlags()` method; every shellout uses it.

Lowest churn, explicit at the call site, no new abstractions.

## Component changes

### 1. `cmd/root.go` — `runPathDiscovery`

**Signature change:**

```go
// before
func runPathDiscovery(ns, coordinator, sshUsr, sshKey, k8sCtx string) *configui.DetectedPaths

// after
func runPathDiscovery(ns, coordinator, sshUsr, sshKey, k8sCtx, kubeconfig string) *configui.DetectedPaths
```

**`makeRemoteCmd` K8s branch updated to inject `--kubeconfig` (and
preserve the existing `--context`):**

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
        fullArgs = append(fullArgs, "exec", host, "-n", ns, "--",
            "sh", "-c", strings.Join(args, " "))
        return exec.Command("kubectl", fullArgs...)
    }
}
```

**Inner `kubectl get pods` at line 1967** — currently builds args with
neither `--context` nor `--kubeconfig`. Mirror the pattern:

```go
var listArgs []string
if kubeconfig != "" {
    listArgs = append(listArgs, "--kubeconfig", kubeconfig)
}
if k8sCtx != "" {
    listArgs = append(listArgs, "--context", k8sCtx)
}
listArgs = append(listArgs, "get", "pods", "-n", ns, "-o", "name")
listCmd := exec.Command("kubectl", listArgs...)
```

This is a free `--context` fix as a side-effect — pre-existing bug for
non-default contexts.

**Caller at `cmd/root.go:997`:**

```go
detected = runPathDiscovery(namespace, coordinatorStr, sshUser, sshKeyLoc, k8sContext, kubeconfigPath)
```

### 2. `cmd/root/kubernetes/kubernetes.go` — `CheckRBAC`

**Signature change:**

```go
// before
func CheckRBAC(k8sContext, namespace string) error

// after
func CheckRBAC(k8sContext, namespace, kubeconfigPath string) error
```

**Body** rebuilds args once per check with conditional flags:

```go
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
    ...
}
```

**Caller at `cmd/root.go:483`:**

```go
if err := kubernetes.CheckRBAC(kubeArgs.K8SContext, kubeArgs.Namespace, kubeArgs.KubeconfigPath); err != nil { ... }
```

### 3. `cmd/root/kubectl/kubectl.go` — `CliK8sActions`

**Struct field added:**

```go
type CliK8sActions struct {
    cli            cli.CmdExecutor
    labelSelector  string
    kubectlPath    string
    namespace      string
    k8sContext     string
    kubeconfigPath string  // NEW
    pidHosts       map[string]string
    m              sync.Mutex
    retriesEnabled bool
}
```

**`NewKubectlK8sActions` updated to populate the field and pass
`--kubeconfig` to the early `kubectl config current-context` call:**

```go
func NewKubectlK8sActions(hook shutdown.CancelHook, kubeArgs kubernetes.KubeArgs) (*CliK8sActions, error) {
    kubectl, err := exec.LookPath("kubectl")
    if err != nil { ... }
    cliInstance := cli.NewCli(hook)

    k8sContext := kubeArgs.K8SContext
    if k8sContext == "" {
        ctxArgs := []string{}
        if kubeArgs.KubeconfigPath != "" {
            ctxArgs = append(ctxArgs, "--kubeconfig", kubeArgs.KubeconfigPath)
        }
        ctxArgs = append(ctxArgs, "config", "current-context")
        k8sContextRaw, err := cliInstance.Execute(false, kubectl, ctxArgs...)
        if err != nil { ... }
        k8sContext = strings.TrimSpace(k8sContextRaw)
    }

    retriesEnabled, err := CanRetryTransfers(kubectl)
    if err != nil { ... }

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
}
```

**`CanRetryTransfers` — switch to `--client`** (no signature change). The
function only parses `ClientVersion` from the JSON; passing `--client`
makes the call kubeconfig-independent and avoids the misleading
"unable to run kubectl version → disable kubectl" path when the cluster
is unreachable for unrelated reasons:

```go
func CanRetryTransfers(kubectlPath string) (bool, error) {
    kubectlExec := exec.Command(kubectlPath, "version", "--client", "-o", "json")
    out, err := kubectlExec.Output()
    ...
}
```

Verified empirically: `kubectl --kubeconfig=/nonexistent version --client -o json`
exits 0 with valid JSON.

**New helper method `k8sFlags()`:**

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

**Use sites — every shellout uses the helper.** Sketches below show
front-loaded placement (helper output immediately after `c.kubectlPath`
or, in slices that don't prepend the path, at the front). kubectl is
permissive about flag position; the existing code is itself slightly
inconsistent (some sites put `--context` after `-n NAMESPACE`, one puts
it after `get pods`). Front-loading is conventional and the choice that
makes the helper integration uniform across all sites. Behaviour is
identical regardless of placement.

Site 6 — `getContainerName` (line 130):

```go
args := []string{c.kubectlPath}
args = append(args, c.k8sFlags()...)
args = append(args, "-n", c.namespace, "get", "pods", string(podName), "-o", `jsonpath={.spec.containers[*].name}`)
conts, err := c.cli.Execute(false, args...)
```

Site 7 — `HostExecuteAndStream` (lines 184/186):

```go
kubectlArgs := []string{c.kubectlPath}
kubectlArgs = append(kubectlArgs, c.k8sFlags()...)
kubectlArgs = append(kubectlArgs, "exec")
if pat != "" {
    kubectlArgs = append(kubectlArgs, "-i")
}
kubectlArgs = append(kubectlArgs, "-n", c.namespace, "-c", container, hostString, "--",
    "sh", "-c", strings.Join(args, " "))
return c.cli.ExecuteAndStreamOutput(mask, output, pat, kubectlArgs...)
```

Site 8 — `CopyToHost` (line 214):

```go
args := []string{c.kubectlPath}
args = append(args, c.k8sFlags()...)
args = append(args, "cp", "-n", c.namespace, "-c", container)
args = c.addRetries(args)
args = append(args, c.cleanLocal(source), fmt.Sprintf("%v:%v", hostString, destination))
return c.cli.Execute(false, args...)
```

Site 9 — `SearchPods` (line 227):

```go
args := []string{c.kubectlPath}
args = append(args, c.k8sFlags()...)
args = append(args, "get", "pods", "-n", c.namespace,
    "-l", c.labelSelector, "--field-selector", "status.phase=Running", "-o", "name")
out, err := c.cli.Execute(false, args...)
```

Site 10 — `CleanupRemote` (lines 289 and 302):

```go
kubectlArgs := append([]string{}, c.k8sFlags()...)
kubectlArgs = append(kubectlArgs, "exec", "-n", c.namespace, "-c", container, host, "--", "cat", pidFile)
// (...same pattern for the "kill", "-15", pid call later)
```

Site 11 — `StreamFromHost` (line 399):

```go
args := append([]string{}, c.k8sFlags()...)
args = append(args, "exec", host, "-n", c.namespace, "-c", containerName, "--",
    "sh", "-c", fmt.Sprintf("%s '%s'", streamCmd, escapedPath))
cmd := exec.Command(c.kubectlPath, args...)
```

Same pattern adds `--context` here too — that was missing before
(pre-existing bug).

## Tests

### New unit test

`cmd/root/kubectl/kubectl_test.go` — table-driven test of the helper:

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

### Existing tests preserved

- `TestNewKubectlK8sActions` (cmd/root/kubernetes/kubernetes_test.go:37)
  should still pass after the struct field addition.
- `cmd/root/kubectl/kubectl_test.go` existing tests (Execute, exec, etc.)
  should still pass after the args-building refactor — args are
  semantically identical when `kubeconfigPath` and `k8sContext` are empty.

### Sites without new tests

- `runPathDiscovery`, `CheckRBAC` — heavy shellout helpers with no
  existing tests in the codebase. Adding tests would mean refactoring
  for testability — out of scope for a fix.

## Manual verification

After all changes:

```
go fmt ./...
go vet ./...
golangci-lint run     # expect 0 issues
go build -o bin/ddc.exe .
go test -short ./...  # only integrationtest/kube fails (pre-existing)
```

Smoke test (optional):

```
.\bin\ddc.exe collect k8s standard --kubeconfig=C:\nonexistent\config --namespace=foo
```

Should fail at path discovery with a kubectl error mentioning the
supplied path, **not** the in-cluster fallback error message.

## Integration

**Amend the squash commit `f9ffdca`** — original message ("Prompt for
kubeconfig path when none is auto-detected") already covers
"make `--kubeconfig` work end-to-end" in spirit; no message change
needed.

```
git status --short                # confirm changed file set
# Restore CRLF noise files via `git checkout HEAD -- <noise files>`
git add <real-content-changes>
git commit --amend --no-edit
git log -1 --stat
```

The commit hasn't been pushed (`git status` shows no upstream), so
amending is safe.

## Files touched (additive to the squash commit)

| File | Change |
|---|---|
| `cmd/root.go` | `runPathDiscovery` signature gains `kubeconfig`; both kubectl shellouts inside use the new param; caller at `:997` updated |
| `cmd/root/kubernetes/kubernetes.go` | `CheckRBAC` signature gains `kubeconfigPath`; body rebuilds args with conditional `--kubeconfig`; caller at `cmd/root.go:483` updated |
| `cmd/root/kubectl/kubectl.go` | `CliK8sActions.kubeconfigPath` field; `k8sFlags()` helper; all 8+ shellouts use it; `NewKubectlK8sActions` passes `--kubeconfig` to early `current-context` call; `CanRetryTransfers` switches to `--client` |
| `cmd/root/kubectl/kubectl_test.go` | `TestCliK8sActions_k8sFlags` table-driven (4 cases) |

## Out-of-scope follow-ups (noted, not done)

- Generic `pkg/kubectlcmd` builder package (overkill for ~12 sites).
- Refactoring `runPathDiscovery` and `CheckRBAC` for testability.
- Bumping `Version = "4.0.0-beta1"` in `pkg/versions/version.go` to
  `4.0.0-beta3` (asked separately).
- Fixing `GetClientset`'s silent fallback when an explicit non-existent
  kubeconfig path is given (flagged in prior review; pre-existing in
  Task 4).
- The cosmetic "paths" → "path" plural fix in `promptKubeconfigPath`
  retry-exhaustion message.
