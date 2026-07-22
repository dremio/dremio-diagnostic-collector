# Disable wlm_cluster_usage by Default Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `collect-wlm-cluster-usage` boolean knob (default `false` in both standard and diagnosis modes) so the expensive `wlm_cluster_usage` RocksDB export (#338) is opt-in, while `wlm_queues`/`wlm_rules`/`wlm_engines` stay collected under the existing `collect-wlm` flag.

**Architecture:** Follow the exact wiring pattern of the existing `collect-wlm` knob: conf key + mode defaults → Cobra flags on the standard/diagnosis leaf commands → `CollectionArgs` → `RocksCollectArgs` → a skip inside the single WLM loop in `RunRocksDBCollection`. The TUI gets a confirm toggle in both forms and emits the flag in the generated CLI command. Gating semantics: `wlm_cluster_usage` is exported only when `collect-wlm` **and** `collect-wlm-cluster-usage` are both true.

**Tech Stack:** Go 1.21+, Cobra (CLI), charmbracelet/huh (TUI), standard `testing` package.

**Spec:** `docs/superpowers/specs/2026-07-22-wlm-cluster-usage-default-off-design.md`

## Global Constraints

- **NEVER commit without the user's explicit approval** (project CLAUDE.md + user rule: all changes are reviewed before commit). Where a task ends in "Commit", instead STOP and present the change summary; the user triggers the actual commit. A single commit at the end covering all tasks is expected.
- Default for the new key is `false` in BOTH mode profiles — no Cobra mode-guard needed (`BoolVar` init-time leakage is only a hazard when defaults differ between modes).
- Windows test caveat: the `cmd/local/rockscollect` and `cmd/root/collection` test binaries embed UPX-compressed ELF binaries; Windows Defender may block them in Go's temp dir with `Access is denied`. Fallback: `go test -short -c -o bin/pkg.test.exe ./<pkg>/` then run `.\bin\pkg.test.exe --% -test.short -test.v` from the workspace, and delete the exe afterward.
- Run `gofmt -w` (or `go fmt ./...`) after struct-literal/const-block edits — alignment will shift.
- After all tasks: `go build -o bin/ddc.exe .` must succeed (project rule: always build after changes).

---

### Task 1: Config key and mode defaults (`cmd/local/conf`)

**Files:**
- Modify: `cmd/local/conf/conf_key_names.go` (~line 57, inside the const block)
- Modify: `cmd/local/conf/defaults.go` (`DiagnosisCollectionProfile` ~line 66, `StandardCollectionProfile` ~line 146)
- Test: `cmd/local/conf/defaults_test.go` (~line 73 and ~line 143)

**Interfaces:**
- Consumes: existing `setDefault`, `conf.KeyCollectWLM` pattern.
- Produces: `conf.KeyCollectWLMClusterUsage` (string constant `"collect-wlm-cluster-usage"`), present with value `false` in the maps returned by `conf.StandardDefaultMap()` and `conf.DiagnosisDefaultMap()`. Tasks 2–4 rely on this exact constant name.

- [ ] **Step 1: Write the failing test rows**

In `cmd/local/conf/defaults_test.go`, in `TestSetViperDefaultsWithDiagnostic` find:

```go
		// API collection
		{conf.KeyCollectWLM, true},
```

and change to:

```go
		// API collection
		{conf.KeyCollectWLM, true},
		{conf.KeyCollectWLMClusterUsage, false},
```

In `TestSetViperDefaultsWithStandard` find:

```go
		// WLM enabled (via RocksDB viewer), KV store disabled
		{conf.KeyCollectWLM, true},
```

and change to:

```go
		// WLM enabled (via RocksDB viewer), KV store disabled
		{conf.KeyCollectWLM, true},
		{conf.KeyCollectWLMClusterUsage, false},
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -short ./cmd/local/conf/`
Expected: FAIL — compile error `undefined: conf.KeyCollectWLMClusterUsage`

- [ ] **Step 3: Add the key and defaults**

In `cmd/local/conf/conf_key_names.go` find:

```go
	KeyCollectWLM                  = "collect-wlm"
```

and change to:

```go
	KeyCollectWLM                  = "collect-wlm"
	KeyCollectWLMClusterUsage      = "collect-wlm-cluster-usage"
```

