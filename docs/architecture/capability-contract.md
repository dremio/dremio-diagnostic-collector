# Capability Contract

Active requirements that define DDC's expected behavior. These represent the current capability contract — what DDC must do and why.

For architectural decisions behind these capabilities, see [decisions.md](decisions.md).

## Discovery

| ID | Class | Requirement | Why it matters |
|----|-------|-------------|----------------|
| R038 | core | User-provided `--coordinator-log-dir` / `--executor-log-dir` paths used directly by RunDiscovery; hardcoded candidates probed only as fallback | Users set paths via CLI/TUI but discovery ignored them, causing zero files collected |
| R039 | core | probeDir skips directories that exist but contain no regular files, tries next candidate | `/var/log/dremio` exists on K8s pods but is empty; discovery stopped there instead of checking `/opt/dremio/log` |
| R040 | core | discoverPID filters out PID 1 and uses specific pattern | `pgrep -f dremio` returns PID 1 on containers. Superseded by R106 |
| R058 | core | `listFiles` uses `find -L -type f` to resolve K8s ConfigMap symlinks | Config files are symlinks on K8s; `find -type f` misses them entirely |
| R059 | core | `..data` and versioned directories excluded from collected file lists | K8s mount artifacts cause streaming errors when `cat` is attempted |
| R066 | core | Files matching GC log patterns (gc*.log*) tagged FileType "gc-log" during discovery | GC logs in the log directory must bypass the gc-log gating correctly |
| R067 | core | Separate GC log candidate directory search removed; GC logs found via filename pattern in log dir | Separate path search was the root cause of the classification bug |
| R068 | core | Probe sha256sum/md5sum availability during discovery; store in RemoteNodeInfo | Avoids computing both hashes for every byte during streaming |
| R069 | core | streamFileOnce computes only the available hash algorithm from discovery | Computing both hashes wastes CPU on both sides |
| R071 | core | Probe gzip availability on remote node during discovery | Gzip streaming requires remote node to have gzip installed |
| R106 | core | discoverPID cascades: jcmd -l -> pgrep -x java -> pgrep -f 'dremio.*java' preferring PID 1 | Old logic picked up wrong host-namespace PID in shared PID namespace containers. Supersedes R040 |
| R138 | core | Local transport auto-detects log/conf/RocksDB dirs and PID using RunDiscovery logic | Users shouldn't specify paths when running DDC on the same machine as Dremio |

## Streaming and Transport

| ID | Class | Requirement | Why it matters |
|----|-------|-------------|----------------|
| R070 | core | bufio.Writer in streamFileOnce increased to 8MB | Reduces syscall overhead for large file transfers |
| R072 | core | StreamFromHost uses `gzip -c` when gzip available; orchestrator decompresses transparently | Compressing the stream reduces transfer time for text-heavy log files |
| R073 | core | Falls back to uncompressed `cat` when gzip unavailable | Not all nodes have gzip; fallback must work seamlessly |
| R074 | quality | Progress UI shows uncompressed file size and decompressed bytes percentage | Users expect actual file size, not compressed transfer size |
| R075 | core | Hash computed in background goroutine after streaming (not inline via io.MultiWriter) | Background hashing unblocks the streaming pipeline for the next file |
| R076 | core | NewSPDYExecutor replaced with NewFallbackExecutor(WebSocket, SPDY, shouldFallback) | WebSocket is modern K8s exec transport (GA since 1.29); SPDY is deprecated |

## CLI Flags and Structure

| ID | Class | Requirement | Why it matters |
|----|-------|-------------|----------------|
| R042 | core | `--dremio-log-dir` replaced with `--coordinator-log-dir` and `--executor-log-dir` (breaking) | Coordinators and executors use different log paths |
| R094 | core | `--verbose` / `-v` on `RootCmd.PersistentFlags()`, inherited by all commands | Verbose logging is cross-cutting; should be visible at top level |
| R095 | core | `--skip-version-check` on `RootCmd.PersistentFlags()`, inherited by all commands | Version checking applies to the entire CLI |
| R096 | core | `--tarball-out-dir` renamed to `--output-file` on local-collect | Consistent naming across commands |
| R097 | core | `--no-log-dir` removed from local-collect | Dead flag |

