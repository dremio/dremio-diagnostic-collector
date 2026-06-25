# Log-path autodetection, RocksDB-catalog gating, container-name memoization — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make DDC reliably collect `queries.json`/server logs and `queries-perf` from K8s clusters where Dremio's log path is overridden and the catalog lives only on the master.

**Architecture:** Three independent fixes in the streaming-collection layer. (A) `RunDiscovery` inspects the live Dremio process to resolve the log directory before probing. (B) `RunRocksDBCollection` skips nodes lacking a RocksDB catalog. (C) K8s `getContainerName` memoizes per-pod results so transient `kubectl get pods` failures don't abort collection.

**Tech Stack:** Go; Cobra CLI; transports via `HostExecutor` (K8s/SSH/Local); table-driven tests with the existing `mockExecutor` / `tests.MockCli`.

## Global Constraints

- Build after changes: `go build -o bin/ddc.exe .` (must succeed).
- Tests on Windows run without `-race`: `go test -short ./...`.
- Spec: `docs/superpowers/specs/2026-06-25-ddc-logpath-rocks-gating-design.md`.
- Commits happen ONLY with explicit user approval (project rule: never commit unless asked). The commit steps below are gated on that approval.
- No new dependencies. Match existing code style in each file.
- Remote commands must stay transport-safe: no nested `sh -c`, no shell pipes — plain multi-token commands only (each transport already runs args through one shell).

---

### Task 1: Share `ExtractEnvValue` and factor `dirHasFiles`

Mechanical refactor with no behavior change, prerequisite for Task 2.

**Files:**
- Modify: `cmd/root/collection/discovery.go` (add `ExtractEnvValue`, add `dirHasFiles`, simplify `probeDir`)
- Modify: `cmd/root.go` (remove local `extractEnvValue`, call `collection.ExtractEnvValue`)
- Test: `cmd/root/collection/discovery_test.go` (add `ExtractEnvValue` tests)

**Interfaces:**
- Produces: `collection.ExtractEnvValue(ps, key string) string`; `dirHasFiles(executor HostExecutor, host, dir string) bool` (unexported).

- [ ] **Step 1: Write the failing test for `ExtractEnvValue`**

Add to `cmd/root/collection/discovery_test.go`:

```go
func TestExtractEnvValue(t *testing.T) {
	ps := "java -DREMIO_LOG_DIR=/opt/dremio/log -Ddremio.log.path=/opt/dremio/log " +
		"-Ddremio.log.path=/opt/dremio/data/log DREMIO_LOG_DIR=/opt/dremio/log"
	if got := ExtractEnvValue(ps, "-Ddremio.log.path="); got != "/opt/dremio/data/log" {
		t.Errorf("dremio.log.path: want /opt/dremio/data/log, got %q", got)
	}
	if got := ExtractEnvValue(ps, "DREMIO_LOG_DIR="); got != "/opt/dremio/log" {
		t.Errorf("DREMIO_LOG_DIR: want /opt/dremio/log, got %q", got)
	}
	if got := ExtractEnvValue(ps, "-Dmissing="); got != "" {
		t.Errorf("missing key: want empty, got %q", got)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -short -run TestExtractEnvValue ./cmd/root/collection/...`
Expected: FAIL — `undefined: ExtractEnvValue`.

- [ ] **Step 3: Add `ExtractEnvValue` to `discovery.go`**

Add (exported copy of the logic currently in `cmd/root.go`; `LastIndex` is required so an override later on the command line wins):

```go
// ExtractEnvValue returns the value following key in a `ps eww` / cmdline+environ
// blob. LastIndex matches JVM semantics: when -Dfoo= appears multiple times, the
// JVM resolves the last one (Dremio launchers derive -Ddremio.log.path= from
// DREMIO_LOG_DIR and then let DREMIO_JAVA_SERVER_EXTRA_OPTS override it).
func ExtractEnvValue(ps, key string) string {
	idx := strings.LastIndex(ps, key)
	if idx < 0 {
		return ""
	}
	rest := ps[idx+len(key):]
	end := strings.IndexAny(rest, " \t\n\x00")
	value := rest
	if end >= 0 {
		value = rest[:end]
	}
	return strings.TrimRight(strings.TrimSpace(value), ",")
}
```

