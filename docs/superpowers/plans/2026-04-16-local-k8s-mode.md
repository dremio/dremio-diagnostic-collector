# local-k8s Collection Mode Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `local-k8s` collect mode that runs from the coordinator pod, combining local filesystem collection with K8s API cluster-level collection (resources + container logs), gracefully degrading when the K8s API is unavailable.

**Architecture:** Reuse `LocalCollector` for filesystem collection. Add a new `RemoteCollect()` branch that creates an in-cluster K8s clientset and sets up a `clusterCollect` closure calling the existing `ClusterK8sExecute`, `GetPreviousLogsForRestartedPods`, and `GetClusterLogs` functions. No new Collector type. Three files modified: `cmd/root.go`, `cmd/configui/configui.go`, `cmd/cli_generator.go`.

**Tech Stack:** Go, Cobra CLI, `k8s.io/client-go` (rest.InClusterConfig), charmbracelet/huh TUI

**Spec:** `docs/superpowers/specs/2026-04-16-local-k8s-mode-design.md`

---

### Task 1: Register LocalK8s Cobra Commands

**Files:**
- Modify: `cmd/root.go:224-250` (command definitions)
- Modify: `cmd/root.go:1244-1253` (command tree wiring)
- Test: `cmd/root_test.go`

- [ ] **Step 1: Write failing tests for the new command tree**

Add to `cmd/root_test.go`:

```go
func TestBareLocalK8sShowsHelp(t *testing.T) {
	err := Execute([]string{"ddc", "collect", "local-k8s"})
	if err != nil {
		t.Errorf("bare 'ddc collect local-k8s' should show help without error, got: %v", err)
	}
}

func TestLocalK8sStandardSetsMode(t *testing.T) {
	oldMode := collectionMode
	defer func() { collectionMode = oldMode }()
	collectionMode = ""

	// local-k8s standard is zero-config — should proceed to collection (which will
	// fail in test since no Dremio process is running, but mode must be set).
	_ = Execute([]string{"ddc", "collect", "local-k8s", "standard"})
	if collectionMode != collects.StandardCollection {
		t.Errorf("expected collectionMode=%q after 'collect local-k8s standard', got %q", collects.StandardCollection, collectionMode)
	}
}

func TestLocalK8sDiagnosisSetsMode(t *testing.T) {
	oldMode := collectionMode
	defer func() { collectionMode = oldMode }()
	collectionMode = ""

	_ = Execute([]string{"ddc", "collect", "local-k8s", "diagnosis"})
	if collectionMode != collects.DiagnosisCollection {
		t.Errorf("expected collectionMode=%q after 'collect local-k8s diagnosis', got %q", collects.DiagnosisCollection, collectionMode)
	}
}

func TestCollectSubcommandHelp_IncludesLocalK8s(t *testing.T) {
	usage := CollectCmd.UsageString()
	if !strings.Contains(usage, "local-k8s") {
		t.Errorf("collect usage should list 'local-k8s' subcommand, got:\n%s", usage)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -short -run "TestBareLocalK8sShowsHelp|TestLocalK8sStandardSetsMode|TestLocalK8sDiagnosisSetsMode|TestCollectSubcommandHelp_IncludesLocalK8s" ./cmd/...`
Expected: FAIL — `local-k8s` command does not exist yet.

- [ ] **Step 3: Add LocalK8s command definitions**

In `cmd/root.go`, after the `LocalDiagnosisCmd` definition (after line 250), add:

```go
// LocalK8sCmd is the local-k8s transport subcommand under collect.
// It runs from inside a Kubernetes coordinator pod, collecting local Dremio logs
// plus K8s cluster resources and container logs via the in-cluster API.
var LocalK8sCmd = &cobra.Command{
	Use:   "local-k8s",
	Short: "Collect from coordinator pod with K8s cluster info",
}

// LocalK8sStandardCmd runs a standard collection in local-k8s mode.
var LocalK8sStandardCmd = &cobra.Command{
	Use:   "standard",
	Short: "Usage data collection",
	PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
		collectionMode = collects.StandardCollection
		return nil
	},
	Run: func(_ *cobra.Command, _ []string) {},
}

// LocalK8sDiagnosisCmd runs a diagnosis collection in local-k8s mode.
var LocalK8sDiagnosisCmd = &cobra.Command{
	Use:   "diagnosis",
	Short: "Full diagnostics [Support only]",
	PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
		collectionMode = collects.DiagnosisCollection
		return nil
	},
	Run: func(_ *cobra.Command, _ []string) {},
}
```

