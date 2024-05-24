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
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/apicollect"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/configcollect"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/jvmcollect"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/logcollect"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/nodeinfocollect"
	"github.com/dremio/dremio-diagnostic-collector/pkg/archive"
	"github.com/dremio/dremio-diagnostic-collector/pkg/clusterstats"
	"github.com/dremio/dremio-diagnostic-collector/pkg/dirs"
	"github.com/dremio/dremio-diagnostic-collector/pkg/masking"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
	"github.com/dremio/dremio-diagnostic-collector/pkg/validation"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/ddcio"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/threading"
	"github.com/dremio/dremio-diagnostic-collector/pkg/versions"
)

var ddcYamlLoc, collectionMode, pid string
var patStdIn bool

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

	if err := os.MkdirAll(c.ClusterStatsOutDir(), perms); err != nil {
		return fmt.Errorf("unable to create cluster-stats directory due to error %v", err)
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
	if !c.DisableFreeSpaceCheck() {
		if err := dirs.CheckFreeSpace(c.TarballOutDir(), uint64(c.MinFreeSpaceGB())); err != nil {
			return fmt.Errorf("%v. Use a larger directory by using ddc --transfer-dir or if using ddc local-collect --tarball-out-dir", err)
		}
	}
	if err := createAllDirs(c); err != nil {
		return fmt.Errorf("unable to create directories due to error %w", err)
	}

	// we can probably remove this now that we have gone to single threaded, but keeping it for the delayed execution and logging for now
	t, err := threading.NewThreadPool(1, 1, true, false)
	if err != nil {
		return fmt.Errorf("unable to spawn thread pool: %w", err)
	}

	wrapConfigJob := func(name string, j func(c *conf.CollectConf) error) threading.Job {
		return threading.Job{
			Name:    name,
			Process: func() error { return j(c) },
		}
	}

	// rest call so we move it the front in case the token expires
	if !c.CollectWLM() {
		simplelog.Debug("Skipping Workload Manager report collection")
	} else {
		t.AddJob(wrapConfigJob("WLM COLLECTION", apicollect.RunCollectWLM))
	}

	// rest call so we move it the front in case the token expires
	if !c.CollectSystemTablesExport() {
		simplelog.Debug("Skipping system tables collection")
	} else {
		t.AddJob(wrapConfigJob("SYSTEM TABLE COLLECTION", apicollect.RunCollectDremioSystemTables))
	}

	if !c.IsDremioCloud() {
		// rest call so we move it the front in case the token expires
		if !c.CollectKVStoreReport() {
			simplelog.Debug("Skipping KV store report collection")
		} else {
			t.AddJob(wrapConfigJob("KV STORE COLLECTION", apicollect.RunCollectKvReport))
		}

		if !c.CollectDiskUsage() {
			simplelog.Info("Skipping disk usage collection")
		} else {
			t.AddJob(wrapConfigJob("DISK USAGE COLLECTION", nodeinfocollect.RunCollectDiskUsage))
		}

		if !c.CollectDremioConfiguration() {
			simplelog.Info("Skipping Dremio config collection")
		} else {
			t.AddJob(wrapConfigJob("DREMIO CONFIG COLLECTION", configcollect.RunCollectDremioConfig))
		}

		if !c.CollectOSConfig() {
			simplelog.Info("Skipping OS config collection")
		} else {
			t.AddJob(wrapConfigJob("OS CONFIG COLLECTION", runCollectOSConfig))
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
			t.AddJob(threading.Job{
				Name:    "QUERIES.JSON COLLECTION",
				Process: logCollector.RunCollectQueriesJSON,
			})

		}

		if !c.CollectServerLogs() {
			simplelog.Debug("Skipping server log collection")
		} else {
			t.AddJob(threading.Job{
				Name:    "SERVER LOG COLLECTION",
				Process: logCollector.RunCollectDremioServerLog,
			})
		}

		if !c.CollectGCLogs() {
			simplelog.Debug("Skipping gc log collection")
		} else {
			t.AddJob(threading.Job{
				Name:    "GC LOG COLLECTION",
				Process: logCollector.RunCollectGcLogs,
			})
		}

		if !c.CollectMetaRefreshLogs() {
			simplelog.Debug("Skipping metadata refresh log collection")
		} else {
			t.AddJob(threading.Job{
				Name:    "METADATA LOG COLLECTION",
				Process: logCollector.RunCollectMetadataRefreshLogs,
			})
		}

		if !c.CollectReflectionLogs() {
			simplelog.Debug("Skipping reflection log collection")
		} else {
			t.AddJob(threading.Job{
				Name:    "REFLECTING LOG COLLECTION",
				Process: logCollector.RunCollectReflectionLogs,
			})
		}

		if !c.CollectAccelerationLogs() {
			simplelog.Debug("Skipping acceleration log collection")
		} else {
			t.AddJob(threading.Job{
				Name:    "ACCELERATION LOG COLLECTION",
				Process: logCollector.RunCollectAccelerationLogs,
			})
		}

		if !c.CollectAccessLogs() {
			simplelog.Debug("Skipping access log collection")
		} else {
			t.AddJob(threading.Job{
				Name:    "ACCESS LOG COLLECTION",
				Process: logCollector.RunCollectDremioAccessLogs,
			})
		}

		if !c.CollectAuditLogs() {
			simplelog.Debug("Skipping audit log collection")
		} else {
			t.AddJob(threading.Job{
				Name:    "AUDIT LOG COLLECTION",
				Process: logCollector.RunCollectDremioAuditLogs,
			})
		}

		if !c.CollectJVMFlags() {
			simplelog.Debug("Skipping JVM Flags collection")
		} else {
			t.AddJob(wrapConfigJob("JVM FLAG COLLECTION", jvmcollect.RunCollectJVMFlags))
		}

		if !c.CollectTtop() {
			simplelog.Debugf("Skipping ttop collection")
		} else {
			t.AddJob(wrapConfigJob("TTOP COLLECTION", jvmcollect.RunTtopCollect))
		}
		if !c.CollectJFR() {
			simplelog.Debugf("Skipping Java Flight Recorder collection")
		} else {
			t.AddJob(wrapConfigJob("JFR COLLECTION", jvmcollect.RunCollectJFR))
		}

		if !c.CollectJStack() {
			simplelog.Debugf("Skipping Java thread dumps collection")
		} else {
			t.AddJob(wrapConfigJob("JSTACK COLLECTION", jvmcollect.RunCollectJStacks))
		}

		if !c.CaptureHeapDump() {
			simplelog.Debugf("Skipping Java heap dump collection")
		} else {
			t.AddJob(wrapConfigJob("HEAP DUMP COLLECTION", jvmcollect.RunCollectHeapDump))
		}
	}

	if err := t.ProcessAndWait(); err != nil {
		simplelog.Errorf("thread pool has an error: %v", err)
	}

	// this has to happen after the queries.json collection so we don't have much choice and have to leave it here
	if c.NumberJobProfilesToCollect() == 0 {
		simplelog.Debugf("Skipping job profiles collection")
	} else {
		if err := apicollect.RunCollectJobProfiles(c); err != nil {
			simplelog.Errorf("during job profile collection there was an error: %v", err)
		}
	}
	if err := runCollectClusterStats(c); err != nil {
		simplelog.Errorf("during unable to collect cluster stats like cluster ID: %v", err)
	}
	return nil
}

