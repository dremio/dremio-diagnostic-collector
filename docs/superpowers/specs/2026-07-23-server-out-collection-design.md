# Collect server.out as part of Server logs collection

Date: 2026-07-23
Issue: [#337 — DDC v4 does not collect server.out Logs](https://github.com/dremio/dremio-diagnostic-collector/issues/337)

## Problem

`server.out` is never collected, in any mode, even when "Server logs" collection is
enabled. The TUI and the `--collect-server-logs` flag suggest it would be. Root cause:
`server.out` sits on the hard exclusion list `alwaysExcludedPrefixes` in
`cmd/root/collection/streaming_collect.go`, so `streamNodeFiles` drops it with
`stream exclude (blocked)` before any mode or log-type gating runs.

Two secondary gaps would keep `server.out` uncollected even after un-blocking it:

1. **Date filter.** `server.out` has no date in its filename, so `isWithinDateRange`
   falls back to file mtime. Its mtime reflects the last write to stdout — typically the
   last restart — so a `--days=3` window on a long-running node silently drops it.
2. **Split log directories.** The launcher writes `server.out` to `$DREMIO_LOG_DIR`
   (`/opt/dremio/bin/dremio:196`: `logout="${DREMIO_LOG_DIR}/server.out"`), while
   `server.log`/`queries.json` are logback appenders following `-Ddremio.log.path`
   (`logback.xml`: `${dremio.log.path}/server.log`). `dremio-config` derives
   `dremio.log.path` from `DREMIO_LOG_DIR`, but `DREMIO_JAVA_SERVER_EXTRA_OPTS` can
   override the property independently, so the two directories can diverge (observed in
   production; see the capgroup case in `2026-06-25-ddc-logpath-rocks-gating-design.md`).
   Discovery lists files from only one resolved log dir per node, so on such clusters
   `server.out` — and GC logs, which also live in `DREMIO_LOG_DIR` — are never discovered.

Verified on `dremio-master-0` (read-only): a single `/opt/dremio/log/server.out`
(19 KB) holding startup timestamps and ulimit dumps appended across restarts.

## Requirements (user-confirmed)

- No new CLI flags and no new TUI selections. `server.out` is gated by the existing
  Server-logs selection (`--collect-server-logs` / TUI "Server logs" item).
- Collected in **both** standard and diagnosis modes.
- Part of the Server-logs collection; the TUI labels become **"Server logs & out"** in
  both the diagnosis multi-select and the standard per-log day-select.
- Exempt from the day/date-range filter (always collected when Server logs are enabled).
- Split-dir clusters are handled: when `DREMIO_LOG_DIR` differs from the resolved log
  dir, `server.out*` **and GC logs** are additionally discovered there (GC files remain
  gated by the existing GC-logs toggle).

## Non-goals

- Un-blocking `server.json`, `audit.`, `admin_backup`, or any other excluded file.
- Changes to streaming, archive layout, flags, or config defaults.
- General multi-directory log discovery beyond the `DREMIO_LOG_DIR` secondary pass.

## Design

Approach: ride the existing `server.` prefix rules. Once un-blocked, `server.out`
automatically inherits the `CollectServerLogs` gate (`isLogTypeEnabled`) and the
standard-mode allowlist (`isLogAllowedInStandardMode`), in both modes, and is routed to
the per-node `logs/` archive folder as FileType `"log"`. Rotated variants
(`server.out.1`) are covered by prefix matching. The existing 0-byte skip still applies.

### 1. Un-block (`cmd/root/collection/streaming_collect.go`)

Remove `"server.out"` from `alwaysExcludedPrefixes`. All other entries stay.

### 2. Date-filter exemption (same file, `logDayLimit`)

New first check: base names with prefix `server.out` return `-1` (no limit). It must
precede both the diagnosis-mode `DiagLogDays` branch and the standard-mode `server.`
case so the exemption holds in both modes.

### 3. Secondary-dir discovery (`cmd/root/collection/discovery.go`)

- `RunDiscovery` calls `readProcessInfo` once, right after PID discovery, and passes the
  blob into `resolveLogDir` (which stops reading process info internally).
- From the same blob, extract `DREMIO_LOG_DIR` via `ExtractEnvValue`. If it is non-empty,
  differs from the resolved primary log dir, and `dirHasFiles`, list it with
  `listFiles(..., "log", "1")` (maxdepth 1 — `server.out` and GC logs sit at top level).
- Keep **only** files whose base name matches `server.out*` or the GC-log patterns.
  Factor the existing inline GC/queries reclassification in `RunDiscovery` into a shared
  helper so both listings classify identically: GC files → `"gc-log"`, `server.out` →
  `"log"`.
- Dedup: skip paths already present in `info.Files`.
- The secondary pass runs even when the primary log dir came from an explicit
  `--coordinator-log-dir`/`--executor-log-dir` flag — the flag states where server.log
  is, not where `DREMIO_LOG_DIR` points. If PID discovery failed (pid 0), the pass is
  skipped (no process info to read).

### 4. TUI labels (`cmd/configui/configui.go`)

Rename the two `"Server logs"` titles to `"Server logs & out"`: the diagnosis
logs-and-data multi-select option (~line 506) and the standard-mode day-select
(~line 694).

## Testing

- `cmd/root/collection/streaming_collect_test.go`
  - Flip the existing case asserting `server.out` is always excluded.
  - `logDayLimit`: `server.out` → `-1` in standard **and** diagnosis mode; `server.log`
    behavior unchanged.
  - `isLogAllowedInStandardMode("server.out")` is true; `isLogTypeEnabled` gates
    `server.out` on `CollectServerLogs`.
- `cmd/root/collection/discovery_test.go` (existing `mockExecutor`)
  - Differing `DREMIO_LOG_DIR` → secondary listing returns `server.out` + GC files only;
    unrelated files in that dir are not picked up.
  - `DREMIO_LOG_DIR` equal to the primary dir → no secondary listing.
  - Overlapping paths are deduped; pid 0 → no secondary pass.
- Live verification against `dremio-master-0` (read-only): `k8s standard` and
  `k8s diagnosis` runs with Server logs enabled → `server.out` in the tarball's `logs/`
  folder; a small `--days` run proves the date-filter exemption.
- `go build -o bin/ddc.exe .` and `go test -short ./...` pass.

## Risks

- The secondary listing adds one `ps eww` read on nodes where discovery previously
  skipped it (explicit log-dir flag set). Cost is one lightweight exec per node.
- On clusters where `DREMIO_LOG_DIR` is unset in the process environment, the secondary
  pass never triggers; behavior is identical to today except `server.out` in the primary
  dir is now collected — which is the fix for issue #337 itself.
