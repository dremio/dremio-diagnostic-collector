# K8s Stream Truncation Fix (#339) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop K8s collections over slow links from silently truncating (and then skipping) streamed files, detect truncation on every stream path, and report skipped/incomplete collections honestly.

**Architecture:** File streams (`StreamFromHost`) and the queries-perf data stream use a SPDY exec executor with keepalive pings disabled — the pings are what make the Linux kernel reset a closing connection and drop the output tail. queries-perf additionally appends an end-of-stream marker (`printf '\n__DDC_EOS__:%d\n' $?`) whose absence proves truncation. In the collection layer a rotation-safe byte-count guard retries short file transfers (including JVM artifacts), an incomplete queries-perf keeps its records and is flagged, and the summary counts skips and lists incomplete items.

**Tech Stack:** Go 1.24.13, k8s.io/client-go + apimachinery v0.32.3 (SPDY remotecommand), k8s.io/kubelet v0.32.3 (test-only, real `ServeExec` for the loopback regression test).

**Spec:** `docs/superpowers/specs/2026-09-29-k8s-stream-truncation-design.md`

## Global Constraints

- Go toolchain from `go.mod` (`go 1.24.13`); client-go / apimachinery stay at `v0.32.3`.
- `k8s.io/kubelet` is added **test-only and pinned**: `go get k8s.io/kubelet@v0.32.3`. Never run `go get k8s.io/kubelet` without `@v0.32.3` (latest v0.37.1 requires Go 1.26 and lacks the package). The production binary's module list must stay unchanged.
- No new CLI flags, TUI options or config keys; process exit code unchanged.
- `summary.json`: only the additive field `IncompleteCollections []string \`json:"incompleteCollections,omitempty"\``; `skippedFiles` format unchanged.
- Only the Kubernetes API transport (`cmd/root/kubernetes`) implements `collection.KeepaliveFreeStreamer`; the `Collector` interface is unchanged.
- SPDY keepalive pings stay on for every exec except `StreamFromHost` and `HostExecuteAndStreamNoKeepalive`.
- End-of-stream marker text is exactly `printf '\n__DDC_EOS__:%d\n' $?` appended as `; printf …` to the joined command.
- **Commits:** work happens on branch `fix/339-k8s-stream-truncation`. Each task ends with exactly one local commit `wip(#339): task N — <task title>` (these give reviewers a per-task diff). When all tasks are done, every commit — code, spec and plan — is squashed into a single commit. Never push.
- After code changes in every task, `go build -o bin/ddc.exe .` must succeed.
- Tests on Windows: `go test -short ./...` (no `-race` without CGO). gosec runs on test files: never disable TLS verification (trust the httptest certificate via `RootCAs`/`CAData` instead) and set `MinVersion: tls.VersionTLS12` on any `tls.Config`. Do not use `math/rand` in new code.
- Public repository: no customer names, account IDs, hostnames or endpoints in code, tests, docs or changelog.
- Line numbers below refer to HEAD `3a93f32`; re-locate by the quoted code if they drift.

## Review Focus

1. A marker line split across two `Write` calls (`"a\n\n__DDC_E"` + `"OS__:0\n"`) must still be recognised as the marker — pinned in Task 3 (`marker split across writes`).
2. A data line that only looks like the marker (`__DDC_EOS__:x`) must pass through as data — pinned in Task 3 (`marker-like data line`).
3. A file rotated to 0 bytes between discovery and streaming (got 0, now 0, expected > 0) must be accepted, not retried and skipped — pinned in Task 4 (`rotated to empty`).
4. queries-perf on a transport without `KeepaliveFreeStreamer` (SSH/local/kubectl) that errors after some records must be INCOMPLETE with records kept, same as K8s — pinned in Task 6 (`TestCollectQueriesPerf_FallbackTransportIncomplete`).
5. A node with both skipped files and an incomplete queries-perf must show both on its status line, skipped part first — pinned in Task 7 (`skipped and incomplete`).

---

## File Structure

| File | Change | Responsibility |
|---|---|---|
| `cmd/root/kubernetes/keepalive_free.go` | Create | Keepalive-free SPDY executor factory; `eosLineWriter`; `HostExecuteAndStreamNoKeepalive` |
| `cmd/root/kubernetes/keepalive_free_test.go` | Create | Executor build, executor routing, marker parsing, keepalive-free stream tests |
| `cmd/root/kubernetes/stream_loopback_test.go` | Create | Loopback regression test with real kubelet `ServeExec` behind two upgrade proxies |
| `cmd/root/kubernetes/kubernetes.go` | Modify | `streamExecutorFn` field, `NewK8sAPI` wiring, `newStreamExecutor`, `StreamFromHost` uses it |
| `cmd/root/collection/collector.go` | Modify | `KeepaliveFreeStreamer`, `ErrStreamIncomplete`, `successRate`, summary log |
| `cmd/root/collection/streaming_collect.go` | Modify | `ErrStreamTruncated`, `streamBackoffSleep`, `checkStreamComplete`, guard in `streamFileOnce`, `nodeDoneStatus`, aggregation |
| `cmd/root/collection/jvmcollect.go` | Modify | `probeRemoteFileSizeOK`; `streamRemoteFile` delegates to `streamFile` |
| `cmd/root/collection/rockscollect.go` | Modify | `IncompleteCollectionError`; `collectQueriesPerf` streamer choice + incomplete; `RunRocksDBCollection` signature |
| `cmd/root/collection/summary.go` | Modify | `IncompleteCollections` field |
| `cmd/root/collection/stream_guard_test.go` | Create | Guard + retry tests; `withNoStreamBackoff`, `statCollector` helpers |
| `cmd/root/collection/streaming_collect_test.go` | Modify | Fixture sizes aligned with streamed content |
| `cmd/root/collection/jvmcollect_test.go` | Modify | Truncated-artifact tests; no-backoff in two failure tests |
| `cmd/root/collection/rockscollect_test.go` | Modify | queries-perf tests; call sites for new signature |
| `cmd/root/collection/collector_test.go` | Modify | `successRate`, `nodeDoneStatus`, `summary.json` tests |
| `go.mod`, `go.sum` | Modify | Test-only `k8s.io/kubelet v0.32.3` |
| `CHANGELOG.md`, `CLAUDE.md`, `docs/architecture/{decisions,capability-contract,patterns-and-gotchas}.md` | Modify | Release notes, D077, stale WebSocket statements, gotcha |

---

### Task 1: Keepalive-free SPDY executor for file streams

**Files:**
- Create: `cmd/root/kubernetes/keepalive_free.go`
- Modify: `cmd/root/kubernetes/kubernetes.go` (struct at ~133-145, `NewK8sAPI` at ~65-78, after `newExecutor` at ~147-150, `StreamFromHost` at ~675)
- Test: `cmd/root/kubernetes/keepalive_free_test.go` (create)

**Interfaces:**
- Consumes: existing `ExecutorFactory func(config *rest.Config, method string, url *url.URL) (remotecommand.Executor, error)`, `KubeCtlAPIActions`.
- Produces:
  - `func newKeepaliveFreeSPDYExecutor(config *rest.Config, method string, u *url.URL) (remotecommand.Executor, error)`
  - field `KubeCtlAPIActions.streamExecutorFn ExecutorFactory`
  - `func (c *KubeCtlAPIActions) newStreamExecutor(method string, u *url.URL) (remotecommand.Executor, error)`
  - test helpers (package `kubernetes`, file `keepalive_free_test.go`): `type scriptedExecutor struct{ stdout, stderr string; err error }`, `func recordingFactory(called *bool, ex remotecommand.Executor) ExecutorFactory`, `func newRoutingTestActions(t *testing.T, spdyFn, streamFn ExecutorFactory) *KubeCtlAPIActions`

- [ ] **Step 1: Write the failing tests**

Create `cmd/root/kubernetes/keepalive_free_test.go`:

```go
//	Copyright 2023 Dremio Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kubernetes

import (
	"bytes"
	"context"
	"io"
	"net/url"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/shutdown"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// scriptedExecutor stands in for a remote command: it writes fixed stdout and
// stderr, then returns err.
type scriptedExecutor struct {
	stdout, stderr string
	err            error
}

func (s *scriptedExecutor) Stream(o remotecommand.StreamOptions) error {
	return s.StreamWithContext(context.Background(), o)
}

func (s *scriptedExecutor) StreamWithContext(_ context.Context, o remotecommand.StreamOptions) error {
	if o.Stdout != nil {
		_, _ = io.WriteString(o.Stdout, s.stdout)
	}
	if o.Stderr != nil {
		_, _ = io.WriteString(o.Stderr, s.stderr)
	}
	return s.err
}

// recordingFactory returns an ExecutorFactory that flags its use and hands out ex.
func recordingFactory(called *bool, ex remotecommand.Executor) ExecutorFactory {
	return func(_ *rest.Config, _ string, _ *url.URL) (remotecommand.Executor, error) {
		*called = true
		return ex, nil
	}
}

// newRoutingTestActions builds actions with both executor factories injected.
// The clientset only builds request URLs; nothing touches the network.
func newRoutingTestActions(t *testing.T, spdyFn, streamFn ExecutorFactory) *KubeCtlAPIActions {
	t.Helper()
	cfg := &rest.Config{Host: "https://fake"}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("NewForConfig: %v", err)
	}
	return &KubeCtlAPIActions{
		namespace:        "ns",
		client:           cs,
		config:           cfg,
		hook:             shutdown.NewHook(),
		pidHosts:         map[string]string{},
		containerCache:   map[string]string{"pod-0": "dremio-coordinator"},
		timeoutMinutes:   1,
		protocol:         "SPDY",
		spdyExecutorFn:   spdyFn,
		streamExecutorFn: streamFn,
	}
}

func TestNewKeepaliveFreeSPDYExecutor_Builds(t *testing.T) {
	cfg := &rest.Config{Host: "https://example.invalid", BearerToken: "token"}
	u, _ := url.Parse("https://example.invalid/api/v1/namespaces/ns/pods/p/exec")
	ex, err := newKeepaliveFreeSPDYExecutor(cfg, "POST", u)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ex == nil {
		t.Fatal("expected an executor")
	}
}

func TestNewKeepaliveFreeSPDYExecutor_BadTLSConfig(t *testing.T) {
	cfg := &rest.Config{
		Host:            "https://example.invalid",
		TLSClientConfig: rest.TLSClientConfig{CAFile: "does-not-exist.pem"},
	}
	u, _ := url.Parse("https://example.invalid/api/v1/namespaces/ns/pods/p/exec")
	if _, err := newKeepaliveFreeSPDYExecutor(cfg, "POST", u); err == nil {
		t.Fatal("expected an error for an unreadable CA file")
	}
}

func TestStreamFromHost_UsesKeepaliveFreeExecutor(t *testing.T) {
	var usedDefault, usedStream bool
	a := newRoutingTestActions(t,
		recordingFactory(&usedDefault, &scriptedExecutor{}),
		recordingFactory(&usedStream, &scriptedExecutor{stdout: "data"}))

	var buf bytes.Buffer
	if err := a.StreamFromHost("pod-0", "/opt/dremio/log/server.log", &buf, false); err != nil {
		t.Fatalf("StreamFromHost: %v", err)
	}
	if !usedStream || usedDefault {
		t.Fatalf("StreamFromHost must use the keepalive-free executor (stream=%v default=%v)", usedStream, usedDefault)
	}
	if buf.String() != "data" {
		t.Errorf("got %q, want %q", buf.String(), "data")
	}
}

func TestHostExecute_KeepsDefaultExecutor(t *testing.T) {
	var usedDefault, usedStream bool
	a := newRoutingTestActions(t,
		recordingFactory(&usedDefault, &scriptedExecutor{stdout: "hi\n"}),
		recordingFactory(&usedStream, &scriptedExecutor{}))

	out, err := a.HostExecute(false, "pod-0", "echo", "hi")
	if err != nil {
		t.Fatalf("HostExecute: %v", err)
	}
	if !usedDefault || usedStream {
		t.Fatalf("HostExecute must keep the default keepalive executor (default=%v stream=%v)", usedDefault, usedStream)
	}
	if out != "hi" {
		t.Errorf("got %q, want %q", out, "hi")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -short ./cmd/root/kubernetes/ -run "KeepaliveFree|UsesKeepaliveFree|KeepsDefault" -v`
