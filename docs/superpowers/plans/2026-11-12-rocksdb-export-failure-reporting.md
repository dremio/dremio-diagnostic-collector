# RocksDB export failure reporting, no pre-flight count, keep-awake, Go 1.27 — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every failed or unavailable RocksDB-viewer export visible, stop the queries-perf pre-flight count, document `*-num-days 0`, deprecate `--collector-timeout`, keep macOS/Windows awake during a collection, and move DDC to Go 1.27.4 without changing its runtime defaults.

**Architecture:** Viewer results gain a third outcome next to "memory guard" and "incomplete": `FailedCollectionError`, returned by `RunRocksDBCollection` in a `RocksDBResult` struct and reported through the same three surfaces (node status line, `summary.json`, ddc.log summary). A missing catalog store is detected like the memory-guard line and becomes an INFO "not available". Keep-awake wraps `collection.Execute` through a swappable function variable around `crgimenes/native/power`.

**Tech Stack:** Go 1.27.4 (`godebug default=go1.24`), Cobra v1.9.1, `github.com/crgimenes/native/power` v0.1.18 (+ `github.com/ebitengine/purego` v0.11.3), golangci-lint v2.15.0, gosec v2.30.0.

**Spec:** `docs/superpowers/specs/2026-11-12-rocksdb-export-failure-reporting-design.md`

## Global Constraints

- Branch: `feat/rocksdb-export-failure-reporting`, created from `main` in place (keeps `bin/ddc.exe` where the user expects it). One `wip:` commit per task; Task 8 squashes into one commit (code + spec + plan). Never push.
- `go.mod`: `go 1.27.4` and `godebug default=go1.24`. New requirements only `github.com/crgimenes/native v0.1.18` and `github.com/ebitengine/purego v0.11.3 // indirect`. No other version changes; `k8s.io/kubelet` stays `v0.32.3`.
- **Never run `go mod tidy`** (it regroups the require blocks) and **never `go fmt ./...`** (CRLF checkout, rewrites ~100 files). Format touched files only: `gofmt -w <file>`.
- Unit tests always as `SKIP_INTEGRATION_SETUP=true go test -short ./...` (without the variable the test setup deploys Dremio into the current kube context). No `-race` on Windows.
- Known environmental failures on this Windows box, not caused by this work: "Access is denied" in `cmd/local/rockscollect` or `cmd/local/jvmcollect` (`TestJFRCapture`).
- Lint with `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 run ./...` (doesn't replace the user's installed v2.1.6).
- Exact strings (copied from the spec):
  - `FailedCollectionError.Error()`: `"%s failed (%v)"` (Item, Cause)
  - summary field: `FailedCollections []string \`json:"failedCollections,omitempty"\``
  - node status suffix: `, FAILED: <item>, <item>`
  - not-available INFO: `rocksdb-viewer -type <type> on <host>: not available in this catalog (<cf> column family not found)`
  - unfiltered WARN: `rocksdb-viewer queries_perf on <host>: no day limit (--queries-perf-num-days 0); exporting the full job history, which can take hours on large catalogs`
  - keep-awake reason: `Dremio Diagnostic Collector collection`
  - deprecation message: `it has no effect and will be removed in a future release`
- No exit-code change, no retries, no console output for keep-awake, no Linux keep-awake.
- No customer names, hostnames or user IDs in code, comments, tests or commit messages.
- Next release is 4.1.0. The CHANGELOG and release are the user's (spec non-goal); not part of this plan.
- After the last task, build `bin/ddc.exe` (`go build -o bin/ddc.exe .`).

## Review Focus

1. A success rate whose float value lands just below a tenth (29/50 = 0.58 → `57.99999…` in float64) must print `58.0%`, not `57.9%` → Task 6 formats from integers (`TestFormatSuccessRate`).
2. The viewer's missing-store line arriving with a trailing `\r` or spaces over SSH/kubectl must still be recognised → Task 5 table case in `TestStoreMissingName`.
3. A scale-out coordinator without a catalog must not produce a FAILED entry → Task 4 adds `len(res.Failed) == 0` to `TestRunRocksDBCollectionSkipsWhenNoCatalog`.
4. Keep-awake must be released exactly once when the collection fails early → Task 2 `TestExecute_ReleasesKeepAwakeOnError`.
5. The missing-store line must never end up inside an export file, even if that export exits 0 → Task 5 `TestCollectRocksType_StoreMissingLineNeverWritten`.

---

### Task 1: Upgrade to Go 1.27.4 and the matching tools

**Files:**
- Modify: `go.mod` (line 3 `go 1.24.13`)
- Modify: `script/bootstrap:26` (golangci-lint v2.1.6), `script/bootstrap:35` (gosec v2.22.4)
- Modify: `script/bootstrap.ps1:24` (wrong v1 module path + v2.1.6), `script/bootstrap.ps1:33` (gosec v2.22.4)
- Modify: `cmd/root/collection/stats.go:106`, `pkg/consoleprint/consoleprint.go:643`, `pkg/consoleprint/consoleprint.go:646`

**Interfaces:**
- Consumes: nothing.
- Produces: a module that builds with go1.27.4; later tasks use it unchanged.

