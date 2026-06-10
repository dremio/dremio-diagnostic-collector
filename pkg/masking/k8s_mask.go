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

	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/simplelog"
)

var secretK8sKeywords = []string{
	"pat_token",
	"passw",
	"sas_url",
}

var supportedTypesForMasking = map[string]bool{
	"cronjob":     true,
	"job":         true,
	"statefulset": true,
	"pod":         true,
}

// safeMap extracts a nested map value by key, returning an error if the key is missing or not a map.
func safeMap(m map[string]interface{}, key string) (map[string]interface{}, error) {
	raw, ok := m[key]
	if !ok {
		return nil, fmt.Errorf("key %q not found", key)
	}
	result, ok := raw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("key %q is %T, not map[string]interface{}", key, raw)
	}
	return result, nil
}

// safeSlice extracts a nested slice value by key, returning an error if the key is missing or not a slice.
func safeSlice(m map[string]interface{}, key string) ([]interface{}, error) {
	raw, ok := m[key]
	if !ok {
		return nil, fmt.Errorf("key %q not found", key)
	}
	result, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("key %q is %T, not []interface{}", key, raw)
	}
	return result, nil
}

func getContainers(k8sItem map[string]interface{}) ([]interface{}, error) {
	kindRaw, ok := k8sItem["kind"]
	if !ok {
		return nil, fmt.Errorf("unable to read kind %#v", k8sItem)
	}
	kindStr, ok := kindRaw.(string)
	if !ok {
		return nil, fmt.Errorf("kind is %T, expected string", kindRaw)
	}
	kind := strings.ToLower(kindStr)

	if !supportedTypesForMasking[kind] {
		simplelog.Debugf("There is no password masking for kubernetes type %s", kind)
		return nil, nil
	}

	spec, err := safeMap(k8sItem, "spec")
	if err != nil {
		return nil, fmt.Errorf("unable to read spec: %w", err)
	}

	var containers []interface{}
	switch kind {
	case "cronjob":
		jobTemplate, err := safeMap(spec, "jobTemplate")
		if err != nil {
			return nil, fmt.Errorf("cronjob spec: %w", err)
		}
		jtSpec, err := safeMap(jobTemplate, "spec")
		if err != nil {
			return nil, fmt.Errorf("cronjob jobTemplate: %w", err)
		}
		template, err := safeMap(jtSpec, "template")
		if err != nil {
			return nil, fmt.Errorf("cronjob jobTemplate.spec: %w", err)
		}
		tSpec, err := safeMap(template, "spec")
		if err != nil {
			return nil, fmt.Errorf("cronjob template: %w", err)
		}
		containers, err = safeSlice(tSpec, "containers")
		if err != nil {
			return nil, fmt.Errorf("cronjob containers: %w", err)
		}
	case "job":
		template, err := safeMap(spec, "template")
		if err != nil {
			return nil, fmt.Errorf("job spec: %w", err)
		}
		tSpec, err := safeMap(template, "spec")
		if err != nil {
			return nil, fmt.Errorf("job template: %w", err)
		}
		containers, err = safeSlice(tSpec, "containers")
		if err != nil {
			return nil, fmt.Errorf("job containers: %w", err)
		}
	case "pod":
		containers, err = safeSlice(spec, "containers")
		if err != nil {
			return nil, fmt.Errorf("pod containers: %w", err)
		}
	case "statefulset":
		template, err := safeMap(spec, "template")
		if err != nil {
			return nil, fmt.Errorf("statefulset spec: %w", err)
		}
		tSpec, err := safeMap(template, "spec")
		if err != nil {
			return nil, fmt.Errorf("statefulset template: %w", err)
		}
		containers, err = safeSlice(tSpec, "containers")
		if err != nil {
			return nil, fmt.Errorf("statefulset containers: %w", err)
		}
	default:
		simplelog.Errorf("Unsupported kind %v file a bug", kind)
	}

	return containers, nil
}

func maskDictSecrets(containers []interface{}) {
	for _, container := range containers {
		containerMap, ok := container.(map[string]interface{})
		if !ok {
			continue
		}
		envVarsRaw, ok := containerMap["env"]
		if !ok {
			continue
		}
		envVars, ok := envVarsRaw.([]interface{})
		if !ok {
			continue
		}
		for _, envVar := range envVars {
			envVarMap, ok := envVar.(map[string]interface{})
			if !ok {
				continue
			}
			nameRaw, ok := envVarMap["name"]
			if !ok {
				continue
			}
			name, ok := nameRaw.(string)
			if !ok {
				continue
			}
			if checkK8sStringForSecret(strings.ToLower(name)) {
				envVarMap["value"] = "REMOVED_POTENTIAL_SECRET"
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
	metadataRaw, ok := k8sObject["metadata"]
	if !ok {
		return
	}
	metadataMap, ok := metadataRaw.(map[string]interface{})
	if !ok {
		return
	}
	annotationsRaw, ok := metadataMap["annotations"]
	if !ok {
		return
	}
	annotations, ok := annotationsRaw.(map[string]interface{})
	if !ok {
		return
	}
	if _, ok := annotations["kubectl.kubernetes.io/last-applied-configuration"]; ok {
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
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			return "", fmt.Errorf("item is %T, expected map[string]interface{}", item)
		}
		maskLastAppliedConfig(itemMap)
		containerList, err := getContainers(itemMap)
		if err != nil {
			return "", fmt.Errorf("getContainers: %w", err)
		}
		maskDictSecrets(containerList)
	}

	outBytes, err := json.Marshal(dataDict)
	if err != nil {
		return "", err
	}

	return string(outBytes), nil
}
