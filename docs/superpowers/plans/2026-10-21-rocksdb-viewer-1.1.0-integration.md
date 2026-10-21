# Integrate dremio-rocksdb-viewer 1.1.0 — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Vendor viewer 1.1.0 into DDC and make DDC handle it correctly:
- a time window for `sys.jobs_recent`;
- memory-guard (exit 3) skips on every transport;
- viewer output streamed to file;
- no 64 KiB line hang;
- guard skips reported in their own section.

**Architecture:**
- **Exit 3 detection.** A small set of primitives in package `collection` (`memguard.go`) turns any transport's error into an exit code and spots the viewer's guard line.
- **`collectRocksType`** streams each viewer type to a temp file and renames it on success. K8s uses the keepalive-free stream with the end marker, except `wlm_cluster_usage`.
- **`collectQueriesPerf`** gets exit-3 handling that discards partial data.
- **`RunRocksDBCollection`** returns guard skips as a fourth value. They flow into a new `summary.json` field, a new summary-log section and a new node-status label.

**Tech Stack:**
- Go 1.24.13; client-go v0.32.3 (the `util/exec` `ExitError` interface).
- Docker (alpine image) to run the static Linux viewer against local RocksDB catalogs.
- `kubectl` read-only on the `healthcheck-aks` cluster.

**Spec:** `docs/superpowers/specs/2026-10-21-rocksdb-viewer-1.1.0-integration-design.md`

## Global Constraints

