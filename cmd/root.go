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
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/awselogs"
	local "github.com/dremio/dremio-diagnostic-collector/v3/cmd/local"
	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/root/collection"
	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/root/fallback"
	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/root/helpers"
	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/root/kubectl"
	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/root/kubernetes"
	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/root/ssh"
	version "github.com/dremio/dremio-diagnostic-collector/v3/cmd/version"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/collects"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/consoleprint"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/dirs"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/masking"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/shutdown"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/simplelog"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/validation"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/versions"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	_ "k8s.io/client-go/plugin/pkg/client/auth" // in case one needs auth plugins
)

// var scaleoutCoordinatorContainer string
var (
	coordinatorStr string
	executorsStr   string
	labelSelector  string
	sshKeyLoc      string
	sshUser        string
	transferDir    string
	ddcYamlLoc     string
)

var outputLoc string

var (
	sudoUser                string
	namespace               string
	k8sContext              string
	disableFreeSpaceCheck   bool
	disableKubeCtl          bool
	minFreeSpaceGB          uint64
	disablePrompt           bool
	detectNamespace         bool
	collectionMode          string
	cliAuthToken            string
	pid                     string
	transferThreads         int
	manualPATPrompt         bool
	noLogDir                bool
	archiveSizeLimitMB      int
	disableArchiveSplitting bool
)

// var isEmbeddedK8s bool
// var isEmbeddedSSH bool

// RootCmd represents the base command when called without any subcommands
var RootCmd = &cobra.Command{
	Use:   "ddc",
	Short: versions.GetCLIVersion() + " ddc connects via to dremio servers collects logs into an archive",
	Long: versions.GetCLIVersion() + ` ddc connects via ssh or kubectl and collects a series of logs and files for dremio, then puts those collected files in an archive
examples:

for a ui prompt just run:
	ddc 

for ssh based communication to VMs or Bare metal hardware:

	ddc --coordinator 10.0.0.19 --executors 10.0.0.20,10.0.0.21,10.0.0.22 --ssh-user myuser --ssh-key ~/.ssh/mykey --sudo-user dremio 

for kubernetes deployments:

	# run against a specific namespace and retrieve 2 days of logs
	ddc --namespace mynamespace

	# run against a specific namespace with a standard collection (includes jfr, top and 30 days of queries.json logs)
	ddc --namespace mynamespace	--collect standard

	# run against a specific namespace with a Health Check (runs 2 threads and includes everything in a standard collection plus collect 25,000 job profiles, system tables, kv reports and Work Load Manager (WLM) reports)
	ddc --namespace mynamespace	--collect health-check
`,
	Run: func(_ *cobra.Command, _ []string) {
	},
}

// validateUnknownFlags checks if there are any unknown flags in the arguments
func validateUnknownFlags(args []string) error {
	// Create a temporary flag set with all the defined flags
	tempFlagSet := pflag.NewFlagSet("temp", pflag.ContinueOnError)
	RootCmd.Flags().VisitAll(func(flag *pflag.Flag) {
		tempFlagSet.AddFlag(flag)
	})

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
	ticker := time.NewTicker(time.Second * 2)
	quit := make(chan struct{})
	consoleprint.PrintState()
	go func() {
		for {
			select {
			case <-ticker.C:
				// Action to be performed on each tick
				consoleprint.PrintState()
			case <-quit:
				ticker.Stop()
				return
			}
		}
	}()

	return func() {
		close(quit)
	}
}

