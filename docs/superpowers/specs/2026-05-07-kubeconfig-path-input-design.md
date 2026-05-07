# Kubeconfig Path Input — Design

**Date:** 2026-05-07
**Status:** Approved (pending spec review)
**Branch:** ddc_v4

## Problem

When a user runs `ddc` interactively on a workstation that has no Kubernetes
config (no `~/.kube/config`, no `$KUBECONFIG`), selecting the Kubernetes
transport in the TUI fails with the unhelpful message:

```
unable to load in-cluster configuration, KUBERNETES_SERVICE_HOST
and KUBERNETES_SERVICE_PORT must be defined
```

…and DDC exits with a "completed" status screen but zero collected nodes.

The fix: when no kubeconfig is auto-detected, prompt the user for a path,
validate it, and thread that path through every subsequent kubeconfig
consumer. Also expose a `--kubeconfig` flag for non-interactive use.

## Goals

- Detect missing-or-empty kubeconfig before the TUI fails silently.
- Show an input field with an OS-aware example placeholder when needed.
- Validate the supplied path quickly (file/parse/contexts) and verify
  cluster connectivity before letting the user advance.
- Surface the chosen path in the generated reproducible-CLI command.
- Allow non-interactive use via a `--kubeconfig` flag on `K8sCmd` and
  `LocalK8sCmd` (covers k8s standard, k8s diagnosis, and local-k8s).

## Non-goals

- Multi-path `$KUBECONFIG` (colon-separated lists). The input takes a
  single file. Users with merged kubeconfigs continue to set the env var.
- Encrypted/vaulted kubeconfigs (vault, age, sops). These will fail
  validation as "invalid kubeconfig".
- A separate `--namespace` flag on `LocalK8sCmd`. The service-account
  namespace file remains the source there.
- A TUI flow for local-k8s. local-k8s stays flag-only.
- RBAC pre-flight inside the connectivity probe (the existing `checkRBAC`
  helper handles that later).
- Persisting the entered path across runs.

## Trigger condition

In the TUI's K8s branch, after the user selects "Kubernetes":

1. Call `kubernetes.ListContexts("")` (existing default-loading behavior).
2. If contexts come back → existing flow unchanged (no input shown).
3. If `len(contexts) == 0` → show the new path-input step.

This covers two failure modes with one check:
- No kubeconfig file at any default location.
- A file exists but defines zero contexts.

## TUI input step

Single-input `huh` form, shown immediately after Kubernetes is picked
(before the existing context picker and namespace picker).

- **Title:** `Kubeconfig file path`
- **Description:** `No Kubernetes config auto-detected. Enter the path
  to your kubeconfig file.`
- **Placeholder (OS-aware via `runtime.GOOS`):**
  - Windows → `C:\Users\you\.kube\config`
  - Linux/macOS → `/home/you/.kube/config`
- **Tilde expansion:** `~/...` and `~\...` are expanded to `$HOME` before
  validation, so Linux/macOS users typing `~/.kube/config` works.

### Validation — two layers

`huh.Validate` is synchronous, so a network probe inside it would freeze
the form. Split into:

**Layer 1 — inline `huh.Validate` (fast):**

The validator is extracted to a named exported function in
`cmd/configui/` so tests can target it directly without driving a `huh`
form:

```go
// cmd/configui/kubeconfig.go
package configui

// ValidateKubeconfigPath checks that the supplied path resolves to a
// loadable kubeconfig with at least one context. Tilde-expansion is
// applied before the file check.
func ValidateKubeconfigPath(s string) error {
    s = dirs.ExpandTilde(s)
    if s == "" {
        return fmt.Errorf("path is required")
    }
    if _, err := os.Stat(s); err != nil {
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

The form in `cmd/root.go` wires it in as `.Validate(configui.ValidateKubeconfigPath)`.

**Layer 2 — post-form connectivity probe (with spinner):**

After the form returns, run a `huh/spinner` titled `Verifying cluster
connectivity...` that calls a new helper:

```go
// VerifyConnectivity issues a cheap API call (namespace list) using the
// supplied kubeconfig + context to confirm the cluster is reachable.
func VerifyConnectivity(kubeconfigPath, k8sContext string) error
```

On failure:
- Print the error: `unable to reach cluster: <err>`
- Re-run the input form (max 3 retries, then exit cleanly with a
  cancellation message).
- Preserve the previously typed value as the form's starting input so the
  user can edit instead of retyping.

After connectivity passes, `kubeconfigPath` is set globally and the
existing flow continues: `ListContexts(kubeconfigPath)` is re-invoked to
populate the context picker (skipped if the file has only one context),
then namespace picker.

## New CLI flag

Package-level variable in `cmd/root.go`:

```go
var kubeconfigPath string
```

Registered on **both** transport commands (same global, two registrations):

```go
K8sCmd.PersistentFlags().StringVar(&kubeconfigPath, "kubeconfig", "",
    "path to kubeconfig file (overrides $KUBECONFIG and ~/.kube/config)")

