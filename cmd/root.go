//	Copyright 2023 Dremio Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// cmd package contains all the command line flag and initialization logic for commands
package cmd

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/huh/spinner"

	"github.com/charmbracelet/huh"
	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/configui"
	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/collection"
	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/helpers"
	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/kubectl"
	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/kubernetes"
	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/local"
	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/ssh"
	version "github.com/dremio/dremio-diagnostic-collector/v4/cmd/version"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/collects"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/consoleprint"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/dirs"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/shutdown"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/simplelog"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/validation"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/versions"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	k8sapi "k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth" // in case one needs auth plugins
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	coordinatorStr              string
	executorsStr                string
	detectLabelSelector         string
	containerLogLabelSelector   string
	sshKeyLoc                   string
	sshUser                     string
)

var outputLoc string

var (
	sudoUser              string
	namespace             string
	k8sContext            string
	kubeconfigPath        string
	disableFreeSpaceCheck bool
	enableKubeCtl         bool
	collectionMode        collects.CollectionMode
	transportCmd          string // "ssh", "k8s", "local", or "local-k8s", set from command path or TUI
	cliAuthToken          string
	pid                   string
	collectionThreads     int
	// v4 CLI flags
	skipVersionCheck  bool
	collectorTimeout  string
	startDate         string
	daysFlag          int
	collectHeapDump   bool
	allowInsecureSSL  bool
	diagTimeSeconds   int
	progressFormat    string
	coordinatorLogDir string
	executorLogDir    string
	dremioConfDir     string
	dremioRocksDBDir  string
	localLogDir       string // set from --local-log-dir flag on LocalCmd; maps to both coordinator/executor log dirs
	dremioHome        string // set from --dremio-home flag on LocalCmd; default /opt/dremio
	// dremioGCLogsDir removed — autodetected via conf.go; --dremio-gclogs-dir flag deleted
	dremioEndpoint       string
	systemTables         string
	nodesFlag            string
	excludeNodesFlag     string
	collectContainerLogs bool
	sshStrictHostKeys    bool

	// per-log day counts (standard mode)
	serverLogsNumDays  int
	trackerJSONNumDays int
	vacuumLogNumDays   int
	queriesJSONNumDays int
	queriesPerfNumDays int
	collectQueriesPerf bool

	// log collection toggles
	collectServerLogs     bool
	collectGCLogs         bool
	collectTrackerJSON    bool
	collectVacuumLog      bool
	collectQueriesJSON    bool
	collectAcceleration   bool
	collectAccessLog      bool
	collectHiveDeprecated bool
	collectMetaRefresh    bool
	// collectAuditLog removed — flag and feature deleted
	collectHSErrFiles bool

	// JVM diagnostic toggles
	collectJFR                 bool
	collectJStack              bool
	collectTop                 bool
	collectAsyncProfiler       bool
	collectWLM                 bool
	collectKVStoreReport       bool
	collectProblematicProfiles bool
)

// RootCmd represents the base command when called without any subcommands
var RootCmd = &cobra.Command{
	Use:   "ddc",
	Short: versions.GetCLIVersion() + " ddc connects via to dremio servers collects logs into an archive",
	Long: versions.GetCLIVersion() + ` ddc connects via ssh or kubectl and collects a series of logs and files for dremio, then puts those collected files in an archive
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
`,
	Run: func(_ *cobra.Command, _ []string) {
	},
}

// CollectCmd represents the non-interactive collect subcommand.
// It has no Run — bare "ddc collect" shows subcommand help.
var CollectCmd = &cobra.Command{
	Use:   "collect",
	Short: "Run non-interactive collection with provided flags",
	Long: `Run a non-interactive collection using the provided CLI flags.
Requires a transport subcommand (ssh or k8s) and a mode subcommand (standard or diagnosis).

examples:

	ddc collect k8s standard --namespace mynamespace
	ddc collect ssh diagnosis --coordinator 10.0.0.1 --ssh-user myuser --ssh-key ~/.ssh/mykey
`,
}

// SSHCmd is the SSH transport subcommand under collect.
var SSHCmd = &cobra.Command{
	Use:   "ssh",
	Short: "Collect via SSH from bare metal / VM nodes",
}

// K8sCmd is the Kubernetes transport subcommand under collect.
var K8sCmd = &cobra.Command{
	Use:   "k8s",
	Short: "Collect via Kubernetes API",
}

// SSHStandardCmd runs a standard collection via SSH.
var SSHStandardCmd = &cobra.Command{
	Use:   "standard",
	Short: "Usage data collection",
	PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
		collectionMode = collects.StandardCollection
		return nil
	},
	Run: func(_ *cobra.Command, _ []string) {},
}

// SSHDiagnosisCmd runs a diagnosis collection via SSH.
var SSHDiagnosisCmd = &cobra.Command{
	Use:   "diagnosis",
	Short: "Full diagnostics [Support only]",
	PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
		collectionMode = collects.DiagnosisCollection
		return nil
	},
	Run: func(_ *cobra.Command, _ []string) {},
}

// K8sStandardCmd runs a standard collection via Kubernetes.
var K8sStandardCmd = &cobra.Command{
	Use:   "standard",
	Short: "Usage data collection",
	PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
		collectionMode = collects.StandardCollection
		return nil
	},
	Run: func(_ *cobra.Command, _ []string) {},
}

// K8sDiagnosisCmd runs a diagnosis collection via Kubernetes.
var K8sDiagnosisCmd = &cobra.Command{
	Use:   "diagnosis",
	Short: "Full diagnostics [Support only]",
	PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
		collectionMode = collects.DiagnosisCollection
		return nil
	},
	Run: func(_ *cobra.Command, _ []string) {},
}

// LocalCmd is the local transport subcommand under collect.
var LocalCmd = &cobra.Command{
	Use:   "local",
	Short: "Collect from the local node (no SSH or Kubernetes)",
}

// LocalStandardCmd runs a standard collection locally.
var LocalStandardCmd = &cobra.Command{
	Use:   "standard",
	Short: "Usage data collection",
	PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
		collectionMode = collects.StandardCollection
		return nil
	},
	Run: func(_ *cobra.Command, _ []string) {},
}

// LocalDiagnosisCmd runs a diagnosis collection locally.
var LocalDiagnosisCmd = &cobra.Command{
	Use:   "diagnosis",
	Short: "Full diagnostics [Support only]",
	PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
		collectionMode = collects.DiagnosisCollection
		return nil
	},
	Run: func(_ *cobra.Command, _ []string) {},
}

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

// buildMergedFlagSet returns a flag set that includes all flags relevant to subCmd:
// root persistent flags, collect persistent flags, transport-level persistent flags
// (SSHCmd or K8sCmd), the subcommand's own local flags, and root local flags.
// The returned set shares value pointers with the original flags, so parsing it
// sets the same global variables.
func buildMergedFlagSet(subCmd *cobra.Command) *pflag.FlagSet {
	merged := pflag.NewFlagSet("merged", pflag.ContinueOnError)
	addOnce := func(flag *pflag.Flag) {
		if merged.Lookup(flag.Name) == nil {
			merged.AddFlag(flag)
		}
	}
	// Include root persistent flags (--verbose, --skip-version-check)
	RootCmd.PersistentFlags().VisitAll(addOnce)
	// Include collect persistent flags (shared across all collection subcommands)
	CollectCmd.PersistentFlags().VisitAll(addOnce)
	// Include subcommand local flags (mode-specific flags).
	if subCmd != nil {
		subCmd.LocalFlags().VisitAll(addOnce)
	}
	// Include transport-level persistent flags (SSHCmd or K8sCmd)
	if subCmd != nil && subCmd.Parent() != nil && subCmd.Parent() != CollectCmd && subCmd.Parent() != RootCmd {
		subCmd.Parent().PersistentFlags().VisitAll(addOnce)
	}
	// Include root local flags (interactive mode)
	RootCmd.LocalFlags().VisitAll(addOnce)
	return merged
}

// validateUnknownFlags checks if there are any unknown flags in the arguments
func validateUnknownFlags(args []string, subCmd *cobra.Command) error {
	tempFlagSet := buildMergedFlagSet(subCmd)

	// Parse the arguments and check for errors
	err := tempFlagSet.Parse(args)
	if err != nil {
		return fmt.Errorf("invalid flag: %w", err)
	}

	// Check for any unknown flags
	unknown := tempFlagSet.Args()
	for _, arg := range unknown {
		if strings.HasPrefix(arg, "-") {
			return fmt.Errorf("unknown flag: %s", arg)
		}
	}

	return nil
}

// startTicker starts a ticker that ticks every specified duration and returns
// a function that can be called to stop the ticker.
func startTicker() (stop func()) {
	if !consoleprint.IsStatusOutput() {
		consoleprint.EnterStatusScreen()
	}
	ticker := time.NewTicker(200 * time.Millisecond)
	quit := make(chan struct{})
	done := make(chan struct{})
	consoleprint.PrintState()
	go func() {
		defer close(done)
		for {
			select {
			case <-ticker.C:
				consoleprint.PrintState()
			case <-quit:
				ticker.Stop()
				return
			}
		}
	}()

	var once sync.Once
	return func() {
		once.Do(func() {
			close(quit)
			<-done
			if !consoleprint.IsStatusOutput() {
				consoleprint.ExitStatusScreen()
			}
		})
	}
}

