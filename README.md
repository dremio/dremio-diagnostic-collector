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

### Architecture Overview

DDC collects diagnostics from Dremio clusters using a **streaming transport** — files are transferred directly from each node to your local machine through a pipe, with no intermediate staging on the remote node.

- **Kubernetes**: DDC uses the Kubernetes API to stream file contents from each pod via `cat`.
- **SSH**: DDC opens an SSH session to each node and streams file contents via `cat` over the SSH channel.
- **Local**: DDC collects diagnostics directly on the current host (no remote transport). Useful for standalone Dremio installations.

**Remote JVM collection**: JVM diagnostics (jcmd for JFR, jstack for thread dumps, top for process snapshots) are executed remotely on each Dremio node. Async-profiler is streamed as a binary to the remote node via stdin and executed in place. All results are streamed back — no binaries are left behind.

### Command Structure

DDC v4 uses a subcommand-based CLI:

```
ddc collect <transport> <mode> [flags]
```

Where `<transport>` is `k8s`, `ssh`, or `local`, and `<mode>` is `standard` or `diagnosis`.

### Collection Modes

| Mode | Purpose | Default behavior |
|------|---------|-----------------|
| `diagnosis` | Active incident investigation | Full diagnostics: JFR, jstack, top, async-profiler, all logs, GC logs, heap monitor analysis. Parallel collection. |
| `standard` | Routine collection / performance review | Lightweight: server logs (1 day), queries.json (30 days), config. Sequential. |

All settings are configured via CLI flags. There is no `ddc.yaml` configuration file.

### Guided Collection (Interactive TUI)

```bash
ddc
```

The interactive TUI guides you through transport selection (Kubernetes, SSH, or Local), collection mode, path configuration, and collection toggles.

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

##### diagnosis with API-based collection (job profiles, system tables, WLM, KV store)
_Requires Dremio admin privileges. Set `DDC_PAT_TOKEN` env var or use `--dremio-pat-token`._
```bash
export DDC_PAT_TOKEN="your-token-here"
ddc collect k8s diagnosis --namespace mynamespace --dremio-pat-token "$DDC_PAT_TOKEN"
```

##### standard with system tables
```bash
ddc collect k8s standard --namespace mynamespace --dremio-pat-token "$DDC_PAT_TOKEN"
```

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
| `-l, --label-selector` | K8s label selector (default: `role=dremio-cluster-pod`) |
| `-d, --enable-kubectl` | Use kubectl CLI instead of embedded K8s API client |
| `--collect-container-logs` | Collect K8s container logs (default: enabled for diagnosis) |
| `--nodes` | Collect from specific nodes only (comma-separated) |
| `--exclude-nodes` | Exclude specific nodes (comma-separated) |

**Local** (`ddc collect local ...`):

| Flag | Description |
|------|-------------|
| `--dremio-home` | Dremio installation directory (default: `/opt/dremio`) |
| `--local-log-dir` | Log directory on this node (autodetected if not specified) |

#### Authentication
| Flag | Description |
|------|-------------|
| `--dremio-pat-token` | Dremio PAT token (env: `DDC_PAT_TOKEN`) |
| `-t, --pat-prompt` | Prompt for PAT token interactively |
| `--allow-insecure-ssl` | Allow insecure SSL connections (default: true) |
| `--dremio-endpoint` | Dremio REST API endpoint |

#### Date Range (Diagnosis Only)
| Flag | Description |
|------|-------------|
| `--days` | Number of days to collect (default: 3, applies to all log types) |
| `--start-date` | Start of date range (date-only, e.g. `2026-03-20`). Defaults to now minus `--days` |

#### Diagnostic Tool Toggles (Diagnosis Only)
| Flag | Default | Description |
|------|---------|-------------|
| `--diag-jfr` | true | Collect Java Flight Recorder recording |
| `--diag-jstack` | true | Collect jstack thread dumps |
| `--diag-top` | true | Collect top process snapshots |
| `--diag-async-profiler` | true | Collect async-profiler recording |
| `--diag-heap-dump` | false | Collect heap dump |
| `--diag-time-seconds` | 60 | Duration in seconds for all diagnostic tools |

#### Log Collection Toggles
| Flag | Default (diagnosis) | Default (standard) |
|------|--------------------|--------------------|
| `--collect-server-logs` | true | true |
| `--collect-queries-json` | true | true |
| `--collect-gc-logs` | true | N/A |
| `--collect-tracker-json` | false | true |
| `--collect-vacuum-log` | false | true |
| `--collect-acceleration-log` | false | N/A |
| `--collect-access-log` | false | N/A |
| `--collect-hs-err-files` | true | N/A |

#### Per-Log Day Counts (Standard Mode Only)
| Flag | Default | Description |
|------|---------|-------------|
| `--server-logs-num-days` | 1 | Days of server logs to collect |
| `--tracker-json-num-days` | 1 | Days of tracker.json to collect |
| `--vacuum-log-num-days` | 1 | Days of vacuum.json to collect |
| `--dremio-queries-json-num-days` | 30 | Days of queries.json to collect |

#### API Collection (Requires PAT)
| Flag | Default (diagnosis) | Default (standard) |
|------|--------------------|--------------------|
| `--collect-wlm` | true | true |
| `--collect-kvstore-report` | false | false |
| `--collect-problematic-profiles` | true | N/A |
| `--system-tables` | full list | full list |

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
ddc connects via ssh, kubectl, or locally and collects a series of logs and files
for dremio, then puts those collected files in an archive

examples:

for interactive mode just run:
        ddc

for non-interactive collection use the collect subcommand with a transport:

for kubernetes deployments:
        ddc collect k8s standard --namespace mynamespace
        ddc collect k8s diagnosis --namespace mynamespace --dremio-pat-token $DDC_PAT_TOKEN

for ssh based communication to VMs or bare metal hardware:
        ddc collect ssh standard --coordinator 10.0.0.19 --executors 10.0.0.20,10.0.0.21,10.0.0.22 --ssh-user myuser --ssh-key ~/.ssh/mykey --sudo-user dremio

for local collection on the Dremio host:
        ddc collect local standard

Usage:
  ddc [flags]
  ddc [command]

Available Commands:
  collect     Run non-interactive collection with provided flags
  version     Print the version number of DDC
  help        Help about any command
```
