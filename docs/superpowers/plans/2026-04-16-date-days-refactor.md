# Date/Days Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Simplify diagnosis mode date handling by removing `--date-end`, renaming `--date-start` to `--start-date` (date-only format), and making `--days` the authoritative duration.

**Architecture:** Remove all `DateEnd`/`endDate` paths. Rename `DateStart` → `StartDate` and `date-start` → `start-date` throughout. Simplify `diagLogDays()` to just return `daysFlag`. Update TUI placeholder to show auto-computed date. Update rocksdb-viewer command construction to use `-start-date` + `-days`.

**Tech Stack:** Go, Cobra CLI, charmbracelet/huh TUI

---

### Task 1: Config key constants and CollectConf struct

**Files:**
- Modify: `cmd/local/conf/conf_key_names.go:78-79`
- Modify: `cmd/local/conf/conf.go:125-126,262-280,740-768`

- [ ] **Step 1: Rename KeyStartDate and remove KeyEndDate**

In `cmd/local/conf/conf_key_names.go`, change:

```go
KeyStartDate                  = "date-start"
KeyEndDate                    = "date-end"
```

to:

```go
KeyStartDate = "start-date"
```

- [ ] **Step 2: Remove endDate from CollectConf struct**

In `cmd/local/conf/conf.go`, change the struct fields (around line 125-126):

```go
startDate                time.Time
endDate                  time.Time
```

to:

```go
startDate time.Time
```

- [ ] **Step 3: Remove endDate parsing and default logic**

In `cmd/local/conf/conf.go`, replace lines 262-280:

```go
// date-range filtering
if startDateStr := GetString(confData, KeyStartDate); startDateStr != "" {
	t, err := parseDateString(startDateStr)
	if err != nil {
		return &CollectConf{}, fmt.Errorf("invalid %v value %q: %w", KeyStartDate, startDateStr, err)
	}
	c.startDate = t
}
if endDateStr := GetString(confData, KeyEndDate); endDateStr != "" {
	t, err := parseDateString(endDateStr)
	if err != nil {
		return &CollectConf{}, fmt.Errorf("invalid %v value %q: %w", KeyEndDate, endDateStr, err)
	}
	c.endDate = t
}
// If startDate is set but endDate is not, default endDate to now
if !c.startDate.IsZero() && c.endDate.IsZero() {
	c.endDate = time.Now()
}
```

with:

```go
// date-range filtering
if startDateStr := GetString(confData, KeyStartDate); startDateStr != "" {
	t, err := parseDateString(startDateStr)
	if err != nil {
		return &CollectConf{}, fmt.Errorf("invalid %v value %q: %w", KeyStartDate, startDateStr, err)
	}
	c.startDate = t
}
```

- [ ] **Step 4: Remove EndDate/HasDateRange accessors, simplify parseDateString**

In `cmd/local/conf/conf.go`, remove `EndDate()` (lines 745-748) and `HasDateRange()` (lines 750-753). Keep only `StartDate()` (lines 740-743).

Replace `parseDateString` (lines 755-768):

```go
func parseDateString(s string) (time.Time, error) {
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported date format, expected RFC3339, 2006-01-02T15:04:05, or 2006-01-02")
}
```

with:

```go
func parseDateString(s string) (time.Time, error) {
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("unsupported date format, expected 2006-01-02")
}
```

- [ ] **Step 5: Build and run tests**

```bash
go build -o bin/ddc.exe .
go test -short ./cmd/local/conf/...
```

Expected: build succeeds, tests pass.

- [ ] **Step 6: Commit**

```bash
git add cmd/local/conf/conf_key_names.go cmd/local/conf/conf.go
git commit -m "refactor: rename start-date config key, remove date-end from conf"
```

---

### Task 2: Collection pipeline structs

**Files:**
- Modify: `cmd/root/collection/collector.go:94-99`
- Modify: `cmd/root/collection/rockscollect.go:99-114,259-283`
- Modify: `cmd/root/collection/streaming_collect.go:911-925`

