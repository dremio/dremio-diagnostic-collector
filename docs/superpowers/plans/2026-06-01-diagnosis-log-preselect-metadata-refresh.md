# Diagnosis log preselection + metadata_refresh.log Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Preselect five log types by default in diagnosis mode (Tracker JSON, Vacuum, Hive deprecated, Acceleration, Access), and make `metadata_refresh.log` a first-class toggleable log type (standard off by default, diagnosis on by default) across CLI and TUI.

**Architecture:** Defaults live in `cmd/local/conf/defaults.go` and propagate to CLI flag defaults (`cmd/root.go`) and TUI checkboxes (`cmd/configui/configui.go`) via `DiagnosisDefaultMap()`/`StandardDefaultMap()`. The streaming collector gates each discovered `FileType "log"` file through `isLogTypeEnabled` (both modes) and `isLogAllowedInStandardMode` (standard hard allowlist) in `cmd/root/collection/streaming_collect.go`. Hive-deprecated is currently dead (no flag, hardcoded `false` in `Args`, unmapped TUI value) and must be wired end-to-end; `metadata_refresh` reuses the existing `collect-meta-refresh-log` conf key.

**Tech Stack:** Go, Cobra (CLI), charmbracelet/huh (TUI), table-driven Go tests.

**Spec:** `docs/superpowers/specs/2026-06-01-diagnosis-log-preselect-metadata-refresh-design.md`

**Build/test notes (Windows):** Use `go test -short ./...` (no `-race` on Windows). Always finish with `go build -o bin/ddc.exe .`. Per project rule, do NOT `git commit` unless the user explicitly asks — the commit steps below are written for completeness, but defer to the user.

---

### Task 1: Flip the five diagnosis-mode log defaults

**Files:**
- Modify: `cmd/local/conf/defaults.go` (in `DiagnosisCollectionProfile`)
- Test: `cmd/local/conf/defaults_test.go:45-55`

- [ ] **Step 1: Update the diagnosis defaults test to expect the new values**

In `cmd/local/conf/defaults_test.go`, in `TestSetViperDefaultsWithDiagnosis`, change these four lines (currently `false`) to `true`:

```go
		{conf.KeyCollectAccelerationLog, true},
		{conf.KeyCollectAccessLog, true},
```
and
```go
		{conf.KeyCollectVacuumLog, true},
```
and
```go
		{conf.KeyCollectTrackerJSON, true},
		{conf.KeyCollectHiveDeprecatedLog, true},
```

Leave `TestSetViperDefaultsWithStandard` unchanged (standard stays `false` for all five).

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -short -run TestSetViperDefaultsWithDiagnosis ./cmd/local/conf/...`
Expected: FAIL — e.g. `Unexpected value for 'collect-acceleration-log'. Got false, Expected true`.

- [ ] **Step 3: Flip the defaults in `DiagnosisCollectionProfile`**

In `cmd/local/conf/defaults.go`, inside `DiagnosisCollectionProfile`, change the five `setDefault` calls from `false` to `true`:

```go
	setDefault(confData, KeyCollectVacuumLog, true)
	setDefault(confData, KeyCollectAccelerationLog, true)
	setDefault(confData, KeyCollectAccessLog, true)
```
and in the "New log types for v4" block:
```go
	// New log types for v4
	setDefault(confData, KeyCollectTrackerJSON, true)
	setDefault(confData, KeyCollectHiveDeprecatedLog, true)
```

Do NOT touch `StandardCollectionProfile`.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test -short -run "TestSetViperDefaultsWithDiagnosis|TestSetViperDefaultsWithStandard" ./cmd/local/conf/...`
Expected: PASS (both).

- [ ] **Step 5: Commit**

```bash
git add cmd/local/conf/defaults.go cmd/local/conf/defaults_test.go
git commit -m "feat: preselect tracker/vacuum/acceleration/access/hive logs in diagnosis mode"
```

---

### Task 2: Wire Hive deprecated log end-to-end (CLI flag + Args + TUI mapping)

Hive-deprecated has a conf key, default (now `true` after Task 1), gating case (`hive-deprecated.` in `isLogTypeEnabled`), and a TUI checkbox — but no global var, no CLI flag, `Args.CollectHiveDeprecated` is hardcoded `false`, and the TUI value is never mapped to a global. This task connects all of it.