- [ ] **Step 4: Register flags on LocalK8sCmd**

In the `init()` function in `cmd/root.go`, after the local transport flags block (after line 1167), add:

```go
// ── Local-K8s transport flags — on LocalK8sCmd.PersistentFlags() ──
LocalK8sCmd.PersistentFlags().StringVar(&dremioHome, "dremio-home", "/opt/dremio", "Dremio installation directory")
LocalK8sCmd.PersistentFlags().StringVar(&localLogDir, "local-log-dir", "", "Log directory on this node (autodetected if not specified)")
```

- [ ] **Step 5: Register mode-specific flags on LocalK8s leaf commands**

In `cmd/root.go`, add `LocalK8sStandardCmd` and `LocalK8sDiagnosisCmd` to the existing flag registration loops. Update each loop that registers flags on standard/diagnosis commands:

Line 1191 — collection toggles (standard): add `LocalK8sStandardCmd` to the slice:
```go
for _, cmd := range []*cobra.Command{SSHStandardCmd, K8sStandardCmd, LocalStandardCmd, LocalK8sStandardCmd} {
```

Line 1201 — collection toggles (diagnosis): add `LocalK8sDiagnosisCmd`:
```go
for _, cmd := range []*cobra.Command{SSHDiagnosisCmd, K8sDiagnosisCmd, LocalDiagnosisCmd, LocalK8sDiagnosisCmd} {
```

Line 1213 — `--collect-hs-err-files` (diagnosis only): add `LocalK8sDiagnosisCmd`:
```go
for _, cmd := range []*cobra.Command{SSHDiagnosisCmd, K8sDiagnosisCmd, LocalDiagnosisCmd, LocalK8sDiagnosisCmd} {
```

Line 1218 — per-log day counts (standard only): add `LocalK8sStandardCmd`:
```go
for _, cmd := range []*cobra.Command{SSHStandardCmd, K8sStandardCmd, LocalStandardCmd, LocalK8sStandardCmd} {
```

Line 1226 — diagnosis-only flags: add `LocalK8sDiagnosisCmd`:
```go
for _, cmd := range []*cobra.Command{SSHDiagnosisCmd, K8sDiagnosisCmd, LocalDiagnosisCmd, LocalK8sDiagnosisCmd} {
```

- [ ] **Step 6: Wire up the command tree**

In `cmd/root.go`, after `LocalCmd.AddCommand(LocalDiagnosisCmd)` (line 1250), add:

```go
LocalK8sCmd.AddCommand(LocalK8sStandardCmd)
LocalK8sCmd.AddCommand(LocalK8sDiagnosisCmd)
CollectCmd.AddCommand(LocalK8sCmd)
```

- [ ] **Step 7: Update command detection logic**

In `cmd/root.go`, update all places that check for transport commands by name:

Line 573 — `isLeafCmd` detection: add `"local-k8s"`:
```go
isLeafCmd := err == nil && (foundCmd.Use == "standard" || foundCmd.Use == "diagnosis") &&
    foundCmd.Parent() != nil && (foundCmd.Parent().Use == "ssh" || foundCmd.Parent().Use == "k8s" || foundCmd.Parent().Use == "local" || foundCmd.Parent().Use == "local-k8s")
```

