# Patterns and Gotchas

Recurring patterns, rules, and pitfalls discovered during DDC v4 development. Organized by domain. Each entry includes the pattern/rule and why it matters.

## Transport and Streaming

### Rate limiter placement in io.Writer chains

When composing io.Writer chains (rate limiter -> progress tracker -> file/hash writers), place the rate limiter *before* the progress tracker. This ensures progress reports reflect actual throttled throughput, not buffered throughput.

Chain order: `RateLimitedWriter` -> `MultiWriter` -> `progressWriter` wraps the rate-limited chain.

### Advisory verification pattern

Integrity verification (checksums) should be advisory: log a warning on mismatch but don't fail the operation. Failing on checksum mismatch would block the entire collection for a non-critical integrity issue — the user still wants whatever data they can get. This applies to all non-critical verification steps in diagnostic collection.

### K8SWriter line-buffering for chunked streaming

When consuming output from Kubernetes SPDY streaming APIs (or any API that delivers data in arbitrary byte chunks), the `io.Writer` receiving the data must buffer partial lines across `Write()` calls and emit complete lines only on newline boundaries. A final `Flush()` call after the stream completes emits any trailing partial line.

**Gotcha:** Naive `strings.Split(chunk, "\n")` on each `Write()` corrupts data at chunk boundaries — the last field of one chunk gets concatenated with the first field of the next.

### Post-stream masking preserves checksum integrity

When files need post-processing (e.g. secret masking), do it after streaming via read-modify-write on the local file. Never wrap streaming writers with transformations when checksum verification is needed — inline wrapping would make checksums reflect modified content instead of verifying the transfer itself.

### io.Pipe + goroutine for transparent stream decompression

When `StreamFromHost` blocks until complete, you can't wrap the writer with a `gzip.Reader` directly. Use `io.Pipe`: goroutine calls `StreamFromHost(host, path, pipeWriter, true)` then closes pipeWriter; main thread reads from pipeReader through `gzip.NewReader`.

**Critical:** Always `pipew.CloseWithError(err)` in the goroutine to signal EOF or error. Close both sides on error paths to prevent goroutine leaks.

### HostExecute trailing newline contamination

`HostExecute` in kubectl.go and ssh.go returns stdout with trailing newlines from the shell. Always `strings.TrimRight(out, "\n")` before returning. Discovery probes (`command -v gzip`, checksum tool probes) depend on clean output for boolean checks.

### Use path.Join for remote Linux paths

When the DDC orchestrator runs on Windows but constructs paths for remote Linux targets (K8s pods, SSH hosts), `filepath.Join` produces Windows backslashes. Use `path.Join` (POSIX, always forward slashes) for any path destined for a remote Linux filesystem.

## Collector Interface

### Extension requires all implementors

The Collector interface has 4 implementations: K8s API (`kubernetes.go`), kubectl CLI (`kubectl.go`), SSH (`ssh.go`), and local (`local.go`). Adding a method to the interface requires implementing it in all 4 types, even if only 2 are expected to be used at runtime.

### Method removal requires research-first

Before removing any interface method, `rg` for all callers across all 4 implementations AND orchestrator code. Plan deletions only after confirming zero live callers. This was learned when M005 planned to remove `CopyToHost` but research discovered it was actively used by `CollectAsyncProfiler`.

### Remote JVM function signature convention

All remote JVM collection functions follow: `CollectX(c Collector, host string, pid int, ...toolParams, outDir string) error`. Stdout-capture tools (jstack, ttop, JVM flags) use `HostExecute` and write directly. File-producing tools (JFR, heap dump, async-profiler) use the shared `streamRemoteFile` helper.

### Non-fatal JVM collection errors

JVM collection errors are logged but never fail the overall diagnostic collection. One node's JVM issue (wrong PID, missing jcmd, permission denied) must not block collecting data from other nodes or other diagnostic categories.

## CLI and Cobra

### Flag renaming via conf key constant value change

When renaming CLI flags, change the string value of the Go constant (e.g. `KeyStartDate = "date-start"`) rather than renaming the Go identifier itself. All code referencing the constant compiles unchanged — only the user-facing flag name changes.

### Flag default changes cascade through test fixtures

When changing defaults, proactively search for all test files that build config objects via `ReadConf` and add explicit overrides before running tests.