- [ ] **Step 1: Update collection.Args struct**

In `cmd/root/collection/collector.go`, replace lines 94-99:

```go
// Diagnosis mode: unified day limit for all log types (from --days or --date-start)
DiagLogDays int

// Date range (diagnosis mode)
DateStart string
DateEnd   string
```

with:

```go
// Diagnosis mode: unified day limit for all log types (from --days)
DiagLogDays int

// Start date (diagnosis mode, date-only format 2006-01-02)
StartDate string
```

- [ ] **Step 2: Update RocksCollectArgs struct**

In `cmd/root/collection/rockscollect.go`, replace lines 110-113:

```go
QueriesPerfDays     int    // standard mode: from --queries-perf-num-days
Days                int    // diagnosis mode: from --days
DateStart           string // diagnosis mode
DateEnd             string // diagnosis mode
```

with:

```go
QueriesPerfDays int    // standard mode: from --queries-perf-num-days
Days            int    // diagnosis mode: from --days
StartDate       string // diagnosis mode (date-only, e.g. 2026-04-07)
```

- [ ] **Step 3: Rewrite buildQueriesPerfFilterArgs**

In `cmd/root/collection/rockscollect.go`, replace lines 259-279:

```go
func buildQueriesPerfFilterArgs(args RocksCollectArgs) string {
	// Diagnosis mode: --days or --date-start override the per-log default.
	if args.Days > 0 {
		return fmt.Sprintf(" -days %d", args.Days)
	}
	if args.DateStart != "" {
		filter := fmt.Sprintf(" -date-start %s", args.DateStart)
		if args.DateEnd != "" {
			filter += fmt.Sprintf(" -date-end %s", args.DateEnd)
		}
		return filter
	}
	// Standard mode: use the queries-perf-specific day count.
	if args.QueriesPerfDays > 0 {
		return fmt.Sprintf(" -days %d", args.QueriesPerfDays)
	}
	return ""
}
```

with:

```go
func buildQueriesPerfFilterArgs(args RocksCollectArgs) string {
	// Diagnosis mode: -start-date + -days, or just -days.
	if args.StartDate != "" {
		return fmt.Sprintf(" -start-date %s -days %d", args.StartDate, args.Days)
	}
	if args.Days > 0 {
		return fmt.Sprintf(" -days %d", args.Days)
	}
	// Standard mode: use the queries-perf-specific day count.
	if args.QueriesPerfDays > 0 {
		return fmt.Sprintf(" -days %d", args.QueriesPerfDays)
	}
	return ""
}
```

- [ ] **Step 4: Update log message**

In `cmd/root/collection/rockscollect.go`, replace line 283:

```go
simplelog.Infof("rocksdb-viewer queries_perf filter: QueriesPerfDays=%d Days=%d DateStart=%q filterArgs=%q", args.QueriesPerfDays, args.Days, args.DateStart, filterArgs)
```

with:

```go
simplelog.Infof("rocksdb-viewer queries_perf filter: QueriesPerfDays=%d Days=%d StartDate=%q filterArgs=%q", args.QueriesPerfDays, args.Days, args.StartDate, filterArgs)
```

- [ ] **Step 5: Update RocksCollectArgs construction in streaming_collect.go**

In `cmd/root/collection/streaming_collect.go`, replace lines 911-925:

```go
rocksArgs := RocksCollectArgs{
	Collector:           c,
	CopyStrategy:        s,
	Host:                host,
	NodeType:            nodeType,
	RocksDBDir:          collectionArgs.DremioRocksDBDir,
	CollectSystemTables: collectionArgs.CollectSystemTables,
	SystemTables:        collectionArgs.SystemTables,
	CollectWLM:          collectionArgs.CollectWLM,
	CollectQueriesPerf:  collectionArgs.CollectQueriesPerf,
	QueriesPerfDays:     collectionArgs.QueriesPerfNumDays,
	Days:                collectionArgs.DiagLogDays,
	DateStart:           collectionArgs.DateStart,
	DateEnd:             collectionArgs.DateEnd,
}
```

