# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Behavioral Rules

- Do what has been asked; nothing more, nothing less
- NEVER create files unless absolutely necessary; prefer editing existing files
- NEVER proactively create documentation files (*.md) or README files unless explicitly requested
- NEVER save working files, text/mds, or tests to the root folder
- ALWAYS read a file before editing it
- NEVER commit secrets, credentials, or .env files
- NEVER commit anything into Git unless explicitly asked by the user
- Do NOT deploy anything in a Kubernetes cluster! You are allowed to read from an existing cluster and run DDC diagnosis against it.
- For implementation tasks, use the `golang-pro` and `context7` skills
- For planning tasks, use the `superpowers` skills (brainstorming, writing-plans, executing-plans, etc.)
- ALWAYS build the binary to `bin/ddc.exe` after code changes (`go build -o bin/ddc.exe .`) and confirm in the last sentence that the binary was built

## Build & Test Commands

```bash
# Build (always run after changes)
go build -o bin/ddc.exe .

# Unit tests (skips integration tests; -race requires CGO_ENABLED=1, unavailable on Windows by default)
go test -race -short ./...

# Unit tests on Windows (no -race)
go test -short ./...

# Single package tests
go test -race -short ./cmd/configui/...
go test -race -short ./cmd/local/conf/...

# Single test
go test -race -short -run TestSpecificName ./cmd/...

# All tests including integration (requires cluster access)
go test -timeout 20m -race ./...

# Lint
go fmt ./...
golangci-lint run

# Coverage
go test -race -short -coverpkg=./... -coverprofile=covprofile ./...
go tool cover -func=covprofile
```

## Architecture

DDC (Dremio Diagnostic Collector) collects diagnostics from Dremio clusters via streaming transports (SSH, Kubernetes, or Local). No intermediate staging on remote nodes.

### Command Tree (Cobra)

```
main.go -> cmd.Execute()
  |-- ddc (no subcommand)              -> interactive TUI -> collect
  |-- ddc collect                      -> non-interactive CLI
  |   |-- ddc collect ssh standard     -> SSH lightweight log collection
  |   |-- ddc collect ssh diagnosis    -> SSH full JVM diagnostics
  |   |-- ddc collect k8s standard     -> K8s lightweight log collection
  |   |-- ddc collect k8s diagnosis    -> K8s full JVM diagnostics
  |   |-- ddc collect local standard   -> Local lightweight log collection
  |   +-- ddc collect local diagnosis  -> Local full JVM diagnostics
  +-- ddc version
```

`cmd/root.go` is the central file (~72KB): all flag definitions, TUI orchestration, config assembly, and collection dispatch live here. Each transport command (`SSHCmd`, `K8sCmd`, `LocalCmd`) has `standard` and `diagnosis` subcommands. Flags are scoped per level: transport-specific on transport command PersistentFlags, mode-specific on leaf command Flags(), shared on `CollectCmd` PersistentFlags.

### Two Collection Modes

| Mode | Behavior |
|------|----------|
| `diagnosis` | JFR, jstack, top, async-profiler, full logs. Parallel per-node. |
| `standard` | Server logs, queries.json, config. Sequential, rate-limited. |

Mode constants live in `pkg/collects/`. Some CLI flags are mode-specific: per-log num-days flags only on standard subcommands, `--diag-time-seconds` only on diagnosis subcommands.

### Key Package Map

- **`cmd/root.go`** -- CLI flags, TUI launch, config assembly, collection dispatch
- **`cmd/configui/`** -- Interactive TUI using charmbracelet/huh forms. `DiagnosisConfig` and `StandardConfig` structs carry form results back to root.go
- **`cmd/root/collection/`** -- Collection orchestration: `streaming_collect.go` manages node-parallel streaming, `jvmcollect.go` remote JVM diagnostics, `nodeinfocollect.go` per-node OS/disk/JVM info, `discovery.go` remote file/PID discovery, `apicollect.go` REST API collections
- **`cmd/local/conf/`** -- Config parsing, key constants (`conf_key_names.go`), defaults per mode (`defaults.go`), HOCON parser
- **`cmd/local/jvmcollect/`** -- JVM diagnostics: JFR, jstack, async-profiler, heap dumps
- **`cmd/root/kubectl/`** -- kubectl CLI transport
- **`cmd/root/kubernetes/`** -- K8s API client transport (WebSocket primary, SPDY fallback)
- **`cmd/root/ssh/`** -- SSH transport
- **`cmd/root/local/`** -- Local transport: streams files via `os.Open`/`io.Copy`, auto-detects node role from dremio.conf
- **`cmd/remotecollect/`** -- REST API-based collection (job profiles, system tables, WLM, KV store)
- **`pkg/consoleprint/`** -- Status screen rendering (progress bars, task counts, per-node status with tool errors)
- **`pkg/shutdown/`** -- Graceful shutdown hooks; `hook.Add()` registers cleanup, `hook.Cleanup()` runs them
- **`pkg/simplelog/`** -- Logging; output goes to `ddc.log` in the archive
- **`pkg/archive/`** -- TAR/GZIP archive creation

### Important Patterns

**Cobra flag registration side effects**: `BoolVar`/`IntVar` set global variables to their default values at `init()` time, not at parse time. This means diagnosis-mode flag defaults (e.g., `--collect-jfr=true`) leak into standard mode unless explicitly guarded with a mode check.

**TUI DescriptionFunc closures**: `huh` form `DescriptionFunc` closures execute during form *rendering*, not after form completion. Use pointers to live data (`&selectedNodes`), not post-form computed values.

**TUI node selection by transport**: In diagnosis mode, K8s auto-discovers pods via the Kubernetes API and shows a multi-select node picker. SSH mode populates the node picker from `--coordinator`/`--executors` flags or API-based discovery (when endpoint + PAT are available). Local mode has no node selection — it collects from the current host only. The node-mapping code in `runDiagnosisConfigScreen` branches by transport (K8s, SSH, Local).

**No `sh -c` wrapping for SSH remote commands**: SSH's remote sshd already runs commands via `/bin/sh -c`, so an extra `sh -c` layer breaks argument parsing — `sh -c` only takes the NEXT whitespace-delimited token as the command. Pass commands with pipes/globs as a single string argument instead (e.g., `HostExecute(host, "pgrep -f 'dremio.*java' | head -1")`). This applies to `HostExecute`, `exec.Command("ssh", ...)` in `runPathDiscovery`, and any other SSH remote execution.

**Streaming transport**: Files stream directly from remote nodes through a pipe. TCP backpressure propagates to the remote process.

### Architecture Reference

Detailed architecture docs synthesized from development history live in `docs/architecture/`:
- `decisions.md` -- 48 key architectural decisions grouped by domain
- `patterns-and-gotchas.md` -- recurring patterns and pitfalls
- `design-history.md` -- narrative arc of DDC v4 development (M001-M018)
- `capability-contract.md` -- active requirements defining expected behavior

### Integration Tests

Located in `integrationtest/` (separate from unit tests). Require actual cluster access:
- `integrationtest/kube/` -- Kubernetes collection tests
- `integrationtest/ssh/` -- SSH collection tests

### Linting

Uses golangci-lint v2 (`.golangci.yml`). G204 (command execution) is excluded from gosec since DDC executes system commands by design.