Expected: FAIL to compile — `undefined: newKeepaliveFreeSPDYExecutor` and `unknown field streamExecutorFn`.

- [ ] **Step 3: Implement the executor factory**

Create `cmd/root/kubernetes/keepalive_free.go`:

```go
//	Copyright 2023 Dremio Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kubernetes

import (
	"net/http"
	"net/url"

	"k8s.io/apimachinery/pkg/util/httpstream/spdy"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// newKeepaliveFreeSPDYExecutor is client-go's spdy.RoundTripperFor +
// NewSPDYExecutor with the 5 s SPDY keepalive ping disabled; authentication,
// TLS and proxy handling are identical.
//
// When a remote command (cat, gzip -c) exits while its output is still draining
// to a slow client, the server side closes its socket. A later client PING that
// reaches the closed socket makes the Linux kernel reset the connection
// (TCPAbortOnData), discarding the queued stdout tail and the exit status, and
// client-go then reports success on truncated output (#339). A streaming exec is
// never idle, so it needs no keepalive; OS-level TCP keepalive stays on.
func newKeepaliveFreeSPDYExecutor(config *rest.Config, method string, u *url.URL) (remotecommand.Executor, error) {
	tlsConfig, err := rest.TLSConfigFor(config)
	if err != nil {
		return nil, err
	}
	proxy := http.ProxyFromEnvironment
	if config.Proxy != nil {
		proxy = config.Proxy
	}
	upgrader, err := spdy.NewRoundTripperWithConfig(spdy.RoundTripperConfig{
		TLS:        tlsConfig,
		Proxier:    proxy,
		PingPeriod: 0,
	})
	if err != nil {
		return nil, err
	}
	wrapper, err := rest.HTTPWrappersForConfig(config, upgrader)
	if err != nil {
		return nil, err
	}
	return remotecommand.NewSPDYExecutorForTransports(wrapper, upgrader, method, u)
}
```

- [ ] **Step 4: Wire it into `KubeCtlAPIActions`**

In `cmd/root/kubernetes/kubernetes.go`:

(a) In the `KubeCtlAPIActions` struct, add a field directly below `spdyExecutorFn      ExecutorFactory`:

```go
	streamExecutorFn    ExecutorFactory // keepalive-free, for streaming execs (#339)
```

(b) In `NewK8sAPI`, directly below the `spdyExecutorFn: func(...) {...},` literal, add:

```go
		streamExecutorFn: newKeepaliveFreeSPDYExecutor,
```

(c) Directly below the existing `newExecutor` method, add:

```go
// newStreamExecutor creates a keepalive-free SPDY executor for long streaming
// execs (file streams, queries-perf); see newKeepaliveFreeSPDYExecutor.
func (c *KubeCtlAPIActions) newStreamExecutor(method string, u *url.URL) (remotecommand.Executor, error) {
	return c.streamExecutorFn(c.config, method, u)
}
```

(d) In `StreamFromHost`, replace

```go
	executor, err := c.newExecutor("POST", req.URL())
	if err != nil {
		return fmt.Errorf("StreamFromHost: executor creation failed for %v:%v: %w", host, remotePath, err)
	}
```

with

```go
	executor, err := c.newStreamExecutor("POST", req.URL())
	if err != nil {
		return fmt.Errorf("StreamFromHost: executor creation failed for %v:%v: %w", host, remotePath, err)
	}
```

Then run `go fmt ./cmd/root/kubernetes/` (realigns the struct fields).

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test -short ./cmd/root/kubernetes/ -v -run "KeepaliveFree|UsesKeepaliveFree|KeepsDefault"`
Expected: PASS (4 tests).
Run: `go test -short ./cmd/root/kubernetes/`
Expected: `ok`.

- [ ] **Step 6: Build**

Run: `go build -o bin/ddc.exe .`
Expected: exit 0.

- [ ] **Step 7: Commit**

Run: `git add -A && git status --short` (confirm only this task's files are staged; `bin/` stays ignored), then
`git commit -m "wip(#339): task 1 — Keepalive-free SPDY executor for file streams"`.
Expected: one new commit on `fix/339-k8s-stream-truncation`.

---

### Task 2: Loopback regression test for #339 (real kubelet exec server)

**Files:**
- Create: `cmd/root/kubernetes/stream_loopback_test.go`
- Modify: `go.mod`, `go.sum` (test-only `k8s.io/kubelet v0.32.3`)

**Interfaces:**
- Consumes: `newKeepaliveFreeSPDYExecutor` (Task 1).
- Produces: test-only helpers `newExecLoopback`, `streamOnce`, `testPayload` (file-local).

- [ ] **Step 1: Write the regression tests**

Create `cmd/root/kubernetes/stream_loopback_test.go`:

```go
//	Copyright 2023 Dremio Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kubernetes

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/proxy"
	remotecommandconsts "k8s.io/apimachinery/pkg/util/remotecommand"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	kubeletrc "k8s.io/kubelet/pkg/cri/streaming/remotecommand"
)

// payloadExec is a kubelet exec backend that writes payload to stdout as fast
// as the connection accepts it and exits, like `gzip -1 -c file.gz`.
type payloadExec struct{ payload []byte }

func (p payloadExec) ExecInContainer(_ context.Context, _ string, _ types.UID, _ string, _ []string, _ io.Reader, out, _ io.WriteCloser, _ bool, _ <-chan remotecommand.TerminalSize, _ time.Duration) error {
	// 32 KB writes like the container runtime's io.Copy: realistic SPDY frame
	// sizes, so backpressure reaches the socket buffers. One huge write becomes a
	// single frame the client reads into memory at once, which hides the bug.
	const chunk = 32 << 10
	for off := 0; off < len(p.payload); off += chunk {
		if _, err := out.Write(p.payload[off:min(off+chunk, len(p.payload))]); err != nil {
			return err
		}
	}
	return nil
}

type proxyErrResponder struct{}

func (proxyErrResponder) Error(w http.ResponseWriter, _ *http.Request, err error) {
	http.Error(w, err.Error(), http.StatusBadGateway)
}

// newExecLoopback starts a TLS kubelet exec endpoint (real ServeExec) behind two
// apimachinery UpgradeAwareHandler hops — kubelet proxy and API server proxy —
// and returns the exec URL a client POSTs to plus the PEM of the certificate
// all httptest TLS servers share (it covers 127.0.0.1), so every hop verifies TLS.
func newExecLoopback(t *testing.T, payload []byte) (*url.URL, []byte) {
	t.Helper()
	kubelet := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		kubeletrc.ServeExec(w, r, payloadExec{payload}, "pod", "", "c", []string{"stream"},
			&kubeletrc.Options{Stdout: true, Stderr: true}, time.Hour, 30*time.Second,
			remotecommandconsts.SupportedStreamingProtocols)
	}))
	t.Cleanup(kubelet.Close)
	roots := x509.NewCertPool()
	roots.AddCert(kubelet.Certificate())
	target := kubelet.URL
	for i := 0; i < 2; i++ {
		u, _ := url.Parse(target + "/exec")
		h := proxy.NewUpgradeAwareHandler(u, &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
		}, false, true, proxyErrResponder{})
		h.UseLocationHost = true
		srv := httptest.NewTLSServer(h)
		t.Cleanup(srv.Close)
		target = srv.URL
	}
	u, _ := url.Parse(target + "/exec")
	return u, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: kubelet.Certificate().Raw})
}

// throttledBuffer drains at bytesPerSec, modelling a slow client link.
type throttledBuffer struct {
	buf         bytes.Buffer // not embedded: a promoted ReadFrom would let io.Copy bypass Write
	bytesPerSec float64
	start       time.Time
}

func (b *throttledBuffer) Write(p []byte) (int, error) {
	if b.start.IsZero() {
		b.start = time.Now()
	}
	n, err := b.buf.Write(p)
	want := time.Duration(float64(b.buf.Len()) / b.bytesPerSec * float64(time.Second))
	if d := want - time.Since(b.start); d > 0 {
		time.Sleep(d)
	}
	return n, err
}

func loopbackConfig(u *url.URL, caPEM []byte) *rest.Config {
	return &rest.Config{Host: u.Scheme + "://" + u.Host, TLSClientConfig: rest.TLSClientConfig{CAData: caPEM}}
}

// streamOnce runs one exec and reports whether the whole payload arrived intact.
func streamOnce(t *testing.T, ex remotecommand.Executor, payload []byte, bytesPerSec float64) (bool, error) {
	t.Helper()
	out := &throttledBuffer{bytesPerSec: bytesPerSec}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	err := ex.StreamWithContext(ctx, remotecommand.StreamOptions{Stdout: out, Stderr: io.Discard})
	return out.buf.Len() == len(payload) && sha256.Sum256(out.buf.Bytes()) == sha256.Sum256(payload), err
}

// testPayload returns n deterministic bytes.
func testPayload(n int) []byte {
	p := make([]byte, n)
	for i := range p {
		p[i] = byte(i * 7)
	}
	return p
}

func TestKeepaliveFreeExecutor_DeliversSlowStreamCompletely(t *testing.T) {
	payload := testPayload(4 << 20)
	u, caPEM := newExecLoopback(t, payload)
	ex, err := newKeepaliveFreeSPDYExecutor(loopbackConfig(u, caPEM), "POST", u)
	if err != nil {
		t.Fatalf("executor: %v", err)
	}
	if ok, err := streamOnce(t, ex, payload, 2<<20); err != nil || !ok {
		t.Fatalf("stream incomplete: ok=%v err=%v", ok, err)
	}
}

