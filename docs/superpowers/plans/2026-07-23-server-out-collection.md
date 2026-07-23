# Collect server.out as Part of Server Logs Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Collect `server.out` (and, on split-directory clusters, GC logs from `DREMIO_LOG_DIR`) as part of the existing Server-logs collection, in both standard and diagnosis modes — fixing [issue #337](https://github.com/dremio/dremio-diagnostic-collector/issues/337).

**Architecture:** Remove `server.out` from the stream-time hard exclusion list so it rides the existing `server.` prefix rules (gated by `CollectServerLogs`, allowed in standard mode, routed to `logs/`). Exempt it from the date-range filter (its mtime reflects the last restart, not content age). Add a secondary discovery pass: when the process env's `DREMIO_LOG_DIR` differs from the resolved log dir, list that directory too, keeping only `server.out*` and GC logs. Rename two TUI labels to "Server logs & out".

**Tech Stack:** Go, table-driven tests with the existing `mockExecutor`, kubectl-reachable test cluster (`dremio-master-0`, namespace `default`).

**Spec:** `docs/superpowers/specs/2026-07-23-server-out-collection-design.md`

## Global Constraints

- **NEVER run `git commit` without explicit user approval.** At every "Commit" step: stop, tell the user what changed, and wait for their go-ahead. (Project rule: user reviews all changes before any commit.)
- **No new CLI flags. No new TUI selections.** Only the two label renames.
- After any code change: `go build -o bin/ddc.exe .` must succeed.
- Tests on Windows: `go test -short ./...` (no `-race`; CGO unavailable).
- Test cluster: read-only use of `dremio-master-0`. Running DDC collections against it is allowed; do not modify the cluster.
- Match existing code style; touch nothing unrelated.

---

### Task 1: Un-block server.out and exempt it from the date filter

**Files:**
- Modify: `cmd/root/collection/streaming_collect.go` (`alwaysExcludedPrefixes` ~line 435, `logDayLimit` ~line 514)
- Test: `cmd/root/collection/streaming_collect_test.go` (`TestIsAlwaysExcluded` ~line 572)

**Interfaces:**
- Consumes: existing `isAlwaysExcluded(baseName string) bool`, `logDayLimit(base string, args Args) int`, `isLogAllowedInStandardMode(baseName string) bool`, `isLogTypeEnabled(baseName string, args Args) bool` — all package-private in `collection`.
- Produces: no signature changes. Behavior change only: `isAlwaysExcluded("server.out") == false`; `logDayLimit("server.out*", …) == -1` in every mode.

- [ ] **Step 1: Update the existing exclusion test and add the new behavior tests**

In `cmd/root/collection/streaming_collect_test.go`, inside `TestIsAlwaysExcluded`, move the `server.out` case from the blocked group to the not-blocked group — change:

```go
		{"server.json", "server.json", true},
		{"server.out", "server.out", true},
```

to:

```go
		{"server.json", "server.json", true},
```

and add to the `// Should NOT be blocked.` group:

```go
		{"server.out", "server.out", false},
		{"rotated server.out", "server.out.1", false},
```

Then add two new test functions after `TestIsAlwaysExcluded` (the file already imports `collects`; if the compiler complains, add `"github.com/dremio/dremio-diagnostic-collector/v4/pkg/collects"` to the imports):

