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
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/apicollect"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/consent"
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
			return fmt.Errorf("unable to create configuration directory due to error %v", err)
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

func collect(numberThreads int, c *conf.CollectConf) {
	if err := createAllDirs(c); err != nil {
		fmt.Printf("unable to create directories due to error %v\n", err)
		os.Exit(1)
	}
	t := threading.NewThreadPool(numberThreads, 1)
	wrapConfigJob := func(j func(c *conf.CollectConf) error) func() error {
		return func() error { return j(c) }
	}
	if !c.IsDremioCloud() {
		//put all things that take time up front

		// os diagnostic collection
		if !c.CollectNodeMetrics() {
			simplelog.Debugf("Skipping node metrics collection")
		} else {
			t.AddJob(wrapConfigJob(runCollectNodeMetrics))
		}

		if !c.CollectJFR() {
			simplelog.Debugf("Skipping Java Flight Recorder collection")
		} else {
			t.AddJob(wrapConfigJob(runCollectJFR))
		}

		if !c.CollectJStack() {
			simplelog.Debugf("Skipping Java thread dumps collection")
		} else {
			t.AddJob(wrapConfigJob(runCollectJStacks))
		}

		if !c.CaptureHeapDump() {
			simplelog.Debugf("Skipping Java heap dump collection")
		} else {
			t.AddJob(wrapConfigJob(runCollectHeapDump))
		}

		if !c.CollectDiskUsage() {
			simplelog.Info("Skipping disk usage collection")
		} else {
			t.AddJob(wrapConfigJob(runCollectDiskUsage))
		}

		if !c.CollectDremioConfiguration() {
			simplelog.Info("Skipping Dremio config collection")
		} else {
			t.AddJob(wrapConfigJob(runCollectDremioConfig))
		}
		t.AddJob(wrapConfigJob(runCollectOSConfig))

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

		t.AddJob(wrapConfigJob(runCollectJvmConfig))

		// rest call collections

		if !c.CollectKVStoreReport() {
			simplelog.Debug("Skipping KV store report collection")
		} else {
			t.AddJob(wrapConfigJob(apicollect.RunCollectKvReport))
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
}

func runCollectDiskUsage(c *conf.CollectConf) error {
	diskWriter, err := os.Create(path.Clean(filepath.Join(c.NodeInfoOutDir(), "diskusage.txt")))
	if err != nil {
		return fmt.Errorf("unable to create diskusage.txt due to error %v", err)
	}
	defer func() {
		if err := diskWriter.Sync(); err != nil {
			simplelog.Warningf("unable to sync the os_info.txt file due to error: %v", err)
		}
		if err := diskWriter.Close(); err != nil {
			simplelog.Warningf("unable to close the os_info.txt file due to error: %v", err)
		}
	}()
	err = ddcio.Shell(diskWriter, "df -h")
	if err != nil {
		simplelog.Warningf("unable to read df -h due to error %v", err)
	}

	// this detection only makes sense in kubernetes TODO fix this to work with more than just kubernetes
	if strings.Contains(c.NodeName(), "dremio-master") {
		rocksDbDiskUsageWriter, err := os.Create(path.Clean(filepath.Join(c.NodeInfoOutDir(), "rocksdb_disk_allocation.txt")))
		if err != nil {
			return fmt.Errorf("unable to create rocksdb_disk_allocation.txt due to error %v", err)
		}
		defer func() {
			if err := rocksDbDiskUsageWriter.Close(); err != nil {
				simplelog.Warningf("unable to close rocksdb usage writer the file maybe incomplete %v", err)
			}
		}()
		err = ddcio.Shell(rocksDbDiskUsageWriter, "du -sh /opt/dremio/data/db/*")
		if err != nil {
			simplelog.Warningf("unable to write du -sh to rocksdb_disk_allocation.txt due to error %v", err)
		}

	}
	simplelog.Debugf("... Collecting Disk Usage from %v COMPLETED", c.NodeName())

	return nil
}

func runCollectOSConfig(c *conf.CollectConf) error {
	simplelog.Debug("Collecting OS Information")
	osInfoFile := path.Join(c.NodeInfoOutDir(), "os_info.txt")
	w, err := os.Create(path.Clean(osInfoFile))
	if err != nil {
		return fmt.Errorf("unable to create file %v due to error %v", path.Clean(osInfoFile), err)
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
	_, err = w.Write([]byte("___\n>>> hostname\n"))
	if err != nil {
		simplelog.Warningf("unable to write hostname for os_info.txt due to error %v", err)
	}
	err = ddcio.Shell(w, "hostname")
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

func runCollectDremioConfig(c *conf.CollectConf) error {
	simplelog.Debugf("Collecting Configuration Information from %v ...", c.NodeName())

	dremioConfDest := filepath.Join(c.ConfigurationOutDir(), "dremio.conf")
	err := ddcio.CopyFile(filepath.Join(c.DremioConfDir(), "dremio.conf"), dremioConfDest)
	if err != nil {
		simplelog.Warningf("unable to copy dremio.conf due to error %v", err)
	}
	simplelog.Debugf("masking passwords in dremio.conf")
	if err := masking.RemoveSecretsFromDremioConf(dremioConfDest); err != nil {
		simplelog.Warningf("UNABLE TO MASK SECRETS in dremio.conf due to error %v", err)
	}
	err = ddcio.CopyFile(filepath.Join(c.DremioConfDir(), "dremio-env"), filepath.Join(c.ConfigurationOutDir(), "dremio.env"))
	if err != nil {
		simplelog.Warningf("unable to copy dremio.env due to error %v", err)
	}
	err = ddcio.CopyFile(filepath.Join(c.DremioConfDir(), "logback.xml"), filepath.Join(c.ConfigurationOutDir(), "logback.xml"))
	if err != nil {
		simplelog.Warningf("unable to copy logback.xml due to error %v", err)
	}
	err = ddcio.CopyFile(filepath.Join(c.DremioConfDir(), "logback-access.xml"), filepath.Join(c.ConfigurationOutDir(), "logback-access.xml"))
	if err != nil {
		simplelog.Warningf("unable to copy logback-access.xml due to error %v", err)
	}
	simplelog.Debugf("... Collecting Configuration Information from %v COMPLETED", c.NodeName())

	return nil
}

func runCollectJvmConfig(c *conf.CollectConf) error {
	jvmSettingsFile := path.Join(c.NodeInfoOutDir(), "jvm_settings.txt")
	jvmSettingsFileWriter, err := os.Create(path.Clean(jvmSettingsFile))
	if err != nil {
		return fmt.Errorf("unable to create file %v due to error %v", path.Clean(jvmSettingsFile), err)
	}
	defer func() {
		if err := jvmSettingsFileWriter.Sync(); err != nil {
			simplelog.Warningf("unable to sync the os_info.txt file due to error: %v", err)
		}
		if err := jvmSettingsFileWriter.Close(); err != nil {
			simplelog.Warningf("unable to close the os_info.txt file due to error: %v", err)
		}
	}()
	dremioPID := c.DremioPID()
	err = ddcio.Shell(jvmSettingsFileWriter, fmt.Sprintf("jcmd %v VM.flags", dremioPID))
	if err != nil {
		simplelog.Warningf("unable to write jvm_settings.txt file due to error %v", err)
	}
	return nil
}

func runCollectNodeMetrics(c *conf.CollectConf) error {
	simplelog.Debugf("Collecting Node Metrics for %v seconds ....", c.NodeMetricsCollectDurationSeconds())
	nodeMetricsFile := path.Join(c.NodeInfoOutDir(), "metrics.txt")
	nodeMetricsJSONFile := path.Join(c.NodeInfoOutDir(), "metrics.json")
	return nodeinfocollect.SystemMetrics(c.NodeMetricsCollectDurationSeconds(), path.Clean(nodeMetricsFile), path.Clean(nodeMetricsJSONFile))
}

func runCollectJFR(c *conf.CollectConf) error {
	var w bytes.Buffer
	if err := ddcio.Shell(&w, fmt.Sprintf("jcmd %v VM.unlock_commercial_features", c.DremioPID())); err != nil {
		simplelog.Warningf("Error trying to unlock commercial features %v. Note: newer versions of OpenJDK do not support the call VM.unlock_commercial_features. This is usually safe to ignore", err)
	}
	simplelog.Debugf("node: %v - jfr unlock commerictial output - %v", c.NodeName(), w.String())
	w = bytes.Buffer{}
	if err := ddcio.Shell(&w, fmt.Sprintf("jcmd %v JFR.start name=\"DREMIO_JFR\" settings=profile maxage=%vs  filename=%v/%v.jfr dumponexit=true", c.DremioPID(), c.DremioJFRTimeSeconds(), c.JFROutDir(), c.NodeName())); err != nil {
		return fmt.Errorf("unable to run JFR due to error %v", err)
	}
	simplelog.Debugf("node: %v - jfr start output - %v", c.NodeName(), w.String())
	time.Sleep(time.Duration(c.DremioJFRTimeSeconds()) * time.Second)
	// do not "optimize". the recording first needs to be stopped for all processes before collecting the data.
	simplelog.Debugf("... stopping JFR %v", c.NodeName())
	w = bytes.Buffer{}
	if err := ddcio.Shell(&w, fmt.Sprintf("jcmd %v JFR.dump name=\"DREMIO_JFR\"", c.DremioPID())); err != nil {
		return fmt.Errorf("unable to dump JFR due to error %v", err)
	}
	simplelog.Debugf("node: %v - jfr dump output %v", c.NodeName(), w.String())
	w = bytes.Buffer{}
	if err := ddcio.Shell(&w, fmt.Sprintf("jcmd %v JFR.stop name=\"DREMIO_JFR\"", c.DremioPID())); err != nil {
		return fmt.Errorf("unable to dump JFR due to error %v", err)
	}
	simplelog.Debugf("node: %v - jfr stop output %v", c.NodeName(), w.String())

	return nil
}

func runCollectJStacks(c *conf.CollectConf) error {
	simplelog.Debug("Collecting GC logs ...")
	threadDumpFreq := c.DremioJStackFreqSeconds()
	iterations := c.DremioJStackTimeSeconds() / threadDumpFreq
	simplelog.Debugf("Running Java thread dumps every %v second(s) for a total of %v iterations ...", threadDumpFreq, iterations)
	for i := 0; i < iterations; i++ {
		var w bytes.Buffer
		if err := ddcio.Shell(&w, fmt.Sprintf("jcmd %v Thread.print -l", c.DremioPID())); err != nil {
			simplelog.Warningf("unable to capture jstack of pid %v due to error %v", c.DremioPID(), err)
		}
		date := time.Now().Format("2006-01-02_15_04_05")
		threadDumpFileName := path.Join(c.ThreadDumpsOutDir(), fmt.Sprintf("threadDump-%s-%s.txt", c.NodeName(), date))
		if err := os.WriteFile(path.Clean(threadDumpFileName), w.Bytes(), 0600); err != nil {
			return fmt.Errorf("unable to write thread dump %v due to error %v", threadDumpFileName, err)
		}
		simplelog.Debugf("Saved %v", threadDumpFileName)
		simplelog.Debugf("Waiting %v second(s) ...", threadDumpFreq)
		time.Sleep(time.Duration(threadDumpFreq) * time.Second)
	}
	return nil
}

func runCollectHeapDump(c *conf.CollectConf) error {
	simplelog.Debug("Capturing Java Heap Dump")
	dremioPID := c.DremioPID()
	baseName := fmt.Sprintf("%v.hprof", c.NodeName())
	hprofFile := fmt.Sprintf("/tmp/%v.hprof", baseName)
	hprofGzFile := fmt.Sprintf("%v.gz", hprofFile)
	if err := os.Remove(path.Clean(hprofGzFile)); err != nil {
		simplelog.Warningf("unable to remove hprof.gz file with error %v", err)
	}
	if err := os.Remove(path.Clean(hprofFile)); err != nil {
		simplelog.Warningf("unable to remove hprof file with error %v", err)
	}
	var w bytes.Buffer
	if err := ddcio.Shell(&w, fmt.Sprintf("jmap -dump:format=b,file=%v %v", hprofFile, dremioPID)); err != nil {
		return fmt.Errorf("unable to capture heap dump %v", err)
	}
	simplelog.Debugf("heap dump output %v", w.String())
	if err := ddcio.GzipFile(hprofFile, hprofGzFile); err != nil {
		return fmt.Errorf("unable to gzip heap dump file")
	}
	if err := os.Remove(path.Clean(hprofFile)); err != nil {
		simplelog.Warningf("unable to remove old hprof file, must remove manually %v", err)
	}
	dest := path.Join(c.HeapDumpsOutDir(), baseName+".gz")
	if err := os.Rename(path.Clean(hprofGzFile), path.Clean(dest)); err != nil {
		return fmt.Errorf("unable to move heap dump to %v due to error %v", dest, err)
	}
	return nil
}

var LocalCollectCmd = &cobra.Command{
	Use:   "local-collect",
	Short: "retrieves all the dremio logs and diagnostics for the local node and saves the results in a compatible format for Dremio support",
	Long:  `Retrieves all the dremio logs and diagnostics for the local node and saves the results in a compatible format for Dremio support. This subcommand needs to be run with enough permissions to read the /proc filesystem, the dremio logs and configuration files`,
	Run: func(cobraCmd *cobra.Command, args []string) {

		simplelog.Infof("ddc local-collect version: %v", versions.GetCLIVersion())
		simplelog.Infof("args: %v", strings.Join(args, " "))
		overrides := make(map[string]*pflag.Flag)
		//if a cli flag was set go ahead and use those values to override the viper configuration
		cobraCmd.Flags().Visit(func(flag *pflag.Flag) {
			overrides[flag.Name] = flag
			simplelog.Warningf("overriding yaml with cli flag %v and value %v", flag.Name, flag.Value.String())
		})
		c, err := conf.ReadConfFromExecLocation(overrides)
		if err != nil {
			simplelog.Errorf("unable to read configuration %v", err)
			os.Exit(1)
		}
		if !c.AcceptCollectionConsent() {
			fmt.Println(consent.OutputConsent(c))
			os.Exit(1)
		}
		//check if required flags are set
		requiredFlags := []string{"dremio-endpoint", "dremio-username"}

		failed := false
		for _, flag := range requiredFlags {
			if viper.GetString(flag) == "" {
				simplelog.Errorf("required flag '--%s' not set", flag)
				failed = true
			}
		}
		if failed {
			err := cobraCmd.Usage()
			if err != nil {
				simplelog.Errorf("unable to even print usage, this is critical report this bug %v", err)
				os.Exit(1)
			}
			os.Exit(1)
		}

		// Run application
		simplelog.Info("Starting collection...")
		collect(c.NumberThreads(), c)
		ddcLoc, err := os.Executable()
		if err != nil {
			simplelog.Warningf("unable to find ddc itself..so can't copy it's log due to error %v", err)
		} else {
			ddcDir := filepath.Dir(ddcLoc)
			if err := ddcio.CopyFile(filepath.Join(ddcDir, "ddc.log"), filepath.Join(c.OutputDir(), fmt.Sprintf("ddc-%v.log", c.NodeName()))); err != nil {
				simplelog.Warningf("uanble to copy log to archive due to error %v", err)
			}
		}
		tarballName := c.OutputDir() + c.NodeName() + ".tar.gz"
		simplelog.Debugf("collection complete. Archiving %v to %v...", c.OutputDir(), tarballName)
		if err := archive.TarGzDir(c.OutputDir(), tarballName); err != nil {
			simplelog.Errorf("unable to compress archive exiting due to error %v", err)
			os.Exit(1)
		}
		simplelog.Infof("Archive %v complete", tarballName)
	},
}

func init() {
	//wire up override flags
	// consent form
	LocalCollectCmd.Flags().Bool("accept-collection-consent", false, "consent for collection of files, if not true, then collection will stop and a log message will be generated")
	// command line flags ..default is set at runtime due to the CountVarP not having this capacity
	LocalCollectCmd.Flags().CountP("verbose", "v", "Logging verbosity")
	LocalCollectCmd.Flags().Bool("collect-acceleration-log", false, "Run the Collect Acceleration Log collector")
	LocalCollectCmd.Flags().Bool("collect-access-log", false, "Run the Collect Access Log collector")
	LocalCollectCmd.Flags().String("dremio-gclogs-dir", "", "by default will read from the Xloggc flag, otherwise you can override it here")
	LocalCollectCmd.Flags().String("dremio-log-dir", "", "directory with application logs on dremio")
	LocalCollectCmd.Flags().IntP("number-threads", "t", 0, "control concurrency in the system")
	// Add flags for Dremio connection information
	LocalCollectCmd.Flags().String("dremio-endpoint", "", "Dremio REST API endpoint")
	LocalCollectCmd.Flags().String("dremio-username", "", "Dremio username")
	LocalCollectCmd.Flags().String("dremio-pat-token", "", "Dremio Personal Access Token (PAT)")
	LocalCollectCmd.Flags().String("dremio-rocksdb-dir", "", "Path to Dremio RocksDB directory")
	LocalCollectCmd.Flags().String("dremio-conf-dir", "", "Directory where to find the configuration files")
	LocalCollectCmd.Flags().Bool("collect-dremio-configuration", true, "Collect Dremio Configuration collector")
	LocalCollectCmd.Flags().Int("number-job-profiles", 0, "Randomly retrieve number job profiles from the server based on queries.json data but must have --dremio-pat-token set to use")
	LocalCollectCmd.Flags().Bool("capture-heap-dump", false, "Run the Heap Dump collector")
	LocalCollectCmd.Flags().Bool("allow-insecure-ssl", false, "When true allow insecure ssl certs when doing API calls")
}
