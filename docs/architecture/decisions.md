# Architectural Decisions

This document records the key architectural and design decisions made during DDC v4 development, grouped by domain. Each decision includes the choice made and the rationale behind it.

For the full development narrative, see [design-history.md](design-history.md).

## Collection Transport

DDC v4 replaced the old "push binary, collect tarball, copy back" model with direct file streaming. These decisions shaped the streaming architecture.

| ID | Decision | Choice | Rationale |
|----|----------|--------|-----------|
| D008 | Collection transport architecture | Replace push-binary-collect-tarball-copy with direct file streaming via `kubectl exec cat` / `ssh cat` | kubectl cp is unreliable, remote tarball creation consumes node disk, embedded binaries bloat the build by ~13MB |
| D009 | Node discovery without local-collect binary | Orchestrator runs `ls`/`find`/`cat /proc` remotely via kubectl exec/ssh | No binary on the node means discovery must happen via remote shell commands |
| D011 | Transfer rate limiting mechanism | Rate-limited `io.Writer` on orchestrator side; SPDY/SSH TCP backpressure propagates to slow remote `cat` | Client-side throttle works because SPDY WebSocket and SSH both have TCP flow control; kernel pipe buffer is ~64KB bounded |
| D012 | Streaming parallelism model | Sequential files per node, N nodes in parallel (controlled by `--collection-threads`) | Simple, predictable, avoids overwhelming a single node with concurrent cat processes |
| D013 | Error handling during file streaming | Skip on permission/not-found errors; retry up to 3 times on connection drops | Permission and not-found are permanent errors. Connection drops are transient and worth retrying |
| D035 | Post-stream masking approach for config files | Read-modify-write on local file after streaming, not inline stream wrapping | Masking after streaming preserves checksum verification accuracy. Wrapping the stream writer would mean checksums reflect masked content |
| D040 | Passing gzip flag to StreamFromHost | Add `useGzip bool` parameter directly to `StreamFromHost` signature | Minimal diff, keeps interface clean. All implementations just swap `cat` for `gzip -c` in command construction |
| D076 | K8s exec transport upgrade | Replace `NewSPDYExecutor` with `NewFallbackExecutor(WebSocket, SPDY, shouldFallback)` | WebSocket is the modern K8s exec transport (GA since 1.29). SPDY is deprecated. WebSocket offers better proxy compatibility |

## Discovery Engine

These decisions govern how DDC discovers files, processes, and paths on remote nodes.

| ID | Decision | Choice | Rationale |
|----|----------|--------|-----------|
| D017 | Shared discovery function for K8s and SSH | Extract into shared function accepting an executor closure, callable from both transports | Both transports use the same `HostExecute(host, args...)` signature. Discovery commands are identical shell commands |
| D022 | PID source for remote JVM commands | Use `RemoteNodeInfo.DremioPID` from existing discovery (`pgrep`) | Discovery already detects the Dremio PID. Running a second detection would be redundant |
| D029 | Discovery path priority strategy | User-provided path first; hardcoded candidates only when no path configured | When the user explicitly provides a path, discovery should trust it. Probing candidates is a fallback |
| D030 | Executor log dir autodetection in TUI | Auto-probe first executor pod alongside master | Executor log paths differ from coordinator. Probing only the master misses the executor path |
| D053 | RocksDB dir autodetection approach | Parse dremio.conf during TUI path discovery, remove TUI field, keep `--dremio-rocksdb-dir` CLI flag as override | Reuses existing `GetRocksDBPath()` HOCON parser. CLI flag preserved for non-standard setups |
| D067 | Auto-detect node role in local transport | Parse dremio.conf via existing HOCON parser, check `services.coordinator.enabled` / `services.executor.enabled` | HOCON parser already exists. Conf file is canonical source. More reliable than parsing JVM process args |
| D069 | HOCON boolean key detection method | Use `GetString` instead of `HasPath` | `HasPath` returns false when a key exists but its value is `false`, making it impossible to distinguish "missing" from "set to false" |

## JVM Diagnostics

DDC's diagnosis mode runs JVM tools (JFR, jstack, heap dump, async-profiler) remotely. These decisions shaped that subsystem.

| ID | Decision | Choice | Rationale |
|----|----------|--------|-----------|
| D014 | JVM tool output strategy (file vs stdout) | JFR/heap dump/asprof write to temp file on pod then stream back; jstack/ttop/JVM flags capture stdout directly | JFR, heap dump, and async-profiler produce binary output that must be written to files. Text tools can be captured from stdout |
| D015 | Synchronized JVM collection timing | Issue all JVM start commands in parallel goroutines across all nodes; best-effort ~1s skew tolerance | Users need correlated JVM data from the same time range across a distributed system |
| D021 | JVM collection function location | `jvmcollect.go` in `cmd/root/collection/` (same package as `streaming_collect.go`) | Functions need direct access to the Collector interface. A separate package would require exporting the interface or creating circular imports |
| D023 | CollectJVMFlags default in diagnosis mode | Defaults to true, no separate CLI flag | JVM flags are always useful in diagnosis mode with near-zero overhead |
| D025 | Collector interface method removal scope | Remove `CopyFromHost` only; retain `CopyToHost` | `CopyToHost` is actively used by `CollectAsyncProfiler` to upload the asprof binary. Research caught this before deletion |
| D051 | JVM flags collection scope | Unconditional in both standard and diagnosis modes | JVM settings are basic diagnostic context that support engineers expect in every tarball |
| D052 | Node-info collection scope | Unconditional in both modes (os_info, diskusage, rocksdb_disk_allocation, jvm_settings) | Lightweight files providing essential context for any support case. Mode-gating created gaps |
| D054 | JVM collection phase integration | Keep `runJVMCollection` as separate post-streaming phase; only inline `runNodeInfoCollection` into `collectNode` | `runJVMCollection` uses a `startBarrier` for synchronized capture. Moving it into `collectNode` would break timing guarantees |

