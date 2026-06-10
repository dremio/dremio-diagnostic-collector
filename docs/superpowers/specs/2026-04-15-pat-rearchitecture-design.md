# PAT Rearchitecture Design

Eliminate PAT token dependency from standard mode and reduce it to optional in diagnosis mode by replacing REST API collection with an embedded `dremio-rocksdb-viewer` binary that reads directly from Dremio's RocksDB catalog on the coordinator node.

## Approach

Vertical slices by mode: build the shared rocksdb-viewer embed + execution layer first, then refactor standard mode end-to-end, then diagnosis mode end-to-end.

## Section 1: RocksDB Viewer Embed & Execution Layer

### New Package: `cmd/local/rockscollect/`

Parallel to `cmd/local/jvmcollect/`, follows the same embed pattern as `asprof_embed.go`.

**Embed (`rocksdb_embed.go`):**
- `go:embed` for `dremio-rocksdb-viewer-linux-amd64` and `dremio-rocksdb-viewer-linux-arm64` placeholder binaries
- `GetRocksDBViewerBinary(arch string) ([]byte, error)` — returns architecture-specific binary
- `ErrRocksDBViewerEmpty` — returned when the embedded binary is an empty placeholder

**Source binaries:** Copy from `C:\Users\chufe\Workspaces\golang\dremio-rocksdb-viewer\bin` into the project.

**Execution flow (coordinator/master node only):**
1. Detect remote architecture via `uname -m`
2. `CopyToHost` the binary to `/tmp/dremio-rocksdb-viewer` on the coordinator
3. `chmod +x` via `HostExecute`
4. For each requested data type, run:
   ```
   /tmp/dremio-rocksdb-viewer -db <rocksdbDir>/catalog -type <type> [-days N | -date-start X -date-end Y]
   ```
5. Stream stdout back via `HostExecute`
6. Write output to local temp:
   - System tables: `system-tables/<host>/sys.<table>.json`
   - WLM: `wlm/<host>/<type>.json`
   - queries-perf: `queries-perf/<host>/queries-perf-<n>.json` (128MB split)
7. Cleanup: `rm -f /tmp/dremio-rocksdb-viewer` via deferred `HostExecute`

**The `-db` path** uses the existing `--dremio-rocksdb-dir` flag (autodetected) with `/catalog` suffix appended.

## Section 2: Standard Mode Changes

### TUI Changes (`cmd/configui/configui.go`)

1. **Remove "API-based collection" page** — removes Dremio endpoint, Allow insecure SSL, and PAT token fields from standard flow
2. **Remove "PAT-enabled collections" page** — the WLM/system tables/KV store multi-select gated behind PAT
3. **Remove KV store report** from standard collection — requires PAT+endpoint, not available via rocksdb-viewer
4. **Add `queries-perf` selector** to "Log collection" page, directly below `queries.json` selector — same day-range options (30/14/7/3/1 days). New field in `StandardConfig`: `QueriesPerfDays int`

### CLI Flag Changes

**Remove from `StandardCmd`:**
- `--dremio-pat-token`
- `--dremio-endpoint`
- `--allow-insecure-ssl`
- `--collect-kvstore-report`

**Add to `StandardCmd`:**
- `--collect-queries-perf-json` (bool)
- `--queries-perf-num-days` (int)

### Collection Changes

- Rocksdb-viewer collection runs as part of the **coordinator node's streaming phase** (node-level status), not in the orchestrator goroutine
- Progress shows under the coordinator node: "Collecting system tables from RocksDB...", "Collecting WLM from RocksDB...", "Collecting queries-perf from RocksDB..."
- Replaces `RunCollectWLM`, `RunCollectSystemTables` REST calls for standard mode

### Net Effect

No PAT token required. No Dremio endpoint detection needed. System tables, WLM, and queries-perf all come from RocksDB directly.

## Section 3: Diagnosis Mode Changes

### TUI Restructuring (`cmd/configui/configui.go`)

1. **Rename "Date range" page → "Logs Collection"**
2. **Move options from "Diagnostic Collection" into "Logs Collection":**
   - Log types multi-select (queries.json, server logs, GC logs, hs_err, tracker, vacuum, hive deprecated, acceleration, access)
   - Collect K8s container logs toggle
3. **Move from "API-based collection" → "Logs Collection":**
   - "Collect problematic job profiles" (default changes to **No**)
4. **Move from "PAT-enabled collections" → "Logs Collection":**
   - "Collect KV store report" toggle
