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
	"strings"

	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
	"github.com/spf13/viper"
)

func ParseConfig(configDir string, supportedExtensions []string, overrides map[string]string) (foundConfig string) {

	//read viper config
	baseConfig := "ddc"
	viper.SetConfigName(baseConfig) // Name of config file (without extension)
	viper.AddConfigPath(configDir)

	var confFiles []string
	for _, e := range supportedExtensions {
		confFiles = append(confFiles, fmt.Sprintf("%v.%v", baseConfig, e))
	}

	simplelog.Debugf("searching in directory %v for the following: %v", configDir, strings.Join(confFiles, ", "))
	//searching for all known
	for _, ext := range supportedExtensions {
		viper.SetConfigType(ext)
		unableToReadConfigError := viper.ReadInConfig()
		if unableToReadConfigError == nil {
			foundConfig = fmt.Sprintf("%v.%v", baseConfig, ext)
			break
		}
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
	return
}