**Files:**
- Modify: `cmd/root.go` (global var block ~line 121; Args literal line 1211; diagnosis flag loop ~line 1391; diagnosis TUI→global map ~line 1674)
- Modify: `cmd/configui/configui.go` (diagnosis CLI builder ~line 899)
- Test: `cmd/root_flags_test.go`

- [ ] **Step 1: Write the failing flag-registration test**

Add to `cmd/root_flags_test.go`:

```go
func TestHiveDeprecatedOnlyOnDiagnosis(t *testing.T) {
	if SSHDiagnosisCmd.Flags().Lookup("collect-hive-deprecated-log") == nil {
		t.Error("SSHDiagnosisCmd should have --collect-hive-deprecated-log")
	}
	if K8sDiagnosisCmd.Flags().Lookup("collect-hive-deprecated-log") == nil {
		t.Error("K8sDiagnosisCmd should have --collect-hive-deprecated-log")
	}
	if SSHStandardCmd.Flags().Lookup("collect-hive-deprecated-log") != nil {
		t.Error("SSHStandardCmd should not have --collect-hive-deprecated-log")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -short -run TestHiveDeprecatedOnlyOnDiagnosis ./cmd/...`
Expected: FAIL — "SSHDiagnosisCmd should have --collect-hive-deprecated-log".

- [ ] **Step 3: Add the global var**

In `cmd/root.go`, in the "log collection toggles" var block (currently ends around line 124), add `collectHiveDeprecated`:

```go
	// log collection toggles
	collectServerLogs   bool
	collectGCLogs       bool
	collectTrackerJSON  bool
	collectVacuumLog    bool
	collectQueriesJSON  bool
	collectAcceleration bool
	collectAccessLog    bool
	collectHiveDeprecated bool
	// collectAuditLog removed — flag and feature deleted
	collectHSErrFiles bool
```

- [ ] **Step 4: Register the CLI flag on the diagnosis command loop**

In `cmd/root.go`, in the diagnosis-only flag loop (the `for _, cmd := range []*cobra.Command{SSHDiagnosisCmd, K8sDiagnosisCmd, LocalDiagnosisCmd, LocalK8sDiagnosisCmd}` block that contains `--collect-acceleration-log`), add after the access-log line (~line 1392):

```go
		cmd.Flags().BoolVar(&collectHiveDeprecated, "collect-hive-deprecated-log", conf.GetBoolDefault(diagDef, conf.KeyCollectHiveDeprecatedLog), "collect hive-deprecated.log files")
```

- [ ] **Step 5: Wire the Args field (replace the hardcoded false)**

In `cmd/root.go`, in the `collection.Args{...}` literal, change line 1211 from:

```go
			CollectHiveDeprecated:  false,
```
to:
```go
			CollectHiveDeprecated:  collectHiveDeprecated,
```

- [ ] **Step 6: Map the TUI value back to the global**

In `cmd/root.go`, in the diagnosis TUI→global mapping block (the run of `collect... = cfg.Collect...` around lines 1669-1676), add after `collectAccessLog = cfg.CollectAccess`:

```go
	collectHiveDeprecated = cfg.CollectHiveDeprecated
```

- [ ] **Step 7: Emit the flag in the diagnosis CLI command builder**

In `cmd/configui/configui.go`, in `buildDiagnosisCLICommand`, the line that prints `--collect-gc-logs ... --collect-acceleration-log ... --collect-access-log` (line 899). Replace it with a version that appends hive-deprecated:

```go
	parts = append(parts, fmt.Sprintf("  --collect-gc-logs=%t --collect-acceleration-log=%t --collect-access-log=%t"+cont, cfg.CollectGCLogs, cfg.CollectAcceleration, cfg.CollectAccess))
	parts = append(parts, fmt.Sprintf("  --collect-hive-deprecated-log=%t"+cont, cfg.CollectHiveDeprecated))
```

- [ ] **Step 8: Run the test to verify it passes**

Run: `go test -short -run TestHiveDeprecatedOnlyOnDiagnosis ./cmd/...`
Expected: PASS.

- [ ] **Step 9: Build to confirm everything compiles**

