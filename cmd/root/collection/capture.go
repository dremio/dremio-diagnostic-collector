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

// collection package provides the interface for collection implementation and the actual collection execution
package collection

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/root/ddcbinary"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/consoleprint"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/shutdown"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/simplelog"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/strutils"
)

type FindErr struct {
	Cmd string
}

func (fe FindErr) Error() string {
	return fmt.Sprintf("find failed: %v:", fe.Cmd)
}

func matchToConst(jobText string) string {
	switch jobText {
	case "DISK USAGE COLLECTION":
		return consoleprint.DiskUsage
	case "DREMIO CONFIG COLLECTION":
		return consoleprint.DremioConfig
	case "OS CONFIG COLLECTION":
		return consoleprint.OSConfig
	case "QUERIES.JSON COLLECTION":
		return consoleprint.Queries
	case "SERVER LOG COLLECTION":
		return consoleprint.ServerLog
	case "GC LOG COLLECTION":
		return consoleprint.GcLog
	case "JFR COLLECTION":
		return consoleprint.Jfr
	case "JSTACK COLLECTION":
		return consoleprint.Jstack
	case "JVM FLAG COLLECTION":
		return consoleprint.JVMFlags
	case "METADATA LOG COLLECTION":
		return consoleprint.MetadataLog
	case "REFLECTING LOG COLLECTION":
		return consoleprint.ReflectionLog
	case "TTOP COLLECTION":
		return consoleprint.Ttop
	case "ACCELERATION LOG COLLECTION":
		return consoleprint.AccelerationLog
	case "ACCESS LOG COLLECTION":
		return consoleprint.AccessLog
	case "AUDIT LOG COLLECTION":
		return consoleprint.AuditLog
	case "JOB PROFILES COLLECTION":
		return consoleprint.JobProfiles
	case "KV STORE COLLECTION":
		return consoleprint.KVStore
	case "SYSTEM TABLE COLLECTION":
		return consoleprint.SystemTable
	case "WLM COLLECTION":
		return consoleprint.Wlm
	case "HEAP DUMP COLLECTION":
		return consoleprint.HeapDump
	default:
		// try and guess
		return strings.ReplaceAll(jobText, " ", "_")
	}
}

func extractJobText(line string) (status string, statusUX string) {
	statusUX = strings.TrimSpace(strings.TrimPrefix(line, "JOB START - "))
	status = matchToConst(statusUX)
	return
}

func extractJobFailedText(line string) (status string, statusUX string, message string) {
	text := strings.TrimPrefix(line, "JOB FAILED - ")
	for _, r := range text {
		if r == '-' {
			statusUX = strings.TrimSpace(statusUX)
			break
		}
		statusUX += string(r)
	}
	status = matchToConst(statusUX)
	message = strings.TrimSpace(strings.TrimPrefix(text, statusUX+" - "))
	return
}

func extractJobProgressText(line string) (status string, statusUX string, message string) {
	status = consoleprint.JobProfiles
	statusUX = "JOB PROFILE DOWNLOAD"
	message = strings.TrimSpace(strings.TrimPrefix(line, "JOB PROGRESS - "))
	return
}

// valid status list

