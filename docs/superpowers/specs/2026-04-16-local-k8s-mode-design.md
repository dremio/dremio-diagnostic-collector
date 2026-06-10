# Design: local-k8s Collection Mode

**Date:** 2026-04-16
**Status:** Approved

## Purpose

Introduce a `local-k8s` collect mode that runs directly from the coordinator/master pod. It combines local filesystem collection (Dremio application logs, JVM diagnostics) with K8s API collection (cluster resources, container logs) — without connecting to or distributing binaries to executor pods.

## Command Tree

```
ddc collect local-k8s standard
ddc collect local-k8s diagnosis
```

Added as a sibling to the existing `local`, `k8s`, and `ssh` commands under `collect`.

## Approach

Reuse the existing `LocalCollector` for all per-node filesystem collection. Add a new branch in `RemoteCollect()` that additionally sets up a `clusterCollect` closure using an in-cluster K8s clientset — the same pattern the existing k8s mode uses. No new Collector type is needed. No changes to existing k8s, local, or ssh code paths.

## What local-k8s Collects

### From the local filesystem (coordinator only)

Same as existing `local` mode:

- Dremio application logs (server.log, queries.json, etc.)
- GC logs
- dremio.conf, dremio-env
- JVM diagnostics in diagnosis mode (JFR, jstack, top, async-profiler, heap dump)
- Node info (OS, disk, JVM)

### From the K8s API (cluster-level)

Same set as existing `k8s` mode:

- **Cluster resources:** nodes, storage classes, PVCs, PVs, services, endpoints, pods, deployments, statefulsets, daemonsets, replicasets, cronjobs, jobs, events, ingresses, limit ranges, resource quotas, HPAs, PDBs, priority classes
- **Container logs (current + previous):** All pods in the auto-detected namespace, no label selector filtering
- **Previous logs for restarted pods:** All pods in namespace with RestartCount > 0

### Container log behavior by mode

| Mode | Container logs |
|------|---------------|
| `standard` | Previous logs for restarted pods only (unconditional, same as k8s) |
| `diagnosis` | Previous logs for restarted pods + current logs for all pods |

## Flag Scoping

### LocalK8sCmd PersistentFlags (transport-specific)

| Flag | Default | Description |
|------|---------|-------------|
| `--dremio-home` | `/opt/dremio` | Dremio installation directory |
| `--local-log-dir` | (auto-detected) | Override log directory path |

### Inherited from CollectCmd PersistentFlags

`--output-file`, `--collection-threads`, `--collector-timeout`, `--progress`, path overrides (`--coordinator-log-dir`, `--executor-log-dir`, `--dremio-conf-dir`, `--dremio-rocksdb-dir`), `--allow-insecure-ssl`, `--disable-free-space-check`

### Mode-specific flags

Same as other transports: standard gets per-log num-days flags; diagnosis gets JVM diagnostic flags (`--diag-jfr`, `--diag-jstack`, `--diag-top`, `--diag-async-profiler`, `--diag-heap-dump`, `--diag-time-seconds`).

### No K8s-specific flags

`--namespace`, `--context`, `--label-selector`, `--enable-kubectl` are NOT available on local-k8s. Namespace is auto-detected from the pod's service account.

## RemoteCollect Routing

A new branch in `RemoteCollect()`, triggered by a `localK8sEnabled` boolean (derived from the command path `local-k8s`):

1. Create `LocalCollector` (identical to local mode)
2. Read namespace from `/var/run/secrets/kubernetes.io/serviceaccount/namespace`
3. Create in-cluster K8s clientset via `rest.InClusterConfig()`
4. If K8s client succeeds, set up `clusterCollect` closure:
   - `ClusterK8sExecute(namespace, clientset, ...)`
   - `GetPreviousLogsForRestartedPods(namespace, clientset, ..., "")` — empty label selector = all pods
   - In diagnosis mode: `GetClusterLogs(namespace, clientset, ..., "")` — empty label selector = all pods
   - Each call: on error, log to ddc.log and continue