func RemoteCollect(collectionArgs collection.Args, sshArgs ssh.Args, kubeArgs kubernetes.KubeArgs, fallbackEnabled bool, hook shutdown.Hook, cliMode bool, localK8sMode bool) error {
	consoleprint.UpdateCollectionMode(collectionArgs.CollectionMode)

	outputDir, err := filepath.Abs(filepath.Dir(outputLoc))
	// This is where the SSH or K8s collection is determined. We create an instance of the interface based on this
	// which then determines whether the commands are routed to the SSH or K8s commands
	if err != nil {
		return fmt.Errorf("error when getting directory for copy strategy: %w", err)
	}

	cs := helpers.NewHCCopyStrategy(collectionArgs.DDCfs, &helpers.RealTimeService{}, outputDir)
	hook.AddFinalSteps(cs.Close, "running cleanup on copy strategy")
	clusterCollect := func() {}
	var collectorStrategy collection.Collector
	if localK8sMode {
		simplelog.Info("using local-k8s collection (local filesystem + K8s cluster info)")
		confPath := filepath.Join(dremioConfDir, "dremio.conf")
		collectorStrategy = local.NewLocalCollector(hook, confPath, dremioHome)

		// Default container logs: enabled for diagnosis, disabled for standard.
		if cliMode {
			collectContainerLogs = (collectionMode == collects.DiagnosisCollection)
		}

		// Auto-detect namespace; fall back to kubeconfig current-context if
		// no service-account file is present.
		const nsPath = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
		detectedNS, nsErr := detectK8sNamespace(nsPath)

		// Try in-cluster config first; if that fails, fall back to a
		// user-supplied kubeconfig (flag → env → home).
		var clientSet *k8sapi.Clientset
		restCfg, cfgErr := rest.InClusterConfig()
		if cfgErr == nil {
			kclientSet, csErr := k8sapi.NewForConfig(restCfg)
			if csErr != nil {
				simplelog.Warningf("local-k8s: K8s API unavailable (clientset creation failed: %v) — skipping cluster resource and container log collection", csErr)
			} else {
				clientSet = kclientSet
			}
		} else {
			kclientSet, _, kcErr := kubernetes.GetClientset("", kubeconfigPath)
			if kcErr != nil {
				simplelog.Warningf("local-k8s: K8s API unavailable (in-cluster config failed: %v; kubeconfig fallback also failed: %v) — skipping cluster resource and container log collection", cfgErr, kcErr)
			} else {
				clientSet = kclientSet
				simplelog.Info("local-k8s: using kubeconfig fallback for K8s API access")
				// If the namespace file wasn't readable, use the kubeconfig's current-context namespace.
				if nsErr != nil {
					if nsFromKubeconfig, kcNsErr := readNamespaceFromKubeconfig(kubeconfigPath); kcNsErr == nil && nsFromKubeconfig != "" {
						detectedNS = nsFromKubeconfig
						nsErr = nil
					}
				}
			}
		}
		if nsErr != nil {
			simplelog.Warningf("local-k8s: namespace detection failed (%v) and no fallback available — skipping cluster resource and container log collection", nsErr)
		} else if clientSet != nil {
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
		simplelog.Info("using local collection")
		confPath := filepath.Join(dremioConfDir, "dremio.conf")
		// dremioHome is the package-level global set from --dremio-home flag (default: /opt/dremio)
		collectorStrategy = local.NewLocalCollector(hook, confPath, dremioHome)
		consoleprint.UpdateRuntime(
			versions.GetCLIVersion(),
			simplelog.GetLogLoc(),
			0,
			0,
			0,
		)
	} else if kubeArgs.Namespace != "" {
		cs.IsK8s = true
		simplelog.Info("using Kubernetes api based collection")
		consoleprint.UpdateCollectionArgs(fmt.Sprintf("namespace: '%v', detect-label-selector: '%v'", kubeArgs.Namespace, kubeArgs.DetectLabelSelector))
		collectorStrategy, err = kubernetes.NewK8sAPI(kubeArgs, hook)
		if err != nil {
			return err
		}
		// K8s RBAC pre-check
		_ = spinner.New().
			Title("Checking Kubernetes permissions...").
			Action(func() {
				if err := kubernetes.CheckRBAC(kubeArgs.K8SContext, kubeArgs.Namespace, kubeArgs.KubeconfigPath); err != nil {
					simplelog.Warningf("RBAC check: %v", err)
				}
			}).
			Run()
		if enableKubeCtl {
			potentialStrategy, err := kubectl.NewKubectlK8sActions(hook, kubeArgs)
			if err != nil {
				simplelog.Warningf("kubectl not available, using embedded k8s api: %v", err)
			} else {
				collectorStrategy = potentialStrategy
			}
		}

		consoleprint.UpdateRuntime(
			versions.GetCLIVersion(),
			simplelog.GetLogLoc(),
			0,
			0,
			0,
		)

		// Default container logs: enabled for diagnosis, disabled for standard.
		// Only apply in CLI mode — in TUI mode the config screen already sets collectContainerLogs.
		if cliMode && !K8sCmd.PersistentFlags().Changed("collect-container-logs") {
			collectContainerLogs = (collectionMode == collects.DiagnosisCollection)
		}

		clusterCollect = func() {
			clientSet, _, err := kubernetes.GetClientset(k8sContext, kubeconfigPath)
			if err != nil {
				simplelog.Errorf("when getting Kubernetes info, the following error was returned: %v", err)
				return
			}
			err = collection.ClusterK8sExecute(hook, kubeArgs.Namespace, clientSet, cs, collectionArgs.DDCfs)
			if err != nil {
				simplelog.Errorf("when getting Kubernetes info, the following error was returned: %v", err)
			}
			// Always collect previous logs for pods that have restarted
			err = collection.GetPreviousLogsForRestartedPods(hook, kubeArgs.Namespace, clientSet, cs, collectionArgs.DDCfs, containerLogLabelSelector)
			if err != nil {
				simplelog.Errorf("when getting previous container logs for restarted pods, the following error was returned: %v", err)
			}
			if collectContainerLogs {
				err = collection.GetClusterLogs(hook, kubeArgs.Namespace, clientSet, cs, collectionArgs.DDCfs, containerLogLabelSelector)
				if err != nil {
					simplelog.Errorf("when getting container logs, the following error was returned: %v", err)
				}
			} else {
				simplelog.Info("skipping container log collection (disabled)")
			}
		}
	} else {
		err := validateSSHParameters(sshArgs)
		if err != nil {
			fmt.Println("COMMAND HELP TEXT:")
			fmt.Println("")
			helpErr := RootCmd.Help()
			if helpErr != nil {
				return fmt.Errorf("unable to print help: %w", helpErr)
			}
			return fmt.Errorf("invalid command flag detected: %w", err)
		}
		simplelog.Info("using SSH based collection")

		// Pre-check SSH connectivity to all nodes
		allHosts := []string{}
		for _, h := range strings.Split(sshArgs.CoordinatorStr, ",") {
			if h = strings.TrimSpace(h); h != "" {
				allHosts = append(allHosts, h)
			}
		}
		for _, h := range strings.Split(sshArgs.ExecutorStr, ",") {
			if h = strings.TrimSpace(h); h != "" {
				allHosts = append(allHosts, h)
			}
		}
		var unreachable []string
		_ = spinner.New().
			Title(fmt.Sprintf("Checking SSH connectivity to %d node(s)...", len(allHosts))).
			Action(func() {
				for _, host := range allHosts {
					if err := ssh.CheckSSHConnectivity(host, sshArgs.SSHUser, sshArgs.SSHKeyLoc, 5*time.Second); err != nil {
						simplelog.Warningf("SSH pre-check failed for %s: %v", host, err)
						unreachable = append(unreachable, host)
					}
				}
			}).
			Run()
		if len(unreachable) > 0 {
			return fmt.Errorf("SSH connectivity check failed for %d node(s): %s. Fix connectivity before running DDC", len(unreachable), strings.Join(unreachable, ", "))
		}

		consoleprint.UpdateCollectionArgs(fmt.Sprintf("login: %v, user: %v, coordinator: %v, executor: %v, key: %v", sshArgs.SSHUser, sshArgs.SudoUser, sshArgs.CoordinatorStr, sshArgs.ExecutorStr, sshArgs.SSHKeyLoc))
		collectorStrategy = ssh.NewCmdSSHActions(sshArgs, hook)
	}

	// Launch the collection
	err = collection.Execute(collectorStrategy,
		cs,
		collectionArgs,
		hook,
		clusterCollect,
	)
	if err != nil {
		hook.SetError(err)
		return err
	}
	return nil
}

// diagLogDays returns the unified day limit for diagnosis mode log collection.
// Returns 0 in non-diagnosis modes so the default value Cobra writes into
// daysFlag at init() time (a side effect of registering --days only on
// diagnosis subcommands) does not leak into standard mode and override
// per-log day flags like --queries-perf-num-days.
func diagLogDays() int {
	if collectionMode != collects.DiagnosisCollection {
		return 0
	}
	return daysFlag
}

// resolveMetaRefresh returns the effective collect-meta-refresh-log value.
// The flag is registered on both the standard and diagnosis command loops via a
// single shared global; because the diagnosis loop registers last, the global's
// init-time default is the diagnosis value (true) and pflag does not reset it for
// an unset standard run. In CLI mode (skipPromptUI) the mode-resolved confData
// (mode default + explicit-flag override) is authoritative; in TUI mode the form
// sets the global explicitly, so trust it.
func resolveMetaRefresh(skipPromptUI bool, global bool, confData map[string]interface{}) bool {
	if skipPromptUI {
		if v, ok := confData[conf.KeyCollectMetaRefreshLog].(bool); ok {
			return v
		}
	}
	return global
}

// BuildConfData constructs the configuration map from CLI flags and mode defaults.
// All settings come from flags or mode profile defaults.
func BuildConfData(cmd *cobra.Command, collectionMode collects.CollectionMode) map[string]interface{} {
	confData := make(map[string]interface{})
	// Apply mode defaults first
	conf.SetViperDefaults(confData, "", 0, collectionMode)
	// Override with any CLI flags that were explicitly set
	if coordinatorLogDir != "" {
		confData[conf.KeyDremioLogDir] = coordinatorLogDir
	}
	if dremioConfDir != "" {
		confData[conf.KeyDremioConfDir] = dremioConfDir
	}
	if dremioRocksDBDir != "" {
		confData[conf.KeyDremioRocksdbDir] = dremioRocksDBDir
	}
	if dremioEndpoint != "" {
		confData[conf.KeyDremioEndpoint] = dremioEndpoint
	}
	if startDate != "" {
		confData[conf.KeyStartDate] = startDate
	}

	confData[conf.KeyAllowInsecureSSL] = allowInsecureSSL
	confData[conf.KeyDremioPatToken] = "" // will be set separately

	// Per-log day counts — standard mode only. Diagnosis mode uses
	// --days/--start-date for all log date filtering.
	if collectionMode == collects.StandardCollection {
		confData[conf.KeyServerLogsNumDays] = serverLogsNumDays
		confData[conf.KeyTrackerJSONNumDays] = trackerJSONNumDays
		confData[conf.KeyVacuumLogNumDays] = vacuumLogNumDays
		confData[conf.KeyQueriesJSONNumDays] = queriesJSONNumDays
		confData[conf.KeyQueriesPerfNumDays] = queriesPerfNumDays
	}
	// Only override mode defaults when the user explicitly passed these flags.
	if cmd != nil {
		if cmd.Flags().Changed(conf.KeyCollectQueriesPerfJSON) {
			confData[conf.KeyCollectQueriesPerfJSON] = collectQueriesPerf
		}
		if cmd.Flags().Changed(conf.KeyCollectTrackerJSON) {
			confData[conf.KeyCollectTrackerJSON] = collectTrackerJSON
		}
		if cmd.Flags().Changed(conf.KeyCollectVacuumLog) {
			confData[conf.KeyCollectVacuumLog] = collectVacuumLog
		}
		if cmd.Flags().Changed(conf.KeyCollectMetaRefreshLog) {
			confData[conf.KeyCollectMetaRefreshLog] = collectMetaRefresh
		}
	}
	// Log the configuration
	simplelog.Infof("v4 configuration for mode %v:", collectionMode)
	for k, v := range confData {
		if k == conf.KeyDremioPatToken && v != "" {
			simplelog.Debugf("conf key '%v':'REDACTED'", k)
		} else {
			simplelog.Debugf("conf key '%v':'%v'", k, v)
		}
	}
	return confData
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute(args []string) error {
	// Defer closing the logger
	defer func() {
		if err := simplelog.Close(); err != nil {
			log.Printf("unable to close log: %v", err)
		}
	}()

	foundCmd, _, err := RootCmd.Find(args[1:])
	// Handle subcommand detection — leaf commands are standard/diagnosis under ssh/k8s
	isLeafCmd := err == nil && (foundCmd.Use == "standard" || foundCmd.Use == "diagnosis") &&
		foundCmd.Parent() != nil && (foundCmd.Parent().Use == "ssh" || foundCmd.Parent().Use == "k8s" || foundCmd.Parent().Use == "local" || foundCmd.Parent().Use == "local-k8s")
	isCollectSubCmd := isLeafCmd
	isRootCmd := err == nil && foundCmd.Use == RootCmd.Use

	// Derive transport from command path
	var transportFromCmd string
	if isLeafCmd {
		transportFromCmd = foundCmd.Parent().Use // "ssh" or "k8s"
	}

	// Detect removed --mode flag on collect-related commands and return migration error
	if isCollectSubCmd || (err == nil && foundCmd.Use == "collect") {
		for _, arg := range args[1:] {
			if arg == "--mode" || strings.HasPrefix(arg, "--mode=") {
				return fmt.Errorf("the --mode flag has been removed; use 'ddc collect ssh standard' or 'ddc collect k8s diagnosis' instead")
			}
		}
	}

	// Detect old syntax: "ddc collect standard ..." or "ddc collect diagnosis ..."
	// (standard/diagnosis are no longer direct children of collect)
	if err == nil && foundCmd.Use == "collect" {
		for i, arg := range args[1:] {
			if arg == "collect" && i+1 < len(args[1:]) {
				next := args[1:][i+1]
				if next == "standard" || next == "diagnosis" {
					return fmt.Errorf("command structure has changed: use 'ddc collect ssh %s' or 'ddc collect k8s %s' instead of 'ddc collect %s'", next, next, next)
				}
				break
			}
		}
	}

	// Bare "ddc collect" without subcommand — show subcommand help
	if err == nil && foundCmd.Use == "collect" {
		return CollectCmd.Help()
	}

	// Bare "ddc collect ssh" or "ddc collect k8s" or "ddc collect local" or "ddc collect local-k8s" without mode — show subcommand help
	if err == nil && (foundCmd.Use == "ssh" || foundCmd.Use == "k8s" || foundCmd.Use == "local" || foundCmd.Use == "local-k8s") && foundCmd.Parent() != nil && foundCmd.Parent().Use == "collect" {
		return foundCmd.Help()
	}

	// "ddc collect k8s standard help" — treat trailing "help" as a help request
	if isCollectSubCmd {
		for _, arg := range args[1:] {
			if arg == "help" {
				return foundCmd.Help()
			}
		}
	}

	if (isRootCmd || isCollectSubCmd) && !errors.Is(buildMergedFlagSet(foundCmd).Parse(args[1:]), pflag.ErrHelp) {
		// Set collection mode from subcommand (PersistentPreRunE doesn't fire in manual dispatch)
		nonInteractive := isCollectSubCmd
		switch foundCmd.Use {
		case "standard":
			collectionMode = collects.StandardCollection
		case "diagnosis":
			collectionMode = collects.DiagnosisCollection
		}

		// Store transport for later use
		transportCmd = transportFromCmd

		// Check for unknown flags
		if err := validateUnknownFlags(args[1:], foundCmd); err != nil {
			return err
		}

		// Early validation — before creating the shutdown hook so that CLI
		// errors exit cleanly without rendering the status screen.
		if nonInteractive {
			if transportFromCmd == "ssh" && coordinatorStr == "" {
				return fmt.Errorf("--coordinator is required for SSH transport. Example: ddc collect ssh standard --coordinator 10.0.0.1 --ssh-user myuser --ssh-key ~/.ssh/id_rsa")
			}
			if transportFromCmd == "k8s" && namespace == "" {
				return fmt.Errorf("--namespace is required for K8s transport. Example: ddc collect k8s standard --namespace mynamespace")
			}
			// local transport is zero-config — no required flags
		}

		// Initialize logger after flags have been parsed
		if outputLoc != "" {
			outputDir := filepath.Dir(outputLoc)
			simplelog.InitLoggerWithOutputDir(outputDir)
		}

		hook := shutdown.NewHook()
		defer hook.Cleanup()
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-c
			simplelog.Info("CTRL+C interrupt starting graceful shutdown")
			consoleprint.UpdateResult("CANCELLING")
			hook.Interrupt()
			os.Exit(1)
		}()
		if pid != "" {
			if _, err := os.Stat(pid); err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("unable to read pid location '%v': %w", pid, err)
				}
				// this means nothing is present great continue
				if err := os.WriteFile(filepath.Clean(pid), []byte(""), 0o600); err != nil {
					return fmt.Errorf("unable to write pid file '%v': %w", pid, err)
				}
				hook.AddFinalSteps(func() {
					if err := os.Remove(pid); err != nil {
						msg := fmt.Sprintf("unable to remove pid '%v': '%v', it will need to be removed manually", pid, err)
						consoleprint.WarningPrint(msg)
						simplelog.Warning(msg)
					}
				}, fmt.Sprintf("removing root pid file %v", pid))
			} else {
				return fmt.Errorf("DDC is running based on pid file '%v'. If this is a stale file then please remove", pid)
			}
		}

		skipPromptUI := enableFallback || nonInteractive || (namespace != "") || sshUser != ""
		if !skipPromptUI {
			var newerVersion string
			if !skipVersionCheck {
				if info, err := version.CheckForUpdate(versions.Version); err == nil && info != nil && info.IsNewer {
					newerVersion = info.LatestVersion
				}
			}
			configui.PrintBanner(versions.Version, newerVersion)
			// Step 1: Collection mode
			if err := huh.NewForm(
				huh.NewGroup(
					huh.NewSelect[collects.CollectionMode]().
						Title("Collection Mode").
						Options(
							huh.NewOption("Standard  — Usage data       [Healthcheck, WAF]", collects.StandardCollection),
							huh.NewOption("Diagnosis — Full diagnostics [Support ONLY]", collects.DiagnosisCollection),
						).
						Value(&collectionMode),
					huh.NewNote().Description("\n\n\n\n\n\n\n\n\n\n"),
				),
			).WithTheme(huh.ThemeCharm()).Run(); err != nil {
				fmt.Println("\nCancelled")
				os.Exit(0)
			}

			// Diagnosis mode: gate behind support password before proceeding
			if collectionMode == collects.DiagnosisCollection {
				var pw string
				diagKeymap := huh.NewDefaultKeyMap()
				diagKeymap.Quit = key.NewBinding(key.WithKeys("esc", "ctrl+c"))
				if err := huh.NewForm(
					huh.NewGroup(
						huh.NewInput().
							Title("WARNING: Diagnosis mode runs JVM diagnostic tools that may cause\nbrief unresponsiveness on an already degraded cluster.").
							Description("\nType the support password to continue, or press Esc to cancel.").
							EchoMode(huh.EchoModePassword).
							Value(&pw).
							Validate(func(s string) error {
								if s != "support" {
									return fmt.Errorf("incorrect password")
								}
								return nil
							}),
						huh.NewNote().Description("\n\n\n\n\n\n\n\n\n\n"),
					),
				).WithTheme(huh.ThemeCharm()).WithKeyMap(diagKeymap).Run(); err != nil {
					fmt.Println("\nCancelled")
					os.Exit(0)
				}
			}

			// Step 2: Transport selection
			var transport string
			if err := huh.NewForm(
				huh.NewGroup(
					huh.NewSelect[string]().
						Title("Transport").
						Description("Select how DDC connects to Dremio nodes").
						Options(
							huh.NewOption("Kubernetes", "k8s"),
							huh.NewOption("SSH", "ssh"),
							huh.NewOption("Local", "local"),
							huh.NewOption("Local-K8s", "local-k8s"),
						).
						Value(&transport),
					huh.NewNote().Description("\n\n\n\n\n\n\n\n\n\n"),
				),
			).WithTheme(huh.ThemeCharm()).Run(); err != nil {
				fmt.Println("\nCancelled")
				os.Exit(0)
			}

			// Store transport for CLI generation and later use
			transportCmd = transport

			switch transport {
			case "ssh":
				// Step 3a: SSH connection details
				home, err := os.UserHomeDir()
				if err != nil {
					return err
				}
				sshDir := filepath.Join(home, ".ssh")
				entries, err := os.ReadDir(sshDir)
				if err != nil {
					return err
				}
				var keyOptions []huh.Option[string]
				for _, e := range entries {
					if strings.HasPrefix(e.Name(), "id_") && !strings.HasSuffix(e.Name(), ".pub") {
						keyPath := filepath.Join(sshDir, e.Name())
						keyOptions = append(keyOptions, huh.NewOption(keyPath, keyPath))
					}
				}
				if len(keyOptions) == 0 {
					keyOptions = append(keyOptions, huh.NewOption("(no keys found — enter path manually)", ""))
				}

				sshForm := huh.NewForm(
					huh.NewGroup(
						huh.NewInput().Title("SSH user").Value(&sshUser).Validate(func(s string) error {
							if s == "" {
								return fmt.Errorf("required")
							}
							return nil
						}),
						huh.NewInput().Title("Sudo user").Value(&sudoUser).Description("Runs on remote servers as this user (e.g. dremio)"),
						huh.NewSelect[string]().Title("SSH key").Options(keyOptions...).Value(&sshKeyLoc),
						huh.NewInput().Title("Coordinators").Value(&coordinatorStr).
							Description("Comma-separated IPs, e.g. 192.168.1.10,192.168.1.12").
							Validate(func(s string) error {
								if s == "" {
									return fmt.Errorf("at least one coordinator required")
								}
								return nil
							}),
						huh.NewInput().Title("Executors").Value(&executorsStr).
							Description("Comma-separated IPs, e.g. 192.168.1.20,192.168.1.21"),
					).Title("SSH Connection").Description(" "),
				).WithTheme(huh.ThemeCharm())

				if err := sshForm.Run(); err != nil {
					return fmt.Errorf("SSH configuration failed: %w", err)
				}
				// Strip spaces from comma-separated host lists so downstream
				// consumers (CLI command generation, SSH transport) get clean values.
				coordinatorStr = strings.ReplaceAll(coordinatorStr, " ", "")
				executorsStr = strings.ReplaceAll(executorsStr, " ", "")
			case "k8s":
				// Step 3a (new): If no kubeconfig auto-detected (file missing or zero contexts),
				// prompt for an explicit path with inline validation and a connectivity probe.
				contexts, currentCtx, ctxErr := kubernetes.ListContexts(kubeconfigPath)
				if ctxErr != nil {
					simplelog.Warningf("could not list kubeconfig contexts: %v", ctxErr)
				}
				if len(contexts) == 0 {
					if err := promptKubeconfigPath(); err != nil {
						return err
					}
					// Re-enumerate contexts now that the user has supplied a path.
					contexts, currentCtx, ctxErr = kubernetes.ListContexts(kubeconfigPath)
					if ctxErr != nil {
						simplelog.Warningf("could not list kubeconfig contexts after input: %v", ctxErr)
					}
				}

				// Step 3b: K8s context selection (if multiple contexts available)
				if len(contexts) > 0 {
					// Preselect the current context.
					k8sContext = currentCtx
					var ctxOptions []huh.Option[string]
					for _, ctx := range contexts {
						ctxOptions = append(ctxOptions, huh.NewOption(ctx, ctx))
					}
					if err := huh.NewForm(
						huh.NewGroup(
							huh.NewSelect[string]().
								Title("Kubernetes Context").
								Description("Select the kubeconfig context to use").
								Options(ctxOptions...).
								Value(&k8sContext),
							huh.NewNote().Description("\n\n\n\n\n\n\n\n\n\n"),
						),
					).WithTheme(huh.ThemeCharm()).Run(); err != nil {
						fmt.Println("\nCancelled")
						os.Exit(0)
					}
				}

				// Step 3c: K8s namespace selection
				var clustersToList []string
				var clusterErr error
				_ = spinner.New().
					Title("Detecting Kubernetes namespaces...").
					Action(func() {
						clustersToList, clusterErr = kubernetes.GetClusters(k8sContext, detectLabelSelector, kubeconfigPath)
					}).
					Run()
				if clusterErr != nil {
					return clusterErr
				}
				var nsOptions []huh.Option[string]
				for _, ns := range clustersToList {
					nsOptions = append(nsOptions, huh.NewOption(ns, ns))
				}
				if err := huh.NewForm(
					huh.NewGroup(
						huh.NewSelect[string]().
							Title("Kubernetes Namespace").
							Description("Select the namespace with your Dremio cluster").
							Options(nsOptions...).
							Value(&namespace),
						huh.NewNote().Description("\n\n\n\n\n\n\n\n\n\n"),
					),
				).WithTheme(huh.ThemeCharm()).Run(); err != nil {
					fmt.Println("\nCancelled")
					os.Exit(0)
				}
			}

		}

		// Interactive mode: run path discovery on a target node, then show config screen
		if !skipPromptUI {
			var detected *configui.DetectedPaths
			if transportCmd == "local" || transportCmd == "local-k8s" {
				detected = runLocalPathDiscovery()
			} else {
				detected = runPathDiscovery(namespace, coordinatorStr, sshUser, sshKeyLoc, k8sContext, kubeconfigPath)
			}
			switch collectionMode {
			case collects.StandardCollection:
				if err := runStandardConfigScreen(detected); err != nil {
					return err
				}
			case collects.DiagnosisCollection:
				if err := runDiagnosisConfigScreen(detected); err != nil {
					return err
				}
			}
		}

		if sshKeyLoc == "" {
			sshDefault, err := sshDefault()
			if err != nil {
				return fmt.Errorf("unable to get the ssh directory. This is a critical error and should result in a bug report: %w", err)
			}
			sshKeyLoc = sshDefault
		}

		simplelog.Info(versions.GetCLIVersion())
		simplelog.Infof("user cli command: %v", strings.Join(args, " "))

		// Version check — for non-interactive runs, log the update notice to ddc.log.
		// Interactive mode already shows the update in the TUI banner.
		if !skipVersionCheck && skipPromptUI {
			if info, err := version.CheckForUpdate(versions.Version); err == nil && info != nil && info.IsNewer {
				simplelog.Infof("DDC %v is available (current: %v). Download: %v", info.LatestVersion, info.CurrentVersion, info.ReleaseURL)
			}
		}

		// PAT token: CLI flag > env var > stdin > config
		if cliAuthToken == "" {
			if envPAT := os.Getenv("DDC_PAT_TOKEN"); envPAT != "" {
				cliAuthToken = envPAT
				simplelog.Info("using PAT token from DDC_PAT_TOKEN environment variable")
			}
		}

		if skipPromptUI && collectionMode == "" {
			return fmt.Errorf("collection mode is required. Use 'ddc collect standard' or 'ddc collect diagnosis'")
		}
		// Default to standard if interactive mode didn't set it yet
		if collectionMode == "" {
			collectionMode = collects.StandardCollection
		}

		// Validate v4 flag combinations
		if err := validateV4Flags(collectionMode, skipPromptUI); err != nil {
			return err
		}

		confData := BuildConfData(foundCmd, collectionMode)
		if !disableFreeSpaceCheck {
			abs, err := filepath.Abs(outputLoc)
			if err != nil {
				return err
			}
			outputFolder := filepath.Dir(abs)
			freeSpaceGB := uint64(25)
			if collectionMode == collects.DiagnosisCollection {
				freeSpaceGB = 40
			}
			if err := dirs.CheckFreeSpace(outputFolder, freeSpaceGB); err != nil {
				return dirs.FormatFreeSpaceError(false, err, collectionMode, collects.StandardCollection)
			}
		}

		// PAT resolution: CLI flag/env > stdin > config
		dremioPAT := ""
		if cliAuthToken != "" {
			dremioPAT = cliAuthToken
		} else if confPAT, ok := confData[conf.KeyDremioPatToken].(string); ok && confPAT != "" {
			dremioPAT = confPAT
		}
		if dremioPAT == "" {
			fi, err := os.Stdin.Stat()
			if err != nil {
				return err
			}
			if fi.Size() > 0 {
				simplelog.Info("accepting PAT from standard in")
				inputReader := RootCmd.InOrStdin()
				b, err := io.ReadAll(inputReader)
				if err != nil {
					return err
				}
				dremioPAT = strings.TrimSpace(string(b[:]))
			}
		}
		if err := validation.ValidateCollectMode(collectionMode); err != nil {
			return err
		}

		// Pre-check PAT validity in non-interactive mode — fail fast before starting collection.
		if dremioPAT != "" && dremioEndpoint != "" && nonInteractive {
			result := configui.ValidatePAT(dremioEndpoint, dremioPAT, allowInsecureSSL)
			if strings.Contains(result, "failed") {
				return fmt.Errorf("PAT pre-check failed against %s: %s", dremioEndpoint, result)
			}
			simplelog.Info("PAT pre-check passed")
		}
		patSet := dremioPAT != ""
		var enabled []string
		var disabled []string
		for k, v := range confData {
			if k == conf.KeyNumberJobProfiles {
				if v.(int) > 0 && patSet {
					enabled = append(enabled, "job-profiles")
				} else {
					disabled = append(disabled, "job-profiles")
				}
				continue
			}
			if strings.HasPrefix(k, "collect-") {
				newName := strings.TrimPrefix(k, "collect-")
				if value, ok := v.(bool); ok {
					// check pat so they end up in the right column
					if !patSet {
						if k == conf.KeyCollectKVStoreReport {
							disabled = append(disabled, newName)
							continue
						}
					}
					if value {
						enabled = append(enabled, newName)
					} else {
						disabled = append(disabled, newName)
					}
				}
			}
		}
		consoleprint.SetVersion(versions.GetCLIVersion())
		// Initialize Threads/Queued rows with N/A so they appear immediately on the status page.
		consoleprint.UpdateThreadStatus(-1, 1, 0)
		if progressFormat == "json" {
			consoleprint.EnableStatusOutput()
		}
		stop := startTicker()
		hook.AddUIStop(stop)
		// Parse system tables list
		var systemTablesList []string
		if systemTables != "" {
			for _, t := range strings.Split(systemTables, ",") {
				t = strings.TrimSpace(t)
				if t != "" {
					systemTablesList = append(systemTablesList, t)
				}
			}
		}
		simplelog.Debugf("system tables to collect (%d): %v", len(systemTablesList), systemTablesList)
		simplelog.Infof("collection args resolved: mode=%s daysFlag=%d diagLogDays=%d queriesPerfNumDays=%d queriesJSONNumDays=%d serverLogsNumDays=%d trackerJSONNumDays=%d vacuumLogNumDays=%d startDate=%q",
			collectionMode, daysFlag, diagLogDays(), queriesPerfNumDays, queriesJSONNumDays, serverLogsNumDays, trackerJSONNumDays, vacuumLogNumDays, startDate)
		collectionArgs := collection.Args{
			OutputLoc:             filepath.Clean(outputLoc),
			DDCfs:                 helpers.NewRealFileSystem(),
			DremioPAT:             dremioPAT,
			Enabled:               enabled,
			Disabled:              disabled,
			DisableFreeSpaceCheck: disableFreeSpaceCheck,
			CollectionMode:        collectionMode,
			CollectionThreads:     collectionThreads,
			CoordinatorLogDir:     coordinatorLogDir,
			ExecutorLogDir:        executorLogDir,
			DremioConfDir:         dremioConfDir,
			DremioRocksDBDir:      dremioRocksDBDir,
			ServerLogsNumDays:     serverLogsNumDays,
			TrackerJSONNumDays:    trackerJSONNumDays,
			VacuumLogNumDays:      vacuumLogNumDays,
			QueriesJSONNumDays:    queriesJSONNumDays,
			DiagLogDays:           diagLogDays(),
			StartDate:             startDate,
			// API collections (run from orchestrator)
			DremioEndpoint:             dremioEndpoint,
			AllowInsecureSSL:           allowInsecureSSL,
			RestHTTPTimeout:            30,
			CollectWLM:                 collectWLM,
			CollectKVStoreReport:       collectKVStoreReport,
			CollectProblematicProfiles: collectProblematicProfiles && collectionMode == collects.DiagnosisCollection,
			CollectSystemTables:        len(systemTablesList) > 0,
			SystemTables:               systemTablesList,
			// JVM collection (diagnosis mode only)
			CollectJStack:        collectJStack && collectionMode == collects.DiagnosisCollection,
			CollectTop:           collectTop && collectionMode == collects.DiagnosisCollection,
			CollectJVMFlags:      collectionMode == collects.DiagnosisCollection,
			CollectJFR:           collectJFR && collectionMode == collects.DiagnosisCollection,
			CollectHeapDump:      collectHeapDump && collectionMode == collects.DiagnosisCollection,
			CollectAsyncProfiler: collectAsyncProfiler && collectionMode == collects.DiagnosisCollection,
			DiagTimeSeconds:      diagTimeSeconds,
			CollectQueriesPerf:   collectQueriesPerf,
			QueriesPerfNumDays:   queriesPerfNumDays,
			// File collection gating
			CollectGCLogs:          collectGCLogs && collectionMode == collects.DiagnosisCollection,
			CollectServerLogs:      collectServerLogs,
			CollectQueriesJSON:     collectQueriesJSON,
			CollectTrackerJSON:     collectTrackerJSON,
			CollectVacuumLog:       collectVacuumLog,
			CollectAccelerationLog: collectAcceleration,
			CollectAccessLog:       collectAccessLog,
			CollectHSErrFiles:      collectHSErrFiles,
			CollectHiveDeprecated:  collectHiveDeprecated,
			CollectMetaRefreshLog:  resolveMetaRefresh(skipPromptUI, collectMetaRefresh, confData),
			// Node filtering (from TUI node selection or --nodes/--exclude-nodes flags)
			IncludeNodes: parseNodeList(nodesFlag),
			ExcludeNodes: parseNodeList(excludeNodesFlag),
		}
		sshArgs := ssh.Args{
			SSHKeyLoc:      sshKeyLoc,
			SSHUser:        sshUser,
			SudoUser:       sudoUser,
			ExecutorStr:    executorsStr,
			CoordinatorStr: coordinatorStr,
		}
		kubeArgs := kubernetes.KubeArgs{
			Namespace:      namespace,
			DetectLabelSelector:  detectLabelSelector,
			K8SContext:     k8sContext,
			KubeconfigPath: kubeconfigPath,
		}
		// Local transport uses the fallback (local collector) path in RemoteCollect.
		if transportCmd == "local" {
			enableFallback = true
			// --local-log-dir maps to both coordinator and executor log dirs
			if localLogDir != "" {
				coordinatorLogDir = localLogDir
				executorLogDir = localLogDir
				collectionArgs.CoordinatorLogDir = localLogDir
				collectionArgs.ExecutorLogDir = localLogDir
			}
		}
		// Local-K8s transport: local collector + optional K8s cluster-level collection
		if transportCmd == "local-k8s" {
			enableFallback = true
			if localLogDir != "" {
				coordinatorLogDir = localLogDir
				executorLogDir = localLogDir
				collectionArgs.CoordinatorLogDir = localLogDir
				collectionArgs.ExecutorLogDir = localLogDir
			}
		}
		localK8sMode := transportCmd == "local-k8s"
		if err := RemoteCollect(collectionArgs, sshArgs, kubeArgs, enableFallback, hook, skipPromptUI, localK8sMode); err != nil {
			// Detect user cancellation (Ctrl+C in TUI forms, cancelled config screens)
			// and show a clean message instead of the full error in the status screen.
			errMsg := err.Error()
			if strings.Contains(errMsg, "user aborted") || strings.Contains(errMsg, "cancelled by user") {
				consoleprint.UpdateResult("CANCELLED")
			} else {
				consoleprint.UpdateResult(errMsg)
			}
		}
		return nil
	}
	if err := RootCmd.Execute(); err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "user aborted") || strings.Contains(errMsg, "cancelled by user") {
			consoleprint.UpdateResult("CANCELLED")
			return nil
		}
		return err
	}
	return nil
}