In `cmd/local/conf/defaults.go`, in `DiagnosisCollectionProfile` find:

```go
	setDefault(confData, KeyCollectKVStoreReport, false)
	setDefault(confData, KeyCollectWLM, true)
	setDefault(confData, KeyCollectProblematicProfiles, false)
```

and change to:

```go
	setDefault(confData, KeyCollectKVStoreReport, false)
	setDefault(confData, KeyCollectWLM, true)
	// wlm_cluster_usage is expensive on large catalogs (#338) — opt-in only
	setDefault(confData, KeyCollectWLMClusterUsage, false)
	setDefault(confData, KeyCollectProblematicProfiles, false)
```

In `StandardCollectionProfile` find:

```go
	// WLM enabled in standard mode (collected via dremio-rocksdb-viewer)
	setDefault(confData, KeyCollectWLM, true)
```

and change to:

```go
	// WLM enabled in standard mode (collected via dremio-rocksdb-viewer);
	// wlm_cluster_usage is expensive on large catalogs (#338) — opt-in only
	setDefault(confData, KeyCollectWLM, true)
	setDefault(confData, KeyCollectWLMClusterUsage, false)
```

Run `gofmt -w cmd/local/conf/conf_key_names.go cmd/local/conf/defaults.go` (const-block alignment shifts).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -short ./cmd/local/conf/`
Expected: `ok  	github.com/dremio/dremio-diagnostic-collector/v4/cmd/local/conf`

- [ ] **Step 5: Checkpoint**

Do NOT commit. Note Task 1 complete for the final review summary.

---

### Task 2: Collection gating (`cmd/root/collection`)

**Files:**
- Modify: `cmd/root/collection/rockscollect.go` (`RocksCollectArgs` struct ~line 120, WLM loop ~line 233)
- Modify: `cmd/root/collection/collector.go` (`CollectionArgs` struct, "API collections" block ~line 129)
- Modify: `cmd/root/collection/streaming_collect.go` (`RocksCollectArgs` literal ~line 982)
- Test: `cmd/root/collection/rockscollect_test.go` (`TestWLMFileLayout` ~line 208, plus one new test)

**Interfaces:**
- Consumes: nothing new from Task 1 (pure struct/loop change).
- Produces: `RocksCollectArgs.CollectWLMClusterUsage bool` and `CollectionArgs.CollectWLMClusterUsage bool`. Task 4 sets `CollectionArgs.CollectWLMClusterUsage` from the CLI global.

- [ ] **Step 1: Write the failing tests**

In `cmd/root/collection/rockscollect_test.go`, add `"sync"` to the import block:

```go
import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)
```

In `TestWLMFileLayout` find:

```go
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
```

and change to:

```go
	args := RocksCollectArgs{
		Collector:              mc,
		CopyStrategy:           cs,
		Host:                   "dremio-master-0",
		NodeType:               "coordinator",
		RocksDBDir:             "/opt/dremio/data/db",
		CollectSystemTables:    false,
		CollectWLM:             true,
		CollectWLMClusterUsage: true,
		CollectQueriesPerf:     false,
	}
