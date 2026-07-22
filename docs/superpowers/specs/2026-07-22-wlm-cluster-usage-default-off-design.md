# Disable wlm_cluster_usage collection by default (opt-in flag)

Date: 2026-07-22

## Problem

Issue [#338](https://github.com/dremio/dremio-diagnostic-collector/issues/338): the
`wlm_cluster_usage` export scans every profile in the RocksDB catalog and dominated an
8.5-hour collection run. The upstream viewer fix (re-vendored in commit `5868a36`) makes the
scan substantially faster (~46s on a 430k-profile catalog), but it remains the most expensive
WLM export and its cost scales with catalog size — unlike `wlm_queues`, `wlm_rules`, and
`wlm_engines`, which are cheap point reads.

Today a single `collect-wlm` boolean (default `true` in both modes) gates all four WLM types
in one loop (`cmd/root/collection/rockscollect.go:135,233`). There is no way to keep the
cheap WLM config exports while skipping the expensive usage scan.

## Goals

- `wlm_cluster_usage` is **not** collected by default, in both standard and diagnosis modes.
- The other three WLM types remain collected by default (under `collect-wlm`, still `true`).
- Users can opt back in explicitly (CLI flag, config key, or TUI toggle).
- A skipped usage export leaves a trace in `ddc.log`.

## Non-goals

- No per-type flags for the other three WLM types.
- No change to the vendored dremio-rocksdb-viewer binary or its CLI contract.
- No day-window filtering of the usage aggregation (possible follow-up noted in #338).
- No streamed progress for the usage scan (optional follow-up).

## Design

New boolean config key **`collect-wlm-cluster-usage`**, default **`false`** in both mode
profiles. Gating semantics: `wlm_cluster_usage` is exported only when `collect-wlm` **and**
`collect-wlm-cluster-usage` are both true. The flag follows the exact wiring pattern of the
existing `collect-wlm` knob.

### Touch points

1. **`cmd/local/conf/conf_key_names.go`** — add `KeyCollectWLMClusterUsage = "collect-wlm-cluster-usage"`.
2. **`cmd/local/conf/defaults.go`** — `setDefault(confData, KeyCollectWLMClusterUsage, false)`
   in both `DiagnosisCollectionProfile` and `StandardCollectionProfile`, with a comment noting
   the #338 rationale.
3. **`cmd/root.go`** —
   - new package var `collectWLMClusterUsage bool` beside `collectWLM`;
   - register `--collect-wlm-cluster-usage` in the standard-commands flag loop (from `stdDef`)
     and the diagnosis-commands flag loop (from `diagDef`), help text
     `"collect WLM cluster usage data (can be slow on large catalogs)"`;
   - pass `CollectWLMClusterUsage: collectWLMClusterUsage` into `CollectionArgs`;
   - apply the TUI result in both dispatch blocks
     (`collectWLMClusterUsage = cfg.CollectWLMClusterUsage`, beside the existing
     `collectWLM = cfg.CollectWLM` at ~1613 and ~1717).
4. **`cmd/configui/configui.go`** —
   - `CollectWLMClusterUsage bool` field on `StandardConfig` and `DiagnosisConfig`;
   - seed from `conf.GetBoolDefault(...)` in both config constructors;
   - a `huh.NewConfirm().Title("Collect WLM cluster usage")` right after the existing
     "Collect WLM configuration" confirm in both forms;
   - emit `--collect-wlm-cluster-usage=%t` in `buildStandardCLICommand` (on the
     `--collect-wlm` line) and `buildDiagnosisCLICommand` (on the WLM/PAT-collections line).
5. **`cmd/root/collection/collector.go`** — `CollectWLMClusterUsage bool` on `CollectionArgs`.
6. **`cmd/root/collection/streaming_collect.go`** — thread the field into the single
   `RocksCollectArgs` construction (~line 982).
7. **`cmd/root/collection/rockscollect.go`** — `CollectWLMClusterUsage bool` on
   `RocksCollectArgs`; in the WLM loop, skip `wlm_cluster_usage` with a
   `simplelog.Infof` line when the flag is false.

Neither mode-guarding nor Cobra `BoolVar` init-time leakage is a concern: the default is
`false` in both modes, so the registration order cannot change behavior.

## Tests

- **`cmd/local/conf/defaults_test.go`** — `{conf.KeyCollectWLMClusterUsage, false}` rows in
  both the diagnosis and standard default checks.
- **`cmd/root/collection/rockscollect_test.go`** —
  - `TestWLMFileLayout` gains `CollectWLMClusterUsage: true` (keeps asserting all four files);
  - new `TestWLMClusterUsageSkippedByDefault`: with the field left false, `queues.json`,
    `rules.json`, `engines.json` are collected, `cluster_usage.json` is absent, and no host
    command contains `-type wlm_cluster_usage`.
- **`cmd/configui/configui_test.go`** — standard command string contains
  `--collect-wlm-cluster-usage=false` by default; diagnosis command string contains
  `--collect-wlm-cluster-usage=true` when enabled.

## Build / verification

`go test -short ./cmd/local/conf/... ./cmd/root/collection/... ./cmd/configui/...`,
`go vet ./...`, `golangci-lint run`, then `go build -o bin/ddc.exe .`.

(Windows note: the `rockscollect` test binary embeds UPX-compressed ELF binaries and may be
blocked by Defender when run from Go's temp dir; compile with `go test -c` into the workspace
and run it there if that occurs.)

## Risks

- Support engineers accustomed to finding `cluster_usage.json` in bundles will stop getting
  it unless they pass the new flag — intended, but worth a mention when closing #338.
- Config files that set only `collect-wlm: true` previously implied usage collection; after
  this change they must also set `collect-wlm-cluster-usage: true`. This is the requested
  behavior change.