- [ ] **Step 1: Create the branch**

Run: `git switch -c feat/rocksdb-export-failure-reporting` (from `main`, clean tree except the untracked spec/plan).
Expected: `Switched to a new branch 'feat/rocksdb-export-failure-reporting'`.

- [ ] **Step 2: Bump the go line and pin the runtime defaults**

Run `go get go@1.27.4` (output: `go: upgraded go 1.24.13 => 1.27.4`, no other `upgraded` lines). Then add, on its own line after `go 1.27.4` with one blank line between:
```
godebug default=go1.24
```

- [ ] **Step 3: Verify the build and the GODEBUG pin**

Run: `go build -o bin/ddc.exe . && go version -m bin/ddc.exe | grep -E 'go1\.27\.4|DefaultGODEBUG'`
Expected: `go1.27.4`, and a `DefaultGODEBUG=` line containing `tlssha1=1`, `x509sslcertoverrideplatform=0`, `urlstrictcolons=0`, `tlssecpmlkem=0`, `containermaxprocs=0`.
Also run `git diff main -- go.mod`: only the `go` line and the `godebug` line changed.

- [ ] **Step 4: Run the new linter to see it fail**

Run: `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 run ./...`
Expected: exactly 3 issues, staticcheck QF1012, at `cmd/root/collection/stats.go:106`, `pkg/consoleprint/consoleprint.go:643` and `:646`.

- [ ] **Step 5: Fix the three lines**

Replace each `x.WriteString(fmt.Sprintf(...))` with `fmt.Fprintf(<the same builder>, ...)` (take the address if the builder is a value). Same format strings and arguments.

- [ ] **Step 6: Bump the tool pins**

- `script/bootstrap:26`: `v2.1.6` → `v2.15.0` (install.sh call).
- `script/bootstrap.ps1:24`: `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0`.
- `script/bootstrap:35` and `script/bootstrap.ps1:33`: `gosec@v2.22.4` → `gosec@v2.30.0`.

- [ ] **Step 7: Lint, audit and tests pass**

Run: `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 run ./...` → `0 issues.`
Run: `go run github.com/securego/gosec/v2/cmd/gosec@v2.30.0 -exclude=G204,G402 ./...` → gosec completes and prints its `Summary` block. Record the issue count in the task report. If v2.30.0 fails to load the go1.27 module, try the next lower release until one loads, and pin that version in both bootstrap scripts.
Run: `SKIP_INTEGRATION_SETUP=true go test -short ./...` → all `ok`, apart from the known environmental failures.

- [ ] **Step 8: Commit**

```bash
gofmt -l cmd/root/collection/stats.go pkg/consoleprint/consoleprint.go   # expect no output
git add go.mod go.sum script/bootstrap script/bootstrap.ps1 cmd/root/collection/stats.go pkg/consoleprint/consoleprint.go
git commit -m "wip: build with Go 1.27.4, keep Go 1.24 runtime defaults, bump lint and gosec"
```

---

### Task 2: Keep the collecting machine awake

**Files:**
- Create: `cmd/root/collection/keepawake.go`
- Create: `cmd/root/collection/keepawake_test.go`
- Create: `cmd/root/collection/keepawake_windows_test.go` (`//go:build windows`)
- Modify: `cmd/root/collection/collector.go:242-244` (`Execute`)
- Modify: `go.mod`, `go.sum`
- Modify: `docs/architecture/decisions.md` (Collection Transport table, after D077)

**Interfaces:**
- Consumes: Task 1's Go 1.27.4 module.
- Produces (package `collection`):
  - `const keepAwakeReason = "Dremio Diagnostic Collector collection"`
  - `var keepAwake func(reason string) (release func() error, err error)` — default wraps `power.PreventSleep(reason)` and returns `tok.Release`; tests replace it.
  - `func startKeepAwake() (stop func())` — never returns nil.

- [ ] **Step 1: Write the failing tests** in `keepawake_test.go`

Each test swaps `keepAwake` (restore in `t.Cleanup`) and captures ddc.log with `simplelog.InitLoggerWithOutputDir(dir)` as `TestLogDistributedCollectionSummary_GuardSection` does.
```go
func TestStartKeepAwake_AcquiresAndReleases(t *testing.T)
// fake records the reason and counts releases.
// reason == keepAwakeReason; after stop(): releases == 1; calling stop() again: releases still 1;
// ddc.log contains "keep-awake: idle system sleep blocked until the collection ends" and "keep-awake: released".

func TestStartKeepAwake_Unsupported(t *testing.T)
// fake returns (nil, power.ErrUnsupported); stop() must not panic;
// ddc.log contains "keep-awake: not supported on "+runtime.GOOS and no "WARN".

func TestStartKeepAwake_OtherErrorIsWarning(t *testing.T)
// fake returns (nil, errors.New("boom")); ddc.log contains "keep-awake: unavailable: boom".

func TestStartKeepAwake_ReleaseErrorIsWarning(t *testing.T)
// release returns errors.New("gone"); ddc.log contains "keep-awake: release failed: gone".

func TestExecute_ReleasesKeepAwakeOnError(t *testing.T)
// fake counts acquires/releases; Execute(...) with the zero-node setup of TestStreamingCollect_ZeroNodes
// returns an error containing "no hosts found"; acquires == 1, releases == 1.
```

