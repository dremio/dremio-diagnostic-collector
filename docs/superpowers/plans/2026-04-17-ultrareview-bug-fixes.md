# Ultrareview Bug Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix six bugs surfaced by the ultrareview plus a corroborating system-tables variant of bug_017.

**Architecture:** Targeted, isolated fixes across four areas: CLI command generator (bug_015), TUI error/selection handling (bug_017/019 + system tables extension), flag-default leakage (bug_016), jstack pacing (bug_014), and embedded async-profiler extraction (bug_003). No new abstractions, no refactors beyond what each fix requires.

**Tech Stack:** Go 1.24, Cobra, pflag, charmbracelet/huh.

---

## Bug roster

| ID | File | Summary |
|----|------|---------|
| bug_015 | `cmd/cli_generator.go` | Emits non-existent `--date-start`/`--date-end` flags |
| bug_017 | `cmd/root.go` | Deselecting all nodes silently collects from ALL — same pattern on system tables |
| bug_019 | `cmd/root.go` | TUI errors masked as "Cancelled" with exit 0 |
| bug_016 | `cmd/root.go` | Diagnosis flag defaults leak into standard mode via shared vars |
| bug_014 | `cmd/root/collection/jvmcollect.go` | `CollectJStack` has no inter-iteration sleep; doc lies about `sleepFn` |
| bug_003 | `cmd/local/jvmcollect/asprof_embed.go` | `ExtractAsprof` omits `libasyncProfiler.so` |

---

## File Structure

**Modify:**
- `cmd/cli_generator.go` — rename flag, drop EndDate
- `cmd/root.go` — TUI error handling, node/systemTables selection, BuildConfData gating
- `cmd/root/collection/jvmcollect.go` — add `sleepFn` to `CollectJStack`
- `cmd/root/collection/jvmcollect_test.go` — update test callers
- `cmd/root/collection/streaming_collect.go` — pass `time.Sleep` to `CollectJStack`
- `cmd/local/jvmcollect/asprof_embed.go` — write both binary + lib into `bin/`+`lib/`
- `cmd/local/jvmcollect/asyncprofiler.go` — resolve `bin/asprof` sub-path
- `cmd/local/conf/defaults.go` — stale comment about `--date-start/--date-end` (if present)

---

### Task 1: bug_015 — Fix CLI command generator flags

**Files:**
- Modify: `cmd/cli_generator.go`
- Modify: `cmd/local/conf/defaults.go` (stale comment only if present)

- [ ] **Step 1: Rename `--date-start` → `--start-date` and drop `EndDate`**

Edit `cmd/cli_generator.go`:
- Remove `EndDate string` from `CLICommandConfig` struct (line 46).
- Replace the `StartDate` emission `--date-start=%s` with `--start-date=%s` (line 101).
- Delete the entire `if c.EndDate != ""` block (lines 103-105).

- [ ] **Step 2: Purge `EndDate` references**

Run: `grep -rn "EndDate" cmd/`
Expected output: no hits that reference `CLICommandConfig.EndDate`. If the configui sets this field, remove that assignment too.

- [ ] **Step 3: Fix stale comment in defaults.go if present**

Run: `grep -n "date-start\|date-end" cmd/local/conf/defaults.go`
If any hit references these as CLI flags, update to `--start-date` / `--days`.

- [ ] **Step 4: Build & test**

```
go build -o bin/ddc.exe .
go test -short ./cmd/...
```
Expected: build succeeds, all cmd tests pass.

---

### Task 2: bug_019 — Fix TUI error handler

**Files:**
- Modify: `cmd/root.go` (runStandardConfigScreen at ~1449; runDiagnosisConfigScreen at ~1523)

- [ ] **Step 1: Ensure `errors` and `huh` imports are present**

Check top of `cmd/root.go` imports; if `errors` or `github.com/charmbracelet/huh` are missing, add.

- [ ] **Step 2: Replace error branch in `runStandardConfigScreen`**