- [ ] **Step 4: Factor `dirHasFiles` and simplify `probeDir` in `discovery.go`**

Replace the existing `probeDir` (currently `discovery.go:168-183`) with:

```go
// dirHasFiles reports whether dir exists on host and contains at least one
// regular file. An existing-but-empty directory is logged and treated as unusable.
func dirHasFiles(executor HostExecutor, host, dir string) bool {
	out, err := executor(host, "test", "-d", dir, "&&", "echo", "exists")
	if err != nil || !strings.Contains(out, "exists") {
		return false
	}
	filesOut, err := executor(host, "find", "-L", dir, "-maxdepth", "1", "-type", "f", "-print", "-quit")
	if err != nil || strings.TrimSpace(filesOut) == "" {
		simplelog.Infof("dirHasFiles: %v exists but is empty on %v, skipping", dir, host)
		return false
	}
	return true
}

// probeDir returns the first candidate that exists and contains a file.
func probeDir(executor HostExecutor, host string, candidates []string) string {
	for _, dir := range candidates {
		if dirHasFiles(executor, host, dir) {
			return dir
		}
	}
	return ""
}
```

- [ ] **Step 5: Replace the local `extractEnvValue` in `cmd/root.go` with the shared one**

In `cmd/root.go`, delete the `func extractEnvValue(ps, key string) string { ... }` definition. Replace its three call sites (`runLocalPathDiscovery` ~line 1760, 1762; `runPathDiscovery`-related ~line 1909, 1944; and any other `extractEnvValue(` calls) with `collection.ExtractEnvValue(`. Confirm `cmd/root.go` already imports the collection package (it uses `collection.RemoteNodeInfo` / `configui` types); if not, add `"github.com/dremio/dremio-diagnostic-collector/v3/cmd/root/collection"` (verify the exact module path from other imports in the file).

Find all call sites first:

Run: `grep -n "extractEnvValue" cmd/root.go`
Replace each `extractEnvValue(` with `collection.ExtractEnvValue(`, then delete the function definition.

- [ ] **Step 6: Build and run tests**

Run: `go build -o bin/ddc.exe . && go test -short ./cmd/root/collection/... ./cmd/...`
Expected: build succeeds; `TestExtractEnvValue` passes; all existing `discovery_test.go` and `root` tests still pass (probeDir behavior unchanged).

- [ ] **Step 7: Commit (after user approval)**

```bash
git add cmd/root/collection/discovery.go cmd/root/collection/discovery_test.go cmd/root.go
git commit -m "refactor: share ExtractEnvValue and factor dirHasFiles"
```

---

### Task 2: Per-node log-directory autodetection in `RunDiscovery`

**Files:**
- Modify: `cmd/root/collection/discovery.go` (`RunDiscovery` reorder; add `readProcessInfo`, `resolveLogDir`)
- Test: `cmd/root/collection/discovery_test.go`

**Interfaces:**
- Consumes: `ExtractEnvValue`, `dirHasFiles`, `probeDir`, `discoverPID`, `logDirCandidates` (Task 1 + existing).
- Produces: `readProcessInfo(executor HostExecutor, host string, pid int) string`; `resolveLogDir(executor HostExecutor, host, logDir string, pid int) string`.

- [ ] **Step 1: Write the failing tests**

Add to `cmd/root/collection/discovery_test.go`:

