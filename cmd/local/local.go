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
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/apicollect"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/configcollect"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/consent"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/jvmcollect"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/logcollect"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/nodeinfocollect"
	"github.com/dremio/dremio-diagnostic-collector/pkg/archive"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/ddcio"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/threading"
	"github.com/dremio/dremio-diagnostic-collector/pkg/masking"
	"github.com/dremio/dremio-diagnostic-collector/pkg/versions"
)

func createAllDirs(c *conf.CollectConf) error {
	var perms fs.FileMode = 0750
	if !c.IsDremioCloud() {
		if err := os.MkdirAll(c.ConfigurationOutDir(), perms); err != nil {
			return fmt.Errorf("unable to create configuration directory %v due to error %v", c.ConfigurationOutDir(), err)
		}
		if err := os.MkdirAll(c.JFROutDir(), perms); err != nil {
			return fmt.Errorf("unable to create jfr directory due to error %v", err)
		}
		if err := os.MkdirAll(c.ThreadDumpsOutDir(), perms); err != nil {
			return fmt.Errorf("unable to create thread-dumps directory due to error %v", err)
		}
		if err := os.MkdirAll(c.HeapDumpsOutDir(), perms); err != nil {
			return fmt.Errorf("unable to create heap-dumps directory due to error %v", err)
		}
		if err := os.MkdirAll(c.KubernetesOutDir(), perms); err != nil {
			return fmt.Errorf("unable to create kubernetes directory due to error %v", err)
		}
		if err := os.MkdirAll(c.KVstoreOutDir(), perms); err != nil {
			return fmt.Errorf("unable to create kvstore directory due to error %v", err)
		}
		if err := os.MkdirAll(c.LogsOutDir(), perms); err != nil {
			return fmt.Errorf("unable to create logs directory due to error %v", err)
		}
		if err := os.MkdirAll(c.NodeInfoOutDir(), perms); err != nil {
			return fmt.Errorf("unable to create node-info directory due to error %v", err)
		}
		if err := os.MkdirAll(c.QueriesOutDir(), perms); err != nil {
			return fmt.Errorf("unable to create queries directory due to error %v", err)
		}
		if err := os.MkdirAll(c.TtopOutDir(), perms); err != nil {
			return fmt.Errorf("unable to create ttop directory due to error %v", err)
		}
	}

	if err := os.MkdirAll(c.SystemTablesOutDir(), perms); err != nil {
		return fmt.Errorf("unable to create system-tables directory due to error %v", err)
	}
	if err := os.MkdirAll(c.WLMOutDir(), perms); err != nil {
		return fmt.Errorf("unable to create wlm directory due to error %v", err)
	}
	if err := os.MkdirAll(c.JobProfilesOutDir(), perms); err != nil {
		return fmt.Errorf("unable to create job-profiles directory due to error %v", err)
	}
	return nil
}