5. **Rename "PAT-enabled collections" → "Dremio System Tables & WLM"** — only contains system table and WLM type configuration
6. **"API-based collection" page becomes conditional** — shown only if "Collect problematic job profiles" = Yes OR "Collect KV store report" = Yes. PAT token becomes mandatory when this page appears.
7. **No new TUI field for `queries-perf`** — reuses `--days`, `--date-start`, `--date-end` from "Logs Collection" (if `--days` is set in DDC, days is passed to rocksdb-viewer). `--collect-queries-perf-json` defaults to `true` in diagnosis mode.

### CLI Flag Changes

**Add to `DiagnosisCmd`:**
- `--collect-queries-perf-json` (bool)

**Diagnosis mode retains:** `--dremio-pat-token`, `--dremio-endpoint`, `--allow-insecure-ssl` (needed for job profiles and KV store report)

### Collection Changes

- System tables + WLM collection switches from REST API to rocksdb-viewer, shown in **coordinator node-level status**
- Job profiles + KV store report remain REST/PAT-based, shown in **global/orchestrator status**
- "Diagnostic Collection" page is removed — contents absorbed into "Logs Collection"

### Net Effect

PAT is only needed if the user explicitly opts into job profiles or KV store report. System tables and WLM come from RocksDB without authentication.

## Section 4: queries-perf Collection & File Splitting

### Execution

- Rocksdb-viewer invoked on coordinator:
  ```
  /tmp/dremio-rocksdb-viewer -db <rocksdbDir>/catalog -type queries_perf [-days N | -date-start X -date-end Y]
  ```
- Standard mode: uses day count from TUI selector (`QueriesPerfDays`)
- Diagnosis mode: reuses existing `--days` / `--date-start` / `--date-end` values

### Streaming & Splitting

- DDC reads stdout from the remote command via `HostExecute`
- Writes to local temp folder: `<tmpDir>/queries-perf/<host>/queries-perf-1.json`
- Tracks bytes written per file; when reaching 128MB, finishes the current line, closes the file, opens `queries-perf-2.json`, etc.
- Line-aware splitting: scan for `\n`, only split between complete lines

### Archive Integration

- `queries-perf/` folder archived into the final tarball alongside `queries/`, `system-tables/`, etc.
- Follows the same `CopyStrategy.CreatePath` pattern used by other file types

### Config Fields

- `StandardConfig.QueriesPerfDays int` — from TUI selector
- `CollectionArgs.CollectQueriesPerf bool` + `CollectionArgs.QueriesPerfNumDays int`
- Diagnosis mode: derives days from existing `CollectionArgs.Days` / date range fields

## Section 5: CLI Flag & Cleanup Changes

### Flag Rename

- `--dremio-queries-json-num-days` → `--queries-json-num-days`
- Updated in: `conf_key_names.go`, `root.go`, `configui.go` (generated CLI), tests, README

### Generated CLI

The TUI-generated CLI command preview includes:
- `--collect-queries-perf-json` and `--queries-perf-num-days` (standard mode)
- `--collect-queries-perf-json` (diagnosis mode)
- Renamed `--queries-json-num-days`

### Code Cleanup (`apicollect.go`)

**Remove (replaced by rocksdb-viewer in both modes):**
- `RunCollectWLM`
- `RunCollectSystemTables`
- `RunCollectClusterStats`

**Keep (still PAT-based in diagnosis mode):**
- `RunCollectKVStore`
- `RunLogBasedProfileCollection`

### Dremio Endpoint Autodetection

Remove endpoint autodetection logic for standard mode (no longer needed).

## Section 6: Error Handling & Testing

### Error Handling

- Embedded binary is empty placeholder (`ErrRocksDBViewerEmpty`): log warning, skip rocksdb-viewer collection (same as async-profiler fallback)
- `CopyToHost` fails: log error, skip rocksdb-viewer collection for that run, don't fail the whole collection
- Individual `-type` invocation fails: log error for that type, continue with remaining types
- `--dremio-rocksdb-dir` empty and autodetection failed: log warning, skip rocksdb-viewer collection
- Cleanup (`rm -f`) runs via deferred `HostExecute` regardless of success/failure

### Testing

- **Unit tests for `rocksdb_embed.go`:** `GetRocksDBViewerBinary` returns correct binary per arch, returns error for empty placeholders and unsupported arches
- **Unit tests for 128MB file splitter:** verify line-aware splitting, correct file naming, boundary conditions (exactly 128MB, single line > 128MB)
- **Unit tests for flags:** update `root_flags_test.go` to verify `--queries-json-num-days` rename, new `--collect-queries-perf-json` and `--queries-perf-num-days` flags
- **TUI tests:** update `configui_test.go` for removed pages in standard mode, restructured diagnosis pages, new queries-perf selector
- **Integration tests (require cluster):** end-to-end rocksdb-viewer execution on coordinator
