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

	if err := os.MkdirAll(tmpOutputDir, 0700); err != nil {
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
`, tmpOutputDir)
	if _, err := w.WriteString(yamlText); err != nil {
		log.Fatal(err)
	}
	return testDDCYaml
}
func TestCaptureSystemMetrics(t *testing.T) {
	tmpDirForConf, err := os.MkdirTemp("", "ddc")
	if err != nil {
		log.Fatal(err)
	}
	yamlLocation := writeConf(tmpDirForConf)
	c, err := conf.ReadConf(make(map[string]*pflag.Flag), filepath.Dir(yamlLocation))
	if err != nil {
		log.Fatalf("reading config %v", err)
	}

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
	if len(rows) > 12 {
		t.Errorf("%v rows created by metrics file, this is too many and the default should be around 10", len(rows))
	}
	if len(rows) < 8 {
		t.Errorf("%v rows created by metrics file, this is too few and the default should be around 10", len(rows))
	}
	t.Logf("%v rows of metrics captured", len(rows))
}

func TestCreateAllDirs(t *testing.T) {
	tmpDirForConf, err := os.MkdirTemp("", "ddc")
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

// func TestFindGCLocation(t *testing.T) {
// 	expectedOutput := "1 com.dremio.dac.daemon.DremioDaemon\n2 myfoo\n3 nothing"
// 	var returnValues [][]interface{}
// 	e := []interface{}{expectedOutput, nil}
// 	returnValues = append(returnValues, e)
// 	processFlags := `1:
//     VM Arguments:
//     jvm_args: -Djava.util.logging.config.class=org.slf4j.bridge.SLF4JBridgeHandler -Djava.library.path=/opt/dremio/lib -XX:+PrintGCDetails -XX:+PrintGCDateStamps -Ddremio.plugins.path=/opt/dremio/plugins -Xmx4096m -XX:MaxDirectMemorySize=2048m -XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath=/var/log/dremio -Dio.netty.maxDirectMemory=0 -Dio.netty.tryReflectionSetAccessible=true -DMAPR_IMPALA_RA_THROTTLE -DMAPR_MAX_RA_STREAMS=400 -XX:+UseG1GC -Ddremio.log.path=/opt/dremio/data/log -Xloggc:/opt/dremio/data/log/gc.log -XX:+UseGCLogFileRotation -XX:NumberOfGCLogFiles=5 -XX:GCLogFileSize=4000k -XX:+PrintGCDetails -XX:+PrintGCTimeStamps -XX:+PrintGCDateStamps -XX:+PrintClassHistogramBeforeFullGC -XX:+PrintClassHistogramAfterFullGC -XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath=/opt/dremio/data -XX:+UseG1GC -XX:G1HeapRegionSize=32M -XX:MaxGCPauseMillis=500 -XX:InitiatingHeapOccupancyPercent=25 -XX:+PrintAdaptiveSizePolicy -XX:+PrintReferenceGC -XX:ErrorFile=/opt/dremio/data/hs_err_pid%p.log -Dzookeeper=zk-hs:2181 -Dservices.coordinator.enabled=false -Dservices.coordinator.master.enabled=false -Dservices.coordinator.master.embedded-zookeeper.enabled=false -Dservices.executor.enabled=true -Dservices.conduit.port=45679 -Dservices.node-tag=default -XX:+PrintClassHistogramBeforeFullGC -XX:+PrintClassHistogramAfterFullGC
//     java_command: com.dremio.dac.daemon.DremioDaemon
//     java_class_path (initial): /opt/dremio/conf:/opt/dremio/jars/dremio-services-coordinator-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-hive-function-registry-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-ce-sabot-serializer-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-hive2-plugin-launcher-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-ee-services-credentials-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-ee-services-accesscontrol-common-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-ce-sabot-scheduler-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-dac-common-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-services-usersessions-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-ee-services-sysflight-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-protocol-20.0.0-202201050826310141-8cc7162b-proto.jar:/opt/dremio/jars/dremio-services-telemetry-impl-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-services-jobtelemetry-client-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-hive3-plugin-launcher-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-ce-services-cachemanager-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-ee-dac-tools-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-services-base-rpc-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-services-datastore-20.0.0-202201050826310141-8cc7162b-proto.jar:/opt/dremio/jars/dremio-sabot-logical-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-services-transientstore-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-services-resourcescheduler-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-dac-daemon-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-ee-services-namespace-20.0.0-202201050826310141-8cc7162b-tests.jar:/opt/dremio/j
//     Launcher Type: SUN_STANDARD`
// 	e = []interface{}{processFlags, nil}
// 	returnValues = append(returnValues, e)
// 	mockCollector := &MockCollector{
// 		Returns: returnValues,
// 	}

// 	location, err := findGCLogLocation()
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	expected := "/opt/dremio/data/log/gc.log*"
// 	if location != expected {
// 		t.Errorf("expected '%v' but was '%v'", expected, location)
// 	}
// }