```go
func TestResolveLogDir(t *testing.T) {
	const psBlob = "java -Ddremio.log.path=/opt/dremio/data/log DREMIO_LOG_DIR=/opt/dremio/log"

	// Helper to build a responder: ps eww returns psBlob; dir checks per map.
	build := func(nonEmpty map[string]bool) HostExecutor {
		return func(_ string, args ...string) (string, error) {
			joined := strings.Join(args, " ")
			switch {
			case strings.HasPrefix(joined, "ps eww"):
				return psBlob, nil
			case strings.HasPrefix(joined, "test -d"):
				dir := args[2]
				if nonEmpty[dir] {
					return "exists", nil
				}
				return "", nil
			case strings.HasPrefix(joined, "find -L"):
				dir := args[2]
				if nonEmpty[dir] {
					return dir + "/server.log", nil
				}
				return "", nil
			}
			return "", nil
		}
	}

	// Explicit flag wins outright — ps must not even be consulted.
	called := false
	exec1 := func(_ string, args ...string) (string, error) {
		if strings.HasPrefix(strings.Join(args, " "), "ps eww") {
			called = true
		}
		return "", nil
	}
	if got := resolveLogDir(exec1, "h", "/explicit/path", 123); got != "/explicit/path" {
		t.Errorf("explicit: want /explicit/path, got %q", got)
	}
	if called {
		t.Errorf("explicit flag must skip process inspection")
	}

	// -Ddremio.log.path used when its dir has files.
	if got := resolveLogDir(build(map[string]bool{"/opt/dremio/data/log": true}), "h", "", 123); got != "/opt/dremio/data/log" {
		t.Errorf("logpath: want /opt/dremio/data/log, got %q", got)
	}

	// Detected dir empty -> fall through to DREMIO_LOG_DIR (which has files).
	if got := resolveLogDir(build(map[string]bool{"/opt/dremio/log": true}), "h", "", 123); got != "/opt/dremio/log" {
		t.Errorf("fallthrough to DREMIO_LOG_DIR: want /opt/dremio/log, got %q", got)
	}

	// Nothing detected, nothing on disk -> probe returns "".
	if got := resolveLogDir(build(map[string]bool{}), "h", "", 123); got != "" {
		t.Errorf("none: want empty, got %q", got)
	}

	// PID 0 -> skip process inspection; probe candidate that has files.
	if got := resolveLogDir(build(map[string]bool{"/var/log/dremio": true}), "h", "", 0); got != "/var/log/dremio" {
		t.Errorf("pid0 probe: want /var/log/dremio, got %q", got)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -short -run TestResolveLogDir ./cmd/root/collection/...`
Expected: FAIL — `undefined: resolveLogDir`.

- [ ] **Step 3: Add `readProcessInfo` and `resolveLogDir` to `discovery.go`**

```go
// readProcessInfo returns the Dremio process command line and environment as a
// single text blob (for ExtractEnvValue). Pipe-free so it is transport-safe.
// Returns "" if process info cannot be read.
func readProcessInfo(executor HostExecutor, host string, pid int) string {
	if pid <= 0 {
		return ""
	}
	pidStr := strconv.Itoa(pid)
	// Primary: ps eww shows both JVM args and environment in one blob.
	if out, err := executor(host, "ps", "eww", pidStr); err == nil && strings.TrimSpace(out) != "" {
		return out
	}
	// Fallback for minimal images without ps: read /proc and translate the NUL
	// separators in Go (no shell pipe needed).
	var b strings.Builder
	if out, err := executor(host, "cat", "/proc/"+pidStr+"/cmdline"); err == nil {
		b.WriteString(strings.ReplaceAll(out, "\x00", " "))
		b.WriteString(" ")
	}
	if out, err := executor(host, "cat", "/proc/"+pidStr+"/environ"); err == nil {
		b.WriteString(strings.ReplaceAll(out, "\x00", " "))
	}
	return b.String()
}

// resolveLogDir determines the Dremio log directory using, in order:
//  1. an explicit operator path (logDir), used as-is;
//  2. -Ddremio.log.path= from the process, if that dir has files;
//  3. DREMIO_LOG_DIR= from the process env, if that dir has files;
//  4. probing the well-known candidate directories.
func resolveLogDir(executor HostExecutor, host, logDir string, pid int) string {
	if logDir != "" {
		return logDir
	}
	if procInfo := readProcessInfo(executor, host, pid); procInfo != "" {
		if d := ExtractEnvValue(procInfo, "-Ddremio.log.path="); d != "" && dirHasFiles(executor, host, d) {
			simplelog.Infof("resolveLogDir: using -Ddremio.log.path=%v on %v", d, host)
			return d
		}
		if d := ExtractEnvValue(procInfo, "DREMIO_LOG_DIR="); d != "" && dirHasFiles(executor, host, d) {
			simplelog.Infof("resolveLogDir: using DREMIO_LOG_DIR=%v on %v", d, host)
			return d
		}
	}
	return probeDir(executor, host, logDirCandidates)
}
```

