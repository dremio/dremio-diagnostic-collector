# Integrate dremio-rocksdb-viewer 1.1.0 (memory guard, jobs_recent window)

Date: 2026-10-21
Inputs: two handovers from the viewer repo (1.1.0 memory safety; jobs_recent time window), the
2026-10-17 customer incident analysis (coordinator OOM-killed during `sys.jobs_recent`).
DDC state checked at `7e6075c`.

## Intent

On 2026-10-17, DDC v4.0.5 ran `dremio-rocksdb-viewer -type sys.jobs_recent` inside a
customer's Dremio master container. The catalog was 791 GB with about 14M jobs, and the pod
had a 100Gi limit. Viewer 1.0.0 kept every row in memory. The container was OOM-killed,
which restarted the production master.

Viewer **1.1.0** (viewer commit `19d513c`) fixes the viewer side:
- every `sys.*` type streams, so memory no longer grows with row count;
- `sys.jobs_recent` honours `-days` / `-start-date`;
- a new **memory guard** exits with **code 3** before the container gets near its limit.

This change vendors 1.1.0 into DDC and makes DDC handle it correctly. The release itself
(version bump, changelog, tag) is done by the user and is **not** part of this work.

## Goals

1. Ship viewer 1.1.0 inside DDC (linux-amd64 and linux-arm64).
2. Bound `sys.jobs_recent` with a time window.
3. Handle exit 3 correctly on every transport: skip the type, keep going, and report why.
4. Stop DDC from holding a viewer type's whole output in memory.
5. Prove DDC still works end to end: unit tests, Docker runs against local catalogs, and a
   live collection on a test cluster.

## Non-goals

- Release work: version bump, `CHANGELOG`, tags.
- Recording the catalog WAL size, changing the TUI label, adding a DDC flag for
  `-mem-reserve-mb`, and "open once, export many types" (`-type a,b,c -out-dir`).
- Retrying viewer runs, or changing retry behaviour anywhere else.
- Changing how non-exit-3 viewer failures are reported (they remain ddc.log ERROR lines).
- Changing how the success rate is calculated (rocksdb items are not part of it today).
- Changing the K8s pinged executor, `HostExecute`, or any other transport behaviour beyond
  what is listed below (§3 executor choice, §4 typed exit errors, §5 long-line fix).

## Anti-goals (failure even if it "works")

- Any viewer run that could again grow without bound in the coordinator container.
- A guard-stopped (exit 3) run that leaves partial or bogus data in the bundle. Examples: a
  half-written `sys.*.json`, partial `queries-perf.*.json` files, or a
  `queries-perf.unknown.json` holding the guard's error line.
- A guard stop that is silent: only in a WARN line, or missing from the summary.
- Changed output for any type other than `sys.jobs_recent` (1.1.0 is byte-identical to
  1.0.0 when no new flags are passed).

## Facts the design rests on (verified at 7e6075c)