5. If K8s client or namespace detection fails, log warning to ddc.log and leave `clusterCollect` as no-op

No changes to the existing local, k8s, or ssh branches.

## Namespace Auto-Detection

Read from the standard Kubernetes service account path:

```
/var/run/secrets/kubernetes.io/serviceaccount/namespace
```

This file is mounted automatically by Kubernetes into every pod (unless `automountServiceAccountToken: false`). No `--namespace` flag is needed.

## Graceful Degradation

All K8s API access is best-effort. Failures are logged to ddc.log and collection continues.

| Failure scenario | Behavior |
|---|---|
| Not running inside a pod (namespace file unreadable) | Skip all K8s collection, log warning |
| `rest.InClusterConfig()` fails | Skip all K8s collection, log warning |
| Service account lacks RBAC permissions | Per-resource/per-pod skip with log entry |
| Firewall blocks API server | 60-second timeout per call, log timeout cause, continue |
| Partial RBAC (some resources accessible) | Collect what succeeds, skip what fails |

Log message format for full K8s skip:
```
local-k8s: K8s API unavailable (<reason>) — skipping cluster resource and container log collection
```

## Timeout Handling

The existing `cluster.go` functions already use 60-second timeouts per K8s API call via `context.WithTimeoutCause`. The `clusterRequestTimeout` for container log streaming is 120 seconds. These existing timeouts satisfy the 60-second requirement for resource collection; container log streaming uses the existing 120-second value.

## TUI Integration

| TUI Step | local-k8s behavior |
|---|---|
| Transport selection | `local-k8s` appears as fourth option |
| Transport-specific config | Same as `local` — no namespace/context prompts |
| Path discovery | Same as `local` — `pgrep`/`ps` to find Dremio PID |
| Node selection (diagnosis) | None — single local coordinator |
| Mode-specific config | Same as `local` for both modes |

`StandardConfig` and `DiagnosisConfig` carry `Transport: "local-k8s"` through to `RemoteCollect()`.

## CLI Generator

The CLI generator emits commands like:

```
ddc collect local-k8s standard --dremio-home /opt/dremio
ddc collect local-k8s diagnosis --dremio-home /opt/dremio --diag-time-seconds 30
```

Same flags as `local` mode — no K8s-specific flags.

## Archive Output Structure

| Source | Archive path |
|---|---|
| Local Dremio logs | `coordinator/logs/server.log`, etc. (same as local mode) |
| K8s cluster resources | `kubernetes/events.json`, `kubernetes/pods.json`, etc. |
| Container logs | `kubernetes/container-logs/<pod>-<container>.txt` |
| Previous container logs | `kubernetes/container-logs/<pod>-<container>-previous.txt` |

## Console Output

`consoleprint.UpdateCollectionArgs(...)` displays: `"mode: local-k8s, namespace: <auto-detected>"`.

## Files to Modify

| File | Change |
|---|---|
| `cmd/root.go` | Add `LocalK8sCmd`, `LocalK8sStandardCmd`, `LocalK8sDiagnosisCmd`; register flags; add `RemoteCollect` branch; add `localK8sEnabled` routing; register in command tree |
| `cmd/configui/configui.go` | Add `local-k8s` to transport picker; route config screens |
| `cmd/cli_generator.go` | Handle `local-k8s` transport in CLI command generation |

## Files NOT Modified

| File | Reason |
|---|---|
| `cmd/root/local/local.go` | Reused as-is via `LocalCollector` |
| `cmd/root/collection/cluster.go` | Called with different parameters, not modified |
| `cmd/root/collection/streaming_collect.go` | Transport-agnostic, works with any Collector |
| `cmd/root/kubernetes/` | Not used by local-k8s (it uses `rest.InClusterConfig()` directly) |