- **New viewer binaries** come from `C:\Users\chufe\Workspaces\golang\dremio-rocksdb-viewer\bin\`:

  | File | Size (bytes) | SHA-256 |
  |---|---|---|
  | `dremio-rocksdb-viewer-linux-amd64` | 2,451,652 | `e70081001d175fb98f858e4d4bc06451766054b59401ce023f41f22c64d70ab9` |
  | `dremio-rocksdb-viewer-linux-arm64` | 2,179,748 | `49a5746fd76c52b714b1ae252ac8d28b67596f32d1051f32a8fa6acfbf843f6f` |

- **Old viewer, for comparison:** linux-amd64 at `HEAD` has SHA-256 `0dde8d0f7d40faa1fe4f7fda586676b538783de07ad4addc7349fea28b9133e7`.
- **Viewer exit codes:** 0 ok; 1 error; 2 flag or panic; **3 memory guard**.
- **Guard stderr line** starts with exactly `error: memory guard: ` and is followed by `refusing to start|aborting`, then `: source=<cgroup2|cgroup1|host> used_mib=<n> limit_mib=<n> reserve_mib=<n>`.
- **`sys.jobs_recent` window:**
  - if `StartDate != ""` or `Days > 0`, use the same args as `buildQueriesPerfFilterArgs` (no cap);
  - otherwise use ` -days min(QueriesPerfDays, 7)`, with 7 when `QueriesPerfDays <= 0`.
  - No other type gets filter args.
- **Executor:** `wlm_cluster_usage` always uses `HostExecuteAndStream`. Every other viewer type uses `KeepaliveFreeStreamer.HostExecuteAndStreamNoKeepalive` when the collector implements it, otherwise `HostExecuteAndStream`.
- **Exit 3:**
  - discard that type's output;
  - log ERROR with the guard text;
  - record a guard skip;
  - continue with the next type;
  - **never retry**;
  - never abort `RunRocksDBCollection`, and the viewer cleanup (`rm -f`) still runs.
- **Reporting:**
  - `summary.json` field **`memoryGuardSkips`** (`[]string`, `omitempty`);
  - summary log count line `  Memory Guard Skips: N` and list header `MEMORY GUARD SKIPS:`, both only when N > 0;
  - node status suffix `, MEMORY GUARD: <item>[, <item>…]`;
  - entry text `<host>: <item> (<detail>)`, or `<host>: <item> (memory guard, exit 3)` when no guard line was captured;
  - #339 INCOMPLETE counts and wording stay exactly as they are;
  - the success-rate calculation is unchanged.
- **Long lines:** `ExecuteAndStreamOutput` allows lines up to **64 MiB** (`64 << 20`). A longer line returns an error; it never hangs.
- **Item names:** the viewer type string (`cluster_stats`, `sys.jobs_recent`, `wlm_queues`, …). queries_perf uses `queries-perf`, matching the existing INCOMPLETE item.
- **Tests:** `SKIP_INTEGRATION_SETUP=true go test -short ./...` on Windows (no `-race`), and `golangci-lint run` must pass. After code changes, always build with `go build -o bin/ddc.exe .`.
  - Always set `SKIP_INTEGRATION_SETUP=true`: the `integrationtest/kube` `TestMain` ignores `-short` and otherwise deploys a test Dremio into the current kube context.
  - Check formatting with `gofmt -l` on changed files only, because `core.autocrlf=true` makes it list every CRLF file.
- **No commits during tasks.** The user reviews everything, then the work ends in **one squashed commit** on branch `feat/rocksdb-viewer-1.1.0`, including the spec and this plan. Push or open a PR only if the user chooses that in the finish step (`superpowers:finishing-a-development-branch`).
- **Working files** (extracted binaries, outputs, scripts, bundles) go in `SCRATCH=$TEMP/ddc-viewer-110`, never in the repo.
- **Live cluster:** collection only, on context `healthcheck-aks`, namespace `default`. Never deploy anything or change the cluster.

## Review Focus

1. **A non-zero exit other than 3** (1, 2, 137 as in the incident) must stay a plain error, never a guard skip. Test: `TestCollectRocksType_Exit137IsPlainError` (Task 5).
2. **Exit 3 with no captured guard line** (the transport lost stderr) must still be a guard skip, reported as `memory guard, exit 3`. Test: the `no detail` case in `TestAsMemoryGuardError` (Task 3).
3. **Several types hitting exit 3 in a row** must all be recorded, the next type must still run, and the viewer cleanup must still happen. Test: `TestRunRocksDBCollection_GuardSkipsContinue` (Task 5).
4. **Streamed files now end with a newline**, while today's `HostExecute` path trimmed it. Cluster-ID detection must still parse `cluster-stats.json`. Test: `TestFindClusterID_ParsesStreamedClusterStats` (Task 5).
5. **A queries_perf `-count` failure that is not exit 3** must still run the data stream (unchanged behaviour). Test: `TestCollectQueriesPerf_CountFailureNonGuardStillStreams` (Task 6).

---

### Task 1: Branch, re-vendor 1.1.0, log the viewer version

**Files:**
- Replace: `cmd/local/rockscollect/rocksdb/dremio-rocksdb-viewer-linux-amd64`, `cmd/local/rockscollect/rocksdb/dremio-rocksdb-viewer-linux-arm64`
- Modify: `cmd/root/collection/rockscollect.go` (`RunRocksDBCollection`, right after the `chmod +x` call)
- Test: `cmd/root/collection/rockscollect_test.go`

**Interfaces:**
- Produces: the log line `rocksdb-viewer on <host>: <trimmed -version output>` (INFO), or `rocksdb-viewer -version on <host>: <err>` (WARN).

- [ ] **Step 1: Create the branch**

Run: `git switch -c feat/rocksdb-viewer-1.1.0` (from `main`; the only untracked file should be the spec).
Expected: `Switched to a new branch 'feat/rocksdb-viewer-1.1.0'`.

- [ ] **Step 2: Keep the old amd64 binary for comparison (Task 8)**

Run:
```bash
SCRATCH="$TEMP/ddc-viewer-110"; mkdir -p "$SCRATCH"
git show HEAD:cmd/local/rockscollect/rocksdb/dremio-rocksdb-viewer-linux-amd64 > "$SCRATCH/viewer-1.0.0-amd64"
sha256sum "$SCRATCH/viewer-1.0.0-amd64"
```
Expected: `0dde8d0f…33e7`.

- [ ] **Step 3: Copy and verify the new binaries**

Copy both files from the viewer `bin\` folder into `cmd/local/rockscollect/rocksdb/`. Then run `sha256sum` and `ls -l` on both.
Expected: the sizes and SHA-256s exactly match the Global Constraints table.

- [ ] **Step 4: Run the embed tests**

Run: `go test -short ./cmd/local/rockscollect/...`
Expected: PASS.

- [ ] **Step 5: Write the failing tests**

Both tests use a `mockStreamCollector` whose `hostExecuteFunc` records every command and answers as follows:
- the catalog check → `exists`;
- `uname -m` → `x86_64`;
- `chmod` and `rm -f` → `""`;
- `-type cluster_stats` → `{"cluster":"stub"}`.

Tests:
- `TestRunRocksDBCollection_LogsViewerVersion`:
  - `-version` → `dremio-rocksdb-viewer v1.1.0`;
  - assert the command `/tmp/dremio-rocksdb-viewer -version` was issued **after** `chmod +x /tmp/dremio-rocksdb-viewer` and **before** any `-type` command.
- `TestRunRocksDBCollection_VersionFailureNonFatal`:
  - `-version` returns `errors.New("exec failed")`;
  - assert `RunRocksDBCollection` returns `err == nil` and a collected file whose base name is `cluster-stats.json`.

- [ ] **Step 6: Run them to verify they fail**

Run: `go test -short -run 'TestRunRocksDBCollection_(LogsViewerVersion|VersionFailureNonFatal)' ./cmd/root/collection/`
Expected: `LogsViewerVersion` FAILS (no `-version` command was issued).

- [ ] **Step 7: Implement the version call**

In `RunRocksDBCollection`, after the successful `chmod`, call `c.HostExecute(false, host, rocksdbViewerRemotePath+" -version")`. Log the result at INFO, or the error at WARN, using the texts from the Interfaces block. Never return the error.

- [ ] **Step 8: Run the package tests**

Run: `go test -short ./cmd/root/collection/`
Expected: PASS.

---

### Task 2: `cli` exposes exit codes and survives long lines

**Files:**
- Modify: `cmd/root/cli/cli.go` (`UnableToStartErr`, `ExecuteAndStreamOutput`)
- Test: `cmd/root/cli/cli_test.go` (package `cli_test`)

**Interfaces:**
- Produces:
  - `func (u UnableToStartErr) Unwrap() error`, which returns `u.Err`, so `errors.As(err, &*exec.ExitError)` works through it;
  - an unexported const `maxOutputLineBytes = 64 << 20`.
- Produces the error shape: on a read error, `ExecuteAndStreamOutput` returns `fmt.Errorf("cmd '%s' output: %w", <joined args>, errors.Join(readErr, waitErr))`.
  - `readErr` is the first scanner `Err()`, for example `bufio.ErrTooLong`.
  - `waitErr` is the `UnableToStartErr` from `cmd.Wait()`, or nil.
  - Without a read error, the existing behaviour is unchanged.

- [ ] **Step 1: Write the failing tests (helper-process pattern)**

Add `TestHelperProcess`. It returns at once unless `DDC_CLI_TEST_HELPER` is set. Otherwise it runs one mode and then **always** calls `os.Exit`, so the test framework never prints `PASS`:

| Mode | Behaviour |
|---|---|
| `long1m` | print a line of 1 MiB `x`, then `end`; exit 0 |
| `toolong` | print one line of `(64<<20)+1` bytes; exit 0 |
| `exit3` | print `error: memory guard: refusing to start: source=host used_mib=1 limit_mib=1 reserve_mib=1` to stderr; exit 3 |
| `toolong-exit3` | as `toolong`, then exit 3 |

Add a helper `runHelper(t, mode) ([]string, error)`:
- call `t.Setenv("DDC_CLI_TEST_HELPER", mode)`;
- run `c.ExecuteAndStreamOutput(false, collect, "", os.Args[0], "-test.run=^TestHelperProcess$")` in a goroutine;
- call `t.Fatal("ExecuteAndStreamOutput hung")` if it hasn't returned after 60 s.

Tests:
- `TestExecuteAndStreamOutput_LongLineIntact` (`long1m`): `err == nil`; lines are `[strings.Repeat("x", 1<<20), "end"]`.
- `TestExecuteAndStreamOutput_LineOverMaxReturnsErrorNoHang` (`toolong`): returns before the timeout; `errors.Is(err, bufio.ErrTooLong)`.
- `TestExecuteAndStreamOutput_ExitCodeReachable` (`exit3`): `errors.As(err, &ee)` with `ee.ExitCode() == 3`; lines contain the guard line.
- `TestExecuteAndStreamOutput_TooLongAndExitCode` (`toolong-exit3`): `errors.Is(err, bufio.ErrTooLong)`, **and** `errors.As` reaches an `*exec.ExitError` with code 3.

- [ ] **Step 2: Run them to verify they fail**

Run: `go test -short -run 'TestExecuteAndStreamOutput_' ./cmd/root/cli/`
Expected:
- `LongLineIntact` fails, because the line is dropped;
- `ExitCodeReachable` fails, because there is no `Unwrap`;
- `LineOverMax` fails through the 60 s hang guard.

- [ ] **Step 3: Implement**

- Add `Unwrap()`.
- Give both scanners `Buffer(make([]byte, 0, 64*1024), maxOutputLineBytes)`.
- After each scan loop:
  - if `scanner.Err() != nil`, record the first read error under the existing mutex;
  - then run `io.Copy(io.Discard, <that pipe>)` so the process can finish.
- After `cmd.Wait()`, build the return value as described in the Interfaces block.

- [ ] **Step 4: Run the package tests**

Run: `go test -short ./cmd/root/cli/`
Expected: PASS, including the existing tests.

---

### Task 3: Memory-guard primitives and the typed remote exit error

**Files:**
- Create: `cmd/root/collection/memguard.go`, `cmd/root/collection/memguard_test.go`
- Modify: `cmd/root/collection/collector.go` (add `RemoteExitError` after `ErrStreamIncomplete`)
- Modify: `cmd/root/kubernetes/keepalive_free.go` (`eosLineWriter.finish`, `HostExecuteAndStreamNoKeepalive`)
- Test: `cmd/root/kubernetes/keepalive_free_test.go`

**Interfaces:**
- Consumes: `cli.UnableToStartErr.Unwrap` (Task 2).
- Produces (package `collection`):
  ```go
  // RemoteExitError: a remote command ran to completion with a non-zero exit code.
  type RemoteExitError struct {
      Code   int
      Stderr string // last stderr line, when the transport captured it
  }
  func (e *RemoteExitError) Error() string // "remote command exited with code <Code>"

  const viewerExitMemoryGuard = 3
  const memoryGuardPrefix = "error: memory guard: "

  // Order: *RemoteExitError, interface{ ExitStatus() int } (client-go), *exec.ExitError.
  func remoteExitCode(err error) (code int, ok bool)
  func memoryGuardDetail(line string) (detail string, ok bool) // text after memoryGuardPrefix
  func memoryGuardDetailIn(output string) string                // detail of the last guard line in a multi-line output, "" if none

  type MemoryGuardError struct {
      Item   string // viewer item, e.g. "sys.jobs_recent", "queries-perf"
      Detail string // guard text without the prefix; "" if not captured
  }
  func (e *MemoryGuardError) Error() string   // "<Item> skipped by memory guard: <Detail>" | "<Item> skipped by memory guard (exit 3)"
  func (e *MemoryGuardError) Summary() string // "<Item> (<Detail>)" | "<Item> (memory guard, exit 3)"

  // Returns a guard error when err carries exit code 3. captured wins; otherwise
  // the detail comes from a *RemoteExitError's Stderr; otherwise Detail is "".
  func asMemoryGuardError(item string, err error, captured string) (*MemoryGuardError, bool)
  ```
- Produces (package `kubernetes`): `HostExecuteAndStreamNoKeepalive` returns `*collection.RemoteExitError` for a non-zero marker code. `Stderr` is set to the last stderr line. The message is unchanged.

- [ ] **Step 1: Capture the real guard line as the test fixture**

Run:
```bash
MSYS_NO_PATHCONV=1 docker run --rm --memory=1g --memory-swap=1g \
  -v "C:/Users/chufe/Workspaces/golang/dremio-rocksdb-viewer/catalog/test1:/data/catalog" \
  -v "C:/Users/chufe/Workspaces/golang/dremio-diagnostic-collector/cmd/local/rockscollect/rocksdb:/v:ro" \
  alpine:3.20 /v/dremio-rocksdb-viewer-linux-amd64 -db /data/catalog -type sys.version; echo "exit=$?"
