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
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/apicollect"
	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/configcollect"
	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/jvmcollect"
	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/logcollect"
	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/nodeinfocollect"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/archive"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/clusterstats"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/dirs"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/masking"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/shutdown"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/simplelog"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/validation"

	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/ddcio"
	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/threading"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/versions"
)

var (
	ddcYamlLoc, collectionMode, pid string
	patStdIn                        bool
)

func createAllDirs(c *conf.CollectConf) error {
	var perms fs.FileMode = 0o750
	if !c.IsRESTCollect() {
		if err := os.MkdirAll(c.ConfigurationOutDir(), perms); err != nil {
			return fmt.Errorf("unable to create configuration directory %v: %w", c.ConfigurationOutDir(), err)
		}
		if err := os.MkdirAll(c.JFROutDir(), perms); err != nil {
			return fmt.Errorf("unable to create jfr directory: %w", err)
		}
		if err := os.MkdirAll(c.ThreadDumpsOutDir(), perms); err != nil {
			return fmt.Errorf("unable to create thread-dumps directory: %w", err)
		}
		if err := os.MkdirAll(c.HeapDumpsOutDir(), perms); err != nil {
			return fmt.Errorf("unable to create heap-dumps directory: %w", err)
		}
		if err := os.MkdirAll(c.KubernetesOutDir(), perms); err != nil {
			return fmt.Errorf("unable to create kubernetes directory: %w", err)
		}
		if err := os.MkdirAll(c.LogsOutDir(), perms); err != nil {
			return fmt.Errorf("unable to create logs directory: %w", err)
		}
		if err := os.MkdirAll(c.NodeInfoOutDir(), perms); err != nil {
			return fmt.Errorf("unable to create node-info directory: %w", err)
		}
		if err := os.MkdirAll(c.QueriesOutDir(), perms); err != nil {
			return fmt.Errorf("unable to create queries directory: %w", err)
		}
		if err := os.MkdirAll(c.TtopOutDir(), perms); err != nil {
			return fmt.Errorf("unable to create ttop directory: %w", err)
		}
	}
	if !c.IsDremioCloud() {
		if err := os.MkdirAll(c.KVstoreOutDir(), perms); err != nil {
			return fmt.Errorf("unable to create kvstore directory: %w", err)
		}
	}

	if err := os.MkdirAll(c.ClusterStatsOutDir(), perms); err != nil {
		return fmt.Errorf("unable to create cluster-stats directory: %w", err)
	}
	if err := os.MkdirAll(c.SystemTablesOutDir(), perms); err != nil {
		return fmt.Errorf("unable to create system-tables directory: %w", err)
	}
	if err := os.MkdirAll(c.WLMOutDir(), perms); err != nil {
		return fmt.Errorf("unable to create wlm directory: %w", err)
	}
	if err := os.MkdirAll(c.JobProfilesOutDir(), perms); err != nil {
		return fmt.Errorf("unable to create job-profiles directory: %w", err)
	}
	return nil
}

