# Stop silent truncation of K8s exec streams (skipped queries.json / server logs)

Date: 2026-09-29
Issue: [#339 — Few queries.json files missed while collecting DDC bundle using DDC v4.0.5 CLI](https://github.com/dremio/dremio-diagnostic-collector/issues/339)

## Problem

On a Kubernetes (EKS) cluster reached over a slow link (~500 KB/s per stream, API endpoint
behind a local port-forward), `ddc collect k8s standard` skips 15–27 files per run:
`queries.*.json.gz` archives on the coordinator and `server.*.log.gz` / `server.log` on
executors. Every skip is the same: three attempts of
`gzip decompress copy failed … unexpected EOF`, each logged right after
`StreamFromHost: completed streaming`, followed by `stream skip … exhausted 3 retries`.
The summary still reports `Success Rate: 100.0% (125/125)`.

### Root cause (reproduced)

1. `StreamFromHost` runs `gzip -1 -c <file>` over a SPDY exec stream. The remote process
   finishes almost immediately — socket buffers along the path absorb most of the file —
   while several MB are still queued for the slow client.
2. The server side (kubelet `ServeExec` / API-server `UpgradeAwareHandler`) writes the
   Success status and closes its socket while that tail is still draining.
3. client-go's SPDY transport sends a keepalive **PING every 5 s**
   (`spdy.RoundTripperFor`, `PingPeriod: 5s`, hard-coded). When a PING reaches the
   already-closed socket, the Linux kernel aborts the connection (`TCPAbortOnData`): it
   sends RST and discards the queued stdout tail **and** the Success status.
4. client-go's `watchErrorStream` treats "no status received" as success, so
   `StreamWithContext` returns `nil` on a truncated stream.
5. DDC's gzip reader detects the missing trailer (`unexpected EOF`); the 3 immediate
   retries hit the same conditions; the file is skipped.

Evidence:

- Customer ddc.log: 102/205 stream attempts failed; **0 of 67 attempts shorter than 5 s
  failed**; every failure was cut 5–17 s in; files < 1 MB never failed; 17 files failed
  and later succeeded (per-attempt, not per-file). A 2 GB queries-perf stream crossed the
  same link intact (no mid-stream loss).
- Local reproduction with the exact libraries DDC ships (client-go / apimachinery /
  kubelet `ServeExec` v0.32.3) on Linux, 8 MB payload, 500 KB/s:

  | Scenario | Lost data |
  |---|---|
  | 5 s pings, 2 proxy hops (plain / TLS / TLS + slow-link relay) | 5/6 · 6/6 · 6/6 |
  | Pings disabled (plain / TLS / TLS + slow link) | 0/6 · 0/6 · 0/6 |
  | Prototype of the executor designed below (rest.Config + bearer → TLS → `PingPeriod: 0`) | 0/6, bearer token reached kubelet |
  | 2 MB payload (3.9 s, shorter than the ping period) | 0/6 |

  Kernel `TcpExtTCPAbortOnData` increased by exactly the number of truncated trials, and by
  0 with pings off. With pings off the client sends nothing after stream setup except a
  final 24 B TLS `close_notify`, so no hop (API server, NLB, SSM/SSH tunnel) can receive
  data on a closed socket — the fix is path-independent. The abort is a server-side Linux
  kernel behaviour: it affects DDC on every client OS (native Windows loopback does not
  reproduce it).

### Related exposures (same mechanism, no detection today)

- **JVM artifacts** (JFR, heap dumps, async-profiler output) stream with `cat` through
  `streamRemoteFile`; `expectedSize` is probed but only used for progress. A truncated heap
  dump is saved silently, and callers delete the remote temp file right afterwards.
- **`cat` fallback** (nodes without gzip): truncation is only caught by the advisory
  checksum (warning only).
- **queries-perf** streams through `HostExecuteAndStream` with no completeness check; the
  pre-flight `-count` cannot serve as one because the `-days` window slides between count
  and stream (the customer run ended +1 over the count).

## Requirements (user-confirmed)

- Scope: root-cause fix plus completeness guards; **no offset-resume**.
- Ping-free SPDY for **file streams** (`StreamFromHost`) and for the **queries-perf data
  stream only**. All other execs (discovery, `HostExecute`, RocksDB `-type` commands,
  cleanup, `CopyToHost`) keep client-go's 5 s pings.
- The queries-perf path is reached through an **optional interface** implemented only by
  the K8s API transport; the `Collector` interface, other transports and mocks stay
  unchanged.
- Completeness detection "approach A": rotation-safe byte-count guard for file streams;
  end-of-stream sentinel for the queries-perf stream.
- An incomplete queries-perf export keeps its partial data, is flagged **INCOMPLETE**, and
  is not retried automatically.
- Reporting: skipped files count against the success rate; INCOMPLETE items appear in the
  summary log, `summary.json` (additive field) and the node status line; skipped entries in
  ddc.log name their host. `summary.json` `skippedFiles` format stays unchanged.
- No new CLI flags or TUI options; process exit code unchanged.

## Non-goals

- Offset-resume of truncated streams.
- A WebSocket executor (client-go's WebSocket executor also pings every 5 s, so it is not
  expected to avoid this).
- Changes to the kubectl-CLI transport, retry counts or backoff.
- Fixing the pre-existing concurrent stdout/stderr writes into one `K8SWriter` in
  `HostExecuteAndStream`; the new method avoids it, the existing method is left as is.

## Design

### 1. K8s transport (`cmd/root/kubernetes/kubernetes.go`)

**Two executor flavours.** `KubeCtlAPIActions` gains a second factory next to
`spdyExecutorFn`:

```go
streamExecutorFn ExecutorFactory // ping-free; set in NewK8sAPI

func (c *KubeCtlAPIActions) newStreamExecutor(method string, u *url.URL) (remotecommand.Executor, error) {
    return c.streamExecutorFn(c.config, method, u)
}
```

The production factory mirrors client-go's `spdy.RoundTripperFor` with only the ping
removed, so authentication (bearer token, exec credential plugins such as
`aws eks get-token`, client certificates) and proxy handling stay identical:

```go
func newKeepaliveFreeSPDYExecutor(config *rest.Config, method string, u *url.URL) (remotecommand.Executor, error) {
    tlsConfig, err := rest.TLSConfigFor(config)
    if err != nil { return nil, err }
    proxy := http.ProxyFromEnvironment
    if config.Proxy != nil { proxy = config.Proxy }
    upgrader, err := spdy.NewRoundTripperWithConfig(spdy.RoundTripperConfig{
        TLS: tlsConfig, Proxier: proxy, PingPeriod: 0, // no keepalive: see #339
    })
    if err != nil { return nil, err }
    wrapper, err := rest.HTTPWrappersForConfig(config, upgrader)
    if err != nil { return nil, err }
    return remotecommand.NewSPDYExecutorForTransports(wrapper, upgrader, method, u)
}
```

(`spdy` here is `k8s.io/apimachinery/pkg/util/httpstream/spdy`.) `newExecutor` is
unchanged and keeps serving every other exec. `Protocol()` still reports `SPDY`.

**Users of the ping-free executor:**

1. `StreamFromHost` — every file stream.
2. New method `HostExecuteAndStreamNoKeepalive(host string, output cli.OutputHandler, args ...string) error`.

**Optional interface and error** (`cmd/root/collection/collector.go`, beside `Collector`):

```go
// KeepaliveFreeStreamer is implemented by transports that can run a long,
// continuously streaming command without keepalive pings and prove it completed.
type KeepaliveFreeStreamer interface {
    HostExecuteAndStreamNoKeepalive(host string, output cli.OutputHandler, args ...string) error
}

var ErrStreamIncomplete = errors.New("remote stream ended before end-of-stream marker")
```

**`HostExecuteAndStreamNoKeepalive` behaviour:**

- Command: `sh -c "<args joined>; printf '\n__DDC_EOS__:%d\n' $?"`. The leading `\n`
  guarantees the marker is on its own line even when the command's last line has no
  newline. The marker shares stdout with the command's output, so it can only arrive if
  every earlier byte did.
- stdout goes to a new `eosLineWriter`; stderr goes to a separate `K8SWriter` whose lines
  are logged at WARN. The two streams never share a writer.
- `eosLineWriter` splits lines like `K8SWriter`, but holds back one complete line:
  - On a line matching `^__DDC_EOS__:(-?\d+)$`: record the exit code, drop the held-back
    line if it is empty (the synthetic line from the leading `\n`), otherwise emit it. Never
    forward the marker.
  - At stream end without the marker: emit the held-back line (it is complete), **discard
    any trailing partial line** (it may be a cut-off record), return `ErrStreamIncomplete`.
  - Marker with exit code ≠ 0: return `fmt.Errorf("remote command exited with code %d", rc)`.
    Required because `sh` itself now always exits 0.
- No stdin/PAT support (not needed by the caller); args logged unmasked like the existing
  unmasked path. Runs under `c.hook.GetContext()` (no extra timeout), like
  `HostExecuteAndStream`.

**Accepted risk:** the queries-perf stream has no SPDY keepalive; a RocksDB viewer phase
that emits nothing for longer than an **L7 proxy's** idle timeout can drop the connection.
L4 load balancers (AWS NLB, Azure LB) are still covered: apimachinery dials with a
zero-value `net.Dialer`, so Go's default OS-level TCP keepalive (15 s, payload-free, cannot
trigger `TCPAbortOnData`) keeps refreshing their idle timers. In the reporter's run the
first record arrived within 59 s. The sentinel turns any drop into a flagged INCOMPLETE
instead of silent loss.

