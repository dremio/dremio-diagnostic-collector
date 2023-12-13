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
package apicollect_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/apicollect"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf"
)

func setupConfigDir(t *testing.T, endpoint string) (confDir string) {
	t.Helper()
	confDir, err := os.MkdirTemp("", "ddc-tester-wlm-test")
	if err != nil {
		t.Fatalf("unable to create tmp dir due to error %v", err)
	}
	outDir, err := os.MkdirTemp("", "ddc-tester-wlm-test-out")
	if err != nil {
		t.Fatalf("unable to create tmp dir due to error %v", err)
	}
	nodeName := "tester-node-1"
	err = os.MkdirAll(filepath.Join(outDir, "wlm", nodeName), 0700)
	if err != nil {
		t.Fatalf("unable to create wlm dir due to error %v", err)
	}
	err = os.WriteFile(filepath.Join(confDir, "ddc.yaml"), []byte(fmt.Sprintf(`
dremio-log-dir: %v
dremio-conf-dir: %v
verbose: vvvv
number-threads: 2
dremio-endpoint: %v
dremio-username: dremio
dremio-pat-token: mypat
accept-collection-consent: true
allow-insecure-ssl: true
node-name: %v
tmp-output-dir: %v
`, LogDir(), ConfDir(), endpoint, nodeName, strings.ReplaceAll(outDir, "\\", "\\\\"))), 0600)
	if err != nil {
		t.Fatalf("unable to create ddc.yaml due to error %v", err)
	}
	return confDir
}

func LogDir() string {
	return filepath.Join("testdata", "logs")
}

func ConfDir() string {
	return filepath.Join("testdata", "logs")
}

func TestRunCollectWLM(t *testing.T) {
	queueAPIResponse := `{"queue": "queue data"}`
	ruleAPIResponse := `{"rule": "rule data"}`

	// Create a test server with a handler function
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apiv2/login":
			fmt.Fprint(w, `{"token": "fake_token"}`)
		case "/api/v3/wlm/queue":
			fmt.Fprint(w, `{"queue": "queue data"}`)
		case "/api/v3/wlm/rule":
			fmt.Fprint(w, `{"rule": "rule data"}`)
		default:
			http.Error(w, "Not Found", http.StatusNotFound)
		}
	}))
	defer server.Close()
	//allow the server to startup
	time.Sleep(1 * time.Second)
	confDir := setupConfigDir(t, server.URL)
	ddcYaml := filepath.Join(confDir, "ddc.yaml")
	// Prepare the configuration
	overrides := make(map[string]string)
	c, err := conf.ReadConf(overrides, ddcYaml)
	if err != nil {
		t.Fatalf("unable to read conf due to error %v", err)
	}

	err = apicollect.RunCollectWLM(c)
	if err != nil {
		t.Errorf("RunCollectWLM() returned an error: %v", err)
	}
	// Define the file paths
	queueFilePath := filepath.Join(c.WLMOutDir(), "queues.json")
	ruleFilePath := filepath.Join(c.WLMOutDir(), "rules.json")

	// Check if the 'queues.json' file was created
	if _, err := os.Stat(queueFilePath); os.IsNotExist(err) {
		t.Errorf("Failed to create 'queues.json'")
	} else {
		// Check the content of the file
		content, err := os.ReadFile(queueFilePath)
		if err != nil {
			t.Errorf("Failed to read 'queues.json': %v", err)
		} else if strings.TrimRight(string(content), "\n") != queueAPIResponse {
			t.Errorf("Content of 'queues.json' is not as expected")
		}
	}

	// Check if the 'rules.json' file was created
	if _, err := os.Stat(ruleFilePath); os.IsNotExist(err) {
		t.Errorf("Failed to create 'rules.json'")
	} else {
		// Check the content of the file
		content, err := os.ReadFile(ruleFilePath)
		if err != nil {
			t.Errorf("Failed to read 'rules.json': %v", err)
		} else if strings.TrimRight(string(content), "\n") != ruleAPIResponse {
			t.Errorf("Content of 'rules.json' is not as expected")
		}
	}
}
