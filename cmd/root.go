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
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/cmd/awselogs"
	local "github.com/dremio/dremio-diagnostic-collector/cmd/local"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/cmd/root/collection"
	"github.com/dremio/dremio-diagnostic-collector/cmd/root/helpers"
	"github.com/dremio/dremio-diagnostic-collector/cmd/root/kubernetes"
	"github.com/dremio/dremio-diagnostic-collector/cmd/root/ssh"
	version "github.com/dremio/dremio-diagnostic-collector/cmd/version"
	"github.com/dremio/dremio-diagnostic-collector/pkg/consoleprint"
	"github.com/dremio/dremio-diagnostic-collector/pkg/masking"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
	"github.com/dremio/dremio-diagnostic-collector/pkg/versions"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// var scaleoutCoordinatorContainer string
var coordinatorContainer string
var executorsContainer string
var coordinatorStr string
var executorsStr string
var sshKeyLoc string
var sshUser string
var promptForDremioPAT bool
var transferDir string
var ddcYamlLoc string

var outputLoc string

var kubectlPath string
var isK8s bool
var sudoUser string
var namespace string

// var isEmbeddedK8s bool
// var isEmbeddedSSH bool

// RootCmd represents the base command when called without any subcommands
var RootCmd = &cobra.Command{
	Use:   "ddc",
	Short: versions.GetCLIVersion() + " ddc connects via to dremio servers collects logs into an archive",
	Long: versions.GetCLIVersion() + ` ddc connects via ssh or kubectl and collects a series of logs and files for dremio, then puts those collected files in an archive
examples:

for ssh based communication to VMs or Bare metal hardware:

	# coordinator only
	ddc --coordinator 10.0.0.19 --ssh-user myuser 
	# coordinator and executors
	ddc --coordinator 10.0.0.19 --executors 10.0.0.20,10.0.0.21,10.0.0.22 --ssh-user myuser 
	# to collect job profiles, system tables, kv reports and wlm 
	ddc --coordinator 10.0.0.19 --executors 10.0.0.20,10.0.0.21,10.0.0.22 --ssh-user myuser  --dremio-pat-prompt
	# to avoid using the /tmp folder on nodes
	ddc --coordinator 10.0.0.19 --executors 10.0.0.20,10.0.0.21,10.0.0.22 --ssh-user myuser --transfer-dir /mnt/lots_of_storage/	

for kubernetes deployments:

	# coordinator only
	ddc --k8s --namespace mynamespace --coordinator app=dremio-coordinator 
	# coordinator and executors
	ddc --k8s --namespace mynamespace --coordinator app=dremio-coordinator --executors app=dremio-executor 
	# to collect job profiles, system tables, kv reports and wlm 
	ddc --k8s -n mynamespace -c app=dremio-coordinator -e app=dremio-executor --dremio-pat-prompt
`,
	Run: func(c *cobra.Command, args []string) {

	},
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

func RemoteCollect(collectionArgs collection.Args, sshArgs ssh.Args, kubeArgs kubernetes.KubeArgs, k8sEnabled bool) error {
	consoleprint.UpdateRuntime(
		versions.GetCLIVersion(),
		simplelog.GetLogLoc(),
		collectionArgs.DDCYamlLoc,
		"",
		collectionArgs.Enabled,
		collectionArgs.Disabled,
		collectionArgs.PATSet,
		0,
		0,
	)
	err := validateParameters(collectionArgs, sshArgs, k8sEnabled)
	if err != nil {
		fmt.Println("COMMAND HELP TEXT:")
		fmt.Println("")
		helpErr := RootCmd.Help()
		if helpErr != nil {
			return fmt.Errorf("unable to print help %w", helpErr)
		}
		return fmt.Errorf("invalid command flag detected: %w", err)
	}
	outputDir, err := filepath.Abs(filepath.Dir(outputLoc))
	// This is where the SSH or K8s collection is determined. We create an instance of the interface based on this
	// which then determines whether the commands are routed to the SSH or K8s commands
	if err != nil {
		return fmt.Errorf("error when getting directory for copy strategy: %v", err)
	}
	cs := helpers.NewHCCopyStrategy(collectionArgs.DDCfs, &helpers.RealTimeService{}, outputDir)

	defer cs.Close()
	var clusterCollect = func([]string) {}
	var collectorStrategy collection.Collector
	if k8sEnabled {
		simplelog.Info("using Kubernetes kubectl based collection")
		collectorStrategy = kubernetes.NewKubectlK8sActions(kubeArgs)
		consoleprint.UpdateRuntime(
			versions.GetCLIVersion(),
			simplelog.GetLogLoc(),
			collectionArgs.DDCYamlLoc,
			collectorStrategy.Name(),
			collectionArgs.Enabled,
			collectionArgs.Disabled,
			collectionArgs.PATSet,
			0,
			0,
		)
		clusterCollect = func(pods []string) {
			err = collection.ClusterK8sExecute(kubeArgs.Namespace, cs, collectionArgs.DDCfs, collectorStrategy, kubeArgs.KubectlPath)
			if err != nil {
				simplelog.Errorf("when getting Kubernetes info, the following error was returned: %v", err)
			}
			err = collection.GetClusterLogs(kubeArgs.Namespace, cs, collectionArgs.DDCfs, kubeArgs.KubectlPath, pods)
			if err != nil {
				simplelog.Errorf("when getting container logs, the following error was returned: %v", err)
			}
			err = collection.GetClusterNodes(kubeArgs.Namespace, cs, collectionArgs.DDCfs, kubeArgs.KubectlPath)
			if err != nil {
				simplelog.Errorf("when getting cluster nodes, the following error was returned: %v", err)
			}
			err = collection.GetClusterPods(kubeArgs.Namespace, cs, collectionArgs.DDCfs, kubeArgs.KubectlPath)
			if err != nil {
				simplelog.Errorf("when getting cluster pods, the following error was returned: %v", err)
			}
		}
	} else {
		simplelog.Info("using SSH based collection")
		collectorStrategy = ssh.NewCmdSSHActions(sshArgs)
	}

	// Launch the collection
	err = collection.Execute(collectorStrategy,
		cs,
		collectionArgs,
		clusterCollect,
	)
	if err != nil {
		return err
	}
	return nil
}

func ValidateAndReadYaml(ddcYaml string) (map[string]interface{}, error) {
	emptyOverrides := make(map[string]string)
	confData, err := conf.ParseConfig(ddcYaml, emptyOverrides)
	if err != nil {
		return make(map[string]interface{}), err
	}

	simplelog.Infof("parsed configuration for %v follows", ddcYaml)
	for k, v := range confData {
		if k == conf.KeyDremioPatToken && v != "" {
			simplelog.Infof("yaml key '%v':'REDACTED'", k)
		} else {
			simplelog.Infof("yaml key '%v':'%v'", k, v)
		}
	}

	// set defaults so we get an accurate reading of if these will be enabled or not
	conf.SetViperDefaults(confData, "", 0)
	return confData, nil
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute(args []string) error {
	if len(args) < 2 {
		fmt.Println("COMMAND HELP TEXT:")
		fmt.Println("")
		helpErr := RootCmd.Help()
		if helpErr != nil {
			return fmt.Errorf("unable to print help %w", helpErr)
		}
		return nil
	}
	foundCmd, _, err := RootCmd.Find(args[1:])
	// default cmd if no cmd is given
	if err == nil && foundCmd.Use == RootCmd.Use && foundCmd.Flags().Parse(args[1:]) != pflag.ErrHelp {
		stop := startTicker()
		defer stop()
		if sshKeyLoc == "" {
			sshDefault, err := sshDefault()
			if err != nil {
				return fmt.Errorf("unexpected error getting ssh directory '%v'. This is a critical error and should result in a bug report", err)
			}
			sshKeyLoc = sshDefault
		}
		dremioPAT := ""
		if promptForDremioPAT {
			pat, err := masking.PromptForPAT()
			if err != nil {
				return fmt.Errorf("unable to get PAT due to: %v", err)
			}
			dremioPAT = pat
		}
		patSet := dremioPAT != ""
		simplelog.Info(versions.GetCLIVersion())
		simplelog.Infof("cli command: %v", strings.Join(args, " "))
		confData, err := ValidateAndReadYaml(ddcYamlLoc)
		if err != nil {
			return fmt.Errorf("CRITICAL ERROR: unable to parse %v: %v", ddcYamlLoc, err)
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
		collectionArgs := collection.Args{
			CoordinatorStr: coordinatorStr,
			ExecutorsStr:   executorsStr,
			OutputLoc:      filepath.Clean(outputLoc),
			SudoUser:       sudoUser,
			DDCfs:          helpers.NewRealFileSystem(),
			DremioPAT:      dremioPAT,
			TransferDir:    transferDir,
			DDCYamlLoc:     ddcYamlLoc,
			Enabled:        enabled,
			Disabled:       disabled,
			PATSet:         patSet,
		}
		sshArgs := ssh.Args{
			SSHKeyLoc: sshKeyLoc,
			SSHUser:   sshUser,
		}
		kubeArgs := kubernetes.KubeArgs{
			Namespace:            namespace,
			CoordinatorContainer: coordinatorContainer,
			ExecutorsContainer:   executorsContainer,
			KubectlPath:          kubectlPath,
		}
		if err := RemoteCollect(collectionArgs, sshArgs, kubeArgs, isK8s); err != nil {
			consoleprint.UpdateResult(err.Error())
		} else {
			consoleprint.UpdateResult(fmt.Sprintf("complete at %v", time.Now().Format(time.RFC1123)))
		}
		// we put the error in result so just return nil
		consoleprint.PrintState()
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

	RootCmd.Flags().StringVar(&coordinatorContainer, "coordinator-container", "dremio-master-coordinator,dremio-coordinator", "for use with -k8s flag: sets the container name to use to retrieve logs in the coordinators")
	RootCmd.Flags().StringVar(&executorsContainer, "executors-container", "dremio-executor", "for use with -k8s flag: sets the container name to use to retrieve logs in the executors")
	RootCmd.Flags().StringVarP(&coordinatorStr, "coordinator", "c", "", "coordinator to connect to for collection. With ssh set a list of ip addresses separated by commas. In K8s use a label that matches to the pod(s).")
	RootCmd.Flags().StringVarP(&executorsStr, "executors", "e", "", "either a common separated list or a ip range of executors nodes to connect to. With ssh set a list of ip addresses separated by commas. In K8s use a label that matches to the pod(s).")
	RootCmd.Flags().StringVarP(&sshKeyLoc, "ssh-key", "s", "", "location of ssh key to use to login")
	RootCmd.Flags().StringVarP(&sshUser, "ssh-user", "u", "", "user to use during ssh operations to login")
	RootCmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "namespace to use for kubernetes pods")
	RootCmd.Flags().StringVarP(&kubectlPath, "kubectl-path", "p", "kubectl", "where to find kubectl")
	RootCmd.Flags().BoolVarP(&isK8s, "k8s", "k", false, "use kubernetes to retrieve the diagnostics instead of ssh, instead of hosts pass in labels to the --coordinator and --executors flags")
	RootCmd.Flags().BoolVarP(&promptForDremioPAT, "dremio-pat-prompt", "t", false, "Prompt for Dremio Personal Access Token (PAT)")
	RootCmd.Flags().StringVarP(&sudoUser, "sudo-user", "b", "", "if any diagnostics commands need a sudo user (i.e. for jcmd)")
	RootCmd.Flags().StringVar(&transferDir, "transfer-dir", fmt.Sprintf("/tmp/ddc-%v", time.Now().Format("20060102150405")), "directory to use for communication between the local-collect command and this one")
	RootCmd.Flags().StringVar(&outputLoc, "output-file", "diag.tgz", "name of tgz file to save the diagnostic collection to")
	execLoc, err := os.Executable()
	if err != nil {
		fmt.Printf("unable to find ddc, critical error %v", err)
		os.Exit(1)
	}
	execLocDir := filepath.Dir(execLoc)
	RootCmd.Flags().StringVar(&ddcYamlLoc, "ddc-yaml", filepath.Join(execLocDir, "ddc.yaml"), "location of ddc.yaml that will be transferred to remote nodes for collection configuration")

	//init
	RootCmd.AddCommand(local.LocalCollectCmd)
	RootCmd.AddCommand(version.VersionCmd)
	RootCmd.AddCommand(awselogs.AWSELogsCmd)
}

func validateParameters(args collection.Args, sshArgs ssh.Args, isK8s bool) error {
	if args.CoordinatorStr == "" {
		if isK8s {
			return errors.New("the coordinator string was empty you must pass a label that will match your coordinators --coordinator or -c arguments. Example: -c \"mylabel=coordinator\"")
		}
		return errors.New("the coordinator string was empty you must pass a single host or a comma separated lists of hosts to --coordinator or -c arguments. Example: -e 192.168.64.12,192.168.65.10")
	}

	if !isK8s {
		if sshArgs.SSHKeyLoc == "" {
			return errors.New("the ssh private key location was empty, pass --ssh-key or -s with the key to get past this error. Example --ssh-key ~/.ssh/id_rsa")
		}
		if sshArgs.SSHUser == "" {
			return errors.New("the ssh user was empty, pass --ssh-user or -u with the user name you want to use to get past this error. Example --ssh-user ubuntu")
		}
	}
	return nil
}
