# Separate detection and container-log label selectors

**Status:** Approved
**Date:** 2026-05-26
**Related:** GitHub issue #335, commit `585380a` (initial fix that restored v3 namespace-wide container log collection)

## Problem

`--label-selector` (`-l`) currently serves two conceptually different concerns under one flag:

1. **Detection** — identifying which pods are Dremio coordinators/executors so the collector can stream files from them.
2. **Container log filtering** — choosing which pods' container logs end up in `kubernetes/container-logs/`.

This conflation produced two bugs in succession:

- **Pre-`585380a`**: container log collection applied the detection selector, silently dropping every non-Dremio pod (catalog-server, opensearch, mongodb, nats, operators) — issue #335.
- **Post-`585380a`**: container log collection is unconditionally namespace-wide, with no way to scope it. An engineer who wants to debug a single pod cannot.

Neither end of the dial is correct. The two concerns need separate controls.

## Goals

- Detection always has a sensible default; users with non-standard deployments can override.
- Container log collection defaults to namespace-wide (preserving the issue #335 fix), but users can opt into pod-scoped collection.
- Each flag has one clear purpose; help text and behavior are unambiguous.
- The change is implementable with real unit tests rather than the tautological fake-client tests that previously existed for this code path.

## Non-goals

- Filtering of namespace-wide K8s resource dumps in `ClusterK8sExecute` (`pods.json`, `events.json`, etc.). Those serve a cluster-context role distinct from per-pod log collection.
- TUI changes. The config UI does not currently expose the label selector and will not start to.
- Backwards-compatibility aliases. v4 is still RC; the rename is a clean break.

## Design

### Flags

| Flag | Short | Default | Purpose | Consumers |
|---|---|---|---|---|
| `--detect-label-selector` | *(none)* | `role=dremio-cluster-pod` | Identifies Dremio coordinator/executor pods for file streaming. | `kubernetes.DiscoverPods`, `kubernetes.GetClusters`, `KubeCtlAPIActions` list operations, `CliK8sActions` kubectl pod listing. |
| `--container-log-label-selector` | `-l` | *(empty)* | Filters which pods' container logs are collected. Empty → list every pod in the namespace. | `collection.GetClusterLogs`, `collection.GetPreviousLogsForRestartedPods`. |

`--label-selector` is **removed**. Passing it produces cobra's standard "unknown flag" error. CHANGELOG and release notes document the replacements.

### Behavior matrix

| User invocation | Detection behavior | Container-log behavior |
|---|---|---|
| (no flags) | `role=dremio-cluster-pod` (default) | namespace-wide |
| `-l app=foo` | `role=dremio-cluster-pod` (default) | filtered to `app=foo` |
| `--detect-label-selector app=foo` | `app=foo` | namespace-wide |
| `--detect-label-selector statefulset.kubernetes.io/pod-name=dremio-master-0 -l statefulset.kubernetes.io/pod-name=dremio-master-0` | scoped to master-0 | scoped to master-0 |

The last row is the engineer's targeted-debug case — explicit but unambiguous.

### Code structure

**`cmd/root.go`**
- Remove `labelSelector` package variable and its `RegisterPersistentFlags` entry.
- Add `detectLabelSelector` variable, default `"role=dremio-cluster-pod"`, registered as `--detect-label-selector`.
- Add `containerLogLabelSelector` variable, default `""`, registered as `-l, --container-log-label-selector`.
- Where `KubeArgs.LabelSelector` was populated, populate `KubeArgs.DetectLabelSelector` instead.
- Pass `containerLogLabelSelector` as a new parameter into `collection.GetClusterLogs` and `collection.GetPreviousLogsForRestartedPods` at all four call sites (lines 436, 440, 523, 528).

**`cmd/root/collection/cluster.go`**
- Re-add a `labelSelector string` parameter to `GetClusterLogs` and `GetPreviousLogsForRestartedPods`. Empty → `metav1.ListOptions{}`. Non-empty → `metav1.ListOptions{LabelSelector: labelSelector}`.
- **Change both signatures** to accept `kubernetes.Interface` instead of `*k8sapi.Clientset`. The implementations only call `.CoreV1().Pods(...).List(...)` and `.GetLogs(...)`, both available on the interface. This unblocks real unit tests via `fake.NewSimpleClientset` (the previous tautology tests existed because the concrete type couldn't be faked).
- Doc comments updated to explain the empty-vs-set semantics and to make clear that the filter does NOT cascade to K8s resource dumps.

**`cmd/root/kubernetes/kubernetes.go`**
- Rename `KubeArgs.LabelSelector` field to `DetectLabelSelector`.
- Rename internal `labelSelector` field on `KubeCtlAPIActions` to `detectLabelSelector` for consistency with the exported field.

**`cmd/root/kubectl/kubectl.go`**
- Rename `KubeArgs.LabelSelector` consumer to `DetectLabelSelector`.
- Rename internal `labelSelector` field on `CliK8sActions` to `detectLabelSelector`.

**`cmd/cli_generator_test.go:196`**
- Update the local-k8s-mode assertion: `--label-selector` is gone; assert that neither `--detect-label-selector` nor `--container-log-label-selector` appears in the generated command for local-k8s mode (their absence in that mode is the existing invariant — only the flag names change).

**`cmd/root/kubernetes/kubernetes_test.go`, `cmd/root/kubectl/kubectl_test.go`**
- Update references to renamed `KubeArgs` field and any internal field rename.

**`README.md:154`**
- Replace the single `-l, --label-selector` row with two rows for `--detect-label-selector` and `-l, --container-log-label-selector` with their respective defaults and descriptions.

**`CHANGELOG.md`**
- Add a "Breaking" entry under the next v4 release header noting:
  - `--label-selector` has been split into `--detect-label-selector` and `--container-log-label-selector`.
  - `-l` short form now maps to `--container-log-label-selector`.
  - Migration: replace any existing `-l <selector>` with the appropriate new flag(s); see help text.

### Tests

**New unit tests in `cmd/root/collection/cluster_test.go`** (file replaces the deleted `cluster_label_selector_test.go`):

- `TestGetClusterLogs_EmptySelector_ListsAllNamespacePods` — fake clientset with three pods (two Dremio, one ecosystem); assert pod-list action's restrictions are empty; assert all three pods' logs are read.
- `TestGetClusterLogs_NonEmptySelector_FiltersPods` — same setup with selector `role=dremio-cluster-pod`; assert only matching pods' logs are read.
- `TestGetPreviousLogsForRestartedPods_EmptySelector_ListsAllNamespacePods` — analogous to the first.
- `TestGetPreviousLogsForRestartedPods_NonEmptySelector_FiltersPods` — analogous to the second.

These are real tests that drive the functions through `fake.NewSimpleClientset` and assert on both the recorded list-action restrictions and the resulting on-disk file set.

**Integration tests** (`integrationtest/kube/collect_kube_test.go`)
- No changes required. Existing invocations don't pass `-l`, so the default-empty container-log selector yields the same namespace-wide 14-file output committed in `585380a`.
- Optional follow-up (out of scope for this design): add an integration test that exercises `-l` and asserts scoped container-logs output. Worth doing later; not blocking.

### Migration path

Users discover the change via cobra's unknown-flag error on first run with a v4 RC ≥ this change. CHANGELOG + release notes + updated CLI help text are the documentation channel. No silent behavior shift — anyone who was using `--label-selector` gets a clear error and a documented replacement.

## Risks

- **Engineers with scripted runs**: anyone with `-l <selector>` in CI or runbooks gets an error or — worse for them — a working command with new semantics (their `-l` no longer scopes detection; it scopes container logs). This is the intentional consequence of the rename and is documented as a breaking change. Mitigation: CHANGELOG entry with explicit migration examples.
- **Signature change to `kubernetes.Interface`**: low risk; only two functions, both with simple usage. Existing call sites pass `*k8sapi.Clientset` which already satisfies `kubernetes.Interface` — no caller-side changes beyond the existing parameter additions.
- **Field rename `KubeArgs.LabelSelector → DetectLabelSelector`**: mechanical, compiler-enforced. The build will fail on any miss.

## Acceptance criteria

- `go build ./...` passes.
- New unit tests in `cluster_test.go` pass.
- `go test ./cmd/...` is green.
- `./ddc collect k8s standard --namespace foo` (no `-l`) produces a `kubernetes/container-logs/` directory containing entries for every pod in the namespace, including non-Dremio pods (verifies issue #335 fix preserved).
- `./ddc collect k8s standard --namespace foo -l role=dremio-cluster-pod` produces a `kubernetes/container-logs/` directory containing entries only for pods matching the selector.
- `./ddc collect k8s standard --namespace foo --label-selector role=dremio-cluster-pod` exits with cobra's unknown-flag error.
- README and CHANGELOG reflect the new flags.
