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
	"encoding/json"
	"errors"
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
	"github.com/dremio/dremio-diagnostic-collector/pkg/clusterstats"
	"github.com/dremio/dremio-diagnostic-collector/pkg/consoleprint"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
	"github.com/dremio/dremio-diagnostic-collector/pkg/versions"
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
	HostExecute(mask bool, hostString string, isCoordinator bool, args ...string) (stdOut string, err error)
	HostExecuteAndStream(mask bool, hostString string, output cli.OutputHandler, isCoordinator bool, args ...string) error
	HelpText() string
	Name() string
}

type Args struct {
	DDCfs          helpers.Filesystem
	CoordinatorStr string
	ExecutorsStr   string
	OutputLoc      string
	SudoUser       string
	CopyStrategy   CopyStrategy
	DremioPAT      string
	TransferDir    string
	DDCYamlLoc     string
}

type HostCaptureConfiguration struct {
	IsCoordinator  bool
	Collector      Collector
	Host           string
	OutputLocation string
	SudoUser       string
	CopyStrategy   CopyStrategy
	DDCfs          helpers.Filesystem
	DremioPAT      string
	TransferDir    string
}

func Execute(c Collector, s CopyStrategy, collectionArgs Args, clusterCollection ...func([]string)) error {
	start := time.Now().UTC()
	coordinatorStr := collectionArgs.CoordinatorStr
	executorsStr := collectionArgs.ExecutorsStr
	outputLoc := collectionArgs.OutputLoc
	sudoUser := collectionArgs.SudoUser
	ddcfs := collectionArgs.DDCfs
	dremioPAT := collectionArgs.DremioPAT
	transferDir := collectionArgs.TransferDir
	ddcYamlFilePath := collectionArgs.DDCYamlLoc
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
		return fmt.Errorf("making ddc binary failed: '%v'", err)
	}

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
		return fmt.Errorf("coordinator string '%v' and executor string '%v' were not able to connect: %v ", coordinatorStr, executorsStr, c.HelpText())
	}
	hosts := append(coordinators, executors...)

	//now safe to collect cluster level information
	for _, c := range clusterCollection {
		c(hosts)
	}
	var files []helpers.CollectedFile
	var totalFailedFiles []string
	var totalSkippedFiles []string
	var nodesConnectedTo int
	var m sync.Mutex
	var wg sync.WaitGroup
	consoleprint.UpdateRuntime(
		versions.GetCLIVersion(),
		simplelog.GetLogLoc(),
		collectionArgs.DDCYamlLoc,
		c.Name(),
		0,
		len(coordinators)+len(executors),
	)
	for _, coordinator := range coordinators {
		nodesConnectedTo++
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			coordinatorCaptureConf := HostCaptureConfiguration{
				Collector:      c,
				IsCoordinator:  true,
				Host:           host,
				OutputLocation: s.GetTmpDir(),
				SudoUser:       sudoUser,
				CopyStrategy:   s,
				DDCfs:          ddcfs,
				TransferDir:    transferDir,
				DremioPAT:      dremioPAT,
			}
			//we want to be able to capture the job profiles of all the nodes
			skipRESTCalls := false
			size, f, err := Capture(coordinatorCaptureConf, ddcLoc, ddcYamlFilePath, s.GetTmpDir(), skipRESTCalls)
			if err != nil {
				m.Lock()
				totalFailedFiles = append(totalFailedFiles, f)
				m.Unlock()
			} else {
				m.Lock()
				files = append(files, helpers.CollectedFile{
					Path: f,
					Size: size,
				})
				m.Unlock()
			}

		}(coordinator)
	}

	for _, executor := range executors {
		nodesConnectedTo++
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			executorCaptureConf := HostCaptureConfiguration{
				Collector:      c,
				IsCoordinator:  false,
				Host:           host,
				OutputLocation: s.GetTmpDir(),
				SudoUser:       sudoUser,
				CopyStrategy:   s,
				DDCfs:          ddcfs,
				TransferDir:    transferDir,
			}
			//always skip executor calls
			skipRESTCalls := true
			size, f, err := Capture(executorCaptureConf, ddcLoc, ddcYamlFilePath, s.GetTmpDir(), skipRESTCalls)
			if err != nil {
				m.Lock()
				totalFailedFiles = append(totalFailedFiles, f)
				m.Unlock()
			} else {
				m.Lock()
				files = append(files, helpers.CollectedFile{
					Path: f,
					Size: size,
				})
				m.Unlock()
			}
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
	collectionInfo.DDCVersion = versions.GetCLIVersion()
	o, err := collectionInfo.String()
	if err != nil {
		return err
	}

	tarballs, err := FindTarGzFiles(path.Dir(s.GetTmpDir()))
	if err != nil {
		return err
	}
	if len(tarballs) > 0 {
		simplelog.Debugf("extracting the following tarballs %v", strings.Join(tarballs, ", "))
		for _, t := range tarballs {
			simplelog.Debugf("extracting %v to %v", t, s.GetTmpDir())
			if err := ExtractTarGz(t, s.GetTmpDir()); err != nil {
				simplelog.Errorf("unable to extract tarball %v due to error %v", t, err)
			}
			simplelog.Debugf("extracted %v", t)
			if err := os.Remove(t); err != nil {
				simplelog.Errorf("unable to delete tarball %v due to error %v", t, err)
			}
			simplelog.Debugf("removed %v", t)
		}
	}

	clusterstats, err := FindClusterID(s.GetTmpDir())
	if err != nil {
		simplelog.Errorf("unable to find cluster ID in %v: %v", s.GetTmpDir(), err)
	} else {
		versions := make(map[string]string)
		clusterIDs := make(map[string]string)
		for _, stats := range clusterstats {
			versions[stats.NodeName] = stats.DremioVersion
			clusterIDs[stats.NodeName] = stats.ClusterID
		}
		collectionInfo.ClusterID = clusterIDs
		collectionInfo.DremioVersion = versions
	}
	if len(files) == 0 {
		return errors.New("no files transferred")
	}
	// archives the collected files
	// creates the summary file too
	err = s.ArchiveDiag(o, outputLoc)
	if err != nil {
		return err
	}
	fullPath, err := filepath.Abs(outputLoc)
	if err != nil {
		return err
	}
	consoleprint.UpdateTarballDir(fullPath)
	return nil
}

func FindClusterID(outputDir string) (clusterStatsList []clusterstats.ClusterStats, err error) {
	err = filepath.Walk(outputDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err // Handle the error according to your needs
		}

		if info.Name() == "cluster-stats.json" {
			b, err := os.ReadFile(filepath.Clean(path))
			if err != nil {
				return err
			}
			var clusterStats clusterstats.ClusterStats

			err = json.Unmarshal(b, &clusterStats)
			if err != nil {
				return err
			}
			clusterStatsList = append(clusterStatsList, clusterStats)
		}

		return nil
	})
	return
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
	simplelog.Debugf("looking in %v for tar.gz files", rootDir)
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
