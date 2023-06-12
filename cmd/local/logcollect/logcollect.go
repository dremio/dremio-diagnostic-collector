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

// package logcollect contains the logic for log collection in the local-collect sub command
package logcollect

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/ddcio"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
)

type Collector struct {
	dremioGCFilePattern      string
	dremioLogDir             string
	logsOutDir               string
	gcLogsDir                string
	queriesOutDir            string
	dremioLogsNumDays        int
	dremioQueriesJSONNumDays int
}

func NewLogCollector(dremioLogDir, logsOutDir, gcLogsDir, dremioGCFilePattern, queriesOutDir string, dremioQueriesJSONNumDays, dremioLogsNumDays int) *Collector {
	return &Collector{
		dremioLogDir:             dremioLogDir,
		logsOutDir:               logsOutDir,
		dremioLogsNumDays:        dremioLogsNumDays,
		dremioQueriesJSONNumDays: dremioQueriesJSONNumDays,
		dremioGCFilePattern:      dremioGCFilePattern,
		queriesOutDir:            queriesOutDir,
		gcLogsDir:                gcLogsDir,
	}
}

func (l *Collector) RunCollectDremioServerLog() error {
	simplelog.Info("Collecting GC logs ...")
	var errs []error
	if err := l.exportArchivedLogs(l.dremioLogDir, "server.log", "server", l.dremioLogsNumDays); err != nil {
		errs = append(errs, fmt.Errorf("trying to archive server logs we got error: %v", err))
	}
	simplelog.Info("... collecting server.out")
	src := path.Join(l.dremioLogDir, "server.out")
	dest := path.Join(l.logsOutDir, "server.out")
	if err := ddcio.CopyFile(path.Clean(src), path.Clean(dest)); err != nil {
		errs = append(errs, fmt.Errorf("unable to copy %v to %v due to error %v", src, dest, err))
	}
	if len(errs) > 1 {
		return fmt.Errorf("serveral errors while copying dremio server logs: %v", errors.Join(errs...))
	} else if (len(errs)) == 1 {
		return errs[0]
	}
	simplelog.Info("... collecting server logs COMPLETED")
	return nil
}

func (l *Collector) RunCollectGcLogs() error {
	if l.gcLogsDir == "" {
		simplelog.Warningf("Skipping GC Logs no gc log directory is configured set dremio-gclogs-dir in ddc.yaml")
	} else {
		simplelog.Info("Collecting GC logs ...")
	}
	files, err := os.ReadDir(path.Clean(l.gcLogsDir))
	if err != nil {
		return fmt.Errorf("error reading directory: %w", err)
	}
	var errs []error
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		matched, err := filepath.Match(l.dremioGCFilePattern, file.Name())
		if err != nil {
			errs = append(errs, fmt.Errorf("error matching file pattern %v with error '%v'", l.dremioGCFilePattern, err))
		}
		if matched {
			srcPath := filepath.Join(l.gcLogsDir, file.Name())
			destPath := filepath.Join(l.logsOutDir, file.Name())
			if err := ddcio.CopyFile(path.Clean(srcPath), path.Clean(destPath)); err != nil {
				errs = append(errs, fmt.Errorf("error copying file %s: %w", file.Name(), err))
			}
			simplelog.Debugf("Copied file %s to %s", srcPath, destPath)
		}
	}
	if len(errs) > 1 {
		return fmt.Errorf("serveral errors while copying dremio server logs: %v", errors.Join(errs...))
	} else if (len(errs)) == 1 {
		return errs[0]
	}
	simplelog.Warning("GC logs from executors and scale-out coordinators must be collected separately!")
	simplelog.Info("... collecting GC logs COMPLETED")

	return nil
}

func (l *Collector) RunCollectMetadataRefreshLogs() error {
	simplelog.Info("Collecting metadata refresh logs from Coordinator(s) ...")
	if err := l.exportArchivedLogs(l.dremioLogDir, "metadata_refresh.log", "metadata_refresh", l.dremioLogsNumDays); err != nil {
		return fmt.Errorf("unable to collect metadata refresh logs due to error %v", err)
	}
	simplelog.Warning("Metadata refresh logs from scale-out coordinators must be collected separately!")
	simplelog.Info("... collecting meta data refresh logs from Coordinator(s) COMPLETED")
	return nil
}