### 2. Collection-layer guards (`cmd/root/collection/`)

**2a. Rotation-safe byte-count guard.** New helper in `streaming_collect.go`:

```go
var ErrStreamTruncated = errors.New("stream truncated")

func checkStreamComplete(c Collector, host, remotePath string, got, expected int64) error
```

Called in `streamFileOnce` after flush/close and before hashing, on both the gzip and the
`cat` path (`got` = bytes written locally, i.e. decompressed bytes on the gzip path):

| Condition | Result |
|---|---|
| `expected <= 0` | accept (size unknown) |
| `got >= expected` | accept (active files grow) |
| `got < expected`: `cur, ok := probeRemoteFileSizeOK(...)` | |
| … `ok && cur < expected && got >= cur` | accept; INFO `stream: <host>:<path> shrank <expected>→<cur> (rotated), accepted` |
| … `ok && cur < expected && got < cur` | `*fileRotatedError{Size: cur}` wrapping `ErrStreamTruncated`; `streamFile` retries expecting the fresh size `cur` (the rotated file only grows), so an active log that rotated after discovery is collected while real truncation is still caught |
| … otherwise (incl. probe failure) | `fmt.Errorf("%w for %v:%v: got %d of %d bytes", ErrStreamTruncated, …)` |

On error the local file is removed. The error flows through the existing
`isTransientError` (`ErrStreamTruncated` is always transient) and the existing retry/backoff in `streamFile`.
`probeRemoteFileSizeOK(c, host, path) (int64, bool)` is added next to
`probeRemoteFileSize` in `jvmcollect.go` (same `stat -c %s` / `stat -f %z` probes; returns
`ok=false` when both fail or parsing fails). Existing `probeRemoteFileSize` callers are
unchanged. The guard applies to all transports; on SSH, local and kubectl it only fires
on real truncation.

