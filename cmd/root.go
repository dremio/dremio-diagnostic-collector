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

	"github.com/dremio/dremio-diagnostic-collector/cmd/root/collection"
	"github.com/dremio/dremio-diagnostic-collector/cmd/root/helpers"
	"github.com/dremio/dremio-diagnostic-collector/cmd/root/kubernetes"
	"github.com/dremio/dremio-diagnostic-collector/cmd/root/ssh"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
	"github.com/dremio/dremio-diagnostic-collector/pkg/versions"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var coordinatorContainer string
var executorsContainer string
var coordinatorStr string
var executorsStr string
var sshKeyLoc string
var sshUser string

const outputLoc = "diag.tgz"

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

ddc --coordinator 10.0.0.19 --executors 10.0.0.20,10.0.0.21,10.0.0.22 --ssh-key $HOME/.ssh/id_rsa_dremio

ddc --k8s --namespace mynamespace --coordinator app=dremio-coordinator --executors app=dremio-executor 
`,
	Run: func(c *cobra.Command, args []string) {

	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	foundCmd, _, err := RootCmd.Find(os.Args[1:])
	// default cmd if no cmd is given
	if err == nil && foundCmd.Use == RootCmd.Use && foundCmd.Flags().Parse(os.Args[1:]) != pflag.ErrHelp {
		if sshKeyLoc == "" {
			sshDefault, err := sshDefault()
			if err != nil {
				simplelog.Errorf("unexpected error getting ssh directory '%v'. This is a critical error and should result in a bug report.", err)
				os.Exit(1)
			}
			sshKeyLoc = sshDefault
		}

		collectionArgs := collection.Args{
			CoordinatorStr: coordinatorStr,
			ExecutorsStr:   executorsStr,
			OutputLoc:      filepath.Clean(outputLoc),
			SudoUser:       sudoUser,
			DDCfs:          helpers.NewRealFileSystem(),
		}

		err := validateParameters(collectionArgs, sshKeyLoc, sshUser, isK8s)
		if err != nil {
			fmt.Println("COMMAND HELP TEXT:")
			fmt.Println("")
			err := RootCmd.Help()
			if err != nil {
				simplelog.Errorf("unable to print help %v", err)
				os.Exit(1)
			}
			fmt.Println("")
			fmt.Println("")
			fmt.Printf("Invalid command flag detected: %v\n", err)
			fmt.Println("")
			os.Exit(1)
		}
		simplelog.Info(versions.GetCLIVersion())
		simplelog.Infof("cli command: %v", strings.Join(os.Args, " "))
		cs := helpers.NewHCCopyStrategy(collectionArgs.DDCfs)
		// This is where the SSH or K8s collection is determined. We create an instance of the interface based on this
		// which then determines whether the commands are routed to the SSH or K8s commands

		//default no op
		var clusterCollect = func() {}
		var collectorStrategy collection.Collector
		if isK8s {
			simplelog.Info("using Kubernetes kubectl based collection")
			collectorStrategy = kubernetes.NewKubectlK8sActions(kubectlPath, coordinatorContainer, executorsContainer, namespace)
			clusterCollect = func() {
				err = collection.ClusterK8sExecute(namespace, cs, collectionArgs.DDCfs, collectorStrategy, kubectlPath)
				if err != nil {
					simplelog.Errorf("when getting Kubernetes info, the following error was returned: %v", err)
				}
			}
		} else {
			simplelog.Info("using SSH based collection")
			collectorStrategy = ssh.NewCmdSSHActions(sshKeyLoc, sshUser)
		}

		// Launch the collection
		err = collection.Execute(collectorStrategy,
			cs,
			collectionArgs,
			clusterCollect,
		)
		if err != nil {
			simplelog.Errorf("unexpected error running collection '%v'", err)
			os.Exit(1)
		}
	}
	if err := RootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
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

	RootCmd.Flags().StringVar(&coordinatorContainer, "coordinator-container", "dremio-master-coordinator", "for use with -k8s flag: sets the container name to use to retrieve logs in the coordinators")
	RootCmd.Flags().StringVar(&executorsContainer, "executors-container", "dremio-executor", "for use with -k8s flag: sets the container name to use to retrieve logs in the executors")
	RootCmd.Flags().StringVarP(&coordinatorStr, "coordinator", "c", "", "coordinator to connect to for collection. With ssh set a list of ip addresses separated by commas. In K8s use a label that matches to the pod(s).")
	RootCmd.Flags().StringVarP(&executorsStr, "executors", "e", "", "either a common separated list or a ip range of executors nodes to connect to. With ssh set a list of ip addresses separated by commas. In K8s use a label that matches to the pod(s).")
	RootCmd.Flags().StringVarP(&sshKeyLoc, "ssh-key", "s", "", "location of ssh key to use to login")
	RootCmd.Flags().StringVarP(&sshUser, "ssh-user", "u", "", "user to use during ssh operations to login")
	RootCmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "namespace to use for kubernetes pods")
	RootCmd.Flags().StringVarP(&kubectlPath, "kubectl-path", "p", "kubectl", "where to find kubectl")
	RootCmd.Flags().BoolVarP(&isK8s, "k8s", "k", false, "use kubernetes to retrieve the diagnostics instead of ssh, instead of hosts pass in labels to the --cordinator and --executors flags")
	RootCmd.Flags().StringVarP(&sudoUser, "sudo-user", "b", "", "if any diagnostcs commands need a sudo user (i.e. for jcmd)")
	// TODO implement embedded k8s and ssh support using go libs
	//rootCmd.Flags().BoolVar(&isEmbeddedK8s, "embedded-k8s", false, "use embedded k8s client in place of kubectl binary")
	//rootCmd.Flags().BoolVar(&isEmbeddedSSH, "embedded-ssh", false, "use embedded ssh go client in place of ssh and scp binary")

}

func validateParameters(args collection.Args, sshKeyLoc, sshUser string, isK8s bool) error {
	if args.CoordinatorStr == "" {
		if isK8s {
			return errors.New("the coordinator string was empty you must pass a label that will match your coordinators --coordinator or -c arguments. Example: -c \"mylabel=coordinator\"")
		}
		return errors.New("the coordinator string was empty you must pass a single host or a comma separated lists of hosts to --coordinator or -c arguments. Example: -e 192.168.64.12,192.168.65.10")
	}
	if args.ExecutorsStr == "" {
		if isK8s {
			return errors.New("the executor string was empty you must pass a label that will match your executors --executor or -e arguments. Example: -e \"mylabel=executor\"")
		}
		return errors.New("the executor string was empty you must pass a single host or a comma separated lists of hosts to --executor or -e arguments. Example: -e 192.168.64.12,192.168.65.10")
	}

	if !isK8s {
		if sshKeyLoc == "" {
			return errors.New("the ssh private key location was empty, pass --ssh-key or -s with the key to get past this error. Example --ssh-key ~/.ssh/id_rsa")
		}
		if sshUser == "" {
			return errors.New("the ssh user was empty, pass --ssh-user or -u with the user name you want to use to get past this error. Example --ssh-user ubuntu")
		}
	}
	return nil
}