LocalK8sCmd.PersistentFlags().StringVar(&kubeconfigPath, "kubeconfig", "",
    "path to kubeconfig file used when in-cluster config is unavailable")
```

A `KubeconfigPath string` field is added to `kubernetes.KubeArgs`,
populated from the global at `RemoteCollect` time so the lifecycle is
explicit rather than implicit-via-globals.

## Resolution precedence

New helper in `cmd/root/kubernetes/kubernetes.go`:

```go
// resolveKubeconfigPath returns the effective kubeconfig path with
// precedence:
//   1. explicit (--kubeconfig flag or TUI input), tilde-expanded
//   2. $KUBECONFIG env var, tilde-expanded
//   3. $HOME/.kube/config
// Returns "" if none could be determined.
func resolveKubeconfigPath(explicit string) string
```

This matches `kubectl --kubeconfig` precedence exactly. CLI flag wins
over env var.

**Tilde expansion** (`~/...`, `~\...`) is applied centrally inside this
helper so it covers all entry points: PowerShell users (no shell-side
tilde expansion) passing `--kubeconfig=~/.kube/config`, TUI users typing
`~/.kube/config` into the input field, and `$KUBECONFIG` set with a
tilde-prefixed value.

## Plumbing

Function signatures change:

| Before | After |
|---|---|
| `GetClientset(k8sContext string)` | `GetClientset(k8sContext, kubeconfigPath string)` |
| `ListContexts()` | `ListContexts(kubeconfigPath string)` |
| `GetClusters(k8sContext, labelSelector string)` | `GetClusters(k8sContext, labelSelector, kubeconfigPath string)` |
| `DiscoverPods(k8sContext, namespace, labelSelector string)` | `DiscoverPods(k8sContext, namespace, labelSelector, kubeconfigPath string)` |

Each implementation calls `resolveKubeconfigPath(kubeconfigPath)`
internally. `NewK8sAPI(kubeArgs, hook)` keeps its signature; it pulls the
path from `kubeArgs.KubeconfigPath` when calling `GetClientset`.

Call sites updated in `cmd/root.go`:
- `:490` — `GetClientset(k8sContext, kubeconfigPath)`
- `:897` — `ListContexts(kubeconfigPath)`
- `:929` — `GetClusters(k8sContext, labelSelector, kubeconfigPath)`
- `:1178` — `KubeArgs{ ..., KubeconfigPath: kubeconfigPath }`
- `:1521` — `DiscoverPods(k8sContext, namespace, labelSelector, kubeconfigPath)`

## local-k8s integration (flag-only)

Today's flow at `cmd/root.go:382-424` is "in-cluster only or skip." It is
extended to fall back to kubeconfig:

```
1. Try rest.InClusterConfig()                        ← unchanged primary
2. If that fails:
     If resolveKubeconfigPath(kubeconfigPath) != "":
       try kubernetes.GetClientset("", kubeconfigPath)
       on success: use that clientset
     If still no luck: log warning, skip cluster collection (existing
       degrade-gracefully behavior)
3. Namespace detection:
     try /var/run/secrets/kubernetes.io/serviceaccount/namespace  ← unchanged
     if missing AND we used the kubeconfig fallback:
       use the current-context's default namespace from the kubeconfig
     if still empty: log warning, skip cluster collection
```

No TUI input is added for local-k8s. Laptop-against-remote-cluster users
pass `--kubeconfig=/path` directly on the command line. This keeps scope
tight and matches the choice in Question 5 (option A).

The local file collection (the primary purpose of local-k8s) is
unaffected — only the optional cluster-resource and container-log
collection is gated on a usable clientset.

## CLI command preview

Add to `CLICommandConfig`:

```go
Kubeconfig string // K8s/local-k8s — emitted only if non-empty
```

Emit logic in `GenerateCLICommand`:

```go
if c.Kubeconfig != "" {
    parts = append(parts, fmt.Sprintf("--kubeconfig=%s", c.Kubeconfig))
}
```

**Position:** above `--namespace`. The flag goes immediately after the
mode word, before any other transport flag. Resulting order:

```
ddc collect k8s standard \
  --kubeconfig=/path/to/config \
  --namespace=mynamespace \
  --context=mycontext \
  ...