| Fact | Where |
|---|---|
| `collectRocksType` runs `<viewer> -db <path> -type <type>` with no filter. It is used for `cluster_stats`, every `sys.*` table (call site l.243) and the WLM types. | `cmd/root/collection/rockscollect.go:295` |
| `collectRocksType` uses `HostExecute`, which reads the entire stdout into one string (`cli.CollectOutput`) before `os.WriteFile`. | `rockscollect.go:296`, `cmd/root/cli/cli.go:60` |
| On SSH, local and kubectl-CLI, **stderr is merged into the same line handler as stdout** (`ExecuteAndStreamOutput`). The K8s pinged `HostExecuteAndStream` also writes stdout and stderr into one `K8SWriter`. | `cli.go:89-155`, `kubernetes.go:~370` |
| SSH/local/kubectl return non-zero exits as `cli.UnableToStartErr{Err: *exec.ExitError}`. That type has **no `Unwrap()`**, so `errors.As` cannot reach the exit code today. | `cli.go:38-45, 151` |
| K8s pinged exec returns client-go's exec error for a non-zero exit (`command terminated with exit code N`, the `k8s.io/client-go/util/exec.ExitError` interface). | observed in the incident `ddc.log` (`exit code 137`) |
| `HostExecuteAndStreamNoKeepalive` (K8s only, #339): stdout goes through an end-of-stream marker carrying the exit code; stderr is logged at WARN and never passed to the output handler. A non-zero exit returns a plain `fmt.Errorf("remote command exited with code %d")`, which is untyped. | `cmd/root/kubernetes/keepalive_free.go:79-176` |
| queries_perf data streams through `KeepaliveFreeStreamer` when available, else `HostExecuteAndStream`. On error with lines already written, it keeps the date files and returns `*IncompleteCollectionError` (#339 design). | `rockscollect.go:350-470` |
| `buildQueriesPerfFilterArgs` builds the queries_perf window: diagnosis `-start-date D -days N` or `-days N`; standard `-days <queries-perf-num-days>` (default 30). | `rockscollect.go:321` |
| `RocksCollectArgs.Days` is `DiagLogDays`, which is 0 outside diagnosis mode and `--days` (default 3) in diagnosis mode. | `cmd/root.go:611`, `streaming_collect.go:1073` |
| Incomplete items are already reported in the summary log (`Incomplete Collections: N` and the `INCOMPLETE COLLECTIONS:` list), in `summary.json` `incompleteCollections`, and on the node status line (`, INCOMPLETE: <item> (<n> records)`). | `streaming_collect.go:755-760, 1098, 1216`, `summary.go:31`, `collector.go:297, 334` |
| `ExecuteAndStreamOutput` (SSH, local, kubectl-CLI) reads stdout and stderr with `bufio.Scanner` at its **default 64 KiB line limit** and never checks `Err()`. A longer line stops the stdout reader, the remote process blocks on a full pipe, and `waitGroup.Wait()` never returns, so **DDC hangs**. Today's `HostExecute` for `sys.*` already has this bug. `sys.jobs_recent` (`query`) and `sys.views` (`sql_definition`) rows can exceed 64 KiB. | `cli.go:100, 107, 125-145` |
| `wlm_cluster_usage` scans every profile and prints one JSON object only at the end. It can run silently for hours on large catalogs (#338) and is off by default. | viewer `wlm.go`; DDC spec 2026-07-22 |
| The new binaries match the handover: amd64 2,451,652 B `e70081…0ab9`; arm64 2,179,748 B `49a574…3f6f`. | `sha256sum` on `dremio-rocksdb-viewer/bin/` |

### Viewer 1.1.0 contract (from its handover)

- **No new flags means identical output.** stdout and stderr are byte-identical to 1.0.0 for
  every type. Exit 3 is the only new behaviour.
- **Exit codes:** 0 ok; 1 error; 2 flag-parse error or panic; **3 memory guard**.
- **Exit 3, refuse:** the guard checks before the DB is opened; stdout is empty.
- **Exit 3, abort:** the guard re-checks every 1 s while running; stdout holds partial output,
  possibly ending mid-line.
- **Exit 3, stderr:** exactly one line:
  `error: memory guard: <refusing to start|aborting>: source=<cgroup2|cgroup1|host> used_mib=<n> limit_mib=<n> reserve_mib=<n>`.
  Callers must discard stdout, log the line, skip the type, continue, not retry, and show the
  line in the report.
- **Window flags:** `sys.jobs_recent` honours `-days N` (now − N days … now) and
  `-start-date D [-days N]`, the same semantics as queries_perf. Bounds are inclusive, in the
  process's local time zone (usually UTC in a pod). Rows without a submitted time are
  excluded when a filter is set. Other `sys.*` types ignore the flags.

## Decisions (user)

1. **Exit 3 → skip that type and continue** with the node's remaining viewer types. Do not
   stop the node's other viewer runs.
2. **jobs_recent window:**
   - standard mode passes `-days min(queries-perf-num-days, 7)`, with 7 when that value is
     ≤ 0;
   - diagnosis mode passes the same window as queries_perf (`buildQueriesPerfFilterArgs`),
     trusting the operator's explicit `--days` / `--start-date` with **no 7-day cap**.
3. **Stream viewer output to file** instead of buffering it in DDC memory.
4. **Live test** on the `healthcheck-aks` cluster (namespace `default`, `dremio-master-0`):
   collection only, nothing deployed.
5. **Scope:** integrate and verify only; the release is done by the user.
6. **K8s connection per type:** `wlm_cluster_usage` stays on today's pinged exec. Every other
   viewer type uses the keepalive-free stream with the end marker.
7. **Fix the 64 KiB line hang** in `ExecuteAndStreamOutput` as part of this change.
8. **Guard skips get their own reporting:** a new additive `summary.json` field, their own
   summary-log section and their own node-status label. The #339 INCOMPLETE output is left
   untouched.

## Design

### 1. Re-vendor

- Replace `cmd/local/rockscollect/rocksdb/dremio-rocksdb-viewer-linux-amd64` and
  `-linux-arm64` with the 1.1.0 files from
  `C:\Users\chufe\Workspaces\golang\dremio-rocksdb-viewer\bin\`.
- Verify both sizes and SHA-256 against the table above before and after copying.
- The embedding code (`rocksdb_embed.go`) is unchanged.

### 2. jobs_recent window

- Add `buildJobsRecentFilterArgs(args RocksCollectArgs) string` next to
  `buildQueriesPerfFilterArgs`:
  - if `StartDate != ""` or `Days > 0`, return `buildQueriesPerfFilterArgs(args)`;
  - otherwise return `-days min(QueriesPerfDays, 7)`, using 7 when `QueriesPerfDays <= 0`.
- Append it to the `-type sys.jobs_recent` command only. Other types get no filter.
- Log the resolved filter at INFO, as queries_perf already does.

### 3. Stream viewer output to file (`collectRocksType`)

Everything that goes through `collectRocksType` streams: `cluster_stats`, every `sys.*`
table and the WLM types.

- **Executor per type (user decision 6):**
  - `wlm_cluster_usage`: always `HostExecuteAndStream` (on K8s, today's pinged exec). Its
    output is one small object printed after a scan that can stay silent for hours, so it
    needs the keepalive pings, and its small tail makes #339 truncation unlikely. On this
    path the exit code arrives as client-go's exec `ExitError`, and stderr is merged into the
    line handler.
  - Every other type: `KeepaliveFreeStreamer.HostExecuteAndStreamNoKeepalive` when the
    collector implements it, otherwise `HostExecuteAndStream`. This mirrors
    `collectQueriesPerf`.
    - On K8s this gives end-of-stream detection (no silent #339 truncation) and keeps stderr
      out of the data.
    - The #339 spec's "accepted risk" (no keepalive while the viewer is silent, for example
      during a long catalog open) already applies to queries_perf and now applies to these
      types as well.
  - Update the keepalive-free decision record (D077 in `docs/architecture/decisions.md`) and
    the K8s line in `CLAUDE.md` ("file streams and the queries-perf stream run without
    keepalive pings") to cover the viewer `-type` streams, with `wlm_cluster_usage` as the
    exception.
- Write lines to a temporary file in the destination directory and rename it to the final
  name only on success. On **any** error (exit ≠ 0, missing end marker, write error),
  delete the temp file and produce nothing for that type.
  - `sys.*` and WLM types have no INCOMPLETE semantics today, and this keeps it that way.
- On merged-output transports, a stderr line can arrive in the line handler. Lines that
  start with `error: memory guard:` are never written to the data file; they are captured
  for reporting (§4).
  - On success the viewer prints nothing to stderr (byte-identical to 1.0.0), so no other
    filtering is needed.
- The empty-output case keeps today's behaviour (INFO "returned empty output", no file).
- The success log line (`rocksdb-viewer: collected <type> -> <path> (<bytes> bytes)`) is
  kept.

### 4. Exit 3 handling

**Detection.** Add one helper that extracts a remote exit code from any transport's error.
Changes it needs:
- add `Unwrap()` to `cli.UnableToStartErr`, so SSH, local and kubectl-CLI expose
  `*exec.ExitError` (for SSH this is ssh's own exit code, which is the remote command's);
- recognise client-go's exec `ExitError` (`ExitStatus()`) for the K8s pinged exec;
- replace the untyped `"remote command exited with code %d"` in `keepalive_free.go` with a
  typed error in package `collection` (next to `ErrStreamIncomplete`) that carries the code;
  its message stays the same.

**Guard line.** The report must show the guard's stderr line.
- On merged-output transports, the line handler captures it (§3).
- On the K8s keepalive-free path, stderr is already logged at WARN. That path also has to
  make the last stderr line available to the caller, for example carried in the typed exit
  error, so it can be reported.
- If no line is available, report `memory guard (exit 3)` without details.

**Behaviour per path:**

| Path | On exit 3 |
|---|---|
| `collectRocksType` (cluster_stats, sys.*, WLM) | Discard (delete temp file), ERROR log with the guard line, record a guard skip, continue with the next type. |
| queries_perf `-count` pre-flight | Record a guard skip and **skip queries_perf on that node**; the data stream is not started. *(my call)* |
| queries_perf data stream | Delete **all** date files written by this run, ERROR log, record a guard skip. **Not** `IncompleteCollectionError`, so no partial data is kept. Other errors keep today's #339 INCOMPLETE behaviour. |

- No retries anywhere.
- An exit 3 never aborts `RunRocksDBCollection`; cleanup (`rm -f` of the viewer) still runs.

**Reporting** (user decision 8). Guard skips are a category of their own, separate from
#339 INCOMPLETE items:

- **`summary.json`:** new additive field `memoryGuardSkips` (`[]string`, `omitempty`).
  Each entry looks like `dremio-master-0: sys.jobs_recent (refusing to start:
  source=cgroup2 used_mib=… limit_mib=… reserve_mib=…)`. Existing fields are unchanged.
- **Summary log:** a count line `Memory Guard Skips: N` next to `Incomplete Collections: N`,
  and a `MEMORY GUARD SKIPS:` list in the same style as `INCOMPLETE COLLECTIONS:`. Both
  appear only when N > 0. *(exact wording: my call)*
- **Node status line:** `, MEMORY GUARD: <item>[, <item>…]`, next to the existing
  `, INCOMPLETE: …` label. *(exact wording: my call)*
- `Incomplete Collections` counts, INCOMPLETE labels and their wording stay exactly as
  they are; guard skips are never counted there.
- `RunRocksDBCollection` returns the guard skips separately from the incomplete items. The
  builder chooses the shape: an extra return value or a result struct.

**Viewer version in ddc.log** *(my call)*. After uploading the binary, run `<viewer>
-version` once and log the result at INFO, so every bundle shows which viewer version ran.
A failure there is logged at WARN and does not stop collection.

### 5. Long lines on SSH, local and kubectl-CLI (`cmd/root/cli/cli.go`)

User decision 7: fix the 64 KiB hang in `ExecuteAndStreamOutput`.

- Give both scanners (stdout and stderr) a larger maximum line size: **64 MiB** *(my call)*,
  with a small initial buffer so normal lines cost nothing extra.
- After each scan loop, check `scanner.Err()`. On an error, including a line longer than
  the maximum:
  - keep draining that pipe (`io.Copy(io.Discard, …)`) so the remote process can exit and
    the other reader and `cmd.Wait()` finish;
  - return an error that names the cause (for example `line exceeds 64 MiB`) instead of
    hanging.

  If both the read error and a non-zero exit happen, return the read error, wrapping the
  exit error so §4's exit-code helper can still find it.
- `Execute` (uses `CombinedOutput`) and the K8s `K8SWriter` have no line limit and stay
  unchanged.

## Verification

1. `go test -short ./...` (Windows, no `-race`), `go fmt ./...`, `golangci-lint run`, and the
   build to `bin/ddc.exe` all pass.
2. **Unit tests**, with mock collectors as in the existing `rockscollect_test.go`, covering:
   - `buildJobsRecentFilterArgs`: standard 30 → 7, standard 3 → 3, standard 0 → 7,
     diagnosis `-days 3`, diagnosis `-start-date D -days N`, diagnosis 30 → 30 (no cap);
   - `collectRocksType` streaming:
     - success writes the exact bytes;
     - an error leaves no file;
     - a guard line on merged output is not written to data;
     - an empty output produces no file;
   - exit 3 on `sys.*`: skipped, reported, next type still runs;
   - exit 3 on queries_perf `-count`: data stream not started;
   - exit 3 on the queries_perf data stream after N lines: no date files remain, reported as
     a guard skip, not INCOMPLETE;
   - the exit-code helper recognises all three error shapes (`UnableToStartErr` wrapping
     `*exec.ExitError`, client-go exec `ExitError`, the typed keepalive-free error);
   - `wlm_cluster_usage` goes through `HostExecuteAndStream` even when the collector
     implements `KeepaliveFreeStreamer`, and every other type goes keepalive-free;
   - reporting:
     - a guard skip appears in `memoryGuardSkips`, in the summary-log section and on the
       node status line;
     - the INCOMPLETE count and wording are unchanged when both kinds occur together;
   - `ExecuteAndStreamOutput`:
     - a line over 64 KiB (for example 1 MiB) arrives intact;
     - a line over the maximum returns an error and **does not hang** (test with a
       timeout);
     - a non-zero exit is still reported. Use the Go helper-process pattern so this runs
       on Windows too;
   - existing #339 tests (`keepalive_free_test.go`, INCOMPLETE flow) still pass unchanged
     in behaviour.
3. **Docker, local catalogs** (`dremio-rocksdb-viewer/catalog/{cataloghc,datev,test1}`):
   - run the new linux-amd64 binary with the exact command strings DDC now builds;
   - every type except `sys.jobs_recent` is byte-identical to the currently vendored 1.0.0;
   - `sys.jobs_recent` with the window equals the unfiltered output filtered by
     `submitted_epoch_millis`. The local catalogs hold old data, so also use a
     `-start-date` that covers it, so the window isn't trivially empty;
   - `docker run --memory=1g` produces exit 3 and the exact guard stderr line. Use that line
     as the fixture text in the unit tests.
4. **Live, healthcheck-aks** (`--context healthcheck-aks --namespace default`):
   - run `ddc collect k8s standard` with the system tables including `tables`, `views` and
     `jobs_recent`, plus WLM and queries-perf;
   - check that:
     - ddc.log shows `dremio-rocksdb-viewer v1.1.0`;
     - the jobs_recent command carries `-days 7`, and every row's `submitted_epoch_millis`
       falls inside the window;
     - every other rocksdb file is non-empty and valid JSONL;
     - no viewer error or WARN stderr lines appear;
     - the summary has no incomplete or guard entries;
     - the pod's restart count is unchanged.
   - Collection only, per CLAUDE.md: no deployments, no changes to the cluster.

## Left to the builder

- Function and type names, file placement, and exactly how the guard line travels from
  `HostExecuteAndStreamNoKeepalive` to the caller.
- How the temp file is named and how the date files from a queries_perf run are tracked
  for deletion.
- Whether `collectRocksType` gains a parameter for extra arguments or jobs_recent gets its
  own call path.
- Test structure, beyond the cases listed above.

## Process notes

- No commits until the user has reviewed. The work ends in **one squashed commit on a
  feature branch** that includes this spec and the plan. Push or open a PR only when the
  user chooses it in the finish step.
- Always rebuild `bin/ddc.exe` after code changes.
