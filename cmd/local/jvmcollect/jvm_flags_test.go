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

// package jvmcollect_test validates the jvmcollect package
package jvmcollect_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/jvmcollect"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/collects"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/shutdown"
)

func TestJvmFlagsAreWritten(t *testing.T) {
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
	if err := os.Mkdir(confDir, 0o700); err != nil {
		t.Fatal(err)
	}
	tmpOutDir := filepath.Join(t.TempDir(), "ddcout")
	if err := os.Mkdir(tmpOutDir, 0o700); err != nil {
		t.Fatal(err)
	}
	nodeName := "node1"

	ddcYamlString := fmt.Sprintf(`
dremio-log-dir: %v
dremio-conf-dir: %v
tarball-out-dir: %v
node-name: %v
dremio-pid: %v
`, filepath.Join("testdata", "logs"),
		filepath.Join("testdata", "conf"),
		strings.ReplaceAll(tmpOutDir, "\\", "\\\\"),
		nodeName,
		cmd.Process.Pid,
	)
	ddcYaml := filepath.Join(confDir, "ddc.yaml")
	if err := os.WriteFile(ddcYaml, []byte(ddcYamlString), 0o600); err != nil {
		t.Fatal(err)
	}
	hook := shutdown.NewHook()
	defer hook.Cleanup()
	c, err := conf.ReadConf(hook, overrides, ddcYaml, collects.StandardCollection)
	if err != nil {
		t.Fatal(err)
	}
	// make the dir..this simulates existing work that happens inside of local.go
	nodeInfoDir := filepath.Join(c.OutputDir(), "node-info", nodeName)
	if err := os.MkdirAll(nodeInfoDir, 0o700); err != nil {
		t.Fatal(err)
	}

	err = jvmcollect.RunCollectJVMFlags(c, hook)
	if err != nil {
		t.Fatalf("expected no error but got %v", err)
	}
	b, err := os.ReadFile(filepath.Join(nodeInfoDir, "jvm_settings.txt"))
	if err != nil {
		t.Fatal(err)
	}
	expected := "-Dmyflag=1 -Xmx128M"
	if !strings.Contains(string(b), expected) {
		t.Errorf("expected %v to contain %v", string(b), expected)
	}
}
