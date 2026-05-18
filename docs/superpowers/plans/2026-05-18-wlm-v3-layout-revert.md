# WLM v3 Layout Revert — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Drop the `wlm_` prefix from WLM output filenames so DDC writes `wlm/<host>/queues.json`, `wlm/<host>/rules.json`, `wlm/<host>/engines.json`, `wlm/<host>/cluster_usage.json` — matching the v3 layout that downstream consumers depend on. Per-host folder layout is preserved.

**Architecture:** Single source-line change in `cmd/root/collection/rockscollect.go` plus one new test in `cmd/root/collection/rockscollect_test.go`. The remote `dremio-rocksdb-viewer` binary's `-type` argument (`wlm_queues`, etc.) stays the same — only the local on-disk filename is rewritten via `strings.TrimPrefix`. Test reuses the existing `mockStreamCollector` and `mockCopyStrategy` in the `collection` package.

**Tech Stack:** Go 1.24, standard library (`strings`, `os`, `path/filepath`), `testing`.

**Spec:** `docs/superpowers/specs/2026-05-18-wlm-v3-layout-revert-design.md`

**Commit policy:** Per user instruction, do **not** create commits per-task. The final step squashes the spec, plan, source change, and test into a single commit.

---

## File Structure

**Modify:**
- `cmd/root/collection/rockscollect.go` — one-line change at line 230 (the `fname` derivation inside the `if args.CollectWLM` loop).

**Create (in new test cases only — file already exists):**
- `cmd/root/collection/rockscollect_test.go` — append a new `TestWLMFileLayout` function.

**Reused (read-only):**
- `cmd/root/collection/streaming_collect_test.go` — provides `mockStreamCollector` and `mockCopyStrategy`, both in `package collection`, so directly usable from the new test without re-declaring.

**No changes:**
- `docs/superpowers/specs/2026-05-18-wlm-v3-layout-revert-design.md` (already written, will be included in the final commit).
- README, FAQ, other docs — WLM filenames do not appear there.

---

### Task 1: Write the failing test

**Files:**
- Modify: `cmd/root/collection/rockscollect_test.go` (append new function at end of file)

**Context for the implementer:**
- `mockStreamCollector` (in `streaming_collect_test.go`) is in the same package and has these relevant hooks: `hostExecuteFunc func(mask bool, host string, args ...string) (string, error)` and `copyToHostFunc func(host, local, remote string) (string, error)`.
- `mockCopyStrategy.CreatePath(fileType, source, _)` returns `<tmpDir>/<fileType>/<source>` and `MkdirAll`s it — so `CreatePath("wlm", "dremio-master-0", "coordinator")` yields `<tmpDir>/wlm/dremio-master-0`.
- `RunRocksDBCollection` always tries to collect cluster_stats first (regardless of flags), so the `HostExecute` stub must respond to `-type cluster_stats` too.
- `RunRocksDBCollection` invokes (in order): `uname -m`, `CopyToHost(... rocksdb-viewer binary)`, `chmod +x ...`, `<bin> -db <path> -type cluster_stats`, then per WLM type `<bin> -db <path> -type wlm_<thing>`, then `rm -f ...` in defer.

- [ ] **Step 1: Append the failing test**