with:

```go
rocksArgs := RocksCollectArgs{
	Collector:           c,
	CopyStrategy:        s,
	Host:                host,
	NodeType:            nodeType,
	RocksDBDir:          collectionArgs.DremioRocksDBDir,
	CollectSystemTables: collectionArgs.CollectSystemTables,
	SystemTables:        collectionArgs.SystemTables,
	CollectWLM:          collectionArgs.CollectWLM,
	CollectQueriesPerf:  collectionArgs.CollectQueriesPerf,
	QueriesPerfDays:     collectionArgs.QueriesPerfNumDays,
	Days:                collectionArgs.DiagLogDays,
	StartDate:           collectionArgs.StartDate,
}
```

- [ ] **Step 6: Build and run tests**

```bash
go build -o bin/ddc.exe .
go test -short ./cmd/root/collection/...
```

Expected: build succeeds, tests pass.

- [ ] **Step 7: Commit**

```bash
git add cmd/root/collection/collector.go cmd/root/collection/rockscollect.go cmd/root/collection/streaming_collect.go
git commit -m "refactor: rename StartDate, remove DateEnd from collection pipeline"
```

---

### Task 3: CLI flags, globals, validation, and config assembly

**Files:**
- Modify: `cmd/root.go:82-83,570-593,614-619,1133-1135,1342-1344,1411-1424,1576-1581`

- [ ] **Step 1: Remove endDate global**

In `cmd/root.go`, replace lines 82-84:

```go
startDate         string
endDate           string
daysFlag          int
```

with:

```go
startDate string
daysFlag  int
```

- [ ] **Step 2: Simplify diagLogDays()**

In `cmd/root.go`, replace lines 570-593:

```go
// diagLogDays computes the unified day limit for diagnosis mode log collection.
// Returns daysFlag if set, or computes days from --date-start to --date-end (or now), or 0 (no limit).
func diagLogDays() int {
	if daysFlag > 0 {
		return daysFlag
	}
	if startDate != "" {
		start, err := time.Parse("2006-01-02T15:04:05", startDate)
		if err != nil {
			return 0
		}
		end := time.Now()
		if endDate != "" {
			if t, err := time.Parse("2006-01-02T15:04:05", endDate); err == nil {
				end = t
			}
		}
		days := int(end.Sub(start).Hours()/24) + 1
		if days > 0 {
			return days
		}
	}
	return 0
}
```

with:

```go
// diagLogDays returns the unified day limit for diagnosis mode log collection.
func diagLogDays() int {
	return daysFlag
}
```

- [ ] **Step 3: Remove endDate from BuildConfData()**

In `cmd/root.go`, replace lines 614-619:

```go
if startDate != "" {
	confData[conf.KeyStartDate] = startDate
}
if endDate != "" {
	confData[conf.KeyEndDate] = endDate
}
```

with:

```go
if startDate != "" {
	confData[conf.KeyStartDate] = startDate
}
```

- [ ] **Step 4: Update collection.Args construction**

In `cmd/root.go`, replace lines 1133-1135:

```go
DiagLogDays:           diagLogDays(),
DateStart:             startDate,
DateEnd:               endDate,
```

with:

```go
DiagLogDays: diagLogDays(),
StartDate:   startDate,
```

- [ ] **Step 5: Rename --date-start to --start-date, remove --date-end flag**

In `cmd/root.go`, replace lines 1342-1344:

```go
cmd.Flags().StringVar(&startDate, "date-start", "", "start of collection date range (ISO 8601, e.g. 2026-03-20T10:00:00)")
cmd.Flags().StringVar(&endDate, "date-end", "", "end of collection date range (ISO 8601). Defaults to now if --date-start is set")
cmd.Flags().IntVar(&daysFlag, "days", conf.GetIntDefault(diagDef, conf.KeyDremioLogsNumDays), "number of days to collect (sets end=now, start=now-N). Overridden by --date-start")
```

