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

package masking_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/masking"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/simplelog"
)

func TestK8SMasking_WhenRemoveSecretsFromK8sJSON(t *testing.T) {
	//It("should mask secrets from k8s JSON", func() {
	input := `{
				"items": [
					{
						"kind": "pod",
						"metadata": {
							"annotations": {
								"kubectl.kubernetes.io/last-applied-configuration": "secret"
							}
						},
						"spec": {
							"containers": [
								{
									"env": [
										{
											"name": "password",
											"value": "secret"
										}
									]
								}
							]
						}
					}
				]
			}`
	expected := `{
				"items": [
					{
						"kind": "pod",
						"metadata": {
							"annotations": {
								"kubectl.kubernetes.io/last-applied-configuration": "REMOVED_POTENTIAL_SECRET"
							}
						},
						"spec": {
							"containers": [
								{
									"env": [
										{
											"name": "password",
											"value": "REMOVED_POTENTIAL_SECRET"
										}
									]
								}
							]
						}
					}
				]
			}`
	output, err := masking.RemoveSecretsFromK8sJSON([]byte(input))
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	if jsonCompact(output) != jsonCompact(expected) {
		t.Errorf("expected %v to equal %v", expected, output)
	}

	//It("should no op for unsupported kind", func() {
	input = `{
				"items": [
					{
						"kind": "unsupported",
						"metadata": {},
						"spec": {}
					}
				]
			}`
	expected = `{
				"items": [
					{
						"kind": "unsupported",
						"metadata": {},
						"spec": {}
					}
				]
			}`
	output, err = masking.RemoveSecretsFromK8sJSON([]byte(input))
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	if jsonCompact(output) != jsonCompact(expected) {
		t.Errorf("expected %v to equal %v", expected, output)
	}

	//})

	//It("should handle invalid JSON input", func() {
	input = `{
				"items": "invalid"
			}`

	_, err = masking.RemoveSecretsFromK8sJSON([]byte(input))
	if err == nil {
		t.Error("expected error but there was none")
	}
	if !strings.Contains(err.Error(), "items must be an array but was 'string'") {
		t.Errorf("expected %v to contain message 'items must be an array but was 'string''", err.Error())
	}
}

func jsonCompact(s string) string {
	buf := new(bytes.Buffer)
	if err := json.Compact(buf, []byte(s)); err != nil {
		simplelog.Errorf("json compact failed due to error %v", err)
	}
	return buf.String()
}