func findClusterID(c *conf.CollectConf) (string, error) {
	startTime := time.Now().Unix()
	var clusterID string
	rocksDBDir := c.DremioRocksDBDir()
	simplelog.Debugf("checking dir %v for cluster version", rocksDBDir)

	err := filepath.Walk(rocksDBDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			simplelog.Errorf("error accessing path %q: %v", path, err)
			return nil
		}

		if clusterID != "" {
			return nil
		}
		if !info.IsDir() {
			// readonly cannot modify the files touched
			f, err := os.Open(filepath.Clean(path))
			simplelog.Debugf("checking file %v for cluster version", f.Name())
			if err != nil {
				simplelog.Errorf("error reading file %q: %v", path, err)
				return nil
			}
			defer f.Close()
			var tempString string
			reader := bufio.NewReader(f)
			matched := ""
			nextChar := 'c'
			skipped := 0
			var bytesRead int64
			for {
				// Read file byte by byte
				b, err := reader.ReadByte()
				if err != nil {
					break // End of file or an error
				}
				bytesRead++
				if tempString == "clusterIdentity" {
					if skipped != 4 {
						skipped++
						continue
					}
					matched += string(b)
					if len(matched) == 36 {
						endTime := time.Now().Unix()
						seconds := endTime - startTime
						if seconds > 0 {
							simplelog.Infof("found cluster ID '%v' in file %v in %v seconds at %.2f bytes/second", matched, path, seconds, float64(bytesRead)/float64(seconds))
						} else {
							simplelog.Infof("found cluster ID '%v' in file %v in less than a second", matched, path)
						}
						clusterID = matched
						return nil
					}
				} else {
					// looking for starting clusterIdentity
					c := rune(b)
					if nextChar == c {
						tempString += string(b)
						switch tempString {
						case "c":
							nextChar = 'l'
						case "cl":
							nextChar = 'u'
						case "clu":
							nextChar = 's'
						case "clus":
							nextChar = 't'
						case "clust":
							nextChar = 'e'
						case "cluste":
							nextChar = 'r'
						case "cluster":
							nextChar = 'I'
						case "clusterI":
							nextChar = 'd'
						case "clusterId":
							nextChar = 'e'
						case "clusterIde":
							nextChar = 'n'
						case "clusterIden":
							nextChar = 't'
						case "clusterIdent":
							nextChar = 'i'
						case "clusterIdenti":
							nextChar = 't'
						case "clusterIdentit":
							nextChar = 'y'
						case "clusterIdentity":
							simplelog.Infof("found clusterIdentity key in file %v", path)
						}
					} else {
						tempString = ""
						nextChar = 'c'
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("error walking the path %v: %v", rocksDBDir, err)
	}
	simplelog.Infof("total time to search clusterID in directory %v was %v seconds", rocksDBDir, time.Now().Unix()-startTime)
	return clusterID, nil
}

func parseVersionFromClassPath(classPath string) string {
	re := regexp.MustCompile(`dremio-common-(.+)\.jar`)
	lines := strings.Split(classPath, ":")
	for _, line := range lines {
		matches := re.FindStringSubmatch(line)
		if len(matches) > 1 {
			return matches[1]
		}
	}
	return ""
}

func getClassPath(pid int) (string, error) {
	var w bytes.Buffer
	if err := ddcio.Shell(&w, fmt.Sprintf("jcmd %v VM.system_properties", pid)); err != nil {
		return "", err
	}
	out := w.String()
	scanner := bufio.NewScanner(strings.NewReader(out))
	buf := make([]byte, 0, 256*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "java.class.path=") {
			return line, nil
		}
	}
	if scanner.Err() != nil {
		return "", fmt.Errorf("error while scanning '%v' for version: %v", out, scanner.Err())
	}
	return "", fmt.Errorf("no matches for java.class.path= found in '%v'", pid)
}

func runCollectClusterStats(c *conf.CollectConf) error {
	simplelog.Debugf("Collecting cluster stats")
	classPath, err := getClassPath(c.DremioPID())
	if err != nil {
		return err
	}
	dremioVersion := parseVersionFromClassPath(classPath)
	simplelog.Debugf("dremio version %v", dremioVersion)
	clusterID, err := findClusterID(c)
	if err != nil {
		return err
	}
	clusterStats := &clusterstats.ClusterStats{
		DremioVersion: dremioVersion,
		ClusterID:     clusterID,
		NodeName:      c.NodeName(),
	}
	b, err := json.Marshal(clusterStats)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(c.ClusterStatsOutDir(), "cluster-stats.json"), b, 0600)
}

func runCollectOSConfig(c *conf.CollectConf) error {
	simplelog.Debug("Collecting OS Information")
	osInfoFile := filepath.Join(c.NodeInfoOutDir(), "os_info.txt")
	w, err := os.Create(filepath.Clean(osInfoFile))
	if err != nil {
		return fmt.Errorf("unable to create file %v due to error %v", filepath.Clean(osInfoFile), err)
	}
	defer func() {

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
	_, err = w.Write([]byte("___\n>>> mount\n"))
	if err != nil {
		simplelog.Warningf("unable to write mount header for os_info.txt due to error %v", err)
	}
	err = ddcio.Shell(w, "mount")
	if err != nil {
		simplelog.Warningf("unable to write mount for os_info.txt due to error %v", err)
	}
	_, err = w.Write([]byte("___\n>>> lsblk\n"))
	if err != nil {
		simplelog.Warningf("unable to write lsblk header for os_info.txt due to error %v", err)
	}
	err = ddcio.Shell(w, "lsblk")
	if err != nil {
		simplelog.Warningf("unable to write lsblk for os_info.txt due to error %v", err)
	}

	if c.DremioPID() > 0 {
		_, err = w.Write([]byte("___\n>>> ps eww\n"))
		if err != nil {
			simplelog.Warningf("unable to write ps eww header for os_info.txt due to error %v", err)
		}
		err = ddcio.Shell(w, fmt.Sprintf("ps eww %v | grep dremio | awk '{$1=$2=$3=$4=\"\"; print $0}'", c.DremioPID()))
		if err != nil {
			simplelog.Warningf("unable to write ps eww output for os_info.txt due to error %v", err)
		}
	}
	if err := w.Sync(); err != nil {
		return fmt.Errorf("unable to sync the os_info.txt file due to error: %v", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("unable to close the os_info.txt file due to error: %v", err)
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
					overrides[flag.Name] = pat
				} else {
					overrides[flag.Name] = flag.Value.String()
				}
				//we do not want to log the token
				simplelog.Debugf("overriding yaml with cli flag %v and value 'REDACTED'", flag.Name)
			} else {
				simplelog.Debugf("overriding yaml with cli flag %v and value %q", flag.Name, flag.Value.String())
				overrides[flag.Name] = flag.Value.String()
			}
		})
		if patStdIn {
			var inputReader io.Reader = cobraCmd.InOrStdin()
			b, err := io.ReadAll(inputReader)
			if err != nil {
				fmt.Printf("\nCRITICAL ERROR: %v\n", err)
				os.Exit(1)
			}
			pat := strings.TrimSpace(string(b[:]))
			if pat != "" {
				overrides[conf.KeyDremioPatToken] = pat
			}
		}
		msg, err := Execute(args, overrides)
		if err != nil {
			fmt.Printf("\nCRITICAL ERROR: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(msg)
	},
}

func Execute(args []string, overrides map[string]string) (string, error) {
	simplelog.Infof("ddc local-collect version: %v", versions.GetCLIVersion())
	simplelog.Infof("args: %v", strings.Join(args, " "))
	fmt.Println(strings.TrimSpace(versions.GetCLIVersion()))
	if pid != "" {
		if _, err := os.Stat(pid); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return "", fmt.Errorf("unable to read pid location '%v' with error: '%w'", pid, err)
			}
			// this means nothing is present great continue
			if err := os.WriteFile(filepath.Clean(pid), []byte(""), 0600); err != nil {
				return "", fmt.Errorf("unable to write pid file '%v: %w", pid, err)
			}
			defer func() {
				if err := os.Remove(pid); err != nil {
					msg := fmt.Sprintf("unable to remove pid '%v': '%v', it will need to be removed manually", pid, err)
					fmt.Println(msg)
					simplelog.Error(msg)
				}
			}()
		} else {
			return "", fmt.Errorf("DDC is running based on pid file '%v'. If this is a stale file then please remove", pid)
		}
	}
	startTime := time.Now().Unix()
	if err := validation.ValidateCollectMode(collectionMode); err != nil {
		return "", err
	}

	c, err := conf.ReadConf(overrides, ddcYamlLoc, collectionMode)
	if err != nil {
		return "", fmt.Errorf("unable to read configuration %w", err)
	}

	fmt.Println("looking for logs in: " + c.DremioLogDir())

	// Run application
	simplelog.Info("Starting collection...")
	if err := collect(c); err != nil {
		return "", fmt.Errorf("unable to collect: %w", err)
	}

	logLoc := simplelog.GetLogLoc()
	if logLoc != "" {
		if err := ddcio.CopyFile(simplelog.GetLogLoc(), filepath.Join(c.OutputDir(), fmt.Sprintf("ddc-%v.log", c.NodeName()))); err != nil {
			simplelog.Warningf("unable to copy log to archive due to error %v", err)
		}
	}
	tarballName := filepath.Join(c.TarballOutDir(), c.NodeName()+".tar.gz")
	simplelog.Debugf("collection complete. Archiving %v to %v...", c.OutputDir(), tarballName)
	if err := archive.TarGzDir(c.OutputDir(), tarballName); err != nil {
		return "", fmt.Errorf("unable to compress archive from folder '%v exiting due to error %w", c.OutputDir(), err)
	}
	if err := os.RemoveAll(c.OutputDir()); err != nil {
		simplelog.Errorf("unable to remove %v: %v", c.OutputDir(), err)
	}
	simplelog.Infof("Archive %v complete", tarballName)
	endTime := time.Now().Unix()
	fi, err := os.Stat(tarballName)
	if err != nil {
		// quickly just supplying tarball name and elapsed
		return fmt.Sprintf("file %v - %v secs collection", tarballName, endTime-startTime), nil
	}
	return fmt.Sprintf("file %v - %v seconds for collection - size %v bytes", tarballName, endTime-startTime, fi.Size()), nil
}

