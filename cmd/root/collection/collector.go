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
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/cmd/root/cli"
	"github.com/dremio/dremio-diagnostic-collector/cmd/root/ddcbinary"
	"github.com/dremio/dremio-diagnostic-collector/cmd/root/helpers"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
)

var DirPerms fs.FileMode = 0750

type CopyStrategy interface {
	CreatePath(fileType, source, nodeType string) (path string, err error)
	ArchiveDiag(o string, outputLoc string) error
	GetTmpDir() string
}

type Collector interface {
	CopyFromHost(hostString string, isCoordinator bool, source, destination string) (out string, err error)
	CopyToHost(hostString string, isCoordinator bool, source, destination string) (out string, err error)
	CopyFromHostSudo(hostString string, isCoordinator bool, sudoUser, source, destination string) (out string, err error)
	CopyToHostSudo(hostString string, isCoordinator bool, sudoUser, source, destination string) (out string, err error)
	FindHosts(searchTerm string) (podName []string, err error)
	HostExecute(hostString string, isCoordinator bool, args ...string) (stdOut string, err error)
	HostExecuteAndStream(hostString string, output cli.OutputHandler, isCoordinator bool, args ...string) error
	HelpText() string
}

type Args struct {
	DDCfs          helpers.Filesystem
	CoordinatorStr string
	ExecutorsStr   string
	OutputLoc      string
	SudoUser       string
	CopyStrategy   CopyStrategy
}

type HostCaptureConfiguration struct {
	NodeCaptureOutput string
	IsCoordinator     bool
	Collector         Collector
	Host              string
	OutputLocation    string
	SudoUser          string
	CopyStrategy      CopyStrategy
	DDCfs             helpers.Filesystem
}

func Execute(c Collector, s CopyStrategy, collectionArgs Args, clusterCollection ...func()) error {
	start := time.Now().UTC()
	coordinatorStr := collectionArgs.CoordinatorStr
	executorsStr := collectionArgs.ExecutorsStr
	outputLoc := collectionArgs.OutputLoc
	sudoUser := collectionArgs.SudoUser
	ddcfs := collectionArgs.DDCfs
	var ddcLoc string
	var err error
	tmpIinstallDir, err := os.MkdirTemp("", "ddcex-output")
	if err != nil {
		return err
	}
	defer func() {
		if err := os.RemoveAll(tmpIinstallDir); err != nil {
			simplelog.Warningf("unable to cleanup temp install directory: '%v'", err)
		}
	}()
	ddcLoc, err = ddcbinary.WriteOutDDC(tmpIinstallDir)
	if err != nil {
		return fmt.Errorf("unable to to find ddc cannot copy it to hosts due to error '%v'", err)
	}
	execLoc, err := os.Executable()
	if err != nil {
		return err
	}
	ddcYamlFilePath := filepath.Join(path.Dir(execLoc), "ddc.yaml")
	coordinators, err := c.FindHosts(coordinatorStr)
	if err != nil {
		return err
	}

	executors, err := c.FindHosts(executorsStr)
	if err != nil {
		return err
	}

	totalNodes := len(executors) + len(coordinators)
	if totalNodes == 0 {
		return fmt.Errorf("there are no nodes to connect: %v", c.HelpText())
	}
	//now safe to collect cluster level information
	for _, c := range clusterCollection {
		c()
	}
	var files []helpers.CollectedFile
	var totalFailedFiles []FailedFiles
	var totalSkippedFiles []string
	var nodesConnectedTo int
	var m sync.Mutex
	var wg sync.WaitGroup

	for _, coordinator := range coordinators {
		nodesConnectedTo++
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			coordinatorCaptureConf := HostCaptureConfiguration{
				Collector:         c,
				IsCoordinator:     true,
				Host:              host,
				OutputLocation:    s.GetTmpDir(),
				SudoUser:          sudoUser,
				CopyStrategy:      s,
				DDCfs:             ddcfs,
				NodeCaptureOutput: "/tmp/ddc", //TODO use node output dirs from the config
			}
			//we want to be able to capture the job profiles of all the nodes
			skipRESTCalls := false
			writtenFiles, failedFiles, skippedFiles := Capture(coordinatorCaptureConf, ddcLoc, ddcYamlFilePath, s.GetTmpDir(), skipRESTCalls)
			m.Lock()
			totalFailedFiles = append(totalFailedFiles, failedFiles...)
			totalSkippedFiles = append(totalSkippedFiles, skippedFiles...)
			files = append(files, writtenFiles...)
			m.Unlock()
		}(coordinator)
	}

	for _, executor := range executors {
		nodesConnectedTo++
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			executorCaptureConf := HostCaptureConfiguration{
				Collector:         c,
				IsCoordinator:     false,
				Host:              host,
				OutputLocation:    s.GetTmpDir(),
				SudoUser:          sudoUser,
				CopyStrategy:      s,
				DDCfs:             ddcfs,
				NodeCaptureOutput: "/tmp/ddc",
			}
			//always skip executor calls
			skipRESTCalls := true
			writtenFiles, failedFiles, skippedFiles := Capture(executorCaptureConf, ddcLoc, ddcYamlFilePath, s.GetTmpDir(), skipRESTCalls)
			m.Lock()
			totalFailedFiles = append(totalFailedFiles, failedFiles...)
			totalSkippedFiles = append(totalSkippedFiles, skippedFiles...)
			files = append(files, writtenFiles...)
			m.Unlock()
		}(executor)
	}
	wg.Wait()
	end := time.Now().UTC()
	var collectionInfo SummaryInfo
	collectionInfo.EndTimeUTC = end
	collectionInfo.StartTimeUTC = start
	seconds := end.Unix() - start.Unix()
	collectionInfo.TotalRuntimeSeconds = seconds
	collectionInfo.ClusterInfo.TotalNodesAttempted = len(coordinators) + len(executors)
	collectionInfo.ClusterInfo.NumberNodesContacted = nodesConnectedTo
	collectionInfo.CollectedFiles = files
	totalBytes := int64(0)
	for _, f := range files {
		totalBytes += f.Size
	}
	collectionInfo.TotalBytesCollected = totalBytes
	collectionInfo.Coordinators = coordinators
	collectionInfo.Executors = executors
	collectionInfo.FailedFiles = totalFailedFiles
	collectionInfo.SkippedFiles = totalSkippedFiles

	o, err := collectionInfo.String()
	if err != nil {
		return err
	}

	tarballs, err := FindTarGzFiles(path.Dir(s.GetTmpDir()))
	if err != nil {
		return err
	}
	if len(tarballs) > 0 {
		simplelog.Infof("extracting the following tarballs %v", strings.Join(tarballs, ", "))
		for _, t := range tarballs {
			simplelog.Infof("extracting %v to %v", t, s.GetTmpDir())
			if err := ExtractTarGz(t, s.GetTmpDir()); err != nil {
				simplelog.Errorf("unable to extract tarball %v due to error %v", t, err)
			}
			simplelog.Infof("extracted %v", t)
			if err := os.Remove(t); err != nil {
				simplelog.Errorf("unable to delete tarball %v due to error %v", t, err)
			}
			simplelog.Infof("removed %v", t)
		}
	}
	// archives the collected files
	// creates the summary file too
	return s.ArchiveDiag(o, outputLoc)

}