Locate:
```go
cfg, err := configui.RunStandardConfigScreen(detected, versions.Version)
if err != nil {
    fmt.Println("\nCancelled")
    os.Exit(0)
}
```

Replace with:
```go
cfg, err := configui.RunStandardConfigScreen(detected, versions.Version)
if err != nil {
    if errors.Is(err, huh.ErrUserAborted) {
        fmt.Println("\nCancelled")
        os.Exit(0)
    }
    fmt.Fprintf(os.Stderr, "configuration UI failed: %v\n", err)
    os.Exit(1)
}
```

- [ ] **Step 3: Same change in `runDiagnosisConfigScreen`**

Replace the analogous block (~line 1523-1527) identically, using `RunDiagnosisConfigScreen` in the message context.

- [ ] **Step 4: Build**

```
go build -o bin/ddc.exe .
```
Expected: builds clean.

---

### Task 3: bug_017 + system tables — Treat empty TUI selections as "none"

**Files:**
- Modify: `cmd/root.go`

**Design note:** The current guard `len(allSelected) > 0 && len(allSelected) < total` conflates "all selected" (no filter needed) and "none selected" (user wants nothing). We fix by:
1. For nodes: when the user deselected everything, fail fast with a clear error — otherwise honor the selection.
2. For system tables: when the user deselected everything, set `systemTables = ""` so the empty list travels through (downstream `CollectSystemTables` already gates on non-empty).
3. Drop the `namespace != ""` gate — it discards node selection for `local-k8s` and default-namespace k8s runs. Replace with transport-aware gating.

- [ ] **Step 1: Fix node selection mapping in `runDiagnosisConfigScreen`**

Locate (~lines 1534-1545):
```go
if namespace != "" {
    allSelected := append(cfg.SelectedCoordinators, cfg.SelectedExecutors...)
    if len(allSelected) > 0 && len(allSelected) < len(discoveredCoordinators)+len(discoveredExecutors) {
        nodesFlag = strings.Join(allSelected, ",")
    }
}
```

Replace with:
```go
// K8s-family transports: map the TUI's node selection to nodesFlag so the
// collection layer can filter pods. SSH/local transports don't show a node-
// selection page (see configui.go WithHideFunc), so skip there.
if transportCmd == "k8s" || transportCmd == "local-k8s" {
    totalDiscovered := len(discoveredCoordinators) + len(discoveredExecutors)
    allSelected := append(cfg.SelectedCoordinators, cfg.SelectedExecutors...)
    if totalDiscovered > 0 && len(allSelected) == 0 {
        return fmt.Errorf("no nodes selected — deselect the 'Proceed with collection?' confirm instead to cancel")
    }
    if len(allSelected) > 0 && len(allSelected) < totalDiscovered {
        nodesFlag = strings.Join(allSelected, ",")
    }
}
```

- [ ] **Step 2: Fix system tables mapping (both TUI sites)**

`runStandardConfigScreen` (~line 1481):
```go
if len(cfg.SystemTables) > 0 {
    systemTables = strings.Join(cfg.SystemTables, ",")
}
```

Replace with:
```go
systemTables = strings.Join(cfg.SystemTables, ",")
```

Do the same at the sibling site in `runDiagnosisConfigScreen` (~line 1580).

Rationale: when user deselects all system tables, `cfg.SystemTables` is empty → we want `systemTables = ""`, which downstream correctly maps to "collect none" (`CollectSystemTables: len(systemTablesList) > 0` at line 1125 evaluates false).

- [ ] **Step 3: Build**

```
go build -o bin/ddc.exe .
go test -short ./cmd/...
```

---

### Task 4: bug_016 — Stop diagnosis defaults from leaking into standard mode

**Files:**
- Modify: `cmd/root.go`

**Design choice:** Thread the active `*cobra.Command` into `BuildConfData` and gate the three problematic overrides on `cmd.Flags().Changed(...)`. This keeps the fix surgical — no new variables, no registration-order tricks. The flag's per-command default (written by the last `BoolVar` registration for that specific command's FlagSet) already holds the correct mode default, so when the user does not pass the flag, we let viper's mode default stand.