Run: `go build -o bin/ddc.exe .`
Expected: no output, exit 0.

- [ ] **Step 10: Commit**

```bash
git add cmd/root.go cmd/configui/configui.go cmd/root_flags_test.go
git commit -m "feat: wire collect-hive-deprecated-log end-to-end (flag, args, TUI)"
```

---

### Task 3: Add `metadata_refresh` gating + Args field

Add the `Args.CollectMetaRefreshLog` field, a real `isLogTypeEnabled` case (removing it from the `default: return true` fall-through), and a standard-mode allowlist entry. No `logDayLimit` case (no day filter; returns `-1`).

**Files:**
- Modify: `cmd/root/collection/collector.go:113-122` (file collection gating section of `Args`)
- Modify: `cmd/root/collection/streaming_collect.go` (`standardModeLogAllowlist` ~line 427, `isLogTypeEnabled` ~line 490)
- Test: `cmd/root/collection/streaming_collect_test.go`

- [ ] **Step 1: Write the failing gating tests**

Add to `cmd/root/collection/streaming_collect_test.go`:

```go
func TestIsLogTypeEnabled_MetadataRefresh(t *testing.T) {
	enabled := Args{CollectMetaRefreshLog: true}
	disabled := Args{CollectMetaRefreshLog: false}

	if !isLogTypeEnabled("metadata_refresh.log", enabled) {
		t.Error("metadata_refresh.log should be enabled when CollectMetaRefreshLog=true")
	}
	if isLogTypeEnabled("metadata_refresh.log", disabled) {
		t.Error("metadata_refresh.log should be disabled when CollectMetaRefreshLog=false")
	}
	// Dated rotation must follow the same toggle.
	if !isLogTypeEnabled("metadata_refresh.2022-12-04.log.gz", enabled) {
		t.Error("dated metadata_refresh rotation should be enabled when CollectMetaRefreshLog=true")
	}
}

func TestIsLogAllowedInStandardMode_MetadataRefresh(t *testing.T) {
	if !isLogAllowedInStandardMode("metadata_refresh.log") {
		t.Error("metadata_refresh.log should be in the standard-mode allowlist")
	}
	if !isLogAllowedInStandardMode("metadata_refresh.2022-12-04.log.gz") {
		t.Error("dated metadata_refresh rotation should be allowlisted in standard mode")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -short -run "TestIsLogTypeEnabled_MetadataRefresh|TestIsLogAllowedInStandardMode_MetadataRefresh" ./cmd/root/collection/...`
Expected: FAIL to compile — `unknown field 'CollectMetaRefreshLog' in struct literal of type Args`.

- [ ] **Step 3: Add the Args field**

In `cmd/root/collection/collector.go`, in the `// File collection gating` block, add the field next to the other log toggles:

```go
	// File collection gating
	CollectGCLogs          bool
	CollectServerLogs      bool
	CollectQueriesJSON     bool
	CollectTrackerJSON     bool
	CollectVacuumLog       bool
	CollectAccelerationLog bool
	CollectAccessLog       bool
	CollectHSErrFiles      bool
	CollectHiveDeprecated  bool
	CollectMetaRefreshLog  bool
```

- [ ] **Step 4: Add the allowlist entry**

In `cmd/root/collection/streaming_collect.go`, add `"metadata_refresh."` to `standardModeLogAllowlist`:

```go
var standardModeLogAllowlist = []string{
	"server.",
	"tracker.",
	"vacuum.",
	"metadata_refresh.",
}
```

- [ ] **Step 5: Add the `isLogTypeEnabled` case**

In `cmd/root/collection/streaming_collect.go`, add a case to the `isLogTypeEnabled` switch (before `default:`):

```go
	case strings.HasPrefix(baseName, "metadata_refresh."):
		return args.CollectMetaRefreshLog
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test -short -run "TestIsLogTypeEnabled_MetadataRefresh|TestIsLogAllowedInStandardMode_MetadataRefresh" ./cmd/root/collection/...`
Expected: PASS.

- [ ] **Step 7: Run the full collection package tests (guard against regressions)**

Run: `go test -short ./cmd/root/collection/...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add cmd/root/collection/collector.go cmd/root/collection/streaming_collect.go cmd/root/collection/streaming_collect_test.go
git commit -m "feat: gate metadata_refresh.log via CollectMetaRefreshLog in both modes"
```

