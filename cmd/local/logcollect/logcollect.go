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

	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/ddcio"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/simplelog"
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
	simplelog.Debug("Collecting Dremio Server logs ...")
	var errs []error
	if err := l.exportArchivedLogs(l.dremioLogDir, "server.log", "server", l.dremioLogsNumDays); err != nil {
		errs = append(errs, fmt.Errorf("trying to archive server logs we got error: %w", err))
	}
	simplelog.Debug("... collecting server.out")
	src := path.Join(l.dremioLogDir, "server.out")
	dest := path.Join(l.logsOutDir, "server.out")
	if err := ddcio.CopyFile(path.Clean(src), path.Clean(dest)); err != nil {
		errs = append(errs, fmt.Errorf("unable to copy %v to %v: %w", src, dest, err))
	}
	if len(errs) > 1 {
		return fmt.Errorf("several errors while copying dremio server logs: %w", errors.Join(errs...))
	} else if (len(errs)) == 1 {
		return errs[0]
	}
	simplelog.Debug("... collecting server logs COMPLETED")
	return nil
}

func (l *Collector) RunCollectGcLogs() error {
	if l.gcLogsDir == "" {
		simplelog.Warningf("Skipping GC Logs no gc log directory is configured set dremio-gclogs-dir in ddc.yaml")
	} else {
		simplelog.Debug("Collecting GC logs ...")
	}
	files, err := os.ReadDir(path.Clean(l.gcLogsDir))
	if err != nil {
		return fmt.Errorf("error reading directory: %w", err)
	}
	now := time.Now()
	logAgeLimit := now.AddDate(0, 0, -l.dremioLogsNumDays)
	var errs []error
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		simplelog.Debugf("found file %v in gc log folder: '%v'", file.Name(), l.gcLogsDir)
		matched, err := filepath.Match(l.dremioGCFilePattern, file.Name())
		if err != nil {
			errs = append(errs, fmt.Errorf("error matching file pattern %v with error '%w'", l.dremioGCFilePattern, err))
		}
		if matched {
			srcPath := filepath.Join(l.gcLogsDir, file.Name())
			f, err := os.Stat(srcPath)
			if err != nil {
				errs = append(errs, fmt.Errorf("while getting file info for %v there was an error: %w", srcPath, err))
				continue
			}
			if f.ModTime().Before(logAgeLimit) {
				simplelog.Debugf("skipping file %v due to having mode time of %v when logage is %v and current time of collection at %v resulting in all logs being skipped older than %v", srcPath, f.ModTime(), l.dremioLogsNumDays, now, logAgeLimit)
				continue
			}
			destPath := filepath.Join(l.logsOutDir, file.Name())
			if err := ddcio.CopyFile(path.Clean(srcPath), path.Clean(destPath)); err != nil {
				errs = append(errs, fmt.Errorf("error copying file %s: %w", file.Name(), err))
				continue
			}
			simplelog.Debugf("Copied file %s to %s", srcPath, destPath)
		} else {
			simplelog.Debugf("skipping file %v in gc log folder: '%v' did not match gc pattern: '%v'", file.Name(), l.gcLogsDir, l.dremioGCFilePattern)
		}
	}
	if len(errs) > 1 {
		return fmt.Errorf("several errors while copying dremio server logs: %w", errors.Join(errs...))
	} else if (len(errs)) == 1 {
		return errs[0]
	}
	simplelog.Debug("... collecting GC logs COMPLETED")

	return nil
}

func (l *Collector) RunCollectMetadataRefreshLogs() error {
	simplelog.Debug("Collecting metadata refresh logs from Coordinator(s) ...")
	if err := l.exportArchivedLogs(l.dremioLogDir, "metadata_refresh.log", "metadata_refresh", l.dremioLogsNumDays); err != nil {
		return fmt.Errorf("unable to collect metadata refresh logs: %w", err)
	}
	simplelog.Debug("... collecting meta data refresh logs from Coordinator(s) COMPLETED")
	return nil
}

func (l *Collector) RunCollectReflectionLogs() error {
	simplelog.Debug("Collecting reflection logs from Coordinator(s) ...")
	if err := l.exportArchivedLogs(l.dremioLogDir, "reflection.log", "reflection", l.dremioLogsNumDays); err != nil {
		return fmt.Errorf("unable to collect reflection logs: %w", err)
	}
	simplelog.Debug("... collecting reflection logs from Coordinator(s) COMPLETED")

	return nil
}

func (l *Collector) RunCollectVacuumLogs() error {
	simplelog.Debug("Collecting vacuum logs from Coordinator(s) ...")
	if err := l.exportArchivedLogs(l.dremioLogDir, "vacuum.json", "vacuum", l.dremioLogsNumDays); err != nil {
		return fmt.Errorf("unable to collect vacuum logs: %w", err)
	}
	simplelog.Debug("... collecting vacuum logs from Coordinator(s) COMPLETED")

	return nil
}

