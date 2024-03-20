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
	"path"
	"path/filepath"
	"strings"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/pkg/consoleprint"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
	"github.com/dremio/dremio-diagnostic-collector/pkg/strutils"
)

type FindErr struct {
	Cmd string
}

func (fe FindErr) Error() string {
	return fmt.Sprintf("find failed due to error %v:", fe.Cmd)
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

//valid status list

// Capture collects diagnostics, conf files and log files from the target hosts. Failures are permissive and
// are first logged and then returned at the end with the reason for the failure.
func StartCapture(c HostCaptureConfiguration, localDDCPath, localDDCYamlPath string, skipRESTCollect bool, disableFreeSpaceCheck bool, minFreeSpaceGB int) error {
	host := c.Host
	consoleprint.UpdateNodeState(consoleprint.NodeState{
		Node:    host,
		Message: "STARTING",
		Status:  consoleprint.Starting,
	})
	// we cannot use filepath.join here as it will break everything during the transfer
	pathToDDC := path.Join(c.TransferDir, "ddc")
	// we cannot use filepath.join here as it will break everything during the transfer
	pathToDDCYAML := path.Join(c.TransferDir, "ddc.yaml")
	dremioPAT := c.DremioPAT
	versionMatch := false
	// if out, err := ComposeExecute(conf, []string{pathToDDC, "version"}); err != nil {
	// 	simplelog.Warningf("host %v unable to find ddc version due to error '%v' with output '%v'", host, err, out)
	// } else {
	// 	simplelog.Infof("host %v has ddc version '%v' already installed", host, out)
	// 	versionMatch = out == versions.GetDDCRuntimeVersion()
	// }
	//if versions don't match go ahead and install a copy in the ddc tmp directory
	if !versionMatch {
		consoleprint.UpdateNodeState(consoleprint.NodeState{
			Node:     host,
			Status:   consoleprint.CreatingRemoteDir,
			StatusUX: "CREATING REMOTE DIR",
			Result:   consoleprint.ResultPending,
		})
		//remotely make TransferDir
		if out, err := c.Collector.HostExecute(false, c.Host, "mkdir", "-p", c.TransferDir); err != nil {
			consoleprint.UpdateNodeState(consoleprint.NodeState{
				Node:       host,
				Status:     consoleprint.CreatingRemoteDir,
				Message:    fmt.Sprintf("(%v) %v", err, out),
				Result:     consoleprint.ResultFailure,
				EndProcess: true,
			})
			return fmt.Errorf("host %v unable to make dir %v due to error '%v' with output '%v'", host, c.TransferDir, err, out)
		}

		consoleprint.UpdateNodeState(consoleprint.NodeState{
			Node:     host,
			Status:   consoleprint.CopyDDCToHost,
			StatusUX: "COPY DDC TO HOST",
			Result:   consoleprint.ResultPending,
		})
		//copy file to TransferDir assume there is
		if out, err := c.Collector.CopyToHost(c.Host, localDDCPath, pathToDDC); err != nil {
			consoleprint.UpdateNodeState(consoleprint.NodeState{
				Node:       host,
				Status:     consoleprint.CopyDDCToHost,
				StatusUX:   "COPY DDC TO HOST",
				Result:     consoleprint.ResultFailure,
				Message:    fmt.Sprintf("(%v) %v", err, out),
				EndProcess: true,
			})
			return fmt.Errorf("unable to copy local ddc %v to remote path due to error: '%v' with output '%v'", localDDCPath, err, out)
			//this is a critical error so it is safe to exit
		}
		simplelog.Infof("successfully copied ddc to host %v at %v", host, pathToDDC)
		defer func() {
			// clear out when done
			if out, err := c.Collector.HostExecute(false, c.Host, "rm", pathToDDC); err != nil {
				simplelog.Warningf("on host %v unable to remove ddc due to error '%v' with output '%v'", host, err, out)
			}
		}()
		defer func() {
			// clear out w&hen done
			if out, err := c.Collector.HostExecute(false, c.Host, "rm", pathToDDC+".log"); err != nil {
				simplelog.Warningf("on host %v unable to remove ddc.log due to error '%v' with output '%v'", host, err, out)
			}
		}()
		consoleprint.UpdateNodeState(consoleprint.NodeState{
			Node:     host,
			Status:   consoleprint.SettingDDCPermissions,
			StatusUX: "SETTING DDC PERMISSIONS",
			Result:   consoleprint.ResultPending,
		})
		//make  exec TransferDir
		if out, err := c.Collector.HostExecute(false, c.Host, "chmod", "+x", pathToDDC); err != nil {
			consoleprint.UpdateNodeState(consoleprint.NodeState{
				Node:       host,
				Status:     consoleprint.SettingDDCPermissions,
				StatusUX:   "SETTING DDC PERMISSIONS",
				Result:     consoleprint.ResultFailure,
				Message:    fmt.Sprintf("(%v) %v", err, out),
				EndProcess: true,
			})
			return fmt.Errorf("host %v unable to make ddc exec %v and cannot proceed with capture due to error '%v' with output '%v'", host, pathToDDC, err, out)
		}
	}
	consoleprint.UpdateNodeState(consoleprint.NodeState{
		Node:     host,
		Status:   consoleprint.CopyDDCYaml,
		StatusUX: "COPY DDC YAML",
		Result:   consoleprint.ResultPending,
	})
	//always update the configuration
	if out, err := c.Collector.CopyToHost(c.Host, localDDCYamlPath, pathToDDCYAML); err != nil {
		consoleprint.UpdateNodeState(consoleprint.NodeState{
			Node:       host,
			Status:     consoleprint.CopyDDCYaml,
			StatusUX:   "COPY DDC YAML",
			Result:     consoleprint.ResultFailure,
			Message:    fmt.Sprintf("(%v) %v", err, out),
			EndProcess: true,
		})
		return fmt.Errorf("unable to copy local ddc yaml '%v' to remote path due to error: '%v' with output '%v'", localDDCYamlPath, err, out)
		//this is a critical step and will not work without it so exit
	}
	simplelog.Infof("successfully copied ddc.yaml to host %v at %v", host, pathToDDCYAML)
	defer func() {
		// clear out when done
		if out, err := c.Collector.HostExecute(false, c.Host, "rm", pathToDDCYAML); err != nil {
			simplelog.Warningf("on host %v unable to do initial cleanup capture due to error '%v' with output '%v'", host, err, out)
		}
	}()

	consoleprint.UpdateNodeState(consoleprint.NodeState{
		Node:     host,
		Status:   consoleprint.Collecting,
		StatusUX: "COLLECTING",
		Result:   consoleprint.ResultPending,
	})
	//execute local-collect with a tarball-out-dir flag it must match our transfer-dir flag
	var mask bool // to mask PAT token in logs
	localCollectArgs := []string{pathToDDC, "local-collect", fmt.Sprintf("--%v", conf.KeyTarballOutDir), c.TransferDir, fmt.Sprintf("--%v", conf.KeyCollectionMode), c.CollectionMode, fmt.Sprintf("--%v", conf.KeyMinFreeSpaceGB), fmt.Sprintf("%v", minFreeSpaceGB)}
	if disableFreeSpaceCheck {
		localCollectArgs = append(localCollectArgs, fmt.Sprintf("--%v", conf.KeyDisableFreeSpaceCheck))
	}
	if skipRESTCollect {
		//if skipRESTCollect is set blank the pat
		localCollectArgs = append(localCollectArgs, fmt.Sprintf("--%v", conf.KeyDisableRESTAPI))
	} else if dremioPAT != "" {
		//if the dremio PAT is set, mask its logging then go ahead and pass it in
		localCollectArgs = append(localCollectArgs, "--dremio-pat-token", dremioPAT)
		mask = true
	} else {
		mask = false
	}
	var allHostLog []string
	err := c.Collector.HostExecuteAndStream(mask, c.Host, func(line string) {
		if strings.HasPrefix(line, "JOB START") {
			status, statusUX := extractJobText(line)
			consoleprint.UpdateNodeState(consoleprint.NodeState{
				Node:     c.Host,
				Status:   status,
				StatusUX: statusUX,
				Result:   consoleprint.ResultPending,
			})
		} else if strings.HasPrefix(line, "JOB FAILED") {
			status, statusUX, message := extractJobFailedText(line)
			consoleprint.UpdateNodeState(consoleprint.NodeState{
				Node:     c.Host,
				Message:  message,
				Status:   status,
				StatusUX: statusUX,
				Result:   consoleprint.ResultFailure,
			})
		} else if strings.HasPrefix(line, "JOB PROGRESS") {
			status, statusUX, message := extractJobProgressText(line)
			consoleprint.UpdateNodeState(consoleprint.NodeState{
				Node:     c.Host,
				Message:  message,
				Status:   status,
				StatusUX: statusUX,
				Result:   consoleprint.ResultPending,
			})
		} else {
			allHostLog = append(allHostLog, line)
		}
		if strings.Contains(line, "AUTODETECTION DISABLED") {
			consoleprint.UpdateNodeAutodetectDisabled(host, true)
		}
		simplelog.HostLog(host, line)
	}, localCollectArgs...)
	if err != nil {
		consoleprint.UpdateNodeState(consoleprint.NodeState{
			Node:       c.Host,
			Status:     consoleprint.Collecting,
			StatusUX:   "LOCAL-COLLECT",
			Result:     consoleprint.ResultFailure,
			EndProcess: true,
			Message:    strutils.LimitString(strings.Join(allHostLog, " - "), 1024),
		})
		return fmt.Errorf("on host %v capture failed due to error '%v' output was %v", host, err, strings.Join(allHostLog, "\n"))
	}

	simplelog.Debugf("on host %v capture successful", host)
	consoleprint.UpdateNodeState(consoleprint.NodeState{
		Node:     c.Host,
		Status:   consoleprint.CollectingAwaitingTransfer,
		StatusUX: "COLLECTED - AWAITING TRANSFER",
		Result:   consoleprint.ResultPending,
	})
	return nil
}

func TransferCapture(c HostCaptureConfiguration, outputLoc string) (int64, string, error) {
	hostname, err := c.Collector.HostExecute(false, c.Host, "cat", "/proc/sys/kernel/hostname")
	if err != nil {
		consoleprint.UpdateNodeState(consoleprint.NodeState{
			Node:       c.Host,
			Status:     consoleprint.Collecting,
			StatusUX:   "COLLECT HOSTNAME",
			Result:     consoleprint.ResultFailure,
			EndProcess: true,
			Message:    err.Error(),
		})
		return 0, c.Host, fmt.Errorf("on host %v detect real hostname so I cannot copy back the capture due to error %v", c.Host, err)
	}

	//copy tar.gz back
	tgzFileName := fmt.Sprintf("%v.tar.gz", strings.TrimSpace(hostname))
	//IMPORTANT we must use path.join and not filepath.join or everything will break
	tarGZ := path.Join(c.TransferDir, tgzFileName)
	outDir := path.Dir(outputLoc)
	if outDir == "" {
		outDir = fmt.Sprintf(".%v", filepath.Separator)
	}
	consoleprint.UpdateNodeState(consoleprint.NodeState{
		Node:     c.Host,
		Status:   consoleprint.TarballTransfer,
		StatusUX: "TARBALL TRANSFER",
		Result:   consoleprint.ResultPending,
	})

	destFile := filepath.Join(outDir, tgzFileName)
	if out, err := c.Collector.CopyFromHost(c.Host, tarGZ, destFile); err != nil {
		consoleprint.UpdateNodeState(consoleprint.NodeState{
			Node:       c.Host,
			Status:     consoleprint.TarballTransfer,
			StatusUX:   "TARBALL TRANSFER",
			Result:     consoleprint.ResultFailure,
			EndProcess: true,
			Message:    fmt.Sprintf("(%v) %v", err, out),
		})
		return 0, destFile, fmt.Errorf("unable to copy file %v from host %v to directory %v due to error %v with output %v", tarGZ, c.Host, outDir, err, out)
	}

	fileInfo, err := c.DDCfs.Stat(destFile)
	//we assume a file size of zero if we are not able to retrieve the file size for some reason
	size := int64(0)
	if err != nil {
		simplelog.Warningf("cannot get file size for file %v due to error %v. Storing size as 0", destFile, err)
	} else {
		size = fileInfo.Size()
	}
	consoleprint.UpdateNodeState(consoleprint.NodeState{
		Node:       c.Host,
		Status:     consoleprint.Completed,
		StatusUX:   "COMPLETED",
		EndProcess: true,
		Result:     consoleprint.ResultPending,
	})
	simplelog.Infof("host %v copied %v to %v it was %v bytes", c.Host, tarGZ, destFile, size)
	//defer delete tar.gz
	defer func() {
		if out, err := c.Collector.HostExecute(false, c.Host, "rm", tarGZ); err != nil {
			simplelog.Warningf("on host %v unable to cleanup remote capture due to error '%v' with output '%v'", c.Host, err, out)
		} else {
			simplelog.Debugf("on host %v file %v has been removed", c.Host, c.TransferDir)
		}
	}()
	return size, destFile, nil
}
