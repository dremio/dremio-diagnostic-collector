# Log-path autodetection, RocksDB-catalog gating, and container-name memoization

Date: 2026-06-25

## Problem

A production `collect k8s standard` run (bundle `capgroup-prod-ddc.tar.gz`) collected **no
server logs and no `queries.json`**, and **no `queries-perf`** data. Investigation of the
bundle's `ddc.log` found three independent defects.

### 1. Log directory mis-resolved (CLI skips `-Ddremio.log.path` autodetection)

`queries.json` and `server.log` are written by Dremio's logback file appenders to
`${dremio.log.path}`. On this cluster the Helm config sets, via
`DREMIO_JAVA_SERVER_EXTRA_OPTS`, `-Ddremio.log.path=/opt/dremio/data/log`, while
`DREMIO_LOG_DIR=/opt/dremio/log` only governs GC logs and `server.out`.

DDC resolves the log directory two different ways:

- **TUI (`ddc` interactive):** `runPathDiscovery` / `runLocalPathDiscovery` in `cmd/root.go`
  run `ps eww <pid>` and parse `-Ddremio.log.path=` (then `DREMIO_LOG_DIR=`). This is gated
  behind `if !skipPromptUI` (`cmd/root.go:1033`) — **interactive mode only**.
- **CLI (`ddc collect …`):** `skipPromptUI == true`, so the parser never runs. The streaming
  collector falls back to `RunDiscovery` → `probeDir` (`cmd/root/collection/discovery.go`),
  which walks a fixed candidate list
  `["/var/log/dremio", "/opt/dremio/log", "/opt/dremio/data/log"]` and returns the **first
  directory containing any file**.

On the coordinators/master, probing matched `/opt/dremio/log` (it holds GC logs, `server.out`,
and an `hs_err_*` crash file) and stopped there — **`/opt/dremio/data/log` was never tested**
(grep count 0 in the log). Everything in `/opt/dremio/log` was then dropped (GC logs disabled,
`server.out` blocked), so the bundle has no `logs/` directory at all.

### 2. `queries-perf` (and all rocksdb-viewer collections) run on nodes with no catalog

`streaming_collect.go:976` runs RocksDB-viewer collection on **every** `nodeType ==
"coordinator"`. The RocksDB catalog (KV store) exists only on the **master** coordinator;
scale-out `dremio-coordinator-*` pods connect to it remotely and have no local catalog. On
`dremio-coordinator-0/1` the viewer failed with
`No such file or directory: /opt/dremio/data/db/catalog/CURRENT`.

### 3. `getContainerName` is uncached → fragile under transient API throttling

`getContainerName` (`cmd/root/kubectl/kubectl.go`) runs a fresh
`kubectl get pods <pod> -o jsonpath={.spec.containers[*].name}` on **every** `HostExecute`.
For `dremio-master-0` the log shows **73** such calls. A clustered ~17-second `kubectl get
pods` brown-out (07:50:40–57) produced **10 consecutive "unable to get container name"
failures**, which aborted `queries-perf` (and several system-table / WLM collections) on the
master. The pod was reachable throughout — `sys.reflections` was successfully collected at
07:50:38, an exec mid-window — so the failure is in the redundant `get pods` read path, not
the pod or the exec path. master-0's first `get pods` succeeded at 07:03:31.

## Goals

- Auto-detect the Dremio log directory in **both** TUI and CLI modes, per node.
- Run RocksDB-viewer collections only on the node that actually holds the catalog.
- Make K8s container-name resolution resilient to transient API failures.

## Non-goals

- Changing what logback writes or where Dremio is configured to log.
- Reworking the TUI pre-config-screen discovery beyond sharing one helper.
- Broader retry/throttling policy changes outside container-name resolution.

---

## Part A — Per-node log-directory autodetection (`cmd/root/collection/discovery.go`)

Move the log-dir resolution into `RunDiscovery` so it runs for both TUI actual-collection and
CLI, per node (executors detect their own path independently).

### Resolution order

1. **Explicit flag** — if the `logDir` parameter is non-empty (from
   `--coordinator-log-dir` / `--executor-log-dir`), use it directly and skip detection.
   *(Explicit user override wins outright.)*
2. **`-Ddremio.log.path=`** — parsed from the live Dremio process; use it **only if the
   directory exists and is non-empty**.
3. **`DREMIO_LOG_DIR=`** — parsed from the process environment; use if non-empty.
4. **Probe** — existing `probeDir` over the candidate list.

"Non-empty" reuses the directory-has-a-file check currently inline in `probeDir`, factored out
to `dirHasFiles(executor, host, dir) bool`. If a detected directory is empty, fall through to
the next step (this is exactly the `/opt/dremio/data/log` vs `/opt/dremio/log` case).

### Implementation notes

- **Reorder `RunDiscovery`** so PID discovery happens *first*; the resolved PID feeds log-dir
  resolution. If PID is 0 (not found), skip steps 2–3 and probe; log a line noting the
  fallback.
- **`readProcessInfo(executor, host, pid) string`** — new helper. Pipe-free, to stay
  transport-safe across K8s (`sh -c "<joined>"`), SSH (remote shell), and Local (`sh -c`):
  - Primary: `executor(host, "ps", "eww", pid)` — one command, returns JVM args **and** env in
    one blob. Same shape as the existing `discoverPID` `pgrep -f 'dremio.*java'` call, which is
    confirmed working on every node in the source bundle's `ddc.log`.
  - Fallback (busybox / no `ps`): two separate calls `cat /proc/<pid>/cmdline` and
    `cat /proc/<pid>/environ`; split the NUL separators **in Go** (`strings.Split(out,
    "\x00")`) — no `tr`, no pipe, no shell operators.