func collect(c *conf.CollectConf) error {
	if err := createAllDirs(c); err != nil {
		return fmt.Errorf("unable to create directories due to error %w", err)
	}
	t, err := threading.NewThreadPool(c.NumberThreads(), 1)
	if err != nil {
		return fmt.Errorf("unable to spawn thread pool: %w", err)
	}
	wrapConfigJob := func(j func(c *conf.CollectConf) error) func() error {
		return func() error { return j(c) }
	}
	if !c.IsDremioCloud() {
		if !c.CollectDiskUsage() {
			simplelog.Info("Skipping disk usage collection")
		} else {
			t.AddJob(wrapConfigJob(nodeinfocollect.RunCollectDiskUsage))
		}

		if !c.CollectDremioConfiguration() {
			simplelog.Info("Skipping Dremio config collection")
		} else {
			t.AddJob(wrapConfigJob(configcollect.RunCollectDremioConfig))
		}

		if !c.CollectOSConfig() {
			simplelog.Info("Skipping OS config collection")
		} else {
			t.AddJob(wrapConfigJob(runCollectOSConfig))
		}

		// log collection

		logCollector := logcollect.NewLogCollector(
			c.DremioLogDir(),
			c.LogsOutDir(),
			c.GcLogsDir(),
			c.DremioGCFilePattern(),
			c.QueriesOutDir(),
			c.DremioQueriesJSONNumDays(),
			c.DremioLogsNumDays(),
		)

		if !c.CollectQueriesJSON() && c.NumberJobProfilesToCollect() == 0 {
			simplelog.Debug("Skipping queries.json collection")
		} else {
			if !c.CollectQueriesJSON() {
				simplelog.Warning("NOT Skipping collection of Queries JSON, because --number-job-profiles is greater than 0 and job profile download requires queries.json ...")
			}
			t.AddJob(logCollector.RunCollectQueriesJSON)
		}

		if !c.CollectServerLogs() {
			simplelog.Debug("Skipping server log collection")
		} else {
			t.AddJob(logCollector.RunCollectDremioServerLog)
		}

		if !c.CollectGCLogs() {
			simplelog.Debug("Skipping gc log collection")
		} else {
			t.AddJob(logCollector.RunCollectGcLogs)
		}

		if !c.CollectMetaRefreshLogs() {
			simplelog.Debug("Skipping metadata refresh log collection")
		} else {
			t.AddJob(logCollector.RunCollectMetadataRefreshLogs)
		}

		if !c.CollectReflectionLogs() {
			simplelog.Debug("Skipping reflection log collection")
		} else {
			t.AddJob(logCollector.RunCollectReflectionLogs)
		}

		if !c.CollectAccelerationLogs() {
			simplelog.Debug("Skipping acceleration log collection")
		} else {
			t.AddJob(logCollector.RunCollectAccelerationLogs)
		}

		if !c.CollectAccessLogs() {
			simplelog.Debug("Skipping access log collection")
		} else {
			t.AddJob(logCollector.RunCollectDremioAccessLogs)
		}

		if !c.CollectAuditLogs() {
			simplelog.Debug("Skipping audit log collection")
		} else {
			t.AddJob(logCollector.RunCollectDremioAuditLogs)
		}

		if !c.CollectJVMFlags() {
			simplelog.Debug("Skipping JVM Flags collection")
		} else {
			t.AddJob(wrapConfigJob(jvmcollect.RunCollectJVMFlags))
		}
		// rest call collections

		if !c.CollectKVStoreReport() {
			simplelog.Debug("Skipping KV store report collection")
		} else {
			t.AddJob(wrapConfigJob(apicollect.RunCollectKvReport))
		}

		if !c.CollectTtop() {
			simplelog.Debugf("Skipping ttop collection")
		} else {
			t.AddJob(wrapConfigJob(jvmcollect.RunTtopCollect))
		}
		if !c.CollectJFR() {
			simplelog.Debugf("Skipping Java Flight Recorder collection")
		} else {
			t.AddJob(wrapConfigJob(jvmcollect.RunCollectJFR))
		}

		if !c.CollectJStack() {
			simplelog.Debugf("Skipping Java thread dumps collection")
		} else {
			t.AddJob(wrapConfigJob(jvmcollect.RunCollectJStacks))
		}

		if !c.CaptureHeapDump() {
			simplelog.Debugf("Skipping Java heap dump collection")
		} else {
			t.AddJob(wrapConfigJob(jvmcollect.RunCollectHeapDump))
		}
	}

	if !c.CollectWLM() {
		simplelog.Debug("Skipping Workload Manager report collection")
	} else {
		t.AddJob(wrapConfigJob(apicollect.RunCollectWLM))
	}

	if !c.CollectSystemTablesExport() {
		simplelog.Debug("Skipping system tables collection")
	} else {
		t.AddJob(wrapConfigJob(apicollect.RunCollectDremioSystemTables))
	}

	if err := t.ProcessAndWait(); err != nil {
		simplelog.Errorf("thread pool has an error: %v", err)
	}

	//we wait on the thread pool to empty out as this is also multithreaded and takes the longest
	if c.NumberJobProfilesToCollect() == 0 {
		simplelog.Debugf("Skipping job profiles collection")
	} else {
		if err := apicollect.RunCollectJobProfiles(c); err != nil {
			simplelog.Errorf("during job profile collection there was an error: %v", err)
		}
	}
	return nil
}

