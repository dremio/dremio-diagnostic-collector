# Report failed RocksDB exports, drop the queries-perf pre-flight count, keep the collector awake

Date: 2026-11-12
Inputs: analysis of a customer bundle from a large Kubernetes cluster, collected with DDC v4.0.7
(`collect k8s standard`), whose queries-perf data was missing although DDC reported success; the
bundle's `ddc.log` and `summary.json`; the health-check import job history for that bundle; the
user's decisions of 2026-11-12 (go with the recommendations; keep the machine awake with an
existing Go library; upgrade to Go 1.27 for it; drop macOS 11 and 12). DDC state checked at `1fd36db`.

## Intent

A customer collection ran for over two hours and reported `Success Rate: 100.0%`. Its
`summary.json` had no failed, skipped, incomplete or memory-guard entries. Its
`queries-perf/dremio-master-0/` folder was empty, so every queries-perf model failed in the
health-check import and the problem was only discovered there.

What `ddc.log` shows (times relative to the start of the queries-perf step):

1. `T+0` DDC starts the pre-flight `dremio-rocksdb-viewer -type queries_perf -count` on the
   master. `--queries-perf-num-days 0` was passed by hand, which means no day filter, so the
   viewer scans the whole job history.
2. `T+15 min` every other collection is finished. Nothing else is logged for 1h40m; DDC is
   waiting only for the count.
3. `T+1 h 55 min` the count returns with no error and no number. DDC logs no warning and uses 0.
4. `T+1 h 55 min` the data export exec fails before it starts:
   `dial tcp: lookup <api-server>: no such host`. The collecting machine, a Mac, had lost its
   network or VPN. Whether it slept or the VPN dropped on its own can't be told from the log.
5. `collectQueriesPerf` returns a plain error because zero records were streamed.
   `RunRocksDBCollection` only logs it (`rockscollect.go:308-309`), so it reaches no report.

The network loss was the trigger. DDC turned it into a silent loss for three reasons, made a
long run more likely to be interrupted for a fourth, and has one misleading flag:

