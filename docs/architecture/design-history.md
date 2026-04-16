# DDC v4 Design History

This document traces the development arc of DDC v4 across 18 milestones, organized by phase. Each phase describes what changed, why, and the key decisions that shaped the outcome.

For individual decision details, see [decisions.md](decisions.md). For patterns discovered during development, see [patterns-and-gotchas.md](patterns-and-gotchas.md).

## Phase 1: Foundation (M001-M005)

*Replaced the legacy push-binary-collect-tarball model with direct file streaming and remote JVM collection.*

### M001: Orchestrator-driven collection begins

The first milestone moved cluster-stats.json collection from per-node local-collect to the orchestrator. Cluster stats are cluster-wide data (same on every node), so collecting from each node was redundant. The orchestrator scrapes the Dremio HTML homepage for the embedded JSON config — no PAT required (D001, D003). This established the pattern of collecting data at the orchestrator level when possible.

### M002: Dead code removal

Removed ~5,600 lines of dead streaming/resume infrastructure. Standardized async-profiler flags to match the JFR pattern, renamed date flags for alphabetical adjacency, and consolidated redundant free-space-check flags. This cleanup surfaced the *preservation boundary pattern* — when deleting code that shares naming with preserved code, you must explicitly enumerate what to keep.

### M003: Direct file streaming transport

The architectural heart of DDC v4. Replaced the old model (push ddc binary to node, run local-collect, create tarball, copy back) with direct streaming via `kubectl exec cat` / `ssh cat` (D008). Built RunDiscovery as a shared function with HostExecutor callback for both K8s and SSH transports (D017). Added per-file progress tracking, dual-hash checksum verification (sha256sum -> md5sum -> skip fallback, D010), and client-side rate limiting using TCP backpressure (D011).

Net result: 215 files changed, ~9,400 lines removed. The codebase became simpler with a more reliable collection architecture.

### M004: Remote JVM collection

Moved all six JVM diagnostic tools (jstack, ttop, JVM flags, JFR, heap dump, async-profiler) from local-collect to orchestrator-driven remote execution. Binary-output tools (JFR, heap dump, asprof) write to temp files then stream back; text tools capture stdout directly (D014). JVM commands launch in parallel goroutines across all nodes with ~1s skew tolerance (D015) so users get correlated diagnostic data.

### M005: Post-migration cleanup

Removed ~400 lines of dead code including `CopyFromHost` (but preserved `CopyToHost` — research caught it was still used by async-profiler, D025). Simplified local-collect for standalone use and rewrote the README. This completed the migration to the streaming architecture.

Cumulative impact of Phase 1: ~216 files changed, ~18,000 lines removed. DDC went from a complex push-binary model to a clean streaming architecture with remote JVM collection.

## Phase 2: Reliability (M006-M008)

*Fixed discovery bugs, split log directories per node type, and resolved streaming data corruption.*

### M006: Discovery engine fixes and log dir split

Fixed three critical bugs in the discovery engine: user-provided paths were ignored (D029 — user paths now take priority), empty directories stopped probing (probeDir now skips and tries next candidate), and PID detection matched PID 1 on containers. Split `--dremio-log-dir` into `--coordinator-log-dir` and `--executor-log-dir` (D027, breaking change) because coordinators and executors use different log paths. The TUI auto-probes both the master and first executor pod for path autodetection (D030).

### M007: HostExecute data corruption and pod selection

Fixed two bugs causing "no files found" on K8s clusters. First, the K8SWriter corrupted data at chunk boundaries — Kubernetes SPDY APIs deliver data in arbitrary byte chunks, so the writer needed line-buffering with a final flush. Second, executor pod selection used negative exclusion (`!Contains("master")`) which matched non-Dremio pods. Changed to positive matching with `HasPrefix("dremio-executor")`.

### M008: Streaming performance and TUI status

Added 256KB write buffering to the streaming pipeline for throughput improvement. Fixed four TUI status display issues and removed three dead CLI flags (`--compression-level`, `--transfer-dir`, `--no-log-dir`).

## Phase 3: CLI Restructure (M009-M012)

*Consolidated flags, restructured the command tree, and added operational visibility.*

### M009: Config collection, flag cleanup, and TUI improvements

Fixed K8s ConfigMap config file collection — ConfigMap mounts use symlinks, so `find -type f` returns nothing; changed to `find -L -type f`. Added post-stream secret masking for credentials in sso.json (D035). Added K8s context picker to the TUI. Consolidated `--parallel-nodes` and `--kubectl-max-connections` into a single `--collection-threads` flag (D034) since the old flags were never wired to execution. Added `--label-selector` propagation to all 5 pod listing paths.

### M010: Streaming pipeline optimization