func collect(c *conf.CollectConf, hook shutdown.Hook) error {
	if !c.DisableFreeSpaceCheck() {
		if err := dirs.CheckFreeSpace(c.TarballOutDir(), uint64(c.MinFreeSpaceGB())); err != nil {
			return fmt.Errorf("%w. Use a larger directory by using ddc --transfer-dir or if using ddc local-collect --tarball-out-dir", err)
		}
	}
	if err := createAllDirs(c); err != nil {
		return fmt.Errorf("unable to create directories: %w", err)
	}

	// we can probably remove this now that we have gone to single threaded, but keeping it for the delayed execution and logging for now
	t, err := threading.NewThreadPool(1, 1, true, false)
	if err != nil {
		return fmt.Errorf("unable to spawn thread pool: %w", err)
	}

	wrapConfigJob := func(name string, j func(c *conf.CollectConf, h shutdown.CancelHook) error) threading.Job {
		return threading.Job{
			Name:    name,
			Process: func() error { return j(c, hook) },
		}
	}

	wrapConfigJobWithFileRemovalTasks := func(name string, j func(c *conf.CollectConf, h shutdown.Hook) error) threading.Job {
		return threading.Job{
			Name:    name,
			Process: func() error { return j(c, hook) },
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
	}

	if !c.IsRESTCollect() {
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

		if !c.CollectHSErrFiles() {
			simplelog.Debug("Skipping hs_err file collection")
		} else {
			t.AddJob(threading.Job{
				Name:    "HS ERR FILE COLLECTION",
				Process: logCollector.RunCollectHSErrFiles,
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

		if !c.CollectVacuumLogs() {
			simplelog.Debug("Skipping vacuum log collection")
		} else {
			t.AddJob(threading.Job{
				Name:    "VACUUM LOG COLLECTION",
				Process: logCollector.RunCollectVacuumLogs,
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
			t.AddJob(wrapConfigJob("TTOP COLLECTION", RunTtopCollect))
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
			t.AddJob(wrapConfigJobWithFileRemovalTasks("HEAP DUMP COLLECTION", jvmcollect.RunCollectHeapDump))
		}
	}

	if err := t.ProcessAndWait(); err != nil {
		simplelog.Errorf("thread pool has an error: %v", err)
	}

	// this has to happen after the queries.json collection so we don't have much choice and have to leave it here
	if c.NumberJobProfilesToCollect() == 0 {
		simplelog.Debugf("Skipping job profiles collection")
	} else {
		if err := apicollect.RunCollectJobProfiles(c, hook); err != nil {
			simplelog.Errorf("during job profile collection there was an error: %v", err)
		}
	}
	if err := runCollectClusterStats(c, hook); err != nil {
		simplelog.Errorf("during unable to collect cluster stats like cluster ID: %v", err)
	}
	return nil
}

func findClusterID(c *conf.CollectConf) (string, error) {
	tries := []string{c.DremioEndpoint()}
	if c.DremioEndpoint() == "http://localhost:9047" {
		// try https if the default is being used, this means it's likely unconfigured and we should try ssl as well
		tries = append(tries, "https://localhost:9047")
	}
	// ignore bad certs because dremio provides an easy way to use self signed certs
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402
	}
	client := &http.Client{Transport: tr}
	exec := func(url string) (string, error) {
		simplelog.Debugf("checking %v for cluster version", url)
		startTime := time.Now().Unix()
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(c.CollectSystemTablesTimeoutSeconds())*time.Second)
		defer cancel() // avoid leaks

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return "", err
		}
		res, err := client.Do(req)
		if err != nil {
			return "", err
		}
		if res.StatusCode != http.StatusOK {
			return "", fmt.Errorf("invalid response %v when trying to access %v", res.Status, url)
		}
		body, err := io.ReadAll(res.Body)
		if err != nil {
			return "", err
		}
		endTime := time.Now().Unix()
		seconds := endTime - startTime
		clusterID, err := parseClusterIDFromBody(string(body))
		if err != nil {
			return "", err
		}
		simplelog.Infof("found cluster ID '%v' in html from url %v in %v seconds", clusterID, url, seconds)
		return clusterID, nil
	}
	var results []string
	for i := 0; i < len(tries); i++ {
		url := tries[i]
		clusterID, err := exec(url)
		if err != nil {
			// TODO clean this up, we have some structure being passed to an unstructured error message
			results = append(results, fmt.Sprintf("{url: %v, error: %v}", url, err))
			continue
		}
		return clusterID, nil
	}

	return "", fmt.Errorf("unable to find client id tried the following urls with the following errors: %v", strings.Join(results, ","))
}

func parseClusterIDFromBody(body string) (string, error) {
	// Regular expression to extract the JSON string
	re := regexp.MustCompile(`JSON\.parse\('([^']+)'`)
	matches := re.FindStringSubmatch(body)
	if len(matches) < 2 {
		return "", fmt.Errorf("no cluster ID found in the http response %v", body)
	}

	// Decode the JSON string
	var config map[string]interface{}
	// remove the javascript double quote escapes
	cleaned := strings.ReplaceAll(matches[1], "\\\"", "\"")

	err := json.Unmarshal([]byte(cleaned), &config)
	if err != nil {
		return "", fmt.Errorf("unable to decode json '%v': %w", cleaned, err)
	}

	// Extract the clusterID
	clusterID, ok := config["clusterId"].(string)
	if !ok {
		return "", fmt.Errorf("clusterId not found in json: %v", matches[1])
	}
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

func getClassPath(hook shutdown.CancelHook, pid int) (string, error) {
	var w bytes.Buffer
	if err := ddcio.Shell(hook, &w, fmt.Sprintf("jcmd %v VM.system_properties", pid)); err != nil {
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
		return "", fmt.Errorf("unable to find version '%v': %w", out, scanner.Err())
	}
	return "", fmt.Errorf("no matches for java.class.path= found in '%v'", pid)
}

func runCollectClusterStats(c *conf.CollectConf, hook shutdown.CancelHook) error {
	simplelog.Debugf("Collecting cluster stats")
	classPath, err := getClassPath(hook, c.DremioPID())
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
	return os.WriteFile(filepath.Join(c.ClusterStatsOutDir(), "cluster-stats.json"), b, 0o600)
}

func RunTtopCollect(c *conf.CollectConf, hook shutdown.CancelHook) error {
	simplelog.Debug("Running top -H to get thread information")
	duration := c.DremioTtopTimeSeconds() / c.DremioTtopFreqSeconds()
	if duration == 0 {
		return errors.New("cannot have duration of 0 for ttop")
	}
	var w bytes.Buffer
	err := ddcio.Shell(hook, &w, fmt.Sprintf("LINES=100 top -H -n %v -p %v -d %v -bw", duration, c.DremioPID(), c.DremioTtopFreqSeconds()))
	if err != nil {
		return fmt.Errorf("failed collecting top: %w", err)
	}
	loc := fmt.Sprintf("%v/ttop.txt", c.TtopOutDir())
	if err := os.WriteFile(loc, w.Bytes(), 0o600); err != nil {
		return fmt.Errorf("unable to write top out: %w", err)
	}
	simplelog.Debugf("top -H written to %v", loc)
	return nil
}

func runCollectOSConfig(c *conf.CollectConf, hook shutdown.CancelHook) error {
	simplelog.Debug("Collecting OS Information")
	osInfoFile := filepath.Join(c.NodeInfoOutDir(), "os_info.txt")
	w, err := os.Create(filepath.Clean(osInfoFile))
	if err != nil {
		return fmt.Errorf("unable to create file %v: %w", filepath.Clean(osInfoFile), err)
	}

	simplelog.Debug("/etc/*-release")

	_, err = w.Write([]byte("___\n>>> cat /etc/*-release\n"))
	if err != nil {
		simplelog.Warningf("unable to write release file header for os_info.txt: %v", err)
	}

	err = ddcio.Shell(hook, w, "cat /etc/*-release")
	if err != nil {
		simplelog.Warningf("unable to write release files for os_info.txt: %v", err)
	}

	_, err = w.Write([]byte("___\n>>> uname -r\n"))
	if err != nil {
		simplelog.Warningf("unable to write uname header for os_info.txt: %v", err)
	}

	err = ddcio.Shell(hook, w, "uname -r")
	if err != nil {
		simplelog.Warningf("unable to write uname -r for os_info.txt: %v", err)
	}
	_, err = w.Write([]byte("___\n>>> cat /etc/issue\n"))
	if err != nil {
		simplelog.Warningf("unable to write cat /etc/issue header for os_info.txt: %v", err)
	}
	err = ddcio.Shell(hook, w, "cat /etc/issue")
	if err != nil {
		simplelog.Warningf("unable to write /etc/issue for os_info.txt: %v", err)
	}
	_, err = w.Write([]byte("___\n>>> cat /proc/sys/kernel/hostname\n"))
	if err != nil {
		simplelog.Warningf("unable to write hostname for os_info.txt: %v", err)
	}
	err = ddcio.Shell(hook, w, "cat /proc/sys/kernel/hostname")
	if err != nil {
		simplelog.Warningf("unable to write hostname for os_info.txt: %v", err)
	}
	_, err = w.Write([]byte("___\n>>> cat /proc/meminfo\n"))
	if err != nil {
		simplelog.Warningf("unable to write /proc/meminfo header for os_info.txt: %v", err)
	}
	err = ddcio.Shell(hook, w, "cat /proc/meminfo")
	if err != nil {
		simplelog.Warningf("unable to write /proc/meminfo for os_info.txt: %v", err)
	}
	_, err = w.Write([]byte("___\n>>> lscpu\n"))
	if err != nil {
		simplelog.Warningf("unable to write lscpu header for os_info.txt: %v", err)
	}
	err = ddcio.Shell(hook, w, "lscpu")
	if err != nil {
		simplelog.Warningf("unable to write lscpu for os_info.txt: %v", err)
	}
	_, err = w.Write([]byte("___\n>>> mount\n"))
	if err != nil {
		simplelog.Warningf("unable to write mount header for os_info.txt: %v", err)
	}
	err = ddcio.Shell(hook, w, "mount")
	if err != nil {
		simplelog.Warningf("unable to write mount for os_info.txt: %v", err)
	}
	_, err = w.Write([]byte("___\n>>> lsblk\n"))
	if err != nil {
		simplelog.Warningf("unable to write lsblk header for os_info.txt: %v", err)
	}
	err = ddcio.Shell(hook, w, "lsblk")
	if err != nil {
		simplelog.Warningf("unable to write lsblk for os_info.txt: %v", err)
	}
	const s = `stat -fc %T /sys/fs/cgroup/`
	_, err = w.Write([]byte(fmt.Sprintf("___\n>>> %v\n", s)))
	if err != nil {
		simplelog.Warningf("unable to write %s header for os_info.txt: %v", s, err)
	}
	err = ddcio.Shell(hook, w, s)
	if err != nil {
		simplelog.Warningf("unable to write %s for os_info.txt: %v", s, err)
	}

	// this only retrieves cgroupv2 files and will fail on cgroup1 and of course on prem
	cgroupFiles := []string{
		"memory.current",
		"memory.swap.current",
		"memory.pressure",
		"cpu.pressure",
		"io.pressure",
	}
	for _, cgroupFile := range cgroupFiles {
		commandToExecute := fmt.Sprintf("cat /sys/fs/cgroup/%v", cgroupFile)
		_, err = w.Write([]byte(fmt.Sprintf("___\n>>> %v\n", commandToExecute)))
		if err != nil {
			simplelog.Warningf("unable to write %s header for os_info.txt: %v", commandToExecute, err)
		}
		err = ddcio.Shell(hook, w, commandToExecute)
		if err != nil {
			simplelog.Warningf("unable to write %s for os_info.txt: %v", commandToExecute, err)
		}
	}

	loadCommand := "cat /proc/loadavg"
	_, err = w.Write([]byte(fmt.Sprintf("___\n>>> %v\n", loadCommand)))
	if err != nil {
		simplelog.Warningf("unable to write %s header for os_info.txt: %v", s, err)
	}
	err = ddcio.Shell(hook, w, loadCommand)
	if err != nil {
		simplelog.Warningf("unable to write %s for os_info.txt: %v", s, err)
	}

	if c.DremioPID() > 0 {
		_, err = w.Write([]byte("___\n>>> ps eww\n"))
		if err != nil {
			simplelog.Warningf("unable to write ps eww header for os_info.txt: %v", err)
		}
		err = ddcio.Shell(hook, w, fmt.Sprintf("ps eww %v | grep dremio | awk '{$1=$2=$3=$4=\"\"; print $0}'", c.DremioPID()))
		if err != nil {
			simplelog.Warningf("unable to write ps eww output for os_info.txt: %v", err)
		}
	}
	if err := w.Sync(); err != nil {
		return fmt.Errorf("unable to sync the os_info.txt file: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("unable to close the os_info.txt file: %w", err)
	}
	simplelog.Debugf("... Collecting OS Information from %v COMPLETED", c.NodeName())

	return nil
}

var LocalCollectCmd = &cobra.Command{
	Use:   "local-collect",
	Short: "Retrieves all the dremio logs and diagnostics for the local node and saves the results in a compatible format for Dremio support",
	Long:  `Retrieves all the dremio logs and diagnostics for the local node and saves the results in a compatible format for Dremio support. This subcommand needs to be run with enough permissions to read the /proc filesystem, the dremio logs and configuration files`,
	Run: func(cobraCmd *cobra.Command, args []string) {
		overrides := make(map[string]string)
		// if a cli flag was set go ahead and use those values to override the yaml configuration
		cobraCmd.Flags().Visit(func(flag *pflag.Flag) {
			if flag.Name == conf.KeyDremioPatToken {
				if flag.Value.String() == "" {
					pat, err := masking.PromptForPAT()
					if err != nil {
						fmt.Printf("unable to get PAT: %v\n", err)
						os.Exit(1)
					}
					overrides[flag.Name] = pat
				} else {
					overrides[flag.Name] = flag.Value.String()
				}
				// we do not want to log the token
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
	hook := shutdown.NewHook()
	defer hook.Cleanup()
	cSignal := make(chan os.Signal, 1)
	signal.Notify(cSignal, os.Interrupt, syscall.SIGTERM)
	killOnlyCleanup := func() {}
	go func() {
		<-cSignal
		simplelog.Infof("graceful shutdown initiated")
		hook.Cleanup()
		simplelog.Infof("removing tarball out folder if present")
		killOnlyCleanup()
		simplelog.Infof("cleanup complete")
		os.Exit(1)
	}()
	simplelog.Infof("ddc local-collect version: %v", versions.GetCLIVersion())
	simplelog.Infof("args: %v", strings.Join(args, " "))
	fmt.Println(strings.TrimSpace(versions.GetCLIVersion()))
	if pid != "" {
		if _, err := os.Stat(pid); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return "", fmt.Errorf("unable to read pid location '%v': %w", pid, err)
			}
			// this means nothing is present great continue
			if err := os.WriteFile(filepath.Clean(pid), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
				return "", fmt.Errorf("unable to write pid file '%v': %w", pid, err)
			}
			hook.AddFinalSteps(func() {
				if err := os.Remove(pid); err != nil {
					msg := fmt.Sprintf("unable to remove pid '%v': '%v', it will need to be removed manually", pid, err)
					fmt.Println(msg)
					simplelog.Error(msg)
				}
			}, fmt.Sprintf("removing pid file %v", pid))
		} else {
			return "", fmt.Errorf("DDC is running based on pid file '%v'. If this is a stale file then please remove", pid)
		}
	}
	startTime := time.Now().Unix()
	if err := validation.ValidateCollectMode(collectionMode); err != nil {
		return "", err
	}

	c, err := conf.ReadConf(hook, overrides, ddcYamlLoc, collectionMode)
	if err != nil {
		return "", fmt.Errorf("unable to read configuration %w", err)
	}
	killOnlyCleanup = func() {
		if err := os.RemoveAll(c.OutputDir()); err != nil {
			simplelog.Errorf("unable to cleanup %v: %v", c.OutputDir(), err)
		}
		if err := os.RemoveAll(c.TarballOutDir()); err != nil {
			simplelog.Errorf("unable to cleanup %v: %v", c.TarballOutDir(), err)
		}
	}
	if !c.IsRESTCollect() {
		fmt.Println("looking for logs in: " + c.DremioLogDir())
	}

	// Run application
	simplelog.Info("Starting collection...")
	if err := collect(c, hook); err != nil {
		return "", fmt.Errorf("unable to collect: %w", err)
	}

	logLoc := simplelog.GetLogLoc()
	if logLoc != "" {
		if err := ddcio.CopyFile(simplelog.GetLogLoc(), filepath.Join(c.OutputDir(), fmt.Sprintf("ddc-%v.log", c.NodeName()))); err != nil {
			simplelog.Warningf("unable to copy log to archive: %v", err)
		}
	}
	tarballName := filepath.Join(c.TarballOutDir(), c.NodeName()+".tar.gz")
	simplelog.Debugf("collection complete. Archiving %v to %v...", c.OutputDir(), tarballName)
	if err := archive.TarGzDir(c.OutputDir(), tarballName); err != nil {
		return "", fmt.Errorf("unable to compress archive from folder '%v': %w", c.OutputDir(), err)
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
	// wire up override flags
	LocalCollectCmd.Flags().CountP("verbose", "v", "Logging verbosity")
	LocalCollectCmd.Flags().String("dremio-pat-token", "	", "Dremio Personal Access Token (PAT)")
	LocalCollectCmd.Flags().String("tarball-out-dir", "/tmp/ddc", "directory where the final <hostname>.tar.gz file is placed. This is also the location where final archive will be output for pickup by the ddc command")
	LocalCollectCmd.Flags().Bool(conf.KeyDisableFreeSpaceCheck, false, "disables the free space check for the --tarball-out-dir")
	LocalCollectCmd.Flags().Bool(conf.KeyNoLogDir, false, "when enabled then the process will not fail if the log directory is invalid. This is useful if one just wants to collect kubernetes log output and diagnostics information")
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
	LocalCollectCmd.Flags().StringVar(&collectionMode, "collect", "light", "type of collection: 'light'- 2 days of logs (no top, jstack or jfr). 'standard' - includes jfr, top, 7 days of logs and 30 days of queries.json logs. 'standard+jstack' - all of 'standard' plus jstack. 'health-check' - all of 'standard' + WLM, KV Store Report, 25,000 Job Profiles")
}