type unableToGetHomeDir struct {
	Err error
}

func (u unableToGetHomeDir) Error() string {
	return fmt.Sprintf("unable to get home dir '%v'", u.Err)
}

// sshDefault returns the default .ssh key typically used on most deployments

func sshDefault() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", unableToGetHomeDir{Err: err}
	}
	return filepath.Join(home, ".ssh", "id_rsa"), nil
}

func init() {
	// command line flags — organized by command level.

	// ── SSH transport flags — on SSHCmd.PersistentFlags() ──
	SSHCmd.PersistentFlags().StringVarP(&coordinatorStr, "coordinator", "c", "", "set a list of ip addresses separated by commas")
	SSHCmd.PersistentFlags().StringVarP(&executorsStr, "executors", "e", "", "set a list of ip addresses separated by commas")
	SSHCmd.PersistentFlags().StringVarP(&sshKeyLoc, "ssh-key", "s", "", "of ssh key to use to login")
	SSHCmd.PersistentFlags().StringVarP(&sshUser, "ssh-user", "u", "", "user to use during ssh operations to login")
	SSHCmd.PersistentFlags().StringVarP(&sudoUser, "sudo-user", "b", "", "if any diagnostics commands need a sudo user (i.e. for jcmd)")
	SSHCmd.PersistentFlags().BoolVar(&sshStrictHostKeys, "ssh-strict-host-keys", false, "enable strict host key checking (default: false for backward compatibility)")

	// ── K8s transport flags — on K8sCmd.PersistentFlags() ──
	K8sCmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "", "namespace to use for kubernetes pods")
	K8sCmd.PersistentFlags().StringVarP(&k8sContext, "context", "x", "", "context to use for kubernetes pods")
	K8sCmd.PersistentFlags().StringVar(&kubeconfigPath, "kubeconfig", "", "path to kubeconfig file (overrides $KUBECONFIG and ~/.kube/config)")
	K8sCmd.PersistentFlags().StringVar(&detectLabelSelector, "detect-label-selector", "role=dremio-cluster-pod", "label selector used to identify Dremio coordinator/executor pods for file streaming; follows kubernetes label syntax (https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#label-selectors)")
	K8sCmd.PersistentFlags().StringVarP(&containerLogLabelSelector, "container-log-label-selector", "l", "", "label selector to filter which pods' container logs are collected (default: empty = all pods in the namespace); follows kubernetes label syntax")
	K8sCmd.PersistentFlags().BoolVarP(&enableKubeCtl, "enable-kubectl", "d", false, "uses the kubectl CLI for transfers and copying instead of the embedded k8s api client")
	K8sCmd.PersistentFlags().BoolVar(&collectContainerLogs, "collect-container-logs", false, "collect Kubernetes container logs (default: disabled for standard, enabled for diagnosis)")
	K8sCmd.PersistentFlags().StringVar(&nodesFlag, "nodes", "", "comma-separated list of nodes to collect from")
	K8sCmd.PersistentFlags().StringVar(&excludeNodesFlag, "exclude-nodes", "", "comma-separated list of nodes to exclude (mutually exclusive with --nodes)")

	// ── Local transport flags — on LocalCmd.PersistentFlags() ──
	LocalCmd.PersistentFlags().StringVar(&dremioHome, "dremio-home", "/opt/dremio", "Dremio installation directory")
	LocalCmd.PersistentFlags().StringVar(&localLogDir, "local-log-dir", "", "Log directory on this node (autodetected if not specified)")

	// ── Local-K8s transport flags — on LocalK8sCmd.PersistentFlags() ──
	LocalK8sCmd.PersistentFlags().StringVar(&dremioHome, "dremio-home", "/opt/dremio", "Dremio installation directory")
	LocalK8sCmd.PersistentFlags().StringVar(&localLogDir, "local-log-dir", "", "Log directory on this node (autodetected if not specified)")
	LocalK8sCmd.PersistentFlags().StringVar(&kubeconfigPath, "kubeconfig", "", "path to kubeconfig file used when in-cluster config is unavailable")

	// ── Shared flags — on CollectCmd.PersistentFlags(), inherited by all leaf commands ──
	CollectCmd.PersistentFlags().BoolVar(&disableFreeSpaceCheck, conf.KeyDisableFreeSpaceCheck, false, "disables the free space check for the output directory")
	CollectCmd.PersistentFlags().StringVar(&pid, "pid", "", "write a pid")
	if err := CollectCmd.PersistentFlags().MarkHidden("pid"); err != nil {
		simplelog.Errorf("unable to mark flag hidden critical error %v", err)
		os.Exit(1)
	}
	CollectCmd.PersistentFlags().IntVar(&collectionThreads, "collection-threads", 0, "number of threads to collect from nodes simultaneously (0 = mode default: 20 diagnosis, 5 standard)")
	CollectCmd.PersistentFlags().StringVar(&outputLoc, "output-file", fmt.Sprintf("diag-%s.tgz", time.Now().Format("20060102-150405")), "name and location of diagnostic tarball")
	CollectCmd.PersistentFlags().StringVar(&collectorTimeout, "collector-timeout", "", "per-collector timeout (default: 20m diagnosis, 10m standard)")
	CollectCmd.PersistentFlags().StringVar(&progressFormat, "progress", "", "progress output format: 'json' for machine-readable CI/CD output")
	CollectCmd.PersistentFlags().StringVar(&coordinatorLogDir, "coordinator-log-dir", "", "Coordinator log directory (autodetected if not specified)")
	CollectCmd.PersistentFlags().StringVar(&executorLogDir, "executor-log-dir", "", "Executor log directory (autodetected if not specified)")
	CollectCmd.PersistentFlags().StringVar(&dremioConfDir, "dremio-conf-dir", "", "Dremio configuration directory (autodetected if not specified)")
	CollectCmd.PersistentFlags().StringVar(&dremioRocksDBDir, "dremio-rocksdb-dir", "", "Dremio RocksDB directory (autodetected if not specified)")
	// --allow-insecure-ssl is diagnosis-only (registered below with other diagnosis flags)

	// Derive CLI flag defaults from the single source of truth in defaults.go.
	stdDef := conf.StandardDefaultMap()
	diagDef := conf.DiagnosisDefaultMap()

	// ── Collection toggles — standard commands use stdDef, diagnosis commands use diagDef ──
	for _, cmd := range []*cobra.Command{SSHStandardCmd, K8sStandardCmd, LocalStandardCmd, LocalK8sStandardCmd} {
		cmd.Flags().BoolVar(&collectQueriesJSON, "collect-queries-json", conf.GetBoolDefault(stdDef, conf.KeyCollectQueriesJSON), "collect queries.json files")
		cmd.Flags().BoolVar(&collectQueriesPerf, "collect-queries-perf-json", conf.GetBoolDefault(stdDef, conf.KeyCollectQueriesPerfJSON), "collect queries performance data from RocksDB")
		cmd.Flags().BoolVar(&collectServerLogs, "collect-server-logs", conf.GetBoolDefault(stdDef, conf.KeyCollectServerLogs), "collect server.log files")
		cmd.Flags().BoolVar(&collectTrackerJSON, "collect-tracker-json", conf.GetBoolDefault(stdDef, conf.KeyCollectTrackerJSON), "collect tracker.json files")
		cmd.Flags().BoolVar(&collectVacuumLog, "collect-vacuum-log", conf.GetBoolDefault(stdDef, conf.KeyCollectVacuumLog), "collect vacuum.json files")
		cmd.Flags().BoolVar(&collectMetaRefresh, "collect-meta-refresh-log", conf.GetBoolDefault(stdDef, conf.KeyCollectMetaRefreshLog), "collect metadata_refresh.log files")
		cmd.Flags().BoolVar(&collectWLM, "collect-wlm", conf.GetBoolDefault(stdDef, conf.KeyCollectWLM), "collect WLM configuration")
		cmd.Flags().StringVar(&systemTables, "system-tables", strings.Join(conf.SystemTableList(), ","), "comma-separated list of system tables to collect")
		cmd.Flags().IntVar(&queriesPerfNumDays, conf.KeyQueriesPerfNumDays, conf.GetIntDefault(stdDef, conf.KeyQueriesPerfNumDays), "number of days of queries performance data to collect")
	}
	for _, cmd := range []*cobra.Command{SSHDiagnosisCmd, K8sDiagnosisCmd, LocalDiagnosisCmd, LocalK8sDiagnosisCmd} {
		cmd.Flags().BoolVar(&collectKVStoreReport, "collect-kvstore-report", conf.GetBoolDefault(diagDef, conf.KeyCollectKVStoreReport), "collect KV store report (requires --dremio-pat-token)")
		cmd.Flags().BoolVar(&collectQueriesJSON, "collect-queries-json", conf.GetBoolDefault(diagDef, conf.KeyCollectQueriesJSON), "collect queries.json files")
		cmd.Flags().BoolVar(&collectQueriesPerf, "collect-queries-perf-json", conf.GetBoolDefault(diagDef, conf.KeyCollectQueriesPerfJSON), "collect queries performance data from RocksDB")
		cmd.Flags().BoolVar(&collectServerLogs, "collect-server-logs", conf.GetBoolDefault(diagDef, conf.KeyCollectServerLogs), "collect server.log files")
		cmd.Flags().BoolVar(&collectTrackerJSON, "collect-tracker-json", conf.GetBoolDefault(diagDef, conf.KeyCollectTrackerJSON), "collect tracker.json files")
		cmd.Flags().BoolVar(&collectVacuumLog, "collect-vacuum-log", conf.GetBoolDefault(diagDef, conf.KeyCollectVacuumLog), "collect vacuum.json files")
		cmd.Flags().BoolVar(&collectMetaRefresh, "collect-meta-refresh-log", conf.GetBoolDefault(diagDef, conf.KeyCollectMetaRefreshLog), "collect metadata_refresh.log files")
		cmd.Flags().BoolVar(&collectWLM, "collect-wlm", conf.GetBoolDefault(diagDef, conf.KeyCollectWLM), "collect WLM configuration")
		cmd.Flags().StringVar(&systemTables, "system-tables", strings.Join(conf.SystemTableList(), ","), "comma-separated list of system tables to collect")
	}

	// ── --collect-hs-err-files — diagnosis only ──
	for _, cmd := range []*cobra.Command{SSHDiagnosisCmd, K8sDiagnosisCmd, LocalDiagnosisCmd, LocalK8sDiagnosisCmd} {
		cmd.Flags().BoolVar(&collectHSErrFiles, "collect-hs-err-files", conf.GetBoolDefault(diagDef, conf.KeyCollectHSErrFiles), "collect hs_err crash dump files")
	}

	// ── Per-log day counts — standard mode only ──
	for _, cmd := range []*cobra.Command{SSHStandardCmd, K8sStandardCmd, LocalStandardCmd, LocalK8sStandardCmd} {
		cmd.Flags().IntVar(&queriesJSONNumDays, conf.KeyQueriesJSONNumDays, conf.GetIntDefault(stdDef, conf.KeyQueriesJSONNumDays), "number of days of queries.json to collect")
		cmd.Flags().IntVar(&serverLogsNumDays, conf.KeyServerLogsNumDays, conf.GetIntDefault(stdDef, conf.KeyServerLogsNumDays), "number of days of server logs to collect")
		cmd.Flags().IntVar(&trackerJSONNumDays, conf.KeyTrackerJSONNumDays, conf.GetIntDefault(stdDef, conf.KeyTrackerJSONNumDays), "number of days of tracker.json to collect")
		cmd.Flags().IntVar(&vacuumLogNumDays, conf.KeyVacuumLogNumDays, conf.GetIntDefault(stdDef, conf.KeyVacuumLogNumDays), "number of days of vacuum.json to collect")
	}

	// ── Diagnosis-only flags ──
	for _, cmd := range []*cobra.Command{SSHDiagnosisCmd, K8sDiagnosisCmd, LocalDiagnosisCmd, LocalK8sDiagnosisCmd} {
		cmd.Flags().StringVar(&cliAuthToken, "dremio-pat-token", "", "Dremio PAT token for API-based collection (env: DDC_PAT_TOKEN)")
		cmd.Flags().StringVar(&dremioEndpoint, "dremio-endpoint", "", "Dremio REST API endpoint (e.g. http://localhost:9047)")
		cmd.Flags().BoolVar(&allowInsecureSSL, "allow-insecure-ssl", true, "allow insecure SSL connections to Dremio REST API")
		cmd.Flags().BoolVar(&collectProblematicProfiles, "collect-problematic-profiles", conf.GetBoolDefault(diagDef, conf.KeyCollectProblematicProfiles), "scan server.log for problematic job IDs and download their profiles (requires --dremio-pat-token)")
		cmd.Flags().BoolVar(&collectJFR, "diag-jfr", conf.GetBoolDefault(diagDef, conf.KeyCollectJFR), "collect Java Flight Recorder recording")
		cmd.Flags().BoolVar(&collectJStack, "diag-jstack", conf.GetBoolDefault(diagDef, conf.KeyCollectJStack), "collect jstack thread dumps")
		cmd.Flags().BoolVar(&collectTop, "diag-top", conf.GetBoolDefault(diagDef, conf.KeyCollectTop), "collect top process snapshots")
		cmd.Flags().BoolVar(&collectHeapDump, "diag-heap-dump", conf.GetBoolDefault(diagDef, conf.KeyCaptureHeapDump), "collect heap dump (requires disk >= -Xmx per pod)")
		cmd.Flags().BoolVar(&collectAsyncProfiler, "diag-async-profiler", conf.GetBoolDefault(diagDef, conf.KeyCollectAsyncProfiler), "collect async-profiler recording")
		cmd.Flags().BoolVar(&collectGCLogs, "collect-gc-logs", conf.GetBoolDefault(diagDef, conf.KeyCollectGCLogs), "collect GC log files")
		cmd.Flags().BoolVar(&collectAcceleration, "collect-acceleration-log", conf.GetBoolDefault(diagDef, conf.KeyCollectAccelerationLog), "collect acceleration.log files")
		cmd.Flags().BoolVar(&collectAccessLog, "collect-access-log", conf.GetBoolDefault(diagDef, conf.KeyCollectAccessLog), "collect access.log files")
		cmd.Flags().BoolVar(&collectHiveDeprecated, "collect-hive-deprecated-log", conf.GetBoolDefault(diagDef, conf.KeyCollectHiveDeprecatedLog), "collect hive-deprecated.log files")
		cmd.Flags().IntVar(&diagTimeSeconds, "diag-time-seconds", conf.GetIntDefault(diagDef, conf.KeyDiagTimeSeconds), "duration in seconds for all diagnostic tools (JFR, jstack, top, async-profiler)")
		cmd.Flags().StringVar(&startDate, "start-date", "", "start of collection date range (date-only, e.g. 2026-03-20). Defaults to now minus --days")
		cmd.Flags().IntVar(&daysFlag, "days", conf.GetIntDefault(diagDef, conf.KeyDremioLogsNumDays), "number of days to collect from --start-date (default: 3)")
	}

	// Wire up subcommands
	SSHCmd.AddCommand(SSHStandardCmd)
	SSHCmd.AddCommand(SSHDiagnosisCmd)
	K8sCmd.AddCommand(K8sStandardCmd)
	K8sCmd.AddCommand(K8sDiagnosisCmd)
	LocalCmd.AddCommand(LocalStandardCmd)
	LocalCmd.AddCommand(LocalDiagnosisCmd)
	LocalK8sCmd.AddCommand(LocalK8sStandardCmd)
	LocalK8sCmd.AddCommand(LocalK8sDiagnosisCmd)
	CollectCmd.AddCommand(SSHCmd)
	CollectCmd.AddCommand(K8sCmd)
	CollectCmd.AddCommand(LocalCmd)
	CollectCmd.AddCommand(LocalK8sCmd)

	// init
	cobra.EnableCommandSorting = false
	RootCmd.PersistentFlags().CountP("verbose", "v", "Logging verbosity")
	RootCmd.PersistentFlags().BoolVar(&skipVersionCheck, "skip-version-check", false, "skip checking for newer DDC versions at startup")
	RootCmd.AddCommand(CollectCmd)
	RootCmd.AddCommand(version.VersionCmd)
	RootCmd.CompletionOptions.DisableDefaultCmd = true
}