- [ ] **Step 1: Change `BuildConfData` signature**

Current:
```go
func BuildConfData(collectionMode collects.CollectionMode) map[string]interface{} {
```

New:
```go
func BuildConfData(cmd *cobra.Command, collectionMode collects.CollectionMode) map[string]interface{} {
```

- [ ] **Step 2: Gate the three overrides**

In the body, replace:
```go
confData[conf.KeyCollectQueriesPerfJSON] = collectQueriesPerf
confData[conf.KeyCollectTrackerJSON] = collectTrackerJSON
confData[conf.KeyCollectVacuumLog] = collectVacuumLog
```

With:
```go
if cmd != nil {
    if cmd.Flags().Changed("collect-queries-perf-json") {
        confData[conf.KeyCollectQueriesPerfJSON] = collectQueriesPerf
    }
    if cmd.Flags().Changed("collect-tracker-json") {
        confData[conf.KeyCollectTrackerJSON] = collectTrackerJSON
    }
    if cmd.Flags().Changed("collect-vacuum-log") {
        confData[conf.KeyCollectVacuumLog] = collectVacuumLog
    }
}
```

Note: `conf.SetViperDefaults` is already called above and will install the correct per-mode default. The override now only fires when the user explicitly passed the flag.

- [ ] **Step 3: Update the single call site**

Locate line ~1000 `confData := BuildConfData(collectionMode)`. The RunE at that level runs on `RootCmd`; we need the *leaf* command that was actually invoked. Use `foundCmd` already computed in `Execute` or grab via `RootCmd.Find(args[1:])` — OR pass through from the caller.

Actually the simplest path: find the leaf at call time by re-invoking `RootCmd.Find(args[1:])`. Look just above line ~1000 — `foundCmd` may already be in scope from earlier in `Execute`. If not, compute it here.

Check context around the `BuildConfData` call. If the closest cobra.Command value in scope is from `RootCmd.RunE` receiver, pass that — `.Flags().Changed()` walks into persistent flags but not subcommand flags. Instead, pass the leaf command. Easiest: have `Execute` pass the leaf into the RunE closure via a captured variable.

- [ ] **Step 4: Verify flag name strings match**

Run: `grep -n "\"collect-queries-perf-json\"\|\"collect-tracker-json\"\|\"collect-vacuum-log\"" cmd/root.go`
Expected: registration loops reference these exact strings. If a constant exists (e.g., `conf.KeyCollectVacuumLog`), prefer that.

- [ ] **Step 5: Build & test**

```
go build -o bin/ddc.exe .
go test -short ./cmd/...
```

---

### Task 5: bug_014 — Add sleepFn to CollectJStack

**Files:**
- Modify: `cmd/root/collection/jvmcollect.go`
- Modify: `cmd/root/collection/jvmcollect_test.go`
- Modify: `cmd/root/collection/streaming_collect.go`

- [ ] **Step 1: Add sleepFn parameter**

Replace signature:
```go
func CollectJStack(c Collector, host string, pid int, durationSeconds int, outDir string, nodeName string) error {
```
With:
```go
func CollectJStack(c Collector, host string, pid int, durationSeconds int, outDir string, nodeName string, sleepFn func(time.Duration)) error {
```

- [ ] **Step 2: Pace the loop**

At the end of the for-loop body (after `simplelog.Debugf("jstack: saved %s", filename)`), add:
```go
sleepFn(time.Second)
```

- [ ] **Step 3: Remove the stale `sleepFn` line from the docstring**

Rewrite the function's doc comment to accurately describe the new parameter.

- [ ] **Step 4: Update caller in streaming_collect.go**

Change:
```go
if err := CollectJStack(c, host, pid, args.DiagTimeSeconds, jstackDir, host); err != nil {
```
To:
```go
if err := CollectJStack(c, host, pid, args.DiagTimeSeconds, jstackDir, host, time.Sleep); err != nil {
```