Eliminated dual-hash checksum waste (probe once during discovery, compute only the available hash in a background goroutine). Added gzip compressed streaming — `gzip -c` on the remote node with transparent decompression on the orchestrator (D040). Increased write buffer from 256KB to 8MB. Upgraded K8s exec from SPDY-only to WebSocket-primary with SPDY fallback for older clusters.

### M011: CLI command tree restructure

Replaced `ddc collect --mode standard|diagnosis` with Cobra subcommands `ddc collect standard` / `ddc collect diagnosis` (D044). This enabled per-subcommand flag scoping — diagnosis-only flags (like `--diag-time-seconds`) are invisible to standard mode, enforced structurally rather than via runtime validation. Bare `ddc collect` now shows subcommand help instead of running a default (D043). Removed 3 more dead flags and added targeted remote disk pre-flight checks.

### M012: CLI polish and thread status

Moved `--verbose` and `--skip-version-check` to root persistent flags (visible in `ddc --help`). Renamed `--tarball-out-dir` to `--output-file`. Added Threads (active/max) and Queued row to the TUI status page (D049) for visibility into collection concurrency.

## Phase 4: UX Refinement (M013-M017)

*Restored missing collection data, surfaced errors, and redesigned the diagnosis workflow.*

### M013: Restore node-info collection

Restored unconditional collection of all 4 node-info files (os_info.txt, diskusage.txt, rocksdb_disk_allocation.txt, jvm_settings.txt) in both modes. JVM flags collection moved from the JVM tools phase to node-info (D052) — it logically belongs with node-info (produces diagnostic context) not with JVM tools (produces performance data).

### M014: Error surfacing, PID fix, and RocksDB autodetection

Fixed PID detection with a three-step cascade: jcmd -l (most specific), pgrep -x java, pgrep -f dremio.*java. The old logic preferred non-PID-1 results, which on K8s picked up the wrong host-namespace PID. Added per-tool error details to the TUI (tool name + error summary per node). Inlined node-info collection into per-node goroutines. Added RocksDB directory autodetection from dremio.conf (D053).

### M015: Pod phase filtering and node registration

Added Running-phase pod filtering to both K8s discovery paths (API and kubectl CLI). Pending, Failed, Succeeded, and Unknown pods are now excluded from collection. Added upfront node registration in the TUI — all nodes appear immediately after discovery with "Queued" status, rather than appearing incrementally as threads become available.

### M016: Diagnosis mode workflow redesign

Redesigned the diagnosis mode interactive TUI with three major additions: node selection (multi-select with deselected nodes fully skipped, D057), bidirectional date range (Days/Date Start/Date End with reactive linking), and split tool selection/configuration pages. Moved Endpoint/PAT page before Node Selection (D058) to enable API-based node discovery.

### M017: Diagnosis form polish

Five surgical edits: removed redundant computed range note, simplified Date Start label, added Date End auto-fill from Days (D063), reordered diagnostics page to put tool selection first, and consolidated four per-tool parameter groups into one with huh.NewNote() sub-headers (D065).

## Phase 5: Transport Extension (M018)

*Added local transport for single-node collection.*

### M018: Local transport

Added `ddc collect local standard|diagnosis` as a first-class transport alongside SSH and K8s. The LocalCollector implements the full Collector interface with role auto-detection from dremio.conf — it parses `services.coordinator.enabled` / `services.executor.enabled` to determine if the machine is a coordinator or executor (D067). Used `GetString` instead of `HasPath` for HOCON boolean detection (D069 — HasPath returns false for keys set to false). Deleted the old `cmd/root/fallback/` package and established `cmd/root/local/` following the per-transport package pattern (D066).

The TUI integrates Local as a third transport option, requiring three-way branching throughout the interactive flow. CLI commands are wired with proper flag scoping matching the SSH and K8s patterns.

## Current State

DDC v4 is a shipping Go CLI tool with three collection transports (K8s, SSH, Local) and two modes (standard, diagnosis). The architecture follows a clean pattern:

- **Orchestrator** (`cmd/root/collection/`) drives all collection via the Collector interface
- **Transports** (`cmd/root/kubernetes/`, `cmd/root/ssh/`, `cmd/root/local/`) implement the Collector interface for their respective environments
- **Streaming** flows directly from remote nodes through `kubectl exec cat` / `ssh cat` / local `os.Open` with progress tracking, checksum verification, and optional gzip compression
- **JVM diagnostics** run remotely via the same Collector interface with synchronized parallel execution across nodes
- **Interactive TUI** (`cmd/configui/`) provides transport selection, path autodetection, node selection, and tool configuration via huh forms
- **CLI** uses Cobra subcommands: `ddc collect {ssh|k8s|local} {standard|diagnosis}` with flags scoped per command level

The codebase is approximately 54,000 lines lighter than the pre-v4 baseline, with a simpler and more reliable architecture.