```

**When is `Kubeconfig` populated?**
Only when the user **explicitly supplied a non-default path**:

- Typed into the new TUI input, or
- Passed `--kubeconfig=...` on the original command line and that value
  flowed through to the preview.

If the path was auto-detected (env or `~/.kube/config`), the field stays
empty and the flag is omitted. Matches the rule "if the default
Kubernetes config (home or any other pulled default) is not being used,
the generated shown command will receive an additional flag."

## Tests

### Unit — `cmd/root/kubernetes/`

- `TestResolveKubeconfigPath` — table-driven precedence
  - explicit set → returns explicit
  - explicit empty, env set → returns env
  - both empty, home exists → returns `~/.kube/config`
  - all empty → returns ""
- `TestListContexts_ExplicitPath` — write a temp kubeconfig, point at it
  via the explicit param, assert contexts and current-context match
- `TestGetClientset_ExplicitPathOverridesEnv` — set `$KUBECONFIG` to one
  temp file, pass a different temp file as the explicit param, assert
  the explicit one wins (verified by inspecting the resulting `*rest.Config`)

### Unit — `cmd/`

- `TestGenerateCLICommand_KubeconfigEmitted` — `Kubeconfig: "/foo/bar"`
  produces `--kubeconfig=/foo/bar`
- `TestGenerateCLICommand_KubeconfigOmittedWhenEmpty` — empty field
  produces no flag
- `TestGenerateCLICommand_KubeconfigPositionedAboveNamespace` — assert
  `--kubeconfig` appears before `--namespace` in the output

### Unit — `cmd/configui/` (or co-located)

- `TestKubeconfigInputValidation` — table-driven against the `Validate`
  callback extracted to a named function:
  - empty path → error
  - non-existent file → error
  - malformed YAML → error
  - YAML with zero contexts → error
  - valid kubeconfig with one context → ok
  - valid kubeconfig with multiple contexts → ok
- `TestExpandTilde` (in `pkg/dirs/dirs_test.go`) — `~/foo`, `~\foo`, `~` only, no-tilde paths, paths with `~user` (left untouched — not supported)

### Integration

- `TestLocalK8sFallbackToKubeconfig` — simulate `rest.InClusterConfig()`
  failure (env vars unset), provide a temp kubeconfig via the flag,
  assert `GetClientset` succeeds. Mock the actual cluster API.

No new `huh` form integration tests — the existing TUI doesn't have
those, and adding a framework for them is out of scope.

## Files touched

| File | Change |
|---|---|
| `cmd/root/kubernetes/kubernetes.go` | new `resolveKubeconfigPath`; signatures of `GetClientset`, `ListContexts`, `GetClusters`, `DiscoverPods`; new `VerifyConnectivity`; new `KubeconfigPath` field on `KubeArgs` |
| `cmd/root.go` | new `kubeconfigPath` global; `--kubeconfig` on `K8sCmd` and `LocalK8sCmd`; new TUI input step with 3-retry connectivity loop; thread path through call sites; local-k8s in-cluster→kubeconfig fallback chain |
| `cmd/cli_generator.go` | new `Kubeconfig` field on `CLICommandConfig`; emit logic; ordering |
| `cmd/cli_generator_test.go` | three new test cases |
| `cmd/root/kubernetes/kubernetes_test.go` | precedence + explicit-path tests; tilde-expansion test |
| `cmd/root/kubernetes/list_contexts_test.go` | explicit-path test |
| `cmd/configui/kubeconfig.go` (new) | `ValidateKubeconfigPath` (uses `dirs.ExpandTilde`) |
| `cmd/configui/kubeconfig_test.go` (new) | validation table tests |
| `pkg/dirs/dirs.go` | new `ExpandTilde(path string) string` helper, shared between `kubernetes` and `configui` packages |
| `pkg/dirs/dirs_test.go` | tilde-expansion table tests (POSIX + Windows) |

## Open questions / risks

- **`huh` form retry loop on connectivity failure.** Re-running
  `Form.Run()` from inside the same code path is supported (forms are
  immutable per run), but we need to confirm the TUI cleanly clears and
  redraws between attempts. Mitigation: print a divider line + the error
  before re-running.
- **Tilde-expansion correctness on Windows.** Windows shells don't
  natively expand `~`, but if the user types it into the TUI we should
  still handle it. `os.UserHomeDir()` returns the right path on both
  platforms; helper just needs to substitute the leading `~` token.
- **`KubeArgs.KubeconfigPath` propagation.** A handful of internal helpers
  may construct `KubeArgs` ad-hoc; need to grep for `KubeArgs{` during
  implementation to ensure none miss the new field.