- **Failed viewer exports are not reported.** Partial queries-perf exports (`incompleteCollections`)
  and memory-guard stops (`memoryGuardSkips`) are reported. Any other viewer failure is only a
  ddc.log ERROR line: queries-perf with zero records, a `cluster_stats` / `sys.*` / `wlm_*`
  export error, or a failed viewer upload. The 2026-10-21 viewer-1.1.0 spec deliberately left
  this alone ("non-exit-3 viewer failures ... remain ddc.log ERROR lines"; "rocksdb items are
  not part of [the success rate]"). This spec reverses both of those non-goals.
- **The pre-flight `-count` hides its own failure and costs a full extra scan.** It goes through
  the pinged `HostExecute` path, which has no end-of-stream marker. That is the path D077
  describes, where client-go can report success after losing the output. Its parse error is
  also discarded: `totalRecords, _ := strconv.Atoi(...)`. It only drives a progress percentage,
  but it scans the catalog once more before the data export starts. With a 30-day filter that
  took 8.5 minutes on another catalog (about 100k records in the window); unfiltered it ran
  for at least 1h55m here.
- **`--queries-perf-num-days 0` means "no limit".** The help text does not say so, and neither
  does the README. In the TUI, 0 means "Skip". The TUI is internally consistent because it
  emits `--collect-queries-perf-json=false`, but someone typing the CLI by hand cannot tell.
  The same holds for all five standard `*-num-days` flags.
- **Nothing keeps the collecting machine awake.** Collections run for hours and are mostly
  silent on the console. A laptop that idle-sleeps drops its VPN and its connections to the
  cluster, and the collection fails.
- **`--collector-timeout` does nothing.** It is declared (`cmd/root.go:1364`) and documented
  (`README.md:290`) but never read. The same command passed `--collector-timeout 5m`.

## Goals

1. Every viewer export that fails or produces nothing because of an error is reported, in the
   same three places where incomplete exports and memory-guard skips already appear: the node
   status line, `summary.json`, and the ddc.log collection summary. Failed exports and
   memory-guard skips lower the success rate, and the printed rate can no longer read 100.0%
   when anything failed, was skipped by the memory guard, or was skipped as a file.
2. queries-perf no longer runs a pre-flight `-count`.
3. The CLI help text and README say what 0 means for the standard `*-num-days` flags. ddc.log
   warns when queries-perf runs with no day limit.
4. `--collector-timeout` is marked deprecated (hidden, warns when used) and removed from the
   README, so it no longer pretends to bound anything.
5. While a collection runs, DDC asks macOS and Windows not to idle-sleep, using the existing
   library `github.com/crgimenes/native/power`. DDC only calls its API.
6. DDC builds with Go 1.27.4, which that library requires, and its runtime behaviour stays
   the same as with Go 1.24. The one accepted change: on macOS, DDC now needs macOS 13 Ventura
   or newer (user decision; see section 7).

## Non-goals

- Changing the exit code. Incomplete exports and memory-guard skips don't change it today,
  and failed exports won't either.
- Retrying viewer exports. With the network gone a retry fails too, and repeating a multi-hour
  scan is not worth it.
- Changing what 0 means for any `*-num-days` flag, or changing the TUI.
- Implementing a real `--collector-timeout`.
- The master-detection probe (`test -f <catalog>/CURRENT`, `rockscollect.go:185`) treats a
  transport error the same as "no catalog" and silently skips all viewer exports. This is
  known and left as is: it's one short command at the start of the node's RocksDB step, and
  telling the two cases apart needs a separate change.
- A console warning at startup. The status screen erases the terminal when it exits
  (`consoleprint.ExitStatusScreen`), and DDC has no pattern for warnings that stay visible.
  The node status line and ddc.log are where people look.
- Removing empty `queries-perf/<host>/` folders after a failed export. The import fails on
  missing data either way; the fix is that DDC now says the data is missing.
- Keep-awake on Linux. The library has no Linux backend, a D-Bus inhibitor is often refused by
  polkit over SSH, and DDC on Linux mostly runs on servers that don't sleep.
- Preventing sleep that the user causes: closing the lid on battery, choosing Sleep, or the
  Windows Modern Standby limit on battery. Microsoft documents that on battery these requests
  end 5 minutes after the sleep timeout expires. No API a CLI can use without admin rights
  prevents these. VPN idle or session timeouts are not prevented either.
- A flag to turn keep-awake off. It only blocks idle sleep while a collection runs.
- Adopting the runtime defaults that changed in Go 1.25-1.27 (see section 7).
- Supporting macOS 11 Big Sur and 12 Monterey. Go 1.27 binaries require macOS 13. The user
  accepted this on 2026-11-12: both versions are 5-6 years old and out of Apple's support.
- A macOS CI job. The pre-release Mac check is manual (see "Release prerequisites").
- Upgrading any other dependency. `k8s.io/kubelet` stays pinned at v0.32.3.
- A third-party notice for Go dependencies. `NOTICE-bin.txt` covers only the Java libraries,
  and no Go dependency is listed today.
- Release work: version bump, `CHANGELOG`, tag, and the Go version on the release build
  machines. The user does this (precedent: 4.0.7); see "Release prerequisites".

## Design

### 1. Report failed viewer exports (`cmd/root/collection/`)

**New error type** in `rockscollect.go`, next to `IncompleteCollectionError`:

```go
// FailedCollectionError reports a viewer export that produced no usable data
// because of an error (not a memory-guard stop, not a partial export).
type FailedCollectionError struct {
	Item  string // "queries-perf", "cluster_stats", "sys.options", "wlm_queues", "rocksdb-viewer"
	Cause error
}

func (e *FailedCollectionError) Error() string { return fmt.Sprintf("%s failed (%v)", e.Item, e.Cause) }
func (e *FailedCollectionError) Unwrap() error  { return e.Cause }
```

**What counts as failed**

| Where | Today | New |
|---|---|---|
| `recordRocksType`, `case err != nil` (cluster_stats, every `sys.<table>`, every `wlm_*` incl. `wlm_cluster_usage`) | ERROR log only | ERROR log **and** a `FailedCollectionError{Item: dataType}` |
| queries-perf, `case err != nil` in `RunRocksDBCollection` (zero records streamed, write error, create-path error) | ERROR log only | ERROR log **and** `FailedCollectionError{Item: "queries-perf"}` |
| `RunRocksDBCollection` returns an error before any export (uname, binary lookup, temp dir, local write, upload, chmod); the caller in `streaming_collect.go` (~line 1085) logs `RocksDB collection failed on ...` | ERROR log only | ERROR log **and** `FailedCollectionError{Item: "rocksdb-viewer"}` for that node |

These are not failures and stay as they are: a memory-guard stop (`memoryGuardSkips`), a
partial queries-perf export (`incompleteCollections`), a node without a catalog (scale-out
coordinator), an export the user disabled, and an export whose data store doesn't exist in
the catalog (below).

**Store not in this catalog (option b, decided).** The viewer exits 1 with the stderr line
`error: '<cf>' column family not found` when a WLM export's store is missing: `wlmqueue` for
`wlm_queues`, `rulesmanager` for `wlm_rules`, `provisioning` for `wlm_engines`
(`dremio-rocksdb-viewer/cmd/dremio-reader/wlm.go:138-141, 219-222, 389-392`). OSS catalogs
probably lack the WLM stores, and DDC's integration cluster runs `dremio/dremio-oss:25.1`.
Reporting this as FAILED would flag every OSS run, so it is treated as "not available" instead:
- **Detection** mirrors the memory guard in `collectRocksType`. On merged-output transports
  (SSH, kubectl, local, and the K8s pinged path), the line arrives in the handler. It is matched
  there, kept out of the export file, and remembered, the same way `memoryGuardDetail` handles
  the guard line. On the K8s keepalive-free path, it is the last stderr line in
  `RemoteExitError.Stderr` (`keepalive_free.go:113`). Match only the exact viewer form
  `error: '<name>' column family not found`.
- **Outcome**, only when the export failed and that line was seen: no file, no FAILED entry, no
  effect on the success rate, and a ddc.log INFO line:
  `rocksdb-viewer -type <type> on <host>: not available in this catalog (<cf> column family not found)`.
  Return it as its own sentinel or typed error so `recordRocksType` can tell it apart; the
  shape is the builder's call.
- This applies to every viewer `-type` export, not only WLM. `sys.*` and `cluster_stats` never
  print the line today because they return empty output for a missing store.

`Item` uses the same names already used for guard skips and in ddc.log: the viewer `-type`
value (`sys.options`, `wlm_queues`, `cluster_stats`), `queries-perf` for the queries-perf export,
and `rocksdb-viewer` when the whole step failed.

**Plumbing.** `RunRocksDBCollection` also returns the failed items. `streaming_collect.go` keeps a
per-node `nodeFailed` list next to `nodeIncomplete` / `nodeGuardSkips` and a run-wide
`totalFailedCollections []string` next to `totalIncomplete`, with entries formatted
`"<host>: <FailedCollectionError.Error()>"`, the same shape as `totalIncomplete`.

**Reporting surfaces**

1. **`summary.json`**: a new field next to the two existing ones:
   ```go
   FailedCollections []string `json:"failedCollections,omitempty"`
   ```
   Example entry:
   `"dremio-master-0: queries-perf failed (execute rocksdb-viewer queries_perf: error sending request: ...: no such host)"`.
   `failedFiles` stays untouched. Streaming mode never fills it, and changing it is out of scope.
2. **Node status line** (`nodeDoneStatus`): appended after the existing parts, in this order:
   skipped, `INCOMPLETE:`, `MEMORY GUARD:`, then `, FAILED: <item>, <item>`.
   For example: `Done: 3 files (1.0KB), 0 skipped, FAILED: queries-perf`.
   Existing outputs don't change when nothing failed.
3. **ddc.log collection summary** (`logDistributedCollectionSummary`):
   - `Failed Collections: N` includes the failed exports.
   - The existing `FAILED COLLECTIONS:` section is printed when there are failed exports, and
     lists each as `   <host>: <item> failed (<cause>)`, in the same style as the existing lines.
   - **Success rate (my call):** every failed export and every memory-guard skip counts as one
     failed attempt, since nothing of it is kept. An incomplete export does not lower the rate:
     its records are kept and its files already count as collected, and it stays flagged as
     INCOMPLETE. `successRate(collected, failed, skipped)` keeps its signature;
     `failed` = failed nodes + failed exports + memory-guard skips.
   - **Printed rate is truncated, not rounded**, to one decimal (`math.Floor(rate*10)/10`), so
     anything below 100% prints below 100.0%. Example: 1999/2000 prints `99.9% (1999/2000)`;
     today `%.1f` would print `100.0%`. `successRate` itself and `TestSuccessRate` don't change.

### 2. Remove the queries-perf pre-flight count (`collectQueriesPerf`)

- Delete the `-count` `HostExecute` call, its memory-guard check, the `strconv.Atoi` parse and
  `totalRecords`.
- Progress uses the existing branches for an unknown total:
  `Collecting queries-perf data` at the start, then `Collecting queries-perf data (N queries)`,
  then `Collected queries-perf data (N queries)` at the end when N > 0. The percentage and the
  `Finalizing queries-perf data` message go away.
- ddc.log lines drop the total: periodic `rocksdb-viewer queries_perf streaming on <host>: N records`
  and final `rocksdb-viewer queries_perf on <host>: streamed N records`.
- Nothing else changes. The data export already handles memory-guard stops, the end-of-stream
  marker (D077) and partial exports, and none of that depends on the count.
- What is lost: the progress percentage, and the total that appeared in ddc.log next to a
  truncated export (`pre-flight count N`). `IncompleteCollectionError` never showed the
  total, so `summary.json` and the node status line don't change.
- This applies to every transport; `collectQueriesPerf` is shared.

### 3. Say what 0 means for `*-num-days` (`cmd/root.go`, `README.md`, `collectQueriesPerf`)

Behaviour doesn't change.

- Help text (`cmd/root.go:1387`, `1409-1412`): append `(0 = no limit)` to
  `--queries-json-num-days`, `--server-logs-num-days`, `--tracker-json-num-days` and
  `--vacuum-log-num-days`. For `--queries-perf-num-days`, append
  `(0 = no limit: exports the full job history, which can take hours on large catalogs)`.
- `README.md` rows 257-261: the same wording in the description column.
- ddc.log: in `collectQueriesPerf`, when the resolved filter is empty (no `-days`), log a WARN:
  `rocksdb-viewer queries_perf on <host>: no day limit (--queries-perf-num-days 0); exporting the full job history, which can take hours on large catalogs`.

### 4. Deprecate `--collector-timeout` (`cmd/root.go`, `README.md`)

- After the flag is defined (`cmd/root.go:1364`), call
  `CollectCmd.PersistentFlags().MarkDeprecated("collector-timeout", "it has no effect and will be removed in a future release")`,
  handling the returned error the same way as the existing `MarkHidden("pid")` call.
- Effect, checked in cobra v1.9.1 / pflag v1.0.6: the flag is still accepted, so existing command
  lines keep working; it is hidden from `--help`; and when it is used cobra prints
  `Flag --collector-timeout has been deprecated, it has no effect and will be removed in a future release`
  to stderr during flag parsing. On a terminal, the status screen may cover this line; that is
  accepted (see non-goals).
- Remove the `--collector-timeout` row (`README.md:290`).
- Leave historical specs that mention the flag as they are.

### 5. Architecture record

Add rows to `docs/architecture/decisions.md`:
- in the same table as D077: failed viewer exports are reported (summary/node status/ddc.log,
  counted in the success rate), and queries-perf has no pre-flight count. The reason is this
  incident: silent loss, plus a pinged, marker-less exec with no time limit;
- in the same table: a viewer export whose store is missing from the catalog is "not available",
  not failed;
- one row for keep-awake (section 6) and the Go 1.27 upgrade with `godebug default=go1.24`,
  including the new macOS 13 minimum (section 7).

### 6. Keep the collecting machine awake (`cmd/root/collection/collector.go`)

**Library (user's choice):** `github.com/crgimenes/native` v0.1.18, package `power` (MIT).
It brings in `github.com/ebitengine/purego` v0.11.3 (Apache-2.0). Both licences are compatible
with DDC's Apache-2.0. API used:

```go
tok, err := power.PreventSleep(reason) // errors.Is(err, power.ErrUnsupported) on Linux
...
_ = tok.Release() // idempotent
```

How the library does it, read at v0.1.18:
- **macOS:** an IOKit `PreventUserIdleSystemSleep` assertion via `IOPMAssertionCreateWithName`,
  called through purego without cgo. It works on battery too. The assertion belongs to the
  process, so macOS drops it when DDC exits, even after a crash. It is the same assertion that
  `caffeinate -i` takes.
- **Windows:** `SetThreadExecutionState(ES_CONTINUOUS | ES_SYSTEM_REQUIRED)` on a dedicated
  goroutine locked to its OS thread; `Release` clears the flag on that same thread. Windows
  drops it when the process exits.
- **Linux and others:** returns `ErrUnsupported`; purego is not linked into Linux binaries.

**Where:** in `collection.Execute` (`collector.go:242`). It is the single entry point that
`cmd/root.go:593` calls for every transport, both modes, and both CLI and TUI. Take the token
before `ExecuteStreamingCollect` and release it when `Execute` returns.

**Behaviour:**
- reason string: `Dremio Diagnostic Collector collection`. macOS shows it in `pmset -g assertions`.
- success → ddc.log INFO `keep-awake: idle system sleep blocked until the collection ends`;
- `ErrUnsupported` → ddc.log INFO `keep-awake: not supported on <GOOS>`;
- any other error → ddc.log WARN `keep-awake: unavailable: <err>`, and the collection continues.
  Keep-awake never fails a collection.
- release → ddc.log INFO `keep-awake: released`; a release error is a WARN only.
- No flag and no console output (non-goals).
- **Testability:** call the library through a package-level function variable (for example
  `preventSleep = power.PreventSleep`) so tests can swap in a fake. Exact shape is the builder's call.

### 7. Upgrade to Go 1.27.4 (`go.mod`, `go.sum`, `script/bootstrap*`)

- `go.mod`: `go 1.24.13` → `go 1.27.4`. The library needs at least 1.27.1; 1.27.4 is the
  latest patch (my call, the same practice as today's `1.24.13`).
- `go.mod`: add `godebug default=go1.24` (my call). Moving the `go` line past 1.24 would change
  these runtime defaults; this line keeps the Go 1.24 behaviour for all of them (values checked
  in `internal/godebugs/table.go` at go1.27.4):

  | Setting | Changed in | New default if not pinned | Why it matters to DDC |
  |---|---|---|---|
  | `tlssha1` | 1.25 | SHA-1 TLS 1.2 signatures refused | old Dremio endpoints could stop connecting |
  | `x509sslcertoverrideplatform` | 1.27 | `SSL_CERT_FILE`/`SSL_CERT_DIR` replace the Windows/macOS cert store | users with these set (e.g. Python/conda) would see different TLS trust |
  | `urlstrictcolons` | 1.26 | stricter `net/url.Parse` | user-supplied endpoint URLs |
  | `tlssecpmlkem` | 1.26 | extra post-quantum key shares | larger TLS handshakes through proxies |
  | `containermaxprocs`, `updatemaxprocs` | 1.25 | GOMAXPROCS follows cgroup limits | DDC in a pod (local-k8s) |
  | `x509sha256skid`, `decoratemappings`, `cryptocustomrand`, `httpservecontentmaxranges`, `tracebacklabels` | 1.25-1.27 | various | not used by DDC; pinned with the rest |

  The settings Go 1.27 removed (`asynctimerchan`, `tls10server`, `tlsrsakex`, `tls3des`,
  `tlsunsafeekm`, `x509keypairleaf`, `gotypesalias`) already had their new defaults under
  `go 1.24`, so removing them changes nothing for DDC. Adopting the new defaults is left for a
  separate change.
- Add the dependency with `go get github.com/crgimenes/native@v0.1.18`. The go.mod diff
  should be limited to the `go` line, the `godebug` line and the two new requirements
  (`crgimenes/native`, `ebitengine/purego` as indirect). Don't let `go mod tidy` regroup the
  existing require blocks (my call, surgical diff). No existing dependency version changes;
  this was confirmed in the trial.
- Lint: bump the pinned golangci-lint from v2.1.6 to **v2.15.0** in `script/bootstrap` and
  `script/bootstrap.ps1`. v2.1.6 is built with go1.24 and can't lint a go1.27 module;
  golangci-lint added Go 1.27 support in v2.13.0. In `bootstrap.ps1` also fix the module path:
  it installs `github.com/golangci/golangci-lint/cmd/golangci-lint@v2.x`, which `go install`
  rejects for a v2 version; the correct path is `github.com/golangci/golangci-lint/v2/cmd/golangci-lint`.
- Security scan: `script/bootstrap*` pin gosec v2.22.4, which `script/audit*` runs. Bump it to
  the newest release (v2.30.0 when this was written) and check that `script/audit` runs on the
  go1.27 module. If it doesn't, use the newest version that does (builder's call).
- **macOS minimum rises to 13 Ventura (accepted).** The Go linker stamps the minimum macOS
  version into every darwin binary: 11.0 with go1.24.13, 12.0 with go1.25/1.26, 13.0 with
  go1.27.4 (`cmd/link/internal/ld/macho.go`). purego v0.11.3 alone already needs go 1.25 (macOS 12).
- **macOS runtime start-up changes for the whole binary.** macOS release builds are
  cross-compiled with `CGO_ENABLED=0`. In that mode purego imports `internal/fakecgo`
  (`purego/nocgo.go`), which replaces the runtime's cgo hooks through `go:linkname` at start-up.
  This affects how every DDC process on macOS creates threads, whether or not keep-awake runs.
  It is purego's standard cgo-free macOS mode, the one Ebitengine ships with. It can't be tested
  on this machine, hence the Mac check under "Release prerequisites". Natively built Mac
  binaries (`script/build-musl-mac`, cgo on) use the real runtime/cgo instead.
- Toolchain on dev machines: `GOTOOLCHAIN=auto` (the default here) downloads go1.27.4
  automatically. The system Go on this machine is 1.22.3 and already switches to 1.24.13 this way.
- Not changed: `CGO_ENABLED` settings in the release scripts. macOS/Windows release builds are
  cross-compiled without cgo, which purego supports on macOS and Windows amd64/arm64 (Tier 1).
  The musl Linux builds use cgo but don't link purego.

### Trial (done before this spec, throwaway copy of the repo, Go 1.27.4)

The repo was copied to `/tmp`; the real repo and the installed tools were not touched. In the
copy: `go get go@1.27.4`, `go get github.com/crgimenes/native@v0.1.18`, a probe package
calling `power.PreventSleep`/`Release`, and `godebug default=go1.24`.

| Check | Result |
|---|---|
| go.mod diff | `go` line, `godebug` line, `crgimenes/native v0.1.18`, `ebitengine/purego v0.11.3`. No existing version changed. `go mod tidy` also regrouped require blocks, hence "don't tidy" above. |
| Cross-compile `ddc`, `CGO_ENABLED=0`, all six release targets | all OK |
| Probe linked per target | purego linked on darwin/windows amd64+arm64; not linked on linux |
| Windows probe run | `PreventSleep` ok; `CallNtPowerInformation(SystemExecutionState)` 0x0 before → 0x1 (`ES_SYSTEM_REQUIRED`) while held → 0x0 after release |
| `go vet ./...` (1.27.4) | clean |
| `go test -short ./...` (1.27.4, `SKIP_INTEGRATION_SETUP=true`) | 34 packages ok, 0 failed (incl. `cmd/local/jvmcollect`, `cmd/local/rockscollect`) |
| Same tests on the unchanged code with 1.24.13 (baseline, run concurrently) | `TestJFRCapture` failed with `TerminateProcess: Access is denied`; environmental and flaky on this box, and it passed under 1.27.4 |
| `godebug default=go1.24` | accepted; the binary's `DefaultGODEBUG` contains `tlssha1=1`, `x509sslcertoverrideplatform=0`, `urlstrictcolons=0`, `tlssecpmlkem=0`, `containermaxprocs=0`; `./cmd/root/... ./pkg/...` tests pass |
| golangci-lint v2.15.0 | 3 new findings, staticcheck QF1012 (`WriteString(fmt.Sprintf(...))` → `fmt.Fprintf`): `cmd/root/collection/stats.go:106`, `pkg/consoleprint/consoleprint.go:643`, `:646`. The current v2.1.6 reports 0 issues on the unchanged code. **The builder fixes these three lines** as part of the lint bump. |
| Not covered | musl static Linux builds (cgo + musl-cc, not available here); purego is not linked there. |

## Release prerequisites (user)

- The release build machine needs Go ≥ 1.27.4, or `GOTOOLCHAIN=auto` with network access to
  download it. With `GOTOOLCHAIN=local` and an older Go, the build stops with
  `go.mod requires go >= 1.27.4`.
- golangci-lint ≥ v2.13 wherever lint runs, and the bumped gosec wherever `script/audit` runs.
- **Mac check before the release goes out:** someone with a Mac on macOS 13 or newer runs the
  release `ddc` for darwin/arm64 (and darwin/amd64 if an Intel Mac is available): `ddc version`,
  then a short `ddc collect k8s standard` against a test cluster. Pass: both complete,
  `pmset -g assertions` lists the DDC assertion during the collection, and ddc.log has the
  keep-awake INFO line. Needed because of the purego start-up change above.
- CHANGELOG: state the new macOS 13 minimum.

## Compatibility

- `summary.json`: one new field, omitted when empty, the same way `incompleteCollections` and
  `memoryGuardSkips` were added. No reader of `summary.json` exists in the health-check
  importer repo (checked).
- Node status line and ddc.log: text is only added; existing lines don't change when nothing failed.
- Success rate: lower when exports fail or are stopped by the memory guard (intended). Display
  change: rates in [99.95, 100) now print `99.9%` instead of `100.0%`.
- CLI: no flag is removed. `--collector-timeout` keeps parsing and is hidden.
- What gets collected doesn't change. queries-perf output is byte-identical; it only skips the extra scan.
- Go 1.27: runtime defaults pinned to Go 1.24 (`godebug default=go1.24`). Compiler, standard
  library and garbage collector are newer, so memory use and timing can differ slightly.
- **macOS: 13 Ventura or newer required** (was 11). macOS release binaries start through
  purego's cgo stand-in.
- WLM exports on catalogs without WLM stores: still no file, as today; now an INFO line instead
  of an ERROR line.
- Keep-awake: on macOS and Windows the machine doesn't idle-sleep while a collection runs. The
  display may still turn off.

## Testing

Unit tests (`go test -short ./...`, Windows without `-race`):

- `FailedCollectionError.Error()` / `Unwrap()`.
- `RunRocksDBCollection` with a fake collector:
  - a `-type` export error (e.g. `sys.options`) → one failed item `sys.options`, and collection continues;
  - queries-perf data exec fails with zero records → failed item `queries-perf`;
  - a memory-guard stop → no failed item (a guard skip, as today);
  - a partial queries-perf export → no failed item (incomplete, as today);
  - `wlm_queues` exits 1 with `error: 'wlmqueue' column family not found`, delivered as a
    handler line (merged-output transport) and as `RemoteExitError.Stderr` (K8s keepalive-free)
    → no failed item, no file, an INFO line, and the line is not written into any export file;
  - any other exit-1 error still becomes a failed item.
- The incident sequence end to end on the collection layer: the data exec errors at connection
  setup → the node status line contains `FAILED: queries-perf`, `summary.json` contains a
  `failedCollections` entry, and the printed success rate is below 100.0%.
- A setup failure (e.g. upload error) → failed item `rocksdb-viewer` for that node.
- `nodeDoneStatus`: new cases for FAILED alone and combined with INCOMPLETE / MEMORY GUARD;
  existing expectations unchanged.
- `SummaryInfo`: `failedCollections` is omitted when empty and present when set (mirrors the
  existing omitempty tests).
- Success-rate display: 1999/2000 → `99.9%`; 10/10 → `100.0%`; a memory-guard skip counts as a
  failed attempt and an incomplete export does not.
- queries-perf issues no command containing `-count`. Update the existing fakes that answer
  `-count` (`rockscollect_test.go` ~450, 540, 564, 634, 678).
- Help text of the five `*-num-days` flags contains `0 = no limit`.
- `--collector-timeout` is deprecated and hidden, and a command line using it still parses.
- WARN on an unfiltered queries-perf export: logged when no `-days` is passed, not logged otherwise.
- Keep-awake, with the fake: the token is taken before collection and released after it,
  including when the collection returns an error; `ErrUnsupported` and other errors are
  logged and the collection still runs.

Manual checks:
- Windows (this machine, no admin rights needed): while `ddc.exe` collects,
  `CallNtPowerInformation(SystemExecutionState)` reports `ES_SYSTEM_REQUIRED`, and after the run
  it doesn't. The trial did this with a probe; `powercfg /requests` shows the same but needs admin.
- macOS: the pre-release Mac check under "Release prerequisites"; `pmset -g assertions` lists
  `PreventUserIdleSystemSleep ... "Dremio Diagnostic Collector collection"` during a run.
  The user has no Mac, so a colleague has to run it.

Environment notes for this machine:
- Set `SKIP_INTEGRATION_SETUP=true` when running `go test -short ./...`; without it the test
  setup deploys Dremio into the current kube context.
- Never run `go fmt ./...` (CRLF checkout, rewrites ~100 files); format only touched files.
- "Access is denied" failures in `cmd/local/rockscollect` or `cmd/local/jvmcollect`
  (`TestJFRCapture`: `TerminateProcess: Access is denied`) are environmental on this Windows
  box; the latter happened on the unchanged code during the trial.
- Build `bin/ddc.exe` after the change.

## Acceptance

1. All unit tests above pass; existing tests still pass, apart from the known environmental
   "Access is denied" failures.
2. `golangci-lint run` (v2.15.0) is clean for the touched packages, including the three QF1012
   lines from the trial.
3. All six release targets cross-compile with Go 1.27.4: linux, darwin and windows on amd64 and arm64.
4. The Windows keep-awake check above passes.
5. Live smoke run against a test cluster the user names (`ddc collect k8s standard`, read-only):
   queries-perf is collected, ddc.log contains no `-type queries_perf -count` and the keep-awake
   INFO line, and `summary.json` has no `failedCollections`. On an OSS cluster, WLM exports for
   missing stores show the "not available in this catalog" INFO line and no FAILED entry.
6. `script/audit` runs with the bumped gosec on the go1.27 module.

## Left to the builder

- How `RunRocksDBCollection` returns the failed items: a fifth return value, or a small result
  struct replacing the current four.
- Whether the success-rate truncation lives in a small helper, for testability.
- Exact log wording, as long as it carries the information given above.
- Log level for the new lines in the existing `FAILED COLLECTIONS:` section: follow that
  section's style.
- The shape of the keep-awake seam for tests.
- How the "not available in this catalog" outcome is returned (sentinel or typed error).
- The exact gosec version, if v2.30.0 doesn't run on the go1.27 module.
- For acceptance item 2, install golangci-lint v2.15.0 into a scratch directory (or use
  `go run ...@v2.15.0`) so the user's installed v2.1.6 isn't replaced.

## Confidence

| Part | Confidence | Why |
|---|---|---|
| 1. Failed-export reporting | 95% | Every failure path was traced in `rockscollect.go` and `streaming_collect.go`; the change mirrors the existing incomplete/guard reporting in the same places. Remaining risk: an unknown outside reader of `summary.json`, mitigated because the field is additive and omitted when empty. The "store not in catalog" detection reuses the memory-guard path, whose two stderr routes were checked in `collectRocksType` and `keepalive_free.go`. |
| 2. Drop the pre-flight count | 94% | The count only feeds the progress display; guard, marker and partial-export handling are on the data export. Remaining risk: the lost percentage is a deliberate UX trade. |
| 3. Document 0 for `*-num-days` | 97% | All five flags reach "no limit" for 0 (`isWithinDateRange`, `buildQueriesPerfFilterArgs`); only text and one WARN change. |
| 4. Deprecate `--collector-timeout` | 96% | No reader of the flag besides its definition; cobra v1.9.1 prints pflag deprecation messages (`command.go:1879-1880`). |
| 6. Keep-awake | 93% | Library source read; Windows effect measured (execution state 0x0 → 0x1 → 0x0). macOS couldn't be tested here; covered by the pre-release Mac check. |
| 7. Go 1.27.4 upgrade | 93% | The trial built all six targets, vet and all unit tests passed, the GODEBUG pin is confirmed in the binary, and the lint fallout is known (3 lines). The macOS minimum (13) is read from the linker and accepted. Remaining risk: purego's start-up change on macOS (untestable here, caught by the Mac check before release), the release machine's Go setup, and the musl cgo builds, which weren't tried here. |
| Overall | 93% | lowest parts: 6 and 7, both gated by the Mac check before release. |