**2b. JVM artifacts.** `streamRemoteFile(c, host, remotePath, localPath)` keeps its
signature and delegates to
`streamFile(c, host, remotePath, localPath, maxRetries, expectedSize, filepath.Base(localPath), "", false)`,
draining the returned hash channel. It gains the size guard and up to 3 attempts before
callers delete the remote temp file. Progress display is unchanged (same
`progressWriter`); an empty checksum tool means no hashing.

**2c. queries-perf INCOMPLETE flow** (`rockscollect.go`).

- `collectQueriesPerf` uses `c.(KeepaliveFreeStreamer)` when available, else
  `HostExecuteAndStream` (unchanged behaviour for SSH/local/kubectl).
- On a streaming error:
  - `lineCount == 0` → failure, as today (`return nil, err`).
  - `lineCount > 0` → close the date files, collect them as usual, and return them with
    an `*IncompleteCollectionError{Item: "queries-perf", Records: N, Cause: err}`.
    Causes: end-of-stream marker missing, exit code ≠ 0, transport error.
- New type in `rockscollect.go`:

  ```go
  // IncompleteCollectionError reports a collection that returned usable but partial data.
  type IncompleteCollectionError struct {
      Item    string // e.g. "queries-perf"
      Records int
      Cause   error
  }
  func (e *IncompleteCollectionError) Error() string // "<item> incomplete after <n> records (<cause>)"
  func (e *IncompleteCollectionError) Unwrap() error { return e.Cause }
  ```

  `RunRocksDBCollection` detects it with `errors.As`, appends the returned files, logs
  WARN, and records the item; any other queries-perf error keeps today's handling (logged,
  files not returned).