// Sanitize archive file pathing from "G305: Zip Slip vulnerability"
func SanitizeArchivePath(d, t string) (v string, err error) {
	v = filepath.Join(d, t)
	if strings.HasPrefix(v, filepath.Clean(d)) {
		return v, nil
	}
	return "", fmt.Errorf("%s: %s", "content filepath is tainted", t)
}

func ExtractTarGz(gzFilePath, dest string) error {
	reader, err := os.Open(path.Clean(gzFilePath))
	if err != nil {
		return err
	}
	defer reader.Close()

	gzReader, err := gzip.NewReader(reader)
	if err != nil {
		return err
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)

	for {
		header, err := tarReader.Next()

		switch {
		case err == io.EOF:
			return nil
		case err != nil:
			return err
		case header == nil:
			continue
		}
		target, err := SanitizeArchivePath(dest, header.Name)
		if err != nil {
			return err
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if _, err := os.Stat(target); err != nil {
				if err := os.MkdirAll(path.Clean(target), 0750); err != nil {
					return err
				}
			}
		case tar.TypeReg:
			file, err := os.OpenFile(path.Clean(target), os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil {
				simplelog.Errorf("skipping file %v due to error %v", file, err)
				continue
			}
			defer file.Close()
			for {
				_, err := io.CopyN(file, tarReader, 1024)
				if err != nil {
					if err == io.EOF {
						break
					}
					return err
				}
			}
			simplelog.Debugf("extracted file %v", file.Name())
		}
	}
}

func FindTarGzFiles(rootDir string) ([]string, error) {
	simplelog.Infof("looking in %v for tar.gz files", rootDir)
	var files []string
	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && strings.HasSuffix(info.Name(), ".tar.gz") {
			files = append(files, path)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return files, nil
}