`keepawake_windows_test.go`:
```go
func TestKeepAwake_RealWindowsSetsSystemRequired(t *testing.T)
// reads SystemExecutionState via powrprof.dll CallNtPowerInformation(16, nil, 0, &state, 4);
// uses the default keepAwake: while held, state&0x1 != 0; after release, state&0x1 == 0,
// unless the bit was already set before acquiring (another process holds it) — then skip that last check.
```

- [ ] **Step 2: Run them to see them fail**

Run: `SKIP_INTEGRATION_SETUP=true go test -short -run 'KeepAwake' ./cmd/root/collection/`
Expected: build failure, `undefined: keepAwake` / `startKeepAwake` / `keepAwakeReason`.

- [ ] **Step 3: Implement `keepawake.go`, then add the dependency**

`startKeepAwake` calls `keepAwake(keepAwakeReason)`:
- `errors.Is(err, power.ErrUnsupported)` → INFO `keep-awake: not supported on <runtime.GOOS>`, return a no-op;
- other error → WARN `keep-awake: unavailable: <err>`, return a no-op;
- success → INFO `keep-awake: idle system sleep blocked until the collection ends`; the returned stop releases once (`sync.Once`): INFO `keep-awake: released`, or WARN `keep-awake: release failed: <err>`.

In `Execute`: `stop := startKeepAwake()` then `defer stop()` before `return ExecuteStreamingCollect(...)`.
With the import in place, run `go get github.com/crgimenes/native@v0.1.18` (not `go mod tidy`). Expected go.mod diff for this task: `github.com/crgimenes/native v0.1.18` without `// indirect`, and `github.com/ebitengine/purego v0.11.3 // indirect`. Nothing else.

- [ ] **Step 4: Run the tests**

Run: `SKIP_INTEGRATION_SETUP=true go test -short -run 'KeepAwake|TestExecute_' ./cmd/root/collection/ -v`
Expected: all PASS, including `TestKeepAwake_RealWindowsSetsSystemRequired` on this machine.

- [ ] **Step 5: Cross-compile the probe targets**

Run, for each of `darwin/amd64 darwin/arm64 windows/amd64 windows/arm64 linux/amd64 linux/arm64`:
`CGO_ENABLED=0 GOOS=<os> GOARCH=<arch> go build -trimpath -o /tmp/ddc-<os>-<arch> .`
Expected: all six succeed; `go version -m /tmp/ddc-darwin-arm64 | grep purego` prints one `dep` line; the linux binaries have none. Delete the `/tmp/ddc-*` binaries afterwards.

- [ ] **Step 6: Record the decision**

Add `D080` to `docs/architecture/decisions.md`, Collection Transport table, after D077:
Decision "Keep the collecting machine awake; Go 1.27"; Choice: `collection.Execute` holds a `crgimenes/native/power` token (macOS IOKit `PreventUserIdleSystemSleep`, Windows `SetThreadExecutionState`, Linux unsupported), released when collection ends; Go 1.27.4 with `godebug default=go1.24`, macOS minimum 13. Rationale: long silent collections on laptops lose the VPN and the cluster when the machine idle-sleeps; the library needs Go 1.27; the godebug pin keeps the 1.24 runtime defaults (TLS SHA-1, `SSL_CERT_FILE` on Windows/macOS, URL parsing).

- [ ] **Step 7: Commit**

```bash
gofmt -l cmd/root/collection/keepawake*.go cmd/root/collection/collector.go   # expect no output
git add go.mod go.sum cmd/root/collection/keepawake.go cmd/root/collection/keepawake_test.go cmd/root/collection/keepawake_windows_test.go cmd/root/collection/collector.go docs/architecture/decisions.md
git commit -m "wip: keep macOS and Windows awake while a collection runs"
```

---

### Task 3: Remove the queries-perf pre-flight count; warn when unfiltered

**Files:**
- Modify: `cmd/root/collection/rockscollect.go:475-622` (`collectQueriesPerf`)
- Modify: `cmd/root/collection/rockscollect_test.go` (`newQueriesPerfCollector` ~450; tests at ~537, ~561; `-count` cases at ~634, ~678)

**Interfaces:**
- Consumes: nothing new.
- Produces: `collectQueriesPerf` keeps its signature `func collectQueriesPerf(c Collector, cs CopyStrategy, host, nodeType, dbPath string, args RocksCollectArgs) ([]helpers.CollectedFile, error)`; it issues no `HostExecute` call any more.

- [ ] **Step 1: Make the fakes refuse `-count`, and write the failing tests**

