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

package conf

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
	"github.com/spf13/viper"
)

func ParseConfig(configDir string, overrides map[string]string) {

	//read viper config
	baseConfig := "ddc"
	viper.SetConfigName(baseConfig) // Name of config file (without extension)
	viper.AddConfigPath(configDir)
	viper.SetConfigType("yaml")
	expectedLoc := filepath.Join(baseConfig, fmt.Sprintf("%v.%v", baseConfig, "yaml"))
	err := viper.ReadInConfig()
	if _, ok := err.(viper.ConfigFileNotFoundError); ok {
		// Config file not found; ignore error if desired
		if entries, err := os.ReadDir(configDir); err != nil {
			simplelog.Errorf("conf %v not found, and cannot read directory %v due to error %v. falling back to defaults", expectedLoc, configDir, err)
		} else {
			var names []string
			for _, e := range entries {
				names = append(names, e.Name())
			}
			simplelog.Errorf("conf %v not found, in that directory are the files: '%v'. falling back to defaults", expectedLoc, strings.Join(names, ", "))
		}
	} else if err == nil {
		simplelog.Debugf("conf %v parsed successfully", expectedLoc)
	} else {
		// Config file was found but another error was produced
		simplelog.Errorf("conf %v not found due to error %v", expectedLoc, err)
	}

	viper.AutomaticEnv() // Automatically read environment variables

	for k, v := range overrides {
		//this really only applies for running over ssh so why am I doing it here? because we end up doing some crazy stuff as a result!
		if v == "\"\"" {
			viper.Set(k, "")
		} else {
			viper.Set(k, v)
		}
	}
}
