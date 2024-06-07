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

	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/simplelog"
	"gopkg.in/yaml.v3"
)

func ParseConfig(ddcYamlLoc string, overrides map[string]string) (map[string]interface{}, error) {
	data := make(map[string]interface{})
	absPath, err := filepath.Abs(ddcYamlLoc)
	if err != nil {
		return data, fmt.Errorf("can't absolute path for %v due to error %w", ddcYamlLoc, err)
	}
	confFile, err := os.ReadFile(filepath.Clean(absPath))
	if err != nil {
		return data, fmt.Errorf("conf %v not readable due to error %w", ddcYamlLoc, err)
	}

	err = yaml.Unmarshal(confFile, &data)

	if err != nil {
		return data, fmt.Errorf("unable to parse yaml: %w", err)
	}

	simplelog.Infof("conf %v parsed successfully", absPath)
	for k, v := range overrides {
		//this really only applies for running over ssh so why am I doing it here? because we end up doing some crazy stuff as a result!
		if v == "\"\"" {
			data[k] = ""
		} else {
			data[k] = v
		}
	}
	return data, nil
}