- `newQueriesPerfCollector`: its `hostExecuteFunc` returns `fmt.Errorf("unexpected host command: %v", args)` for every command (drop the `-count` answer). Update the comment.
- Delete `TestCollectQueriesPerf_CountGuardSkipsStream` and `TestCollectQueriesPerf_CountFailureNonGuardStillStreams` (they test the removed count).
- Remove the `case strings.Contains(cmd, "-count")` branches in `TestRunRocksDBCollection_QueriesPerfGuardSkip` and `TestRunRocksDBCollection_ReportsIncompleteQueriesPerf`.
- Add:
```go
func TestCollectQueriesPerf_NoPreflightCount(t *testing.T)
// keepaliveFreeCollector over newQueriesPerfCollector([]string{perfLine(1), perfLine(2)}, nil),
// hostExecuteFunc records every command; after collectQueriesPerf: no recorded command contains "-count",
// err == nil, len(files) == 1, noKeepaliveCalls == 1.

func TestCollectQueriesPerf_WarnsWithoutDayLimit(t *testing.T)
// ddc.log captured; run with RocksCollectArgs{} → log contains
// "rocksdb-viewer queries_perf on dremio-master-0: no day limit (--queries-perf-num-days 0); exporting the full job history, which can take hours on large catalogs";
// run again with RocksCollectArgs{QueriesPerfDays: 7} into a fresh log → log does not contain "no day limit".
```

- [ ] **Step 2: Run them to see them fail**

Run: `SKIP_INTEGRATION_SETUP=true go test -short -run 'CollectQueriesPerf|RunRocksDBCollection' ./cmd/root/collection/`
Expected: FAIL — the current code issues `-count` (the fakes now error on it, so `TestCollectQueriesPerf_NoPreflightCount` sees the command) and no "no day limit" line is logged.

- [ ] **Step 3: Change `collectQueriesPerf`**

Remove the `-count` `HostExecute`, its memory-guard check, the `strconv.Atoi` parse and `totalRecords`, and every branch that used it (percentage, `Finalizing queries-perf data`). Keep the existing unknown-total messages: `Collecting queries-perf data`, `Collecting queries-perf data (N queries)`, final `Collected queries-perf data (N queries)` when N > 0. Log lines become `rocksdb-viewer queries_perf streaming on %s: %d records` and `rocksdb-viewer queries_perf on %s: streamed %d records`. When `filterArgs == ""`, log the WARN from Global Constraints once, before streaming. Drop `strconv` if unused.

- [ ] **Step 4: Run the package tests**

Run: `SKIP_INTEGRATION_SETUP=true go test -short ./cmd/root/collection/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
gofmt -l cmd/root/collection/rockscollect.go cmd/root/collection/rockscollect_test.go
git add cmd/root/collection/rockscollect.go cmd/root/collection/rockscollect_test.go
git commit -m "wip: drop the queries-perf pre-flight count, warn on unfiltered export"
```

---

### Task 4: Return failed viewer exports from RunRocksDBCollection

**Files:**
- Modify: `cmd/root/collection/rockscollect.go` (type next to `IncompleteCollectionError` ~37-50; `RunRocksDBCollection` ~156-316)
- Modify: `cmd/root/collection/streaming_collect.go:1085-1091` (adapt the call only; reporting is Task 6)
- Modify: `cmd/root/collection/rockscollect_test.go` (all `RunRocksDBCollection` call sites)

**Interfaces:**
- Consumes: Task 3's `collectQueriesPerf` (no `-count`).
- Produces:
```go
type FailedCollectionError struct {
	Item  string // "queries-perf", "cluster_stats", "sys.options", "wlm_queues", "rocksdb-viewer"
	Cause error
}
func (e *FailedCollectionError) Error() string // fmt.Sprintf("%s failed (%v)", e.Item, e.Cause)
func (e *FailedCollectionError) Unwrap() error

// RocksDBResult is what RunRocksDBCollection collected and what it could not.
type RocksDBResult struct {
	Files      []helpers.CollectedFile
	Incomplete []*IncompleteCollectionError
	GuardSkips []*MemoryGuardError
	Failed     []*FailedCollectionError
}
func RunRocksDBCollection(args RocksCollectArgs) (RocksDBResult, error)
```
The `error` return stays for setup failures before any export (uname, binary, temp dir, write, upload, chmod); Task 6 turns it into a `rocksdb-viewer` failure at the caller.

- [ ] **Step 1: Write the failing tests**

Change every existing call to `res, err := RunRocksDBCollection(...)` and read `res.Files`, `res.Incomplete`, `res.GuardSkips`. Add:
```go
func TestFailedCollectionError_Format(t *testing.T)
// e := &FailedCollectionError{Item: "queries-perf", Cause: errors.New("x")}
// e.Error() == "queries-perf failed (x)"; errors.Unwrap(e).Error() == "x".

func TestRunRocksDBCollection_ReportsFailedTypeExport(t *testing.T)
// mockStreamCollector as in TestRunRocksDBCollection_GuardSkipsContinue, SystemTables {"options","tables"};
// "-type sys.options" → ("", errors.New("connection reset")); "-type sys.tables" → (`{"t":1}`, nil).
// err == nil; len(res.Failed) == 1; res.Failed[0].Item == "sys.options";
// strings.Contains(res.Failed[0].Error(), "connection reset"); sys.tables.json in res.Files.

func TestRunRocksDBCollection_ReportsFailedQueriesPerf(t *testing.T)
// keepaliveFreeCollector over newQueriesPerfCollector(nil, errors.New("dial tcp: lookup api-server: no such host")),
// hostExecuteFunc answering CURRENT/uname/chmod/rm/-version/cluster_stats as the existing tests do;
// CollectQueriesPerf: true, QueriesPerfDays: 7.
// res.Failed[0].Error() == "queries-perf failed (execute rocksdb-viewer queries_perf: dial tcp: lookup api-server: no such host)";
// len(res.Incomplete) == 0; len(res.GuardSkips) == 0.
```
Also assert `len(res.Failed) == 0` in `TestRunRocksDBCollection_QueriesPerfGuardSkip`, `TestRunRocksDBCollection_ReportsIncompleteQueriesPerf`, `TestRunRocksDBCollection_GuardSkipsContinue` and `TestRunRocksDBCollectionSkipsWhenNoCatalog` (Review Focus 3).

