# Diagnosis log preselection + metadata_refresh.log collection

**Date:** 2026-06-01
**Status:** Approved (design)

## Summary

Two related changes to DDC log collection:

1. **Diagnosis mode preselects five log types by default** (TUI and CLI):
   Tracker JSON, Vacuum log, Hive deprecated log, Acceleration log, Access log.
   Standard mode defaults are unchanged.

2. **`metadata_refresh.log` becomes a first-class, toggleable log type** in both
   flows. Standard mode: disabled by default. Diagnosis mode: enabled by default.

## Background / current state

The log-file decision path for files discovered with `FileType "log"` is, in order
(`cmd/root/collection/streaming_collect.go`):

1. `isAlwaysExcluded(base)` — hard blocklist (admin_backup, audit., secret suffixes).
2. `isLogTypeEnabled(base, args)` — per-type toggle, applied in **both** modes.
   Files matching no known prefix fall through `default: return true`.
3. `isLogAllowedInStandardMode(base)` — standard-mode-only hard allowlist
   (`server.`, `tracker.`, `vacuum.`).
4. Date-range filter via `logDayLimit(base, args)`.

Discovery (`cmd/root/collection/discovery.go:91-107`) tags every file in the log
dir as `FileType "log"`, reclassifying only `gc*`, `*.gc*`, and `queries.*`. So
`metadata_refresh.log`, `acceleration.log`, `access.log`, and `hive-deprecated.*`
all remain `FileType "log"`.

Defaults live in `cmd/local/conf/defaults.go`. The TUI checkboxes
(`cmd/configui/configui.go`) and CLI flag defaults (`cmd/root.go`) both derive
from `DiagnosisDefaultMap()` / `StandardDefaultMap()`, so a default flip
propagates to both surfaces for already-wired flags.

### Verified gaps

- **Hive deprecated log is dead.** There is no global var, no CLI flag, and the
  collection `Args.CollectHiveDeprecated` is hardcoded `false` at `cmd/root.go:1211`.
  The TUI checkbox at `configui.go:509` exists but its value is never mapped back
  to a global (`root.go:1660-1685` skips it). The gating case in `isLogTypeEnabled`
  (`hive-deprecated.` prefix) already exists. So flipping its default alone does
  nothing — it needs end-to-end wiring.

- **`metadata_refresh.log` is half-built.** The conf key
  `KeyCollectMetaRefreshLog = "collect-meta-refresh-log"`, the
  `CollectConf.CollectMetaRefreshLogs()` getter, and the defaults (diagnosis
  `true`, standard `false`) all already exist. Missing: an `Args` field, a case in
  `isLogTypeEnabled` (it currently falls through `default: return true`), an entry
  in `standardModeLogAllowlist`, a CLI flag, and any TUI option. Net current
  behavior: collected in diagnosis by accident, never in standard, with no real
  toggle.

## Decisions

- **Reuse the existing `collect-meta-refresh-log` key** — do not introduce a new
  flag name. Defaults already match the requirement.
- **No standard-mode day limit** for `metadata_refresh.log`. When enabled in
  standard mode, collect all rotations (no date filter). Diagnosis mode still
  honors the unified `--days`/`--start-date` automatically via `logDayLimit`.
- **Expose in both TUI and CLI** in both modes.
- `reflection.log` shares the same `default: return true` fall-through and is
  similarly half-wired, but is **out of scope** (not requested).

## Change 1 — Preselect five logs in diagnosis mode

### Defaults (`cmd/local/conf/defaults.go`, `DiagnosisCollectionProfile`)

Flip `false` → `true`:

- `KeyCollectTrackerJSON`
- `KeyCollectVacuumLog`
- `KeyCollectAccelerationLog`
- `KeyCollectAccessLog`
- `KeyCollectHiveDeprecatedLog`

Standard mode (`StandardCollectionProfile`) is untouched.

For Tracker, Vacuum, Acceleration, and Access this is sufficient: the diagnosis TUI
checkboxes (`configui.go:507-511`) and CLI flag defaults (`root.go:1360-1392`) read
from `DiagnosisDefaultMap()` and follow automatically.