```

Append this new test at the end of the file (after `TestWLMFileLayout`'s closing brace):

```go
// TestWLMClusterUsageSkippedByDefault verifies that wlm_cluster_usage is NOT
// exported unless CollectWLMClusterUsage is set (default false), while the
// other three WLM types are still collected under CollectWLM.
func TestWLMClusterUsageSkippedByDefault(t *testing.T) {
	tmpDir := t.TempDir()
	cs := &mockCopyStrategy{tmpDir: tmpDir}

	wlmPayloads := map[string]string{
		"wlm_queues":        `{"queues":[]}`,
		"wlm_rules":         `{"rules":[]}`,
		"wlm_engines":       `{"engines":[]}`,
		"wlm_cluster_usage": `{"cluster_usage":[]}`,
	}

	var mu sync.Mutex
	var calls []string
	mc := &mockStreamCollector{
		coordinators: []string{"dremio-master-0"},
		hostExecuteFunc: func(_ bool, _ string, args ...string) (string, error) {
			cmd := strings.Join(args, " ")
			mu.Lock()
			calls = append(calls, cmd)
			mu.Unlock()
			switch {
			case strings.HasPrefix(cmd, "test -f") && strings.Contains(cmd, "/catalog/CURRENT"):
				return "exists", nil
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
		// CollectWLMClusterUsage deliberately left false (the default)
		CollectQueriesPerf: false,
	}

	got, err := RunRocksDBCollection(args)
	if err != nil {
		t.Fatalf("RunRocksDBCollection failed: %v", err)
	}

	seen := map[string]bool{}
	for _, cf := range got {
		seen[filepath.Base(cf.Path)] = true
	}
	for _, want := range []string{"queues.json", "rules.json", "engines.json"} {
		if !seen[want] {
			t.Errorf("expected WLM file %s to be collected, but it was not", want)
		}
	}
	if seen["cluster_usage.json"] {
		t.Error("cluster_usage.json was collected despite CollectWLMClusterUsage=false")
	}
	for _, c := range calls {
		if strings.Contains(c, "-type wlm_cluster_usage") {
			t.Errorf("rocksdb-viewer was invoked with -type wlm_cluster_usage despite CollectWLMClusterUsage=false: %s", c)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -short ./cmd/root/collection/`
Expected: FAIL — compile error `unknown field CollectWLMClusterUsage in struct literal of type RocksCollectArgs`

- [ ] **Step 3: Implement the gate**

In `cmd/root/collection/rockscollect.go` find:

```go
	CollectSystemTables bool
	SystemTables        []string
	CollectWLM          bool
	CollectQueriesPerf  bool
```

and change to:

```go
	CollectSystemTables bool
	SystemTables        []string
	CollectWLM          bool
	// CollectWLMClusterUsage gates the wlm_cluster_usage export separately —
	// it scans every profile in the catalog and can take hours on large KV stores (#338).
	CollectWLMClusterUsage bool
	CollectQueriesPerf     bool
```

In the same file find the WLM loop:

```go
	if args.CollectWLM {
		for _, wt := range wlmTypes {
			consoleprint.UpdateNodeState(consoleprint.NodeState{
```

and change to:

```go
	if args.CollectWLM {
		for _, wt := range wlmTypes {
			if wt == "wlm_cluster_usage" && !args.CollectWLMClusterUsage {
				simplelog.Infof("rocksdb: skipping wlm_cluster_usage on %s (collect-wlm-cluster-usage=false)", host)
				continue
			}
			consoleprint.UpdateNodeState(consoleprint.NodeState{
```

In `cmd/root/collection/collector.go` find:

```go
	CollectWLM                 bool
	CollectKVStoreReport       bool
```

and change to:

```go
	CollectWLM                 bool
	CollectWLMClusterUsage     bool
	CollectKVStoreReport       bool
```

In `cmd/root/collection/streaming_collect.go` find:

```go
					CollectWLM:          collectionArgs.CollectWLM,
					CollectQueriesPerf:  collectionArgs.CollectQueriesPerf,
```

and change to:

```go
					CollectWLM:             collectionArgs.CollectWLM,
					CollectWLMClusterUsage: collectionArgs.CollectWLMClusterUsage,
					CollectQueriesPerf:     collectionArgs.CollectQueriesPerf,
```

Run `gofmt -w cmd/root/collection/rockscollect.go cmd/root/collection/collector.go cmd/root/collection/streaming_collect.go cmd/root/collection/rockscollect_test.go` (struct-literal alignment shifts — the `RocksCollectArgs` literal in `streaming_collect.go` and the structs above will realign).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -short ./cmd/root/collection/`
Expected: `ok  	github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/collection` (~19s; uses the Defender fallback from Global Constraints if `Access is denied` appears)

- [ ] **Step 5: Checkpoint**

Do NOT commit. Note Task 2 complete for the final review summary.

---

### Task 3: TUI toggle and generated CLI command (`cmd/configui`)

**Files:**
- Modify: `cmd/configui/configui.go` (StandardConfig struct ~line 85, DiagnosisConfig struct ~line 137, standard seeding ~line 290, diagnosis seeding ~line 434, standard form group ~line 334, diagnosis form group ~line 569, `buildStandardCLICommand` ~line 843, `buildDiagnosisCLICommand` ~line 922)
- Test: `cmd/configui/configui_test.go` (`TestBuildStandardCLICommand_QueriesPerfEnabled` ~line 83, `TestBuildDiagnosisCLICommand_NoSystemTables` ~line 207)

**Interfaces:**
- Consumes: `conf.KeyCollectWLMClusterUsage` from Task 1.
- Produces: `StandardConfig.CollectWLMClusterUsage bool` and `DiagnosisConfig.CollectWLMClusterUsage bool`. Task 4's dispatch blocks read these exact field names.

- [ ] **Step 1: Write the failing tests**

In `cmd/configui/configui_test.go`, in `TestBuildStandardCLICommand_QueriesPerfEnabled` find:

```go
	if !strings.Contains(cmd, "--collect-wlm=true") {
		t.Errorf("expected --collect-wlm=true, got:\n%s", cmd)
	}
```

and change to:

```go
	if !strings.Contains(cmd, "--collect-wlm=true") {
		t.Errorf("expected --collect-wlm=true, got:\n%s", cmd)
	}
	if !strings.Contains(cmd, "--collect-wlm-cluster-usage=false") {
		t.Errorf("expected --collect-wlm-cluster-usage=false (default off), got:\n%s", cmd)
	}
```

In `TestBuildDiagnosisCLICommand_NoSystemTables` find:

```go
	cmd := buildDiagnosisCLICommand(cfg, &allTools, nil, &days, &dur, new(string))
	if !strings.Contains(cmd, "--system-tables=") {
```

and change to:

```go
	cfg.CollectWLMClusterUsage = true
	cmd := buildDiagnosisCLICommand(cfg, &allTools, nil, &days, &dur, new(string))
	if !strings.Contains(cmd, "--collect-wlm-cluster-usage=true") {
		t.Errorf("expected --collect-wlm-cluster-usage=true when enabled, got:\n%s", cmd)
	}
	if !strings.Contains(cmd, "--system-tables=") {
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -short ./cmd/configui/`
Expected: FAIL — compile error `cfg.CollectWLMClusterUsage undefined`

- [ ] **Step 3: Implement struct fields, seeding, form toggles, command output**

In `cmd/configui/configui.go`, `StandardConfig` struct, find:

```go
	CollectWLM           bool
	CollectSystemTables  bool
```

and change to:

```go
	CollectWLM             bool
	CollectWLMClusterUsage bool
	CollectSystemTables    bool
```

In `DiagnosisConfig` struct find:

```go
	CollectWLM                 bool
	CollectKVStore             bool
```

and change to:

```go
	CollectWLM                 bool
	CollectWLMClusterUsage     bool
	CollectKVStore             bool
```

In the standard-mode `cfg := &StandardConfig{` literal find:

```go
		CollectWLM:         conf.GetBoolDefault(stdDef, conf.KeyCollectWLM),
```

and change to:

```go
		CollectWLM:             conf.GetBoolDefault(stdDef, conf.KeyCollectWLM),
		CollectWLMClusterUsage: conf.GetBoolDefault(stdDef, conf.KeyCollectWLMClusterUsage),
```

In the diagnosis-mode `cfg := &DiagnosisConfig{` literal find:

```go
		CollectWLM:                 conf.GetBoolDefault(diagDef, conf.KeyCollectWLM),
```

and change to:

```go
		CollectWLM:                 conf.GetBoolDefault(diagDef, conf.KeyCollectWLM),
		CollectWLMClusterUsage:     conf.GetBoolDefault(diagDef, conf.KeyCollectWLMClusterUsage),
```

In the standard form's "Additional collections" group find:

```go
			huh.NewConfirm().Title("Collect WLM configuration").Value(&cfg.CollectWLM).Inline(true),
			buildSystemTablesMultiSelect(&cfg.SystemTables),
```

and change to:

```go
			huh.NewConfirm().Title("Collect WLM configuration").Value(&cfg.CollectWLM).Inline(true),
			huh.NewConfirm().Title("Collect WLM cluster usage").Value(&cfg.CollectWLMClusterUsage).Inline(true).
				Description("Scans every job profile in the KV store — can be slow on large catalogs"),
			buildSystemTablesMultiSelect(&cfg.SystemTables),
```

In the diagnosis form's "Dremio System Tables & WLM" group find:

```go
			huh.NewConfirm().Title("Collect WLM configuration").Value(&cfg.CollectWLM).Inline(true),
			buildSystemTablesMultiSelect(&cfg.SystemTables),
```

and change to (identical insertion):

```go
			huh.NewConfirm().Title("Collect WLM configuration").Value(&cfg.CollectWLM).Inline(true),
			huh.NewConfirm().Title("Collect WLM cluster usage").Value(&cfg.CollectWLMClusterUsage).Inline(true).
				Description("Scans every job profile in the KV store — can be slow on large catalogs"),
			buildSystemTablesMultiSelect(&cfg.SystemTables),
```

(Note: both groups contain the same two lines — use surrounding context (`.Title("Additional collections")` vs `.Title("Dremio System Tables & WLM")` a few lines below) to target each occurrence, or use a `replace_all`-style edit followed by a check that exactly two confirms were added.)

In `buildStandardCLICommand` find:

```go
	parts = append(parts, fmt.Sprintf("  --collect-wlm=%t"+cont, cfg.CollectWLM))
```

and change to:

```go
	parts = append(parts, fmt.Sprintf("  --collect-wlm=%t --collect-wlm-cluster-usage=%t"+cont, cfg.CollectWLM, cfg.CollectWLMClusterUsage))
```

In `buildDiagnosisCLICommand` find:

```go
	parts = append(parts, fmt.Sprintf("  --collect-wlm=%t --collect-kvstore-report=%t --collect-problematic-profiles=%t"+cont, cfg.CollectWLM, cfg.CollectKVStore, cfg.CollectProblematicProfiles))
```

and change to:

```go
	parts = append(parts, fmt.Sprintf("  --collect-wlm=%t --collect-wlm-cluster-usage=%t --collect-kvstore-report=%t --collect-problematic-profiles=%t"+cont, cfg.CollectWLM, cfg.CollectWLMClusterUsage, cfg.CollectKVStore, cfg.CollectProblematicProfiles))
```

Run `gofmt -w cmd/configui/configui.go cmd/configui/configui_test.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -short ./cmd/configui/`
Expected: `ok  	github.com/dremio/dremio-diagnostic-collector/v4/cmd/configui`

- [ ] **Step 5: Checkpoint**

Do NOT commit. Note Task 3 complete for the final review summary.

---

### Task 4: CLI flag registration and dispatch (`cmd/root.go`)

**Files:**
- Modify: `cmd/root.go` (var block ~line 133, `CollectionArgs` literal ~line 1216, standard flag loop ~line 1382, diagnosis flag loop ~line 1394, standard TUI dispatch ~line 1613, diagnosis TUI dispatch ~line 1717)

**Interfaces:**
- Consumes: `conf.KeyCollectWLMClusterUsage` (Task 1), `CollectionArgs.CollectWLMClusterUsage` (Task 2), `cfg.CollectWLMClusterUsage` on both configui structs (Task 3).
- Produces: `--collect-wlm-cluster-usage` flag on all 8 leaf commands; global `collectWLMClusterUsage bool`.

- [ ] **Step 1: Add the global variable**

In `cmd/root.go` find:

```go
	collectWLM                 bool
	collectKVStoreReport       bool
```

and change to:

```go
	collectWLM                 bool
	collectWLMClusterUsage     bool
	collectKVStoreReport       bool
```

- [ ] **Step 2: Register the flag on both command groups**

In the standard-commands loop find:

```go
		cmd.Flags().BoolVar(&collectWLM, "collect-wlm", conf.GetBoolDefault(stdDef, conf.KeyCollectWLM), "collect WLM configuration")
```

and change to:

```go
		cmd.Flags().BoolVar(&collectWLM, "collect-wlm", conf.GetBoolDefault(stdDef, conf.KeyCollectWLM), "collect WLM configuration")
		cmd.Flags().BoolVar(&collectWLMClusterUsage, "collect-wlm-cluster-usage", conf.GetBoolDefault(stdDef, conf.KeyCollectWLMClusterUsage), "collect WLM cluster usage data (can be slow on large catalogs)")
```

In the diagnosis-commands loop find:

```go
		cmd.Flags().BoolVar(&collectWLM, "collect-wlm", conf.GetBoolDefault(diagDef, conf.KeyCollectWLM), "collect WLM configuration")
```

and change to:

```go
		cmd.Flags().BoolVar(&collectWLM, "collect-wlm", conf.GetBoolDefault(diagDef, conf.KeyCollectWLM), "collect WLM configuration")
		cmd.Flags().BoolVar(&collectWLMClusterUsage, "collect-wlm-cluster-usage", conf.GetBoolDefault(diagDef, conf.KeyCollectWLMClusterUsage), "collect WLM cluster usage data (can be slow on large catalogs)")
```

- [ ] **Step 3: Thread into CollectionArgs and both TUI dispatch blocks**

In the `CollectionArgs` literal find:

```go
			CollectWLM:                 collectWLM,
```

and change to:

```go
			CollectWLM:                 collectWLM,
			CollectWLMClusterUsage:     collectWLMClusterUsage,
```

In `runStandardConfigScreen`'s apply-block find:

```go
	collectWLM = cfg.CollectWLM
	collectContainerLogs = cfg.CollectContainerLogs
```

and change to:

```go
	collectWLM = cfg.CollectWLM
	collectWLMClusterUsage = cfg.CollectWLMClusterUsage
	collectContainerLogs = cfg.CollectContainerLogs
```

In `runDiagnosisConfigScreen`'s apply-block find:

```go
	collectWLM = cfg.CollectWLM
	collectKVStoreReport = cfg.CollectKVStore
```

and change to:

```go
	collectWLM = cfg.CollectWLM
	collectWLMClusterUsage = cfg.CollectWLMClusterUsage
	collectKVStoreReport = cfg.CollectKVStore
```

Run `gofmt -w cmd/root.go`.

- [ ] **Step 4: Verify by building and inspecting help output**

Run: `go build -o bin/ddc.exe .`
Expected: exit 0, no output.

Run: `.\bin\ddc.exe collect k8s standard --help` and `.\bin\ddc.exe collect ssh diagnosis --help`
Expected: both list `--collect-wlm-cluster-usage   collect WLM cluster usage data (can be slow on large catalogs)` with no `(default true)` suffix (Cobra omits the default marker for false booleans).

- [ ] **Step 5: Checkpoint**

Do NOT commit. Note Task 4 complete for the final review summary.

---

### Task 5: Full verification and review handoff

**Files:** none new — verification only.

- [ ] **Step 1: Full unit test sweep**

Run: `go test -short ./...`
Expected: all packages `ok` (use the Defender fallback from Global Constraints for any `Access is denied` package).

- [ ] **Step 2: Vet and lint**

Run: `go vet ./...` — expected: no output.
Run: `golangci-lint run` — expected: no issues.

- [ ] **Step 3: Rebuild the binary**

Run: `go build -o bin/ddc.exe .`
Expected: exit 0. (Project rule: binary must be built after changes.)

- [ ] **Step 4: Present for user review**

STOP. Show the user: `git status --short`, a summary of the diff per file, and the proposed commit message below. Do NOT commit until the user approves.

Proposed commit message:

```
feat: disable wlm_cluster_usage collection by default (#338)

Adds a collect-wlm-cluster-usage knob (config key, CLI flag on all
standard and diagnosis subcommands, and TUI toggle), default false in
both modes. wlm_cluster_usage scans every profile in the RocksDB
catalog and can dominate collection time on large KV stores; it is now
exported only when both --collect-wlm and --collect-wlm-cluster-usage
are true. The cheap WLM exports (queues, rules, engines) remain on by
default under --collect-wlm. A skipped usage export is logged to
ddc.log.

Spec: docs/superpowers/specs/2026-07-22-wlm-cluster-usage-default-off-design.md
```

---

## Self-Review Notes

- **Spec coverage:** conf key/defaults → Task 1; gating + args threading → Task 2; TUI + command strings → Task 3; CLI flags + dispatch → Task 4; verification/build → Task 5; ddc.log trace → Task 2 Step 3 (`simplelog.Infof`). Spec risks section requires no code.
- **Type consistency:** `CollectWLMClusterUsage bool` is the field name on all four structs (`RocksCollectArgs`, `CollectionArgs`, `StandardConfig`, `DiagnosisConfig`); the global is `collectWLMClusterUsage`; the key constant is `conf.KeyCollectWLMClusterUsage` = `"collect-wlm-cluster-usage"`.
- **Ordering:** Task 4 references Task 2's and Task 3's fields, so tasks must run 1 → 2 → 3 → 4 → 5. Tasks 2 and 3 are independent of each other.