- [ ] **Step 2: Run them to see them fail**

Run: `SKIP_INTEGRATION_SETUP=true go test -short -run 'RunRocksDBCollection|FailedCollectionError' ./cmd/root/collection/`
Expected: build failure (`undefined: FailedCollectionError`, `RunRocksDBCollection` returns 4 values).

- [ ] **Step 3: Implement**

Add the type. In `RunRocksDBCollection` collect into a `RocksDBResult`: `recordRocksType`'s `case err != nil` also appends `&FailedCollectionError{Item: dataType, Cause: err}`; the queries-perf `case err != nil` also appends `&FailedCollectionError{Item: "queries-perf", Cause: err}`. The ERROR log lines stay. Adapt `streaming_collect.go:1085-1091` to the struct (`nodeCollected = append(nodeCollected, res.Files...)`, `nodeIncomplete = res.Incomplete`, `nodeGuardSkips = res.GuardSkips`); `res.Failed` is used in Task 6.

- [ ] **Step 4: Run the package tests**

Run: `SKIP_INTEGRATION_SETUP=true go test -short ./cmd/root/collection/` → `ok`.

- [ ] **Step 5: Commit**

```bash
gofmt -l cmd/root/collection/rockscollect.go cmd/root/collection/rockscollect_test.go cmd/root/collection/streaming_collect.go
git add cmd/root/collection/rockscollect.go cmd/root/collection/rockscollect_test.go cmd/root/collection/streaming_collect.go
git commit -m "wip: return failed viewer exports from RunRocksDBCollection"
```

---

### Task 5: Treat a store missing from the catalog as "not available"

**Files:**
- Create: `cmd/root/collection/storemissing.go`
- Create: `cmd/root/collection/storemissing_test.go`
- Modify: `cmd/root/collection/rockscollect.go` (`collectRocksType` ~321-401, `recordRocksType` ~168-179)
- Modify: `cmd/root/collection/rockscollect_test.go`
- Modify: `docs/architecture/decisions.md` (insert D079 directly before D080; Task 6 inserts D078 before it, giving D077, D078, D079, D080)

**Interfaces:**
- Consumes: Task 4's `RocksDBResult` and `recordRocksType`.
- Produces:
```go
// ErrStoreNotInCatalog marks a viewer export whose RocksDB store is missing from the catalog.
var ErrStoreNotInCatalog = errors.New("not available in this catalog")
// storeMissingName returns the column family when line (trimmed of spaces and \r) is exactly
// "error: '<cf>' column family not found".
func storeMissingName(line string) (cf string, ok bool)
```
`collectRocksType` returns `fmt.Errorf("%w (%s column family not found)", ErrStoreNotInCatalog, cf)` for this case, so `err.Error() == "not available in this catalog (wlmqueue column family not found)"`.

- [ ] **Step 1: Write the failing tests**

```go
func TestStoreMissingName(t *testing.T)
// "error: 'wlmqueue' column family not found"          → ("wlmqueue", true)
// "  error: 'rulesmanager' column family not found\r"   → ("rulesmanager", true)   // Review Focus 2
// "error: open db: IO error"                            → ("", false)
// "error: 'x' column family not found extra"            → ("", false)
// `{"msg":"error: 'x' column family not found"}`        → ("", false)
// ""                                                    → ("", false)

func TestCollectRocksType_StoreMissingOnMergedOutput(t *testing.T)
// streamCollector("error: 'wlmqueue' column family not found", &RemoteExitError{Code: 1}), item "wlm_queues", file "queues.json":
// errors.Is(err, ErrStoreNotInCatalog); err.Error() == "not available in this catalog (wlmqueue column family not found)";
// cf == nil; no regular files in dir.

func TestCollectRocksType_StoreMissingOnKeepaliveFree(t *testing.T)
// keepaliveFreeCollector over streamCollector("", &RemoteExitError{Code: 1, Stderr: "error: 'wlmqueue' column family not found"}):
// same assertions; one keepalive-free call.

func TestCollectRocksType_OtherExit1IsPlainError(t *testing.T)
// &RemoteExitError{Code: 1, Stderr: "error: open db: IO error"} → err != nil, !errors.Is(err, ErrStoreNotInCatalog), not *MemoryGuardError.

func TestCollectRocksType_StoreMissingLineNeverWritten(t *testing.T)   // Review Focus 5
// streamCollector("{\"a\":1}\nerror: 'wlmqueue' column family not found", nil): err == nil, file content == "{\"a\":1}\n".

func TestRunRocksDBCollection_StoreMissingIsNotFailed(t *testing.T)
// ddc.log captured; CollectWLM: true; "-type wlm_queues" → ("error: 'wlmqueue' column family not found", &RemoteExitError{Code: 1});
// "-type wlm_rules" → ("error: 'rulesmanager' column family not found", &RemoteExitError{Code: 1}); "-type wlm_engines" → (`{"e":1}`, nil).
// err == nil; len(res.Failed) == 0; engines.json in res.Files;
// ddc.log contains "rocksdb-viewer -type wlm_queues on dremio-master-0: not available in this catalog (wlmqueue column family not found)".
```

