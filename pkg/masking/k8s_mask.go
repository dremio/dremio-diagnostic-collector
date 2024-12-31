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

// masking hides secrets in files and replaces them with redacted text
package masking

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/simplelog"
)

var secretK8sKeywords = []string{
	"pat_token",
	"passw",
	"sas_url",
}

func getContainers(k8sItem map[string]interface{}) ([]interface{}, error) {
	var containers []interface{}
	kindRaw, valid := k8sItem["kind"]
	if !valid {
		return containers, fmt.Errorf("unable to read kind %#v", k8sItem)
	}
	kind := strings.ToLower(kindRaw.(string))

	supported := false
	supportedTypesForMasking := []string{"cronjob", "job", "statefulset", "pod"}
	for _, k := range supportedTypesForMasking {
		if k == kind {
			supported = true
		}
	}
	if !supported {
		simplelog.Debugf("There is no password masking for kubernetes type %s", kind)
		return containers, nil
	}

	specRaw, valid := k8sItem["spec"]
	if !valid {
		return containers, fmt.Errorf("unable to read spec")
	}
	spec := specRaw.(map[string]interface{})
	switch strings.ToLower(kind) {
	case "cronjob":
		containers = spec["jobTemplate"].(map[string]interface{})["spec"].(map[string]interface{})["template"].(map[string]interface{})["spec"].(map[string]interface{})["containers"].([]interface{})
	case "job":
		containers = spec["template"].(map[string]interface{})["spec"].(map[string]interface{})["containers"].([]interface{})
	case "pod":
		containers = spec["containers"].([]interface{})
	case "statefulset":
		containers = spec["template"].(map[string]interface{})["spec"].(map[string]interface{})["containers"].([]interface{})
	default:
		simplelog.Errorf("Unsupported kind %v file a bug", kind)
	}

	return containers, nil
}

func maskDictSecrets(containers []interface{}) {
	for _, container := range containers {
		envVarsRaw, valid := container.(map[string]interface{})["env"]
		if !valid {
			return
		}
		envVars := envVarsRaw.([]interface{})
		for _, envVar := range envVars {
			nameRaw, valid := envVar.(map[string]interface{})["name"]
			if !valid {
				// skipping
				continue
			}
			name := strings.ToLower(nameRaw.(string))
			if checkK8sStringForSecret(name) {
				envVar.(map[string]interface{})["value"] = "REMOVED_POTENTIAL_SECRET"
			}
		}
	}
}

func checkK8sStringForSecret(s string) bool {
	for _, keyword := range secretK8sKeywords {
		if strings.Contains(strings.ToLower(s), keyword) {
			return true
		}
	}
	return false
}

func maskLastAppliedConfig(k8sObject map[string]interface{}) {
	metadata, valid := k8sObject["metadata"]
	if !valid {
		return
	}
	annotationsRaw, valid := metadata.(map[string]interface{})["annotations"]
	if !valid {
		return
	}
	annotations := annotationsRaw.(map[string]interface{})
	if _, valid := annotations["kubectl.kubernetes.io/last-applied-configuration"]; valid {
		annotations["kubectl.kubernetes.io/last-applied-configuration"] = "REMOVED_POTENTIAL_SECRET"
	}
}

// Input: a json string of a k8s object
func RemoveSecretsFromK8sJSON(k8sJSON []byte) (string, error) {
	var dataDict map[string]interface{}
	if err := json.Unmarshal(k8sJSON, &dataDict); err != nil {
		return "", err
	}
	itemsRaw, valid := dataDict["items"]
	if !valid {
		return "", fmt.Errorf("items key not found or not a slice: %#v", dataDict)
	}
	if itemsRaw == nil {
		simplelog.Infof("no items to mask skipping masking")
		return string(k8sJSON), nil
	}
	items, valid := itemsRaw.([]interface{})
	if !valid {
		return "", fmt.Errorf("items must be an array but was '%T'", itemsRaw)
	}

	for _, item := range items {
		maskLastAppliedConfig(item.(map[string]interface{}))
		containerList, err := getContainers(item.(map[string]interface{}))
		if err != nil {
			return "", err
		}
		maskDictSecrets(containerList)
	}

	outBytes, err := json.Marshal(dataDict)
	if err != nil {
		return "", err
	}

	return string(outBytes), nil
}
