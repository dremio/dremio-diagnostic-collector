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

package jvmcollect_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/jvmcollect"
)

func TestJFRCapture(t *testing.T) {
	jarLoc := filepath.Join("testdata", "demo.jar")
	cmd := exec.Command("java", "-jar", "-Dmyflag=1", "-Xmx128M", jarLoc)
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start() failed with %s\n", err)
	}

	defer func() {
		if err := cmd.Process.Kill(); err != nil {
			t.Fatalf("failed to kill process: %s", err)
		} else {
			t.Log("Process killed successfully.")
		}
	}()
	overrides := make(map[string]string)
	confDir := filepath.Join(t.TempDir(), "ddcyaml")
	if err := os.Mkdir(confDir, 0700); err != nil {
		t.Fatal(err)
	}
	tmpOutDir := filepath.Join(t.TempDir(), "ddcout")
	if err := os.Mkdir(tmpOutDir, 0700); err != nil {
		t.Fatal(err)
	}
	jfrOutDir := filepath.Join(tmpOutDir, "jfr")
	if err := os.Mkdir(jfrOutDir, 0700); err != nil {
		t.Fatal(err)
	}
	nodeName := "node1"
	ddcYamlString := fmt.Sprintf(`
tmp-output-dir: %v
node-name: %v
dremio-pid: %v
dremio-jfr-time-seconds: 2

`, strings.ReplaceAll(tmpOutDir, "\\", "\\\\"),
		nodeName,
		cmd.Process.Pid,
	)
	if err := os.WriteFile(filepath.Join(confDir, "ddc.yaml"), []byte(ddcYamlString), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := conf.ReadConf(overrides, confDir)
	if err != nil {
		t.Fatal(err)
	}
	err = jvmcollect.RunCollectJFR(c)
	if err != nil {
		t.Fatalf("expected no error but got %v", err)
	}

	f, err := os.Stat(filepath.Join(jfrOutDir, fmt.Sprintf("%v.jfr", nodeName)))
	if err != nil {
		t.Fatal(err)
	}
	if f.Size() == 0 {
		t.Errorf("expected a non empty file for the hprof but we got one")
	}
}