Append to `cmd/root/collection/rockscollect_test.go` (don't remove existing tests):

```go
func TestWLMFileLayout(t *testing.T) {
	tmpDir := t.TempDir()
	cs := &mockCopyStrategy{tmpDir: tmpDir}

	wlmPayloads := map[string]string{
		"wlm_queues":        `{"queues":[]}`,
		"wlm_rules":         `{"rules":[]}`,
		"wlm_engines":       `{"engines":[]}`,
		"wlm_cluster_usage": `{"cluster_usage":[]}`,
	}

	mc := &mockStreamCollector{
		coordinators: []string{"dremio-master-0"},
		hostExecuteFunc: func(_ bool, _ string, args ...string) (string, error) {
			// The viewer commands arrive as a single shell string in args[0].
			cmd := strings.Join(args, " ")
			switch {
			case strings.Contains(cmd, "uname -m"):
				return "x86_64\n", nil
			case strings.Contains(cmd, "chmod +x"):
				return "", nil
			case strings.Contains(cmd, "rm -f"):
				return "", nil
			case strings.Contains(cmd, "-type cluster_stats"):
				return `{"cluster":"stub"}`, nil
			}
			for wt, payload := range wlmPayloads {
				if strings.Contains(cmd, "-type "+wt) {
					return payload, nil
				}
			}
			return "", fmt.Errorf("unexpected host command: %s", cmd)
		},
		copyToHostFunc: func(_, _, _ string) (string, error) { return "", nil },
	}

	args := RocksCollectArgs{
		Collector:           mc,
		CopyStrategy:        cs,
		Host:                "dremio-master-0",
		NodeType:            "coordinator",
		RocksDBDir:          "/opt/dremio/data/db",
		CollectSystemTables: false,
		CollectWLM:          true,
		CollectQueriesPerf:  false,
	}

	if _, err := RunRocksDBCollection(args); err != nil {
		t.Fatalf("RunRocksDBCollection failed: %v", err)
	}

	wlmDir := filepath.Join(tmpDir, "wlm", "dremio-master-0")
	wantFiles := map[string]string{
		"queues.json":        `{"queues":[]}`,
		"rules.json":         `{"rules":[]}`,
		"engines.json":       `{"engines":[]}`,
		"cluster_usage.json": `{"cluster_usage":[]}`,
	}
	for name, wantContent := range wantFiles {
		path := filepath.Join(wlmDir, name)
		got, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("expected file %s: %v", path, err)
			continue
		}
		if string(got) != wantContent {
			t.Errorf("content mismatch for %s: got %q want %q", name, string(got), wantContent)
		}
	}

	// Negative: no v4-style filenames should remain under wlm/<host>/.
	leaks, err := filepath.Glob(filepath.Join(wlmDir, "wlm_*.json"))
	if err != nil {
		t.Fatalf("glob failed: %v", err)
	}
	if len(leaks) != 0 {
		t.Errorf("unexpected v4-style filenames present: %v", leaks)
	}
}
```

Verify the `import` block in `rockscollect_test.go` includes `"strings"`. The current import block is:
```go
import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)
```
Add `"strings"` so it becomes:
```go
import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)
```

- [ ] **Step 2: Run the test to confirm it fails**

Run from the repo root:
```
go test -short -run TestWLMFileLayout ./cmd/root/collection/...
```

Expected: **FAIL**. The test should report at least four errors of the form:
```
expected file <tmpDir>/wlm/dremio-master-0/queues.json: open ...: no such file or directory
```
and one leak error listing the four `wlm_*.json` files that are present instead.

If the test fails for any *other* reason (e.g., "unexpected host command" or a panic), stop and fix the test before touching `rockscollect.go`.

---

### Task 2: Apply the source change

**Files:**
- Modify: `cmd/root/collection/rockscollect.go` (one line, around line 230)

- [ ] **Step 1: Apply the edit**

Locate the WLM loop in `RunRocksDBCollection`:

```go
// Collect WLM
if args.CollectWLM {
    for _, wt := range wlmTypes {
        consoleprint.UpdateNodeState(consoleprint.NodeState{
            Node:     host,
            StatusUX: fmt.Sprintf("Collecting WLM: %s", wt),
        })
        fname := fmt.Sprintf("%s.json", wt)
        if cf, err := collectRocksType(c, args.CopyStrategy, host, args.NodeType, dbPath, wt, "wlm", fname); err != nil {
```

Change exactly one line — the `fname` derivation:

**Before:**
```go
fname := fmt.Sprintf("%s.json", wt)
```

**After:**
```go
fname := strings.TrimPrefix(wt, "wlm_") + ".json"
```

Do not change `wlmTypes`, do not change the `collectRocksType` call's `strategyType` argument (`"wlm"`), and do not touch `cluster-stats`, `system-tables`, or `queries-perf` paths. `strings` is already imported in this file (verified at `cmd/root/collection/rockscollect.go:23`), so no import change is needed.

- [ ] **Step 2: Run the test to confirm it now passes**

Run:
```
go test -short -run TestWLMFileLayout ./cmd/root/collection/...
```

Expected: **PASS**. Output:
```
ok  	github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/collection	<duration>
```

---

### Task 3: Verify no regressions in the rest of the package

- [ ] **Step 1: Run the full collection package test suite**

Run:
```
go test -short ./cmd/root/collection/...
```

Expected: **PASS** for all tests in the package, including the existing `TestDateSplitWriter_SingleDay`, `TestDateSplitWriter_MultipleDays`, `TestStreamingCollect_EndToEnd`, and any others. Total test count should be the previous count + 1 (the new `TestWLMFileLayout`).

If anything else fails, stop and investigate. The change is one line and should not affect anything outside the `if args.CollectWLM` block.

- [ ] **Step 2: Run the full repo test suite**

Run:
```
go test -short ./...
```

Expected: all packages PASS. (Per CLAUDE.md, do not use `-race` on Windows.)

Watch specifically for any test that asserts `wlm_queues.json`, `wlm_rules.json`, etc. as a literal path — there should be none, but if one exists it needs updating. (The pre-flight grep in the spec showed only `rockscollect.go` and a historical plan doc reference the `wlm_*` strings.)

---

### Task 4: Build the binary

Per CLAUDE.md, the binary must be rebuilt after every code change.

- [ ] **Step 1: Build**

Run:
```
go build -o bin/ddc.exe .
```

Expected: exit 0, `bin/ddc.exe` written. No build errors or warnings.

- [ ] **Step 2: Confirm in chat**

Report to the user: "Build succeeded — `bin/ddc.exe` updated."

---

### Task 5: Lint

- [ ] **Step 1: Run `go fmt`**

Run:
```
go fmt ./cmd/root/collection/...
```

Expected: empty output (files already formatted) or a single line listing reformatted files. If files were reformatted, inspect the diff to make sure only whitespace changed.

- [ ] **Step 2: Run `go vet`**

Run:
```
go vet ./cmd/root/collection/...
```

Expected: empty output, exit 0.

---

### Task 6: Final squashed commit (manual handoff to user)

**Do not run `git commit` automatically.** Per the user's standing rule ("Review before commit") and the explicit instruction for this task ("one squashed commit incl design and plan"), present the diff and let the user trigger the commit.

- [ ] **Step 1: Show the user the staged change set**

Run, in order:
```
git status
git diff --stat
git diff
```

The change set should contain exactly these files:
- `docs/superpowers/specs/2026-05-18-wlm-v3-layout-revert-design.md` (new)
- `docs/superpowers/plans/2026-05-18-wlm-v3-layout-revert.md` (new — this file)
- `cmd/root/collection/rockscollect.go` (1-line change)
- `cmd/root/collection/rockscollect_test.go` (new test function + `"strings"` import)

If anything else appears in the diff, stop and investigate — the change should be tightly bounded.

- [ ] **Step 2: Ask the user to approve the commit**

Tell the user:
> "Ready to commit. Proposed message:
>
> ```
> Revert WLM output filenames to v3 layout
>
> Drop the wlm_ prefix from WLM output filenames so DDC writes
> wlm/<host>/queues.json, wlm/<host>/rules.json, wlm/<host>/engines.json,
> wlm/<host>/cluster_usage.json — matching the v3 layout that downstream
> consumers depend on. Per-host folder layout is preserved.
>
> The remote dremio-rocksdb-viewer binary's -type argument is unchanged;
> only the local on-disk filename is rewritten via strings.TrimPrefix.
>
> Adds TestWLMFileLayout asserting the exact on-disk paths and content,
> plus a negative assertion that no wlm_*.json filenames remain.
> ```
>
> One commit, all four files. OK to proceed?"

- [ ] **Step 3: On user approval, run the commit**

```
git add docs/superpowers/specs/2026-05-18-wlm-v3-layout-revert-design.md docs/superpowers/plans/2026-05-18-wlm-v3-layout-revert.md cmd/root/collection/rockscollect.go cmd/root/collection/rockscollect_test.go
git commit -m "$(cat <<'EOF'
Revert WLM output filenames to v3 layout

Drop the wlm_ prefix from WLM output filenames so DDC writes
wlm/<host>/queues.json, wlm/<host>/rules.json, wlm/<host>/engines.json,
wlm/<host>/cluster_usage.json — matching the v3 layout that downstream
consumers depend on. Per-host folder layout is preserved.

The remote dremio-rocksdb-viewer binary's -type argument is unchanged;
only the local on-disk filename is rewritten via strings.TrimPrefix.

Adds TestWLMFileLayout asserting the exact on-disk paths and content,
plus a negative assertion that no wlm_*.json filenames remain.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
git status
```

Expected: one new commit on `ddc_v4`, working tree clean.

Do **not** push.
