// Copyright 2023 Dremio Corporation
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
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"strings"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/masking"
)

// buildTarGz creates a tar.gz archive in memory from a map of filename to content.
func buildTarGz(t *testing.T, files map[string]string) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0o600,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("writing tar header for %s: %v", name, err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("writing tar content for %s: %v", name, err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("closing tar writer: %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("closing gzip writer: %v", err)
	}
	return &buf
}

// extractTarGz reads a tar.gz archive and returns a map of filename to content.
func extractTarGz(t *testing.T, data *bytes.Buffer) map[string]string {
	t.Helper()
	result := make(map[string]string)

	gzr, err := gzip.NewReader(data)
	if err != nil {
		t.Fatalf("creating gzip reader: %v", err)
	}
	defer func() { _ = gzr.Close() }()

	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("reading tar entry: %v", err)
		}
		var buf bytes.Buffer
		if _, err := io.Copy(&buf, tr); err != nil {
			t.Fatalf("reading content of %s: %v", hdr.Name, err)
		}
		result[hdr.Name] = buf.String()
	}
	return result
}

func TestStreamingMaskFilter_ConfigFilesMasked(t *testing.T) {
	confContent := `paths: {
  local: "/data"
}
services: {
  javax.net.ssl {
    keyStorePassword: "my-secret-password",
    trustStorePassword: "another-secret"
  }
}`
	input := buildTarGz(t, map[string]string{
		"dremio.conf": confContent,
	})

	filter := masking.NewStreamingMaskFilter()
	var output bytes.Buffer
	if err := filter.Filter(&output, input); err != nil {
		t.Fatalf("filter error: %v", err)
	}

	files := extractTarGz(t, &output)
	masked, ok := files["dremio.conf"]
	if !ok {
		t.Fatal("dremio.conf not found in output")
	}

	if strings.Contains(masked, "my-secret-password") {
		t.Error("keyStorePassword was not masked")
	}
	if strings.Contains(masked, "another-secret") {
		t.Error("trustStorePassword was not masked")
	}
	if !strings.Contains(masked, `<REMOVED_POTENTIAL_SECRET>`) {
		t.Error("expected masking marker not found in output")
	}
	// Non-secret lines should be unchanged.
	if !strings.Contains(masked, `local: "/data"`) {
		t.Error("non-secret line was unexpectedly modified")
	}
}

func TestStreamingMaskFilter_NonConfigPassThrough(t *testing.T) {
	logContent := "2024-01-01 INFO Starting server\npassword visible in logs is fine\n"
	input := buildTarGz(t, map[string]string{
		"server.log": logContent,
	})

	filter := masking.NewStreamingMaskFilter()
	var output bytes.Buffer
	if err := filter.Filter(&output, input); err != nil {
		t.Fatalf("filter error: %v", err)
	}

	files := extractTarGz(t, &output)
	got, ok := files["server.log"]
	if !ok {
		t.Fatal("server.log not found in output")
	}
	if got != logContent {
		t.Errorf("non-config file was modified:\nexpected: %q\ngot:      %q", logContent, got)
	}
}

func TestStreamingMaskFilter_MixedEntries(t *testing.T) {
	confContent := `services.credentials.access_key: "AKIAIOSFODNN7EXAMPLE"`
	logContent := "just a log line\n"

	input := buildTarGz(t, map[string]string{
		"conf/dremio.conf": confContent,
		"logs/server.log":  logContent,
	})

	filter := masking.NewStreamingMaskFilter()
	var output bytes.Buffer
	if err := filter.Filter(&output, input); err != nil {
		t.Fatalf("filter error: %v", err)
	}

	files := extractTarGz(t, &output)

	// Config file should be masked.
	maskedConf, ok := files["conf/dremio.conf"]
	if !ok {
		t.Fatal("conf/dremio.conf not found in output")
	}
	if strings.Contains(maskedConf, "AKIAIOSFODNN7EXAMPLE") {
		t.Error("access_key value was not masked in config file")
	}

	// Log file should be unchanged.
	gotLog, ok := files["logs/server.log"]
	if !ok {
		t.Fatal("logs/server.log not found in output")
	}
	if gotLog != logContent {
		t.Errorf("log file was modified:\nexpected: %q\ngot:      %q", logContent, gotLog)
	}
}

func TestStreamingMaskFilter_EmptyStream(t *testing.T) {
	// Build a tar.gz with no entries.
	input := buildTarGz(t, map[string]string{})

	filter := masking.NewStreamingMaskFilter()
	var output bytes.Buffer
	if err := filter.Filter(&output, input); err != nil {
		t.Fatalf("filter error: %v", err)
	}

	// Output should be a valid tar.gz with no entries.
	files := extractTarGz(t, &output)
	if len(files) != 0 {
		t.Errorf("expected empty archive, got %d entries", len(files))
	}
}

