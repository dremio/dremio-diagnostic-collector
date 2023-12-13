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
	"fmt"
	"os"
	"path/filepath"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/pkg/jps"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
)

// RunCollectJVM collects JVM flags from a java process
func RunCollectJVMFlags(c *conf.CollectConf) error {
	txt, err := jps.CaptureFlagsFromPID(c.DremioPID())
	if err != nil {
		return err
	}
	jvmSettingsFile := filepath.Join(c.NodeInfoOutDir(), "jvm_settings.txt")
	jvmSettingsFileWriter, err := os.Create(filepath.Clean(jvmSettingsFile))
	if err != nil {
		return fmt.Errorf("unable to create file %v due to error %w", filepath.Clean(jvmSettingsFile), err)
	}
	defer func() {
		if err := jvmSettingsFileWriter.Close(); err != nil {
			simplelog.Debugf("This is an automatic close on file %v and safe to ignore this error: %v", filepath.Clean(jvmSettingsFile), err)
		}
	}()
	if _, err := jvmSettingsFileWriter.WriteString(txt); err != nil {
		return fmt.Errorf("unable to write to file %v due to error: %w", filepath.Clean(jvmSettingsFile), err)
	}
	if err := jvmSettingsFileWriter.Sync(); err != nil {
		return fmt.Errorf("unable to sync the jvm_settings.txt file due to error: %w", err)
	}
	if err := jvmSettingsFileWriter.Close(); err != nil {
		return fmt.Errorf("unable to close the jvm_settings.txt file due to error: %w", err)
	}
	return nil
}
