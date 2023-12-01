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

// package jvmcollect handles parsing of the jvm information
package jvmcollect_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/jvmcollect"
	"github.com/dremio/dremio-diagnostic-collector/pkg/tests"
)

func TestHeapDumpCapture(t *testing.T) {
	jarLoc := filepath.Join("testdata", "demo.jar")
	cmd := exec.Command("java", "-jar", "-Dmyflag=1", "-Xmx128M", jarLoc)
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start() failed with %s\n", err)
	}

	defer func() {
		//in windows we may need a bit more time to kill the process
		if runtime.GOOS == "windows" {
			time.Sleep(500 * time.Millisecond)
		}
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
	heapDumpOutDir := filepath.Join(tmpOutDir, "heap-dumps")
	if err := os.Mkdir(heapDumpOutDir, 0700); err != nil {
		t.Fatal(err)
	}
	nodeName := "node1"
	ddcYamlString := fmt.Sprintf(`
dremio-log-dir: %v
dremio-conf-dir: %v
tmp-output-dir: %v
node-name: %v
dremio-pid: %v
`, filepath.Join("testdata", "logs"),
		filepath.Join("testdata", "conf"),
		strings.ReplaceAll(tmpOutDir, "\\", "\\\\"),
		nodeName,
		cmd.Process.Pid,
	)
	ddcYaml := filepath.Join(confDir, "ddc.yaml")
	if err := os.WriteFile(ddcYaml, []byte(ddcYamlString), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := conf.ReadConf(overrides, ddcYaml)
	if err != nil {
		t.Fatal(err)
	}
	err = jvmcollect.RunCollectHeapDump(c)
	if err != nil {
		t.Fatalf("expected no error but got %v", err)
	}

	f, err := os.Stat(filepath.Join(heapDumpOutDir, fmt.Sprintf("%v.hprof.gz", nodeName)))
	if err != nil {
		t.Fatal(err)
	}
	if f.Size() == 0 {
		t.Errorf("expected a non empty file for the hprof but we got one")
	}

	_, err = os.Stat(filepath.Join(heapDumpOutDir, fmt.Sprintf("%v.hprof", nodeName)))
	if err == nil {
		t.Fatal("expected no file to match this, which means it was not deleted")
	}

	actual := filepath.Join(heapDumpOutDir, "node1.hprof.gz")
	expected := filepath.Join(heapDumpOutDir, "node1.hprof")

	tests.ExtractGZip(t, actual, expected)
	if match, err := tests.ContainThisFileInTheGzip(expected, actual); !match && err != nil {
		t.Errorf("expected %v file to contain %v but it did not", expected, actual)
	}

	f, err = os.Stat(filepath.Join(heapDumpOutDir, fmt.Sprintf("%v.hprof", nodeName)))
	if err != nil {
		t.Fatal(err)
	}
	if f.Size() == 0 {
		t.Errorf("expected a non empty file for the hprof but we got one")
	}
}