- [ ] **Step 2: Run them to see them fail**

Run: `SKIP_INTEGRATION_SETUP=true go test -short -run 'StoreMissing|OtherExit1' ./cmd/root/collection/`
Expected: build failure (`undefined: storeMissingName`, `ErrStoreNotInCatalog`).

- [ ] **Step 3: Implement**

`storemissing.go`: the sentinel and `storeMissingName` (anchored match on the trimmed line).
`collectRocksType`: in the handler, next to the existing memory-guard line check, a line matching `storeMissingName` sets a captured `missingCF` and returns without writing it (exactly like the guard line). On `err != nil`, after the memory-guard check: if `missingCF == ""` and `errors.As(err, &rexit)`, try `storeMissingName(rexit.Stderr)`; if a name was found, discard the partial file and return the wrapped `ErrStoreNotInCatalog`. Only the error path reports it; with `err == nil` the export is kept as usual.
`recordRocksType`: new first case `errors.Is(err, ErrStoreNotInCatalog)` → `simplelog.Infof("rocksdb-viewer -type %s on %s: %v", dataType, host, err)` and nothing else (no failed item, no file).

- [ ] **Step 4: Run the package tests**

Run: `SKIP_INTEGRATION_SETUP=true go test -short ./cmd/root/collection/` → `ok`.

- [ ] **Step 5: Record the decision**

Add `D079` to the Collection Transport table: "Viewer export whose store is missing" → `error: '<cf>' column family not found` (exit 1) is "not available in this catalog": INFO, no file, not FAILED; detected like the memory-guard line (handler line / `RemoteExitError.Stderr`). Rationale: OSS catalogs lack the WLM stores; reporting them as FAILED would flag every OSS run.

- [ ] **Step 6: Commit**

```bash
gofmt -l cmd/root/collection/storemissing*.go cmd/root/collection/rockscollect.go cmd/root/collection/rockscollect_test.go
git add cmd/root/collection/storemissing.go cmd/root/collection/storemissing_test.go cmd/root/collection/rockscollect.go cmd/root/collection/rockscollect_test.go docs/architecture/decisions.md
git commit -m "wip: treat a store missing from the catalog as not available"
```

---

### Task 6: Report failed exports on all surfaces; success rate

**Files:**
- Modify: `cmd/root/collection/summary.go:31-32` (new field after `MemoryGuardSkips`)
- Modify: `cmd/root/collection/streaming_collect.go` (`nodeDoneStatus` ~746-770; per-node block ~1053-1125; totals ~856-857; summary ~1229-1255)
- Modify: `cmd/root/collection/collector.go:270-365` (`successRate`, `logDistributedCollectionSummary`)
- Modify: `cmd/root/collection/collector_test.go`, `cmd/root/collection/streaming_collect_test.go` (`mockCopyStrategy` ~112-124)
- Modify: `docs/architecture/decisions.md` (D078)

**Interfaces:**
- Consumes: Task 4's `RocksDBResult.Failed`, `FailedCollectionError`.
- Produces:
```go
// in SummaryInfo
FailedCollections []string `json:"failedCollections,omitempty"`

func nodeDoneStatus(collected int, bytes int64, skipped []string, incomplete []*IncompleteCollectionError,
	guardSkips []*MemoryGuardError, failed []*FailedCollectionError) string

func logDistributedCollectionSummary(collectionMode collects.CollectionMode, coordinators, executors []string,
	files []helpers.CollectedFile, totalFailedFiles, totalFailedNodes, totalSkippedFiles, incomplete, guardSkips,
	failedCollections []string, nodesConnectedTo int, duration time.Duration)

// formatSuccessRate prints collected/attempts truncated to one decimal, from integers.
func formatSuccessRate(collected, attempts int) string // e.g. "99.9%"
```

- [ ] **Step 1: Write the failing tests**