```go
// TestLogDayLimit_ServerOut verifies server.out is never date-filtered:
// its mtime reflects the last restart, and its startup/ulimit content is
// valuable regardless of age.
func TestLogDayLimit_ServerOut(t *testing.T) {
	standard := Args{CollectionMode: collects.StandardCollection, ServerLogsNumDays: 3}
	if got := logDayLimit("server.out", standard); got != -1 {
		t.Errorf("standard server.out = %d, want -1", got)
	}
	if got := logDayLimit("server.out.1", standard); got != -1 {
		t.Errorf("standard server.out.1 = %d, want -1", got)
	}
	// server.log must keep its per-log day count.
	if got := logDayLimit("server.log", standard); got != 3 {
		t.Errorf("standard server.log = %d, want 3", got)
	}

	diagnosis := Args{CollectionMode: collects.DiagnosisCollection, DiagLogDays: 3}
	if got := logDayLimit("server.out", diagnosis); got != -1 {
		t.Errorf("diagnosis server.out = %d, want -1", got)
	}
	// Other logs must keep the unified diagnosis day limit.
	if got := logDayLimit("server.log", diagnosis); got != 3 {
		t.Errorf("diagnosis server.log = %d, want 3", got)
	}
}

// TestServerOut_StandardAllowlistAndGating verifies server.out inherits the
// existing "server." prefix rules once un-blocked.
func TestServerOut_StandardAllowlistAndGating(t *testing.T) {
	if !isLogAllowedInStandardMode("server.out") {
		t.Error("server.out should be allowed in standard mode")
	}
	if !isLogTypeEnabled("server.out", Args{CollectServerLogs: true}) {
		t.Error("server.out should be collected when CollectServerLogs is true")
	}
	if isLogTypeEnabled("server.out", Args{CollectServerLogs: false}) {
		t.Error("server.out should be skipped when CollectServerLogs is false")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -short -run "TestIsAlwaysExcluded|TestLogDayLimit_ServerOut|TestServerOut_StandardAllowlistAndGating" ./cmd/root/collection/...`
Expected: FAIL — `isAlwaysExcluded("server.out") = true, want false` and `logDayLimit("server.out", …) = 3, want -1`. (`TestServerOut_StandardAllowlistAndGating` already passes — the prefix rules exist; it pins the behavior.)

- [ ] **Step 3: Implement**

In `cmd/root/collection/streaming_collect.go`, remove the `"server.out"` entry from `alwaysExcludedPrefixes`:

```go
// alwaysExcludedPrefixes are log files that should never be collected in any mode.
var alwaysExcludedPrefixes = []string{
	"admin_backup",
	"audit.",
	"server.json",
}
```

In `logDayLimit`, add a first check before the diagnosis-mode branch:

```go
func logDayLimit(base string, args Args) int {
	// server.out is one small file whose mtime reflects the last restart; its
	// startup/ulimit content is valuable regardless of age, so it is never
	// date-filtered in any mode.
	if strings.HasPrefix(base, "server.out") {
		return -1
	}
	// Diagnosis mode: all log types use the unified day limit.
	if args.CollectionMode == collects.DiagnosisCollection && args.DiagLogDays > 0 {
		return args.DiagLogDays
	}
	// ... rest unchanged ...
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -short ./cmd/root/collection/...`
Expected: PASS (whole package — confirms no other test relied on the exclusion).

- [ ] **Step 5: Build**

Run: `go build -o bin/ddc.exe .`
Expected: exit 0.

- [ ] **Step 6: Commit checkpoint**

STOP. Summarize the diff for the user and wait for approval. Only on explicit approval:

```bash
git add cmd/root/collection/streaming_collect.go cmd/root/collection/streaming_collect_test.go
git commit -m "fix: collect server.out when server logs are enabled (#337)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 2: Pass process info into resolveLogDir (refactor, no behavior change)

**Files:**
- Modify: `cmd/root/collection/discovery.go` (`RunDiscovery` ~line 83–99, `resolveLogDir` ~line 531)
- Test: `cmd/root/collection/discovery_test.go` (`TestResolveLogDir` ~line 1082)

**Interfaces:**
- Consumes: `readProcessInfo(executor HostExecutor, host string, pid int) string` (unchanged), `ExtractEnvValue(ps, key string) string` (unchanged).
- Produces: **new signature** `resolveLogDir(executor HostExecutor, host, logDir, procInfo string) string` — Task 3 reuses the `procInfo` blob that `RunDiscovery` now holds in a local variable.

- [ ] **Step 1: Update TestResolveLogDir to the new signature**

Replace the whole `TestResolveLogDir` function in `cmd/root/collection/discovery_test.go` with:

```go
func TestResolveLogDir(t *testing.T) {
	const psBlob = "java -Ddremio.log.path=/opt/dremio/data/log DREMIO_LOG_DIR=/opt/dremio/log"

	// Helper to build a responder: dir existence/content checks per map.
	build := func(nonEmpty map[string]bool) HostExecutor {
		return func(_ string, args ...string) (string, error) {
			joined := strings.Join(args, " ")
			switch {
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

	// Explicit flag wins outright — no host commands at all.
	called := false
	exec1 := func(_ string, _ ...string) (string, error) {
		called = true
		return "", nil
	}
	if got := resolveLogDir(exec1, "h", "/explicit/path", psBlob); got != "/explicit/path" {
		t.Errorf("explicit: want /explicit/path, got %q", got)
	}
	if called {
		t.Errorf("explicit flag must not touch the host")
	}

	// -Ddremio.log.path used when its dir has files.
	if got := resolveLogDir(build(map[string]bool{"/opt/dremio/data/log": true}), "h", "", psBlob); got != "/opt/dremio/data/log" {
		t.Errorf("logpath: want /opt/dremio/data/log, got %q", got)
	}

	// Detected dir empty -> fall through to DREMIO_LOG_DIR (which has files).
	if got := resolveLogDir(build(map[string]bool{"/opt/dremio/log": true}), "h", "", psBlob); got != "/opt/dremio/log" {
		t.Errorf("fallthrough to DREMIO_LOG_DIR: want /opt/dremio/log, got %q", got)
	}

	// Nothing detected, nothing on disk -> probe returns "".
	if got := resolveLogDir(build(map[string]bool{}), "h", "", psBlob); got != "" {
		t.Errorf("none: want empty, got %q", got)
	}

	// Empty procInfo (PID unknown upstream) -> straight to probe.
	if got := resolveLogDir(build(map[string]bool{"/var/log/dremio": true}), "h", "", ""); got != "/var/log/dremio" {
		t.Errorf("empty procInfo probe: want /var/log/dremio, got %q", got)
	}
}
```

- [ ] **Step 2: Run to verify it fails to compile**

Run: `go test -short -run TestResolveLogDir ./cmd/root/collection/...`
Expected: FAIL (build error) — `cannot use psBlob (untyped string constant) as int value` (old signature takes `pid int`).

- [ ] **Step 3: Implement the refactor**

In `cmd/root/collection/discovery.go`, replace `resolveLogDir`:

```go
// resolveLogDir determines the Dremio log directory using, in order:
//  1. an explicit operator path (logDir), used as-is;
//  2. -Ddremio.log.path= from procInfo, if that dir has files;
//  3. DREMIO_LOG_DIR= from procInfo, if that dir has files;
//  4. probing the well-known candidate directories.
//
// procInfo is the process cmdline+env blob from readProcessInfo ("" when the
// Dremio PID is unknown).
func resolveLogDir(executor HostExecutor, host, logDir, procInfo string) string {
	if logDir != "" {
		return logDir
	}
	if procInfo != "" {
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

In `RunDiscovery`, change step 2 so the blob is read once (it will feed the secondary pass in Task 3). Replace:

```go
	// 2. Find the log directory: explicit flag > -Ddremio.log.path > DREMIO_LOG_DIR > probe.
	info.LogDir = resolveLogDir(executor, host, logDir, info.DremioPID)
```

with:

```go
	// 2. Read process info once — feeds log-dir resolution and the secondary
	// DREMIO_LOG_DIR pass below.
	procInfo := readProcessInfo(executor, host, info.DremioPID)

	// 3. Find the log directory: explicit flag > -Ddremio.log.path > DREMIO_LOG_DIR > probe.
	info.LogDir = resolveLogDir(executor, host, logDir, procInfo)
```

Renumber the subsequent step comments in `RunDiscovery` (config dir, RocksDB, checksum tool, gzip) accordingly.

Note the intentional behavior nuance: `readProcessInfo` now runs even when an explicit log-dir flag is set (one extra `ps eww` per node) — required so the secondary pass works with explicit flags. With mock executors that don't answer `ps eww`, `readProcessInfo` returns `""` and everything behaves as before, so no other existing test changes.

- [ ] **Step 4: Run the package tests**

Run: `go test -short ./cmd/root/collection/...`
Expected: PASS.

- [ ] **Step 5: Build**

Run: `go build -o bin/ddc.exe .`
Expected: exit 0.

- [ ] **Step 6: Commit checkpoint**

STOP. Summarize the diff for the user and wait for approval. Only on explicit approval:

```bash
git add cmd/root/collection/discovery.go cmd/root/collection/discovery_test.go
git commit -m "refactor: read process info once in RunDiscovery

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 3: Secondary DREMIO_LOG_DIR discovery (server.out + GC logs on split-dir clusters)

**Files:**
- Modify: `cmd/root/collection/discovery.go` (`RunDiscovery` log-listing block ~line 97–118; new helpers at file scope)
- Test: `cmd/root/collection/discovery_test.go`

**Interfaces:**
- Consumes: `procInfo` local from Task 2; `ExtractEnvValue`, `dirHasFiles`, `listFiles`, `baseName` (all existing, unchanged).
- Produces:
  - `reclassifyLogFiles(files []RemoteFileInfo)` — mutates `FileType` in place (gc → `"gc-log"`, `queries.` → `"queries"`); extracted from the inline switch in `RunDiscovery`.
  - `discoverSecondaryLogFiles(executor HostExecutor, host, dir string, existing []RemoteFileInfo) []RemoteFileInfo` — returns only `server.out*` (as `"log"`) and GC logs (as `"gc-log"`) from `dir`, skipping paths already in `existing`.

- [ ] **Step 1: Write the failing tests**

Add to `cmd/root/collection/discovery_test.go`:

```go
// TestRunDiscovery_SecondaryLogDir covers the split-dir cluster case:
// server.log follows -Ddremio.log.path while server.out and GC logs follow
// DREMIO_LOG_DIR. When the two differ, discovery must pick up server.out and
// GC logs (and nothing else) from the DREMIO_LOG_DIR directory.
func TestRunDiscovery_SecondaryLogDir(t *testing.T) {
	responses := map[string]struct {
		out string
		err error
	}{
		// PID cascade → PID 42.
		"jcmd -l":               {out: "", err: fmt.Errorf("not found")},
		"pgrep -x java":         {out: "", err: fmt.Errorf("exit status 1")},
		"pgrep -f dremio.*java": {out: "42\n", err: nil},
		// Process info: log.path and DREMIO_LOG_DIR diverge.
		"ps eww 42": {out: "java -Ddremio.log.path=/opt/dremio/data/log DREMIO_LOG_DIR=/opt/dremio/log", err: nil},
		// Primary dir (from -Ddremio.log.path) exists with files.
		"test -d /opt/dremio/data/log": {out: "exists", err: nil},
		"find -L /opt/dremio/data/log -maxdepth 1 -type f -print -quit": {out: "/opt/dremio/data/log/server.log\n", err: nil},
		"find -L /opt/dremio/data/log -maxdepth 2 -type f -exec stat": {
			out: "1711929600 1000 /opt/dremio/data/log/server.log\n",
			err: nil,
		},
		// Secondary dir (DREMIO_LOG_DIR) exists with a mix of files.
		"test -d /opt/dremio/log": {out: "exists", err: nil},
		"find -L /opt/dremio/log -maxdepth 1 -type f -print -quit": {out: "/opt/dremio/log/server.out\n", err: nil},
		"find -L /opt/dremio/log -maxdepth 1 -type f -exec stat": {
			out: "1711929600 2000 /opt/dremio/log/server.out\n" +
				"1711929600 3000 /opt/dremio/log/gc.log.0\n" +
				"1711929600 4000 /opt/dremio/log/access.log\n" +
				"1711929600 5000 /opt/dremio/log/hs_err_pid42.log\n",
			err: nil,
		},
		// Conf dir not found.
		"test -d /opt/dremio/conf": {out: "", err: fmt.Errorf("not found")},
		"test -d /etc/dremio":      {out: "", err: fmt.Errorf("not found")},
	}

	info, err := RunDiscovery(mockExecutor(responses), "node1", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.LogDir != "/opt/dremio/data/log" {
		t.Errorf("LogDir = %q, want /opt/dremio/data/log", info.LogDir)
	}

	typeByPath := make(map[string]string)
	for _, f := range info.Files {
		typeByPath[f.Path] = f.FileType
	}
	if ft := typeByPath["/opt/dremio/data/log/server.log"]; ft != "log" {
		t.Errorf("server.log FileType = %q, want log", ft)
	}
	if ft := typeByPath["/opt/dremio/log/server.out"]; ft != "log" {
		t.Errorf("server.out FileType = %q, want log", ft)
	}
	if ft := typeByPath["/opt/dremio/log/gc.log.0"]; ft != "gc-log" {
		t.Errorf("gc.log.0 FileType = %q, want gc-log", ft)
	}
	// access.log and hs_err live outside the launcher's writes — the secondary
	// pass must NOT vacuum them up from DREMIO_LOG_DIR.
	if _, ok := typeByPath["/opt/dremio/log/access.log"]; ok {
		t.Error("access.log from secondary dir must not be collected")
	}
	if _, ok := typeByPath["/opt/dremio/log/hs_err_pid42.log"]; ok {
		t.Error("hs_err from secondary dir must not be collected")
	}
	if len(info.Files) != 3 {
		t.Errorf("len(Files) = %d, want 3; files: %+v", len(info.Files), info.Files)
	}
}

// TestRunDiscovery_SecondaryLogDir_SameDir: when DREMIO_LOG_DIR equals the
// resolved log dir, no secondary listing runs.
func TestRunDiscovery_SecondaryLogDir_SameDir(t *testing.T) {
	responses := map[string]struct {
		out string
		err error
	}{
		"jcmd -l":               {out: "", err: fmt.Errorf("not found")},
		"pgrep -x java":         {out: "", err: fmt.Errorf("exit status 1")},
		"pgrep -f dremio.*java": {out: "42\n", err: nil},
		"ps eww 42":             {out: "java -Ddremio.log.path=/opt/dremio/log DREMIO_LOG_DIR=/opt/dremio/log", err: nil},
		"test -d /opt/dremio/log": {out: "exists", err: nil},
		"find -L /opt/dremio/log -maxdepth 1 -type f -print -quit": {out: "/opt/dremio/log/server.out\n", err: nil},
		"find -L /opt/dremio/log -maxdepth 2 -type f -exec stat": {
			out: "1711929600 1000 /opt/dremio/log/server.log\n" +
				"1711929600 2000 /opt/dremio/log/server.out\n",
			err: nil,
		},
		"test -d /opt/dremio/conf": {out: "", err: fmt.Errorf("not found")},
		"test -d /etc/dremio":      {out: "", err: fmt.Errorf("not found")},
	}

	// Wrap the mock to record any secondary (maxdepth 1 stat) listing.
	var secondaryListings int
	inner := mockExecutor(responses)
	recorder := func(host string, args ...string) (string, error) {
		if strings.Contains(strings.Join(args, " "), "-maxdepth 1 -type f -exec stat") {
			secondaryListings++
		}
		return inner(host, args...)
	}

	info, err := RunDiscovery(recorder, "node1", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if secondaryListings != 0 {
		t.Errorf("secondary listing ran %d times, want 0 (dirs are identical)", secondaryListings)
	}
	if len(info.Files) != 2 {
		t.Errorf("len(Files) = %d, want 2; files: %+v", len(info.Files), info.Files)
	}
}

// TestDiscoverSecondaryLogFiles_Dedup: paths already discovered (e.g. via a
// nested primary listing) are not returned twice.
func TestDiscoverSecondaryLogFiles_Dedup(t *testing.T) {
	responses := map[string]struct {
		out string
		err error
	}{
		"test -d /opt/dremio/log": {out: "exists", err: nil},
		"find -L /opt/dremio/log -maxdepth 1 -type f -print -quit": {out: "/opt/dremio/log/server.out\n", err: nil},
		"find -L /opt/dremio/log -maxdepth 1 -type f -exec stat": {
			out: "1711929600 2000 /opt/dremio/log/server.out\n" +
				"1711929600 3000 /opt/dremio/log/gc.log.0\n",
			err: nil,
		},
	}
	existing := []RemoteFileInfo{{Path: "/opt/dremio/log/server.out", Size: 2000, FileType: "log"}}
	got := discoverSecondaryLogFiles(mockExecutor(responses), "node1", "/opt/dremio/log", existing)
	if len(got) != 1 {
		t.Fatalf("expected 1 file after dedup, got %d: %v", len(got), filePaths(got))
	}
	if got[0].Path != "/opt/dremio/log/gc.log.0" || got[0].FileType != "gc-log" {
		t.Errorf("got %+v, want gc.log.0 classified as gc-log", got[0])
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test -short -run "TestRunDiscovery_SecondaryLogDir|TestDiscoverSecondaryLogFiles_Dedup" ./cmd/root/collection/...`
Expected: FAIL (build error) — `undefined: discoverSecondaryLogFiles`.

- [ ] **Step 3: Implement**

In `cmd/root/collection/discovery.go`:

(a) Extract the inline reclassification switch from `RunDiscovery` into a helper (place it near `filterFiles`):

```go
// reclassifyLogFiles rewrites the FileType of well-known log files so they
// are routed to the correct output directories.
func reclassifyLogFiles(files []RemoteFileInfo) {
	for i := range files {
		base := baseName(files[i].Path)
		switch {
		case strings.HasPrefix(base, "gc") && strings.Contains(base, ".log"):
			files[i].FileType = "gc-log"
		case strings.Contains(base, ".gc") || strings.HasSuffix(base, ".gc"):
			files[i].FileType = "gc-log"
		case strings.HasPrefix(base, "queries."):
			files[i].FileType = "queries"
		}
	}
}
```

(b) Add the secondary-pass helper:

```go
// discoverSecondaryLogFiles lists a DREMIO_LOG_DIR that differs from the
// resolved log dir and returns only the files the Dremio launcher writes
// there: server.out* (stdout redirect, bin/dremio) and GC logs. Everything
// else in that directory is ignored, and already-discovered paths are
// skipped. Failures are advisory — discovery continues without the dir.
func discoverSecondaryLogFiles(executor HostExecutor, host, dir string, existing []RemoteFileInfo) []RemoteFileInfo {
	if !dirHasFiles(executor, host, dir) {
		return nil
	}
	files, err := listFiles(executor, host, dir, "log", "1")
	if err != nil {
		simplelog.Warningf("discoverSecondaryLogFiles: failed to list %v on %v: %v", dir, host, err)
		return nil
	}
	reclassifyLogFiles(files)

	seen := make(map[string]bool, len(existing))
	for _, f := range existing {
		seen[f.Path] = true
	}
	var result []RemoteFileInfo
	for _, f := range files {
		base := baseName(f.Path)
		if !strings.HasPrefix(base, "server.out") && f.FileType != "gc-log" {
			continue
		}
		if seen[f.Path] {
			continue
		}
		result = append(result, f)
	}
	if len(result) > 0 {
		simplelog.Infof("discoverSecondaryLogFiles: %d file(s) from DREMIO_LOG_DIR %v on %v", len(result), dir, host)
	}
	return result
}
```

(c) In `RunDiscovery`, replace the inline reclassification loop inside the `info.LogDir != ""` block with a call to the helper — the block becomes:

```go
	if info.LogDir != "" {
		anySuccess = true
		files, err := listFiles(executor, host, info.LogDir, "log", "2")
		if err != nil {
			simplelog.Warningf("DiscoverFiles: failed to list log files on %v: %v", host, err)
		} else {
			// Reclassify well-known log files into specific types so they
			// are routed to the correct output directories.
			reclassifyLogFiles(files)
			info.Files = append(info.Files, files...)
		}
	}
```

(d) Immediately after that block, add the secondary pass:

```go
	// server.out and GC logs follow DREMIO_LOG_DIR (bin/dremio redirects
	// stdout there), while server.log/queries.json follow -Ddremio.log.path.
	// When the two differ, pick up the launcher-written files as well.
	if secondaryDir := ExtractEnvValue(procInfo, "DREMIO_LOG_DIR="); secondaryDir != "" && secondaryDir != info.LogDir {
		info.Files = append(info.Files, discoverSecondaryLogFiles(executor, host, secondaryDir, info.Files)...)
	}
```

- [ ] **Step 4: Run the package tests**

Run: `go test -short ./cmd/root/collection/...`
Expected: PASS — including the pre-existing `TestRunDiscovery_GCLogClassification` and `TestDiscoverFiles_FullInfo` (their mocks don't answer `ps eww`, so `procInfo` is `""` and the secondary pass is skipped).

- [ ] **Step 5: Build**

Run: `go build -o bin/ddc.exe .`
Expected: exit 0.

- [ ] **Step 6: Commit checkpoint**

STOP. Summarize the diff for the user and wait for approval. Only on explicit approval:

```bash
git add cmd/root/collection/discovery.go cmd/root/collection/discovery_test.go
git commit -m "feat: discover server.out and GC logs from DREMIO_LOG_DIR on split-dir clusters (#337)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 4: Rename TUI labels to "Server logs & out"

**Files:**
- Modify: `cmd/configui/configui.go` (~line 506 and ~line 694)

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: display-string change only; option keys (`"server"`) and bound variables are untouched.

- [ ] **Step 1: Make the two edits**

Diagnosis multi-select option (~line 506) — change:

```go
		huh.NewOption("Server logs", "server").Selected(cfg.CollectServerLogs),
```

to:

```go
		huh.NewOption("Server logs & out", "server").Selected(cfg.CollectServerLogs),
```

Standard-mode day-select (~line 694) — change:

```go
		huh.NewSelect[int]().Title("Server logs").Height(4).
```

to:

```go
		huh.NewSelect[int]().Title("Server logs & out").Height(4).
```

- [ ] **Step 2: Verify no other user-facing "Server logs" label remains**

Run: `grep -rn "\"Server logs\"" cmd/`
Expected: no matches. (A repo-wide search during design found only these two sites.)

- [ ] **Step 3: Run configui tests and build**

Run: `go test -short ./cmd/configui/...` then `go build -o bin/ddc.exe .`
Expected: PASS, exit 0. (No test asserts on the label text — verified during design.)

- [ ] **Step 4: Commit checkpoint**

STOP. Summarize the diff for the user and wait for approval. Only on explicit approval:

```bash
git add cmd/configui/configui.go
git commit -m "feat: label Server logs collection as 'Server logs & out' in TUI (#337)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 5: Full verification — test suite, lint, live runs against dremio-master-0

**Files:**
- No source changes. Read-only cluster use.

**Interfaces:**
- Consumes: `bin/ddc.exe` built from Tasks 1–4.
- Produces: verified tarballs proving `server.out` collection end-to-end in both modes.

- [ ] **Step 1: Full unit-test suite and lint**

Run:
```powershell
go fmt ./...
go test -short ./...
golangci-lint run
```
Expected: fmt makes no changes to files you didn't touch, tests PASS, lint clean.

- [ ] **Step 2: Live standard-mode run**

`dremio-master-0` lives in namespace `default` of the current kube context. Its `server.out` is `/opt/dremio/log/server.out` (~19 KB). Run:

```powershell
bin\ddc.exe collect k8s standard -n default --nodes dremio-master-0 --output-file "$env:TEMP\ddc-serverout-std.tgz" --collect-queries-perf-json=false
tar -tzf "$env:TEMP\ddc-serverout-std.tgz" | Select-String "server.out"
```

Expected: one line ending in `logs/server.out` under the `dremio-master-0` node directory. Also extract `ddc.log` from the tarball and confirm it contains a `stream start:` line for `server.out` and NO `stream exclude (blocked): …server.out` line.

- [ ] **Step 3: Live diagnosis-mode run**

Diagnosis with the JVM tools disabled (fast, low-impact — mirrors the reproduction command from issue #337) and `--days=1` (the date filter must not drop `server.out`; note its mtime on this cluster is recent, so the authoritative proof of the exemption is `TestLogDayLimit_ServerOut` — this run proves end-to-end collection):

```powershell
bin\ddc.exe collect k8s diagnosis -n default --nodes dremio-master-0 --days=1 --diag-jfr=false --diag-jstack=false --diag-top=false --diag-async-profiler=false --diag-heap-dump=false --collect-queries-perf-json=false --collect-kvstore-report=false --collect-problematic-profiles=false --output-file "$env:TEMP\ddc-serverout-diag.tgz"
tar -tzf "$env:TEMP\ddc-serverout-diag.tgz" | Select-String "server.out"
```

Expected: one line ending in `logs/server.out`.

- [ ] **Step 4: Report results to the user**

Summarize: unit results, lint, both tarball listings (the `server.out` lines), and confirm the binary was built. STOP for user review before any further action (e.g. closing the issue, PR).