with:

```go
cmd.Flags().StringVar(&startDate, "start-date", "", "start of collection date range (date-only, e.g. 2026-03-20). Defaults to now minus --days")
cmd.Flags().IntVar(&daysFlag, "days", conf.GetIntDefault(diagDef, conf.KeyDremioLogsNumDays), "number of days to collect from --start-date (default: 3)")
```

- [ ] **Step 6: Simplify validateV4Flags()**

In `cmd/root.go`, replace lines 1416-1424:

```go
// --date-start takes precedence over --days (same as TUI behavior)
if daysFlag > 0 && startDate != "" {
	simplelog.Infof("--date-start provided, ignoring --days=%d", daysFlag)
	daysFlag = 0
}
// --date-end without --date-start
if endDate != "" && startDate == "" {
	return fmt.Errorf("--date-start is required when --date-end is specified")
}
```

with:

```go
// Validate --start-date format if provided
if startDate != "" {
	if _, err := time.Parse("2006-01-02", startDate); err != nil {
		return fmt.Errorf("--start-date must be date-only format (e.g. 2026-03-20), got %q", startDate)
	}
}
```

- [ ] **Step 7: Update TUI → globals assignment**

In `cmd/root.go`, replace lines 1576-1581:

```go
daysFlag = cfg.Days
startDate = cfg.DateStart
endDate = cfg.DateEnd
if cfg.DateStart != "" {
	daysFlag = 0
}
```

with:

```go
daysFlag = cfg.Days
startDate = cfg.DateStart
```

- [ ] **Step 8: Build**

```bash
go build -o bin/ddc.exe .
```

Expected: build succeeds.

- [ ] **Step 9: Commit**

```bash
git add cmd/root.go
git commit -m "refactor: rename --start-date, remove --date-end, simplify diagLogDays"
```

---

### Task 4: TUI — DiagnosisConfig struct and form fields

**Files:**
- Modify: `cmd/configui/configui.go:96-150,260-269,532-546,646-654,681,926-940`

- [ ] **Step 1: Remove DateEnd from DiagnosisConfig struct**

In `cmd/configui/configui.go`, replace lines 113-115:

```go
Days      int
DateStart string
DateEnd   string
```

with:

```go
Days      int
DateStart string
```

- [ ] **Step 2: Rename validateISO8601 to validateDateOnly and update format**

In `cmd/configui/configui.go`, replace lines 260-269:

```go
func validateISO8601(s string) error {
	if s == "" {
		return nil // empty = auto from days
	}
	_, err := time.Parse("2006-01-02T15:04:05", s)
	if err != nil {
		return fmt.Errorf("use ISO 8601 format: 2006-01-02T15:04:05")
	}
	return nil
}
```

with:

```go
func validateDateOnly(s string) error {
	if s == "" {
		return nil // empty = auto from days
	}
	_, err := time.Parse("2006-01-02", s)
	if err != nil {
		return fmt.Errorf("use date format: 2006-01-02")
	}
	return nil
}
```

- [ ] **Step 3: Update Logs Collection fields — rename, add PlaceholderFunc, remove Date End**

In `cmd/configui/configui.go`, replace lines 533-547:

```go
logsCollectionFields := []huh.Field{
	huh.NewInput().Title("Days to collect").Value(&days.str).CharLimit(4).Validate(validateInt),
	huh.NewInput().Title("Date Start").Value(&dateStart).Placeholder("e.g. 2026-04-01T10:00:00").Validate(validateISO8601),
	huh.NewInput().Title("Date End").Value(&dateEnd).
		DescriptionFunc(func() string {
			if n, err := strconv.Atoi(days.str); err == nil && n > 0 {
				now := time.Now()
				start := now.AddDate(0, 0, -n).Format("2006-01-02T15:04:05")
				end := now.Format("2006-01-02T15:04:05")
				return fmt.Sprintf("Auto from %d days: %s → %s", n, start, end)
			}
			return ""
		}, &days.str).
		Placeholder("e.g. 2026-04-07T10:00:00").Validate(validateISO8601),
}
```

