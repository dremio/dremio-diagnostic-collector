# FAQ

## DDC is resource intensive

Use the following CLI flags to reduce resource usage:

* `--parallel-nodes 1` — collect from one node at a time
* `--dremio-jstack-freq-seconds 10` — reduce jstack frequency
* `--transfer-rate-limit 10MB/s` — throttle I/O
* `--mode standard` — use standard mode which is I/O-throttled by default

## DDC tarball is too big

* Use `--mode standard` which collects fewer log types and shorter time ranges
* Use `--days 1` (diagnosis mode) to reduce the collection window
* Use `--collect-gc-logs=false` to skip GC logs
* Use `--compression-level 9` for maximum compression

## DDC is too slow

* Use `--parallel-nodes 10` to collect from more nodes concurrently
* Use `--mode diagnosis` which runs collectors in parallel and without I/O throttling
* Use `--compression-level 1` for fastest compression

## I have a tiny /tmp folder and DDC is filling it up

* For remote collections (SSH/K8s): use `--transfer-dir` to specify a different staging directory and `--output-file` to specify where the final tarball is saved
* For local collections: use `--output-file` at the CLI to specify where the final tarball is saved

## DDC didn't capture what I wanted

* Read the `ddc-HOSTNAME.log` logs and see what errors there are (i.e. grep for ERROR)
* Check the `COLLECTION_MANIFEST.json` in the bundle root — it shows which collectors succeeded, failed, or were skipped on each node
* Are the paths correct? Use `--dremio-log-dir`, `--dremio-conf-dir` if autodetection fails
* Job profiles, KV report, WLM report, and system table report all need `--dremio-pat-token` (or set `DDC_PAT_TOKEN` env var)
* Are you running the latest version of DDC? Check https://github.com/dremio/dremio-diagnostic-collector/releases
* If you are running SSH, did you remember to use `--sudo-user` as the dremio user or as a user with admin rights?

## What is captured by DDC?

DDC has two collection modes. All settings are configurable via CLI flags.

### Standard mode (`--mode standard`)

* Server logs (1 day by default, configurable via `--dremio-logs-num-days`)
* metadata\_refresh.log (1 day)
* reflection.log (1 day)
* queries.json (30 days by default)
* Dremio configuration files (dremio.conf, logback.xml, dremio-env)
* OS config and disk usage
* Kubernetes resource info (pods, nodes, StatefulSets) if K8s mode
* If `--dremio-pat-token` is provided: lightweight system tables (sys.nodes, sys.options, sys.reflections)

### Diagnosis mode (`--mode diagnosis`)

Everything in standard mode, plus:

* All log types for 3 days (configurable via `--days` or `--start-date`/`--end-date`):
  - GC logs, vacuum logs, hs\_err crash dumps
  - tracker.json, hive.deprecated.function.warning.log
* JVM diagnostics:
  - Java Flight Recorder (60 seconds)
  - jstack thread dumps (60 seconds at 1-second intervals)
  - ttop CPU/GC metrics (60 seconds)
  - async-profiler CPU profiling (60 seconds)
* Heap dump (opt-in via `--collect-heap-dump`)
* If `--dremio-pat-token` is provided:
  - Auto-identified job profiles from server.log (failed queries, heap monitor events)
  - Full system tables export
  - WLM configuration
  - KV store report
* Timezone detection (timezone.txt in bundle)

### Optionally

* Access logs: `--collect-access-log`
* Audit logs: `--collect-audit-log`
* Acceleration logs: `--collect-acceleration-log`
* Heap dump: `--collect-heap-dump` (diagnosis only)
