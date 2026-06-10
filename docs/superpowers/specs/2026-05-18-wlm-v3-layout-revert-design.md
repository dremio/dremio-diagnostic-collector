# WLM v3 Layout Revert — Design

**Date:** 2026-05-18
**Status:** Draft, pending user review
**Branch:** ddc_v4

## Background

The v4 redesign of DDC changed how WLM (Workload Management) files are emitted on disk. The new layout broke downstream consumers that rely on the v3 file naming convention.

**Current v4 layout (per WLM type):**

```
wlm/<host>/wlm_queues.json
wlm/<host>/wlm_rules.json
wlm/<host>/wlm_engines.json
wlm/<host>/wlm_cluster_usage.json
```

**Target layout (v3-compatible, with v4 per-host folder retained):**

```
wlm/<host>/queues.json
wlm/<host>/rules.json
wlm/<host>/engines.json
wlm/<host>/cluster_usage.json
```

The per-host folder (`wlm/<host>/` for K8s, `wlm/<host>-C/` for SSH coordinators) stays as-is. Only the `wlm_` filename prefix is dropped.

## Scope decisions

- **Unconditional revert.** No opt-in/opt-out flag. v4 is still 4.0.0-rc.1 and adding a permanent layout-selector flag adds long-term maintenance for what should be a one-time correction.
- **WLM only.** `cluster-stats`, `system-tables`, and `queries-perf` keep their current naming. They were not flagged as breaking downstream pipelines.
- **No documentation updates.** WLM filenames do not appear in `README.md`, `FAQ.md`, or `docs/`. No user-facing docs need editing.

## Implementation

### Source change

File: `cmd/root/collection/rockscollect.go`

The `wlmTypes` slice is dual-purpose today — its entries are both the `-type` argument to the remote `dremio-rocksdb-viewer` binary **and** the filename prefix on disk. The remote binary's expected `-type` values (`wlm_queues`, `wlm_rules`, `wlm_engines`, `wlm_cluster_usage`) cannot change. Only the on-disk filename must change.

The fix decouples the two by stripping the prefix when building the local filename:

```go
// Before (around line 230):
fname := fmt.Sprintf("%s.json", wt)

// After:
fname := strings.TrimPrefix(wt, "wlm_") + ".json"
```

`strings` is already imported in this file. `wlmTypes` itself is unchanged.

The path the file is written to is unchanged: `collectRocksType` already calls `CopyStrategy.CreatePath("wlm", host, nodeType)`, which yields the per-host directory used today.

### Test

New test in `cmd/root/collection/rockscollect_test.go`: `TestWLMFileLayout`.

The test verifies the on-disk layout produced by a full `RunRocksDBCollection` invocation with `CollectWLM: true`. It is intentionally an integration-style unit test (exercising `RunRocksDBCollection`, not just the filename construction) so future refactors of `wlmTypes` or `collectRocksType` cannot silently regress the contract.

**Stubs required:**

- A `Collector` stub that satisfies the methods `RunRocksDBCollection` invokes:
  - `HostExecute(false, host, "uname -m")` → returns `"x86_64\n"`.
  - `CopyToHost(host, localBin, "/tmp/dremio-rocksdb-viewer")` → returns success.
  - `HostExecute(false, host, "chmod +x /tmp/dremio-rocksdb-viewer")` → returns success.
  - `HostExecute(false, host, "/tmp/dremio-rocksdb-viewer -db <path> -type cluster_stats")` → returns `{"cluster":"stub"}` (so cluster_stats collection succeeds and doesn't pollute the assertion list).
  - `HostExecute(false, host, "/tmp/dremio-rocksdb-viewer -db <path> -type wlm_queues")` → returns `{"queues":[]}`.
  - Same pattern for `wlm_rules`, `wlm_engines`, `wlm_cluster_usage` with stub JSON payloads.
  - `HostExecute(false, host, "rm -f /tmp/dremio-rocksdb-viewer")` → returns success.
- A `CopyStrategy` stub whose `CreatePath("wlm", host, nodeType)` returns `<tempdir>/wlm/<host>` and creates the dir. (Same pattern as `mockCopyStrategy` in `streaming_collect_test.go:100`.)

**Args:** `RunRocksDBCollection(RocksCollectArgs{CollectWLM: true, CollectSystemTables: false, CollectQueriesPerf: false, Host: "dremio-master-0", NodeType: "coordinator", RocksDBDir: "/opt/dremio/data/db", ...})`.

**Positive assertions:**

```
<tempdir>/wlm/dremio-master-0/queues.json         exists with content {"queues":[]}
<tempdir>/wlm/dremio-master-0/rules.json          exists with content {"rules":[]}
<tempdir>/wlm/dremio-master-0/engines.json        exists with content {"engines":[]}
<tempdir>/wlm/dremio-master-0/cluster_usage.json  exists with content {"cluster_usage":[]}
```

**Negative assertion:** `filepath.Glob(<tempdir>/wlm/dremio-master-0/wlm_*.json)` returns an empty slice.

### Why not change `wlmTypes` itself

Two alternatives were considered and rejected:

1. **Rename `wlmTypes` entries to drop the prefix, then re-prefix when calling the viewer.** Adds a `"wlm_"+wt` at the call site — same number of lines, but the viewer's wire contract becomes implicit instead of explicit. Worse for someone reading the code cold.
2. **Replace `[]string` with `[]struct{viewerType, filename string}`.** Cleaner separation but overkill for four entries. `TrimPrefix` makes the relationship between the two values self-documenting in one line.

## Risks and rollback

- **Risk:** A downstream consumer of v4-rc has already adapted to the `wlm_*.json` names. Rollback is the inverse one-line change.
- **Risk:** Tests elsewhere assert on the `wlm_*.json` paths. Grep across the repo for `wlm_queues`, `wlm_rules`, etc. before merging; update or delete as appropriate. (Initial grep shows the strings appear only in `rockscollect.go` itself and in `docs/superpowers/plans/2026-04-15-pat-rearchitecture.md` (historical plan doc, no need to edit).)

## Out of scope

- Restructuring `RunRocksDBCollection` (e.g., moving WLM out of the per-coordinator loop).
- Changes to `cluster-stats`, `system-tables`, or `queries-perf` file layouts.
- Documentation updates (none required).
- A migration tool that renames already-collected v4-layout tarballs to v3 layout.