with:

```go
logsCollectionFields := []huh.Field{
	huh.NewInput().Title("Days to collect").Value(&days.str).CharLimit(4).Validate(validateInt),
	huh.NewInput().Title("Start Date").Value(&dateStart).
		PlaceholderFunc(func() string {
			if n, err := strconv.Atoi(days.str); err == nil && n > 0 {
				return time.Now().AddDate(0, 0, -n).Format("2006-01-02") + " [auto]"
			}
			return ""
		}, &days.str).
		Validate(validateDateOnly),
}
```

- [ ] **Step 4: Simplify post-form date resolution**

In `cmd/configui/configui.go`, replace lines 646-655:

```go
// Post-form date resolution: explicit dates override days.
if dateStart != "" {
	cfg.DateStart = dateStart
	if dateEnd != "" {
		cfg.DateEnd = dateEnd
	} else {
		cfg.DateEnd = time.Now().Format("2006-01-02T15:04:05")
	}
	cfg.Days = 0
}
```

with:

```go
// Post-form date resolution: start date is optional, days is always set.
if dateStart != "" {
	cfg.DateStart = dateStart
}
```

- [ ] **Step 5: Update buildDiagnosisCLICommand signature and date logic**

In `cmd/configui/configui.go`, replace the signature and date block (lines 926-940):

```go
func buildDiagnosisCLICommand(cfg *DiagnosisConfig, tools *[]string, nodes *[]string, days, diagDur, dateStart, dateEnd *string) string {
	bin, cont, patVar := cliShellFormat()
	var parts []string

	parts = append(parts, bin+" collect "+cfg.Transport+" diagnosis"+cont)
	parts = appendTransportAndPathFlags(parts, cfg.Transport, cfg.Namespace, cfg.K8sContext, cfg.Coordinator, cfg.Executors, cfg.SSHUser, cfg.DremioHome, cfg.CoordinatorLogDir, cfg.ExecutorLogDir, cfg.DremioConfDir, cfg.DremioRocksDBDir, cont)
	// Date range — prefer explicit dates over days
	if dateStart != nil && *dateStart != "" {
		parts = append(parts, fmt.Sprintf("  --date-start=%s"+cont, *dateStart))
		if dateEnd != nil && *dateEnd != "" {
			parts = append(parts, fmt.Sprintf("  --date-end=%s"+cont, *dateEnd))
		}
	} else if days != nil {
		parts = append(parts, fmt.Sprintf("  --days=%s"+cont, *days))
	}
```

with:

```go
func buildDiagnosisCLICommand(cfg *DiagnosisConfig, tools *[]string, nodes *[]string, days, diagDur, dateStart *string) string {
	bin, cont, patVar := cliShellFormat()
	var parts []string

	parts = append(parts, bin+" collect "+cfg.Transport+" diagnosis"+cont)
	parts = appendTransportAndPathFlags(parts, cfg.Transport, cfg.Namespace, cfg.K8sContext, cfg.Coordinator, cfg.Executors, cfg.SSHUser, cfg.DremioHome, cfg.CoordinatorLogDir, cfg.ExecutorLogDir, cfg.DremioConfDir, cfg.DremioRocksDBDir, cont)
	// Start date + days
	if dateStart != nil && *dateStart != "" {
		parts = append(parts, fmt.Sprintf("  --start-date=%s"+cont, *dateStart))
	}
	if days != nil {
		parts = append(parts, fmt.Sprintf("  --days=%s"+cont, *days))
	}
```

- [ ] **Step 6: Update the call site for buildDiagnosisCLICommand**

In `cmd/configui/configui.go`, replace line 681:

```go
cfg.GeneratedCommand = buildDiagnosisCLICommand(cfg, &selectedTools, &selectedNodes, &days.str, &diagDur.str, &dateStart, &dateEnd)
```

with:

