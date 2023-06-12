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

	ddcTmpDir := "/tmp/ddc"
	pathToDDC := path.Join(ddcTmpDir, path.Base(localDDCPath))
	// clear out the old
	if out, err := ComposeExecute(conf, []string{"rm", "-fr", path.Join(ddcTmpDir)}); err != nil {
		simplelog.Warningf("on host %v unable to do initial cleanup capture due to error '%v' with output '%v'", host, err, out)
	}
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
		//remotely make /tmp/ddc/
		if out, err := ComposeExecute(conf, []string{"mkdir", "-p", ddcTmpDir}); err != nil {
			simplelog.Errorf("host %v unable to make dir %v and cannot proceed with capture due to error '%v' with output '%v'", host, ddcTmpDir, err, out)
			return
		}
		//copy file to /tmp/ddc/ assume there is
		if out, err := ComposeCopyTo(conf, localDDCPath, pathToDDC); err != nil {
			failedFiles = append(failedFiles, FailedFiles{
				Path: localDDCPath,
				Err:  fmt.Errorf("unable to copy local ddc to remote path due to error: '%v' with output '%v'", err, out),
			})
		} else {
			simplelog.Infof("successfully copied ddc to host %v", host)
		}
	}
	//always update the configuration
	pathToDDCYAML := path.Join(ddcTmpDir, path.Base(localDDCYamlPath))
	if out, err := ComposeCopyTo(conf, localDDCYamlPath, pathToDDCYAML); err != nil {
		failedFiles = append(failedFiles, FailedFiles{
			Path: localDDCYamlPath,
			Err:  fmt.Errorf("unable to copy local ddc yaml to remote path due to error: '%v' with output '%v'", err, out),
		})
	} else {
		simplelog.Infof("successfully copied ddc.yaml to host %v", host)
	}
	//execute local-collect if skipRESTCollect is set blank the pat
	localCollectArgs := []string{pathToDDC, "local-collect"}
	if skipRESTCollect {
		localCollectArgs = append(localCollectArgs, "--dremio-pat-token", "")
	}
	if err := ComposeExecuteAndStream(conf, func(line string) {
		fmt.Printf("HOST %v - %v\n", host, line)
	}, localCollectArgs); err != nil {
		simplelog.Warningf("on host %v capture failed due to error '%v'", host, err)
		//return
	} else {
		simplelog.Infof("on host %v capture successful", host)
	}
	//defer delete tar.gz
	defer func() {
		if out, err := ComposeExecute(conf, []string{"rm", "-fr", path.Join(ddcTmpDir)}); err != nil {
			simplelog.Warningf("on host %v unable to cleanup remote capture due to error '%v' with output '%v'", host, err, out)
		} else {
			simplelog.Infof("on host %v tarballs in directory %v have been removed", host, ddcTmpDir)
		}
	}()

	//copy tar.gz back
	tarGZ := conf.NodeCaptureOutput
	foundTarGzFiles, err := findFiles(conf, tarGZ, false)
	outDir := path.Dir(outputLoc)
	if outDir == "" {
		outDir = fmt.Sprintf(".%v", filepath.Separator)
	}
	if err != nil {
		simplelog.Errorf("ERROR: host %v unable to find tar.gz in directory %v with error %v", host, tarGZ, err)
	} else {
		for _, sourceFile := range foundTarGzFiles {
			destFile := path.Join(outDir, path.Base(sourceFile))
			simplelog.Infof("found %v for copying to %v", sourceFile, destFile)
			if out, err := ComposeCopy(conf, path.Join(ddcTmpDir, sourceFile), destFile); err != nil {
				failedFiles = append(failedFiles, FailedFiles{
					Path: destFile,
					Err:  err,
				})
				simplelog.Errorf("unable to copy file %v from host %v to directory %v due to error %v with output %v", sourceFile, host, outDir, err, out)
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
				simplelog.Infof("host %v copied %v to %v", host, sourceFile, destFile)
			}
		}
	}
	return files, failedFiles, skippedFiles
}

// findFiles runs a simple ls -1 command to find all the top level files and nothing more
// this does mean you will have some errors.
// it will also attempt to find the gclogs based on startup flags if there is no gclog override specified
func findFiles(conf HostCaptureConfiguration, searchDir string, _ bool) ([]string, error) {
	var out string
	var err error

	// Protect against wildcard search base
	if searchDir == "*" {
		return []string{}, FindErr{Cmd: "wildcard search bases rejected"}
	}

	out, err = ComposeExecute(conf, []string{"ls", "-1", searchDir})

	// For find commands we simply ignore exit status 1 and continue
	// since this is usually something like a "Permission denied" which, in the
	// context of a find command can be ignored.
	if err != nil && !strings.Contains(string(err.Error()), "exit status 1") {
		return []string{}, fmt.Errorf("file search failed failed due to error %v", err)
	}

	rawFoundFiles := strings.Split(out, "\n")
	var foundFiles []string
	for _, f := range rawFoundFiles {
		if f != "" {
			if strings.HasSuffix(f, "tar.gz") {
				foundFiles = append(foundFiles, f)
			}
		}
	}
	return foundFiles, nil
}