## TUI Status and Display

| ID | Class | Requirement | Why it matters |
|----|-------|-------------|----------------|
| R041 | quality | streaming_collect sets IsCoordinator=true for coordinator nodes in TUI | All nodes showed under "executor nodes" even when they are coordinators |
| R098 | core | TUI shows "Threads" row: active/max (e.g. 3/5) | Users need visibility into concurrency utilization |
| R099 | core | TUI shows "Queued" row: nodes waiting for semaphore slot | Shows how many nodes are bottlenecked behind the concurrency limit |
| R107 | failure | Per-node status includes tool name and error summary | Users saw "2 failed" but had no idea which tools or why |
| R108 | failure | TUI includes dedicated errors section grouping tool errors by node | Compact per-node status; bottom section provides full detail |
| R109 | quality | Global status doesn't show per-node operation messages | Global status for per-node work is misleading |
| R111 | quality | TUI row order: Collection, Coordinators, Executors, Threads, Queued, Failed Nodes | Failed Nodes best placed after operational metrics |
| R113 | quality | "Dremio RocksDB dir" input removed from TUI Paths pages | Autodetected value used silently; reduces TUI complexity |
| R117 | quality | All nodes registered in TUI immediately after discovery, before acquiring thread | Users see full collection scope from the start |

## Collection Behavior

| ID | Class | Requirement | Why it matters |
|----|-------|-------------|----------------|
| R043 | core | TUI probes both coordinator and first executor for separate log dir autodetection | Current autodetection only probed master node; executor paths differ |
| R044 | integration | Coordinator/executor log dir split applies to both K8s and SSH transports | SSH clusters have the same path divergence as K8s |
| R060 | security | Post-stream secret masking applied to config files (e.g. clientSecret in sso.json) | Diagnostic archives should not contain plain-text credentials |
| R061 | integration | Live cluster verification: symlinked configs collected, artifacts excluded, secrets masked | Unit tests validate logic; live verification proves end-to-end correctness |
| R110 | core | Node-info collection runs inside collectNode before streaming; separate phase removed | Enables per-node progress display for node-info |
| R112 | core | TUI path discovery: cat dremio.conf from coordinator, parse HOCON, populate RocksDBDir | Users shouldn't manually provide paths derivable from dremio.conf |
| R114 | core | CollectRocksDBDiskUsage accepts RocksDB directory parameter (not hardcoded) | Hardcoded path fails on clusters with custom RocksDB locations |
| R115 | core | `--dremio-rocksdb-dir` CLI flag preserved for user override | Some deployments have non-standard paths |
| R116 | core | GetCoordinators/GetExecutors filter out pods not in Running phase | Pending pods waste thread slots and confuse users |

## Diagnosis Mode

| ID | Class | Requirement | Why it matters |
|----|-------|-------------|----------------|
| R119 | core | Date range page shows Days, Date Start, Date End with bidirectional linkage | Customers need precise date range control beyond a simple day count |
| R120 | core | Days value auto-calculates Date End (now) and Date Start (now - N days) | Sensible defaults from simple input |
| R121 | core | Manual date edits set Days to N/A | Avoids conflicting inputs |
| R130 | core | Endpoint/PAT page before Node Selection in diagnosis mode | Node discovery requires endpoint + PAT |
| R131 | core | SSH diagnosis mode: DiscoverNodesFromAPI populates Node Selection | SSH users don't have to manually list every executor IP |
| R132 | core | K8s diagnosis mode: pod discovery runs before Node Selection | K8s users see real pod names and can deselect specific ones |

## Deferred

| ID | Class | Requirement | Why deferred |
|----|-------|-------------|--------------|
| R037 | core | Remove local-collect code paths only used by orchestrator (threading pool, JOB protocol, pid file) | Lower priority; scheduled for future cleanup milestone |