## CLI Structure

DDC uses a Cobra command tree. These decisions shaped the CLI flag naming, scoping, and command hierarchy.

| ID | Decision | Choice | Rationale |
|----|----------|--------|-----------|
| D027 | CLI flag naming for log dir split | `--coordinator-log-dir` and `--executor-log-dir`; remove `--dremio-log-dir` (breaking change) | Coordinators and executors use different log paths. A single flag cannot serve both |
| D028 | Whether to split conf dir flag | Keep single `--dremio-conf-dir` for both node types | Conf dir (`/opt/dremio/conf`) is the same on both coordinator and executor pods |
| D034 | Concurrency flag consolidation | Single `--collection-threads` flag with default 20 for diagnosis, 5 for standard | `--parallel-nodes` and `--kubectl-max-connections` were dead flags never wired to execution |
| D043 | Behavior of bare `ddc collect` (no verb) | Error with subcommand help | Forces explicit mode selection — no silent defaults that surprise users |
| D044 | CLI command structure for collect modes | Cobra subcommand tree: `ddc collect standard` / `ddc collect diagnosis` | Verb subcommands are idiomatic Cobra, enable per-subcommand flag scoping, and produce better help output |
| D046 | Fate of `--disable-free-space-check` | Keep it; gates local output dir check with hardcoded thresholds (25GB standard, 40GB diagnosis) | Reasonable guard against filling the disk |
| D066 | Package location for local transport | New package `cmd/root/local/local.go`, delete `cmd/root/fallback/` | Follows per-transport package pattern (ssh/, kubernetes/). "fallback" is a poor name for a first-class transport |

## TUI / User Experience

DDC's interactive mode uses `charmbracelet/huh` forms. These decisions shaped the interactive workflow.

| ID | Decision | Choice | Rationale |
|----|----------|--------|-----------|
| D049 | Label for queued nodes in TUI | Use "Queued" (not "Waiting") for nodes blocked on the concurrency semaphore | "Queued" communicates the bottleneck more clearly |
| D057 | Node deselection scope | Skip ALL collection on deselected nodes (logs, config, JVM tools) | Clean semantics — deselected means fully skipped. Simpler implementation |
| D058 | Endpoint/PAT page position in diagnosis mode | Move before Node Selection; standard mode unchanged | Node discovery requires endpoint + PAT. Standard mode has no node selection page |
| D059 | Date input format | Plain text ISO 8601 (e.g. `2026-04-01T10:00:00`) | Consistent with huh's input model; no native date picker in huh |
| D063 | Date End auto-fill mechanism | `huh DescriptionFunc` on Date End field, bound to Days value | Established pattern for cross-field reactivity in huh forms |
| D065 | Tool parameters page consolidation | One "Tool parameters" group with group-level `WithHideFunc`, using `huh.NewNote()` sub-headers per tool | huh has no per-field `WithHideFunc` (only Group has it). Notes display as sub-headers without blocking navigation |

## Integrity and Security

Decisions governing data integrity verification and credential protection.

| ID | Decision | Choice | Rationale |
|----|----------|--------|-----------|
| D003 | HTTP client for cluster-stats | Plain `http.Client` with TLS `InsecureSkipVerify`, not `restclient.APIRequest` | Cluster-stats scrapes HTML — no PAT needed. Keeps collection independent of PAT availability |
| D010 | Checksum verification algorithm | sha256sum first, fall back to md5sum, skip if neither available | sha256sum is stronger but not always available in minimal containers. Graceful degradation |
| D035 | Secret masking timing | Post-stream read-modify-write on local file | Preserves checksum integrity of the actual transfer. Advisory failure ensures masking errors don't block collection |

## Cleanup and Migration

Decisions about removing dead code, changing interfaces, and managing breaking changes.

| ID | Decision | Choice | Rationale |
|----|----------|--------|-----------|
| D004 | Streaming file preservation boundary | Preserve `streaming.go` (LocalStreamReceiver), delete all other streaming infrastructure | `streaming.go` was actively used by `ExecuteStandalone`. All other streaming files were dead code |
| D005 | SSH streaming code removal | Delete `cmd/root/ssh/streaming.go`, `streaming_test.go`, and `RawExec` method | Discovered during execution as additional streaming-only code. Leaving them would create dead code with dangling types |
| D016 | Post-migration cleanup scope | Aggressive: remove CopyFromHost, delete capture.go + ddcbinary + diskestimate.go, simplify local-collect, rewrite README | Dead code adds maintenance burden. Aggressive cleanup reduced codebase by ~1000 lines |
| D068 | Local transport node classification | Single-node, auto-detected as coordinator OR executor from dremio.conf | The machine could be either role. Not hardcoded as coordinator |