```
Expected:
- stdout is empty;
- stderr is a single line, `error: memory guard: refusing to start: source=cgroup2 used_mib=<n> limit_mib=1024 reserve_mib=1024`;
- the output ends with `exit=3`.

Use that exact line as `guardRefuseLine` in `memguard_test.go`. Also define `guardAbortLine`: the same line with `refusing to start` replaced by `aborting`.

- [ ] **Step 2: Write the failing tests**

`memguard_test.go`:
- `TestRemoteExitError_Message`: `(&RemoteExitError{Code: 3}).Error() == "remote command exited with code 3"`.
- `TestRemoteExitCode` (table):

  | Input | Want |
  |---|---|
  | `&RemoteExitError{Code: 3}` | 3, true |
  | `fmt.Errorf("w: %w", &RemoteExitError{Code: 137})` | 137, true |
  | `fmt.Errorf("w: %w", utilexec.CodeExitError{Err: errors.New("command terminated with exit code 3"), Code: 3})` (`k8s.io/client-go/util/exec`) | 3, true |
  | `cli.UnableToStartErr{Err: <real *exec.ExitError>}` | 3, true |
  | `errors.New("x")` | 0, false |
  | `nil` | 0, false |

  To get a real `*exec.ExitError`, run `exec.Command("cmd.exe", "/c", "exit 3").Run()` on Windows and `exec.Command("sh", "-c", "exit 3").Run()` elsewhere.
- `TestMemoryGuardDetail`:
  - `guardRefuseLine` → the text after the prefix, `true`;
  - `{"a":1}` → `"", false`;
  - `warning: slow` → `"", false`.
- `TestMemoryGuardDetailIn`:
  - `"x\n" + guardRefuseLine + "\n"` → the refuse detail;
  - `"x\ny"` → `""`.
- `TestAsMemoryGuardError` (table, item `sys.jobs_recent`):

  | Case | err | captured | Want |
  |---|---|---|---|
  | captured | `&RemoteExitError{Code: 3}` | refuse detail | `Detail` = refuse detail |
  | from stderr | `&RemoteExitError{Code: 3, Stderr: guardAbortLine}` | `""` | `Detail` = abort detail |
  | no detail | `&RemoteExitError{Code: 3}` | `""` | `Detail == ""`; `Summary() == "sys.jobs_recent (memory guard, exit 3)"`; `Error() == "sys.jobs_recent skipped by memory guard (exit 3)"` |
  | other code | `&RemoteExitError{Code: 137}` | | `nil, false` |
  | nil | `nil` | | `nil, false` |

- `TestMemoryGuardError_Text`, with the refuse detail:
  - `Error() == "sys.jobs_recent skipped by memory guard: " + detail`;
  - `Summary() == "sys.jobs_recent (" + detail + ")"`.

`keepalive_free_test.go` (package `kubernetes`, so it can't see the `collection` test fixtures; use a local literal `abort := "error: memory guard: aborting: source=cgroup2 used_mib=1 limit_mib=2 reserve_mib=1"`):
- `TestHostExecuteAndStreamNoKeepalive_GuardExitCarriesStderr`:
  - a `scriptedExecutor` with stdout `"{\"a\":1}\n\n__DDC_EOS__:3\n"` and stderr `abort + "\n"`;
  - `errors.As(err, &rexit)` with `rexit.Code == 3 && rexit.Stderr == abort`;
  - lines are `[{"a":1}]`.
- The existing `TestEOSLineWriter` case `non-zero exit code` must still pass unchanged.

- [ ] **Step 3: Run them to verify they fail**

Run: `go test -short ./cmd/root/collection/ ./cmd/root/kubernetes/`
Expected: compile errors (undefined `RemoteExitError`, `remoteExitCode`, …).

- [ ] **Step 4: Implement**

- Add the primitives in `memguard.go` and `RemoteExitError` in `collector.go`.
- In `keepalive_free.go`:
  - `finish()` returns `&collection.RemoteExitError{Code: w.rc}`;
  - the stderr `K8SWriter` callback keeps logging at WARN and also stores the last line;
  - after `finish()`, use `errors.As` to set `Stderr` on the returned `*RemoteExitError`.

- [ ] **Step 5: Run the tests**

Run: `go test -short ./cmd/root/collection/ ./cmd/root/kubernetes/`
Expected: PASS.

---

### Task 4: `sys.jobs_recent` time window

**Files:**
- Modify: `cmd/root/collection/rockscollect.go`:
  - add `buildJobsRecentFilterArgs` next to `buildQueriesPerfFilterArgs`;
  - add an `extraArgs` parameter to `collectRocksType`;
  - update the three call sites.
- Test: `cmd/root/collection/rockscollect_test.go`

**Interfaces:**
- Produces:
  ```go
  func buildJobsRecentFilterArgs(args RocksCollectArgs) string
  // extraArgs is appended verbatim to "<viewer> -db <path> -type <dataType>"; "" for none.
  func collectRocksType(c Collector, cs CopyStrategy, host, nodeType, dbPath, dataType, extraArgs, strategyType, filename string) (*helpers.CollectedFile, error)
  ```
- The system-tables loop passes `buildJobsRecentFilterArgs(args)` for `table == "jobs_recent"` and `""` for every other table. `cluster_stats` and the WLM types pass `""`.
- It logs `rocksdb-viewer sys.jobs_recent filter: QueriesPerfDays=%d Days=%d StartDate=%q filterArgs=%q` at INFO.

- [ ] **Step 1: Write the failing tests**

- `TestBuildJobsRecentFilterArgs` (table, `RocksCollectArgs` → want):

  | Input | Want |
  |---|---|
  | `{QueriesPerfDays: 30}` | `" -days 7"` |
  | `{QueriesPerfDays: 7}` | `" -days 7"` |
  | `{QueriesPerfDays: 3}` | `" -days 3"` |
  | `{QueriesPerfDays: 0}` | `" -days 7"` |
  | `{Days: 3}` | `" -days 3"` |
  | `{Days: 30, QueriesPerfDays: 30}` | `" -days 30"` |
  | `{StartDate: "2026-10-01", Days: 5}` | `" -start-date 2026-10-01 -days 5"` |

- `TestRunRocksDBCollection_JobsRecentWindow`:
  - setup: `CollectSystemTables: true`, `SystemTables: []string{"jobs_recent", "views"}`, `QueriesPerfDays: 30`;
  - the mock records commands and answers each `-type` with `{"a":1}`;
  - assert the `-type sys.jobs_recent` command ends with `-type sys.jobs_recent -days 7`;
  - assert the `-type sys.views` command contains no `-days`.

- [ ] **Step 2: Run them to verify they fail**

Run: `go test -short -run 'TestBuildJobsRecentFilterArgs|TestRunRocksDBCollection_JobsRecentWindow' ./cmd/root/collection/`
Expected: compile error (undefined `buildJobsRecentFilterArgs`).

- [ ] **Step 3: Implement**

Write the function and the `extraArgs` wiring as described in the Interfaces block. The command string becomes `fmt.Sprintf("%s -db %s -type %s%s", rocksdbViewerRemotePath, dbPath, dataType, extraArgs)`.

- [ ] **Step 4: Run the package tests**

Run: `go test -short ./cmd/root/collection/`
Expected: PASS.

---

### Task 5: Stream viewer types to file, choose the executor per type, record guard skips

**Files:**
- Modify: `cmd/root/collection/rockscollect.go`:
  - add `runViewerStream`;
  - rewrite the `collectRocksType` body;
  - `RunRocksDBCollection` gains a 4th return value and records guard skips for cluster_stats, sys.* and WLM.
- Modify: `cmd/root/collection/streaming_collect.go:1076`. Adapt to the 4 return values with `rocksFiles, inc, _, err :=`; Task 7 wires the third value.
- Modify (test mocks):
  - `cmd/root/collection/streaming_collect_test.go`: `mockStreamCollector.HostExecuteAndStream`;
  - `cmd/root/collection/rockscollect_test.go`: `queriesPerfCollector`, `keepaliveFreeCollector`, and the existing `RunRocksDBCollection` call sites.
- Modify (docs):
  - `docs/architecture/decisions.md`, the D077 row;
  - `CLAUDE.md`, the `cmd/root/kubernetes/` line.
- Test: `cmd/root/collection/rockscollect_test.go`, `cmd/root/collection/collector_test.go`

**Interfaces:**
- Consumes: `remoteExitCode`, `memoryGuardDetail`, `asMemoryGuardError`, `*MemoryGuardError` (Task 3); `collectRocksType` with `extraArgs` (Task 4).
- Produces:
  ```go
  // wlm_cluster_usage → c.HostExecuteAndStream(false, host, out, "", cmd);
  // otherwise KeepaliveFreeStreamer when implemented, else HostExecuteAndStream.
  func runViewerStream(c Collector, host, dataType, cmd string, out cli.OutputHandler) error

  func RunRocksDBCollection(args RocksCollectArgs) ([]helpers.CollectedFile, []*IncompleteCollectionError, []*MemoryGuardError, error)
  ```
- `collectRocksType` behaviour:
  - It streams lines into `<destDir>/<filename>.partial`, writing each line followed by `\n`.
  - Lines for which `memoryGuardDetail` returns `ok` are **not** written; their detail is kept as `captured`.
  - **On success:** if no non-blank line was written, delete the temp file, log the existing INFO "returned empty output", and return `nil, nil`. Otherwise flush, close, rename to `<filename>`, and keep the existing `rocksdb-viewer: collected …` INFO line. `Size` is the number of bytes written.
  - **On any error:** close and delete the temp file. If `asMemoryGuardError(dataType, err, captured)` returns ok, return that `*MemoryGuardError`. Otherwise return `fmt.Errorf("execute rocksdb-viewer -type %s: %w", dataType, err)`.
- `RunRocksDBCollection`: when `errors.As(err, &mg)` matches on any `collectRocksType` call:
  - append `mg` to the guard skips;
  - log `simplelog.Errorf("rocksdb %s on %s: %v", dataType, host, mg)`;
  - continue with the next type.

  Other errors keep today's ERROR log.
- Mock changes:
  - `mockStreamCollector.HostExecuteAndStream`: when `hostExecuteFunc` is set, call it, send each line of the result (none if the result is `""`), and return its error.
  - `queriesPerfCollector.HostExecuteAndStream` and `keepaliveFreeCollector.HostExecuteAndStreamNoKeepalive`: keep today's behaviour only when the joined args contain `-type queries_perf`. Otherwise delegate to `mockStreamCollector.HostExecuteAndStream`.
  - Both record the commands they served (`streamCmds`, `noKeepaliveCmds []string`) for routing assertions.

- [ ] **Step 1: Write the failing tests**

All `collectRocksType` tests use item and file `sys.views` / `sys.views.json`, unless stated otherwise.

- `TestCollectRocksType_StreamsExactBytes`:
  - lines `{"a":1}` and `{"a":2}`;
  - the file content is `"{\"a\":1}\n{\"a\":2}\n"`, `Size == 16`, and no `*.partial` file remains.
- `TestCollectRocksType_EmptyOutputNoFile`: no lines → `nil, nil`, and the directory has no files.
- `TestCollectRocksType_ErrorLeavesNoFile`:
  - lines, then `errors.New("connection reset")`;
  - `err != nil`, `errors.As(err, &mg)` is false, and the directory has no files.
- `TestCollectRocksType_Exit137IsPlainError`: `&RemoteExitError{Code: 137}` → `err != nil`, not a `*MemoryGuardError`, and no files.
- `TestCollectRocksType_GuardOnMergedOutput`:
  - plain collector, item `sys.jobs_recent`;
  - lines `{"a":1}`, then `guardRefuseLine`, then `&RemoteExitError{Code: 3}`;
  - `mg.Item == "sys.jobs_recent"`, `mg.Detail` is the refuse detail, and no files.
- `TestCollectRocksType_GuardOnKeepaliveFree`:
  - keepalive-free collector returns `&RemoteExitError{Code: 3, Stderr: guardAbortLine}`;
  - `mg.Detail` is the abort detail.
- `TestRunViewerStream_ExecutorPerType`:
  - with a keepalive-capable mock, `wlm_cluster_usage` is served by `HostExecuteAndStream`, while `sys.views`, `cluster_stats` and `wlm_queues` are served by `HostExecuteAndStreamNoKeepalive`;
  - with a plain mock, every type uses `HostExecuteAndStream`.
- `TestRunRocksDBCollection_GuardSkipsContinue`:
  - `SystemTables: {"jobs_recent", "views", "tables"}`;
  - `jobs_recent` and `views` answer `guardRefuseLine` with `&RemoteExitError{Code: 3}`; `tables` answers `{"t":1}`;
  - assert:
    - `err == nil`;
    - the guard-skip items are `["sys.jobs_recent", "sys.views"]`;
    - the collected files include `sys.tables.json`;
    - `rm -f /tmp/dremio-rocksdb-viewer` was issued.
- `TestFindClusterID_ParsesStreamedClusterStats`:
  - `collectRocksType` streams `{"dremioVersion":"26.1.7","clusterID":"abc","nodeName":"m0"}` as `cluster-stats.json`;
  - `FindClusterID(<copy-strategy dir>)` returns one entry with `ClusterID == "abc"`.
- Update the existing tests:
  - `TestWLMFileLayout`: every `wantContent` value gains a trailing `"\n"`;
  - all `RunRocksDBCollection` call sites take 4 return values.

- [ ] **Step 2: Run them to verify they fail**

Run: `go test -short ./cmd/root/collection/`
Expected: compile errors (`runViewerStream` undefined, wrong number of return values). After stubbing, the new tests fail.

- [ ] **Step 3: Implement**

Implement `runViewerStream`, the new `collectRocksType` body, the guard-skip handling in `RunRocksDBCollection` and the mock changes, all as specified in the Interfaces block. Then update the docs:
- `CLAUDE.md`: the K8s line becomes `(SPDY exec; file streams, the queries-perf stream and rocksdb-viewer exports except wlm_cluster_usage run without keepalive pings, see D077)`.
- D077: add that the viewer `-type` exports (except `wlm_cluster_usage`, which can be silent for hours) also use the keepalive-free executor with the marker, and that a missing marker or a non-zero exit discards that export.

- [ ] **Step 4: Run the package tests**

Run: `go test -short ./cmd/root/collection/`
Expected: PASS, including every existing #339 test.

---

### Task 6: queries_perf exit-3 handling

**Files:**
- Modify: `cmd/root/collection/rockscollect.go` (`collectQueriesPerf`, plus the queries_perf `switch` in `RunRocksDBCollection`)
- Test: `cmd/root/collection/rockscollect_test.go`

**Interfaces:**
- Consumes: `asMemoryGuardError`, `memoryGuardDetail`, `memoryGuardDetailIn`, `*MemoryGuardError` (Task 3); the 4-value `RunRocksDBCollection` (Task 5).
- Behaviour:
  - **`-count` pre-flight:** if `asMemoryGuardError("queries-perf", err, memoryGuardDetailIn(countOut))` returns ok, return `nil, mg` before creating the destination directory. The data stream is not started. Non-guard count errors keep today's WARN-and-continue.
  - **Stream handler:** lines for which `memoryGuardDetail` returns ok are not written; their detail is kept as `captured`.
  - **After the stream:** if `asMemoryGuardError("queries-perf", streamErr, captured)` returns ok:
    - call `dw.Close()`;
    - `os.Remove` every `queries-perf.<date>.json` in `dw.dates`;
    - return `nil, mg`.

    This check runs **before** the existing #339 handling, which is otherwise unchanged.
  - **`RunRocksDBCollection`:** a new first `switch` case, `errors.As(err, &mg)`, appends to the guard skips and logs `simplelog.Errorf("rocksdb queries_perf on %s: %v", host, mg)`.

- [ ] **Step 1: Write the failing tests**

- `TestCollectQueriesPerf_CountGuardSkipsStream`:
  - `-count` returns `guardRefuseLine` with `&RemoteExitError{Code: 3}`;
  - the result is `files == nil` and a `*MemoryGuardError` with `Item == "queries-perf"` and the refuse detail;
  - `noKeepaliveCalls == 0 && calls == 0`.
- `TestCollectQueriesPerf_CountFailureNonGuardStillStreams`:
  - `-count` returns `errors.New("boom")`, and the stream sends 2 perf lines;
  - `err == nil`, 1 file, `noKeepaliveCalls == 1`.
- `TestCollectQueriesPerf_StreamGuardDiscardsFiles`:
  - keepalive-free lines `perfLine(1)` and `perfLine(2)`, then `&RemoteExitError{Code: 3, Stderr: guardAbortLine}`;
  - `files == nil`, the error is a `*MemoryGuardError` with the abort detail and **not** an `*IncompleteCollectionError`;
  - `<dir>/queries-perf/dremio-master-0/` holds no `*.json`.
- `TestCollectQueriesPerf_FallbackGuardLineNotWritten`:
  - fallback collector with lines `perfLine(1)` and `guardAbortLine`, then `&RemoteExitError{Code: 3}`;
  - no `queries-perf.unknown.json`, no `*.json` at all, and the guard error has the abort detail.
- `TestRunRocksDBCollection_QueriesPerfGuardSkip`:
  - use the setup of `TestRunRocksDBCollection_ReportsIncompleteQueriesPerf`, but the stream returns `&RemoteExitError{Code: 3, Stderr: guardAbortLine}`;
  - the third return value has one item `queries-perf`, the incomplete list is empty, and `files` holds only `cluster-stats.json`.

- [ ] **Step 2: Run them to verify they fail**

Run: `go test -short -run 'TestCollectQueriesPerf_|TestRunRocksDBCollection_QueriesPerfGuardSkip' ./cmd/root/collection/`
Expected: the new tests FAIL (today partial files are kept as INCOMPLETE); the existing #339 tests PASS.

- [ ] **Step 3: Implement** the behaviour described in the Interfaces block.

- [ ] **Step 4: Run the package tests**

Run: `go test -short ./cmd/root/collection/`
Expected: PASS.

---

### Task 7: Reporting — `summary.json`, summary log, node status

**Files:**
- Modify: `cmd/root/collection/summary.go` (`SummaryInfo`)
- Modify: `cmd/root/collection/collector.go` (`logDistributedCollectionSummary`)
- Modify: `cmd/root/collection/streaming_collect.go`:
  - `nodeDoneStatus`;
  - the RocksDB call site at l.1076;
  - the per-node aggregation at l.~1098;
  - the summary build at l.~1216;
  - the `logDistributedCollectionSummary` call at l.~1237.
- Test: `cmd/root/collection/collector_test.go`

**Interfaces:**
- Consumes: the third return value of `RunRocksDBCollection` and `(*MemoryGuardError).Summary()` (Tasks 3, 5).
- Produces:
  ```go
  // SummaryInfo, after IncompleteCollections:
  MemoryGuardSkips []string `json:"memoryGuardSkips,omitempty"`

  func nodeDoneStatus(collected int, bytes int64, skipped []string, incomplete []*IncompleteCollectionError, guardSkips []*MemoryGuardError) string
  func logDistributedCollectionSummary(collectionMode collects.CollectionMode, coordinators, executors []string, files []helpers.CollectedFile, totalFailedFiles, totalFailedNodes, totalSkippedFiles, incomplete, guardSkips []string, nodesConnectedTo int, duration time.Duration)
  ```
- Behaviour:
  - Each node's guard skips become `fmt.Sprintf("%s: %s", host, g.Summary())` in `totalGuardSkips`, guarded by the existing mutex. `summaryInfo.MemoryGuardSkips = totalGuardSkips`.
  - The status line appends `, MEMORY GUARD: ` + the items joined by `, `, after any INCOMPLETE part.
  - The summary log prints `  Memory Guard Skips: %d` right after `  Incomplete Collections: %d`, only when N > 0. After the INCOMPLETE block it prints `simplelog.Warning("MEMORY GUARD SKIPS:")` and then `simplelog.Warningf("  - %s", item)` for each item.

- [ ] **Step 1: Write the failing tests**

- `TestNodeDoneStatus`: existing cases pass `nil` for guard skips. New cases, with `g1 = &MemoryGuardError{Item: "sys.jobs_recent"}` and `g2 = &MemoryGuardError{Item: "sys.views"}`:
  - `guard`: `[g1]` → `"Done: 3 files (1.0KB), 0 skipped, MEMORY GUARD: sys.jobs_recent"`;
  - `incomplete and guard`: `inc` plus `[g1, g2]` → `"Done: 3 files (1.0KB), 0 skipped, INCOMPLETE: queries-perf (187442 records), MEMORY GUARD: sys.jobs_recent, sys.views"`.
- `TestSummaryInfo_MemoryGuardSkipsOmittedWhenEmpty`:
  - `SummaryInfo{}.String()` does not contain `memoryGuardSkips`;
  - with `MemoryGuardSkips: []string{"dremio-master-0: sys.jobs_recent (memory guard, exit 3)"}`, it contains `"memoryGuardSkips"`.
- `TestLogDistributedCollectionSummary_GuardSection`:
  - call `simplelog.InitLoggerWithOutputDir(t.TempDir())`, with `t.Cleanup(func() { _ = simplelog.Close(); simplelog.InitLogger() })`. Close the log first, because Windows can't delete the temp dir while `ddc.log` is open;
  - call the function with one incomplete item and guard skips `[]string{"m0: sys.jobs_recent (x)"}`;
  - read `ddc.log` and assert it contains:
    - `Incomplete Collections: 1`;
    - `Memory Guard Skips: 1`;
    - `MEMORY GUARD SKIPS:`;
    - `  - m0: sys.jobs_recent (x)`;
    - `INCOMPLETE COLLECTIONS:`.
  - A second call with no guard skips writes no `Memory Guard Skips` line.

- [ ] **Step 2: Run them to verify they fail**

Run: `go test -short -run 'TestNodeDoneStatus|TestSummaryInfo_|TestLogDistributedCollectionSummary_' ./cmd/root/collection/`
Expected: compile errors (wrong number of arguments).

- [ ] **Step 3: Implement** the signatures and wiring described in the Interfaces block.

- [ ] **Step 4: Run the full suite and build**

Run: `SKIP_INTEGRATION_SETUP=true go test -short ./... && go build -o bin/ddc.exe .`, plus `gofmt -l` on the changed files.
Expected: no formatting changes, every package PASSES, and the build succeeds.

---

### Task 8: Local verification — lint and Docker against the catalogs

**Files:** none in the repo. Outputs and scripts go in `$SCRATCH`.

- [ ] **Step 1: Lint**

Run: `golangci-lint run`
Expected: `0 issues.`

- [ ] **Step 2: Identity matrix on `test1` with DDC's exact command strings**

Do this once for each binary: `viewer-1.0.0-amd64` (from `$SCRATCH`) and the vendored 1.1.0.

Run each type below with `alpine:3.20`:
- mount `catalog/test1` at `/data/catalog` (read-write; RocksDB writes its `LOG` file);
- mount the binary's folder at `/v:ro`;
- send stdout to `$SCRATCH/id/<ver>/<type>.out`.

Types:
- `cluster_stats`;
- `sys.version`, `sys.options`, `sys.roles`, `sys.membership`, `sys.privileges`, `sys.reflections`, `sys.materializations`, `sys.refreshes`, `sys.reflection_dependencies`, `sys.tables`, `sys.views`;
- `sys.jobs_recent` (no filter);
- `wlm_queues`, `wlm_rules`, `wlm_engines`, `wlm_cluster_usage`;
- `queries_perf -count -days 30`, `queries_perf -days 30`.

Then run `cmp` on each pair.
Expected: all 19 pairs are identical.

- [ ] **Step 3: jobs_recent window equivalence on `cataloghc`**

- Run 1.1.0 `-type sys.jobs_recent` without a filter → `all.out`.
- Pick `D` = the UTC date of the median `submitted_epoch_millis` in `all.out`, minus 5 days, with `N=10`.
- Run 1.1.0 with DDC's diagnosis-form args `-type sys.jobs_recent -start-date D -days 10` → `win.out`.
- Run 1.1.0 with DDC's standard-form args `-type sys.jobs_recent -days 7` → `std.out`.

With `python -I`, check:
- `win.out` equals the lines of `all.out` whose `submitted_epoch_millis` lies in `[D 00:00 UTC, D+10 00:00 UTC]` (inclusive), in the same order;
- every row in `std.out` has `submitted_epoch_millis >= now − 7×86400000 − 60000`.

Expected:
- the window check passes with a non-empty `win.out`;
- `std.out` passes (it may be empty, because the catalog data is old).

- [ ] **Step 4: Exit 3 end to end through the real binary**

Run the Task 3 Step 1 Docker command again with `-type sys.jobs_recent`.
Expected: exit 3, empty stdout, and one guard line on stderr that matches the `guardRefuseLine` format used in the tests.

---

### Task 9: Live collection on `healthcheck-aks`

**Files:** none in the repo. The bundle is `$SCRATCH/live/diag-live.tgz`.

- [ ] **Step 1: Record the master's restart count**

Run: `kubectl --context healthcheck-aks -n default get pod dremio-master-0 -o jsonpath='{.status.containerStatuses[0].restartCount}'`
Expected: a number. Note it as `R0`.

- [ ] **Step 2: Collect**

Run:
```bash
./bin/ddc.exe collect k8s standard --context healthcheck-aks --namespace default \
  --system-tables=version,options,roles,membership,privileges,reflections,materializations,refreshes,reflection_dependencies,tables,views,jobs_recent \
  --collect-wlm=true --collect-queries-perf-json=true --queries-perf-num-days=30 \
  --output-file "$SCRATCH/live/diag-live.tgz"