```go
cfg.GeneratedCommand = buildDiagnosisCLICommand(cfg, &selectedTools, &selectedNodes, &days.str, &diagDur.str, &dateStart)
```

- [ ] **Step 7: Remove the `dateEnd` local variable declaration**

Search for `dateEnd` in the local variables section of `runDiagnosisConfigScreen` (should be near other variable declarations). Remove the declaration of `dateEnd string`. The `dateStart` variable stays.

- [ ] **Step 8: Build and run tests**

```bash
go build -o bin/ddc.exe .
go test -short ./cmd/configui/...
```

Expected: build succeeds, tests will fail (they reference old function signature and old flag names). That's expected — we fix them in Task 5.

- [ ] **Step 9: Commit**

```bash
git add cmd/configui/configui.go
git commit -m "refactor: update TUI — rename Start Date, remove Date End, add PlaceholderFunc"
```

---

### Task 5: Update tests

**Files:**
- Modify: `cmd/configui/configui_test.go:271-345`

- [ ] **Step 1: Update TestBuildDiagnosisCLICommand_DateMode**

In `cmd/configui/configui_test.go`, replace the test (lines 271-293):

```go
func TestBuildDiagnosisCLICommand_DateMode(t *testing.T) {
	days := "3"
	dur := "30"
	ds := "2026-04-01T10:00:00"
	de := "2026-04-07T10:00:00"
	cfg := &DiagnosisConfig{
		Namespace:         "default",
		CoordinatorLogDir: "/var/log/dremio",
		ExecutorLogDir:    "/var/log/dremio",
		DremioConfDir:     "/opt/dremio/conf",
		DremioRocksDBDir:  "/opt/dremio/data/db",
	}
	cmd := buildDiagnosisCLICommand(cfg, &allTools, nil, &days, &dur, &ds, &de)
	if !strings.Contains(cmd, "--date-start=2026-04-01T10:00:00") {
		t.Errorf("expected --date-start flag, got:\n%s", cmd)
	}
	if !strings.Contains(cmd, "--date-end=2026-04-07T10:00:00") {
		t.Errorf("expected --date-end flag, got:\n%s", cmd)
	}
	if strings.Contains(cmd, "--days") {
		t.Errorf("expected no --days flag in date mode, got:\n%s", cmd)
	}
}
```

with:

```go
func TestBuildDiagnosisCLICommand_DateMode(t *testing.T) {
	days := "3"
	dur := "30"
	ds := "2026-04-01"
	cfg := &DiagnosisConfig{
		Namespace:         "default",
		CoordinatorLogDir: "/var/log/dremio",
		ExecutorLogDir:    "/var/log/dremio",
		DremioConfDir:     "/opt/dremio/conf",
		DremioRocksDBDir:  "/opt/dremio/data/db",
	}
	cmd := buildDiagnosisCLICommand(cfg, &allTools, nil, &days, &dur, &ds)
	if !strings.Contains(cmd, "--start-date=2026-04-01") {
		t.Errorf("expected --start-date flag, got:\n%s", cmd)
	}
	if !strings.Contains(cmd, "--days=3") {
		t.Errorf("expected --days=3 alongside --start-date, got:\n%s", cmd)
	}
	if strings.Contains(cmd, "--date-end") {
		t.Errorf("expected no --date-end flag, got:\n%s", cmd)
	}
}
```

- [ ] **Step 2: Update TestBuildDiagnosisCLICommand_DaysMode**

In `cmd/configui/configui_test.go`, replace the test (lines 295-317):

```go
func TestBuildDiagnosisCLICommand_DaysMode(t *testing.T) {
	days := "3"
	dur := "30"
	ds := ""
	de := ""
	cfg := &DiagnosisConfig{
		Namespace:         "default",
		CoordinatorLogDir: "/var/log/dremio",
		ExecutorLogDir:    "/var/log/dremio",
		DremioConfDir:     "/opt/dremio/conf",
		DremioRocksDBDir:  "/opt/dremio/data/db",
	}
	cmd := buildDiagnosisCLICommand(cfg, &allTools, nil, &days, &dur, &ds, &de)
	if !strings.Contains(cmd, "--days=3") {
		t.Errorf("expected --days=3 in days mode, got:\n%s", cmd)
	}
	if strings.Contains(cmd, "--date-start") {
		t.Errorf("expected no --date-start in days mode, got:\n%s", cmd)
	}
	if strings.Contains(cmd, "--date-end") {
		t.Errorf("expected no --date-end in days mode, got:\n%s", cmd)
	}
}
```

