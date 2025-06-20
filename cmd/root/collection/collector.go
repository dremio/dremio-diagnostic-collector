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
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/root/cli"
	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/root/ddcbinary"
	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/root/helpers"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/archive"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/clusterstats"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/consoleprint"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/shutdown"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/simplelog"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/versions"
)

var DirPerms fs.FileMode = 0o750

type CopyStrategy interface {
	CreatePath(fileType, source, nodeType string) (path string, err error)
	ArchiveDiag(o string, outputLoc string) error
	GetTmpDir() string
}

type Collector interface {
	CopyFromHost(hostString string, source, destination string) (out string, err error)
	CopyToHost(hostString string, source, destination string) (out string, err error)
	GetCoordinators() (podName []string, err error)
	GetExecutors() (podName []string, err error)
	HostExecute(mask bool, hostString string, args ...string) (stdOut string, err error)
	HostExecuteAndStream(mask bool, hostString string, output cli.OutputHandler, pat string, args ...string) error
	HelpText() string
	Name() string
	SetHostPid(host, pidFile string)
	CleanupRemote() error
}

type Args struct {
	DDCfs                   helpers.Filesystem
	OutputLoc               string
	CopyStrategy            CopyStrategy
	DremioPAT               string
	TransferDir             string
	DDCYamlLoc              string
	Disabled                []string
	Enabled                 []string
	DisableFreeSpaceCheck   bool
	MinFreeSpaceGB          uint64
	CollectionMode          string
	TransferThreads         int
	NoLogDir                bool
	ArchiveSizeLimitMB      int
	DisableArchiveSplitting bool
}

type HostCaptureConfiguration struct {
	IsCoordinator           bool
	Collector               Collector
	Host                    string
	CopyStrategy            CopyStrategy
	DDCfs                   helpers.Filesystem
	DremioPAT               string
	TransferDir             string
	CollectionMode          string
	NoLogDir                bool
	ArchiveSizeLimitMB      int
	DisableArchiveSplitting bool
}

func FilterCoordinators(coordinators []string) []string {
	// use a map for the unique key property, so we handle duplicates in the list
	filteredList := make(map[string]bool)
	for _, e := range coordinators {
		filteredList[e] = true
	}
	// convert back into a slice
	// again we are dealing with not
	// large enough sizes to warrant optimization
	var result []string
	for k := range filteredList {
		result = append(result, k)
	}
	// sort for consistent behavior and testing
	slices.Sort(result)
	slices.Reverse(result)
	return result
}

func FilterExecutors(executors []string, coordinators []string) []string {
	// use a map for the unique key property, so we handle duplicates in the list
	filteredList := make(map[string]bool)
	for _, e := range executors {
		// we're not going to bother optimizing this:
		// the list will not be long enough to matter
		var dupe bool
		// if it's a coordinator we don't need it
		for _, c := range coordinators {
			if c == e {
				dupe = true
				simplelog.Warningf("found %v in coordinator and executor list, removing from executor list", e)
				break
			}
		}
		if !dupe {
			filteredList[e] = true
		}
	}
	// convert back into a slice
	// again we are dealing with not
	// large enough sizes to warrant optimization
	var result []string
	for k := range filteredList {
		result = append(result, k)
	}
	// sort for consistent behavior and testing
	sort.Strings(result)
	slices.Reverse(result)
	return result
}