### Hive-deprecated end-to-end wiring (`cmd/root.go`)

- Add global `collectHiveDeprecated bool`.
- Register `--collect-hive-deprecated-log` on the diagnosis command loop
  (`SSHDiagnosisCmd, K8sDiagnosisCmd, LocalDiagnosisCmd, LocalK8sDiagnosisCmd`),
  alongside `--collect-acceleration-log` / `--collect-access-log`, default from
  `diagDef`.
- Replace hardcoded `CollectHiveDeprecated: false` (line ~1211) with
  `collectHiveDeprecated`.
- In the diagnosis TUI→global mapping (~line 1673), add
  `collectHiveDeprecated = cfg.CollectHiveDeprecated`.

No streaming change needed — the `hive-deprecated.` case in `isLogTypeEnabled`
already exists. Hive-deprecated remains diagnosis-only (consistent with
acceleration/access); standard default stays `false` and it is absent from the
standard allowlist.

## Change 2 — `metadata_refresh.log`

### Args (`cmd/root/collection/collector.go`)

Add field `CollectMetaRefreshLog bool` to the file-collection-gating section.

### Gating (`cmd/root/collection/streaming_collect.go`)

- Add a case to `isLogTypeEnabled`:
  `case strings.HasPrefix(baseName, "metadata_refresh."): return args.CollectMetaRefreshLog`.
  This removes it from the `default: return true` fall-through, making it a real
  toggle in both modes.
- Add `"metadata_refresh."` to `standardModeLogAllowlist` so it can be collected in
  standard mode when enabled.
- No `logDayLimit` case (returns `-1` = no day filter), per the "no day limit"
  decision.

### Wiring (`cmd/root.go`)

- Add global `collectMetaRefresh bool`.
- Register `--collect-meta-refresh-log` on **both** the standard and diagnosis
  command loops, defaults from `stdDef` / `diagDef` respectively (already
  `false` / `true`).
- Set `CollectMetaRefreshLog: collectMetaRefresh` in the `Args` construction.
- Map `collectMetaRefresh = cfg.CollectMetaRefresh` in both the standard and the
  diagnosis TUI→global blocks.

### TUI (`cmd/configui/configui.go`)

- `DiagnosisConfig` and `StandardConfig`: add `CollectMetaRefresh bool`,
  initialized from `DiagnosisDefaultMap()` / `StandardDefaultMap()`.
- Diagnosis: add
  `huh.NewOption("Metadata refresh log", "meta-refresh").Selected(cfg.CollectMetaRefresh)`
  to the logs multi-select; map `cfg.CollectMetaRefresh = logSet["meta-refresh"]`
  in `syncDiagLogTypes`.
- Standard: add a `huh.NewConfirm()` toggle in `buildStandardLogGroup` (matching
  the K8s container-logs confirm style, since there is no day limit); map the
  value through in the standard post-form block.
- Emit `--collect-meta-refresh-log` in both CLI command builders
  (`buildStandardCLICommand`, `buildDiagnosisCLICommand`).

## Testing & verification

- Update `cmd/local/conf/defaults_test.go`: diagnosis assertions for the four
  flipped flags (tracker, vacuum, acceleration, access) and hive-deprecated.
- Extend `cmd/root/collection/streaming_collect_test.go` for the new
  `metadata_refresh.` allowlist entry and `isLogTypeEnabled` case.
- Add a focused test asserting `metadata_refresh.log` is:
  - collected under diagnosis defaults,
  - excluded under standard defaults,
  - collected in standard mode when the flag is enabled.
- `metadata_refresh.log` classification as `FileType "log"` is already confirmed
  (`discovery.go:91-107`); add a discovery test assertion if a natural fixture
  exists.
- Run `go test -short ./...`, then `go build -o bin/ddc.exe .`.

## Out of scope

- `reflection.log` symmetry (same fall-through pattern; not requested).
- Standard-mode day limit for `metadata_refresh.log`.