---

### Task 4: Add `metadata_refresh` to the TUI (structs, options, CLI builders)

Add `CollectMetaRefresh` to both config structs, initialize from the default maps, add a diagnosis multi-select option + standard confirm toggle, and emit `--collect-meta-refresh-log` in both CLI builders. (root.go global wiring is Task 5; this task makes `cfg.CollectMetaRefresh` exist so Task 5 can reference it.)

**Files:**
- Modify: `cmd/configui/configui.go` (`StandardConfig` ~line 77; `DiagnosisConfig` ~line 129; standard init ~line 281; diagnosis init ~line 425; `buildStandardLogGroup` ~line 701; standard post-form ~line 357; diagnosis multi-select ~line 508; `syncDiagLogTypes` ~line 749; `buildStandardCLICommand` ~line 816; `buildDiagnosisCLICommand` ~line 899)
- Test: `cmd/configui/configui_test.go` (create test func if file exists; otherwise add to an existing `_test.go` in package `configui`)

- [ ] **Step 1: Write the failing syncDiagLogTypes test**

Add a test asserting the diagnosis multi-select maps `meta-refresh` to `CollectMetaRefresh`. Add to `cmd/configui/configui_test.go` (create the file with this content if it does not exist):

```go
package configui

import "testing"

func TestSyncDiagLogTypes_MetaRefresh(t *testing.T) {
	cfg := &DiagnosisConfig{}
	syncDiagLogTypes(cfg, []string{"meta-refresh"})
	if !cfg.CollectMetaRefresh {
		t.Error("expected CollectMetaRefresh=true when 'meta-refresh' is selected")
	}

	cfg2 := &DiagnosisConfig{}
	syncDiagLogTypes(cfg2, []string{"server"})
	if cfg2.CollectMetaRefresh {
		t.Error("expected CollectMetaRefresh=false when 'meta-refresh' is not selected")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -short -run TestSyncDiagLogTypes_MetaRefresh ./cmd/configui/...`
Expected: FAIL to compile — `cfg.CollectMetaRefresh undefined`.

- [ ] **Step 3: Add `CollectMetaRefresh` to both structs**

In `cmd/configui/configui.go`, in `StandardConfig`, add after the vacuum fields (~line 78):

```go
	CollectVacuumLog   bool
	VacuumLogDays      int
	CollectMetaRefresh bool
```

In `DiagnosisConfig`, add after `CollectHiveDeprecated bool` (~line 129):

```go
	CollectHiveDeprecated bool
	CollectMetaRefresh    bool
```

- [ ] **Step 4: Initialize both structs from the default maps**

In the standard config literal (~line 281), add after the vacuum entries:

```go
		CollectVacuumLog:   conf.GetBoolDefault(stdDef, conf.KeyCollectVacuumLog),
		VacuumLogDays:      conf.GetIntDefault(stdDef, conf.KeyVacuumLogNumDays),
		CollectMetaRefresh: conf.GetBoolDefault(stdDef, conf.KeyCollectMetaRefreshLog),
```

In the diagnosis config literal (~line 425), add after the hive-deprecated entry:

```go
		CollectHiveDeprecated:      conf.GetBoolDefault(diagDef, conf.KeyCollectHiveDeprecatedLog),
		CollectMetaRefresh:         conf.GetBoolDefault(diagDef, conf.KeyCollectMetaRefreshLog),
```

- [ ] **Step 5: Add the diagnosis multi-select option + sync mapping**

In `cmd/configui/configui.go`, in the diagnosis `logsAndDataOptions` append block (after the "Access log" option, ~line 511), add:

```go
		huh.NewOption("Metadata refresh log", "meta-refresh").Selected(cfg.CollectMetaRefresh),
```

In `syncDiagLogTypes`, add after `cfg.CollectAccess = logSet["access"]` (~line 752):

```go
	cfg.CollectMetaRefresh = logSet["meta-refresh"]
```

- [ ] **Step 6: Add the standard TUI confirm toggle + post-form mapping**

In `buildStandardLogGroup` (`cmd/configui/configui.go` ~line 710), the `fields` slice ends after the Vacuum select. Add a confirm field before the `if cfg.Transport == "k8s"` block:

```go
	}
	fields = append(fields, huh.NewConfirm().Title("Collect metadata refresh log").Value(&cfg.CollectMetaRefresh).Inline(true).Affirmative("Yes").Negative("No"))
	if cfg.Transport == "k8s" {
		fields = append(fields, huh.NewConfirm().Title("Collect K8s container logs").Value(&cfg.CollectContainerLogs).Inline(true).Affirmative("Yes").Negative("No"))
	}
	return huh.NewGroup(fields...).Title("Log collection").Description(" ")
```

(The `huh.NewConfirm()` binds directly to `&cfg.CollectMetaRefresh`, so no separate post-form assignment is needed — the standard default from Step 4 supplies the initial value, which is `false`.)

- [ ] **Step 7: Emit the flag in both CLI builders**

In `buildStandardCLICommand` (~line 816), after the vacuum block (the `if vacuumDays > 0 { ... } else { ... }`), add:

```go
	parts = append(parts, fmt.Sprintf("  --collect-meta-refresh-log=%t"+cont, cfg.CollectMetaRefresh))
```

In `buildDiagnosisCLICommand`, after the hive-deprecated line added in Task 2 Step 7, add:

```go
	parts = append(parts, fmt.Sprintf("  --collect-meta-refresh-log=%t"+cont, cfg.CollectMetaRefresh))
```

- [ ] **Step 8: Run the test to verify it passes**

Run: `go test -short -run TestSyncDiagLogTypes_MetaRefresh ./cmd/configui/...`
Expected: PASS.

- [ ] **Step 9: Run the full configui package tests**