func runCollectOSConfig(c *conf.CollectConf) error {
	simplelog.Debug("Collecting OS Information")
	osInfoFile := filepath.Join(c.NodeInfoOutDir(), "os_info.txt")
	w, err := os.Create(filepath.Clean(osInfoFile))
	if err != nil {
		return fmt.Errorf("unable to create file %v due to error %v", filepath.Clean(osInfoFile), err)
	}
	defer func() {
		if err := w.Sync(); err != nil {
			simplelog.Warningf("unable to sync the os_info.txt file due to error: %v", err)
		}
		if err := w.Close(); err != nil {
			simplelog.Warningf("unable to close the os_info.txt file due to error: %v", err)
		}
	}()

	simplelog.Debug("/etc/*-release")

	_, err = w.Write([]byte("___\n>>> cat /etc/*-release\n"))
	if err != nil {
		simplelog.Warningf("unable to write release file header for os_info.txt due to error %v", err)
	}

	err = ddcio.Shell(w, "cat /etc/*-release")
	if err != nil {
		simplelog.Warningf("unable to write release files for os_info.txt due to error %v", err)
	}

	_, err = w.Write([]byte("___\n>>> uname -r\n"))
	if err != nil {
		simplelog.Warningf("unable to write uname header for os_info.txt due to error %v", err)
	}

	err = ddcio.Shell(w, "uname -r")
	if err != nil {
		simplelog.Warningf("unable to write uname -r for os_info.txt due to error %v", err)
	}
	_, err = w.Write([]byte("___\n>>> cat /etc/issue\n"))
	if err != nil {
		simplelog.Warningf("unable to write cat /etc/issue header for os_info.txt due to error %v", err)
	}
	err = ddcio.Shell(w, "cat /etc/issue")
	if err != nil {
		simplelog.Warningf("unable to write /etc/issue for os_info.txt due to error %v", err)
	}
	_, err = w.Write([]byte("___\n>>> cat /proc/sys/kernel/hostname\n"))
	if err != nil {
		simplelog.Warningf("unable to write hostname for os_info.txt due to error %v", err)
	}
	err = ddcio.Shell(w, "cat /proc/sys/kernel/hostname")
	if err != nil {
		simplelog.Warningf("unable to write hostname for os_info.txt due to error %v", err)
	}
	_, err = w.Write([]byte("___\n>>> cat /proc/meminfo\n"))
	if err != nil {
		simplelog.Warningf("unable to write /proc/meminfo header for os_info.txt due to error %v", err)
	}
	err = ddcio.Shell(w, "cat /proc/meminfo")
	if err != nil {
		simplelog.Warningf("unable to write /proc/meminfo for os_info.txt due to error %v", err)
	}
	_, err = w.Write([]byte("___\n>>> lscpu\n"))
	if err != nil {
		simplelog.Warningf("unable to write lscpu header for os_info.txt due to error %v", err)
	}
	err = ddcio.Shell(w, "lscpu")
	if err != nil {
		simplelog.Warningf("unable to write lscpu for os_info.txt due to error %v", err)
	}

	simplelog.Debugf("... Collecting OS Information from %v COMPLETED", c.NodeName())
	return nil
}

var LocalCollectCmd = &cobra.Command{
	Use:   "local-collect",
	Short: "retrieves all the dremio logs and diagnostics for the local node and saves the results in a compatible format for Dremio support",
	Long:  `Retrieves all the dremio logs and diagnostics for the local node and saves the results in a compatible format for Dremio support. This subcommand needs to be run with enough permissions to read the /proc filesystem, the dremio logs and configuration files`,
	Run: func(cobraCmd *cobra.Command, args []string) {
		overrides := make(map[string]string)
		//if a cli flag was set go ahead and use those values to override the yaml configuration
		cobraCmd.Flags().Visit(func(flag *pflag.Flag) {
			if flag.Name == conf.KeyDremioPatToken {
				if flag.Value.String() == "" {
					pat, err := masking.PromptForPAT()
					if err != nil {
						fmt.Printf("unable to get PAT due to: %v\n", err)
						os.Exit(1)
					}
					if err := flag.Value.Set(pat); err != nil {
						simplelog.Errorf("critical error unable to set %v due to %v", conf.KeyDremioPatToken, err)
						os.Exit(1)
					}
				}
				//we do not want to log the token
				simplelog.Debugf("overriding yaml with cli flag %v and value 'REDACTED'", flag.Name)
			} else {
				simplelog.Debugf("overriding yaml with cli flag %v and value %q", flag.Name, flag.Value.String())
			}
			overrides[flag.Name] = flag.Value.String()
		})
		if err := Execute(args, overrides); err != nil {
			simplelog.Error(errors.Unwrap(err).Error())
			os.Exit(1)
		}
	},
}