### Cobra validateUnknownFlags must use LocalFlags()

When building a combined flag set from a subcommand for validation, use `subCmd.LocalFlags()` not `subCmd.Flags()`. `Flags()` includes inherited persistent flags and triggers lazy initialization that adds `--help`. When multiple tests call `Execute()` sequentially, the second call panics with "flag redefined: help".

### Prefer structural enforcement over runtime validation

When Cobra subcommands naturally enforce flag scoping (diagnosis-only flags on `DiagnosisCmd.Flags()` are invisible to standard), delete the corresponding runtime validation code. Structural enforcement can't be bypassed and produces better error messages.

## TUI (huh forms)

### DescriptionFunc for cross-field reactivity

`huh.NewInput().DescriptionFunc(fn, &cfg)` accepts exactly one binding argument. For cross-field reactivity (e.g. Days <-> Date range), pass the config struct pointer as the binding arg and capture sibling field pointers via closure. huh re-evaluates the closure on each render cycle.

### WithHideFunc for conditional groups

huh supports `WithHideFunc` only at Group level, not per-field. Use one `huh.NewGroup` per conditional section with `WithHideFunc` checking a multi-select slice, not per-field visibility toggles.

### huh.NewNote() as non-interactive sub-headers

`huh.NewNote()` elements default to `skip: true` — they render as visible text but the cursor skips them during Tab navigation. Ideal as section dividers when consolidating multiple groups into one.

### Post-form intersection for multi-select mapping

When a multi-select presents discovered items and the user deselects some, extract the mapping into a named function for direct unit testing. Never inline complex mapping logic in the form completion block.

## Discovery and Kubernetes

### Positive pod matching over negative exclusion

Pod selection filters should use positive matching (e.g. `strings.HasPrefix(name, "dremio-executor")`) rather than negative exclusion (e.g. `!strings.Contains(name, "master")`). Negative filters break when unexpected pod names appear (sidecars, monitoring agents).

### K8s ConfigMap mounts are symlinks

Kubernetes ConfigMap and Secret volume mounts use a symlink chain: `filename -> ..data/filename -> ..hash/filename`. Use `find -L -type f` to follow symlinks. Filter out `..data` and `..2024_*` directories from results.

### client-go kubeconfig error wrapping on Windows

`clientcmd.Load()` wraps file-not-found errors so `os.IsNotExist(err)` returns false on Windows. Use both `os.IsNotExist` and string matching for "not found" or "no such file" for cross-platform correctness.

### HOCON HasPath returns false for keys set to false

The HOCON parser's `HasPath()` returns false when a key exists but its value is false. Use `GetString()` instead — it returns empty string for missing keys and the string representation for present keys.

### Three-way transport branching

When the number of transport variants increases (SSH, K8s, Local), audit all branching for implicit else clauses that assume only two alternatives. Convert `if ssh { ... } else { /* k8s */ }` to explicit three-way branching.

## Testing Discipline

### Pre-existing Windows test failures

Several tests fail on Windows due to path separators, file locking, and environment differences. These are NOT caused by new changes.

**Rule:** When verifying changes, compare failures against unchanged code before attributing them to new work. Known Windows failures include path separator issues, file locking during TempDir cleanup, and environment variable format differences (`%PATH%` vs `$PATH`).

### Preservation boundary pattern for large deletions

When deleting a major feature that shares naming or directory space with preserved code, explicitly enumerate what must NOT be deleted. Create a CRITICAL PRESERVATION BOUNDARY list and verify all listed files still exist after deletion.

### Stale test assertions after rendering rewrites

After any display/rendering rewrite, `rg` for all test assertions that reference output literals and verify they still match the new output format. Tests that assert on specific output strings may silently break if the test runner doesn't surface them.

## File Collection Patterns

### Config allowlist with filepath.Match globs

`filterFiles()` uses a static allowlist of glob patterns (e.g. `dremio.conf`, `logback*.xml`, `*.json`). Prefer allowlist + glob patterns over blocklist for security-sensitive file filtering.

### FileType-based gating in the streaming loop

To conditionally skip file types based on collection mode, check `FileType + bool flag -> append to skipped list` at the top of the streaming file loop. Gate at streaming time, not discovery time — keeps discovery complete and filtering explicit.