func init() {
	//wire up override flags
	LocalCollectCmd.Flags().CountP("verbose", "v", "Logging verbosity")
	LocalCollectCmd.Flags().String("dremio-pat-token", "", "Dremio Personal Access Token (PAT)")
	LocalCollectCmd.Flags().String("tarball-out-dir", "/tmp/ddc", "directory where the final diag.tgz file is placed. This is also the location where final archive will be output for pickup by the ddc command")
	LocalCollectCmd.Flags().Bool(conf.KeyDisableFreeSpaceCheck, false, "disables the free space check for the --tarball-out-dir")
	LocalCollectCmd.Flags().Int(conf.KeyMinFreeSpaceGB, 40, "min free space needed in GB for the process to run")
	if err := LocalCollectCmd.Flags().MarkHidden(conf.KeyMinFreeSpaceGB); err != nil {
		fmt.Printf("unable to mark flag hidden critical error %v", err)
		os.Exit(1)
	}
	LocalCollectCmd.Flags().Bool("allow-insecure-ssl", false, "When true allow insecure ssl certs when doing API calls")
	LocalCollectCmd.Flags().BoolVar(&patStdIn, "pat-stdin", false, "allows one to pipe the pat to standard in")
	LocalCollectCmd.Flags().Bool("disable-rest-api", false, "disable all REST API calls, this will disable job profile, WLM, and KVM reports")
	LocalCollectCmd.Flags().StringVar(&pid, "pid", "", "write a pid")
	if err := LocalCollectCmd.Flags().MarkHidden("pid"); err != nil {
		fmt.Printf("unable to mark flag hidden critical error %v", err)
		os.Exit(1)
	}
	execLoc, err := os.Executable()
	if err != nil {
		fmt.Printf("unable to find ddc, critical error %v", err)
		os.Exit(1)
	}
	execLocDir := filepath.Dir(execLoc)
	LocalCollectCmd.Flags().StringVar(&ddcYamlLoc, "ddc-yaml", filepath.Join(execLocDir, "ddc.yaml"), "location of ddc.yaml that will be transferred to remote nodes for collection configuration")
	LocalCollectCmd.Flags().StringVar(&collectionMode, "collect", "light", "type of collection: 'light'- 2 days of logs (no ttop, jstack or jfr). 'standard' - includes jfr, ttop, jstack, 7 days of logs and 30 days of queries.json logs. 'health-check' - all of 'standard' + WLM, KV Store Report, 25,000 Job Profiles")
}
