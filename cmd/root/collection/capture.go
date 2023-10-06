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

	"github.com/dremio/dremio-diagnostic-collector/cmd/root/helpers"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
)

type FindErr struct {
	Cmd string
}

func (fe FindErr) Error() string {
	return fmt.Sprintf("find failed due to error %v:", fe.Cmd)
}

// Capture collects diagnostics, conf files and log files from the target hosts. Failures are permissive and
// are first logged and then returned at the end with the reason for the failure.
func Capture(conf HostCaptureConfiguration, localDDCPath, localDDCYamlPath, outputLoc string, skipRESTCollect bool) (files []helpers.CollectedFile, failedFiles []FailedFiles, skippedFiles []string) {
	host := conf.Host
	ddcTmpDir := conf.TransferDir
	// we cannot use filepath.join here as it will break everything during the transfer
	pathToDDC := path.Join(ddcTmpDir, "ddc")
	// we cannot use filepath.join here as it will break everything during the transfer
	pathToDDCYAML := path.Join(ddcTmpDir, "ddc.yaml")
	dremioPAT := conf.DremioPAT
	versionMatch := false
	// //check if the version is up to date
	// if out, err := ComposeExecute(conf, []string{pathToDDC, "version"}); err != nil {
	// 	simplelog.Warningf("host %v unable to find ddc version due to error '%v' with output '%v'", host, err, out)
	// } else {
	// 	simplelog.Infof("host %v has ddc version '%v' already installed", host, out)
	// 	versionMatch = out == versions.GetDDCRuntimeVersion()
	// }
	//if versions don't match go ahead and install a copy in the ddc tmp directory
	if !versionMatch {
		//remotely make TransferDir
		if out, err := ComposeExecute(false, conf, []string{"mkdir", "-p", ddcTmpDir}); err != nil {
			simplelog.Errorf("host %v unable to make dir %v due to error '%v' with output '%v'", host, ddcTmpDir, err, out)
			return
		}
		//copy file to TransferDir assume there is
		if out, err := ComposeCopyTo(conf, localDDCPath, pathToDDC); err != nil {
			failedFiles = append(failedFiles, FailedFiles{
				Path: localDDCPath,
				Err:  fmt.Errorf("unable to copy local ddc to remote path due to error: '%v' with output '%v'", err, out),
			})
			//this is a critical error so it is safe to exit
			return
		}
		simplelog.Infof("successfully copied ddc to host %v at %v", host, pathToDDC)
		defer func() {
			// clear out when done
			if out, err := ComposeExecute(false, conf, []string{"rm", pathToDDC}); err != nil {
				simplelog.Warningf("on host %v unable to remove ddc due to error '%v' with output '%v'", host, err, out)
			}
		}()
		defer func() {
			// clear out when done
			if out, err := ComposeExecute(false, conf, []string{"rm", pathToDDC + ".log"}); err != nil {
				simplelog.Warningf("on host %v unable to remove ddc.log due to error '%v' with output '%v'", host, err, out)
			}
		}()

		//make  exec TransferDir
		if out, err := ComposeExecute(false, conf, []string{"chmod", "+x", pathToDDC}); err != nil {
			simplelog.Errorf("host %v unable to make ddc exec %v and cannot proceed with capture due to error '%v' with output '%v'", host, pathToDDC, err, out)
			return
		}
	}
	//always update the configuration
	if out, err := ComposeCopyTo(conf, localDDCYamlPath, pathToDDCYAML); err != nil {
		failedFiles = append(failedFiles, FailedFiles{
			Path: localDDCYamlPath,
			Err:  fmt.Errorf("unable to copy local ddc yaml to remote path due to error: '%v' with output '%v'", err, out),
		})
		//this is a critical step and will not work without it so exit
		return
	}
	simplelog.Infof("successfully copied ddc.yaml to host %v at %v", host, pathToDDCYAML)
	defer func() {
		// clear out when done
		if out, err := ComposeExecute(false, conf, []string{"rm", pathToDDCYAML}); err != nil {
			simplelog.Warningf("on host %v unable to do initial cleanup capture due to error '%v' with output '%v'", host, err, out)
		}
	}()

	//execute local-collect with a tarball-out-dir flag it must match our transfer-dir flag
	var mask bool // to mask PAT token in logs
	localCollectArgs := []string{pathToDDC, "local-collect", "--tarball-out-dir", conf.TransferDir}
	if skipRESTCollect {
		//if skipRESTCollect is set blank the pat
		localCollectArgs = append(localCollectArgs, "--disable-rest-api")
	} else if dremioPAT != "" {
		//if the dremio PAT is set, mask its logging then go ahead and pass it in
		localCollectArgs = append(localCollectArgs, "--dremio-pat-token", dremioPAT)
		mask = true
	} else {
		mask = false
	}
	if err := ComposeExecuteAndStream(mask, conf, func(line string) {
		//simplelog.HostLog(host, line)
	}, localCollectArgs); err != nil {
		simplelog.Warningf("on host %v capture failed due to error '%v'", host, err)
	} else {
		simplelog.Debugf("on host %v capture successful", host)
	}

	hostname, err := ComposeExecute(false, conf, []string{"cat", "/proc/sys/kernel/hostname"})
	if err != nil {
		simplelog.Errorf("on host %v detect real hostname so I cannot copy back the capture due to error %v", host, err)
		return files, failedFiles, skippedFiles
	}

	//copy tar.gz back
	tgzFileName := fmt.Sprintf("%v.tar.gz", strings.TrimSpace(hostname))
	//IMPORTANT we must use path.join and not filepath.join or everything will break
	tarGZ := path.Join(ddcTmpDir, tgzFileName)
	outDir := path.Dir(outputLoc)
	if outDir == "" {
		outDir = fmt.Sprintf(".%v", filepath.Separator)
	}
	//IMPORTANT we want filepath.Join here for the destination because it may be copying back to windows
	destFile := filepath.Join(outDir, tgzFileName)
	if out, err := ComposeCopyNoSudo(conf, tarGZ, destFile); err != nil {
		failedFiles = append(failedFiles, FailedFiles{
			Path: destFile,
			Err:  err,
		})
		simplelog.Errorf("unable to copy file %v from host %v to directory %v due to error %v with output %v", tarGZ, host, outDir, err, out)
	} else {
		fileInfo, err := conf.DDCfs.Stat(destFile)
		//we assume a file size of zero if we are not able to retrieve the file size for some reason
		size := int64(0)
		if err != nil {
			simplelog.Warningf("cannot get file size for file %v due to error %v. Storing size as 0", destFile, err)
		} else {
			size = fileInfo.Size()
		}
		files = append(files, helpers.CollectedFile{
			Path: destFile,
			Size: size,
		})
		simplelog.Infof("host %v copied %v to %v", host, tarGZ, destFile)
		//defer delete tar.gz
		defer func() {
			if out, err := ComposeExecute(false, conf, []string{"rm", tarGZ}); err != nil {
				simplelog.Warningf("on host %v unable to cleanup remote capture due to error '%v' with output '%v'", host, err, out)
			} else {
				simplelog.Debugf("on host %v file %v has been removed", host, ddcTmpDir)
			}
		}()
	}
	return files, failedFiles, skippedFiles
}