with:

```go
func TestBuildDiagnosisCLICommand_DaysMode(t *testing.T) {
	days := "3"
	dur := "30"
	ds := ""
	cfg := &DiagnosisConfig{
		Namespace:         "default",
		CoordinatorLogDir: "/var/log/dremio",
		ExecutorLogDir:    "/var/log/dremio",
		DremioConfDir:     "/opt/dremio/conf",
		DremioRocksDBDir:  "/opt/dremio/data/db",
	}
	cmd := buildDiagnosisCLICommand(cfg, &allTools, nil, &days, &dur, &ds)
	if !strings.Contains(cmd, "--days=3") {
		t.Errorf("expected --days=3 in days mode, got:\n%s", cmd)
	}
	if strings.Contains(cmd, "--start-date") {
		t.Errorf("expected no --start-date in days mode, got:\n%s", cmd)
	}
}
```

- [ ] **Step 3: Update TestValidateISO8601 → TestValidateDateOnly**

In `cmd/configui/configui_test.go`, replace the test (lines 319-345):

```go
func TestValidateISO8601(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"empty string (auto)", "", false},
		{"valid datetime", "2026-04-01T10:00:00", false},
		{"valid datetime midnight", "2026-12-31T00:00:00", false},
		{"date only no time", "2026-04-01", true},
		{"garbage string", "not-a-date", true},
		{"space instead of T", "2026-04-01 10:00:00", true},
		{"partial date", "2026-04", true},
		{"missing seconds", "2026-04-01T10:00", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateISO8601(tc.input)
			if tc.wantErr && err == nil {
				t.Errorf("validateISO8601(%q) expected error, got nil", tc.input)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("validateISO8601(%q) unexpected error: %v", tc.input, err)
			}
		})
	}
}
```

with:

```go
func TestValidateDateOnly(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"empty string (auto)", "", false},
		{"valid date", "2026-04-01", false},
		{"valid date end of year", "2026-12-31", false},
		{"datetime with time", "2026-04-01T10:00:00", true},
		{"garbage string", "not-a-date", true},
		{"partial date", "2026-04", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateDateOnly(tc.input)
			if tc.wantErr && err == nil {
				t.Errorf("validateDateOnly(%q) expected error, got nil", tc.input)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("validateDateOnly(%q) unexpected error: %v", tc.input, err)
			}
		})
	}
}
```

- [ ] **Step 4: Run all tests**

```bash
go test -short ./cmd/configui/...
go test -short ./cmd/local/conf/...
go test -short ./cmd/root/collection/...
```

Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add cmd/configui/configui_test.go
git commit -m "test: update date/days tests for new start-date format"
```

---

### Task 6: Final validation

- [ ] **Step 1: Full build**

```bash
go build -o bin/ddc.exe .
```

- [ ] **Step 2: Run all unit tests**

```bash
go test -short ./...
```

Expected: all pass.

- [ ] **Step 3: Run linter**

```bash
go fmt ./...
golangci-lint run
```

Expected: no issues.

- [ ] **Step 4: Grep for any remaining references to old names**

Search for `date-start`, `date-end`, `DateEnd`, `endDate`, `dateEnd` in Go files (excluding docs/specs). Fix any stragglers.

```bash
grep -rn "date-start\|date-end\|DateEnd\|endDate\|dateEnd" --include="*.go" cmd/ pkg/
```

Expected: no matches (only `start-date` and `StartDate`/`startDate` should appear).

- [ ] **Step 5: Commit any fixes if needed**
