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

// cmd package contains all the command line flag and initialization logic for commands
package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/nodeinfocollect"
	"github.com/spf13/pflag"
)

func writeConf(tmpOutputDir string) string {

	cleaned := filepath.Clean(tmpOutputDir)
	if err := os.MkdirAll(cleaned, 0700); err != nil {
		log.Fatal(err)
	}
	testDDCYaml := filepath.Join(tmpOutputDir, "ddc.yaml")
	w, err := os.Create(testDDCYaml)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := w.Close(); err != nil {
			log.Printf("WARN: unable to close %v with reason '%v'", testDDCYaml, err)
		}
	}()
	yamlText := fmt.Sprintf(`verbose: vvvv
tmp-output-dir: %v
node-metrics-collect-duration-seconds: 10
"
`, strings.ReplaceAll(tmpOutputDir, "\\", "\\\\"))
	if _, err := w.WriteString(yamlText); err != nil {
		log.Fatal(err)
	}
	return testDDCYaml
}

func TestCaptureSystemMetrics(t *testing.T) {
	tmpDirForConf := t.TempDir() + string(filepath.Separator) + "ddc"
	err := os.Mkdir(tmpDirForConf, 0700)
	if err != nil {
		log.Fatal(err)
	}
	yamlLocation := writeConf(tmpDirForConf)
	c, err := conf.ReadConf(make(map[string]*pflag.Flag), filepath.Dir(yamlLocation))
	if err != nil {
		log.Fatalf("reading config %v", err)
	}
	log.Printf("NODE INFO DIR %v", c.NodeInfoOutDir())
	if err := os.MkdirAll(c.NodeInfoOutDir(), 0700); err != nil {
		t.Errorf("cannot make output dir due to error %v", err)
	}
	defer func() {
		if err := os.RemoveAll(c.NodeInfoOutDir()); err != nil {
			t.Logf("error cleaning up dir %v due to error %v", c.NodeInfoOutDir(), err)
		}
	}()
	if err := runCollectNodeMetrics(c); err != nil {
		t.Errorf("expected no errors but had %v", err)
	}
	metricsFile := filepath.Join(c.NodeInfoOutDir(), "metrics.json")
	fs, err := os.Stat(metricsFile)
	if err != nil {
		t.Errorf("expected to find file but got error %v", err)
	}
	if fs.Size() == 0 {
		t.Errorf("should not have an empty file")
	}
	f, err := os.Open(metricsFile)
	if err != nil {
		t.Errorf("while opening file %v we had error %v", metricsFile, err)
	}
	scanner := bufio.NewScanner(f)
	var rows []nodeinfocollect.SystemMetricsRow
	for scanner.Scan() {
		var row nodeinfocollect.SystemMetricsRow
		text := scanner.Text()
		if err := json.Unmarshal([]byte(text), &row); err != nil {
			t.Errorf("unable to convert text %v to json due to error %v", text, err)
		}
		rows = append(rows, row)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if len(rows) > 12 {
		t.Errorf("%v rows created by metrics file, this is too many and the default should be around 10", len(rows))
	}
	if len(rows) < 8 {
		t.Errorf("%v rows created by metrics file, this is too few and the default should be around 10", len(rows))
	}
	t.Logf("%v rows of metrics captured", len(rows))
}

func TestCreateAllDirs(t *testing.T) {
	tmpDirForConf := filepath.Join(t.TempDir(), "ddc")
	err := os.Mkdir(tmpDirForConf, 0700)
	if err != nil {
		log.Fatal(err)
	}
	yamlLocation := writeConf(tmpDirForConf)
	c, err := conf.ReadConf(make(map[string]*pflag.Flag), filepath.Dir(yamlLocation))
	if err != nil {
		log.Fatalf("reading config %v", err)
	}
	err = createAllDirs(c)
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}

	// WLM should end with nodename
	if !strings.HasSuffix(c.WLMOutDir(), c.NodeName()) {
		t.Errorf("expected %v to end with %v", c.WLMOutDir(), c.NodeName())
	}
	// System table should end with nodename
	if !strings.HasSuffix(c.SystemTablesOutDir(), c.NodeName()) {
		t.Errorf("expected %v to end with %v", c.SystemTablesOutDir(), c.NodeName())
	}
	// job profiles should end with nodename
	if !strings.HasSuffix(c.JobProfilesOutDir(), c.NodeName()) {
		t.Errorf("expected %v to end with %v", c.JobProfilesOutDir(), c.NodeName())
	}
	// kvreport should end with nodename
	// job profiles should end with nodename
	if !strings.HasSuffix(c.KVstoreOutDir(), c.NodeName()) {
		t.Errorf("expected %v to end with %v", c.KVstoreOutDir(), c.NodeName())
	}
}
