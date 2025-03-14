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

package restclient

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/shutdown"
)

func TestAPIRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusOK)
		fmt.Fprintln(rw, `{"message":"success"}`)
	}))
	defer server.Close()

	InitClient(true, 10)
	hook := shutdown.NewHook()
	defer hook.Cleanup()
	_, err := APIRequest(hook, server.URL, "token", "GET", map[string]string{})
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
}

func TestPostQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			t.Fatalf("Expected 'POST', got '%v'", req.Method)
		}
		rw.WriteHeader(http.StatusOK)
		fmt.Fprintln(rw, `{"id":"123"}`)
	}))
	defer server.Close()

	InitClient(true, 10)

	sqlbody := "{\"sql\": \"SELECT * FROM test_table\"}"
	hook := shutdown.NewHook()
	defer hook.Cleanup()
	_, err := PostQuery(hook, server.URL, "token", map[string]string{}, sqlbody)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
}

func TestPostQueryBadStatusCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	InitClient(true, 10)

	sqlbody := "{\"sql\": \"SELECT * FROM test_table\"}"
	hook := shutdown.NewHook()
	defer hook.Cleanup()
	_, err := PostQuery(hook, server.URL, "token", map[string]string{}, sqlbody)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Fatalf("Expected '400' in error message, got %v", err)
	}
}

func TestClientTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		// Delay the response
		time.Sleep(1200 * time.Millisecond)
		rw.WriteHeader(http.StatusOK)
		fmt.Fprintln(rw, `{"message":"success"}`)
	}))
	defer server.Close()

	// Init the client with a timeout of 1 second
	InitClient(true, 1)
	hook := shutdown.NewHook()
	defer hook.Cleanup()
	_, err := APIRequest(hook, server.URL, "token", "GET", map[string]string{})
	if err == nil {
		t.Fatal("Expected error due to client timeout, got nil")
	}

	// We expect a timeout error
	if err.Error() != fmt.Sprintf("API request to url %v exceeded timeout 1s", server.URL) {
		t.Fatalf("Expected timeout error, got %v", err)
	}
}
