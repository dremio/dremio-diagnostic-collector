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

// kubernetes package provides access to log collections on k8s
package kubernetes

import (
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/shutdown"
)

func TestNewKubectlK8sActions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testing in short mode")
	}
	namespace := "mynamespace"
	hook := shutdown.NewHook()
	defer hook.Cleanup()
	actions, err := NewK8sAPI(KubeArgs{
		Namespace: namespace,
	}, hook)
	if err != nil {
		t.Fatal(err)
	}
	if actions.namespace != namespace {
		t.Errorf("\nexpected \n%v\nbut got\n%v", namespace, actions.namespace)
	}
}