// detectK8sNamespace reads the Kubernetes namespace from the service account file.
func detectK8sNamespace(path string) (string, error) {
	data, err := os.ReadFile(filepath.Clean(path)) // #nosec G304 -- path is K8s service account namespace file
	if err != nil {
		return "", fmt.Errorf("unable to read namespace from %s: %w", path, err)
	}
	ns := strings.TrimSpace(string(data))
	if ns == "" {
		return "", fmt.Errorf("namespace file %s is empty", path)
	}
	return ns, nil
}

// readNamespaceFromKubeconfig returns the default namespace declared on the
// current-context of the supplied kubeconfig (after the standard precedence
// resolution explicit → $KUBECONFIG → ~/.kube/config). Returns "" with no
// error if the current-context has no default namespace declared.
func readNamespaceFromKubeconfig(explicit string) (string, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if explicit != "" {
		loadingRules.ExplicitPath = dirs.ExpandTilde(explicit)
	}
	cfg, err := loadingRules.Load()
	if err != nil {
		return "", err
	}
	currentCtx := cfg.CurrentContext
	if currentCtx == "" {
		return "", nil
	}
	ctx, ok := cfg.Contexts[currentCtx]
	if !ok || ctx == nil {
		return "", nil
	}
	return ctx.Namespace, nil
}