func RemoteCollect(collectionArgs collection.Args, sshArgs ssh.Args, kubeArgs kubernetes.KubeArgs, fallbackEnabled bool, hook shutdown.Hook) error {
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
	if fallbackEnabled {
		simplelog.Info("using fallback based collection")
		collectorStrategy = fallback.NewFallback(hook)
		consoleprint.UpdateRuntime(
			versions.GetCLIVersion(),
			simplelog.GetLogLoc(),
			0,
			0,
		)
	} else if kubeArgs.Namespace != "" {
		simplelog.Info("using Kubernetes api based collection")
		consoleprint.UpdateCollectionArgs(fmt.Sprintf("namespace: '%v', label selector: '%v'", kubeArgs.Namespace, kubeArgs.LabelSelector))
		collectorStrategy, err = kubernetes.NewK8sAPI(kubeArgs, hook)
		if err != nil {
			return err
		}
		if !disableKubeCtl {
			potentialStrategy, err := kubectl.NewKubectlK8sActions(hook, kubeArgs)
			if err != nil {
				simplelog.Warningf("kubectl not available failling back to kubeapi: %v", err)
			} else {
				collectorStrategy = potentialStrategy
			}
		}

		consoleprint.UpdateRuntime(
			versions.GetCLIVersion(),
			simplelog.GetLogLoc(),
			0,
			0,
		)

		clusterCollect = func() {
			clientSet, _, err := kubernetes.GetClientset(k8sContext)
			if err != nil {
				simplelog.Errorf("when getting Kubernetes info, the following error was returned: %v", err)
				return
			}
			err = collection.ClusterK8sExecute(hook, kubeArgs.Namespace, clientSet, cs, collectionArgs.DDCfs)
			if err != nil {
				simplelog.Errorf("when getting Kubernetes info, the following error was returned: %v", err)
			}
			err = collection.GetClusterLogs(hook, kubeArgs.Namespace, clientSet, cs, collectionArgs.DDCfs)
			if err != nil {
				simplelog.Errorf("when getting container logs, the following error was returned: %v", err)
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
		return err
	}
	return nil
}

func ValidateAndReadYaml(ddcYaml, collectionMode string) (map[string]interface{}, error) {
	emptyOverrides := make(map[string]string)
	confData, err := conf.ParseConfig(ddcYaml, emptyOverrides)
	if err != nil {
		return make(map[string]interface{}), err
	}
	conf.SetViperDefaults(confData, "", 0, collectionMode)
	simplelog.Infof("parsed configuration for %v follows", ddcYaml)
	for k, v := range confData {
		if k == conf.KeyDremioPatToken && v != "" {
			simplelog.Infof("yaml key '%v':'REDACTED'", k)
		} else {
			simplelog.Infof("yaml key '%v':'%v'", k, v)
		}
	}

	// set defaults so we get an accurate reading of if these will be enabled or not
	conf.SetViperDefaults(confData, "", 0, collects.StandardCollection)
	return confData, nil
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
	// default cmd if no cmd is given
	if err == nil && (foundCmd.Use == RootCmd.Use) && !errors.Is(foundCmd.Flags().Parse(args[1:]), pflag.ErrHelp) {
		// Check for unknown flags when using the root command directly
		if err := validateUnknownFlags(args[1:]); err != nil {
			return err
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
		if disablePrompt {
			consoleprint.EnableStatusOutput()
		}
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

		skipPromptUI := enableFallback || disablePrompt || detectNamespace || (namespace != "") || sshUser != ""
		if !skipPromptUI {
			// fire configuration prompt
			prompt := promptui.Select{
				Label: "select transport for file transfers",
				Items: []string{"kubernetes", "ssh"},
			}
			_, transport, err := prompt.Run()
			if err != nil {
				return fmt.Errorf("prompt failed %w", err)
			}
			if transport == "ssh" {
				// ssh user
				prompt := promptui.Prompt{
					Label: "ssh user ",
				}
				var err error
				sshUser, err = prompt.Run()
				if err != nil {
					return err
				}
				// sudo user
				prompt = promptui.Prompt{
					Label: "sudo user (runs on remote servers as this user)",
				}
				sudoUser, err = prompt.Run()
				if err != nil {
					return err
				}
				home, err := os.UserHomeDir()
				if err != nil {
					return err
				}
				sshDir := filepath.Join(home, ".ssh")
				entries, err := os.ReadDir(sshDir)
				if err != nil {
					return err
				}
				var sshKeys []string
				for _, e := range entries {
					if strings.HasPrefix(e.Name(), "id_") && !strings.HasSuffix(e.Name(), ".pub") {
						sshKeys = append(sshKeys, filepath.Join(sshDir, e.Name()))
					}
				}
				selectPrompt := promptui.Select{
					Label: "ssh key location (from $HOME/.ssh directory)",
					Items: sshKeys,
				}
				_, sshKeyLoc, err = selectPrompt.Run()
				if err != nil {
					return err
				}

				prompt = promptui.Prompt{
					Label: "coordinator list ex (192.168.1.10,192.168.1.12)",
				}
				coordinatorStr, err = prompt.Run()
				if err != nil {
					return err
				}

				prompt = promptui.Prompt{
					Label: "executor list ex (192.168.1.10,192.168.1.12)",
				}
				executorsStr, err = prompt.Run()
				if err != nil {
					return err
				}
			} else {
				clustersToList, err := kubernetes.GetClusters()
				if err != nil {
					return err
				}
				prompt := promptui.Select{
					Label: "The following k8s namespaces have dremio clusters. Select the one you want to collect from",
					Items: clustersToList,
				}
				_, namespace, err = prompt.Run()
				if err != nil {
					return fmt.Errorf("prompt failed: %w", err)
				}
			}
			prompt = promptui.Select{
				Label: "Collection Type: light (2 days logs), standard (7 days logs + 30 days queries.json), standard+jstack (standard w jstack), health-check (needs PAT), waf (needs PAT)",
				Items: []string{"light", "standard", "standard+jstack", "health-check", "waf"},
			}
			_, collectionMode, err = prompt.Run()
			if err != nil {
				return fmt.Errorf("prompt failed: %w", err)
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
		simplelog.Infof("cli command: %v", strings.Join(args, " "))
		confData, err := ValidateAndReadYaml(ddcYamlLoc, collectionMode)
		if err != nil {
			return fmt.Errorf("CRITICAL ERROR: unable to parse %v: %w", ddcYamlLoc, err)
		}
		if !disableFreeSpaceCheck {
			abs, err := filepath.Abs(outputLoc)
			if err != nil {
				return err
			}
			outputFolder := filepath.Dir(abs)
			nonDefaultFreeSpace := minFreeSpaceGB > 0
			if err := dirs.CheckFreeSpace(outputFolder, getFreeSpace(minFreeSpaceGB, collectionMode)); err != nil {
				return dirs.FormatFreeSpaceError(nonDefaultFreeSpace, err, collectionMode, collects.QuickCollection)
			}
		}

		dremioPAT := confData[conf.KeyDremioPatToken].(string)
		if cliAuthToken == "" {
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

		if manualPATPrompt || (collectionMode == collects.HealthCheckCollection || collectionMode == collects.WAFCollection) && dremioPAT == "" {
			pat, err := masking.PromptForPAT()
			if err != nil {
				return fmt.Errorf("unable to get PAT: %w", err)
			}
			dremioPAT = pat
		}
		patSet := dremioPAT != ""
		if detectNamespace {
			validateK8s := func(namespace string) {
				rightsTester, err := kubernetes.NewK8sAPI(kubernetes.KubeArgs{Namespace: namespace}, hook)
				if err != nil {
					msg := fmt.Sprintf("unable to unable to initialize connection to kubernetes falling back to local-collect: %v", err)
					consoleprint.WarningPrint(msg)
					simplelog.Error(msg)
					fallBackToLocal()
					return
				}
				testCoordinators, err := rightsTester.GetCoordinators()
				if err != nil {
					msg := fmt.Sprintf("not sufficient rights to collect the coordinator diagnostic information via the Kubernetes API falling back to local-collect: %v", err)
					consoleprint.WarningPrint(msg)
					simplelog.Error(msg)
					fallBackToLocal()
					return
				}
				for _, c := range testCoordinators {
					_, err := rightsTester.HostExecute(false, c, "ls")
					if err != nil {
						msg := fmt.Sprintf("not sufficient rights to collect the executor diagnostic information via the Kubernetes API falling back to local-collect: %v", err)
						consoleprint.WarningPrint(msg)
						simplelog.Error(msg)
						fallBackToLocal()
						return
					}
				}
			}
			b, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
			if err != nil {
				msg := fmt.Sprintf("did not find the namespace so falling back to local-collect: %v", err)
				consoleprint.WarningPrint(msg)
				simplelog.Error(msg)
				fallBackToLocal()
			} else {
				namespace = string(b)
				validateK8s(namespace)
			}
		}
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
						if k == conf.KeyCollectWLM || k == conf.KeyCollectKVStoreReport || k == conf.KeyCollectSystemTablesExport {
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
		if !disablePrompt {
			stop := startTicker()
			hook.AddUIStop(stop)
		}
		collectionArgs := collection.Args{
			OutputLoc:               filepath.Clean(outputLoc),
			DDCfs:                   helpers.NewRealFileSystem(),
			DremioPAT:               dremioPAT,
			TransferDir:             transferDir,
			DDCYamlLoc:              ddcYamlLoc,
			Enabled:                 enabled,
			Disabled:                disabled,
			DisableFreeSpaceCheck:   disableFreeSpaceCheck,
			MinFreeSpaceGB:          minFreeSpaceGB,
			CollectionMode:          collectionMode,
			TransferThreads:         transferThreads,
			NoLogDir:                noLogDir,
			ArchiveSizeLimitMB:      archiveSizeLimitMB,
			DisableArchiveSplitting: disableArchiveSplitting,
		}
		sshArgs := ssh.Args{
			SSHKeyLoc:      sshKeyLoc,
			SSHUser:        sshUser,
			SudoUser:       sudoUser,
			ExecutorStr:    executorsStr,
			CoordinatorStr: coordinatorStr,
		}
		kubeArgs := kubernetes.KubeArgs{
			Namespace:     namespace,
			LabelSelector: labelSelector,
			K8SContext:    k8sContext,
		}
		if err := RemoteCollect(collectionArgs, sshArgs, kubeArgs, enableFallback, hook); err != nil {
			consoleprint.UpdateResult(err.Error())
		}
		// we put the error in result so just return nil
		if !disablePrompt {
			consoleprint.PrintState()
		}
		return nil
	}
	if err := RootCmd.Execute(); err != nil {
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
		return "", unableToGetHomeDir{}
	}
	return filepath.Join(home, ".ssh", "id_rsa"), nil
}

func init() {
	// command line flags

	// ssh flags
	RootCmd.Flags().StringVarP(&coordinatorStr, "coordinator", "c", "", "SSH ONLY: set a list of ip addresses separated by commas")
	RootCmd.Flags().StringVarP(&executorsStr, "executors", "e", "", "SSH ONLY: set a list of ip addresses separated by commas")
	RootCmd.Flags().StringVarP(&sshKeyLoc, "ssh-key", "s", "", "SSH ONLY: of ssh key to use to login")
	RootCmd.Flags().StringVarP(&sshUser, "ssh-user", "u", "", "SSH ONLY: user to use during ssh operations to login")
	RootCmd.Flags().StringVarP(&sudoUser, "sudo-user", "b", "", "SSH ONLY: if any diagnostics commands need a sudo user (i.e. for jcmd)")

	// k8s flags
	RootCmd.Flags().StringVarP(&namespace, "namespace", "n", "", "K8S ONLY: namespace to use for kubernetes pods")
	RootCmd.Flags().StringVarP(&k8sContext, "context", "x", "", "K8S ONLY: context to use for kubernetes pods")
	RootCmd.Flags().StringVarP(&labelSelector, "label-selector", "l", "role=dremio-cluster-pod", "K8S ONLY: select which pods to collect: follows kubernetes label syntax see https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#label-selectors")

	// shared flags
	RootCmd.Flags().StringVar(&collectionMode, "collect", "light", "type of collection: 'light'- 2 days of logs (no top or jfr). 'standard' - includes jfr, top, 7 days of logs and 30 days of queries.json logs. 'standard+jstack' - all of 'standard' plus jstack. 'health-check' - all of 'standard' + WLM, KV Store Report, 10,000 Job Profiles, 'waf' same as light, then 3 days of logs and queries.json, with 25,000 job profiles")
	RootCmd.Flags().BoolVar(&disableFreeSpaceCheck, conf.KeyDisableFreeSpaceCheck, false, "disables the free space check for the --transfer-dir")
	RootCmd.Flags().BoolVar(&disablePrompt, "disable-prompt", false, "disables the prompt ui")
	RootCmd.Flags().BoolVarP(&disableKubeCtl, "disable-kubectl", "d", false, "uses the embedded k8s api client and skips the use of kubectl for transfers and copying")
	RootCmd.Flags().BoolVarP(&manualPATPrompt, "pat-prompt", "t", false, "prompt for the pat, which will enable collection of kv report, system tables, job profiles and the workload manager report")
	RootCmd.Flags().BoolVar(&noLogDir, conf.KeyNoLogDir, false, "when enabled then the process will not fail if the log directory is invalid. This is useful if one just wants to collect kubernetes log output and diagnostics information")
	RootCmd.Flags().BoolVar(&detectNamespace, "detect-namespace", false, "detect namespace feature to pass the namespace automatically")
	RootCmd.Flags().StringVar(&pid, "pid", "", "write a pid")
	if err := RootCmd.Flags().MarkHidden("pid"); err != nil {
		fmt.Printf("unable to mark flag hidden critical error %v", err)
		os.Exit(1)
	}
	RootCmd.Flags().IntVar(&transferThreads, "transfer-threads", 2, "number of threads to transfer tarballs")
	RootCmd.Flags().Uint64Var(&minFreeSpaceGB, "min-free-space-gb", 0, "min free space needed in GB for the process to run (default 5GB light collect, 25GB for standard and standard+jstack, 40GB for health-check and WAF)")
	RootCmd.Flags().StringVar(&transferDir, "transfer-dir", fmt.Sprintf("/tmp/ddc-%v", time.Now().Format("20060102150405")), "directory to use for communication between the local-collect command and this one")
	RootCmd.Flags().StringVar(&outputLoc, "output-file", "diag.tgz", "name and location of diagnostic tarball")
	RootCmd.Flags().IntVarP(&archiveSizeLimitMB, conf.KeyArchiveSizeLimitMB, "z", 256, "maximum size in MB for each archive file before splitting into multiple files")
	RootCmd.Flags().BoolVarP(&disableArchiveSplitting, conf.KeyDisableArchiveSplitting, "a", false, "disable splitting archives when they exceed the size limit (when enabled, creates single archive regardless of size)")

	execLoc, err := os.Executable()
	if err != nil {
		fmt.Printf("unable to find ddc, critical error %v", err)
		os.Exit(1)
	}
	execLocDir := filepath.Dir(execLoc)
	RootCmd.Flags().StringVar(&ddcYamlLoc, "ddc-yaml", filepath.Join(execLocDir, "ddc.yaml"), "location of ddc.yaml that will be transferred to remote nodes for collection configuration")

	// init
	RootCmd.AddCommand(local.LocalCollectCmd)
	RootCmd.AddCommand(version.VersionCmd)
	RootCmd.AddCommand(awselogs.AWSELogsCmd)
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

func fallBackToLocal() {
	enableFallback = true
}

var enableFallback bool

func getFreeSpace(b uint64, m string) uint64 {
	if b == 0 {
		switch m {
		case collects.QuickCollection:
			b = 5
		case collects.StandardCollection:
			b = 25
		case collects.StandardPlusJSTACKCollection:
			b = 25
		case collects.HealthCheckCollection:
			b = 40
		case collects.WAFCollection:
			b = 40
		}
	}
	return b
}