// Capture collects diagnostics, conf files and log files from the target hosts. Failures are permissive and
// are first logged and then returned at the end with the reason for the failure.
func StartCapture(c HostCaptureConfiguration, ddcBinaryInfo ddcbinary.BinaryInfo, localDDCYamlPath string, skipRESTCollect bool, disableFreeSpaceCheck bool, minFreeSpaceGB uint64) error {
	host := c.Host
	nodeState := consoleprint.NodeState{
		Node:          host,
		Status:        consoleprint.Starting,
		StatusUX:      "STARTING",
		Result:        consoleprint.ResultPending,
		IsCoordinator: c.IsCoordinator,
	}
	consoleprint.UpdateNodeState(nodeState)
	simplelog.HostLog(host, fmt.Sprintf("%#v", nodeState))
	// we cannot use filepath.join here as it will break everything during the transfer
	pathToDDC := path.Join(c.TransferDir, "ddc")
	// we cannot use filepath.join here as it will break everything during the transfer
	pathToDDCYAML := path.Join(c.TransferDir, "ddc.yaml")
	dremioPAT := c.DremioPAT
	versionMatch := false
	// if versions don't match go ahead and install a copy in the ddc tmp directory
	if !versionMatch {
		nodeState = consoleprint.NodeState{
			Node:     host,
			Status:   consoleprint.CreatingRemoteDir,
			StatusUX: "CREATING REMOTE DIR",
			Result:   consoleprint.ResultPending,
		}
		consoleprint.UpdateNodeState(nodeState)
		simplelog.HostLog(host, fmt.Sprintf("%#v", nodeState))

		// remotely make TransferDir
		if out, err := c.Collector.HostExecute(false, c.Host, "mkdir", "-p", c.TransferDir); err != nil {
			nodeState = consoleprint.NodeState{
				Node:            host,
				Status:          consoleprint.CreatingRemoteDir,
				Result:          consoleprint.ResultFailure,
				EndProcess:      true,
				EndProcessError: fmt.Sprintf("'%v' - '%v'", err, out),
			}
			consoleprint.UpdateNodeState(nodeState)
			simplelog.HostLog(host, fmt.Sprintf("%#v", nodeState))
			return fmt.Errorf("host %v unable to make dir %v: '%w' - '%v'", host, c.TransferDir, err, out)
		}

		nodeState = consoleprint.NodeState{
			Node:     host,
			Status:   consoleprint.CopyDDCToHost,
			StatusUX: "COPY DDC TO HOST",
			Result:   consoleprint.ResultPending,
		}
		consoleprint.UpdateNodeState(nodeState)
		simplelog.HostLog(host, fmt.Sprintf("%#v", nodeState))
		cpuArch, err := c.Collector.HostExecute(false, c.Host, "uname", "-m")
		if err != nil {
			return fmt.Errorf("unable to read cpu architecture of %v: %w", c.Host, err)
		}
		var localDDCPath string
		if cpuArch == "aarch64" {
			localDDCPath = ddcBinaryInfo.ArmBinaryLocation
		} else {
			localDDCPath = ddcBinaryInfo.IntelBinaryLocation
		}
		// copy file to TransferDir assume there is
		if out, err := c.Collector.CopyToHost(c.Host, localDDCPath, pathToDDC); err != nil {
			nodeState = consoleprint.NodeState{
				Node:            host,
				Status:          consoleprint.CopyDDCToHost,
				StatusUX:        "COPY DDC TO HOST",
				Result:          consoleprint.ResultFailure,
				EndProcess:      true,
				EndProcessError: fmt.Sprintf("'%v' - '%v'", err, out),
			}
			consoleprint.UpdateNodeState(nodeState)
			simplelog.HostLog(host, fmt.Sprintf("%#v", nodeState))
			return fmt.Errorf("unable to copy local ddc %v to remote path: '%w' - '%v'", localDDCPath, err, out)
			// this is a critical error so it is safe to exit
		}
		simplelog.Infof("successfully copied ddc to host %v at %v", host, pathToDDC)
		defer func() {
			// clear out when done
			if out, err := c.Collector.HostExecute(false, c.Host, "rm", pathToDDC); err != nil {
				simplelog.Warningf("on host %v unable to remove ddc: '%v' - '%v'", host, err, out)
			}
		}()
		defer func() {
			// clear out w&hen done
			if out, err := c.Collector.HostExecute(false, c.Host, "rm", pathToDDC+".log"); err != nil {
				simplelog.Warningf("on host %v unable to remove ddc.log: '%v' - '%v'", host, err, out)
			}
		}()
		nodeState = consoleprint.NodeState{
			Node:     host,
			Status:   consoleprint.SettingDDCPermissions,
			StatusUX: "SETTING DDC PERMISSIONS",
			Result:   consoleprint.ResultPending,
		}
		consoleprint.UpdateNodeState(nodeState)
		simplelog.HostLog(host, fmt.Sprintf("%#v", nodeState))
		// make  exec TransferDir
		if out, err := c.Collector.HostExecute(false, c.Host, "chmod", "+x", pathToDDC); err != nil {
			nodeState = consoleprint.NodeState{
				Node:            host,
				Status:          consoleprint.SettingDDCPermissions,
				StatusUX:        "SETTING DDC PERMISSIONS",
				Result:          consoleprint.ResultFailure,
				EndProcess:      true,
				EndProcessError: fmt.Sprintf("'%v' - '%v'", err, out),
			}
			consoleprint.UpdateNodeState(nodeState)
			simplelog.HostLog(host, fmt.Sprintf("%#v", nodeState))
			return fmt.Errorf("host %v unable to make ddc exec %v and cannot proceed with capture: '%w' - '%v'", host, pathToDDC, err, out)
		}
	}
	nodeState = consoleprint.NodeState{
		Node:     host,
		Status:   consoleprint.CopyDDCYaml,
		StatusUX: "COPY DDC YAML",
		Result:   consoleprint.ResultPending,
	}
	consoleprint.UpdateNodeState(nodeState)
	simplelog.HostLog(host, fmt.Sprintf("%#v", nodeState))
	// always update the configuration
	if out, err := c.Collector.CopyToHost(c.Host, localDDCYamlPath, pathToDDCYAML); err != nil {
		nodeState := consoleprint.NodeState{
			Node:            host,
			Status:          consoleprint.CopyDDCYaml,
			StatusUX:        "COPY DDC YAML",
			Result:          consoleprint.ResultFailure,
			EndProcess:      true,
			EndProcessError: fmt.Sprintf("'%v' - '%v'", err, out),
		}
		consoleprint.UpdateNodeState(nodeState)
		simplelog.HostLog(host, fmt.Sprintf("%#v", nodeState))
		return fmt.Errorf("unable to copy local ddc yaml '%v' to remote path: '%w' - '%v'", localDDCYamlPath, err, out)
		// this is a critical step and will not work without it so exit
	}
	simplelog.Infof("successfully copied ddc.yaml to host %v at %v", host, pathToDDCYAML)
	defer func() {
		// clear out when done
		if out, err := c.Collector.HostExecute(false, c.Host, "rm", pathToDDCYAML); err != nil {
			simplelog.Warningf("on host %v unable to do initial cleanup capture: '%v' - '%v'", host, err, out)
		}
	}()

	nodeState = consoleprint.NodeState{
		Node:     host,
		Status:   consoleprint.Collecting,
		StatusUX: "COLLECTING",
		Result:   consoleprint.ResultPending,
	}
	consoleprint.UpdateNodeState(nodeState)
	simplelog.HostLog(host, fmt.Sprintf("%#v", nodeState))

	// execute local-collect with a tarball-out-dir flag it must match our transfer-dir flag
	var mask bool // to mask PAT token in logs
	pidFile := path.Join(c.TransferDir, "ddc.pid")
	c.Collector.SetHostPid(c.Host, pidFile)
	localCollectArgs := []string{pathToDDC, "local-collect", fmt.Sprintf("--%v", conf.KeyTarballOutDir), c.TransferDir, fmt.Sprintf("--%v", conf.KeyCollectionMode), c.CollectionMode, fmt.Sprintf("--%v", conf.KeyMinFreeSpaceGB), fmt.Sprintf("%v", minFreeSpaceGB), "--pid", pidFile}
	if disableFreeSpaceCheck {
		localCollectArgs = append(localCollectArgs, fmt.Sprintf("--%v", conf.KeyDisableFreeSpaceCheck))
	}
	if c.NoLogDir {
		localCollectArgs = append(localCollectArgs, "--no-log-dir")
	}
	if dremioPAT != "" {
		// if the dremio PAT is set, set the pat-stdin value so we can pass it in via that mechanism
		localCollectArgs = append(localCollectArgs, "--pat-stdin")
		mask = true
	} else {
		mask = false
	}

	var allHostLog []string
	var lastLine string
	err := c.Collector.HostExecuteAndStream(mask, c.Host, func(line string) {
		if strings.HasPrefix(line, "JOB START") {
			status, statusUX := extractJobText(line)
			nodeState := consoleprint.NodeState{
				Node:     c.Host,
				Status:   status,
				StatusUX: statusUX,
				Result:   consoleprint.ResultPending,
			}
			consoleprint.UpdateNodeState(nodeState)
			simplelog.HostLog(host, fmt.Sprintf("%#v", nodeState))
		} else if strings.HasPrefix(line, "JOB FAILED") {
			status, statusUX, _ := extractJobFailedText(line)
			nodeState := consoleprint.NodeState{
				Node:     c.Host,
				Status:   status,
				StatusUX: statusUX,
				Result:   consoleprint.ResultFailure,
			}
			consoleprint.UpdateNodeState(nodeState)
			simplelog.HostLog(host, fmt.Sprintf("%#v", nodeState))
		} else if strings.HasPrefix(line, "JOB PROGRESS") {
			status, statusUX, progressMessage := extractJobProgressText(line)
			nodeState := consoleprint.NodeState{
				Node:     c.Host,
				Status:   status,
				StatusUX: statusUX,
				Result:   consoleprint.ResultPending,
				Progress: progressMessage,
			}
			consoleprint.UpdateNodeState(nodeState)
			simplelog.HostLog(host, fmt.Sprintf("%#v", nodeState))
		} else {
			lastLine = line
			allHostLog = append(allHostLog, line)
		}
		if strings.Contains(line, "AUTODETECTION DISABLED") {
			simplelog.HostLog(host, "autodetection disabled")
		}
		simplelog.HostLog(host, line)
	}, dremioPAT, localCollectArgs...)
	if err != nil {

		nodeState := consoleprint.NodeState{
			Node:            c.Host,
			Status:          consoleprint.Collecting,
			StatusUX:        "LOCAL-COLLECT",
			Result:          consoleprint.ResultFailure,
			EndProcess:      true,
			EndProcessError: fmt.Sprintf("'%v' - '%v'", err, strutils.TruncateString(lastLine, 1024)),
		}
		consoleprint.UpdateNodeState(nodeState)
		simplelog.HostLog(host, fmt.Sprintf("%#v", nodeState))
		return fmt.Errorf("on host %v capture failed: '%w' - %v", host, err, strings.Join(allHostLog, "\n"))
	}

	simplelog.Debugf("on host %v capture successful", host)
	nodeState = consoleprint.NodeState{
		Node:     c.Host,
		Status:   consoleprint.CollectingAwaitingTransfer,
		StatusUX: "COLLECTED - AWAITING TRANSFER",
		Result:   consoleprint.ResultPending,
	}
	consoleprint.UpdateNodeState(nodeState)
	simplelog.HostLog(host, fmt.Sprintf("%#v", nodeState))
	return nil
}

