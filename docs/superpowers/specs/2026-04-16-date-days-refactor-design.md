# Date/Days Refactor — Diagnosis Mode

**Date**: 2026-04-16
**Scope**: CLI flags, TUI, config keys, collection pipeline, rocksdb-viewer invocation

## Problem

Diagnosis mode has two competing date-range mechanisms: `--date-start`/`--date-end` and `--days`. They are mutually exclusive, with `--date-start` taking priority and disabling `--days`. This creates unnecessary complexity — the system converts dates back to a day count internally anyway.

## New Model

The date range is defined by `--start-date` (optional, date-only) + `--days` (always required, default 3).

- Range = `start-date` to `start-date + days`
- When `--start-date` is omitted, it defaults to `now - days`
- `--days` is always the authoritative duration
- There is no `--date-end`
- Date format is `YYYY-MM-DD` (no timestamp)

## Changes by Layer

### CLI Flags (cmd/root.go)

| Current | New |
|---------|-----|
| `--date-start` (ISO 8601 with time) | `--start-date` (date-only, `2006-01-02`) |
| `--date-end` (ISO 8601 with time) | **removed** |
| `--days` (default 3) | `--days` (default 3, unchanged) |

- Remove `endDate` global variable
- Rename `startDate` flag registration from `"date-start"` to `"start-date"`
- `diagLogDays()` simplifies to returning `daysFlag` (always set)
- `validateV4Flags()`: remove endDate validation, remove days-vs-startDate mutual-exclusion. Add date-only format validation for `--start-date`
- `BuildConfData()`: remove endDate config injection

### TUI (cmd/configui/configui.go)

**Logs Collection page (diagnosis mode):**

- "Days to collect" — unchanged
- "Date Start" → renamed to **"Start Date"**
  - `PlaceholderFunc` shows `YYYY-MM-DD [auto]` where the date is `now - days`, updating when days changes
  - `[auto]` placeholder disappears once the user types a value
  - Date format: `2006-01-02` only
- "Date End" — **removed**

**DiagnosisConfig struct:**
- Remove `DateEnd string`
- Keep `DateStart string` (or rename to `StartDate`)
- `Days` always carries a value

**Post-form logic:**
- `cfg.Days` is always set (no longer zeroed when dates are present)
- If `dateStart` is non-empty, set `cfg.DateStart`; otherwise leave empty

**Generated CLI command:**
- Emit `--start-date=2026-04-07 --days=3` (previously `--date-start=... --date-end=...`)

### Config Keys (cmd/local/conf/)

**conf_key_names.go:**
- `KeyStartDate = "date-start"` → `KeyStartDate = "start-date"`
- `KeyEndDate = "date-end"` → **removed**

**conf.go (CollectConf):**
- Remove `endDate time.Time` field and `EndDate()` accessor
- Remove endDate parsing block
- Remove "if startDate set and endDate empty, default endDate to now" logic
- `parseDateString()` supports only `2006-01-02` format

**defaults.go:**
- No changes needed (KeyDremioLogsNumDays default of 3 stays)

### Collection Pipeline (cmd/root/collection/)

**collector.go (collection.Args):**
- Remove `DateEnd string`
- Rename `DateStart string` → `StartDate string`

**streaming_collect.go:**
- `logDayLimit()` — no change needed (already uses `DiagLogDays`)
- `isWithinDayLimit()` — no change needed
- Update `RocksCollectArgs` construction: remove DateEnd, rename DateStart → StartDate

**rockscollect.go (RocksCollectArgs):**
- Remove `DateEnd string`
- Rename `DateStart string` → `StartDate string`

**`buildQueriesPerfFilterArgs()`:**
- Current: emits `-days N` or `-date-start X -date-end Y`
- New: emits `-start-date X -days N` (when StartDate set) or `-days N` (otherwise)
- The rocksdb-viewer `-start-date` flag accepts date-only format (`2026-04-07`)

### Tests

- `configui_test.go`: change `--date-start=` → `--start-date=`, remove `--date-end` assertions, update date format
- `conf_test.go`: update any tests referencing `date-start`/`date-end` config keys
- Any test validating date format: change to `2006-01-02`

## Files Affected

1. `cmd/root.go` — flags, globals, validation, config assembly, diagLogDays()
2. `cmd/configui/configui.go` — TUI fields, DiagnosisConfig struct, post-form logic, generated command
3. `cmd/configui/configui_test.go` — test assertions
4. `cmd/local/conf/conf_key_names.go` — key constants
5. `cmd/local/conf/conf.go` — CollectConf struct, parsing, accessors
6. `cmd/root/collection/collector.go` — collection.Args struct
7. `cmd/root/collection/streaming_collect.go` — RocksCollectArgs construction
8. `cmd/root/collection/rockscollect.go` — RocksCollectArgs struct, buildQueriesPerfFilterArgs()