func validateSSHParameters(sshArgs ssh.Args) error {
	if sshArgs.SSHKeyLoc == "" {
		return errors.New("the ssh private key location was empty, pass --ssh-key or -s with the key to get past this error. Example --ssh-key ~/.ssh/id_rsa")
	}
	if sshArgs.SSHUser == "" {
		return errors.New("the ssh user was empty, pass --ssh-user or -u with the user name you want to use to get past this error. Example --ssh-user ubuntu")
	}
	return nil
}

var enableFallback bool

// validateV4Flags implements the CLI flag validation rules from architecture-plan.md §1.3.
// cliMode is true when running non-interactively (skipPromptUI).
// parseNodeList splits a comma-separated node string into a trimmed slice.
func parseNodeList(s string) []string {
	if s == "" {
		return nil
	}
	var nodes []string
	for _, n := range strings.Split(s, ",") {
		if n = strings.TrimSpace(n); n != "" {
			nodes = append(nodes, n)
		}
	}
	return nodes
}

func validateV4Flags(mode collects.CollectionMode, cliMode bool) error {
	if err := validation.ValidateCollectMode(mode); err != nil {
		return err
	}

	// Validate --start-date format if provided
	if startDate != "" {
		if _, err := time.Parse("2006-01-02", startDate); err != nil {
			return fmt.Errorf("--start-date must be date-only format (e.g. 2026-03-20), got %q", startDate)
		}
	}
	// PAT-dependent collections are silently disabled when no PAT is provided.
	// The warning at the end of this function covers user notification.
	// --nodes and --exclude-nodes are mutually exclusive
	if nodesFlag != "" && excludeNodesFlag != "" {
		return fmt.Errorf("--nodes and --exclude-nodes are mutually exclusive — use one or the other")
	}
	// Diagnosis without PAT: warn
	if mode == collects.DiagnosisCollection && cliAuthToken == "" {
		simplelog.Warning("PAT token not provided — job profiles, system tables, WLM, and KV store report will not be collected")
	}

	return nil
}