Confirm `strconv` is imported in `discovery.go` (it is — used by `parsePIDLines`).

- [ ] **Step 4: Reorder `RunDiscovery` to discover the PID first, then resolve the log dir**

In `RunDiscovery` (`discovery.go`): move the PID-discovery block (currently step 4, the
`pid, err := discoverPID(executor, host)` block at ~`discovery.go:133-142`) to run
immediately after `var anySuccess bool` (before the log-directory block). Then change the
log-directory resolution from:

```go
	// 1. Find the log directory — user-provided path overrides probing.
	if logDir != "" {
		info.LogDir = logDir
	} else {
		info.LogDir = probeDir(executor, host, logDirCandidates)
	}
```

to:

```go
	// 1. Find the log directory: explicit flag > -Ddremio.log.path > DREMIO_LOG_DIR > probe.
	info.LogDir = resolveLogDir(executor, host, logDir, info.DremioPID)
```

Leave the subsequent `if info.LogDir != "" { anySuccess = true; listFiles(...) ... }` block and
the conf/rocksdb/checksum/gzip steps unchanged. Result order: PID → log dir → conf → rocksdb →
checksum → gzip → `if !anySuccess` guard.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test -short -run "TestResolveLogDir|TestRunDiscovery" ./cmd/root/collection/...`
Expected: PASS. Existing `RunDiscovery` tests still pass (they pass `pid`-less mock responses;
`readProcessInfo` tolerates missing `ps`/`proc` responses by returning "" and falling through
to probe).

- [ ] **Step 6: Build and full package test**

Run: `go build -o bin/ddc.exe . && go test -short ./cmd/root/collection/...`
Expected: build succeeds; all tests pass.

- [ ] **Step 7: Commit (after user approval)**

```bash
git add cmd/root/collection/discovery.go cmd/root/collection/discovery_test.go
git commit -m "feat: autodetect Dremio log dir from process in RunDiscovery"
```

---

### Task 3: Gate RocksDB-viewer collection on catalog presence

**Files:**
- Modify: `cmd/root/collection/rockscollect.go` (`RunRocksDBCollection` pre-flight check)
- Modify: `cmd/root/collection/streaming_collect.go` (update `// coordinator only` comment at line 971)
- Test: `cmd/root/collection/rockscollect_test.go` (create if absent)

**Interfaces:**
- Consumes: existing `Collector.HostExecute`, `RocksCollectArgs` (fields `Collector`, `Host`, `RocksDBDir`).
- Produces: no new exported symbols; `RunRocksDBCollection` now returns `(nil, nil)` when no catalog.

- [ ] **Step 1: Write the failing test**