func Execute(c Collector, s CopyStrategy, collectionArgs Args, hook shutdown.Hook, clusterCollection func()) error {
	start := time.Now().UTC()
	outputLoc := collectionArgs.OutputLoc
	outputLocDir := filepath.Dir(outputLoc)
	ddcfs := collectionArgs.DDCfs
	dremioPAT := collectionArgs.DremioPAT
	transferDir := collectionArgs.TransferDir
	ddcYamlFilePath := collectionArgs.DDCYamlLoc
	disableFreeSpaceCheck := collectionArgs.DisableFreeSpaceCheck
	minFreeSpaceGB := collectionArgs.MinFreeSpaceGB
	collectionMode := collectionArgs.CollectionMode
	transferThreads := collectionArgs.TransferThreads
	noLogDir := collectionArgs.NoLogDir
	archiveSizeLimitMB := collectionArgs.ArchiveSizeLimitMB
	disableArchiveSplitting := collectionArgs.DisableArchiveSplitting
	var err error
	tmpInstallDir := filepath.Join(outputLocDir, fmt.Sprintf("ddcex-output-%v", time.Now().Unix()))
	err = os.MkdirAll(tmpInstallDir, 0o700)
	if err != nil {
		return err
	}
	hook.AddFinalSteps(func() {
		if err := os.RemoveAll(tmpInstallDir); err != nil {
			simplelog.Warningf("unable to cleanup temp install directory: '%v'", err)
		}
	}, "cleaning temp install dir")
	ddcFilePath, err := ddcbinary.WriteOutDDC(tmpInstallDir)
	if err != nil {
		return fmt.Errorf("making ddc binary failed: %w", err)
	}

	coordinators, err := c.GetCoordinators()
	if err != nil {
		return err
	}

	executorsRaw, err := c.GetExecutors()
	if err != nil {
		return err
	}
	executors := FilterExecutors(executorsRaw, coordinators)

	totalNodes := len(executors) + len(coordinators)
	if totalNodes == 0 {
		return fmt.Errorf("no hosts found nothing to collect: %v", c.HelpText())
	}

	var tarballs []string
	var files []helpers.CollectedFile
	var totalFailedFiles []string
	var totalSkippedFiles []string
	var nodesConnectedTo int
	var m sync.Mutex
	// block until transfers are commplete
	var transferWg sync.WaitGroup
	// cap at transfer threads
	sem := make(chan struct{}, transferThreads)
	// wait group for the per node capture
	var wg sync.WaitGroup
	consoleprint.UpdateRuntime(
		versions.GetCLIVersion(),
		simplelog.GetLogLoc(),
		0,
		len(coordinators)+len(executors),
	)
	hook.AddCancelOnlyTasks(func() {
		err := c.CleanupRemote()
		if err != nil {
			simplelog.Errorf("error during cleanup %v", err)
		}
	}, "killing ddc local-collect processes")
	for _, coordinator := range coordinators {
		nodesConnectedTo++
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			coordinatorCaptureConf := HostCaptureConfiguration{
				Collector:               c,
				IsCoordinator:           true,
				Host:                    host,
				CopyStrategy:            s,
				DDCfs:                   ddcfs,
				TransferDir:             transferDir,
				DremioPAT:               dremioPAT,
				CollectionMode:          collectionMode,
				NoLogDir:                noLogDir,
				ArchiveSizeLimitMB:      archiveSizeLimitMB,
				DisableArchiveSplitting: disableArchiveSplitting,
			}
			// we want to be able to capture the job profiles of all the nodes
			skipRESTCalls := false
			err := StartCapture(coordinatorCaptureConf, ddcFilePath, ddcYamlFilePath, skipRESTCalls, disableFreeSpaceCheck, minFreeSpaceGB)
			if err != nil {
				simplelog.Errorf("failed generating tarball for host %v: %v", host, err)
				return
			}
			sem <- struct{}{}
			transferWg.Add(1)
			go func() {
				defer transferWg.Done()
				size, f, err := TransferCapture(coordinatorCaptureConf, hook, s.GetTmpDir())
				if err != nil {
					m.Lock()
					totalFailedFiles = append(totalFailedFiles, f)
					m.Unlock()
				} else {
					m.Lock()
					tarballs = append(tarballs, f)
					files = append(files, helpers.CollectedFile{
						Path: f,
						Size: size,
					})
					m.Unlock()
				}
				<-sem
			}()
		}(coordinator)
	}

	for _, executor := range executors {
		nodesConnectedTo++
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			executorCaptureConf := HostCaptureConfiguration{
				Collector:               c,
				IsCoordinator:           false,
				Host:                    host,
				CopyStrategy:            s,
				DDCfs:                   ddcfs,
				TransferDir:             transferDir,
				CollectionMode:          collectionMode,
				NoLogDir:                noLogDir,
				ArchiveSizeLimitMB:      archiveSizeLimitMB,
				DisableArchiveSplitting: disableArchiveSplitting,
			}
			// always skip executor calls
			skipRESTCalls := true
			err := StartCapture(executorCaptureConf, ddcFilePath, ddcYamlFilePath, skipRESTCalls, disableFreeSpaceCheck, minFreeSpaceGB)
			if err != nil {
				simplelog.Errorf("failed generating tarball for host %v: %v", host, err)
				return
			}
			sem <- struct{}{}
			transferWg.Add(1)
			go func() {
				defer transferWg.Done()
				size, f, err := TransferCapture(executorCaptureConf, hook, s.GetTmpDir())
				if err != nil {
					m.Lock()
					totalFailedFiles = append(totalFailedFiles, f)
					m.Unlock()
				} else {
					m.Lock()
					tarballs = append(tarballs, f)
					files = append(files, helpers.CollectedFile{
						Path: f,
						Size: size,
					})
					m.Unlock()
				}
				<-sem
			}()
		}(executor)
	}
	wg.Wait()
	transferWg.Wait()
	// doing cluster collection after all transfers
	clusterCollection()
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
	collectionInfo.CollectionsEnabled = collectionArgs.Enabled
	collectionInfo.CollectionsDisabled = collectionArgs.Disabled

	if len(tarballs) > 0 {
		simplelog.Debugf("extracting the following tarballs %v", strings.Join(tarballs, ", "))
		for _, tarballEntry := range tarballs {
			// Handle both single files and comma-separated multiple files (for split archives)
			tarballFiles := strings.Split(tarballEntry, ",")
			for _, tarball := range tarballFiles {
				tarball = strings.TrimSpace(tarball)
				if tarball == "" {
					continue
				}
				simplelog.Debugf("extracting %v to %v", tarball, s.GetTmpDir())
				if err := archive.ExtractTarGz(tarball, s.GetTmpDir()); err != nil {
					simplelog.Errorf("unable to extract tarball %v: %v", tarball, err)
				}
				simplelog.Debugf("extracted %v", tarball)
				// run a delete immediately as this takes up substantial space
				if err := os.Remove(tarball); err != nil {
					simplelog.Errorf("unable to delete tarball %v: %v", tarball, err)
				}
				hook.AddFinalSteps(func() {
					// run it again on cleanup just to be sure it's removed in case we got a ctrl+c
					if err := os.Remove(tarball); err != nil {
						simplelog.Errorf("unable to delete tarball %v: %v", tarball, err)
					}
				}, fmt.Sprintf("removing local tarball %v", tarball))
				simplelog.Debugf("removed %v", tarball)
			}
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

	// converts the collection info to a string
	// ready to write out to a file
	outString, err := collectionInfo.String()
	if err != nil {
		return err
	}

	// archives the collected files
	// creates the summary file too
	simplelog.Debugf("archiving collected files to %v", outputLoc)
	err = s.ArchiveDiag(outString, outputLoc)
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