// populateDetectedPaths ensures transport info from CLI globals is available in the DetectedPaths
// for CLI command generation. Shared by both config screen launchers.
func populateDetectedPaths(detected *configui.DetectedPaths) *configui.DetectedPaths {
	if detected == nil {
		detected = &configui.DetectedPaths{}
	}
	detected.Transport = transportCmd
	if detected.Namespace == "" {
		detected.Namespace = namespace
	}
	if detected.Kubeconfig == "" {
		detected.Kubeconfig = kubeconfigPath
	}
	if detected.Coordinator == "" {
		detected.Coordinator = coordinatorStr
	}
	if detected.Executors == "" {
		detected.Executors = executorsStr
	}
	if detected.SSHUser == "" {
		detected.SSHUser = sshUser
	}
	if detected.K8sContext == "" {
		detected.K8sContext = k8sContext
	}
	if detected.DremioHome == "" {
		detected.DremioHome = dremioHome
	}
	return detected
}

// runStandardConfigScreen shows the TUI configuration form for standard mode.
func runStandardConfigScreen(detected *configui.DetectedPaths) error {
	detected = populateDetectedPaths(detected)
	cfg, err := configui.RunStandardConfigScreen(detected, versions.Version)
	if err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			fmt.Println("\nCancelled")
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "configuration UI failed: %v\n", err)
		os.Exit(1)
	}
	if cfg.Cancelled {
		fmt.Println("\nCancelled")
		os.Exit(0)
	}
	simplelog.Infof("generated cli command: %v", cfg.GeneratedCommand)
	// Apply config to global variables
	coordinatorLogDir = cfg.CoordinatorLogDir
	executorLogDir = cfg.ExecutorLogDir
	// For local transport, the TUI shows a single "Log dir" field stored in CoordinatorLogDir
	if transportCmd == "local" || transportCmd == "local-k8s" {
		executorLogDir = cfg.CoordinatorLogDir
	}
	dremioConfDir = cfg.DremioConfDir
	dremioRocksDBDir = cfg.DremioRocksDBDir
	allowInsecureSSL = cfg.AllowInsecureSSL
	collectServerLogs = cfg.CollectServerLogs
	serverLogsNumDays = cfg.ServerLogsDays
	collectTrackerJSON = cfg.CollectTrackerJSON
	trackerJSONNumDays = cfg.TrackerJSONDays
	collectVacuumLog = cfg.CollectVacuumLog
	vacuumLogNumDays = cfg.VacuumLogDays
	collectMetaRefresh = cfg.CollectMetaRefresh
	collectQueriesJSON = cfg.CollectQueriesJSON
	queriesJSONNumDays = cfg.QueriesJSONDays
	collectQueriesPerf = cfg.CollectQueriesPerf
	queriesPerfNumDays = cfg.QueriesPerfDays
	collectWLM = cfg.CollectWLM
	collectContainerLogs = cfg.CollectContainerLogs
	systemTables = strings.Join(cfg.SystemTables, ",")

	return nil
}