func TestStreamingMaskFilter_SiteXmlMasked(t *testing.T) {
	// The existing masking logic detects lines containing secret keywords
	// (passw, access_key, secret) and masks the value after a colon.
	// XML property files sometimes embed credentials in attribute or inline form.
	xmlContent := `<configuration>
  <property name="dfs.namenode.password" value="super-secret"/>
  password: "inline-secret-value"
  <property name="safe.setting" value="public"/>
</configuration>`

	input := buildTarGz(t, map[string]string{
		"core-site.xml": xmlContent,
	})

	filter := masking.NewStreamingMaskFilter()
	var output bytes.Buffer
	if err := filter.Filter(&output, input); err != nil {
		t.Fatalf("filter error: %v", err)
	}

	files := extractTarGz(t, &output)
	masked, ok := files["core-site.xml"]
	if !ok {
		t.Fatal("core-site.xml not found in output")
	}
	// The line with "password:" should be masked.
	if strings.Contains(masked, "inline-secret-value") {
		t.Error("password value was not masked in site xml file")
	}
	// The safe setting line should be untouched.
	if !strings.Contains(masked, `safe.setting`) {
		t.Error("safe setting was unexpectedly modified")
	}
}

func TestMaskConfigData_JSONSecrets(t *testing.T) {
	jsonContent := `{
  "clientId": "my-app",
  "clientSecret": "real-secret-value",
  "password": "super-secret",
  "endpoint": "https://example.com"
}`
	masked := string(masking.MaskConfigData([]byte(jsonContent)))

	if strings.Contains(masked, "real-secret-value") {
		t.Error("clientSecret value was not masked")
	}
	if strings.Contains(masked, "super-secret") {
		t.Error("password value was not masked")
	}
	if !strings.Contains(masked, `<REMOVED_POTENTIAL_SECRET>`) {
		t.Error("expected masking marker not found")
	}
	// Non-secret lines should be preserved.
	if !strings.Contains(masked, `"clientId"`) {
		t.Error("non-secret key clientId was unexpectedly modified")
	}
	if !strings.Contains(masked, `"endpoint"`) {
		t.Error("non-secret key endpoint was unexpectedly modified")
	}
}

func TestStreamingMaskFilter_LogbackXmlMatched(t *testing.T) {
	xmlContent := `<configuration>
  <appender name="FILE" class="ch.qos.logback.core.FileAppender">
    <file>/var/log/dremio/server.log</file>
    password: "logback-secret"
  </appender>
</configuration>`
	input := buildTarGz(t, map[string]string{
		"logback.xml": xmlContent,
	})

	filter := masking.NewStreamingMaskFilter()
	var output bytes.Buffer
	if err := filter.Filter(&output, input); err != nil {
		t.Fatalf("filter error: %v", err)
	}

	files := extractTarGz(t, &output)
	masked, ok := files["logback.xml"]
	if !ok {
		t.Fatal("logback.xml not found in output")
	}
	if strings.Contains(masked, "logback-secret") {
		t.Error("password value was not masked in logback.xml")
	}
}

func TestStreamingMaskFilter_LogbackAccessXmlMatched(t *testing.T) {
	input := buildTarGz(t, map[string]string{
		"logback-access.xml": `password: "access-secret"`,
	})

	filter := masking.NewStreamingMaskFilter()
	var output bytes.Buffer
	if err := filter.Filter(&output, input); err != nil {
		t.Fatalf("filter error: %v", err)
	}

	files := extractTarGz(t, &output)
	masked, ok := files["logback-access.xml"]
	if !ok {
		t.Fatal("logback-access.xml not found in output")
	}
	if strings.Contains(masked, "access-secret") {
		t.Error("password value was not masked in logback-access.xml")
	}
}

func TestStreamingMaskFilter_JSONFileMatched(t *testing.T) {
	jsonContent := `{"clientSecret": "json-secret-value", "name": "safe"}`
	input := buildTarGz(t, map[string]string{
		"sso.json": jsonContent,
	})

	filter := masking.NewStreamingMaskFilter()
	var output bytes.Buffer
	if err := filter.Filter(&output, input); err != nil {
		t.Fatalf("filter error: %v", err)
	}

	files := extractTarGz(t, &output)
	masked, ok := files["sso.json"]
	if !ok {
		t.Fatal("sso.json not found in output")
	}
	if strings.Contains(masked, "json-secret-value") {
		t.Error("clientSecret value was not masked in sso.json")
	}
}

func TestConfigPatterns_IncludesLogbackAndJSON(t *testing.T) {
	filter := masking.NewStreamingMaskFilter()

	tests := []struct {
		name  string
		match bool
	}{
		{"logback.xml", true},
		{"logback-access.xml", true},
		{"logback-admin.xml", true},
		{"sso.json", true},
		{"config.json", true},
		{"dremio.conf", true},
		{"dremio-env", true},
		{"core-site.xml", true},
		{"server.log", false},
		{"random.txt", false},
	}
	for _, tc := range tests {
		// Use the filter's streaming path to verify pattern matching: build
		// a tar.gz with a secret line and check if it gets masked.
		content := `password: "test-secret"`
		input := buildTarGz(t, map[string]string{tc.name: content})
		var output bytes.Buffer
		if err := filter.Filter(&output, input); err != nil {
			t.Fatalf("filter error for %s: %v", tc.name, err)
		}
		files := extractTarGz(t, &output)
		got := files[tc.name]
		wasMasked := !strings.Contains(got, "test-secret")
		if wasMasked != tc.match {
			t.Errorf("%s: expected match=%v, got masked=%v", tc.name, tc.match, wasMasked)
		}
	}
}