Line 612 — bare transport help: add `"local-k8s"`:
```go
if err == nil && (foundCmd.Use == "ssh" || foundCmd.Use == "k8s" || foundCmd.Use == "local" || foundCmd.Use == "local-k8s") && foundCmd.Parent() != nil && foundCmd.Parent().Use == "collect" {
```

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test -short -run "TestBareLocalK8sShowsHelp|TestLocalK8sStandardSetsMode|TestLocalK8sDiagnosisSetsMode|TestCollectSubcommandHelp_IncludesLocalK8s" ./cmd/...`
Expected: All PASS.

- [ ] **Step 9: Run full test suite to check for regressions**

Run: `go test -short ./cmd/...`
Expected: All existing tests still pass.

- [ ] **Step 10: Commit**

```bash
git add cmd/root.go cmd/root_test.go
git commit -m "feat: register local-k8s Cobra commands with standard and diagnosis modes"
```

---

### Task 2: Add RemoteCollect Branch for local-k8s

**Files:**
- Modify: `cmd/root.go:334-467` (RemoteCollect function)
- Modify: `cmd/root.go:1088-1103` (transport routing in Execute)
- Test: `cmd/root_test.go`

- [ ] **Step 1: Write failing test for local-k8s namespace auto-detection helper**

Add to `cmd/root_test.go`:

```go
func TestDetectNamespaceFromFile(t *testing.T) {
	// Create a temporary file simulating the K8s service account namespace file
	tmpDir := t.TempDir()
	nsFile := filepath.Join(tmpDir, "namespace")
	if err := os.WriteFile(nsFile, []byte("dremio-prod"), 0600); err != nil {
		t.Fatal(err)
	}
	ns, err := detectK8sNamespace(nsFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ns != "dremio-prod" {
		t.Errorf("expected 'dremio-prod', got %q", ns)
	}
}

func TestDetectNamespaceFromFile_Missing(t *testing.T) {
	_, err := detectK8sNamespace("/nonexistent/path/namespace")
	if err == nil {
		t.Error("expected error for missing namespace file")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -short -run "TestDetectNamespaceFromFile" ./cmd/...`
Expected: FAIL — `detectK8sNamespace` not defined.

- [ ] **Step 3: Implement namespace detection helper**

Add to `cmd/root.go` (near the other helper functions, e.g., after `sshDefault()`):

```go
// detectK8sNamespace reads the namespace from the Kubernetes service account file.
// This file is auto-mounted into pods at the standard path.
func detectK8sNamespace(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("unable to read namespace from %s: %w", path, err)
	}
	ns := strings.TrimSpace(string(data))
	if ns == "" {
		return "", fmt.Errorf("namespace file %s is empty", path)
	}
	return ns, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -short -run "TestDetectNamespaceFromFile" ./cmd/...`
Expected: PASS.

- [ ] **Step 5: Add local-k8s routing in Execute()**

In `cmd/root.go`, update the transport routing section around line 1093. After the existing `if transportCmd == "local"` block, add a new block for `local-k8s`:

```go
// Local-K8s transport: local collector + optional K8s cluster-level collection
if transportCmd == "local-k8s" {
    enableFallback = true
    // --local-log-dir maps to both coordinator and executor log dirs
    if localLogDir != "" {
        coordinatorLogDir = localLogDir
        executorLogDir = localLogDir
        collectionArgs.CoordinatorLogDir = localLogDir
        collectionArgs.ExecutorLogDir = localLogDir
    }
}
```

Also update the `transportCmd` comment on line 73 to include `"local-k8s"`:
```go
transportCmd          string // "ssh", "k8s", "local", or "local-k8s", set from command path or TUI
```

- [ ] **Step 6: Add local-k8s branch in RemoteCollect()**

In `cmd/root.go`, modify `RemoteCollect()`. Change the function signature to accept a `localK8sMode` boolean:

```go
func RemoteCollect(collectionArgs collection.Args, sshArgs ssh.Args, kubeArgs kubernetes.KubeArgs, fallbackEnabled bool, hook shutdown.Hook, cliMode bool, localK8sMode bool) error {
```

Then, inside `RemoteCollect()`, after the `if fallbackEnabled {` block (line 348-359) and before the `} else if kubeArgs.Namespace != "" {` block (line 360), add a new branch. Restructure to:

```go
if localK8sMode {
    simplelog.Info("using local-k8s collection (local filesystem + K8s cluster info)")
    confPath := filepath.Join(dremioConfDir, "dremio.conf")
    collectorStrategy = local.NewLocalCollector(hook, confPath, dremioHome)

    // Auto-detect namespace and set up K8s cluster-level collection
    const nsPath = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
    detectedNS, nsErr := detectK8sNamespace(nsPath)
    if nsErr != nil {
        simplelog.Warningf("local-k8s: K8s API unavailable (namespace detection failed: %v) — skipping cluster resource and container log collection", nsErr)
    } else {
        restCfg, cfgErr := rest.InClusterConfig()
        if cfgErr != nil {
            simplelog.Warningf("local-k8s: K8s API unavailable (in-cluster config failed: %v) — skipping cluster resource and container log collection", cfgErr)
        } else {
            clientSet, csErr := k8sapi.NewForConfig(restCfg)
            if csErr != nil {
                simplelog.Warningf("local-k8s: K8s API unavailable (clientset creation failed: %v) — skipping cluster resource and container log collection", csErr)
            } else {
                simplelog.Infof("local-k8s: K8s API available, namespace=%s — collecting cluster resources and container logs", detectedNS)
                clusterCollect = func() {
                    if err := collection.ClusterK8sExecute(hook, detectedNS, clientSet, cs, collectionArgs.DDCfs); err != nil {
                        simplelog.Errorf("local-k8s: error collecting K8s resources: %v", err)
                    }
                    if err := collection.GetPreviousLogsForRestartedPods(hook, detectedNS, clientSet, cs, collectionArgs.DDCfs, ""); err != nil {
                        simplelog.Errorf("local-k8s: error collecting previous container logs for restarted pods: %v", err)
                    }
                    if collectContainerLogs {
                        if err := collection.GetClusterLogs(hook, detectedNS, clientSet, cs, collectionArgs.DDCfs, ""); err != nil {
                            simplelog.Errorf("local-k8s: error collecting container logs: %v", err)
                        }
                    } else {
                        simplelog.Info("local-k8s: skipping container log collection (disabled)")
                    }
                }
            }
        }
    }
    nsDisplay := detectedNS
    if nsDisplay == "" {
        nsDisplay = "(unavailable)"
    }
    consoleprint.UpdateCollectionArgs(fmt.Sprintf("mode: local-k8s, namespace: %s", nsDisplay))
    consoleprint.UpdateRuntime(
        versions.GetCLIVersion(),
        simplelog.GetLogLoc(),
        0,
        0,
        0,
    )
} else if fallbackEnabled {
```

Add the required imports to `cmd/root.go`:
```go
k8sapi "k8s.io/client-go/kubernetes"
"k8s.io/client-go/rest"
```

- [ ] **Step 7: Update RemoteCollect call site**

In `cmd/root.go`, update the call to `RemoteCollect` around line 1104. Pass the new `localK8sMode` parameter:

```go
localK8sMode := transportCmd == "local-k8s"
if err := RemoteCollect(collectionArgs, sshArgs, kubeArgs, enableFallback, hook, skipPromptUI, localK8sMode); err != nil {
```

- [ ] **Step 8: Set container log default for local-k8s CLI mode**

In the `localK8sMode` branch inside `RemoteCollect()`, the `collectContainerLogs` global needs a default. Add this logic before the `clusterCollect` closure setup (after the clientSet creation succeeds):

```go
// Default container logs: enabled for diagnosis, disabled for standard.
if cliMode {
    collectContainerLogs = (collectionMode == collects.DiagnosisCollection)
}
```

- [ ] **Step 9: Run full test suite**

Run: `go test -short ./cmd/...`
Expected: All tests pass. The existing tests call `RemoteCollect` with the old signature — update any direct calls to add the new `localK8sMode` parameter as `false`.

- [ ] **Step 10: Build binary to confirm compilation**

Run: `go build -o bin/ddc.exe .`
Expected: Successful compilation.

- [ ] **Step 11: Commit**

```bash
git add cmd/root.go cmd/root_test.go
git commit -m "feat: add local-k8s branch in RemoteCollect with namespace auto-detection and graceful K8s degradation"
```

---

### Task 3: Update TUI Transport Selection

**Files:**
- Modify: `cmd/root.go:734-866` (TUI transport selection and config flow)
- Modify: `cmd/configui/configui.go:60,99` (Transport field comments)

- [ ] **Step 1: Add local-k8s to TUI transport picker**

In `cmd/root.go`, update the transport selection form around line 741. Add the `local-k8s` option:

```go
huh.NewOption("Kubernetes", "k8s"),
huh.NewOption("SSH", "ssh"),
huh.NewOption("Local", "local"),
huh.NewOption("Local-K8s", "local-k8s"),
```

- [ ] **Step 2: Update TUI config routing for local-k8s**

In `cmd/root.go`, around line 757-866 where the transport-specific config screens are shown: the existing code has `if transport == "ssh"` and `else if transport == "k8s"`. The `local` transport falls through with no transport-specific config. Add `local-k8s` to share the same behavior as `local` (no transport-specific config screen).

No code change needed here — `local-k8s` already falls through since there's no `if transport == "local-k8s"` branch, just like `local`.

- [ ] **Step 3: Update path discovery routing for local-k8s**

In `cmd/root.go`, around line 873, update the path discovery condition to include `local-k8s`:

```go
if transportCmd == "local" || transportCmd == "local-k8s" {
    detected = runLocalPathDiscovery()
```

- [ ] **Step 4: Update runStandardConfigScreen for local-k8s**

In `cmd/root.go`, around line 1365, update the executor log dir mapping to include `local-k8s`:

```go
if transportCmd == "local" || transportCmd == "local-k8s" {
    executorLogDir = cfg.CoordinatorLogDir
}
```

- [ ] **Step 5: Update runDiagnosisConfigScreen for local-k8s**

In `cmd/root.go`, around line 1451, update the executor log dir mapping:

```go
if transportCmd == "local" || transportCmd == "local-k8s" {
    executorLogDir = cfg.CoordinatorLogDir
}
```

In the same function, the node discovery section (lines 1396-1422) uses `if transportCmd == "k8s"` and `else if transportCmd == "ssh"`. Local-k8s should NOT do node discovery (single coordinator), so it naturally falls through — no change needed.

- [ ] **Step 6: Update configui Transport field comments**

In `cmd/configui/configui.go`, update the `Transport` field comments in both config structs:

Line 60 (`StandardConfig`):
```go
Transport   string // "ssh", "k8s", "local", or "local-k8s"
```

Line 99 (`DiagnosisConfig`):
```go
Transport   string // "ssh", "k8s", "local", or "local-k8s"
```

- [ ] **Step 7: Update runLocalPathDiscovery transport label**

In `cmd/root.go`, around line 1586, the `runLocalPathDiscovery()` function creates a `DetectedPaths` with `Transport: "local"`. This is used for CLI generation. Update to use the actual transport:

```go
Transport:         transportCmd,
```

This way it'll be `"local"` when called from local mode and `"local-k8s"` when called from local-k8s mode.

- [ ] **Step 8: Run test suite**

Run: `go test -short ./cmd/...`
Expected: All tests pass.

- [ ] **Step 9: Build binary**

Run: `go build -o bin/ddc.exe .`
Expected: Successful compilation.

- [ ] **Step 10: Commit**

```bash
git add cmd/root.go cmd/configui/configui.go
git commit -m "feat: add local-k8s to TUI transport selection and path discovery routing"
```

---

### Task 4: Update CLI Generator

**Files:**
- Modify: `cmd/cli_generator.go:33-34` (CLICommandConfig comment)
- Test: `cmd/cli_generator_test.go`

- [ ] **Step 1: Write failing test for local-k8s CLI generation**

Add to `cmd/cli_generator_test.go`:

```go
func TestGenerateCLICommand_LocalK8sStandard(t *testing.T) {
	cmd := GenerateCLICommand(CLICommandConfig{
		Transport: "local-k8s",
		Mode:      "standard",
	})
	if !strings.Contains(cmd, "collect") {
		t.Error("expected command to contain collect subcommand")
	}
	if !strings.Contains(cmd, "local-k8s") {
		t.Error("expected local-k8s transport subcommand")
	}
	if !strings.Contains(cmd, "standard") {
		t.Error("expected standard subcommand verb")
	}
	// Should NOT contain K8s-specific flags
	if strings.Contains(cmd, "--namespace") {
		t.Error("local-k8s mode should not include --namespace")
	}
	if strings.Contains(cmd, "--label-selector") {
		t.Error("local-k8s mode should not include --label-selector")
	}
	if strings.Contains(cmd, "--context") {
		t.Error("local-k8s mode should not include --context")
	}
}

func TestGenerateCLICommand_LocalK8sDiagnosis(t *testing.T) {
	cmd := GenerateCLICommand(CLICommandConfig{
		Transport: "local-k8s",
		Mode:      "diagnosis",
		Days:      3,
	})
	if !strings.Contains(cmd, "local-k8s") {
		t.Error("expected local-k8s transport")
	}
	if !strings.Contains(cmd, "diagnosis") {
		t.Error("expected diagnosis mode")
	}
	if !strings.Contains(cmd, "--days=3") {
		t.Error("expected --days=3")
	}
}
```

- [ ] **Step 2: Run tests to verify they pass (already work with current code)**

The `GenerateCLICommand` function already uses `c.Transport` directly as a subcommand verb, so `"local-k8s"` flows through correctly. The K8s-specific flags are gated by `c.Namespace != ""`, `c.Coordinator != ""`, etc., which will all be empty for local-k8s.

Run: `go test -short -run "TestGenerateCLICommand_LocalK8s" ./cmd/...`
Expected: PASS — the existing `GenerateCLICommand` already handles this correctly.

- [ ] **Step 3: Update CLICommandConfig comment**

In `cmd/cli_generator.go`, update the Transport field comment on line 34:

```go
Transport        string // "ssh", "k8s", "local", or "local-k8s"
```

- [ ] **Step 4: Run full test suite**

Run: `go test -short ./cmd/...`
Expected: All tests pass.

- [ ] **Step 5: Commit**

```bash
git add cmd/cli_generator.go cmd/cli_generator_test.go
git commit -m "feat: add local-k8s CLI generator tests and update Transport comment"
```

---

### Task 5: Final Verification and Build

**Files:**
- None new — verification only

- [ ] **Step 1: Run full test suite**

Run: `go test -short ./...`
Expected: All tests pass across all packages.

- [ ] **Step 2: Run linter**

Run: `go fmt ./...`
Expected: No formatting changes needed.

- [ ] **Step 3: Build final binary**

Run: `go build -o bin/ddc.exe .`
Expected: Successful compilation.

- [ ] **Step 4: Verify CLI help output**

Run: `./bin/ddc.exe collect --help`
Expected: Output includes `local-k8s` subcommand.

Run: `./bin/ddc.exe collect local-k8s --help`
Expected: Shows `standard` and `diagnosis` subcommands.

Run: `./bin/ddc.exe collect local-k8s standard --help`
Expected: Shows `--dremio-home`, `--local-log-dir`, `--collect-server-logs`, and other standard flags. Does NOT show `--namespace`, `--label-selector`, `--context`, `--enable-kubectl`.

Run: `./bin/ddc.exe collect local-k8s diagnosis --help`
Expected: Shows `--dremio-home`, `--local-log-dir`, `--diag-jfr`, `--diag-jstack`, and other diagnosis flags.

- [ ] **Step 5: Commit if any formatting changes were needed**

```bash
git add -A
git commit -m "chore: formatting cleanup for local-k8s mode"
```