// runDiagnosisConfigScreen shows the TUI configuration form for diagnosis mode.
func runDiagnosisConfigScreen(detected *configui.DetectedPaths) error {
	detected = populateDetectedPaths(detected)

	// Discover nodes before showing the config screen.
	var discoveredCoordinators, discoveredExecutors []string
	switch transportCmd {
	case "k8s":
		// K8s mode: discover pods via the K8s API.
		_ = spinner.New().
			Title("Discovering Dremio pods...").
			Action(func() {
				var discoverErr error
				discoveredCoordinators, discoveredExecutors, discoverErr = kubernetes.DiscoverPods(k8sContext, namespace, detectLabelSelector, kubeconfigPath)
				if discoverErr != nil {
					simplelog.Warningf("Pod discovery failed: %v. Continuing without node selection.", discoverErr)
					discoveredCoordinators = nil
					discoveredExecutors = nil
				}
			}).
			Run()
	case "ssh":
		// SSH mode: use manually-entered nodes.
		for _, h := range strings.Split(coordinatorStr, ",") {
			if h = strings.TrimSpace(h); h != "" {
				discoveredCoordinators = append(discoveredCoordinators, h)
			}
		}
		for _, h := range strings.Split(executorsStr, ",") {
			if h = strings.TrimSpace(h); h != "" {
				discoveredExecutors = append(discoveredExecutors, h)
			}
		}
	}

	cfg, err := configui.RunDiagnosisConfigScreen(detected, versions.Version, discoveredCoordinators, discoveredExecutors)
	if err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			fmt.Println("\nCancelled")
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "configuration UI failed: %v\n", err)
		os.Exit(1)
	}
	if cfg.Cancelled {
		fmt.Println("\nCancelled")
		os.Exit(0)
	}
	simplelog.Infof("generated cli command: %v", cfg.GeneratedCommand)

	// Map selected nodes back to global variables.
	// K8s-family transports: map the TUI's node selection to nodesFlag so the
	// collection layer can filter pods. SSH/local transports don't show a node-
	// selection page (see configui WithHideFunc), so coordinatorStr/executorsStr
	// are already set from flags — nothing to do there.
	if transportCmd == "k8s" || transportCmd == "local-k8s" {
		totalDiscovered := len(discoveredCoordinators) + len(discoveredExecutors)
		allSelected := append(cfg.SelectedCoordinators, cfg.SelectedExecutors...)
		if totalDiscovered > 0 && len(allSelected) == 0 {
			return fmt.Errorf("no nodes selected — cancel via the 'Proceed with collection?' confirm instead")
		}
		if len(allSelected) > 0 && len(allSelected) < totalDiscovered {
			nodesFlag = strings.Join(allSelected, ",")
		}
	}
	// Apply config to global variables
	coordinatorLogDir = cfg.CoordinatorLogDir
	executorLogDir = cfg.ExecutorLogDir
	// For local transport, the TUI shows a single "Log dir" field stored in CoordinatorLogDir
	if transportCmd == "local" || transportCmd == "local-k8s" {
		executorLogDir = cfg.CoordinatorLogDir
	}
	dremioConfDir = cfg.DremioConfDir
	dremioRocksDBDir = cfg.DremioRocksDBDir
	allowInsecureSSL = cfg.AllowInsecureSSL
	daysFlag = cfg.Days
	startDate = cfg.DateStart
	collectJFR = cfg.CollectJFR
	collectJStack = cfg.CollectJStack
	collectTop = cfg.CollectTop
	collectHeapDump = cfg.CollectHeapDump
	collectAsyncProfiler = cfg.CollectAsyncProfiler
	diagTimeSeconds = cfg.DiagTimeSeconds
	collectServerLogs = cfg.CollectServerLogs
	collectGCLogs = cfg.CollectGCLogs
	collectTrackerJSON = cfg.CollectTrackerJSON
	collectVacuumLog = cfg.CollectVacuumLog
	collectAcceleration = cfg.CollectAcceleration
	collectAccessLog = cfg.CollectAccess
	collectHiveDeprecated = cfg.CollectHiveDeprecated
	collectMetaRefresh = cfg.CollectMetaRefresh
	collectHSErrFiles = cfg.CollectHSErr
	collectQueriesPerf = cfg.CollectQueriesPerf
	dremioEndpoint = cfg.DremioEndpoint
	if cfg.PATToken != "" {
		cliAuthToken = cfg.PATToken
	}
	collectWLM = cfg.CollectWLM
	collectKVStoreReport = cfg.CollectKVStore
	collectProblematicProfiles = cfg.CollectProblematicProfiles
	collectContainerLogs = cfg.CollectContainerLogs
	systemTables = strings.Join(cfg.SystemTables, ",")

	return nil
}

// runLocalPathDiscovery detects Dremio paths on the local machine by inspecting the
// running Dremio process. Returns nil if no Dremio process is found (config screen
// will use defaults).
func runLocalPathDiscovery() *configui.DetectedPaths {
	var result *configui.DetectedPaths

	_ = spinner.New().
		Title("Detecting local Dremio paths ...").
		Action(func() {
			// Find the Dremio PID on this host
			pidCmd := exec.Command("sh", "-c", "pgrep -f 'dremio.*java' | head -1")
			pidOut, err := pidCmd.Output()
			if err != nil {
				simplelog.Warningf("local path autodetection: could not find Dremio PID: %v", err)
				return
			}
			pid := strings.TrimSpace(string(pidOut))
			if pid == "" {
				simplelog.Warningf("local path autodetection: no Dremio process found")
				return
			}

			// Read process environment and JVM flags
			psCmd := exec.Command("sh", "-c", fmt.Sprintf(
				"ps eww %s 2>/dev/null || cat /proc/%s/cmdline /proc/%s/environ 2>/dev/null | tr '\\0' ' '",
				pid, pid, pid))
			psOut, err := psCmd.Output()
			if err != nil {
				simplelog.Warningf("local path autodetection: could not read process info: %v", err)
				return
			}
			psStr := string(psOut)

			// Parse paths
			coordinatorLog := extractEnvValue(psStr, "-Ddremio.log.path=")
			if coordinatorLog == "" {
				coordinatorLog = extractEnvValue(psStr, "DREMIO_LOG_DIR=")
			}
			confDir := extractEnvValue(psStr, "DREMIO_CONF_DIR=")

			if coordinatorLog == "" && confDir == "" {
				simplelog.Warningf("local path autodetection: no Dremio paths found in process info")
				return
			}

			// Detect endpoint from sso.json if available
			var detectedEndpoint string
			if confDir != "" {
				ssoCmd := exec.Command("sh", "-c", fmt.Sprintf(
					`grep -o '"redirectUrl"[[:space:]]*:[[:space:]]*"[^"]*"' %s/sso.json 2>/dev/null | head -1 | sed 's/.*"redirectUrl"[[:space:]]*:[[:space:]]*"//;s/".*//'`,
					confDir))
				if ssoOut, err := ssoCmd.Output(); err == nil {
					redirectURL := strings.TrimSpace(string(ssoOut))
					if redirectURL != "" {
						if idx := strings.Index(redirectURL, "://"); idx > 0 {
							rest := redirectURL[idx+3:]
							if slashIdx := strings.Index(rest, "/"); slashIdx > 0 {
								detectedEndpoint = redirectURL[:idx+3+slashIdx]
							} else {
								detectedEndpoint = redirectURL
							}
						}
					}
				}
			}
			if detectedEndpoint == "" {
				detectedEndpoint = "http://localhost:9047"
			}

			// Detect RocksDB dir from dremio.conf
			var detectedRocksDBDir string
			if confDir != "" {
				catCmd := exec.Command("cat", confDir+"/dremio.conf")
				if catOut, err := catCmd.Output(); err == nil {
					localDremioHome := dremioHome
					if localDremioHome == "" {
						localDremioHome = "/opt/dremio"
					}
					if hc, err := conf.NewDremioHOCONConfigFromString(string(catOut), localDremioHome); err == nil {
						detectedRocksDBDir = hc.GetRocksDBPath(localDremioHome)
						simplelog.Infof("autodetected local RocksDB dir from dremio.conf: %s", detectedRocksDBDir)
					} else {
						simplelog.Warningf("local path autodetection: could not parse dremio.conf for RocksDB path: %v", err)
					}
				}
			}

			result = &configui.DetectedPaths{
				CoordinatorLogDir: coordinatorLog,
				ConfDir:           confDir,
				RocksDBDir:        detectedRocksDBDir,
				Endpoint:          detectedEndpoint,
				Detected:          true,
				Hostname:          "localhost",
				Transport:         transportCmd,
				DremioHome:        dremioHome,
			}

			simplelog.Infof("autodetected local paths: log=%s conf=%s endpoint=%s", coordinatorLog, confDir, detectedEndpoint)
		}).
		Run()

	return result
}