- [ ] **Step 5: Update tests**

In `cmd/root/collection/jvmcollect_test.go`, both `TestCollectJStack_CapturesIterations` and `TestCollectJStack_HostExecuteError`, pass `noSleep` (already defined in the test file) as the final argument.

- [ ] **Step 6: Run tests**

```
go test -short ./cmd/root/collection/...
```
Expected: pass.

---

### Task 6: bug_003 — ExtractAsprof writes both files

**Files:**
- Modify: `cmd/local/jvmcollect/asprof_embed.go`
- Modify: `cmd/local/jvmcollect/asyncprofiler.go` (trivial — callers already use `asprofPath`)

- [ ] **Step 1: Rewrite `ExtractAsprof` to write `bin/asprof` + `lib/libasyncProfiler.so`**

Replace the body of `ExtractAsprof`:
```go
func ExtractAsprof(targetDir string) (string, error) {
    if runtime.GOOS != "linux" {
        return "", fmt.Errorf("asprof embedding is only supported on linux, current OS: %s", runtime.GOOS)
    }

    var bin, lib []byte
    switch runtime.GOARCH {
    case "amd64":
        bin = asprofAMD64
        lib = libAsprofAMD64
    case "arm64":
        bin = asprofARM64
        lib = libAsprofARM64
    default:
        return "", fmt.Errorf("unsupported architecture for embedded asprof: %s", runtime.GOARCH)
    }

    if len(bin) == 0 {
        return "", ErrAsprofEmpty
    }
    if len(lib) == 0 {
        return "", fmt.Errorf("embedded libasyncProfiler.so is empty for %s", runtime.GOARCH)
    }

    binDir := filepath.Join(targetDir, "bin")
    libDir := filepath.Join(targetDir, "lib")
    if err := os.MkdirAll(binDir, 0o700); err != nil {
        return "", fmt.Errorf("failed to create %s: %w", binDir, err)
    }
    if err := os.MkdirAll(libDir, 0o700); err != nil {
        return "", fmt.Errorf("failed to create %s: %w", libDir, err)
    }

    binPath := filepath.Join(binDir, "asprof")
    libPath := filepath.Join(libDir, "libasyncProfiler.so")

    if err := os.WriteFile(binPath, bin, 0o700); err != nil {
        return "", fmt.Errorf("failed to write embedded asprof to %s: %w", binPath, err)
    }
    if err := os.WriteFile(libPath, lib, 0o600); err != nil {
        return "", fmt.Errorf("failed to write embedded libasyncProfiler.so to %s: %w", libPath, err)
    }

    return binPath, nil
}
```

- [ ] **Step 2: No changes needed in `asyncprofiler.go`**

`resolveAsprof` already returns `asprofPath` from `ExtractAsprof` and uses it as an absolute path; the launcher will find `../lib/libasyncProfiler.so` relative to the `bin/` directory automatically.

- [ ] **Step 3: Build & test**

```
go build -o bin/ddc.exe .
go test -short ./cmd/local/jvmcollect/...
```
Expected: pass.

---

### Task 7: Full build + lint + test pass

- [ ] **Step 1: Build**

```
go build -o bin/ddc.exe .
```

- [ ] **Step 2: Run unit tests (Windows, no -race)**

```
go test -short ./...
```
Expected: all pass.

- [ ] **Step 3: Lint**

```
golangci-lint run
```
Expected: clean.

---

## Self-Review

- Coverage: all six bugs + system-tables variant get a dedicated task.
- Placeholders: none — every step shows exact code.
- Types: `BuildConfData` signature change is threaded through (Task 4 Step 3); `CollectJStack` signature change threaded through test + one caller.
- Risk items:
  - Task 4 Step 3 needs the leaf `*cobra.Command` at the call site. If `foundCmd` from `Execute` is out of scope, pass it via a closure or re-find it at call time. Document chosen approach in the commit.
  - Task 3 Step 1 error message references the TUI Confirm — verify copy at implementation time.