`collector_test.go`:
```go
func TestFormatSuccessRate(t *testing.T)
// (1999, 2000) → "99.9%"; (10, 10) → "100.0%"; (2, 3) → "66.6%"; (29, 50) → "58.0%" (Review Focus 1); (0, 4) → "0.0%"

func TestSummaryInfo_FailedCollectionsOmittedWhenEmpty(t *testing.T)
// mirrors TestSummaryInfo_IncompleteCollectionsOmittedWhenEmpty with
// SummaryInfo{FailedCollections: []string{"dremio-master-0: queries-perf failed (x)"}} → contains `"failedCollections"`.

func TestLogDistributedCollectionSummary_FailedSection(t *testing.T)
// 8 collected files, failedCollections {"m0: queries-perf failed (x)"}, guardSkips {"m0: sys.jobs_recent (y)"},
// incomplete {"m0: queries-perf incomplete after 5 records (z)"}:
// ddc.log contains "Failed Collections: 1", "FAILED COLLECTIONS:", "   m0: queries-perf failed (x)",
// "Success Rate: 80.0% (8/10)"   (guard skip counts as a failed attempt, the incomplete export does not).
```
Update `TestLogDistributedCollectionSummary_GuardSection` and every other caller for the new parameter (`nil`).
`TestNodeDoneStatus`: pass `nil` failed in existing cases; add
- `failed`: `[]*FailedCollectionError{{Item: "queries-perf"}}` → `"Done: 3 files (1.0KB), 0 skipped, FAILED: queries-perf"`;
- `incomplete, guard and failed`: inc + `{g1}` + `{{Item: "wlm_queues"}}` → `"Done: 3 files (1.0KB), 0 skipped, INCOMPLETE: queries-perf (187442 records), MEMORY GUARD: sys.jobs_recent, FAILED: wlm_queues"`.

`streaming_collect_test.go`: `mockCopyStrategy` gets a `summary string` field that `ArchiveDiag(o, _)` stores. Add:
```go
func TestStreamingCollect_RocksDBQueriesPerfFailureReported(t *testing.T)
// setup of TestStreamingCollect_RocksDBViewer_UsesAutodetectedDir; hostExecuteFunc also returns
// ("", errors.New("dial tcp: lookup api-server: no such host")) for commands containing "-type queries_perf";
// Args.CollectQueriesPerf = true, QueriesPerfNumDays = 7. After ExecuteStreamingCollect (err == nil):
// json.Unmarshal(cs.summary) → FailedCollections ==
// []string{"coord1: queries-perf failed (execute rocksdb-viewer queries_perf: dial tcp: lookup api-server: no such host)"};
// ddc.log (captured) contains "FAILED COLLECTIONS:" and "coord1: queries-perf failed", and its "Success Rate:" line does not contain "100.0%".

func TestStreamingCollect_RocksDBSetupFailureReported(t *testing.T)
// same setup but copyToHostFunc returns ("", errors.New("upload denied")):
// FailedCollections == []string{"coord1: rocksdb-viewer failed (upload rocksdb-viewer to coord1: upload denied)"}.
```

- [ ] **Step 2: Run them to see them fail**

Run: `SKIP_INTEGRATION_SETUP=true go test -short ./cmd/root/collection/`
Expected: build failure (new parameters, `formatSuccessRate`, `FailedCollections` undefined).

- [ ] **Step 3: Implement**

- `summary.go`: the field.
- `streaming_collect.go`: per-node `nodeFailed []*FailedCollectionError` from `res.Failed`; when `RunRocksDBCollection` returns an error, keep the ERROR log and set `nodeFailed = []*FailedCollectionError{{Item: "rocksdb-viewer", Cause: err}}`. Under the mutex append `fmt.Sprintf("%s: %v", host, f)` to `totalFailedCollections`. Pass `nodeFailed` to `nodeDoneStatus`, set `summaryInfo.FailedCollections = totalFailedCollections`, pass it to `logDistributedCollectionSummary`.
- `nodeDoneStatus`: append `", FAILED: " + strings.Join(items, ", ")` after the MEMORY GUARD part when `failed` is non-empty.
- `logDistributedCollectionSummary`: `totalFailures := len(totalFailedFiles) + len(totalFailedNodes) + len(failedCollections)`; print the `FAILED COLLECTIONS:` header also when only `failedCollections` is non-empty, with lines `"   " + entry` (Info, same style as the existing lines). Success rate: `successRate(len(files), totalFailures+len(guardSkips), len(totalSkippedFiles))`; print `"Success Rate: %s (%d/%d)"` with `formatSuccessRate(len(files), attempts)`.
- `formatSuccessRate`: per-mille = `collected*1000/attempts` (integer division), printed as `"%d.%d%%"` of `pm/10, pm%10`; `"0.0%"` when attempts == 0.

- [ ] **Step 4: Run the package tests**

Run: `SKIP_INTEGRATION_SETUP=true go test -short ./cmd/root/collection/` → `ok`.

- [ ] **Step 5: Record the decision**

Add `D078` to the Collection Transport table (before D079): "Report failed viewer exports; no queries-perf pre-flight count" → `FailedCollectionError` on node status (`FAILED:`), `summary.json` (`failedCollections`) and the ddc.log summary; failed exports and memory-guard skips count as failed attempts in the success rate, printed truncated; queries-perf runs without `-count`. Rationale: a collection lost all queries-perf data while reporting 100%; the count ran on a pinged, marker-less exec with no time limit and failed silently.

- [ ] **Step 6: Commit**

```bash
gofmt -l cmd/root/collection/summary.go cmd/root/collection/streaming_collect.go cmd/root/collection/collector.go cmd/root/collection/collector_test.go cmd/root/collection/streaming_collect_test.go
git add cmd/root/collection/summary.go cmd/root/collection/streaming_collect.go cmd/root/collection/collector.go cmd/root/collection/collector_test.go cmd/root/collection/streaming_collect_test.go docs/architecture/decisions.md
git commit -m "wip: report failed viewer exports in status, summary.json and ddc.log"
```