// runPathDiscovery probes one target node to autodetect Dremio paths before showing the config screen.
// For K8s mode: runs kubectl exec on dremio-master-0.
// For SSH mode: runs ssh on the first coordinator.
// Returns nil if discovery fails (config screen will use defaults).
// runPathDiscovery probes a target node to detect Dremio paths before the config screen.
// It runs `ps eww` on the target to parse -Ddremio.log.path= and DREMIO_CONF_DIR=
// from the Dremio process — the same approach used by the main branch.
// No DDC binary needs to be pre-installed on the target.
func runPathDiscovery(ns, coordinator, sshUsr, sshKey, k8sCtx, kubeconfig string) *configui.DetectedPaths {
	var targetHost string

	// makeRemoteCmd builds a function that runs a command on the given remote host.
	makeRemoteCmd := func(host string) func(args ...string) *exec.Cmd {
		if ns != "" {
			return func(args ...string) *exec.Cmd {
				var fullArgs []string
				if kubeconfig != "" {
					fullArgs = append(fullArgs, "--kubeconfig", kubeconfig)
				}
				if k8sCtx != "" {
					fullArgs = append(fullArgs, "--context", k8sCtx)
				}
				fullArgs = append(fullArgs, "exec", host, "-n", ns, "--")
				// Wrap in sh -c so pipes/redirects are interpreted by a shell.
				// kubectl exec does not invoke a shell by default.
				fullArgs = append(fullArgs, "sh", "-c", strings.Join(args, " "))
				return exec.Command("kubectl", fullArgs...)
			}
		}
		return func(args ...string) *exec.Cmd {
			sshCmdArgs := []string{"-o", "ConnectTimeout=5", "-o", "StrictHostKeyChecking=no", "-o", "BatchMode=yes"}
			if sshKey != "" {
				sshCmdArgs = append(sshCmdArgs, "-i", sshKey)
			}
			remote := host
			if sshUsr != "" {
				remote = sshUsr + "@" + host
			}
			sshCmdArgs = append(sshCmdArgs, remote)
			sshCmdArgs = append(sshCmdArgs, strings.Join(args, " "))
			return exec.Command("ssh", sshCmdArgs...)
		}
	}

	if ns != "" {
		targetHost = "dremio-master-0"
	} else if coordinator != "" {
		hosts := strings.Split(coordinator, ",")
		targetHost = strings.TrimSpace(hosts[0])
		if targetHost == "" {
			return nil
		}
	} else {
		return nil
	}

	remoteCmd := makeRemoteCmd(targetHost)

	// probeLogDir runs the PID/ps detection on a remote host and returns its log dir.
	probeLogDir := func(host string, cmdFn func(args ...string) *exec.Cmd) string {
		pidCmd := cmdFn("pgrep -f 'dremio.*java' | head -1")
		pidOut, err := pidCmd.Output()
		if err != nil {
			simplelog.Warningf("path autodetection: could not find Dremio PID on %s: %v", host, err)
			return ""
		}
		pid := strings.TrimSpace(string(pidOut))
		if pid == "" {
			simplelog.Warningf("path autodetection: no Dremio process found on %s", host)
			return ""
		}
		psCmd := cmdFn(fmt.Sprintf("ps eww %s 2>/dev/null || cat /proc/%s/cmdline /proc/%s/environ 2>/dev/null | tr '\\0' ' '", pid, pid, pid))
		psOut, err := psCmd.Output()
		if err != nil {
			simplelog.Warningf("path autodetection: could not read process info on %s: %v", host, err)
			return ""
		}
		psStr := string(psOut)
		logDir := extractEnvValue(psStr, "-Ddremio.log.path=")
		if logDir == "" {
			logDir = extractEnvValue(psStr, "DREMIO_LOG_DIR=")
		}
		return logDir
	}

	var result *configui.DetectedPaths

	_ = spinner.New().
		Title("Detecting Dremio paths ...").
		Action(func() {
			// Find the Dremio PID on coordinator
			pidCmd := remoteCmd("pgrep -f 'dremio.*java' | head -1")
			pidOut, err := pidCmd.Output()
			if err != nil {
				simplelog.Warningf("path autodetection: could not find Dremio PID on %s: %v", targetHost, err)
				return
			}
			pid := strings.TrimSpace(string(pidOut))
			if pid == "" {
				simplelog.Warningf("path autodetection: no Dremio process found on %s", targetHost)
				return
			}

			// Read the process environment and JVM flags
			psCmd := remoteCmd(fmt.Sprintf("ps eww %s 2>/dev/null || cat /proc/%s/cmdline /proc/%s/environ 2>/dev/null | tr '\\0' ' '", pid, pid, pid))
			psOut, err := psCmd.Output()
			if err != nil {
				simplelog.Warningf("path autodetection: could not read process info on %s: %v", targetHost, err)
				return
			}
			psStr := string(psOut)

			// Parse paths from the process output (same keys as main branch)
			coordinatorLog := extractEnvValue(psStr, "-Ddremio.log.path=")
			if coordinatorLog == "" {
				coordinatorLog = extractEnvValue(psStr, "DREMIO_LOG_DIR=")
			}
			confDir := extractEnvValue(psStr, "DREMIO_CONF_DIR=")

			if coordinatorLog == "" && confDir == "" {
				simplelog.Warningf("path autodetection: no Dremio paths found in process info on %s", targetHost)
				return
			}

			// Detect endpoint: try sso.json redirectUrl from dremio.conf, then K8s service env
			var detectedEndpoint string
			if confDir != "" {
				ssoCmd := remoteCmd(fmt.Sprintf(
					`grep -o '"redirectUrl"[[:space:]]*:[[:space:]]*"[^"]*"' %s/sso.json 2>/dev/null | head -1 | sed 's/.*"redirectUrl"[[:space:]]*:[[:space:]]*"//;s/".*//'`,
					confDir))
				if ssoOut, err := ssoCmd.Output(); err == nil {
					redirectURL := strings.TrimSpace(string(ssoOut))
					if redirectURL != "" {
						if idx := strings.Index(redirectURL, "://"); idx > 0 {
							rest := redirectURL[idx+3:]
							if slashIdx := strings.Index(rest, "/"); slashIdx > 0 {
								detectedEndpoint = redirectURL[:idx+3+slashIdx]
							} else {
								detectedEndpoint = redirectURL
							}
						}
					}
				}
			}
			if detectedEndpoint == "" {
				webAddr := extractEnvValue(psStr, "DREMIO_CLIENT_PORT_9047_TCP_ADDR=")
				webPort := extractEnvValue(psStr, "DREMIO_CLIENT_PORT_9047_TCP_PORT=")
				if webAddr != "" {
					if webPort == "" {
						webPort = "9047"
					}
					detectedEndpoint = fmt.Sprintf("http://%s:%s", webAddr, webPort)
				}
			}

			// Autodetect RocksDB dir from dremio.conf (graceful — failure just means no override)
			var detectedRocksDBDir string
			if confDir != "" {
				catCmd := remoteCmd("cat", confDir+"/dremio.conf")
				if catOut, err := catCmd.Output(); err == nil {
					dremioHome := "/opt/dremio"
					if hc, err := conf.NewDremioHOCONConfigFromString(string(catOut), dremioHome); err == nil {
						detectedRocksDBDir = hc.GetRocksDBPath(dremioHome)
						simplelog.Infof("autodetected RocksDB dir from dremio.conf: %s", detectedRocksDBDir)
					} else {
						simplelog.Warningf("path autodetection: could not parse dremio.conf for RocksDB path: %v", err)
					}
				} else {
					simplelog.Warningf("path autodetection: could not read dremio.conf from %s: %v", targetHost, err)
				}
			}

			result = &configui.DetectedPaths{
				CoordinatorLogDir: coordinatorLog,
				ConfDir:           confDir,
				RocksDBDir:        detectedRocksDBDir,
				Endpoint:          detectedEndpoint,
				Detected:          true,
				Hostname:          targetHost,
				Namespace:         namespace,
				Coordinator:       coordinatorStr,
				SSHUser:           sshUser,
			}

			// Probe executor node for its log dir
			var executorHost string
			if ns != "" {
				// K8s: find first executor pod
				var listArgs []string
				if kubeconfig != "" {
					listArgs = append(listArgs, "--kubeconfig", kubeconfig)
				}
				if k8sCtx != "" {
					listArgs = append(listArgs, "--context", k8sCtx)
				}
				listArgs = append(listArgs, "get", "pods", "-n", ns, "-o", "name")
				listCmd := exec.Command("kubectl", listArgs...)
				if listOut, err := listCmd.Output(); err == nil {
					executorHost = findExecutorPod(string(listOut))
				}
			} else if executorsStr != "" {
				// SSH: use the first executor from the --executors flag
				execs := strings.Split(executorsStr, ",")
				if h := strings.TrimSpace(execs[0]); h != "" {
					executorHost = h
				}
			}

			if executorHost != "" {
				executorLogDir := probeLogDir(executorHost, makeRemoteCmd(executorHost))
				if executorLogDir != "" {
					result.ExecutorLogDir = executorLogDir
					result.ExecutorHostname = executorHost
					simplelog.Infof("autodetected executor log dir from %s: %s", executorHost, executorLogDir)
				} else {
					simplelog.Warningf("path autodetection: could not detect executor log dir from %s", executorHost)
				}
			}
		}).
		Run()

	if result != nil {
		simplelog.Infof("autodetected from %s: log=%s conf=%s endpoint=%s", targetHost, result.CoordinatorLogDir, result.ConfDir, result.Endpoint)
	}
	return result
}

// findExecutorPod returns the first pod name with a "dremio-executor" prefix
// from kubectl "get pods -o name" output. Returns "" if none found.
func findExecutorPod(podListOutput string) string {
	for _, line := range strings.Split(podListOutput, "\n") {
		name := strings.TrimSpace(strings.TrimPrefix(line, "pod/"))
		if strings.HasPrefix(name, "dremio-executor") {
			return name
		}
	}
	return ""
}

// extractEnvValue extracts a value from a process string for a given key.
// Handles both "KEY=value " and "KEY=value\0" formats.
func extractEnvValue(ps, key string) string {
	// LastIndex matches JVM semantics: when -Dfoo= appears multiple times on
	// the java command line, the JVM resolves the LAST one. Dremio launcher
	// scripts that derive -Ddremio.log.path= from DREMIO_LOG_DIR and then let
	// DREMIO_JAVA_SERVER_EXTRA_OPTS override it produce exactly this case.
	idx := strings.LastIndex(ps, key)
	if idx < 0 {
		return ""
	}
	rest := ps[idx+len(key):]
	// Value ends at space, null byte, or end of string
	end := strings.IndexAny(rest, " \t\n\x00")
	value := rest
	if end >= 0 {
		value = rest[:end]
	}
	return strings.TrimRight(strings.TrimSpace(value), ",")
}

// promptKubeconfigPath shows a TUI input step asking the user for a
// kubeconfig file path, validates it inline (file/parse/contexts), and then
// runs a connectivity probe in a spinner. On connectivity failure the form
// is re-shown up to 2 retries (3 attempts total). On success, the global
// kubeconfigPath is set. Returns a non-nil error only on user cancel or
// final retry exhaustion.
func promptKubeconfigPath() error {
	placeholder := "/home/you/.kube/config"
	if runtime.GOOS == "windows" {
		placeholder = `C:\Users\you\.kube\config`
	}

	const maxAttempts = 3
	var entered string
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 && lastErr != nil {
			fmt.Println()
			fmt.Println("─────────────────────────────────────────────────────────────")
			fmt.Printf("unable to reach cluster: %v\n", lastErr)
			fmt.Println("─────────────────────────────────────────────────────────────")
			fmt.Println()
		}

		form := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Kubeconfig file path").
					Description("No Kubernetes config auto-detected. Enter the path to your kubeconfig file.").
					Placeholder(placeholder).
					Value(&entered).
					Validate(configui.ValidateKubeconfigPath),
			),
		).WithTheme(huh.ThemeCharm())
		if err := form.Run(); err != nil {
			return fmt.Errorf("kubeconfig input cancelled: %w", err)
		}

		// Inline validate has passed (file/parse/contexts). Now probe connectivity.
		candidate := dirs.ExpandTilde(entered)
		var probeErr error
		_ = spinner.New().
			Title("Verifying cluster connectivity...").
			Action(func() {
				probeErr = kubernetes.VerifyConnectivity(candidate, "")
			}).
			Run()
		if probeErr == nil {
			kubeconfigPath = candidate
			return nil
		}
		simplelog.Warningf("kubeconfig connectivity probe failed (attempt %d/%d): %v", attempt+1, maxAttempts, probeErr)
		lastErr = probeErr
	}
	return fmt.Errorf("could not reach a Kubernetes cluster with any of the supplied kubeconfig paths after %d attempts", maxAttempts)
}
