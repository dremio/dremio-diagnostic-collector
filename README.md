[![Go Report Card](https://goreportcard.com/badge/github.com/dremio/dremio-diagnostic-collector/v4)](https://goreportcard.com/report/github.com/dremio/dremio-diagnostic-collector/v4)

Automated log and analytics collection for Dremio clusters

## Important Links

* Read the [FAQ](FAQ.md) for common questions on setting up DDC
* Read the [official Dremio Support page](https://support.dremio.com/hc/en-us/articles/15560006579739) for more details on DDC
* Read the [DDC Diagnostic Tarball Contents](docs/ddc-tarball.md) to know what is saved by a DDC tarball
* Read the [Architecture Docs](docs/architecture/) for design decisions, patterns, and development history

### Install DDC on your local machine

Download the [latest release binary](https://github.com/dremio/dremio-diagnostic-collector/releases/latest):

1. Unzip the binary
2. Open a terminal and change to the directory where you unzipped your binary
3. Run the command `./ddc help`. If you see the DDC command help, you are good to go.

## Recommended: Guided Collection (Interactive TUI)

**The easiest way to use DDC is the interactive TUI.** Just run the binary with no subcommand:

```bash
ddc
```

The TUI walks you through every decision step by step, so you don't have to memorize flags:

1. **Transport** — Kubernetes, SSH, Local, or Local-K8s
2. **Collection mode** — `standard` (routine) or `diagnosis` (full incident investigation)
3. **Paths** — log / config / RocksDB directories, pre-filled with autodetected values
4. **What to collect** — log types, JVM diagnostic tools, and (for diagnosis) API-based collections
5. **Node selection** — pick which coordinators/executors to collect from (Kubernetes & SSH)
6. **API access** (diagnosis only) — Dremio endpoint and PAT token, when API-based collection (KV store report / problematic job profiles) is enabled

Sensible defaults are filled in at each step, and most fields explain themselves inline, so you can usually accept the suggestions and move on.

### It generates a reusable command for you

This is the main reason to prefer the TUI: before collection starts, the final screen shows the **exact equivalent `ddc collect …` command** that reflects all of your choices. Copy it once and you can:

- re-run the identical collection non-interactively — in CI/CD pipelines, cron jobs, or runbooks
- replay it across multiple clusters without clicking through the TUI again
- share it with a colleague or attach it to a support ticket so the collection is reproducible

The PAT is always shown as an environment-variable reference (e.g. `$DDC_PAT_TOKEN`), never the literal token, so the command is safe to copy and share. An abbreviated example of what the TUI prints:

```bash
ddc collect k8s diagnosis \
  --namespace mynamespace \
  --days=3 \
  --diag-jfr=true --diag-jstack=true --diag-top=true \
  --diag-async-profiler=true --diag-time-seconds=60 --diag-heap-dump=false \
  --collect-problematic-profiles=true --collect-wlm=true \
  --dremio-pat-token=$DDC_PAT_TOKEN
```

> **Diagnosis mode is gated behind a support password.** In the TUI, selecting `diagnosis` prompts for a password (`support`) before continuing; `standard` mode is available to everyone. The non-interactive `diagnosis` subcommands are likewise labelled "Support only".

The rest of this README documents the non-interactive `ddc collect …` form — the same commands the TUI generates — for when you already know what you want or need to script DDC.

### Architecture Overview

DDC collects diagnostics from Dremio clusters using a **streaming transport** — files are transferred directly from each node to your local machine through a pipe, with no intermediate staging on the remote node.

- **Kubernetes**: DDC uses the Kubernetes API to stream file contents from each pod via `cat`.
- **SSH**: DDC opens an SSH session to each node and streams file contents via `cat` over the SSH channel.
- **Local**: DDC collects diagnostics directly on the current host (no remote transport). Useful for standalone Dremio installations.
- **Local-K8s**: DDC runs from inside a Dremio coordinator pod, collecting local files plus Kubernetes cluster info via the API. Useful when you cannot reach the cluster from outside.

**Remote JVM collection**: JVM diagnostics (jcmd for JFR, jstack for thread dumps, top for process snapshots) are executed remotely on each Dremio node. Async-profiler is streamed as a binary to the remote node via stdin and executed in place. All results are streamed back — no binaries are left behind.

### Command Structure

DDC v4 uses a subcommand-based CLI:

```
ddc collect <transport> <mode> [flags]
```

Where `<transport>` is `k8s`, `ssh`, `local`, or `local-k8s`, and `<mode>` is `standard` or `diagnosis`.

### Collection Modes

| Mode | Purpose | Default behavior |
|------|---------|-----------------|
| `diagnosis` | Active incident investigation | All log types (3 days), GC logs, hs_err crash dumps, system tables, and (with a PAT) problematic job profiles. JVM tools (JFR, jstack, top, async-profiler) are **opt-in** via `--diag-*` flags — off by default. Parallel collection. |
| `standard` | Routine collection / performance review | Lightweight: server logs (1 day), queries.json (30 days), tracker.json, vacuum logs, config, OS info. Sequential. |

All settings are configured via CLI flags. There is no `ddc.yaml` configuration file.

> Prefer the [interactive TUI](#recommended-guided-collection-interactive-tui) (`ddc` with no subcommand) unless you are scripting — it builds the commands below for you.

### Scripting - Dremio on Kubernetes

DDC connects via the Kubernetes API and streams logs and files from each Dremio pod, then archives the results locally.

For Kubernetes deployments _(relies on a kubeconfig at `$HOME/.kube/config` or `$KUBECONFIG`)_:

##### standard collection
```bash
ddc collect k8s standard --namespace mynamespace
```

##### diagnosis collection
```bash
ddc collect k8s diagnosis --namespace mynamespace
```

##### diagnosis with PAT-based collection (problematic job profiles + KV store report)
A PAT is only used for two diagnosis-mode collectors — problematic job profiles and the KV store
report — and both are opt-in, so you must enable them explicitly. System tables, WLM, and
queries-performance data are read from RocksDB and need no PAT. _Requires Dremio admin privileges._
```bash
export DDC_PAT_TOKEN="your-token-here"
ddc collect k8s diagnosis --namespace mynamespace \
  --collect-problematic-profiles --collect-kvstore-report --dremio-pat-token "$DDC_PAT_TOKEN"
```

> System tables and WLM are collected from RocksDB by default in both modes — no PAT required.
> Standard mode does not use a PAT at all.

### Scripting - Dremio on-prem (SSH)

Specify executors with the `-e` flag and coordinators with the `-c` flag. Specify SSH user and SSH key.

##### coordinator only
```bash
ddc collect ssh standard --coordinator 10.0.0.19 --ssh-user myuser
```

##### coordinator and executors
```bash
ddc collect ssh standard --coordinator 10.0.0.19 --executors 10.0.0.20,10.0.0.21,10.0.0.22 --ssh-user myuser
```

##### diagnosis with API collection
_Requires Dremio admin privileges._
```bash
ddc collect ssh diagnosis --coordinator 10.0.0.19 --executors 10.0.0.20,10.0.0.21,10.0.0.22 --sudo-user dremio --ssh-user myuser --dremio-pat-token "$DDC_PAT_TOKEN"
```

### Scripting - Local Collection

Collect diagnostics directly on the Dremio host (no SSH or Kubernetes required):

```bash
ddc collect local standard
ddc collect local diagnosis --days 5
```

To collect from inside a Dremio coordinator pod and also gather Kubernetes cluster info via the API:

```bash
ddc collect local-k8s standard
ddc collect local-k8s diagnosis --kubeconfig /path/to/kubeconfig
```

### Date-Range Filtering (Diagnosis Mode)

In diagnosis mode, `--days` and `--start-date` control which log files are collected across all log types.

```bash
# Collect last 5 days (counting back from now)
ddc collect k8s diagnosis --namespace mynamespace --days 5

# Collect 3 days starting from a specific date
ddc collect ssh diagnosis --coordinator 10.0.0.19 --ssh-user myuser --start-date 2026-03-20 --days 3
```

### Windows Users

If you are running DDC from Windows, always run in a shell from the `C:` drive prompt.
This is because of a limitation of kubectl (see https://github.com/kubernetes/kubernetes/issues/77310).

### Key CLI Flags

These flags apply to `ddc collect <transport> <mode>` (non-interactive mode). In interactive mode (`ddc` with no subcommand), most settings are prompted.

#### Transport Flags

**SSH** (`ddc collect ssh ...`):

| Flag | Description |
|------|-------------|
| `-c, --coordinator` | Coordinator IP(s), comma-separated |
| `-e, --executors` | Executor IP(s), comma-separated |
| `-s, --ssh-key` | SSH private key file path |
| `-u, --ssh-user` | SSH user for login |
| `-b, --sudo-user` | Sudo user for privileged commands (e.g. jcmd) |
| `--ssh-strict-host-keys` | Enable strict host key checking (default: false) |

**Kubernetes** (`ddc collect k8s ...`):

| Flag | Description |
|------|-------------|
| `-n, --namespace` | K8s namespace |
| `-x, --context` | K8s context to use |
| `--kubeconfig` | Path to kubeconfig file (overrides `$KUBECONFIG` and `~/.kube/config`) |
| `--detect-label-selector` | K8s label selector to identify Dremio coordinator/executor pods (default: `role=dremio-cluster-pod`) |
| `-l, --container-log-label-selector` | K8s label selector to filter which pods' container logs are collected (default: empty = all namespace pods) |
| `-d, --enable-kubectl` | Use kubectl CLI instead of embedded K8s API client |
| `--collect-container-logs` | Collect K8s container logs (default: enabled for diagnosis) |
| `--nodes` | Collect from specific nodes only (comma-separated) |
| `--exclude-nodes` | Exclude specific nodes (comma-separated) |

**Local** (`ddc collect local ...`):

| Flag | Description |
|------|-------------|
| `--dremio-home` | Dremio installation directory (default: `/opt/dremio`) |
| `--local-log-dir` | Log directory on this node (autodetected if not specified) |

**Local-K8s** (`ddc collect local-k8s ...`):

| Flag | Description |
|------|-------------|
| `--dremio-home` | Dremio installation directory (default: `/opt/dremio`) |
| `--local-log-dir` | Log directory on this node (autodetected if not specified) |
| `--kubeconfig` | Path to kubeconfig file used when in-cluster config is unavailable |

#### Authentication (Diagnosis Only)

These flags are only registered on the `diagnosis` subcommands, and the PAT is used only for the two REST-API collectors — the KV store report (`--collect-kvstore-report`) and problematic job profiles (`--collect-problematic-profiles`). Standard mode does not use a PAT (its system tables and WLM data come from RocksDB).

| Flag | Description |
|------|-------------|
| `--dremio-pat-token` | Dremio PAT token for API-based collection (env: `DDC_PAT_TOKEN`) |
| `--dremio-endpoint` | Dremio REST API endpoint (e.g. `http://localhost:9047`) |
| `--allow-insecure-ssl` | Allow insecure SSL connections to the Dremio REST API (default: true) |

#### Date Range (Diagnosis Only)
| Flag | Description |
|------|-------------|
| `--days` | Number of days to collect (default: 3, applies to all log types) |
| `--start-date` | Start of date range (date-only, e.g. `2026-03-20`). Defaults to now minus `--days` |

#### Diagnostic Tool Toggles (Diagnosis Only)

All `--diag-*` tools are **opt-in** (default `false`) in both CLI and TUI.

| Flag | Default | Description |
|------|---------|-------------|
| `--diag-jfr` | false | Collect Java Flight Recorder recording |
| `--diag-jstack` | false | Collect jstack thread dumps |
| `--diag-top` | false | Collect top process snapshots |
| `--diag-async-profiler` | false | Collect async-profiler recording |
| `--diag-heap-dump` | false | Collect heap dump |
| `--diag-time-seconds` | 60 | Duration in seconds for all diagnostic tools |

#### Log Collection Toggles

`N/A` means the flag is not registered for that mode. (The reflection log is collected in diagnosis mode and skipped in standard mode, but is not exposed as a CLI flag.)

| Flag | Default (diagnosis) | Default (standard) |
|------|--------------------|--------------------|
| `--collect-server-logs` | true | true |
| `--collect-queries-json` | true | true |
| `--collect-queries-perf-json` | true | true |
| `--collect-tracker-json` | true | true |
| `--collect-vacuum-log` | true | true |
| `--collect-meta-refresh-log` | true | false |
| `--collect-gc-logs` | true | N/A |
| `--collect-acceleration-log` | true | N/A |
| `--collect-access-log` | true | N/A |
| `--collect-hive-deprecated-log` | true | N/A |
| `--collect-hs-err-files` | true | N/A |

#### Per-Log Day Counts (Standard Mode Only)
| Flag | Default | Description |
|------|---------|-------------|
| `--server-logs-num-days` | 1 | Days of server logs to collect |
| `--tracker-json-num-days` | 1 | Days of tracker.json to collect |
| `--vacuum-log-num-days` | 1 | Days of vacuum.json to collect |
| `--queries-json-num-days` | 30 | Days of queries.json to collect |
| `--queries-perf-num-days` | 30 | Days of queries-performance data to collect |

#### Workload, System Tables & API Collection

WLM, system tables, queries-performance data, and cluster stats are read from the coordinator's RocksDB store and need **no** PAT. Only the KV store report and problematic job profiles use the REST API and require a PAT (diagnosis mode only).

| Flag | Default (diagnosis) | Default (standard) | Needs PAT |
|------|--------------------|--------------------|-----------|
| `--collect-wlm` | true | true | no |
| `--system-tables` | default list | default list | no |
| `--collect-kvstore-report` | false | N/A | yes |
| `--collect-problematic-profiles` | false | N/A | yes |

The default `--system-tables` list is `version,options,roles,membership,privileges,reflections,materializations,refreshes,reflection_dependencies`.

#### Directory Overrides
| Flag | Description |
|------|-------------|
| `--coordinator-log-dir` | Coordinator log directory (autodetected if not set) |
| `--executor-log-dir` | Executor log directory (autodetected if not set) |
| `--dremio-conf-dir` | Dremio configuration directory (autodetected if not set) |
| `--dremio-rocksdb-dir` | Dremio RocksDB directory (autodetected if not set) |

#### Control
| Flag | Description |
|------|-------------|
| `--output-file` | Name and location of the diagnostic tarball |
| `--collection-threads` | Concurrent node collection (0 = mode default: 20 diagnosis, 5 standard) |
| `--collector-timeout` | Per-collector timeout (default: 20m diagnosis, 10m standard) |
| `--progress=json` | Machine-readable NDJSON progress output for CI/CD |
| `--skip-version-check` | Skip update check at startup |
| `--disable-free-space-check` | Skip disk space check |

### ddc usage

```
ddc connects via ssh or kubectl and collects a series of logs and files
for dremio, then puts those collected files in an archive

examples:

for interactive mode just run:
        ddc

for non-interactive collection use the collect subcommand with a transport:

for kubernetes deployments:
        ddc collect k8s standard --namespace mynamespace
        ddc collect k8s diagnosis --namespace mynamespace --dremio-pat-token $DDC_PAT_TOKEN

for ssh based communication to VMs or Bare metal hardware:
        ddc collect ssh standard --coordinator 10.0.0.19 --executors 10.0.0.20,10.0.0.21,10.0.0.22 --ssh-user myuser --ssh-key ~/.ssh/mykey --sudo-user dremio

for local collection (on this host):
        ddc collect local standard
        ddc collect local diagnosis

for local collection inside a Kubernetes pod:
        ddc collect local-k8s standard

Usage:
  ddc [flags]
  ddc [command]

Available Commands:
  collect     Run non-interactive collection with provided flags
  version     Print the version number of DDC
  help        Help about any command
```