Run: `go test -short ./cmd/configui/...`
Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add cmd/configui/configui.go cmd/configui/configui_test.go
git commit -m "feat: expose metadata_refresh log toggle in standard + diagnosis TUI"
```

---

### Task 5: Wire `metadata_refresh` in root.go (global, flags, Args, TUI maps)

**Files:**
- Modify: `cmd/root.go` (global var block ~line 124; Args literal ~line 1210; standard flag loop ~line 1350; diagnosis flag loop ~line 1361; standard TUI→global map ~line 1573; diagnosis TUI→global map ~line 1674)
- Test: `cmd/root_flags_test.go`

- [ ] **Step 1: Write the failing flag-on-both-modes test**

Add to `cmd/root_flags_test.go`:

```go
func TestMetaRefreshLogOnBothModes(t *testing.T) {
	if SSHStandardCmd.Flags().Lookup("collect-meta-refresh-log") == nil {
		t.Error("SSHStandardCmd should have --collect-meta-refresh-log")
	}
	if SSHDiagnosisCmd.Flags().Lookup("collect-meta-refresh-log") == nil {
		t.Error("SSHDiagnosisCmd should have --collect-meta-refresh-log")
	}
	if K8sStandardCmd.Flags().Lookup("collect-meta-refresh-log") == nil {
		t.Error("K8sStandardCmd should have --collect-meta-refresh-log")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -short -run TestMetaRefreshLogOnBothModes ./cmd/...`
Expected: FAIL — "SSHStandardCmd should have --collect-meta-refresh-log".

- [ ] **Step 3: Add the global var**

In `cmd/root.go`, in the "log collection toggles" var block, add after `collectHiveDeprecated bool` (added in Task 2):

```go
	collectHiveDeprecated bool
	collectMetaRefresh    bool
```

- [ ] **Step 4: Register the flag on the standard command loop**

In `cmd/root.go`, in the standard flag loop (`for _, cmd := range []*cobra.Command{SSHStandardCmd, K8sStandardCmd, LocalStandardCmd, LocalK8sStandardCmd}`), add after the `--collect-vacuum-log` line (~line 1350):

```go
		cmd.Flags().BoolVar(&collectMetaRefresh, "collect-meta-refresh-log", conf.GetBoolDefault(stdDef, conf.KeyCollectMetaRefreshLog), "collect metadata_refresh.log files")
```

- [ ] **Step 5: Register the flag on the diagnosis command loop**

In the diagnosis flag loop (`for _, cmd := range []*cobra.Command{SSHDiagnosisCmd, K8sDiagnosisCmd, LocalDiagnosisCmd, LocalK8sDiagnosisCmd}` that contains `--collect-vacuum-log`), add after the `--collect-vacuum-log` line (~line 1361):

```go
		cmd.Flags().BoolVar(&collectMetaRefresh, "collect-meta-refresh-log", conf.GetBoolDefault(diagDef, conf.KeyCollectMetaRefreshLog), "collect metadata_refresh.log files")
```

- [ ] **Step 6: Wire the Args field**

In the `collection.Args{...}` literal, add after `CollectHiveDeprecated: collectHiveDeprecated,` (line ~1211):

```go
			CollectHiveDeprecated:  collectHiveDeprecated,
			CollectMetaRefreshLog:  collectMetaRefresh,
```

- [ ] **Step 7: Map the TUI value back to the global in both modes**

In the standard TUI→global block, add after `vacuumLogNumDays = cfg.VacuumLogDays` (~line 1574):

```go
	collectMetaRefresh = cfg.CollectMetaRefresh
```

In the diagnosis TUI→global block, add after `collectHiveDeprecated = cfg.CollectHiveDeprecated` (added in Task 2):

```go
	collectMetaRefresh = cfg.CollectMetaRefresh
```

- [ ] **Step 8: Run the test to verify it passes**

Run: `go test -short -run TestMetaRefreshLogOnBothModes ./cmd/...`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add cmd/root.go cmd/root_flags_test.go
git commit -m "feat: wire collect-meta-refresh-log flag and TUI to collection Args"
```

---

### Task 6: Full verification

**Files:** none (verification only)

- [ ] **Step 1: Run the full short test suite**

Run: `go test -short ./...`
Expected: PASS, except any pre-existing Windows-only failures (path separators, TempDir file locking, `%PATH%` env format). Compare any failure against `main` before attributing it to this work — see `docs/architecture/patterns-and-gotchas.md` "Pre-existing Windows test failures".

- [ ] **Step 2: Build the binary (required by project rule)**

Run: `go build -o bin/ddc.exe .`
Expected: no output, exit 0.

- [ ] **Step 3: Smoke-check the diagnosis defaults via --help**

Run: `bin/ddc.exe collect ssh diagnosis --help`
Expected: output lists `--collect-tracker-json`, `--collect-vacuum-log`, `--collect-acceleration-log`, `--collect-access-log`, `--collect-hive-deprecated-log`, and `--collect-meta-refresh-log`, each showing `(default true)`.

- [ ] **Step 4: Smoke-check the standard defaults via --help**

Run: `bin/ddc.exe collect ssh standard --help`
Expected: `--collect-meta-refresh-log` is present and shows `(default false)`; `--collect-acceleration-log` / `--collect-access-log` / `--collect-hive-deprecated-log` are NOT present (diagnosis-only).

---

## Self-Review

**Spec coverage:**
- Change 1, four clean default flips → Task 1. ✓
- Change 1, hive-deprecated wiring (flag, Args, TUI map, CLI builder) → Task 2. ✓
- Change 2, Args + gating + allowlist (reuse `collect-meta-refresh-log`, no day limit) → Task 3. ✓
- Change 2, TUI in both flows + CLI builders → Task 4. ✓
- Change 2, root.go flag/global/Args/TUI-map on both modes → Task 5. ✓
- Testing & build verification → Tasks 1-6 (each task tests its own change; Task 6 is the full sweep). ✓
- Out of scope (`reflection.log`, standard day limit) → not implemented, matches spec. ✓

**Placeholder scan:** No TBD/TODO; every code step shows concrete code and exact commands.

**Type consistency:** Field name `CollectMetaRefreshLog` (collection `Args`) vs `CollectMetaRefresh` (configui structs) are intentionally distinct across packages and used consistently within each (`Args.CollectMetaRefreshLog` in Tasks 3/5; `cfg.CollectMetaRefresh` in Tasks 4/5). Global var `collectMetaRefresh`, flag `collect-meta-refresh-log`, conf key `conf.KeyCollectMetaRefreshLog` are consistent across Tasks 4-5. Hive: global `collectHiveDeprecated`, flag `collect-hive-deprecated-log`, `Args.CollectHiveDeprecated`, `cfg.CollectHiveDeprecated` consistent across Task 2. ✓
