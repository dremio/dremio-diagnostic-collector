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
	"testing"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/apicollect"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf"
)

func TestValidateAPICredentials(t *testing.T) {
	var method string
	var uri string
	var headersReceived http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		uri = r.RequestURI
		headersReceived = r.Header
		w.WriteHeader(http.StatusOK)
		response := []byte(`{"message": "Hello, World!"}`)
		if _, err := w.Write(response); err != nil {
			t.Fatalf("unexpected error writing response %v", err)
		}
	}))
	defer server.Close()

	time.Sleep(1 * time.Second)
	overrides := make(map[string]string)
	confDir := filepath.Join(t.TempDir(), "ddcTest")
	err := os.Mkdir(confDir, 0700)
	if err != nil {
		t.Fatalf("missing conf dir %v", err)
	}
	ddcYaml := filepath.Join(confDir, "ddc.yaml")
	err = os.WriteFile(ddcYaml, []byte(fmt.Sprintf(`
dremio-log-dir: %v
dremio-conf-dir: %v
dremio-endpoint: %v
dremio-pat-token: my-pat-token
`, LogDir(), ConfDir(), server.URL)), 0600)
	if err != nil {
		t.Fatalf("missing conf file %v", err)
	}
	c, err := conf.ReadConf(overrides, ddcYaml)
	if err != nil {
		t.Fatalf("unable to read conf %v", err)
	}
	err = apicollect.ValidateAPICredentials(c)
	if err != nil {
		t.Errorf("unable to validate %v", err)
	}
	expected := "/apiv2/login"
	if uri != expected {
		t.Errorf("expected '%q' but had '%q'", expected, uri)
	}
	if method != "GET" {
		t.Errorf("expected 'GET' but got '%v'", method)
	}
	contentType := headersReceived.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected application/json for content-type but got %v", contentType)
	}
	auth := headersReceived.Get("Authorization")
	if auth != "Bearer my-pat-token" {
		t.Errorf("expected Bearermy-pat-token for authorization but got %v", auth)
	}
}

func TestValidateAPICredentialsWithError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	time.Sleep(1 * time.Second)
	overrides := make(map[string]string)
	confDir := filepath.Join(t.TempDir(), "ddcTest")
	err := os.Mkdir(confDir, 0700)
	if err != nil {
		t.Fatalf("missing conf dir %v", err)
	}
	ddcYaml := filepath.Join(confDir, "ddc.yaml")
	err = os.WriteFile(ddcYaml, []byte(`
dremio-endpoint: "http://localhost:9047"
dremio-pat-token: "my-pat-token"
is-dremio-cloud: true
`), 0600)
	if err != nil {
		t.Fatalf("missing conf file %v", err)
	}
	c, err := conf.ReadConf(overrides, ddcYaml)
	if err != nil {
		t.Fatalf("unable to read conf %v", err)
	}
	err = apicollect.ValidateAPICredentials(c)
	if err == nil {
		t.Errorf("expected an error")
	}
}

func TestValidateAPICredentialsWithCloud(t *testing.T) {
	var method string
	var uri string
	var headersReceived http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		uri = r.RequestURI
		headersReceived = r.Header
		w.WriteHeader(http.StatusOK)
		response := []byte(`{"message": "Hello, World!"}`)
		if _, err := w.Write(response); err != nil {
			t.Fatalf("unexpected error writing response %v", err)
		}
	}))
	defer server.Close()

	time.Sleep(1 * time.Second)
	overrides := make(map[string]string)
	confDir := filepath.Join(t.TempDir(), "ddcTest")
	err := os.Mkdir(confDir, 0700)
	if err != nil {
		t.Fatalf("missing conf dir %v", err)
	}
	ddcYaml := filepath.Join(confDir, "ddc.yaml")
	err = os.WriteFile(ddcYaml, []byte(fmt.Sprintf(`
dremio-endpoint: %v
dremio-pat-token: my-pat-token
is-dremio-cloud: true
dremio-cloud-project-id: 1234
`, server.URL)), 0600)
	if err != nil {
		t.Fatalf("missing conf file %v", err)
	}
	c, err := conf.ReadConf(overrides, ddcYaml)
	if err != nil {
		t.Fatalf("unable to read conf %v", err)
	}
	err = apicollect.ValidateAPICredentials(c)
	if err != nil {
		t.Errorf("unable to validate %v", err)
	}
	expected := "/v0/projects/1234"
	if uri != expected {
		t.Errorf("expected '%q' but had '%q'", expected, uri)
	}
	if method != "GET" {
		t.Errorf("expected 'GET' but got '%v'", method)
	}
	contentType := headersReceived.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected application/json for content-type but got %v", contentType)
	}
	auth := headersReceived.Get("Authorization")
	if auth != "Bearer my-pat-token" {
		t.Errorf("expected Bearermy-pat-token for authorization but got %v", auth)
	}
}

func TestValidateAPICredentialsWithCloudWithError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	time.Sleep(1 * time.Second)
	overrides := make(map[string]string)
	confDir := filepath.Join(t.TempDir(), "ddcTest")
	err := os.Mkdir(confDir, 0700)
	if err != nil {
		t.Fatalf("missing conf dir %v", err)
	}
	ddcYaml := filepath.Join(confDir, "ddc.yaml")
	err = os.WriteFile(ddcYaml, []byte(fmt.Sprintf(`
dremio-endpoint: %v
dremio-pat-token: my-pat-token
is-dremio-cloud: true
dremio-cloud-project-id: 1234
`, server.URL)), 0600)
	if err != nil {
		t.Fatalf("missing conf file %v", err)
	}
	c, err := conf.ReadConf(overrides, ddcYaml)
	if err != nil {
		t.Fatalf("unable to read conf %v", err)
	}
	err = apicollect.ValidateAPICredentials(c)
	if err == nil {
		t.Errorf("expected an error")
	}
}