---

### Task 7: Flag text — `*-num-days 0` and `--collector-timeout`

**Files:**
- Modify: `cmd/root.go:1364` (deprecate), `cmd/root.go:1387`, `cmd/root.go:1409-1412` (usage text)
- Modify: `README.md:257-261` (descriptions), `README.md:290` (remove row)
- Modify: `cmd/root_flags_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: nothing used by later tasks.

- [ ] **Step 1: Write the failing tests** in `cmd/root_flags_test.go`

```go
func TestNumDaysFlagsDocumentZero(t *testing.T)
// on K8sStandardCmd.Flags(): "queries-json-num-days", "server-logs-num-days", "tracker-json-num-days",
// "vacuum-log-num-days" → Usage has suffix "(0 = no limit)";
// "queries-perf-num-days" → Usage has suffix "(0 = no limit: exports the full job history, which can take hours on large catalogs)".

func TestCollectorTimeoutDeprecated(t *testing.T)
// f := CollectCmd.PersistentFlags().Lookup("collector-timeout"); f != nil;
// f.Deprecated == "it has no effect and will be removed in a future release"; f.Hidden == true;
// K8sStandardCmd.ParseFlags([]string{"--collector-timeout=5m"}) returns nil.
```

- [ ] **Step 2: Run them to see them fail**

Run: `SKIP_INTEGRATION_SETUP=true go test -short -run 'NumDaysFlagsDocumentZero|CollectorTimeoutDeprecated' ./cmd/`
Expected: FAIL on the usage suffixes and on `Deprecated == ""`.

- [ ] **Step 3: Implement**

- Append ` (0 = no limit)` to the four usage strings at 1409-1412, and ` (0 = no limit: exports the full job history, which can take hours on large catalogs)` at 1387.
- After 1364: `if err := CollectCmd.PersistentFlags().MarkDeprecated("collector-timeout", "it has no effect and will be removed in a future release"); err != nil {` … the same error handling as the `MarkHidden("pid")` call above it.
- README rows 257-261: append the same text to the description column. Remove the `--collector-timeout` row (290).

- [ ] **Step 4: Run the tests**

Run: `SKIP_INTEGRATION_SETUP=true go test -short ./cmd/` → `ok`.

- [ ] **Step 5: Commit**

```bash
gofmt -l cmd/root.go cmd/root_flags_test.go
git add cmd/root.go cmd/root_flags_test.go README.md
git commit -m "wip: document num-days 0, deprecate --collector-timeout"
```

---

### Task 8: Whole-branch verification and squash

**Files:** none new.

- [ ] **Step 1: Full checks**

- `SKIP_INTEGRATION_SETUP=true go test -short ./...` → all `ok`, apart from the known environmental failures.
- `go vet ./...` → clean.
- `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 run ./...` → `0 issues.`
- gosec with the pinned version (`-exclude=G204,G402 ./...`) completes.
- Cross-compile the six targets as in Task 2 Step 5 → all succeed.
- `git diff main -- go.mod`: `go 1.27.4`, `godebug default=go1.24`, `crgimenes/native v0.1.18`, `ebitengine/purego v0.11.3 // indirect`; nothing else.
- `git diff main --stat`: no files outside the ones named in Tasks 1-7, plus the spec and plan.
- Grep the diff for customer names, hostnames and user IDs: none.

- [ ] **Step 2: Build the binary**

Run: `go build -o bin/ddc.exe .` → succeeds.

- [ ] **Step 3: Live smoke run (ask the user for the cluster first)**

Read-only `bin/ddc.exe collect k8s standard` against the test cluster the user names. Pass: queries-perf collected; ddc.log has no `-type queries_perf -count` and has `keep-awake: idle system sleep blocked until the collection ends`; `summary.json` has no `failedCollections`; on an OSS cluster, WLM stores missing from the catalog show the `not available in this catalog` INFO line and no `FAILED:`.

- [ ] **Step 4: Finish**

Use superpowers:finishing-a-development-branch. On the user's go-ahead, squash the `wip:` commits (`git reset --soft $(git merge-base main HEAD)`) into one commit that includes the spec and this plan:

```
feat: report failed RocksDB exports, keep collector awake, Go 1.27

- report viewer exports that fail on the node status line (FAILED:),
  in summary.json (failedCollections) and in the ddc.log summary; failed
  exports and memory-guard skips lower the success rate (printed truncated)
- treat a catalog store that doesn't exist as "not available" (INFO)
- drop the queries-perf pre-flight -count; warn on an unfiltered export
- document "0 = no limit" for the *-num-days flags; deprecate the unused
  --collector-timeout
- keep macOS and Windows from idle-sleeping during a collection
  (crgimenes/native/power)
- build with Go 1.27.4 (godebug default=go1.24 keeps runtime defaults);
  macOS 13 or newer is now required; golangci-lint v2.15.0, newer gosec

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Release prerequisites from the spec stay with the user: Go 1.27.4 on the release machine, the Mac check before the 4.1.0 release, and the CHANGELOG entry including the macOS 13 minimum.
