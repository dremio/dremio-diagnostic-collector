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

// package jvmcollect handles parsing of the jvm information
package jvmcollect

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/local/ddcio"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/shutdown"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/simplelog"
)

func RunJStacks(c *conf.CollectConf, hook shutdown.CancelHook) error {
	return RunJStacksWithTimeService(c, hook, func() time.Time {
		return time.Now()
	})
}

func RunJStacksWithTimeService(c *conf.CollectConf, hook shutdown.CancelHook, timer func() time.Time) error {
	simplelog.Debug("Collecting Jstack ...")
	durationSeconds := c.DiagTimeSeconds()
	simplelog.Debugf("Running Java thread dumps for %v second(s) ...", durationSeconds)
	deadline := time.Now().Add(time.Duration(durationSeconds) * time.Second)
	for i := 0; time.Now().Before(deadline); i++ {
		var w bytes.Buffer
		if err := ddcio.Shell(hook, &w, fmt.Sprintf("jcmd %v Thread.print -l", c.DremioPID())); err != nil {
			simplelog.Warningf("unable to capture jstack of pid %v: %v", c.DremioPID(), err)
		}
		date := timer().Format("2006-01-02_15_04_05")
		threadDumpFileName := filepath.Join(c.ThreadDumpsOutDir(), fmt.Sprintf("threadDump-%s-%s-%d.txt", c.NodeName(), date, i))
		if err := os.WriteFile(filepath.Clean(threadDumpFileName), w.Bytes(), 0o600); err != nil {
			return fmt.Errorf("unable to write thread dump %v: %w", threadDumpFileName, err)
		}
		simplelog.Debugf("Saved %v", threadDumpFileName)
	}
	return nil
}