Create `cmd/root/collection/rockscollect_test.go` (use the mock Collector pattern already used in this package's tests — inspect an existing `*_test.go` in `cmd/root/collection/` for the mock type; the test below assumes a minimal mock implementing `Collector`). If a mock collector helper already exists, reuse it.

```go
package collection

import (
	"strings"
	"testing"
)

// rocksMockCollector records HostExecute calls and answers the catalog check.
type rocksMockCollector struct {
	catalogPresent bool
	calls          []string
}

func (m *rocksMockCollector) HostExecute(_ bool, _ string, args ...string) (string, error) {
	joined := strings.Join(args, " ")
	m.calls = append(m.calls, joined)
	if strings.HasPrefix(joined, "test -f") && strings.Contains(joined, "/catalog/CURRENT") {
		if m.catalogPresent {
			return "exists", nil
		}
		return "", nil
	}
	return "", nil
}

// Implement the remaining Collector methods as no-ops so the type satisfies the
// interface. Copy the method set from an existing mock in this package.
// (CopyToHost, Protocol, GetCoordinators, GetExecutors, StreamFromHost, etc.)

func TestRunRocksDBCollectionSkipsWhenNoCatalog(t *testing.T) {
	m := &rocksMockCollector{catalogPresent: false}
	args := RocksCollectArgs{Collector: m, Host: "dremio-coordinator-0", RocksDBDir: "/opt/dremio/data/db"}

	files, err := RunRocksDBCollection(args)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if files != nil {
		t.Errorf("expected no files, got %v", files)
	}
	// Only the catalog check should have run — no binary upload (uname -m).
	for _, c := range m.calls {
		if strings.Contains(c, "uname") {
			t.Errorf("binary upload attempted despite missing catalog: %q", c)
		}
	}
}
```

> Note: if building a full `Collector` mock is heavy, instead extract the catalog check into a
> tiny helper `hasRocksCatalog(c Collector, host, dbPath string) bool` and unit-test that helper
> directly with the minimal `rocksMockCollector` above (only `HostExecute` needed). Prefer the
> helper approach if `Collector` has many methods.

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -short -run TestRunRocksDBCollectionSkips ./cmd/root/collection/...`
Expected: FAIL (collection proceeds / attempts upload, or helper undefined).

- [ ] **Step 3: Add the pre-flight check to `RunRocksDBCollection`**

In `rockscollect.go`, immediately after `dbPath := args.RocksDBDir + "/catalog"` (line ~142) and
before the `// Upload binary` block, insert:

```go
	// The RocksDB catalog (KV store) lives only on the master coordinator.
	// Scale-out coordinators connect to it remotely and have no local catalog,
	// so rocksdb-viewer cannot read it there — skip them silently.
	catalogCurrent := dbPath + "/CURRENT"
	if out, err := c.HostExecute(false, host, "test", "-f", catalogCurrent, "&&", "echo", "exists"); err != nil || !strings.Contains(out, "exists") {
		simplelog.Infof("rocksdb: no catalog at %s on %s — skipping RocksDB-viewer collection (not a master coordinator)", catalogCurrent, host)
		return nil, nil
	}
```

Confirm `strings` and `simplelog` are imported in `rockscollect.go` (both already used).

- [ ] **Step 4: Update the caller comment**

In `streaming_collect.go:971`, change the comment `// --- RocksDB viewer collection (coordinator only) ---` to `// --- RocksDB viewer collection (coordinators; skipped per-node when no catalog) ---`. No logic change.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test -short -run TestRunRocksDBCollection ./cmd/root/collection/...`
Expected: PASS.

- [ ] **Step 6: Build and full package test**

Run: `go build -o bin/ddc.exe . && go test -short ./cmd/root/collection/...`
Expected: build succeeds; all tests pass.

- [ ] **Step 7: Commit (after user approval)**

```bash
git add cmd/root/collection/rockscollect.go cmd/root/collection/rockscollect_test.go cmd/root/collection/streaming_collect.go
git commit -m "feat: skip rocksdb-viewer collection on nodes without a catalog"
```

---

### Task 4: Memoize K8s container-name resolution

**Files:**
- Modify: `cmd/root/kubectl/kubectl.go` (`CliK8sActions` field; `getContainerName` cache+retry; extract `resolveContainerName`; init map in constructor)
- Test: `cmd/root/kubectl/kubectl_test.go`

**Interfaces:**
- Consumes: existing `c.m sync.Mutex`, `c.cli.Execute`, `c.retriesEnabled`.
- Produces: `containerNames map[string]string` field; unexported `resolveContainerName(podName string) (string, error)`.

- [ ] **Step 1: Write the failing test**

Add to `cmd/root/kubectl/kubectl_test.go`:

```go
func TestGetContainerNameCached(t *testing.T) {
	cli := &tests.MockCli{
		// call 1: get pods -> container; call 2: exec -> ok; call 3: exec -> ok
		StoredResponse: []string{"dremio-coordinator", "ok", "ok"},
		StoredErrors:   []error{nil, nil, nil},
	}
	k := CliK8sActions{cli: cli, kubectlPath: "kubectl", namespace: "ns", k8sContext: "ctx"}

	if _, err := k.HostExecute(false, "pod1", "ls"); err != nil {
		t.Fatalf("first exec: %v", err)
	}
	if _, err := k.HostExecute(false, "pod1", "ls"); err != nil {
		t.Fatalf("second exec: %v", err)
	}

	getPods := 0
	for _, call := range cli.Calls {
		for _, a := range call {
			if a == "pods" {
				getPods++
			}
		}
	}
	if getPods != 1 {
		t.Errorf("expected container name resolved once, got %d get-pods calls", getPods)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -short -run TestGetContainerNameCached ./cmd/root/kubectl/...`
Expected: FAIL — 2 get-pods calls (no caching).

- [ ] **Step 3: Add the cache field and initialize it**

In `cmd/root/kubectl/kubectl.go`, add to the `CliK8sActions` struct (near `pidHosts`):

```go
	containerNames map[string]string
```

In `NewKubectlK8sActions` (the returned struct literal at ~line 67), add after `pidHosts: make(map[string]string),`:

```go
		containerNames: make(map[string]string),
```

- [ ] **Step 4: Rename the current `getContainerName` body to `resolveContainerName` and add a caching `getContainerName`**

Rename the existing `func (c *CliK8sActions) getContainerName(podName string) (string, error)` to
`func (c *CliK8sActions) resolveContainerName(podName string) (string, error)` (body unchanged).
Then add:

```go
// getContainerName returns the Dremio container for a pod, resolving it once and
// caching the result. Resolution is the only kubectl call that previously ran on
// every HostExecute; memoizing it removes exposure to transient `get pods`
// failures mid-collection. A small retry guards the single cold-miss resolution.
func (c *CliK8sActions) getContainerName(podName string) (string, error) {
	c.m.Lock()
	if c.containerNames == nil {
		c.containerNames = make(map[string]string)
	}
	if name, ok := c.containerNames[podName]; ok {
		c.m.Unlock()
		return name, nil
	}
	c.m.Unlock()

	attempts := 1
	if c.retriesEnabled {
		attempts = 3
	}
	var name string
	var err error
	for i := 0; i < attempts; i++ {
		if name, err = c.resolveContainerName(podName); err == nil {
			break
		}
	}
	if err != nil {
		return "", err
	}

	c.m.Lock()
	c.containerNames[podName] = name
	c.m.Unlock()
	return name, nil
}
```

> Note: `addRetries` adds a kubectl `cp`-specific `--retries` flag and is not applicable to
> `get pods`, so the resolution uses this small inline retry instead. Caching is the primary
> fix (resolution happens once, early); the retry is a belt-and-suspenders guard for a cold miss.
> No sleep is used, keeping the test deterministic.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test -short -run "TestGetContainerNameCached|TestKubectl" ./cmd/root/kubectl/...`
Expected: PASS. Existing container-detection tests still pass (they build `CliK8sActions{}`
literals with a nil map; the lazy-init guard handles that).

- [ ] **Step 6: Build and full package test**

Run: `go build -o bin/ddc.exe . && go test -short ./cmd/root/kubectl/...`
Expected: build succeeds; all tests pass.

- [ ] **Step 7: Commit (after user approval)**

```bash
git add cmd/root/kubectl/kubectl.go cmd/root/kubectl/kubectl_test.go
git commit -m "fix: memoize kubectl container-name resolution per pod"
```

---

### Task 5: Final verification

- [ ] **Step 1: Full build and test sweep**

Run: `go build -o bin/ddc.exe . && go test -short ./...`
Expected: build succeeds; full unit suite passes.

- [ ] **Step 2: Lint**

Run: `go fmt ./... && golangci-lint run`
Expected: no new findings in touched files.

- [ ] **Step 3: Confirm the binary was built**

Confirm `bin/ddc.exe` exists and report the build result in the final message.

---

## Self-Review

**Spec coverage:**
- Part A (log autodetect, both modes, per-node, order flag>logpath>LOG_DIR>probe, empty→fallthrough) → Tasks 1 + 2.
- Part B (catalog-presence gating for all rocksdb-viewer collections, silent skip, before upload) → Task 3.
- Part C (memoize container name, lazy-init, retry on cold miss) → Task 4.
- Build/test verification → Task 5.

**Placeholder scan:** the only conditional guidance is the Task 3 "helper vs full mock" note, which gives concrete code for both routes — not a placeholder.

**Type consistency:** `ExtractEnvValue`/`dirHasFiles`/`probeDir` (Task 1) are consumed with identical signatures in Task 2; `resolveLogDir`/`readProcessInfo` signatures match between definition and tests; `resolveContainerName`/`containerNames` names are consistent across Task 4 steps.
