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

	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/ddcio"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/shutdown"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/simplelog"
)

func RunCollectJStacks(c *conf.CollectConf, hook shutdown.CancelHook) error {
	return RunCollectJStacksWithTimeService(c, hook, func() time.Time {
		return time.Now()
	})
}

func RunCollectJStacksWithTimeService(c *conf.CollectConf, hook shutdown.CancelHook, timer func() time.Time) error {
	simplelog.Debug("Collecting Jstack ...")
	threadDumpFreq := c.DremioJStackFreqSeconds()
	iterations := c.DremioJStackTimeSeconds() / threadDumpFreq
	simplelog.Debugf("Running Java thread dumps every %v second(s) for a total of %v iterations ...", threadDumpFreq, iterations)
	for i := 0; i < iterations; i++ {
		var w bytes.Buffer
		if err := ddcio.Shell(hook, &w, fmt.Sprintf("jcmd %v Thread.print -l", c.DremioPID())); err != nil {
			simplelog.Warningf("unable to capture jstack of pid %v: %v", c.DremioPID(), err)
		}
		date := timer().Format("2006-01-02_15_04_05")
		threadDumpFileName := filepath.Join(c.ThreadDumpsOutDir(), fmt.Sprintf("threadDump-%s-%s.txt", c.NodeName(), date))
		if err := os.WriteFile(filepath.Clean(threadDumpFileName), w.Bytes(), 0o600); err != nil {
			return fmt.Errorf("unable to write thread dump %v: %w", threadDumpFileName, err)
		}
		simplelog.Debugf("Saved %v", threadDumpFileName)
		simplelog.Debugf("Waiting %v second(s) ...", threadDumpFreq)
		time.Sleep(time.Duration(threadDumpFreq) * time.Second)
	}
	return nil
}