- **`extractEnvValue`** moves from `cmd/root.go` into the `collection` package so both
  `RunDiscovery` and the TUI's `runPathDiscovery` use one copy. Its existing `strings.LastIndex`
  semantics are required here: the launcher derives `-Ddremio.log.path` from `DREMIO_LOG_DIR`,
  then `DREMIO_JAVA_SERVER_EXTRA_OPTS` overrides it, and the JVM honors the last occurrence.
- `cmd/root.go`'s `runPathDiscovery` / `runLocalPathDiscovery` keep their current role
  (config-screen default display) but call the moved `extractEnvValue`.

### Tests

`discovery_test.go`, table-driven via the existing `mockExecutor`:
- explicit flag wins (detection not invoked);
- `-Ddremio.log.path` honored when dir non-empty;
- detected-but-empty dir falls through to `DREMIO_LOG_DIR`, then to probe;
- `ps` missing → `/proc` fallback parses cmdline/environ;
- PID 0 → probe path.

---

## Part B — RocksDB-catalog gating (`cmd/root/collection/rockscollect.go`)

Gate **all** rocksdb-viewer collections (cluster_stats, system-tables, WLM, queries-perf) to
the node that holds the catalog, detected by presence of the catalog `CURRENT` file.

### Change

Add a pre-flight check at the top of `RunRocksDBCollection`, **before** the binary upload.
`dbPath` is already `RocksDBDir + "/catalog"`, so the marker is `<dbPath>/CURRENT`:

```go
// The RocksDB catalog (KV store) lives only on the master coordinator.
// Scale-out coordinators connect to it remotely and have no local catalog,
// so rocksdb-viewer cannot read it there — skip them silently.
catalogCurrent := dbPath + "/CURRENT"
if out, err := c.HostExecute(false, host, "test", "-f", catalogCurrent, "&&", "echo", "exists");
    err != nil || !strings.Contains(out, "exists") {
    simplelog.Infof("rocksdb: no catalog at %s on %s — skipping RocksDB-viewer collection (not a master coordinator)", catalogCurrent, host)
    return nil, nil
}
```

### Why

- Returning `(nil, nil)` makes the caller (`streaming_collect.go:996`) append zero files and
  log nothing — a clean, silent skip, not an error. Avoids the wasted binary upload + chmod +
  cleanup on secondary coordinators.
- The early return precedes the first `"Uploading dremio-rocksdb-viewer"` status update, so
  secondary coordinators show no misleading RocksDB progress line.
- `test -f … && echo exists` is the same pattern `probeDir` already uses — transport-safe
  (joined and run through one shell; no nested `sh -c`).
- Centralizing in `RunRocksDBCollection` covers all callers; `streaming_collect.go` is
  unchanged except for updating the `// coordinator only` comment.
- Transport-agnostic: SSH/Local single-coordinator or embedded-master deployments have
  `CURRENT` on that node and proceed as before.

### Tests

`rockscollect` test (mock collector): `CURRENT` present → collection proceeds; absent →
returns `(nil, nil)` and no binary upload is attempted.

---

## Part C — Memoize container name (`cmd/root/kubectl/kubectl.go`)

Resolve each pod's container name once and reuse it, mirroring the existing `pidHosts` cache
pattern already on `CliK8sActions` (it has `pidHosts map[string]string` and `m sync.Mutex`).

### Change

- Add `containerNames map[string]string`, guarded by the existing `m` mutex (initialize where
  `pidHosts` is initialized).
- `getContainerName` checks the cache first; on miss it resolves once and stores the result.
  The mutex guards all map access (read and write); the resolution call itself runs
  outside the lock, so two goroutines hitting a cold miss for the same pod may both
  resolve — a benign, idempotent double-resolve (last write wins, same value), not a
  data race.
- Wrap the single (cold-miss) resolution call in a small inline retry gated on
  `retriesEnabled` so the now-once-per-pod call is not a new single point of failure.
  (`addRetries` adds a `cp`-specific kubectl `--retries` flag and is not applicable to
  `get pods`, so the resolution uses an inline retry loop instead.)

### Why this resolves the observed failure

The container name is cached from the first successful resolution (master-0: 07:03:31), so the
07:50 `get pods` brown-out is never reached by later calls — `queries-perf` goes straight to
`kubectl exec`, which the in-window `sys.reflections` success shows was working. This
eliminates ~72 of 73 `get pods` calls for master-0.

### Tests

`kubectl_test.go`: a second `getContainerName` call for the same pod issues no new `kubectl`
invocation (assert the mock executor's call count).

---

## Risks

- **Part A:** `ps eww` may be absent or restrict reading another PID's environ on minimal
  images; the `DREMIO_LOG_DIR` step (3) then depends on the `/proc` fallback. Irrelevant to the
  source cluster (step 2 wins) and covered by the fallback. NUL-through-stdout handling is
  confined to the rarely-hit `/proc` path and handled in Go.
- **Part C:** confidence rests on the failures being a transient `get pods` brown-out rather
  than a pod outage (strongly supported: an exec succeeded mid-window, and the cache warms 47
  minutes earlier). The unobservable residual is whether the `queries-perf` *exec* itself would
  have succeeded at 07:50:56.

## Build / verification

After implementation: `go build -o bin/ddc.exe .` and `go test -short ./...` for the affected
packages (`cmd/root/collection/...`, `cmd/root/kubectl/...`).