// TestKeepaliveTruncation_Regression339 first proves the environment reproduces
// the bug with client-go's default executor (5 s pings), then requires the
// keepalive-free executor to deliver every byte under the same conditions.
func TestKeepaliveTruncation_Regression339(t *testing.T) {
	if testing.Short() {
		t.Skip("slow (~60 s)")
	}
	if runtime.GOOS != "linux" {
		t.Skip("keepalive truncation needs Linux abort-on-data semantics on the server side")
	}
	// 8 MB at 500 KB/s (~16 s per transfer): the payload must exceed what the slow
	// client's receive buffer absorbs, so data is still queued server-side when a
	// ping lands. 4 MB at 400 KB/s did not reproduce in 3/3 trials.
	payload := testPayload(8 << 20)
	const slow = 500 << 10
	u, caPEM := newExecLoopback(t, payload)

	truncated := false
	for i := 0; i < 3 && !truncated; i++ {
		ex, err := remotecommand.NewSPDYExecutor(loopbackConfig(u, caPEM), "POST", u) // client-go default: 5 s pings
		if err != nil {
			t.Fatalf("default executor: %v", err)
		}
		ok, _ := streamOnce(t, ex, payload, slow)
		truncated = !ok
	}
	if !truncated {
		t.Skip("environment does not reproduce keepalive truncation; nothing to compare against")
	}

	for i := 1; i <= 3; i++ {
		ex, err := newKeepaliveFreeSPDYExecutor(loopbackConfig(u, caPEM), "POST", u)
		if err != nil {
			t.Fatalf("keepalive-free executor: %v", err)
		}
		if ok, err := streamOnce(t, ex, payload, slow); err != nil || !ok {
			t.Fatalf("trial %d: keepalive-free executor lost data (err=%v)", i, err)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test -short ./cmd/root/kubernetes/ -run DeliversSlowStreamCompletely`
Expected: FAIL — `no required module provides package k8s.io/kubelet/pkg/cri/streaming/remotecommand`.

- [ ] **Step 3: Add the pinned test-only dependency**

Run:
```bash
go get k8s.io/kubelet@v0.32.3
go mod tidy
git diff go.mod
```
Expected `git diff go.mod`: exactly two added lines — `k8s.io/kubelet v0.32.3` in the direct `require` block and `k8s.io/apiserver v0.32.3 // indirect`; the `go 1.24.13` directive and every existing version unchanged. If anything else changed, revert `go.mod`/`go.sum` and stop.

Verify the production binary's modules are unchanged:
```bash
go list -deps -f '{{with .Module}}{{.Path}} {{.Version}}{{end}}' . | sort -u > "$TMP/after.txt"
git stash push go.mod go.sum -q && go list -deps -f '{{with .Module}}{{.Path}} {{.Version}}{{end}}' . | sort -u > "$TMP/before.txt"; git stash pop -q
diff "$TMP/before.txt" "$TMP/after.txt" && echo UNCHANGED
```
Expected: `UNCHANGED`.

- [ ] **Step 4: Run the short test to verify it passes**

Run: `go test -short ./cmd/root/kubernetes/ -run DeliversSlowStreamCompletely -v`
Expected: PASS in ~2–3 s.

- [ ] **Step 5: Run the Linux regression pair**

On any Linux host (here: WSL Ubuntu-24.04; adjust the `/mnt/c/...` path to your checkout):
```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c -o bin/k8s-loopback.test ./cmd/root/kubernetes/
MSYS_NO_PATHCONV=1 wsl -d Ubuntu-24.04 -- bash -c 'cp /mnt/c/Users/chufe/Workspaces/golang/dremio-diagnostic-collector/bin/k8s-loopback.test /tmp/ && /tmp/k8s-loopback.test -test.run "Regression339|DeliversSlow" -test.v'
```
Expected: `--- PASS: TestKeepaliveTruncation_Regression339` (≈60 s) and `--- PASS: TestKeepaliveFreeExecutor_DeliversSlowStreamCompletely` (≈2 s). (Verified during planning on WSL kernel 6.18: 2/2 runs PASS, no SKIP.) A `SKIP` with "environment does not reproduce" means the kernel didn't reproduce the bug; report it instead of treating it as a pass. Delete `bin/k8s-loopback.test` afterwards.

- [ ] **Step 6: Build**

Run: `go build -o bin/ddc.exe .`
Expected: exit 0.

- [ ] **Step 7: Commit**

Run: `git add -A && git status --short` (confirm only this task's files are staged; `bin/` stays ignored), then
`git commit -m "wip(#339): task 2 — Loopback regression test for #339 (real kubelet exec server)"`.
Expected: one new commit on `fix/339-k8s-stream-truncation`.

---

### Task 3: Sentinel-checked keepalive-free command stream

**Files:**
- Modify: `cmd/root/collection/collector.go` (imports; add interface + error below the `Collector` interface)
- Modify: `cmd/root/kubernetes/keepalive_free.go`
- Test: `cmd/root/kubernetes/keepalive_free_test.go`

**Interfaces:**
- Consumes: `newStreamExecutor`, `newRoutingTestActions`, `scriptedExecutor` (Task 1); existing `K8SWriter`, `logArgs`, `getPrimaryContainer`.
- Produces:
  - `collection.KeepaliveFreeStreamer` with `HostExecuteAndStreamNoKeepalive(host string, output cli.OutputHandler, args ...string) error`
  - `collection.ErrStreamIncomplete` (message `remote stream ended before end-of-stream marker`)
  - `func (c *KubeCtlAPIActions) HostExecuteAndStreamNoKeepalive(hostString string, output cli.OutputHandler, args ...string) error`
  - `const eosMarker = "__DDC_EOS__:"`, `type eosLineWriter`, `func (w *eosLineWriter) finish() error`

- [ ] **Step 1: Write the failing tests**

In `cmd/root/kubernetes/keepalive_free_test.go`, replace the import block with:

```go
import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/collection"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/shutdown"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)
```

and append:

```go
// Compile-time check: the K8s API transport provides the keepalive-free stream.
var _ collection.KeepaliveFreeStreamer = (*KubeCtlAPIActions)(nil)

// runEOS feeds chunks through an eosLineWriter and returns the emitted lines
// and finish()'s verdict.
func runEOS(chunks ...string) ([]string, error) {
	var got []string
	w := &eosLineWriter{output: func(l string) { got = append(got, l) }}
	for _, c := range chunks {
		if _, err := w.Write([]byte(c)); err != nil {
			return got, err
		}
	}
	return got, w.finish()
}

func TestEOSLineWriter(t *testing.T) {
	tests := []struct {
		name       string
		chunks     []string
		want       []string
		incomplete bool
		errSubstr  string
	}{
		{name: "output ending in newline", chunks: []string{"a\nb\n", "\n__DDC_EOS__:0\n"}, want: []string{"a", "b"}},
		{name: "last line without newline", chunks: []string{"a\nb", "\n__DDC_EOS__:0\n"}, want: []string{"a", "b"}},
		{name: "empty output", chunks: []string{"\n__DDC_EOS__:0\n"}, want: nil},
		{name: "legit trailing empty line", chunks: []string{"a\n\n", "\n__DDC_EOS__:0\n"}, want: []string{"a", ""}},
		{name: "marker split across writes", chunks: []string{"a\n\n__DDC_E", "OS__:0\n"}, want: []string{"a"}},
		{name: "marker-like data line", chunks: []string{"__DDC_EOS__:x\n", "\n__DDC_EOS__:0\n"}, want: []string{"__DDC_EOS__:x"}},
		{name: "missing marker drops partial tail", chunks: []string{"a\nb\npar"}, want: []string{"a", "b"}, incomplete: true},
		{name: "missing marker after full line", chunks: []string{"a\nb\n"}, want: []string{"a", "b"}, incomplete: true},
		{name: "non-zero exit code", chunks: []string{"a\n", "\n__DDC_EOS__:3\n"}, want: []string{"a"}, errSubstr: "exited with code 3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := runEOS(tt.chunks...)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("lines = %q, want %q", got, tt.want)
			}
			switch {
			case tt.incomplete:
				if !errors.Is(err, collection.ErrStreamIncomplete) {
					t.Errorf("err = %v, want ErrStreamIncomplete", err)
				}
			case tt.errSubstr != "":
				if err == nil || !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("err = %v, want it to contain %q", err, tt.errSubstr)
				}
			default:
				if err != nil {
					t.Errorf("unexpected err: %v", err)
				}
			}
		})
	}
}

// failIfCalled fails the test if the default keepalive executor is used.
func failIfCalled(t *testing.T) ExecutorFactory {
	return func(_ *rest.Config, _ string, _ *url.URL) (remotecommand.Executor, error) {
		t.Error("default keepalive executor must not be used for keepalive-free streams")
		return &scriptedExecutor{}, nil
	}
}

func TestHostExecuteAndStreamNoKeepalive_CompleteStream(t *testing.T) {
	var gotURL *url.URL
	streamFn := func(_ *rest.Config, _ string, u *url.URL) (remotecommand.Executor, error) {
		gotURL = u
		return &scriptedExecutor{
			stdout: "{\"a\":1}\n{\"a\":2}\n\n__DDC_EOS__:0\n",
			stderr: "warning: slow disk\n",
		}, nil
	}
	a := newRoutingTestActions(t, failIfCalled(t), streamFn)

	var lines []string
	err := a.HostExecuteAndStreamNoKeepalive("pod-0", func(l string) { lines = append(lines, l) },
		"/tmp/dremio-rocksdb-viewer", "-type", "queries_perf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := []string{`{"a":1}`, `{"a":2}`}; !reflect.DeepEqual(lines, want) {
		t.Errorf("lines = %q, want %q (stderr and marker must not reach the handler)", lines, want)
	}
	wantCmd := []string{"sh", "-c", "/tmp/dremio-rocksdb-viewer -type queries_perf; printf '\\n__DDC_EOS__:%d\\n' $?"}
	if got := gotURL.Query()["command"]; !reflect.DeepEqual(got, wantCmd) {
		t.Errorf("command = %q, want %q", got, wantCmd)
	}
}

func TestHostExecuteAndStreamNoKeepalive_TruncatedStream(t *testing.T) {
	a := newRoutingTestActions(t, failIfCalled(t), func(_ *rest.Config, _ string, _ *url.URL) (remotecommand.Executor, error) {
		return &scriptedExecutor{stdout: "{\"a\":1}\n{\"a\":2"}, nil
	})
	var lines []string
	err := a.HostExecuteAndStreamNoKeepalive("pod-0", func(l string) { lines = append(lines, l) }, "viewer")
	if !errors.Is(err, collection.ErrStreamIncomplete) {
		t.Fatalf("err = %v, want ErrStreamIncomplete", err)
	}
	if want := []string{`{"a":1}`}; !reflect.DeepEqual(lines, want) {
		t.Errorf("lines = %q, want %q (partial record must be dropped)", lines, want)
	}
}

func TestHostExecuteAndStreamNoKeepalive_TransportError(t *testing.T) {
	a := newRoutingTestActions(t, failIfCalled(t), func(_ *rest.Config, _ string, _ *url.URL) (remotecommand.Executor, error) {
		return &scriptedExecutor{stdout: "x\n", err: errors.New("connection reset by peer")}, nil
	})
	var lines []string
	err := a.HostExecuteAndStreamNoKeepalive("pod-0", func(l string) { lines = append(lines, l) }, "viewer")
	if err == nil || !strings.Contains(err.Error(), "connection reset") {
		t.Fatalf("err = %v, want the transport error", err)
	}
	if want := []string{"x"}; !reflect.DeepEqual(lines, want) {
		t.Errorf("lines = %q, want %q (complete lines are kept)", lines, want)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test -short ./cmd/root/kubernetes/ -run "EOSLineWriter|NoKeepalive" -v`
Expected: FAIL to compile — `undefined: collection.KeepaliveFreeStreamer`, `undefined: eosLineWriter`, `a.HostExecuteAndStreamNoKeepalive undefined`.

- [ ] **Step 3: Add the interface and error to the collection package**

In `cmd/root/collection/collector.go`, add `"errors"` to the import block, and directly below the closing `}` of the `Collector` interface add:

```go
// KeepaliveFreeStreamer is implemented by transports that can run a long,
// continuously streaming command without keepalive pings and prove that it
// completed (#339). Only the Kubernetes API transport implements it.
type KeepaliveFreeStreamer interface {
	HostExecuteAndStreamNoKeepalive(host string, output cli.OutputHandler, args ...string) error
}

// ErrStreamIncomplete means a remote stream ended before its end-of-stream marker.
var ErrStreamIncomplete = errors.New("remote stream ended before end-of-stream marker")
```

- [ ] **Step 4: Implement the marker writer and the keepalive-free stream**

In `cmd/root/kubernetes/keepalive_free.go`, replace the import block with:

```go
import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/cli"
	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/collection"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/simplelog"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/httpstream/spdy"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/kubectl/pkg/scheme"
)
```

and append:

```go
// eosMarker prefixes the line HostExecuteAndStreamNoKeepalive appends after the
// command's output; the rest of that line is the command's exit code.
const eosMarker = "__DDC_EOS__:"

// HostExecuteAndStreamNoKeepalive runs args through `sh -c` over a
// keepalive-free exec and streams stdout lines to output. The command is
// followed by `printf '\n__DDC_EOS__:%d\n' $?`; the marker shares stdout with
// the command's output, so it only arrives if every byte before it did. A
// stream that ends without it returns collection.ErrStreamIncomplete; a
// non-zero exit code carried by it is returned as an error. stderr lines are
// logged, never passed to output.
func (c *KubeCtlAPIActions) HostExecuteAndStreamNoKeepalive(hostString string, output cli.OutputHandler, args ...string) error {
	logArgs(false, args)
	containerName, err := c.getPrimaryContainer(hostString)
	if err != nil {
		return fmt.Errorf("failed looking for pod %v: %w", hostString, err)
	}
	script := strings.Join(args, " ") + "; printf '\\n" + eosMarker + "%d\\n' $?"
	req := c.client.CoreV1().RESTClient().Post().Resource("pods").Name(hostString).
		Namespace(c.namespace).SubResource("exec")
	req = req.VersionedParams(&v1.PodExecOptions{
		Container: containerName,
		Command:   []string{"sh", "-c", script},
		Stdout:    true,
		Stderr:    true,
	}, scheme.ParameterCodec)
	executor, err := c.newStreamExecutor("POST", req.URL())
	if err != nil {
		return err
	}
	stdout := &eosLineWriter{output: output}
	stderr := &K8SWriter{Output: func(line string) {
		simplelog.Warningf("%v stderr: %v", hostString, line)
	}}
	err = executor.StreamWithContext(c.hook.GetContext(), remotecommand.StreamOptions{
		Stdout: stdout,
		Stderr: stderr,
	})
	stderr.Flush()
	complete := stdout.finish() // always runs: delivers the last complete line
	if err != nil {
		return err
	}
	return complete
}

// eosLineWriter splits a stream into lines for output like K8SWriter, but holds
// back one complete line so it can drop the empty line created by the marker's
// leading newline, strips the marker line, and records the exit code it carries.
type eosLineWriter struct {
	output  cli.OutputHandler
	partial strings.Builder
	held    *string
	sawEOS  bool
	rc      int
}

func (w *eosLineWriter) Write(p []byte) (int, error) {
	data := string(p)
	for {
		idx := strings.IndexByte(data, '\n')
		if idx < 0 {
			w.partial.WriteString(data)
			return len(p), nil
		}
		w.partial.WriteString(data[:idx])
		w.line(w.partial.String())
		w.partial.Reset()
		data = data[idx+1:]
	}
}

func (w *eosLineWriter) line(l string) {
	if w.sawEOS {
		return // nothing is expected after the marker
	}
	if rest, ok := strings.CutPrefix(l, eosMarker); ok {
		if rc, err := strconv.Atoi(rest); err == nil {
			w.sawEOS, w.rc = true, rc
			if w.held != nil && *w.held != "" {
				w.output(*w.held)
			}
			w.held = nil
			return
		}
	}
	if w.held != nil {
		w.output(*w.held)
	}
	w.held = &l
}

// finish must be called once after the stream ends. Without the marker it
// delivers the held-back line, drops any trailing partial line (it may be a
// cut-off record) and returns collection.ErrStreamIncomplete.
func (w *eosLineWriter) finish() error {
	if !w.sawEOS {
		if w.held != nil {
			w.output(*w.held)
			w.held = nil
		}
		w.partial.Reset()
		return collection.ErrStreamIncomplete
	}
	if w.rc != 0 {
		return fmt.Errorf("remote command exited with code %d", w.rc)
	}
	return nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test -short ./cmd/root/kubernetes/ -run "EOSLineWriter|NoKeepalive" -v`
Expected: PASS (9 sub-tests + 3 tests).
Run: `go test -short ./cmd/root/kubernetes/ ./cmd/root/collection/`
Expected: `ok` for both.

- [ ] **Step 6: Build**

Run: `go build -o bin/ddc.exe .`
Expected: exit 0.

- [ ] **Step 7: Commit**

Run: `git add -A && git status --short` (confirm only this task's files are staged; `bin/` stays ignored), then
`git commit -m "wip(#339): task 3 — Sentinel-checked keepalive-free command stream"`.
Expected: one new commit on `fix/339-k8s-stream-truncation`.

---

### Task 4: Rotation-safe completeness guard for file streams

**Files:**
- Modify: `cmd/root/collection/streaming_collect.go` (imports; below `const maxRetries = 3` at line 47; `time.Sleep(backoff)` in `streamFile` at ~206; `streamFileOnce` after the close at ~291-294; new func after `streamFileOnce`)
- Modify: `cmd/root/collection/jvmcollect.go` (`probeRemoteFileSize` at ~325-338)
- Test: `cmd/root/collection/stream_guard_test.go` (create); `cmd/root/collection/streaming_collect_test.go` (fixture sizes)

**Interfaces:**
- Consumes: existing `streamFile`, `streamFileOnce`, `mockStreamCollector` (test).
- Produces:
  - `var ErrStreamTruncated = errors.New("stream truncated")`
  - `var streamBackoffSleep = time.Sleep`
  - `func checkStreamComplete(c Collector, host, remotePath string, got, expected int64) error`
  - `func probeRemoteFileSizeOK(c Collector, host, remotePath string) (int64, bool)`
  - test helpers (file `stream_guard_test.go`): `func withNoStreamBackoff(t *testing.T)`, `func statCollector(size string, statErr error) *mockStreamCollector`

- [ ] **Step 1: Write the failing tests**

Create `cmd/root/collection/stream_guard_test.go`:

```go
// Copyright 2023 Dremio Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package collection

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// withNoStreamBackoff removes the stream retry backoff for one test.
func withNoStreamBackoff(t *testing.T) {
	t.Helper()
	orig := streamBackoffSleep
	streamBackoffSleep = func(time.Duration) {}
	t.Cleanup(func() { streamBackoffSleep = orig })
}

// statCollector answers `stat` probes with size/statErr and rejects anything else.
func statCollector(size string, statErr error) *mockStreamCollector {
	return &mockStreamCollector{
		hostExecuteFunc: func(_ bool, _ string, args ...string) (string, error) {
			if len(args) > 0 && args[0] == "stat" {
				return size, statErr
			}
			return "", fmt.Errorf("unexpected command: %v", args)
		},
	}
}

func TestCheckStreamComplete(t *testing.T) {
	tests := []struct {
		name          string
		got, expected int64
		statOut       string
		statErr       error
		wantTruncated bool
	}{
		{name: "size unknown", got: 5, expected: 0},
		{name: "exact", got: 10, expected: 10},
		{name: "file grew", got: 12, expected: 10},
		{name: "rotated smaller", got: 4, expected: 10, statOut: "4"},
		{name: "rotated to empty", got: 0, expected: 10, statOut: "0"},
		{name: "rotated but still short", got: 3, expected: 10, statOut: "4", wantTruncated: true},
		{name: "not shrunk", got: 5, expected: 10, statOut: "10", wantTruncated: true},
		{name: "probe fails", got: 5, expected: 10, statErr: errors.New("exit code 1"), wantTruncated: true},
		{name: "probe unparsable", got: 5, expected: 10, statOut: "garbage", wantTruncated: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkStreamComplete(statCollector(tt.statOut, tt.statErr), "h", "/r/f.log", tt.got, tt.expected)
			if tt.wantTruncated != errors.Is(err, ErrStreamTruncated) {
				t.Fatalf("err = %v, wantTruncated = %v", err, tt.wantTruncated)
			}
			if !tt.wantTruncated && err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
		})
	}
}

func TestStreamFile_RetriesTruncatedStream(t *testing.T) {
	withNoStreamBackoff(t)
	content := []byte("0123456789")
	calls := 0
	mc := statCollector("10", nil)
	mc.streamFunc = func(_, _ string, w io.Writer) error {
		calls++
		if calls == 1 {
			_, err := w.Write(content[:4])
			return err
		}
		_, err := w.Write(content)
		return err
	}
	dest := filepath.Join(t.TempDir(), "f.log")
	n, hashCh, err := streamFile(mc, "h", "/r/f.log", dest, maxRetries, int64(len(content)), "f.log", "", false)
	if err != nil {
		t.Fatalf("streamFile: %v", err)
	}
	<-hashCh
	if n != int64(len(content)) || calls != 2 {
		t.Fatalf("n=%d calls=%d, want n=%d calls=2", n, calls, len(content))
	}
	if data, _ := os.ReadFile(dest); string(data) != string(content) {
		t.Errorf("file = %q, want %q", data, content)
	}
}

func TestStreamFile_PersistentTruncationSkips(t *testing.T) {
	withNoStreamBackoff(t)
	calls := 0
	mc := statCollector("10", nil)
	mc.streamFunc = func(_, _ string, w io.Writer) error {
		calls++
		_, err := w.Write([]byte("0123"))
		return err
	}
	dest := filepath.Join(t.TempDir(), "f.log")
	_, _, err := streamFile(mc, "h", "/r/f.log", dest, maxRetries, 10, "f.log", "", false)
	if !errors.Is(err, ErrStreamTruncated) {
		t.Fatalf("err = %v, want ErrStreamTruncated", err)
	}
	if calls != maxRetries {
		t.Errorf("calls = %d, want %d", calls, maxRetries)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Error("truncated file must be removed")
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test -short ./cmd/root/collection/ -run "CheckStreamComplete|StreamFile_" -v`
Expected: FAIL to compile — `undefined: streamBackoffSleep`, `undefined: checkStreamComplete`, `undefined: ErrStreamTruncated`.

- [ ] **Step 3: Implement the guard**

In `cmd/root/collection/streaming_collect.go`:

(a) Add `"errors"` to the import block.

(b) Directly below `const maxRetries = 3`, add:

```go
// streamBackoffSleep waits between stream retries; tests replace it to stay fast.
var streamBackoffSleep = time.Sleep

// ErrStreamTruncated means a stream that reported success delivered fewer bytes
// than the remote file holds (#339).
var ErrStreamTruncated = errors.New("stream truncated")
```

(c) In `streamFile`, replace `time.Sleep(backoff)` with `streamBackoffSleep(backoff)`.

(d) In `streamFileOnce`, directly after

```go
	closeErr := f.Close()
	if closeErr != nil {
		return 0, nil, fmt.Errorf("close failed for %v: %w", destPath, closeErr)
	}
```

insert

```go
	if err := checkStreamComplete(c, host, remotePath, pw.n, expectedSize); err != nil {
		_ = os.Remove(destPath)
		return 0, nil, err
	}
```

(e) Directly after the closing `}` of `streamFileOnce`, add:

```go
// checkStreamComplete verifies that a stream reported as successful delivered
// the whole remote file. Active files may have grown (got > expected); a file
// rotated between discovery and streaming may have shrunk, which a fresh stat
// confirms. Anything else — including a failed probe — is a truncated transfer.
func checkStreamComplete(c Collector, host, remotePath string, got, expected int64) error {
	if expected <= 0 || got >= expected {
		return nil
	}
	if cur, ok := probeRemoteFileSizeOK(c, host, remotePath); ok && cur < expected && got >= cur {
		simplelog.Infof("stream: %v:%v shrank %d→%d (rotated), accepted", host, remotePath, expected, cur)
		return nil
	}
	return fmt.Errorf("%w for %v:%v: got %d of %d bytes", ErrStreamTruncated, host, remotePath, got, expected)
}
```

In `cmd/root/collection/jvmcollect.go`, replace the body of `probeRemoteFileSize` and add the `OK` variant below it (keep the existing doc comment of `probeRemoteFileSize`):

```go
func probeRemoteFileSize(c Collector, host, remotePath string) int64 {
	n, _ := probeRemoteFileSizeOK(c, host, remotePath)
	return n
}

// probeRemoteFileSizeOK is probeRemoteFileSize but reports whether the probe
// succeeded, so an empty file (0, true) is distinguishable from a failed probe
// (0, false).
func probeRemoteFileSizeOK(c Collector, host, remotePath string) (int64, bool) {
	out, err := c.HostExecute(false, host, "stat", "-c", "%s", remotePath)
	if err != nil {
		out, err = c.HostExecute(false, host, "stat", "-f", "%z", remotePath)
	}
	if err != nil {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}
```

- [ ] **Step 4: Run the guard tests to verify they pass**

Run: `go test -short ./cmd/root/collection/ -run "CheckStreamComplete|StreamFile_|ProbeRemoteFileSize" -v`
Expected: PASS.

- [ ] **Step 5: Run the package and observe fixtures the guard now rejects**

Run: `go test -short ./cmd/root/collection/`
Expected: FAIL in the tests listed in Step 6 — their mocks stream fewer bytes than the fixture's declared `Size`, which the guard correctly treats as truncation.

- [ ] **Step 6: Align fixture sizes with the streamed content**

In `cmd/root/collection/streaming_collect_test.go`, change only the `Size:` value at these lines (the new size is ≤ the bytes the test's `streamFunc` writes, so the guard sees a complete or grown file):

| Test | Line | Path | Old | New | Streamed content |
|---|---|---|---|---|---|
| `TestStreamingCollect_EndToEnd` | 128 | `server.log` | `100` | `20` | `content-of-server.log-on-<host>` |
| `TestStreamingCollect_EndToEnd` | 129 | `dremio.conf` | `50` | `20` | `content-of-dremio.conf-on-<host>` |
| `TestStreamingCollect_EndToEnd` | 130 | `gc.log.0` | `30` | `20` | `content-of-gc.log.0-on-<host>` |
| `TestStreamingCollect_RetryOnTransientError` | 200 | `server.log` | `100` | `12` | `success-data` |
| `TestStreamingCollect_SkipOnPermissionDenied` | 256 | `server.log` | `200` | `2` | `ok` |
| `TestStreamingCollect_SkipNodeOnDiscoveryFailure` | 309 | `server.log` | `100` | `4` | `data` |
| `TestStreamingCollect_RocksDBViewer_UsesAutodetectedDir` | 360 | `server.log` | `10` | `4` | `stub` |
| `TestStreamingCollect_RocksDBViewer_SkippedWhenNoDir` | 427 | `server.log` | `10` | `4` | `stub` |
| `TestStreamingCollect_AllFilesFailMeansNodeFailed` | 503 | `server.log` | `100` | `2` | `ok` |
| JVM test collector helper (above `TestStreamingCollect_JVMCollection_DiagnosisMode`) | 1183 | `server.log` | `10` | `8` | `log-data` |

For the three `TestStreamingCollect_EndToEnd` lines, add a trailing comment on line 128: `// ≤ streamed length; the completeness guard accepts growth`.

- [ ] **Step 7: Run the package to verify it passes**

Run: `go test -short ./cmd/root/collection/`
Expected: `ok`. If another test fails with `stream truncated`, apply the same rule (declared `Size` ≤ bytes its mock streams) and note it in the review.

- [ ] **Step 8: Build**

Run: `go build -o bin/ddc.exe .`
Expected: exit 0.

- [ ] **Step 9: Commit**

Run: `git add -A && git status --short` (confirm only this task's files are staged; `bin/` stays ignored), then
`git commit -m "wip(#339): task 4 — Rotation-safe completeness guard for file streams"`.
Expected: one new commit on `fix/339-k8s-stream-truncation`.

---

### Task 5: JVM artifacts use the retrying stream path

**Files:**
- Modify: `cmd/root/collection/jvmcollect.go` (`streamRemoteFile` at ~340-360)
- Test: `cmd/root/collection/jvmcollect_test.go`

**Interfaces:**
- Consumes: `streamFile`, `ErrStreamTruncated`, `withNoStreamBackoff` (Task 4); `mockJVMCollector` (existing test mock).
- Produces: `streamRemoteFile(c Collector, host, remotePath, localPath string) error` (signature unchanged).

- [ ] **Step 1: Write the failing tests**

Append to `cmd/root/collection/jvmcollect_test.go` (its imports already cover `errors`, `io`, `os`, `path/filepath`):

```go
func TestStreamRemoteFile_RetriesTruncatedArtifact(t *testing.T) {
	withNoStreamBackoff(t)
	calls := 0
	mock := &mockJVMCollector{
		hostExecuteFn: func(_ bool, _ string, args ...string) (string, error) {
			if len(args) > 0 && args[0] == "stat" {
				return "10", nil
			}
			return "", nil
		},
		streamFromHostFn: func(_ string, _ string, w io.Writer) error {
			calls++
			if calls == 1 {
				_, err := w.Write([]byte("0123"))
				return err
			}
			_, err := w.Write([]byte("0123456789"))
			return err
		},
	}
	local := filepath.Join(t.TempDir(), "recording.jfr")
	if err := streamRemoteFile(mock, "node-1", "/tmp/recording.jfr", local); err != nil {
		t.Fatalf("streamRemoteFile: %v", err)
	}
	if data, _ := os.ReadFile(local); string(data) != "0123456789" || calls != 2 {
		t.Fatalf("file=%q calls=%d, want the full artifact after 2 calls", data, calls)
	}
}

func TestStreamRemoteFile_PersistentTruncationRemovesFile(t *testing.T) {
	withNoStreamBackoff(t)
	mock := &mockJVMCollector{
		hostExecuteFn: func(_ bool, _ string, args ...string) (string, error) {
			if len(args) > 0 && args[0] == "stat" {
				return "10", nil
			}
			return "", nil
		},
		streamFromHostFn: func(_ string, _ string, w io.Writer) error {
			_, err := w.Write([]byte("0123"))
			return err
		},
	}
	local := filepath.Join(t.TempDir(), "heap.hprof")
	err := streamRemoteFile(mock, "node-1", "/tmp/heap.hprof", local)
	if !errors.Is(err, ErrStreamTruncated) {
		t.Fatalf("err = %v, want ErrStreamTruncated", err)
	}
	if _, statErr := os.Stat(local); !os.IsNotExist(statErr) {
		t.Error("a truncated artifact must not be kept")
	}
}
```

In the same file add `withNoStreamBackoff(t)` as the first statement of `TestCollectJFR_StreamFailure` (line ~374) and of `TestCollectAsyncProfiler_StreamFailure` (line ~601): they now retry 3 times and must not wait 3.5 s of real backoff.

- [ ] **Step 2: Run to verify the new tests fail**

Run: `go test -short ./cmd/root/collection/ -run "StreamRemoteFile_" -v`
Expected: FAIL — `RetriesTruncatedArtifact` gets `file="0123" calls=1`; `PersistentTruncationRemovesFile` gets `err = <nil>`.

- [ ] **Step 3: Delegate to `streamFile`**

In `cmd/root/collection/jvmcollect.go`, replace the whole `streamRemoteFile` function (doc comment included) with:

```go
// streamRemoteFile streams a remote JVM artifact (JFR, heap dump,
// async-profiler output) to localPath through streamFile, so a truncated
// transfer is retried (up to maxRetries) before callers delete the remote
// temp file (#339). Progress is reported to the TUI by streamFile.
func streamRemoteFile(c Collector, host, remotePath, localPath string) error {
	expectedSize := probeRemoteFileSize(c, host, remotePath)
	_, hashCh, err := streamFile(c, host, remotePath, localPath, maxRetries, expectedSize, filepath.Base(localPath), "", false)
	if err != nil {
		_ = os.Remove(localPath)
		return fmt.Errorf("StreamFromHost %s: %w", remotePath, err)
	}
	<-hashCh
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -short ./cmd/root/collection/ -run "StreamRemoteFile_|CollectJFR|CollectHeapDump|CollectAsyncProfiler" -v`
Expected: PASS, including `TestCollectHeapDump_HappyPath`, which asserts exactly 3 HostExecute calls (jmap, stat, rm).
Run: `go test -short ./cmd/root/collection/`
Expected: `ok`.

- [ ] **Step 5: Build**

Run: `go build -o bin/ddc.exe .`
Expected: exit 0.

- [ ] **Step 6: Commit**

Run: `git add -A && git status --short` (confirm only this task's files are staged; `bin/` stays ignored), then
`git commit -m "wip(#339): task 5 — JVM artifacts use the retrying stream path"`.
Expected: one new commit on `fix/339-k8s-stream-truncation`.

---

### Task 6: queries-perf INCOMPLETE flow

**Files:**
- Modify: `cmd/root/collection/rockscollect.go` (imports; new type; `RunRocksDBCollection` at 142-269; `collectQueriesPerf` from `dataCmd :=` at ~396 to the end)
- Modify: `cmd/root/collection/streaming_collect.go` (call site at ~1002)
- Test: `cmd/root/collection/rockscollect_test.go`

**Interfaces:**
- Consumes: `KeepaliveFreeStreamer`, `ErrStreamIncomplete` (Task 3); `mockStreamCollector`, `mockCopyStrategy`, `countLines` (existing tests).
- Produces:
  - `type IncompleteCollectionError struct { Item string; Records int; Cause error }` with `Error() string` = `"<item> incomplete after <n> records (<cause>)"` and `Unwrap() error`
  - `func RunRocksDBCollection(args RocksCollectArgs) ([]helpers.CollectedFile, []*IncompleteCollectionError, error)`

- [ ] **Step 1: Write the failing tests**

In `cmd/root/collection/rockscollect_test.go`, replace the import block with:

```go
import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/cli"
	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/helpers"
)
```

(`sync` stays: an existing test uses `sync.Mutex`), and append:

```go
// perfLine is one queries_perf record dated 2026-04-16 (UTC).
func perfLine(i int) string {
	return fmt.Sprintf(`{"query_id":"q%d","query_start_epoch_ms":1776352188426}`, i)
}

// queriesPerfCollector streams fixed lines through HostExecuteAndStream, the
// path used by transports without KeepaliveFreeStreamer (SSH, local, kubectl).
// mockStreamCollector holds an atomic.Bool, so it is embedded by pointer.
type queriesPerfCollector struct {
	*mockStreamCollector
	lines     []string
	streamErr error
	calls     int
}

func (q *queriesPerfCollector) HostExecuteAndStream(_ bool, _ string, out cli.OutputHandler, _ string, _ ...string) error {
	q.calls++
	for _, l := range q.lines {
		out(l)
	}
	return q.streamErr
}

// keepaliveFreeCollector also implements KeepaliveFreeStreamer, like the
// Kubernetes API transport.
type keepaliveFreeCollector struct {
	*queriesPerfCollector
	noKeepaliveCalls int
}

func (k *keepaliveFreeCollector) HostExecuteAndStreamNoKeepalive(_ string, out cli.OutputHandler, _ ...string) error {
	k.noKeepaliveCalls++
	for _, l := range k.lines {
		out(l)
	}
	return k.streamErr
}

// newQueriesPerfCollector answers the pre-flight -count with len(lines).
func newQueriesPerfCollector(lines []string, streamErr error) *queriesPerfCollector {
	return &queriesPerfCollector{
		mockStreamCollector: &mockStreamCollector{
			hostExecuteFunc: func(_ bool, _ string, args ...string) (string, error) {
				if strings.Contains(strings.Join(args, " "), "-count") {
					return strconv.Itoa(len(lines)), nil
				}
				return "", fmt.Errorf("unexpected host command: %v", args)
			},
		},
		lines:     lines,
		streamErr: streamErr,
	}
}

// runQueriesPerf calls collectQueriesPerf and returns its results plus the
// copy-strategy root directory.
func runQueriesPerf(t *testing.T, c Collector) ([]helpers.CollectedFile, string, error) {
	t.Helper()
	dir := t.TempDir()
	files, err := collectQueriesPerf(c, &mockCopyStrategy{tmpDir: dir}, "dremio-master-0", "coordinator",
		"/opt/dremio/data/db/catalog", RocksCollectArgs{QueriesPerfDays: 7})
	return files, dir, err
}

func TestCollectQueriesPerf_UsesKeepaliveFreeStreamer(t *testing.T) {
	k := &keepaliveFreeCollector{queriesPerfCollector: newQueriesPerfCollector([]string{perfLine(1), perfLine(2)}, nil)}
	files, _, err := runQueriesPerf(t, k)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if k.noKeepaliveCalls != 1 || k.calls != 0 {
		t.Fatalf("noKeepaliveCalls=%d fallbackCalls=%d, want 1 and 0", k.noKeepaliveCalls, k.calls)
	}
	if len(files) != 1 {
		t.Fatalf("files = %d, want 1", len(files))
	}
}

func TestCollectQueriesPerf_IncompleteKeepsRecords(t *testing.T) {
	k := &keepaliveFreeCollector{queriesPerfCollector: newQueriesPerfCollector([]string{perfLine(1), perfLine(2)}, ErrStreamIncomplete)}
	files, dir, err := runQueriesPerf(t, k)
	var inc *IncompleteCollectionError
	if !errors.As(err, &inc) {
		t.Fatalf("err = %v, want *IncompleteCollectionError", err)
	}
	if inc.Item != "queries-perf" || inc.Records != 2 || !errors.Is(err, ErrStreamIncomplete) {
		t.Errorf("got %+v, want queries-perf / 2 records / ErrStreamIncomplete", inc)
	}
	want := "queries-perf incomplete after 2 records (remote stream ended before end-of-stream marker)"
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
	if len(files) != 1 {
		t.Fatalf("files = %d, want 1 (partial data kept)", len(files))
	}
	data, readErr := os.ReadFile(filepath.Join(dir, "queries-perf", "dremio-master-0", "queries-perf.2026-04-16.json"))
	if readErr != nil || countLines(data) != 2 {
		t.Errorf("kept file: err=%v lines=%d, want 2 lines", readErr, countLines(data))
	}
}

func TestCollectQueriesPerf_ErrorBeforeFirstRecordFails(t *testing.T) {
	k := &keepaliveFreeCollector{queriesPerfCollector: newQueriesPerfCollector(nil, errors.New("exec failed"))}
	files, _, err := runQueriesPerf(t, k)
	var inc *IncompleteCollectionError
	if err == nil || errors.As(err, &inc) {
		t.Fatalf("err = %v, want a plain failure", err)
	}
	if files != nil {
		t.Errorf("files = %v, want nil", files)
	}
}

func TestCollectQueriesPerf_FallbackTransportIncomplete(t *testing.T) {
	q := newQueriesPerfCollector([]string{perfLine(1), perfLine(2)}, errors.New("ssh: connection lost"))
	files, _, err := runQueriesPerf(t, q)
	var inc *IncompleteCollectionError
	if !errors.As(err, &inc) || inc.Records != 2 {
		t.Fatalf("err = %v, want *IncompleteCollectionError with 2 records", err)
	}
	if q.calls != 1 || len(files) != 1 {
		t.Fatalf("fallbackCalls=%d files=%d, want 1 and 1", q.calls, len(files))
	}
}

func TestRunRocksDBCollection_ReportsIncompleteQueriesPerf(t *testing.T) {
	q := newQueriesPerfCollector([]string{perfLine(1), perfLine(2)}, ErrStreamIncomplete)
	q.hostExecuteFunc = func(_ bool, _ string, args ...string) (string, error) {
		cmd := strings.Join(args, " ")
		switch {
		case strings.HasPrefix(cmd, "test -f") && strings.Contains(cmd, "/catalog/CURRENT"):
			return "exists", nil
		case strings.Contains(cmd, "uname -m"):
			return "x86_64\n", nil
		case strings.Contains(cmd, "chmod +x"), strings.Contains(cmd, "rm -f"):
			return "", nil
		case strings.Contains(cmd, "-type cluster_stats"):
			return `{"cluster":"stub"}`, nil
		case strings.Contains(cmd, "-count"):
			return "2", nil
		}
		return "", fmt.Errorf("unexpected host command: %s", cmd)
	}
	q.copyToHostFunc = func(_, _, _ string) (string, error) { return "", nil }
	k := &keepaliveFreeCollector{queriesPerfCollector: q}

	files, incomplete, err := RunRocksDBCollection(RocksCollectArgs{
		Collector:          k,
		CopyStrategy:       &mockCopyStrategy{tmpDir: t.TempDir()},
		Host:               "dremio-master-0",
		NodeType:           "coordinator",
		RocksDBDir:         "/opt/dremio/data/db",
		CollectQueriesPerf: true,
		QueriesPerfDays:    7,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(incomplete) != 1 || incomplete[0].Item != "queries-perf" || incomplete[0].Records != 2 {
		t.Fatalf("incomplete = %+v, want one queries-perf item with 2 records", incomplete)
	}
	if len(files) != 2 {
		t.Errorf("files = %d, want 2 (cluster-stats + partial queries-perf)", len(files))
	}
}
```

Also update the four existing call sites in this file:
- line ~153: `files, err := RunRocksDBCollection(args)` → `files, _, err := RunRocksDBCollection(args)`
- line ~192: `_, err := RunRocksDBCollection(args)` → `_, _, err := RunRocksDBCollection(args)`
- line ~259: `got, err := RunRocksDBCollection(args)` → `got, _, err := RunRocksDBCollection(args)`
- line ~366: `got, err := RunRocksDBCollection(args)` → `got, _, err := RunRocksDBCollection(args)`

- [ ] **Step 2: Run to verify they fail**

Run: `go test -short ./cmd/root/collection/ -run "QueriesPerf" -v`
Expected: FAIL to compile — `undefined: IncompleteCollectionError` and `assignment mismatch: 3 variables but RunRocksDBCollection returns 2 values`.

- [ ] **Step 3: Implement the error type and the flow**

In `cmd/root/collection/rockscollect.go`:

(a) Add `"errors"` to the import block.

(b) Directly below `const rocksdbViewerRemotePath = "/tmp/dremio-rocksdb-viewer"`, add:

```go
// IncompleteCollectionError reports a collection that returned usable but
// partial data (e.g. a queries-perf stream cut short). Callers keep the data
// and flag the item instead of treating it as a failure (#339).
type IncompleteCollectionError struct {
	Item    string // e.g. "queries-perf"
	Records int
	Cause   error
}

func (e *IncompleteCollectionError) Error() string {
	return fmt.Sprintf("%s incomplete after %d records (%v)", e.Item, e.Records, e.Cause)
}

func (e *IncompleteCollectionError) Unwrap() error { return e.Cause }
```

(c) Change the signature to

```go
func RunRocksDBCollection(args RocksCollectArgs) ([]helpers.CollectedFile, []*IncompleteCollectionError, error) {
```

and inside it change every early return: `return nil, nil` (catalog skip) → `return nil, nil, nil`; each `return nil, fmt.Errorf(...)` (uname, binary, temp dir, write local binary, upload, chmod) → `return nil, nil, fmt.Errorf(...)`.

(d) Replace the queries-perf block and the final return:

```go
	// Collect queries-perf
	if args.CollectQueriesPerf {
		consoleprint.UpdateNodeState(consoleprint.NodeState{
			Node:     host,
			StatusUX: "Collecting queries-perf from RocksDB",
		})
		if files, err := collectQueriesPerf(c, args.CopyStrategy, host, args.NodeType, dbPath, args); err != nil {
			simplelog.Errorf("rocksdb queries_perf on %s: %v", host, err)
		} else {
			collected = append(collected, files...)
		}
	}

	return collected, nil
```

with

```go
	// Collect queries-perf
	var incomplete []*IncompleteCollectionError
	if args.CollectQueriesPerf {
		consoleprint.UpdateNodeState(consoleprint.NodeState{
			Node:     host,
			StatusUX: "Collecting queries-perf from RocksDB",
		})
		files, err := collectQueriesPerf(c, args.CopyStrategy, host, args.NodeType, dbPath, args)
		var inc *IncompleteCollectionError
		switch {
		case errors.As(err, &inc):
			simplelog.Warningf("rocksdb queries_perf on %s: %v", host, err)
			collected = append(collected, files...)
			incomplete = append(incomplete, inc)
		case err != nil:
			simplelog.Errorf("rocksdb queries_perf on %s: %v", host, err)
		default:
			collected = append(collected, files...)
		}
	}

	return collected, incomplete, nil
```

(e) In `collectQueriesPerf`, replace

```go
	dataCmd := fmt.Sprintf("%s -db %s -type queries_perf%s", rocksdbViewerRemotePath, dbPath, filterArgs)
	if err := c.HostExecuteAndStream(false, host, cli.OutputHandler(handler), "", dataCmd); err != nil {
		return nil, fmt.Errorf("execute rocksdb-viewer queries_perf: %w", err)
	}
	if writeErr != nil {
		return nil, writeErr
	}
```

with

```go
	dataCmd := fmt.Sprintf("%s -db %s -type queries_perf%s", rocksdbViewerRemotePath, dbPath, filterArgs)
	var streamErr error
	if kf, ok := c.(KeepaliveFreeStreamer); ok {
		// Keepalive-free and end-of-stream-checked on Kubernetes (#339).
		streamErr = kf.HostExecuteAndStreamNoKeepalive(host, cli.OutputHandler(handler), dataCmd)
	} else {
		streamErr = c.HostExecuteAndStream(false, host, cli.OutputHandler(handler), "", dataCmd)
	}
	mu.Lock()
	streamed := lineCount
	mu.Unlock()
	simplelog.Infof("rocksdb-viewer queries_perf on %s: streamed %d records (pre-flight count %d)", host, streamed, totalRecords)
	if streamErr != nil && streamed == 0 {
		return nil, fmt.Errorf("execute rocksdb-viewer queries_perf: %w", streamErr)
	}
	if writeErr != nil {
		return nil, writeErr
	}
```

and at the end of `collectQueriesPerf` replace

```go
	simplelog.Infof("rocksdb-viewer: collected queries_perf -> %s (%d files, %d records)", destDir, len(dw.dates), lineCount)
	return collected, nil
```

with

```go
	if streamErr != nil {
		return collected, &IncompleteCollectionError{Item: "queries-perf", Records: streamed, Cause: streamErr}
	}
	simplelog.Infof("rocksdb-viewer: collected queries_perf -> %s (%d files, %d records)", destDir, len(dw.dates), lineCount)
	return collected, nil
```

In `cmd/root/collection/streaming_collect.go` (~1002) change `if rocksFiles, err := RunRocksDBCollection(rocksArgs); err != nil {` to `if rocksFiles, _, err := RunRocksDBCollection(rocksArgs); err != nil {` (Task 7 consumes the middle value).

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -short ./cmd/root/collection/ -run "QueriesPerf|RunRocksDB" -v`
Expected: PASS.
Run: `go test -short ./cmd/root/collection/`
Expected: `ok`.

- [ ] **Step 5: Build**

Run: `go build -o bin/ddc.exe .`
Expected: exit 0.

- [ ] **Step 6: Commit**

Run: `git add -A && git status --short` (confirm only this task's files are staged; `bin/` stays ignored), then
`git commit -m "wip(#339): task 6 — queries-perf INCOMPLETE flow"`.
Expected: one new commit on `fix/339-k8s-stream-truncation`.

---

### Task 7: Reporting — success rate, INCOMPLETE items, host-qualified skips

**Files:**
- Modify: `cmd/root/collection/collector.go` (`logDistributedCollectionSummary` at 250-318; new `successRate`)
- Modify: `cmd/root/collection/summary.go` (`SummaryInfo` at 26-42)
- Modify: `cmd/root/collection/streaming_collect.go` (vars at ~776; node streaming at ~970-1057; summary at ~1141 and ~1162)
- Test: `cmd/root/collection/collector_test.go`

**Interfaces:**
- Consumes: `IncompleteCollectionError`, 3-value `RunRocksDBCollection` (Task 6); existing `humanizeBytes`.
- Produces:
  - `func successRate(collected, failed, skipped int) (float64, int)`
  - `func nodeDoneStatus(collected int, bytes int64, skipped []string, incomplete []*IncompleteCollectionError) string`
  - `SummaryInfo.IncompleteCollections []string` (`json:"incompleteCollections,omitempty"`)
  - `logDistributedCollectionSummary(collectionMode collects.CollectionMode, coordinators, executors []string, files []helpers.CollectedFile, totalFailedFiles, totalFailedNodes, totalSkippedFiles, incomplete []string, nodesConnectedTo int, duration time.Duration)`

- [ ] **Step 1: Write the failing tests**

In `cmd/root/collection/collector_test.go`, replace the single-line `import "testing"` with:

```go
import (
	"math"
	"strings"
	"testing"
)
```

and append:

```go
func TestSuccessRate(t *testing.T) {
	tests := []struct {
		collected, failed, skipped int
		wantRate                   float64
		wantAttempts               int
	}{
		{collected: 125, failed: 0, skipped: 27, wantRate: 82.23684210526315, wantAttempts: 152},
		{collected: 10, failed: 2, skipped: 0, wantRate: 83.33333333333333, wantAttempts: 12},
		{collected: 0, failed: 0, skipped: 0, wantRate: 0, wantAttempts: 0},
	}
	for _, tt := range tests {
		rate, attempts := successRate(tt.collected, tt.failed, tt.skipped)
		if attempts != tt.wantAttempts || math.Abs(rate-tt.wantRate) > 1e-9 {
			t.Errorf("successRate(%d,%d,%d) = %v,%d want %v,%d", tt.collected, tt.failed, tt.skipped, rate, attempts, tt.wantRate, tt.wantAttempts)
		}
	}
}

func TestNodeDoneStatus(t *testing.T) {
	inc := []*IncompleteCollectionError{{Item: "queries-perf", Records: 187442, Cause: ErrStreamIncomplete}}
	tests := []struct {
		name       string
		skipped    []string
		incomplete []*IncompleteCollectionError
		want       string
	}{
		{name: "clean", want: "Done: 3 files (1.0KB), 0 skipped"},
		{name: "skipped", skipped: []string{"/opt/dremio/log/archive/queries.2026-08-23.0.json.gz"},
			want: "Done: 3 files (1.0KB), 1 skipped (queries.2026-08-23.0.json.gz)"},
		{name: "incomplete", incomplete: inc,
			want: "Done: 3 files (1.0KB), 0 skipped, INCOMPLETE: queries-perf (187442 records)"},
		{name: "skipped and incomplete", skipped: []string{"/opt/dremio/log/server.log"}, incomplete: inc,
			want: "Done: 3 files (1.0KB), 1 skipped (server.log), INCOMPLETE: queries-perf (187442 records)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nodeDoneStatus(3, 1024, tt.skipped, tt.incomplete); got != tt.want {
				t.Errorf("got %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestSummaryInfo_IncompleteCollectionsOmittedWhenEmpty(t *testing.T) {
	s, err := SummaryInfo{}.String()
	if err != nil {
		t.Fatalf("String: %v", err)
	}
	if strings.Contains(s, "incompleteCollections") {
		t.Error("incompleteCollections must be omitted when empty")
	}
	s, err = SummaryInfo{IncompleteCollections: []string{"dremio-master-0: queries-perf incomplete after 2 records (x)"}}.String()
	if err != nil || !strings.Contains(s, `"incompleteCollections"`) {
		t.Errorf("err=%v, want incompleteCollections in %s", err, s)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test -short ./cmd/root/collection/ -run "SuccessRate|NodeDoneStatus|IncompleteCollectionsOmitted" -v`
Expected: FAIL to compile — `undefined: successRate`, `undefined: nodeDoneStatus`, `unknown field IncompleteCollections`.

- [ ] **Step 3: Implement the summary pieces**

In `cmd/root/collection/summary.go`, add below `SkippedFiles` in `SummaryInfo`:

```go
	IncompleteCollections []string `json:"incompleteCollections,omitempty"`
```

In `cmd/root/collection/collector.go`:

(a) Add above `logDistributedCollectionSummary`:

```go
// successRate returns the share of collected files among attempted ones.
// Skipped files count as attempts, so skips cannot hide behind 100% (#339).
func successRate(collected, failed, skipped int) (float64, int) {
	attempts := collected + failed + skipped
	if attempts == 0 {
		return 0, 0
	}
	return float64(collected) / float64(attempts) * 100, attempts
}
```

(b) Change the signature to

```go
func logDistributedCollectionSummary(collectionMode collects.CollectionMode, coordinators, executors []string, files []helpers.CollectedFile, totalFailedFiles, totalFailedNodes, totalSkippedFiles, incomplete []string, nodesConnectedTo int, duration time.Duration) {
```

(c) Directly below `simplelog.Infof("  Skipped Collections: %d", len(totalSkippedFiles))` add:

```go
	simplelog.Infof("  Incomplete Collections: %d", len(incomplete))
```

(d) Directly below the `// Skipped files` block (ending after the `SKIPPED COLLECTIONS:` loop) add:

```go
	// Incomplete collections: partial data was kept and is flagged (#339)
	if len(incomplete) > 0 {
		simplelog.Warning("INCOMPLETE COLLECTIONS:")
		for _, item := range incomplete {
			simplelog.Warningf("  - %s", item)
		}
	}
```

(e) Replace the `// Success rate` block

```go
	totalAttempts := len(files) + totalFailures
	if totalAttempts > 0 {
		successRate := float64(len(files)) / float64(totalAttempts) * 100
		simplelog.Infof("Success Rate: %.1f%% (%d/%d)", successRate, len(files), totalAttempts)
	}
```

with

```go
	if rate, attempts := successRate(len(files), totalFailures, len(totalSkippedFiles)); attempts > 0 {
		simplelog.Infof("Success Rate: %.1f%% (%d/%d)", rate, len(files), attempts)
	}
```

In `cmd/root/collection/streaming_collect.go`:

(f) Add directly after the closing `}` of `streamNodeFiles`:

```go
// nodeDoneStatus renders a node's completion line: collected files, skipped
// files (base names) and incomplete collections.
func nodeDoneStatus(collected int, bytes int64, skipped []string, incomplete []*IncompleteCollectionError) string {
	s := fmt.Sprintf("Done: %d files (%s), %d skipped", collected, humanizeBytes(bytes), len(skipped))
	if len(skipped) > 0 {
		names := make([]string, len(skipped))
		for i, p := range skipped {
			names[i] = filepath.Base(p)
		}
		s += fmt.Sprintf(" (%s)", strings.Join(names, ", "))
	}
	if len(incomplete) > 0 {
		parts := make([]string, len(incomplete))
		for i, inc := range incomplete {
			parts[i] = fmt.Sprintf("%s (%d records)", inc.Item, inc.Records)
		}
		s += ", INCOMPLETE: " + strings.Join(parts, ", ")
	}
	return s
}
```

(g) Below `var totalSkippedFiles []string` (~776) add:

```go
	var totalSkippedLog []string // host:path, for the ddc.log summary only
	var totalIncomplete []string // "<host>: <item> incomplete after N records (...)"
```

(h) Below `var nodeSkipped []string` (~971) add `var nodeIncomplete []*IncompleteCollectionError`, and replace the RocksDB call

```go
				if rocksFiles, _, err := RunRocksDBCollection(rocksArgs); err != nil {
					simplelog.Errorf("RocksDB collection failed on %s: %v", host, err)
				} else {
					nodeCollected = append(nodeCollected, rocksFiles...)
				}
```

with

```go
				if rocksFiles, inc, err := RunRocksDBCollection(rocksArgs); err != nil {
					simplelog.Errorf("RocksDB collection failed on %s: %v", host, err)
				} else {
					nodeCollected = append(nodeCollected, rocksFiles...)
					nodeIncomplete = inc
				}
```

(i) Inside the `mu.Lock()` block, directly below `totalSkippedFiles = append(totalSkippedFiles, nodeSkipped...)`, add:

```go
		for _, p := range nodeSkipped {
			totalSkippedLog = append(totalSkippedLog, host+":"+p)
		}
		for _, inc := range nodeIncomplete {
			totalIncomplete = append(totalIncomplete, fmt.Sprintf("%s: %v", host, inc))
		}
```

(j) Replace

```go
		statusUX := fmt.Sprintf("Done: %d files (%s), %d skipped", len(nodeCollected), humanizeBytes(nodeBytes), len(nodeSkipped))
		if len(nodeSkipped) > 0 {
			names := make([]string, len(nodeSkipped))
			for i, p := range nodeSkipped {
				names[i] = filepath.Base(p)
			}
			statusUX = fmt.Sprintf("Done: %d files (%s), %d skipped (%s)", len(nodeCollected), humanizeBytes(nodeBytes), len(nodeSkipped), strings.Join(names, ", "))
		}
```

with

```go
		statusUX := nodeDoneStatus(len(nodeCollected), nodeBytes, nodeSkipped, nodeIncomplete)
```

(k) Below `summaryInfo.SkippedFiles = totalSkippedFiles` add `summaryInfo.IncompleteCollections = totalIncomplete`.

(l) In the `logDistributedCollectionSummary(` call replace the line `nil, totalFailedNodes, totalSkippedFiles,` with `nil, totalFailedNodes, totalSkippedLog, totalIncomplete,`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -short ./cmd/root/collection/ -run "SuccessRate|NodeDoneStatus|IncompleteCollectionsOmitted" -v`
Expected: PASS.
Run: `go test -short ./cmd/root/collection/`
Expected: `ok`.

- [ ] **Step 5: Build**

Run: `go build -o bin/ddc.exe .`
Expected: exit 0.

- [ ] **Step 6: Commit**

Run: `git add -A && git status --short` (confirm only this task's files are staged; `bin/` stays ignored), then
`git commit -m "wip(#339): task 7 — Reporting — success rate, INCOMPLETE items, host-qualified skips"`.
Expected: one new commit on `fix/339-k8s-stream-truncation`.

---

### Task 8: Documentation and full verification

**Files:**
- Modify: `CHANGELOG.md` (top), `docs/architecture/decisions.md` (D076 row line 20; add D077), `docs/architecture/capability-contract.md` (R076 row line 33), `docs/architecture/patterns-and-gotchas.md` (Transport and Streaming section), `CLAUDE.md` (line 88)

**Interfaces:**
- Consumes: behaviour from Tasks 1–7.
- Produces: release notes and architecture records.

- [ ] **Step 1: CHANGELOG**

In `CHANGELOG.md`, insert directly below `# Changelog` and its blank line:

```markdown
## [4.0.6] - Unreleased

- Kubernetes collections over slow links no longer skip `queries.*.json.gz` archives and server logs (#339). File streams now use a SPDY exec connection without keepalive pings: client-go's 5 s ping reaching the API server after the remote `gzip`/`cat` had exited made the Linux kernel reset the connection and drop the file's tail, which client-go reported as success.
- Truncated transfers are no longer accepted silently on any transport: a stream shorter than the remote file is retried (a file rotated between discovery and streaming is still accepted), and JFR, heap-dump and async-profiler artifacts are retried up to 3 times before their remote temp file is removed.
- queries-perf on Kubernetes streams without keepalive pings and ends with an end-of-stream marker. An export cut short keeps its records and is reported as INCOMPLETE on the node status line, in ddc.log and in `summary.json` (`incompleteCollections`). The viewer's stderr now goes to ddc.log instead of into the queries-perf files.
- The collection summary's success rate now counts skipped files, and skipped entries in ddc.log name their pod/host.
```

- [ ] **Step 2: Architecture decisions**

In `docs/architecture/decisions.md`, replace the D076 row with:

```markdown
| D076 | K8s exec transport upgrade | Replace `NewSPDYExecutor` with `NewFallbackExecutor(WebSocket, SPDY, shouldFallback)` | WebSocket is the modern K8s exec transport (GA since 1.29). SPDY is deprecated. WebSocket offers better proxy compatibility. **Superseded:** the WebSocket fallback executor was removed during v4 development; the K8s API transport is SPDY-only (see D077) |
| D077 | Keepalive-free SPDY for streaming execs (#339) | `StreamFromHost` and the queries-perf stream use a SPDY executor with `PingPeriod: 0`; queries-perf appends `printf '\n__DDC_EOS__:%d\n' $?` and treats a missing marker as INCOMPLETE; all other execs keep client-go's 5 s pings | client-go's keepalive PING reaching a server socket that closed after the remote command exited makes Linux reset the connection (TCPAbortOnData), dropping the output tail and exit status while client-go reports success. Streaming execs are never idle, so they need no keepalive; the marker proves completeness on the one stream without a size to check. The WebSocket executor also pings every 5 s, so switching transport would not avoid it |
```

In `docs/architecture/capability-contract.md`, replace the R076 row with:

```markdown
| R076 | core | ~~NewSPDYExecutor replaced with NewFallbackExecutor(WebSocket, SPDY, shouldFallback)~~ Superseded: SPDY-only; streaming execs are keepalive-free (D077) | WebSocket fallback was removed during v4 development; see D077 for the streaming executor |
```

- [ ] **Step 3: Gotcha**

In `docs/architecture/patterns-and-gotchas.md`, directly below the "Advisory verification pattern" paragraph, add:

```markdown
**Scope:** only checksum verification is advisory. Byte-count completeness is enforced: a stream shorter than the remote file is retried and, if still short, skipped — the same outcome the gzip path already had for a truncated stream (`unexpected EOF`).
```

and directly below the "K8SWriter line-buffering for chunked streaming" subsection (after its **Gotcha** paragraph), add:

```markdown
### client-go SPDY keepalive truncates slow streams

client-go's SPDY transport sends a PING every 5 s. When a remote command exits while its output is still draining to a slow client, the server side closes its socket; a later PING reaching that socket makes the Linux kernel reset the connection (`TCPAbortOnData`), discarding the queued output tail and the exit status. client-go's `watchErrorStream` treats "no status" as success, so `StreamWithContext` returns `nil` on truncated output (#339).

Use `newStreamExecutor` (keepalive-free) for execs that stream continuously, and never trust a `nil` error alone: check the byte count against the remote size, or append an end-of-stream marker. OS-level TCP keepalive (Go dialer default) remains on and carries no payload, so it cannot trigger the reset.
```

- [ ] **Step 4: CLAUDE.md**

In `CLAUDE.md`, replace

```markdown
- **`cmd/root/kubernetes/`** -- K8s API client transport (WebSocket primary, SPDY fallback)
```

with

```markdown
- **`cmd/root/kubernetes/`** -- K8s API client transport (SPDY exec; file streams and the queries-perf stream run without keepalive pings, see D077)
```

- [ ] **Step 5: Full verification**

Run each and confirm:
```bash
go fmt ./...                # no files listed
golangci-lint run           # no issues
go test -short ./...        # all packages ok
go build -o bin/ddc.exe .   # exit 0
```
Then repeat Task 2 Step 5 (Linux loopback pair) on the final tree. Expected: `PASS` for both loopback tests.

- [ ] **Step 6: Commit**

Run: `git add -A && git status --short` (confirm only this task's files are staged; `bin/` stays ignored), then
`git commit -m "wip(#339): task 8 — Documentation and full verification"`.
Expected: one new commit on `fix/339-k8s-stream-truncation`.

---

### Task 9: Field acceptance on the affected cluster (manual)

**Files:** none.

**Interfaces:**
- Consumes: the reviewed build from Task 8.
- Produces: the acceptance result for #339.

- [ ] **Step 1: Build the Linux binary for the reporter**

Run: `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/ddc-linux-amd64 .`
Expected: exit 0. Share it through the usual internal channel (not on the public issue).

- [ ] **Step 2: Reporter reruns the same collection**

Ask the reporter to run the exact `ddc collect k8s standard …` command printed as `generated cli command:` at the top of their previous ddc.log, then send back ddc.log (redacted as before).

- [ ] **Step 3: Check the result**

On the returned ddc.log:
```bash
grep -c "transient error streaming" ddc.log      # expected 0
grep -c "stream skip:" ddc.log                   # expected 0
grep -n "Success Rate" ddc.log                   # expected 100.0% with no skips
grep -n "INCOMPLETE" ddc.log                     # expected no matches
```
Pass = all four hold (baseline: 15–27 skips per run). Report the outcome on #339 without customer identifiers.