- The pre-flight count is logged next to the result
  (`streamed N records (pre-flight count M)`) and is never a pass/fail signal.
- `RunRocksDBCollection` becomes
  `func RunRocksDBCollection(args RocksCollectArgs) ([]helpers.CollectedFile, []*IncompleteCollectionError, error)`;
  the caller formats the summary entry as `"<host>: <err>"` (e.g.
  `dremio-master-0: queries-perf incomplete after 187442 records (…)`) and the node-line
  entry as `"<item> (<records> records)"`. Call sites: `streaming_collect.go` and 4 tests.

### 3. Reporting

- **Success rate** (`collector.go`): extract
  `successRate(collected, failed, skipped int) (pct float64, attempts int)` with
  `attempts = collected + failed + skipped`. The customer run would read
  `Success Rate: 82.2% (125/152)`. Incomplete items are not part of the rate (their files
  count as collected) and are listed separately.
- **Summary log:** `logDistributedCollectionSummary` receives the host-qualified skipped
  list and a new `incomplete []string`. Adds `Incomplete Collections: N` under COLLECTION
  RESULTS and an `INCOMPLETE COLLECTIONS:` list logged at WARN.
- **Host-qualified skips (log only):** `streaming_collect.go` aggregates
  `totalSkippedFiles` (paths, unchanged, for `summary.json`) and `totalSkippedLog`
  (`host:path`, for the summary log).
- **`summary.json`:** new field
  `IncompleteCollections []string \`json:"incompleteCollections,omitempty"\``. Output is
  byte-identical when nothing is incomplete.
- **Node status line** (coordinator): built from the incomplete items that
  `RunRocksDBCollection` returned for that node; appends `, INCOMPLETE: queries-perf (N records)`
  after the skipped part, e.g. `Done: 86 files (2.3GB), 0 skipped, INCOMPLETE: queries-perf (187442 records)`.
  Incomplete items are also appended to a run-wide `totalIncomplete` list feeding the
  summary log and `summary.json`.
- Unchanged: exit code, TUI "COMPLETED AT" line, archive layout.

### 4. Testing

**Unit tests** (`go test -short ./...`, all OS):

- `kubernetes`: executor routing (`StreamFromHost` and `HostExecuteAndStreamNoKeepalive`
  use `streamExecutorFn`, `HostExecute` uses `spdyExecutorFn`); `eosLineWriter` table
  driven through a `mockExecutor` that writes to `StreamOptions.Stdout/Stderr`: marker with
  rc 0, last line without newline, missing marker (partial tail dropped,
  `ErrStreamIncomplete`), rc 3, stderr isolation, output legitimately ending in an empty
  line.
- `collection`: `checkStreamComplete` table (unknown, grew, exact, rotated, probe failure,
  shortfall without shrink); `streamFile` short-then-full stream succeeds on attempt 2;
  `streamRemoteFile` retries a truncated JFR and removes the file on persistent
  truncation; `collectQueriesPerf` incomplete-after-N-lines, error-before-first-line, and
  fallback without the interface; `RunRocksDBCollection` propagates incomplete items;
  `successRate` helper.
- Test fixtures: streaming mocks (`mockStreamCollector` etc.) stream exactly `Size` bytes
  so the new guard does not reject fixtures declaring `Size` with 0-byte mock streams.

