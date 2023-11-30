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

package configcollect_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/configcollect"
	"github.com/dremio/dremio-diagnostic-collector/pkg/tests"
	"github.com/rogpeppe/go-internal/diff"
)

func TestCollectsConfFilesWithNoSecrets(t *testing.T) {
	confDir := filepath.Join(t.TempDir(), "ddc-conf")
	if err := os.Mkdir(confDir, 0700); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(confDir)
	nodeName := "node1"
	confDestination := filepath.Join(confDir, "configuration", nodeName)

	if err := os.MkdirAll(confDestination, 0700); err != nil {
		t.Fatal(err)
	}
	testDataPath, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatal(err)
	}
	ddcYaml := filepath.Join(confDir, "ddc.yaml")
	if err := os.WriteFile(ddcYaml, []byte(fmt.Sprintf(`
dremio-log-dir: %v
tmp-output-dir: %v
dremio-conf-dir: %v
node-name: %v
`, filepath.Join("testdata", "logs"),
		strings.ReplaceAll(confDir, "\\", "\\\\"),
		strings.ReplaceAll(testDataPath, "\\", "\\\\"),
		nodeName)), 0600); err != nil {
		t.Fatal(err)
	}
	overrides := make(map[string]string)
	c, err := conf.ReadConf(overrides, ddcYaml)
	if err != nil {
		t.Fatal(err)
	}

	if err := configcollect.RunCollectDremioConfig(c); err != nil {
		t.Fatal(err)
	}

	match, err := tests.MatchFile(filepath.Join("testdata", "dremio-env"), filepath.Join(confDestination, "dremio-env"))
	if err != nil {
		t.Fatal(err)
	}
	if !match {
		t.Error("expected dremio-env to match but it did not")
	}

	actual := filepath.Join(confDestination, "dremio.conf")
	expected := filepath.Join("testdata", "dremio.conf")
	match, err = tests.MatchFile(expected, actual)
	if err != nil {
		t.Fatal(err)
	}
	if !match {
		diff, err := DiffText(actual, expected)
		if err != nil {
			t.Fatal(err)
		}
		t.Errorf("expected dremio.conf to match but it did not diff is \n%v", diff)
	}

	match, err = tests.MatchFile(filepath.Join("testdata", "logback.xml"), filepath.Join(confDestination, "logback.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if !match {
		t.Error("expected dremio.conf to match but it did not")
	}

	match, err = tests.MatchFile(filepath.Join("testdata", "logback-access.xml"), filepath.Join(confDestination, "logback-access.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if !match {
		t.Error("expected dremio.conf to match but it did not")
	}
}

func DiffText(expected, actual string) (string, error) {
	e, err := os.ReadFile(expected)
	if err != nil {
		return "", fmt.Errorf("unable to read expected file: %v", err)
	}

	a, err := os.ReadFile(actual)
	if err != nil {
		return "", fmt.Errorf("unable to read actual file: %v", err)
	}
	diffResult := diff.Diff(expected, e, actual, a)

	return string(diffResult), nil
}

func TestCollectsConfFilesAndRedactDremioConf(t *testing.T) {
	confDir := filepath.Join(t.TempDir(), "ddc-conf")
	if err := os.Mkdir(confDir, 0700); err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(confDir)
	nodeName := "node1"
	confDestination := filepath.Join(confDir, "configuration", nodeName)

	if err := os.MkdirAll(confDestination, 0700); err != nil {
		t.Fatal(err)
	}
	testDataPath, err := filepath.Abs(filepath.Join("testdata", "secret-dremio"))
	if err != nil {
		t.Fatal(err)
	}
	ddcYaml := filepath.Join(confDir, "ddc.yaml")
	if err := os.WriteFile(ddcYaml, []byte(fmt.Sprintf(`
dremio-log-dir: %v
tmp-output-dir: %v
dremio-conf-dir: %v
node-name: %v
`,
		filepath.Join("testdata", "logs"),
		strings.ReplaceAll(confDir, "\\", "\\\\"),
		strings.ReplaceAll(testDataPath, "\\", "\\\\"),
		nodeName)), 0600); err != nil {
		t.Fatal(err)
	}
	overrides := make(map[string]string)
	c, err := conf.ReadConf(overrides, ddcYaml)
	if err != nil {
		t.Fatal(err)
	}

	if err := configcollect.RunCollectDremioConfig(c); err != nil {
		t.Fatal(err)
	}

	match, err := tests.MatchFile(filepath.Join("testdata", "dremio.conf"), filepath.Join(confDestination, "dremio.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if match {
		t.Error("expected dremio.conf to not match because we should have modified the file due to the password in it")
	}
	text, err := os.ReadFile(filepath.Join(confDestination, "dremio.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(text), "hidemeplease") {
		t.Errorf("expected text '%v' to not contain the hidden password", string(text))
	}

	if !strings.Contains(string(text), "REMOVED_POTENTIAL_SECRET") {
		t.Errorf("expected text '%v' to contain the REMOVED_POTENTIAL_SECRET but did not", string(text))
	}
}