func Execute(args []string, overrides map[string]string) error {
	simplelog.Infof("ddc local-collect version: %v", versions.GetCLIVersion())
	simplelog.Infof("args: %v", strings.Join(args, " "))

	c, err := conf.ReadConfFromExecLocation(overrides)
	if err != nil {
		return fmt.Errorf("unable to read configuration %w", err)
	}
	if !c.AcceptCollectionConsent() {
		fmt.Println(consent.OutputConsent(c))
		return errors.New("no consent given")
	}

	// Run application
	simplelog.Info("Starting collection...")
	if err := collect(c); err != nil {
		return fmt.Errorf("unable to collect: %w", err)
	}
	ddcLoc, err := os.Executable()
	if err != nil {
		simplelog.Warningf("unable to find ddc itself..so can't copy it's log due to error %v", err)
	} else {
		ddcDir := filepath.Dir(ddcLoc)
		if err := ddcio.CopyFile(filepath.Join(ddcDir, "ddc.log"), filepath.Join(c.OutputDir(), fmt.Sprintf("ddc-%v.log", c.NodeName()))); err != nil {
			simplelog.Warningf("uanble to copy log to archive due to error %v", err)
		}
	}
	tarballName := filepath.Join(c.TarballOutDir(), c.NodeName()+".tar.gz")
	simplelog.Debugf("collection complete. Archiving %v to %v...", c.OutputDir(), tarballName)
	if err := archive.TarGzDir(c.OutputDir(), tarballName); err != nil {
		return fmt.Errorf("unable to compress archive from folder '%v exiting due to error %w", c.OutputDir(), err)
	}
	simplelog.Infof("Archive %v complete", tarballName)
	return nil
}

func init() {
	//wire up override flags
	// consent form
	LocalCollectCmd.Flags().Bool("accept-collection-consent", false, "consent for collection of files, if not true, then collection will stop and a log message will be generated")
	// command line flags ..default is set at runtime due to the CountVarP not having this capacity
	LocalCollectCmd.Flags().CountP("verbose", "v", "Logging verbosity")
	LocalCollectCmd.Flags().Bool("collect-acceleration-log", false, "Run the Collect Acceleration Log collector")
	LocalCollectCmd.Flags().Bool("collect-access-log", false, "Run the Collect Access Log collector")
	LocalCollectCmd.Flags().Bool("collect-audit-log", false, "Run the Collect Audit Log collector")
	LocalCollectCmd.Flags().String("dremio-gclogs-dir", "", "by default will read from the Xloggc flag, otherwise you can override it here")
	LocalCollectCmd.Flags().String("dremio-log-dir", "", "directory with application logs on dremio")
	LocalCollectCmd.Flags().IntP("number-threads", "t", 2, "control concurrency in the system")
	// Add flags for Dremio connection information
	LocalCollectCmd.Flags().String("dremio-endpoint", "", "Dremio REST API endpoint")
	LocalCollectCmd.Flags().String("dremio-username", "", "Dremio username")
	LocalCollectCmd.Flags().String("dremio-pat-token", "", "Dremio Personal Access Token (PAT)")
	LocalCollectCmd.Flags().String("dremio-rocksdb-dir", "", "Path to Dremio RocksDB directory")
	LocalCollectCmd.Flags().String("dremio-conf-dir", "", "Directory where to find the configuration files")
	LocalCollectCmd.Flags().String("tarball-out-dir", "/tmp/ddc", "directory where the final diag.tgz file is placed. This is also the location where final archive will be output for pickup by the ddc command")
	LocalCollectCmd.Flags().Bool("collect-dremio-configuration", true, "Collect Dremio Configuration collector")
	LocalCollectCmd.Flags().Int("number-job-profiles", 0, "Randomly retrieve number job profiles from the server based on queries.json data but must have --dremio-pat-token set to use")
	LocalCollectCmd.Flags().Bool("capture-heap-dump", false, "Run the Heap Dump collector")
	LocalCollectCmd.Flags().Bool("allow-insecure-ssl", false, "When true allow insecure ssl certs when doing API calls")
	LocalCollectCmd.Flags().Bool("disable-rest-api", false, "disable all REST API calls, this will disable job profile, WLM, and KVM reports")
}