func (l *Collector) RunCollectDremioAccessLogs() error {
	simplelog.Debug("Collecting access logs from Coordinator(s) ...")
	if err := l.exportArchivedLogs(l.dremioLogDir, "access.log", "access", l.dremioLogsNumDays); err != nil {
		return fmt.Errorf("unable to archive access.logs: %w", err)
	}
	simplelog.Debug("... collecting access logs from Coordinator(s) COMPLETED")

	return nil
}

func (l *Collector) RunCollectDremioAuditLogs() error {
	simplelog.Debug("Collecting audit logs from Coordinator(s) ...")
	if err := l.exportArchivedLogs(l.dremioLogDir, "audit.json", "audit", l.dremioLogsNumDays); err != nil {
		return fmt.Errorf("unable to archive audit.json files: %w", err)
	}
	simplelog.Debug("... collecting audit logs from Coordinator(s) COMPLETED")

	return nil
}

func (l *Collector) RunCollectAccelerationLogs() error {
	simplelog.Debug("Collecting acceleration logs from Coordinator(s) ...")
	if err := l.exportArchivedLogs(l.dremioLogDir, "acceleration.log", "acceleration", l.dremioLogsNumDays); err != nil {
		return fmt.Errorf("unable to archive acceleration.logs: %w", err)
	}
	simplelog.Debug("... collecting acceleration logs from Coordinator(s) COMPLETED")

	return nil
}

func (l *Collector) RunCollectQueriesJSON() error {
	simplelog.Debug("Collecting queries.json ...")
	err := l.exportArchivedLogs(l.dremioLogDir, "queries.json", "queries", l.dremioQueriesJSONNumDays)
	if err != nil {
		return fmt.Errorf("failed to export archived logs: %w", err)
	}

	simplelog.Debug("... collecting Queries JSON for Job Profiles COMPLETED")
	return nil
}

func (l *Collector) exportArchivedLogs(srcLogDir string, unzippedFile string, logPrefix string, archiveDays int) error {
	var errs []error
	src := path.Join(srcLogDir, unzippedFile)
	var outDir string
	if logPrefix == "queries" {
		outDir = l.queriesOutDir
	} else {
		outDir = l.logsOutDir
	}
	unzippedFileDest := path.Join(outDir, unzippedFile)
	// we must copy before archival to avoid races around the archiving features of logging (which also use gzip)
	if err := ddcio.CopyFile(path.Clean(src), path.Clean(unzippedFileDest)); err != nil {
		errs = append(errs, fmt.Errorf("copying of log file %v failed: %w", unzippedFile, err))
	} else {
		// if this is successful go ahead and gzip it
		if err := ddcio.GzipFile(path.Clean(unzippedFileDest), path.Clean(unzippedFileDest+".gz")); err != nil {
			errs = append(errs, fmt.Errorf("archiving of log file %v failed: %w", unzippedFile, err))
		} else {
			// if we've successfully gzipped the file we can safely delete the source
			if err := os.Remove(path.Clean(unzippedFileDest)); err != nil {
				errs = append(errs, fmt.Errorf("cleanup of old log file %v failed: %w", unzippedFile, err))
			}
		}
	}

	today := time.Now()
	files, err := os.ReadDir(filepath.Join(srcLogDir, "archive"))
	if err != nil {
		// no archives to read go ahead and exit as there is nothing to do
		return fmt.Errorf("unable to read archive folder: %w", err)
	}
	for i := 0; i <= archiveDays; i++ {
		processingDate := today.AddDate(0, 0, -i).Format("2006-01-02")
		// now search files for a match
		for _, f := range files {
			if strings.HasPrefix(f.Name(), fmt.Sprintf("%v.%v", logPrefix, processingDate)) {
				simplelog.Debugf("Copying archive file for %v:%v", processingDate, f.Name())
				src := filepath.Join(srcLogDir, "archive", f.Name())
				dst := filepath.Join(outDir, f.Name())

				// we must copy before archival to avoid races around the archiving features of logging (which also use gzip)
				if err := ddcio.CopyFile(path.Clean(src), path.Clean(dst)); err != nil {
					errs = append(errs, fmt.Errorf("unable to move file %v to %v: %w", src, dst, err))
					continue
				}
				if !strings.HasSuffix(f.Name(), ".gz") {
					// go ahead and archive the file since it's not already
					if err := ddcio.GzipFile(path.Clean(dst), path.Clean(dst+".gz")); err != nil {
						errs = append(errs, fmt.Errorf("unable to archive file %v to %v: %w", src, dst, err))
						continue
					}
					// if we've successfully gzipped the file we can safely delete the source (the continue above will guard against executing this)
					if err := os.Remove(path.Clean(dst)); err != nil {
						errs = append(errs, fmt.Errorf("cleanup of old log file %v failed: %w", unzippedFile, err))
					}
				}
			}
		}
	}
	if len(errs) > 1 {
		return fmt.Errorf("multiple errors archiving: %w", errors.Join(errs...))
	} else if len(errs) == 1 {
		return errs[0]
	}
	return nil
}
