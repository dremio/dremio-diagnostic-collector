# FAQ

DDC v4 uses a subcommand-based CLI: `ddc collect <transport> <mode>`, where `<transport>` is
`k8s`, `ssh`, `local`, or `local-k8s`, and `<mode>` is `standard` or `diagnosis`. There is no
`--mode` flag and no `ddc.yaml` configuration file — every setting is a CLI flag.

## Which collection mode should I use?

* **`standard`** — for capturing routine **usage** data: server logs, queries.json, configuration, system tables, and WLM. This is the day-to-day mode.
* **`diagnosis`** — for **support cases and incident investigation**: the full set of logs plus opt-in JVM diagnostics (JFR, jstack, top, async-profiler, heap dump). Diagnosis is intended for use with Dremio Support and is gated behind a support password in the TUI.

## The tarball is too large

Reduce the number of days collected:

* **Diagnosis:** lower `--days` (e.g. `--days 1`), or use `--start-date` to target a specific window.
* **Standard:** lower the per-log day counts, e.g. `--server-logs-num-days` and `--queries-json-num-days`.

## DDC didn't capture what I expected

* Read `ddc.log` (and the per-node `<node-name>.log` files) in the bundle and grep for `ERROR` / `WARN`
* Check `summary.json` in the bundle root — it records per-node results and any errors
* If path autodetection failed, set the directories explicitly: `--coordinator-log-dir`, `--executor-log-dir`, `--local-log-dir`, `--dremio-conf-dir`, `--dremio-rocksdb-dir`
* For SSH, make sure `--sudo-user` can run `jcmd` / `jstack` (typically the `dremio` user)
* Make sure you are on the latest DDC release: https://github.com/dremio/dremio-diagnostic-collector/releases

## What is captured by DDC?

DDC has two collection modes, selected as a subcommand. All settings are configurable via CLI flags.

### Standard mode (`ddc collect <transport> standard`)

* server.log (1 day by default; `--server-logs-num-days`)
* queries.json (30 days by default; `--queries-json-num-days`)
* queries performance data from RocksDB (30 days; `--queries-perf-num-days`)
* tracker.json (1 day; `--tracker-json-num-days`)
* vacuum.json (1 day; `--vacuum-log-num-days`)
* Dremio configuration files (dremio.conf, dremio-env, logback.xml) with passwords masked
* OS config and disk usage
* WLM configuration and system tables export — both read from RocksDB (no PAT required)
* metadata\_refresh.log — off by default in standard mode; enable with `--collect-meta-refresh-log`
* Kubernetes resource info (k8s / local-k8s transports)

Standard mode does not use a PAT token at all.

### Diagnosis mode (`ddc collect <transport> diagnosis`)

Logs are collected over a date range (`--days`, default 3, or `--start-date`). By default this includes:

* server.log, queries.json, queries performance data, tracker.json, vacuum.json
* GC logs, hs\_err crash dumps, metadata\_refresh.log, reflection.log, acceleration.log, access.log, hive-deprecated.log

JVM diagnostics are **opt-in** — off by default on the CLI, pre-selected in the interactive TUI:

* Java Flight Recorder — `--diag-jfr`
* jstack thread dumps — `--diag-jstack`
* top process snapshots — `--diag-top`
* async-profiler — `--diag-async-profiler`
* heap dump — `--diag-heap-dump`
* duration for all of the above — `--diag-time-seconds` (default 60)

System tables and WLM are read from RocksDB (no PAT). A PAT (`--dremio-pat-token` or `DDC_PAT_TOKEN`) is used only for these two opt-in REST-API collectors:

* problematic job profiles auto-identified from server.log — `--collect-problematic-profiles`
* KV store report — `--collect-kvstore-report`

> Note: access, acceleration, and hive-deprecated logs are collected by default in diagnosis mode
> and are not exposed in standard mode. The `--collect-audit-log` flag from older DDC versions has
> been removed.