```
Expected: exit 0, and the tarball is written.

- [ ] **Step 3: Check the bundle**

Extract the bundle to `$SCRATCH/live/x/` and check `ddc.log`:
- it contains `rocksdb-viewer on dremio-master-0: dremio-rocksdb-viewer v1.1.0`;
- it contains `-type sys.jobs_recent -days 7`;
- it does not contain `memory guard`;
- it has no `dremio-master-0 stderr:` WARN lines;
- it does not contain `Memory Guard Skips` or `Incomplete Collections: [1-9]`.

Check the files:
- `summary.json` has neither `memoryGuardSkips` nor `incompleteCollections`;
- with `python -I`, every file under `system-tables/`, `wlm/`, `cluster-stats/` and `queries-perf/` is non-empty and parses line by line as JSON;
- every `sys.jobs_recent.json` row has `submitted_epoch_millis` within 7 days (plus 1 minute of slack) before the collection start shown in `summary.json` `startTimeUTC`.

Expected: all checks pass.

- [ ] **Step 4: Confirm the pod did not restart**

Run the Step 1 command again.
Expected: the count equals `R0`.

- [ ] **Step 5: Hand back for review**

Report the results of Tasks 8 and 9 to the user, with the diff summary (`git status`, `git diff --stat`). Wait for the user's review. Then follow `superpowers:finishing-a-development-branch`: one squashed commit on `feat/rocksdb-viewer-1.1.0`, including the spec and this plan. Push or open a PR only if the user chooses that.