func (l *Collector) RunCollectReflectionLogs() error {
	simplelog.Info("Collecting reflection logs from Coordinator(s) ...")
	if err := l.exportArchivedLogs(l.dremioLogDir, "reflection.log", "reflection", l.dremioLogsNumDays); err != nil {
		return fmt.Errorf("unable to collect reflection logs due to error %v", err)
	}
	simplelog.Info("... collecting reflection logs from Coordinator(s) COMPLETED")

	return nil
}

func (l *Collector) RunCollectDremioAccessLogs() error {
	simplelog.Info("Collecting access logs from Coordinator(s) ...")
	simplelog.Warning("Access logs from scale-out coordinators must be collected separately!")
	if err := l.exportArchivedLogs(l.dremioLogDir, "access.log", "access", l.dremioLogsNumDays); err != nil {
		return fmt.Errorf("unable to archive access.logs due to error %v", err)
	}
	simplelog.Info("... collecting access logs from Coordinator(s) COMPLETED")

	return nil
}

func (l *Collector) RunCollectAccelerationLogs() error {
	simplelog.Info("Collecting acceleration logs from Coordinator(s) ...")
	simplelog.Warning("Acceleration logs from scale-out coordinators must be collected separately!")
	if err := l.exportArchivedLogs(l.dremioLogDir, "acceleration.log", "acceleration", l.dremioLogsNumDays); err != nil {
		return fmt.Errorf("unable to archive acceleration.logs due to error %v", err)
	}
	simplelog.Info("... collecting acceleragtion logs from Coordinator(s) COMPLETED")

	return nil
}

func (l *Collector) RunCollectQueriesJSON() error {
	simplelog.Info("Collecting queries.json ...")
	err := l.exportArchivedLogs(l.dremioLogDir, "queries.json", "queries", l.dremioQueriesJSONNumDays)
	if err != nil {
		return fmt.Errorf("failed to export archived logs: %v", err)
	}

	simplelog.Warning("Queries.json from scale-out coordinators must be collected separately!")

	simplelog.Info("... collecting Queries JSON for Job Profiles COMPLETED")
	return nil
}

func (l *Collector) exportArchivedLogs(srcLogDir string, unarchivedFile string, logPrefix string, archiveDays int) error {
	var errs []error
	src := path.Join(srcLogDir, unarchivedFile)
	var outDir string
	if logPrefix == "queries" {
		outDir = l.queriesOutDir
	} else {
		outDir = l.logsOutDir
	}
	dest := path.Join(outDir, unarchivedFile)
	//instead of copying it we just archive it to a new location
	if err := ddcio.GzipFile(path.Clean(src), path.Clean(dest+".gz")); err != nil {
		errs = append(errs, fmt.Errorf("archiving of log file %v failed due to error %v", unarchivedFile, err))
	}

	today := time.Now()
	files, err := os.ReadDir(filepath.Join(srcLogDir, "archive"))
	if err != nil {
		//no archives to read go ahead and exist as there is nothing to do
		return fmt.Errorf("unable to read archive folder due to error %v", err)
	}
	for i := 0; i <= archiveDays; i++ {
		processingDate := today.AddDate(0, 0, -i).Format("2006-01-02")
		//now search files for a match
		for _, f := range files {
			if strings.HasPrefix(f.Name(), fmt.Sprintf("%v.%v", logPrefix, processingDate)) {
				simplelog.Info("Copying archive file for " + processingDate + ": " + f.Name())
				src := filepath.Join(srcLogDir, "archive", f.Name())
				dst := filepath.Join(outDir, f.Name())
				if strings.HasSuffix(f.Name(), ".gz") {
					if err := ddcio.CopyFile(path.Clean(src), path.Clean(dst)); err != nil {
						errs = append(errs, fmt.Errorf("unable to move file %v to %v due to error %v", src, dst, err))
						continue
					}
				} else {
					//instead of copying it we just archive it to a new location
					if err := ddcio.GzipFile(path.Clean(src), path.Clean(dst+".gz")); err != nil {
						errs = append(errs, fmt.Errorf("unable to archive file %v to %v due to error %v", src, dst, err))
						continue
					}
				}
			}
		}
	}
	if len(errs) > 1 {
		return fmt.Errorf("multiple errors archiving %v", errors.Join(errs...))
	} else if len(errs) == 1 {
		return errs[0]
	}
	return nil
}