**Loopback regression test** (`cmd/root/kubernetes`): kubelet `ServeExec` with a fake
executor behind an apimachinery `UpgradeAwareHandler`, over TLS (`httptest`), client built
with DDC's real `newKeepaliveFreeSPDYExecutor`, throttled consumer.

- All OS, `-short`: 4 MB drained at 2 MB/s arrives complete.
- Linux only, skipped under `-short`: self-validating pair — the default 5 s-ping executor
  must truncate at least once in 3 trials (otherwise `t.Skip("environment does not
  reproduce keepalive truncation")`), then the ping-free executor must be 0/3.
- Adds `k8s.io/kubelet v0.32.3` as a test-only dependency. **Must be pinned**
  (`go get k8s.io/kubelet@v0.32.3`): an unpinned `go get`/`go mod tidy` resolves v0.37.1,
  which requires Go 1.26 (forces a toolchain switch) and no longer contains the package.
  Verified: pinned, it adds `k8s.io/kubelet` and `k8s.io/apiserver v0.32.3 // indirect` to
  go.mod and leaves the production binary's module list unchanged (79 modules).

**Test speed:** `streamFile`'s backoff sleep becomes a package variable
(`streamBackoffSleep = time.Sleep`) that tests override, so JVM stream-failure tests
(`TestCollectJFR_StreamFailure`, async-profiler failure test) don't pay 3.5 s of real
backoff now that `streamRemoteFile` retries.

**Checks:** `go fmt ./...`, `golangci-lint run`, `go test -short ./...`,
`go build -o bin/ddc.exe .`

## Compatibility (breaking-change review)

No user-facing breaking changes: CLI flags, config keys, archive layout, exit code and
existing `summary.json` fields are unchanged.

- **Exported Go API:** `RunRocksDBCollection` gains a return value (1 production + 4 test
  call sites; `cmd/...` is not consumed as a library).
- **Additive output:** `summary.json` `incompleteCollections` (omitempty); ddc.log success
  rate now counts skips; SKIPPED entries in ddc.log are `host:path`; INCOMPLETE lines in
  log and node status.
- **Behaviour:** truncated transfers on any transport are retried/skipped instead of
  accepted; JVM artifacts get up to 3 attempts; on K8s the queries-perf stream runs without
  SPDY pings (see accepted risk) and its stderr goes to ddc.log instead of into the
  queries-perf data files (stray non-record lines no longer pollute them).
- **Requirements:** the remote shell must provide `printf` (POSIX builtin in dash, bash,
  busybox ash; `sh -c` is already required).
- **Dependencies:** test-only `k8s.io/kubelet v0.32.3` (pinned); production module set
  unchanged.
- **Tests to update:** fixtures whose mock streams write fewer bytes than the declared
  `Size` (default `mockStreamCollector` streams 0 bytes; 17 custom `streamFunc`s).

## Acceptance

1. All unit tests and the loopback test pass; lint clean.
2. Field check: the reporter runs a test build with the same flags on the affected EKS
   cluster. Pass = no `transient error … unexpected EOF`, 0 skipped files (baseline 15–27
   per run), queries-perf not INCOMPLETE.

## Documentation

- `CHANGELOG.md`: `4.0.6` entry — K8s file streams no longer skip files over slow links
  (#339); truncated transfers are retried or flagged instead of accepted; success rate
  counts skipped files; INCOMPLETE queries-perf is reported.
- `docs/architecture/decisions.md`: new decision "Ping-free SPDY executor for streaming
  execs, end-of-stream sentinel for queries-perf" referencing #339.
- Correct the stale "WebSocket primary, SPDY fallback" statements (CLAUDE.md, D076, R076):
  the K8s API transport is SPDY-only.
- `docs/architecture/patterns-and-gotchas.md`: client-go SPDY keepalive + slow link ⇒
  silent tail truncation.

## Confidence

94% that ping-free `StreamFromHost` removes these skips in the reporter's environment
(verified over TLS, slow link and the real proxy/streaming code; path-independent by
construction). The remaining uncertainty is the live EKS path, closed by the field check.