func TransferCapture(c HostCaptureConfiguration, hook shutdown.Hook, outputLoc string) (int64, string, error) {
	hostname, err := c.Collector.HostExecute(false, c.Host, "cat", "/proc/sys/kernel/hostname")
	if err != nil {
		nodeState := consoleprint.NodeState{
			Node:            c.Host,
			Status:          consoleprint.Collecting,
			StatusUX:        "COLLECT HOSTNAME",
			Result:          consoleprint.ResultFailure,
			EndProcess:      true,
			EndProcessError: fmt.Sprintf("'%v' - '%v'", err, hostname),
		}
		consoleprint.UpdateNodeState(nodeState)
		simplelog.HostLog(hostname, fmt.Sprintf("%#v", nodeState))
		return 0, c.Host, fmt.Errorf("on host %v cannot detect real hostname so unable to proceed with capture: %w", c.Host, err)
	}

	// copy tar.gz back
	tgzFileName := fmt.Sprintf("%v.tar.gz", strings.TrimSpace(hostname))
	// IMPORTANT we must use path.join and not filepath.join or everything will break
	tarGZ := path.Join(c.TransferDir, tgzFileName)

	outDir := path.Dir(outputLoc)
	if outDir == "" {
		outDir = fmt.Sprintf(".%v", filepath.Separator)
	}
	nodeState := consoleprint.NodeState{
		Node:     c.Host,
		Status:   consoleprint.TarballTransfer,
		StatusUX: "TARBALL TRANSFER",
		Result:   consoleprint.ResultPending,
	}
	consoleprint.UpdateNodeState(nodeState)
	simplelog.HostLog(hostname, fmt.Sprintf("%#v", nodeState))
	tarballCleanup := func() {
		if out, err := c.Collector.HostExecute(false, c.Host, "rm", tarGZ); err != nil {
			simplelog.Warningf("on host %v unable to cleanup remote capture: '%v' - '%v'", c.Host, err, out)
		} else {
			simplelog.Debugf("on host %v file %v has been removed", c.Host, tarGZ)
		}
	}
	hook.AddFinalSteps(tarballCleanup, fmt.Sprintf("removing tarball %v on host %v", tarGZ, c.Host))
	destFile := filepath.Join(outDir, tgzFileName)
	if out, err := c.Collector.CopyFromHost(c.Host, tarGZ, destFile); err != nil {
		nodeState := consoleprint.NodeState{
			Node:            c.Host,
			Status:          consoleprint.TarballTransfer,
			StatusUX:        "TARBALL TRANSFER",
			Result:          consoleprint.ResultFailure,
			EndProcess:      true,
			EndProcessError: fmt.Sprintf("'%v' - '%v'", err, out),
		}
		consoleprint.UpdateNodeState(nodeState)
		simplelog.HostLog(hostname, fmt.Sprintf("%#v", nodeState))
		return 0, destFile, fmt.Errorf("unable to copy file %v from host %v to directory %v: '%w' - '%v'", tarGZ, c.Host, outDir, err, out)
	}
	// cleanup tarball ASAP
	tarballCleanup()

	hook.AddFinalSteps(func() {
		if _, err := os.Stat(outputLoc); err == nil {
			if err := os.Remove(outputLoc); err != nil {
				simplelog.Warningf("unable to cleanup tgz %v: %v", outputLoc, err)
			}
			simplelog.Debugf("on host %v file %v has been removed", c.Host, tarGZ)
		}
	}, fmt.Sprintf("removing local tarball if present %v", tarGZ))

	fileInfo, err := c.DDCfs.Stat(destFile)
	// we assume a file size of zero if we are not able to retrieve the file size for some reason
	size := int64(0)
	if err != nil {
		simplelog.Warningf("cannot get file size for file %v: %v", destFile, err)
	} else {
		size = fileInfo.Size()
	}
	nodeState = consoleprint.NodeState{
		Node:       c.Host,
		Status:     consoleprint.Completed,
		StatusUX:   "COMPLETED",
		EndProcess: true,
		Result:     consoleprint.ResultPending,
	}
	consoleprint.UpdateNodeState(nodeState)
	simplelog.HostLog(hostname, fmt.Sprintf("%#v", nodeState))
	simplelog.Infof("host %v copied %v to %v it was %v bytes", c.Host, tarGZ, destFile, size)

	return size, destFile, nil
}
