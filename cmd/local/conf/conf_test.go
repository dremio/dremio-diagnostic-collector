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

package conf_test

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/collects"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/shutdown"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/simplelog"
)

var (
	tmpDir        string
	overrides     map[string]string
	err           error
	cfg           *conf.CollectConf
	tarballOutDir string
)
var ts *httptest.Server

var genericConfSetup = func(_ string) {
	tarballOutDir, err = os.MkdirTemp("", "testerDir")
	if err != nil {
		log.Fatalf("unable to create dir with error %v", err)
	}
	tmpDir, err = os.MkdirTemp("", "testdataabdc")
	if err != nil {
		log.Fatalf("unable to create dir with error %v", err)
	}

	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintln(w, "Hello, client")
	}))
	overrides = map[string]string{
		"accept-collection-consent":             "true",
		"collect-acceleration-log":              "true",
		"collect-access-log":                    "true",
		"collect-jvm-flags":                     "true",
		"dremio-pid-detection":                  "false",
		"dremio-log-dir":                        filepath.Join("testdata", "logs"),
		"node-name":                             "node1",
		"dremio-conf-dir":                       filepath.Join("testdata", "conf"),
		"output-file":                           tarballOutDir,
		"number-threads":                        "4",
		"dremio-endpoint":                       ts.URL,
		"dremio-username":                       "admin",
		"dremio-pat-token":                      "your_personal_access_token",
		"dremio-rocksdb-dir":                    "/path/to/dremio/rocksdb",
		"collect-dremio-configuration":          "true",
		"number-job-profiles":                   "10",
		"capture-heap-dump":                     "true",
		"collect-metrics":                       "true",
		"collect-os-config":                     "true",
		"collect-disk-usage":                    "true",
		"tmp-output-dir":                        "/path/to/tmp",
		"dremio-logs-num-days":                  "7",
		"queries-json-num-days":                 "7",
		"dremio-gc-file-pattern":                "*.log",
		"collect-queries-json":                  "true",
		"collect-server-logs":                   "true",
		"collect-meta-refresh-log":              "true",
		"collect-reflection-log":                "true",
		"collect-vacuum-log":                    "true",
		"collect-gc-logs":                       "true",
		"collect-hs-err-files":                  "true",
		"collect-jfr":                           "true",
		"diag-time-seconds":                     "30",
		"collect-jstack":                        "true",
		"collect-wlm":                           "true",
		"collect-top":                           "true",
		"collect-system-tables-export":          "true",
		"collect-kvstore-report":                "true",
		"collect-system-tables-timeout-seconds": "10",
		"collect-cluster-id-timeout-seconds":    "12",
	}
}

var afterEachConfTest = func() {
	// Remove the temporary directory after each test.
	err := os.RemoveAll(tmpDir)
	if err != nil {
		log.Fatalf("unable to remove conf dir with error %v", err)
	}
	if ts != nil {
		ts.Close()
	}
}

func TestConfCanUseTarballOutputDirWithAllowedFiles(t *testing.T) {
	// allowed files are ddc, ddc.log
	outDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(outDir, "ddc"), []byte("myfile"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "ddc.log"), []byte("my log"), 0o600); err != nil {
		t.Fatal(err)
	}
	logDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(logDir, "server.log"), []byte("my log"), 0o600); err != nil {
		t.Fatal(err)
	}
	confDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(confDir, "dremio.conf"), []byte("my log"), 0o600); err != nil {
		t.Fatal(err)
	}
	testOverrides := map[string]string{
		"output-file":     outDir,
		"dremio-log-dir":  logDir,
		"dremio-conf-dir": confDir,
	}
	hook := shutdown.NewHook()
	defer hook.Cleanup()
	_, err = conf.ReadConf(hook, testOverrides, collects.StandardCollection)
	if err != nil {
		t.Errorf("should not have error: %v", err)
	}
}

func TestConfCannotUseTarballOutputDirWithFiles(t *testing.T) {
	// allowed files are ddc, ddc.log
	outDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(outDir, "ddc"), []byte("myfile"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "ddc.log"), []byte("my log"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "server.log"), []byte("my log"), 0o600); err != nil {
		t.Fatal(err)
	}
	testOverrides := map[string]string{
		"output-file":    outDir,
		"dremio-log-dir": outDir,
	}
	hook := shutdown.NewHook()
	defer hook.Cleanup()
	cfg, err = conf.ReadConf(hook, testOverrides, collects.StandardCollection)
	if err == nil {
		t.Fatal("should have an error")
	}
	expected := fmt.Sprintf("cannot use directory '%v' for tarball output as it contains 1 entries: ([server.log])", outDir)
	if err.Error() != expected {
		t.Errorf("expected %v actual %v", expected, err.Error())
	}
}

func TestConfReadingWithAValidConfigurationFile(t *testing.T) {
	genericConfSetup("")
	// should parse the configuration correctly
	hook := shutdown.NewHook()
	defer hook.Cleanup()
	cfg, err = conf.ReadConf(hook, overrides, collects.StandardCollection)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if cfg == nil {
		t.Fatal("invalid conf")
	}

	if cfg.CollectAccelerationLogs() != true {
		t.Errorf("Expected CollectAccelerationLogs to be true, got false")
	}

	if cfg.CollectOSConfig() != true {
		t.Errorf("Expected CollectJVMConf to be true, got false")
	}

	if cfg.CollectJVMFlags() != true {
		t.Errorf("Expected CollectJVMConf to be true, got false")
	}
	if cfg.CollectAccessLogs() != true {
		t.Errorf("Expected CollectAccessLogs to be true, got false")
	}

	if cfg.CollectDiskUsage() != true {
		t.Errorf("Expected CollectDiskUsage to be true, got false")
	}

	if cfg.CollectDremioConfiguration() != true {
		t.Errorf("Expected CollectDremioConfiguration to be true, got false")
	}

	if cfg.CollectMetaRefreshLogs() != true {
		t.Errorf("Expected CollectMetaRefreshLogs to be true, got false")
	}

	if cfg.CollectQueriesJSON() != true {
		t.Errorf("Expected CollectQueriesJSON to be true, got false")
	}

	if cfg.CollectReflectionLogs() != true {
		t.Errorf("Expected CollectReflectionLogs to be true, got false")
	}

	if cfg.CollectVacuumLogs() != true {
		t.Errorf("Expected CollectVacuumLogs to be true, got false")
	}

	if cfg.CollectServerLogs() != true {
		t.Errorf("Expected CollectServerLogs to be true, got false")
	}

	if cfg.CollectTop() != true {
		t.Errorf("Expected CollectTop to be true, got false")
	}

	if cfg.DiagTimeSeconds() != 30 {
		t.Errorf("Expected to have 30 seconds for diag time but was %v", cfg.DiagTimeSeconds())
	}

	testConf := filepath.Join("testdata", "conf")
	if cfg.DremioConfDir() != testConf {
		t.Errorf("Expected DremioConfDir to be '%v', got '%s'", testConf, cfg.DremioConfDir())
	}
	if cfg.TarballOutDir() != tarballOutDir {
		t.Errorf("expected /my-tarball-dir but was %v", cfg.TarballOutDir())
	}
	if cfg.DremioPIDDetection() != false {
		t.Errorf("expected dremio-pid-detection to be disabled")
	}

	afterEachConfTest()
}

func TestConfReadingWhenLoggingRedactsPAT(t *testing.T) {
	genericConfSetup("")
	tempDir := t.TempDir()
	testLog := filepath.Join(tempDir, "ddc.log")
	simplelog.InitLoggerWithOutputDir(tempDir)
	// should log redacted when token is present
	hook := shutdown.NewHook()
	defer hook.Cleanup()
	cfg, err = conf.ReadConf(hook, overrides, collects.StandardCollection)
	if err != nil {
		t.Fatalf("expected no error but had %v", err)
	}
	if cfg == nil {
		t.Error("expected a valid CollectConf but it is nil")
	}
	if err := simplelog.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(testLog)
	if err != nil {
		t.Fatal(err)
	}
	out := string(b)

	if !strings.Contains(out, "conf key 'dremio-pat-token':'REDACTED'") {
		t.Errorf("expected dremio-pat-token to be redacted in '%v' but it was not", out)
	}
	afterEachConfTest()
}

func TestURLsuffix(t *testing.T) {
	testURL := "http://localhost:9047/some/path/"
	expected := "http://localhost:9047/some/path"
	actual := conf.SanitiseURL(testURL)
	if expected != actual {
		t.Errorf("\nexpected: %v\nactual: %v\n'", expected, actual)
	}

	testURL = "http://localhost:9047/some/path"
	expected = "http://localhost:9047/some/path"
	actual = conf.SanitiseURL(testURL)
	if expected != actual {
		t.Errorf("\nexpected: %v\nactual: %v\n'", expected, actual)
	}
}

func TestClusterStatsDirectory(t *testing.T) {
	genericConfSetup("")
	hook := shutdown.NewHook()
	defer hook.Cleanup()
	cfg, err = conf.ReadConf(hook, overrides, collects.StandardCollection)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if cfg == nil {
		t.Fatal("invalid conf")
	}
	outDir := cfg.ClusterStatsOutDir()
	expected := filepath.Join("cluster-stats", "node1")
	if !strings.HasSuffix(outDir, expected) {
		t.Errorf("expected %v to end with %v", outDir, expected)
	}
}

func TestParsePSForConfig(t *testing.T) {
	ps := `   /opt/java/openjdk/bin/java -Djava.util.logging.config.class=org.slf4j.bridge.SLF4JBridgeHandler -Djava.library.path=/opt/dremio/lib -XX:+PrintGCDetails -XX:+PrintGCDateStamps -Ddremio.plugins.path=/opt/dremio/plugins -Xmx2048m -XX:MaxDirectMemorySize=2048m -XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath=/var/log/dremio -Dio.netty.maxDirectMemory=0 -Dio.netty.tryReflectionSetAccessible=true -DMAPR_IMPALA_RA_THROTTLE -DMAPR_MAX_RA_STREAMS=400 -XX:+UseG1GC -Ddremio.log.path=/opt/dremio/data/WRONG -Xloggc:/opt/dremio/data/logs/gc.log -XX:+PrintGCDetails -XX:+PrintGCDateStamps -XX:+PrintTenuringDistribution -XX:+PrintGCCause -XX:+UseGCLogFileRotation -XX:NumberOfGCLogFiles=10 -XX:GCLogFileSize=5M -Dzookeeper=zk-hs:2181 -Dservices.coordinator.enabled=true -Dservices.coordinator.master.enabled=true -Dservices.coordinator.master.embedded-zookeeper.enabled=false -Dservices.executor.enabled=false -Dservices.conduit.port=45679 -Ddremio.admin-only-mode=false -XX:+PrintClassHistogramBeforeFullGC -XX:+PrintClassHistogramAfterFullGC -cp /opt/dremio/conf:/opt/dremio/jars/*:/opt/dremio/jars/ext/*:/opt/dremio/jars/3rdparty/*:/opt/java/openjdk/lib/tools.jar com.dremio.dac.daemon.DremioDaemon DREMIO_PLUGINS_DIR=/opt/dremio/plugins KUBERNETES_SERVICE_PORT_HTTPS=443 KUBERNETES_SERVICE_PORT=443 DREMIO_LOG_DIR=/var/log/dremio JAVA_MAJOR_VERSION=8 DREMIO_IN_CONTAINER=1 HOSTNAME=dremio-master-0 LANGUAGE=en_US:en JAVA_HOME=/opt/java/openjdk AWS_CREDENTIAL_PROFILES_FILE=/opt/dremio/aws/credentials DREMIO_CLIENT_PORT_32010_TCP_PROTO=tcp MALLOC_ARENA_MAX=4 ZK_CS_PORT_2181_TCP_ADDR=192.10.1.1 DREMIO_GC_LOGS_ENABLED=yes DREMIO_CLASSPATH=/opt/dremio/conf:/opt/dremio/jars/*:/opt/dremio/jars/ext/*:/opt/dremio/jars/3rdparty/*:/opt/java/openjdk/lib/tools.jar DREMIO_MAX_HEAP_MEMORY_SIZE_MB=2048 DREMIO_CLIENT_PORT_9047_TCP_PORT=9047 PWD=/opt/dremio JAVA_VERSION_STRING=1.8.0_372 DREMIO_JAVA_SERVER_EXTRA_OPTS=-Ddremio.log.path=/opt/dremio/data/logs -Xloggc:/opt/dremio/data/logs/gc.log -XX:+PrintGCDetails -XX:+PrintGCDateStamps -XX:+PrintTenuringDistribution -XX:+PrintGCCause -XX:+UseGCLogFileRotation -XX:NumberOfGCLogFiles=10 -XX:GCLogFileSize=5M -Dzookeeper=zk-hs:2181 -Dservices.coordinator.enabled=true -Dservices.coordinator.master.enabled=true -Dservices.coordinator.master.embedded-zookeeper.enabled=false -Dservices.executor.enabled=false -Dservices.conduit.port=45679 DREMIO_MAX_DIRECT_MEMORY_SIZE_MB=2048 ZK_CS_PORT_2181_TCP_PROTO=tcp MALLOC_MMAP_MAX_=65536 DREMIO_CLIENT_PORT_32010_TCP_ADDR=192.10.1.13 DREMIO_CLIENT_PORT_31010_TCP_PROTO=tcp DREMIO_CONF_DIR=/opt/dremio/conf TZ=UTC ZK_CS_PORT=tcp://10.43.15.147:2181 DREMIO_ENV_SCRIPT=dremio-env DREMIO_CLIENT_PORT_31010_TCP_ADDR=192.10.1.1 HOME=/var/lib/dremio/dremio LANG=en_US.UTF-8 KUBERNETES_PORT_443_TCP=tcp://192.10.1.1:443 ZK_CS_PORT_2181_TCP_PORT=2181 DREMIO_CLIENT_PORT_9047_TCP_PROTO=tcp LOG_TO_CONSOLE=0 DREMIO_ADMIN_ONLY=false DREMIO_CLIENT_PORT=tcp://192.10.1.13:31010 DREMIO_CLIENT_SERVICE_HOST=192.10.1.13 DREMIO_HOME=/opt/dremio ZK_CS_SERVICE_PORT_CLIENT=2181 DREMIO_CLIENT_SERVICE_PORT_WEB=9047 ZK_CS_SERVICE_PORT=2181 DREMIO_CLIENT_PORT_31010_TCP=tcp://192.10.1.13:31010 DREMIO_CLIENT_SERVICE_PORT_CLIENT=31010 DREMIO_CLIENT_PORT_9047_TCP=tcp://192.10.1.13:9047 DREMIO_PID_DIR=/var/run/dremio DREMIO_CLIENT_SERVICE_PORT=31010 MALLOC_TRIM_THRESHOLD_=131072 DREMIO_GC_OPTS=-XX:+UseG1GC SHLVL=0 DREMIO_CLIENT_PORT_31010_TCP_PORT=31010 DREMIO_GC_LOG_TO_CONSOLE=yes KUBERNETES_PORT_443_TCP_PROTO=tcp is_cygwin=false MALLOC_MMAP_THRESHOLD_=131072 KUBERNETES_PORT_443_TCP_ADDR=10.43.0.1 KUBERNETES_SERVICE_HOST=10.43.0.1 LC_ALL=en_US.UTF-8 AWS_SHARED_CREDENTIALS_FILE=/opt/dremio/aws/credentials KUBERNETES_PORT=tcp://10.43.0.1:443 DREMIO_CLIENT_PORT_9047_TCP_ADDR=192.10.1.13 KUBERNETES_PORT_443_TCP_PORT=443 PATH=/opt/java/openjdk/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin MALLOC_TOP_PAD_=131072 DREMIO_JAVA_OPTS=-Djava.util.logging.config.class=org.slf4j.bridge.SLF4JBridgeHandler -Djava.library.path=/opt/dremio/lib -XX:+PrintGCDetails -XX:+PrintGCDateStamps -Ddremio.plugins.path=/opt/dremio/plugins -Xmx2048m -XX:MaxDirectMemorySize=2048m -XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath=/var/log/dremio -Dio.netty.maxDirectMemory=0 -Dio.netty.tryReflectionSetAccessible=true -DMAPR_IMPALA_RA_THROTTLE -DMAPR_MAX_RA_STREAMS=400 -XX:+UseG1GC -Ddremio.log.path=/opt/dremio/data/logs -Xloggc:/opt/dremio/data/logs/gc.log -XX:+PrintGCDetails -XX:+PrintGCDateStamps -XX:+PrintTenuringDistribution -XX:+PrintGCCause -XX:+UseGCLogFileRotation -XX:NumberOfGCLogFiles=10 -XX:GCLogFileSize=5M -Dzookeeper=zk-hs:2181 -Dservices.coordinator.enabled=true -Dservices.coordinator.master.enabled=true -Dservices.coordinator.master.embedded-zookeeper.enabled=false -Dservices.executor.enabled=false -Dservices.conduit.port=45679 -Ddremio.admin-only-mode=false -XX:+PrintClassHistogramBeforeFullGC -XX:+PrintClassHistogramAfterFullGC DREMIO_CLIENT_PORT_32010_TCP=tcp://192.10.1.1:32010 ZK_CS_SERVICE_HOST=192.10.1.1 DREMIO_CLIENT_SERVICE_PORT_FLIGHT=32010 DREMIO_LOG_TO_CONSOLE=1 DREMIO_CLIENT_PORT_32010_TCP_PORT=32010 JAVA_VERSION=jdk8u372-b07 ZK_CS_PORT_2181_TCP=tcp://192.10.1.1:2181`
	cfgData, err := conf.ParsePSForConfig(ps)
	if err != nil {
		t.Fatal(err)
	}
	if cfgData.ConfDir != "/opt/dremio/conf" {
		t.Errorf("expected /opt/dremio/conf but was %v", cfgData.ConfDir)
	}

	if cfgData.LogDir != "/opt/dremio/data/logs" {
		t.Errorf("expected /opt/dremio/data/logs but was %v", cfgData.LogDir)
	}

	if cfgData.Home != "/opt/dremio" {
		t.Errorf("expected /opt/dremio but was %q", cfgData.Home)
	}
}

func TestParsePSForConfigWithNewLines(t *testing.T) {
	ps := "/opt/java/openjdk/bin/java -Djava.util.logging.config.class=org.slf4j.bridge.SLF4JBridgeHandler -Djava.library.path=/opt/dremio/lib -XX:+PrintGCDetails -XX:+PrintGCDateStamps -Ddremio.plugins.path=/opt/dremio/plugins -Xmx2048m -XX:MaxDirectMemorySize=2048m -XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath=/var/log/dremio -Dio.netty.maxDirectMemory=0 -Dio.netty.tryReflectionSetAccessible=true -DMAPR_IMPALA_RA_THROTTLE -DMAPR_MAX_RA_STREAMS=400 -XX:+UseG1GC -Ddremio.log.path=/opt/dremio/data/logs\n -Xloggc:/opt/dremio/data/logs/gc.log -XX:+PrintGCDetails -XX:+PrintGCDateStamps -XX:+PrintTenuringDistribution -XX:+PrintGCCause -XX:+UseGCLogFileRotation -XX:NumberOfGCLogFiles=10 -XX:GCLogFileSize=5M -Dzookeeper=zk-hs:2181 -Dservices.coordinator.enabled=true -Dservices.coordinator.master.enabled=true -Dservices.coordinator.master.embedded-zookeeper.enabled=false -Dservices.executor.enabled=false -Dservices.conduit.port=45679 -Ddremio.admin-only-mode=false -XX:+PrintClassHistogramBeforeFullGC -XX:+PrintClassHistogramAfterFullGC -cp /opt/dremio/conf:/opt/dremio/jars/*:/opt/dremio/jars/ext/*:/opt/dremio/jars/3rdparty/*:/opt/java/openjdk/lib/tools.jar com.dremio.dac.daemon.DremioDaemon DREMIO_PLUGINS_DIR=/opt/dremio/plugins KUBERNETES_SERVICE_PORT_HTTPS=443 KUBERNETES_SERVICE_PORT=443 DREMIO_LOG_DIR=/var/log/dremio\n JAVA_MAJOR_VERSION=8 DREMIO_IN_CONTAINER=1 HOSTNAME=dremio-master-0 LANGUAGE=en_US:en JAVA_HOME=/opt/java/openjdk AWS_CREDENTIAL_PROFILES_FILE=/opt/dremio/aws/credentials DREMIO_CLIENT_PORT_32010_TCP_PROTO=tcp MALLOC_ARENA_MAX=4 ZK_CS_PORT_2181_TCP_ADDR=192.10.1.1 DREMIO_GC_LOGS_ENABLED=yes DREMIO_CLASSPATH=/opt/dremio/conf:/opt/dremio/jars/*:/opt/dremio/jars/ext/*:/opt/dremio/jars/3rdparty/*:/opt/java/openjdk/lib/tools.jar DREMIO_MAX_HEAP_MEMORY_SIZE_MB=2048 DREMIO_CLIENT_PORT_9047_TCP_PORT=9047 PWD=/opt/dremio JAVA_VERSION_STRING=1.8.0_372 DREMIO_JAVA_SERVER_EXTRA_OPTS=-Ddremio.log.path=/opt/dremio/data/logs\n -Xloggc:/opt/dremio/data/logs/gc.log -XX:+PrintGCDetails -XX:+PrintGCDateStamps -XX:+PrintTenuringDistribution -XX:+PrintGCCause -XX:+UseGCLogFileRotation -XX:NumberOfGCLogFiles=10 -XX:GCLogFileSize=5M -Dzookeeper=zk-hs:2181 -Dservices.coordinator.enabled=true -Dservices.coordinator.master.enabled=true -Dservices.coordinator.master.embedded-zookeeper.enabled=false -Dservices.executor.enabled=false -Dservices.conduit.port=45679 DREMIO_MAX_DIRECT_MEMORY_SIZE_MB=2048 ZK_CS_PORT_2181_TCP_PROTO=tcp MALLOC_MMAP_MAX_=65536 DREMIO_CLIENT_PORT_32010_TCP_ADDR=192.10.1.13 DREMIO_CLIENT_PORT_31010_TCP_PROTO=tcp DREMIO_CONF_DIR=/opt/dremio/conf\n TZ=UTC ZK_CS_PORT=tcp://10.43.15.147:2181 DREMIO_ENV_SCRIPT=dremio-env DREMIO_CLIENT_PORT_31010_TCP_ADDR=192.10.1.1 HOME=/var/lib/dremio/dremio LANG=en_US.UTF-8 KUBERNETES_PORT_443_TCP=tcp://192.10.1.1:443 ZK_CS_PORT_2181_TCP_PORT=2181 DREMIO_CLIENT_PORT_9047_TCP_PROTO=tcp LOG_TO_CONSOLE=0 DREMIO_ADMIN_ONLY=false DREMIO_CLIENT_PORT=tcp://192.10.1.13:31010 DREMIO_CLIENT_SERVICE_HOST=192.10.1.13 DREMIO_HOME=/opt/dremio\n ZK_CS_SERVICE_PORT_CLIENT=2181 DREMIO_CLIENT_SERVICE_PORT_WEB=9047 ZK_CS_SERVICE_PORT=2181 DREMIO_CLIENT_PORT_31010_TCP=tcp://192.10.1.13:31010 DREMIO_CLIENT_SERVICE_PORT_CLIENT=31010 DREMIO_CLIENT_PORT_9047_TCP=tcp://192.10.1.13:9047 DREMIO_PID_DIR=/var/run/dremio DREMIO_CLIENT_SERVICE_PORT=31010 MALLOC_TRIM_THRESHOLD_=131072 DREMIO_GC_OPTS=-XX:+UseG1GC SHLVL=0 DREMIO_CLIENT_PORT_31010_TCP_PORT=31010 DREMIO_GC_LOG_TO_CONSOLE=yes KUBERNETES_PORT_443_TCP_PROTO=tcp is_cygwin=false MALLOC_MMAP_THRESHOLD_=131072 KUBERNETES_PORT_443_TCP_ADDR=10.43.0.1 KUBERNETES_SERVICE_HOST=10.43.0.1 LC_ALL=en_US.UTF-8 AWS_SHARED_CREDENTIALS_FILE=/opt/dremio/aws/credentials KUBERNETES_PORT=tcp://10.43.0.1:443 DREMIO_CLIENT_PORT_9047_TCP_ADDR=192.10.1.13 KUBERNETES_PORT_443_TCP_PORT=443 PATH=/opt/java/openjdk/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin MALLOC_TOP_PAD_=131072 DREMIO_JAVA_OPTS=-Djava.util.logging.config.class=org.slf4j.bridge.SLF4JBridgeHandler -Djava.library.path=/opt/dremio/lib -XX:+PrintGCDetails -XX:+PrintGCDateStamps -Ddremio.plugins.path=/opt/dremio/plugins -Xmx2048m -XX:MaxDirectMemorySize=2048m -XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath=/var/log/dremio -Dio.netty.maxDirectMemory=0 -Dio.netty.tryReflectionSetAccessible=true -DMAPR_IMPALA_RA_THROTTLE -DMAPR_MAX_RA_STREAMS=400 -XX:+UseG1GC -Ddremio.log.path=/opt/dremio/data/logs -Xloggc:/opt/dremio/data/logs/gc.log -XX:+PrintGCDetails -XX:+PrintGCDateStamps -XX:+PrintTenuringDistribution -XX:+PrintGCCause -XX:+UseGCLogFileRotation -XX:NumberOfGCLogFiles=10 -XX:GCLogFileSize=5M -Dzookeeper=zk-hs:2181 -Dservices.coordinator.enabled=true -Dservices.coordinator.master.enabled=true -Dservices.coordinator.master.embedded-zookeeper.enabled=false -Dservices.executor.enabled=false -Dservices.conduit.port=45679 -Ddremio.admin-only-mode=false -XX:+PrintClassHistogramBeforeFullGC -XX:+PrintClassHistogramAfterFullGC DREMIO_CLIENT_PORT_32010_TCP=tcp://192.10.1.1:32010 ZK_CS_SERVICE_HOST=192.10.1.1 DREMIO_CLIENT_SERVICE_PORT_FLIGHT=32010 DREMIO_LOG_TO_CONSOLE=1 DREMIO_CLIENT_PORT_32010_TCP_PORT=32010 JAVA_VERSION=jdk8u372-b07 ZK_CS_PORT_2181_TCP=tcp://192.10.1.1:2181"
	cfgData, err := conf.ParsePSForConfig(ps)
	if err != nil {
		t.Fatal(err)
	}
	if cfgData.ConfDir != "/opt/dremio/conf" {
		t.Errorf("expected /opt/dremio/conf but was %v", cfgData.ConfDir)
	}

	if cfgData.LogDir != "/opt/dremio/data/logs" {
		t.Errorf("expected /opt/dremio/data/logs but was %v", cfgData.LogDir)
	}

	if cfgData.Home != "/opt/dremio" {
		t.Errorf("expected /opt/dremio but was %q", cfgData.Home)
	}
}

func TestLoggingDirsHaveExpectedFiles(t *testing.T) {
	genericConfSetup("")
	// Override log dir to point at a bad directory
	overrides["dremio-log-dir"] = filepath.Join("testdata", "badlogs")
	hook := shutdown.NewHook()
	defer hook.Cleanup()
	// we expect an error since "badlogs" doesnt have the right files
	cfg, err = conf.ReadConf(hook, overrides, collects.StandardCollection)
	if cfg == nil {
		t.Error("expected a valid CollectConf but it is nil")
	}
	// typically the config directory "badlogs" does not have the right files, so
	// we fall back to the auto-detect which in this case comes up empty (expected)
	expected := `invalid dremio log dir '', set --dremio-log-dir flag: directory  does not exist`
	if !strings.Contains(err.Error(), expected) {
		t.Errorf("\nexpected:\n%v\nactual:\n%v", expected, err.Error())
	}

	// reset config (so we point to the normal logs dir)
	genericConfSetup("")
	// we don't expect an error since "logs" has the right files
	cfg, err = conf.ReadConf(hook, overrides, collects.StandardCollection)
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil {
		t.Error("expected a valid CollectConf but it is nil")
	}

	afterEachConfTest()
}

func TestEnvVarsForLogging(t *testing.T) {
	ps := `   /opt/java/openjdk/bin/java -Djava.util.logging.config.class=org.slf4j.bridge.SLF4JBridgeHandler -Djava.library.path=/opt/dremio/lib -XX:+PrintGCDetails -XX:+PrintGCDateStamps -Ddremio.plugins.path=/opt/dremio/plugins -Xmx2048m -XX:MaxDirectMemorySize=2048m -XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath=/var/log/dremio/blah -Dio.netty.maxDirectMemory=0 -Dio.netty.tryReflectionSetAccessible=true -DMAPR_IMPALA_RA_THROTTLE -DMAPR_MAX_RA_STREAMS=400 -XX:+UseG1GC -Xloggc:/opt/dremio/data/logs/gc.log -XX:+PrintGCDetails -XX:+PrintGCDateStamps -XX:+PrintTenuringDistribution -XX:+PrintGCCause -XX:+UseGCLogFileRotation -XX:NumberOfGCLogFiles=10 -XX:GCLogFileSize=5M -Dzookeeper=zk-hs:2181 -Dservices.coordinator.enabled=true -Dservices.coordinator.master.enabled=true -Dservices.coordinator.master.embedded-zookeeper.enabled=false -Dservices.executor.enabled=false -Dservices.conduit.port=45679 -Ddremio.admin-only-mode=false -XX:+PrintClassHistogramBeforeFullGC -XX:+PrintClassHistogramAfterFullGC -cp /opt/dremio/conf:/opt/dremio/jars/*:/opt/dremio/jars/ext/*:/opt/dremio/jars/3rdparty/*:/opt/java/openjdk/lib/tools.jar com.dremio.dac.daemon.DremioDaemon DREMIO_PLUGINS_DIR=/opt/dremio/plugins KUBERNETES_SERVICE_PORT_HTTPS=443 KUBERNETES_SERVICE_PORT=443 DREMIO_LOG_DIR=/var/log/dremio/backup JAVA_MAJOR_VERSION=8 DREMIO_IN_CONTAINER=1 HOSTNAME=dremio-master-0 LANGUAGE=en_US:en JAVA_HOME=/opt/java/openjdk AWS_CREDENTIAL_PROFILES_FILE=/opt/dremio/aws/credentials DREMIO_CLIENT_PORT_32010_TCP_PROTO=tcp MALLOC_ARENA_MAX=4 ZK_CS_PORT_2181_TCP_ADDR=192.10.1.1 DREMIO_GC_LOGS_ENABLED=yes DREMIO_CLASSPATH=/opt/dremio/conf:/opt/dremio/jars/*:/opt/dremio/jars/ext/*:/opt/dremio/jars/3rdparty/*:/opt/java/openjdk/lib/tools.jar DREMIO_MAX_HEAP_MEMORY_SIZE_MB=2048 DREMIO_CLIENT_PORT_9047_TCP_PORT=9047 PWD=/opt/dremio JAVA_VERSION_STRING=1.8.0_372 -Xloggc:/opt/dremio/data/logs/gc.log -XX:+PrintGCDetails -XX:+PrintGCDateStamps -XX:+PrintTenuringDistribution -XX:+PrintGCCause -XX:+UseGCLogFileRotation -XX:NumberOfGCLogFiles=10 -XX:GCLogFileSize=5M -Dzookeeper=zk-hs:2181 -Dservices.coordinator.enabled=true -Dservices.coordinator.master.enabled=true -Dservices.coordinator.master.embedded-zookeeper.enabled=false -Dservices.executor.enabled=false -Dservices.conduit.port=45679 DREMIO_MAX_DIRECT_MEMORY_SIZE_MB=2048 ZK_CS_PORT_2181_TCP_PROTO=tcp MALLOC_MMAP_MAX_=65536 DREMIO_CLIENT_PORT_32010_TCP_ADDR=192.10.1.13 DREMIO_CLIENT_PORT_31010_TCP_PROTO=tcp DREMIO_CONF_DIR=/opt/dremio/conf TZ=UTC ZK_CS_PORT=tcp://10.43.15.147:2181 DREMIO_ENV_SCRIPT=dremio-env DREMIO_CLIENT_PORT_31010_TCP_ADDR=192.10.1.1 HOME=/var/lib/dremio/dremio LANG=en_US.UTF-8 KUBERNETES_PORT_443_TCP=tcp://192.10.1.1:443 ZK_CS_PORT_2181_TCP_PORT=2181 DREMIO_CLIENT_PORT_9047_TCP_PROTO=tcp LOG_TO_CONSOLE=0 DREMIO_ADMIN_ONLY=false DREMIO_CLIENT_PORT=tcp://192.10.1.13:31010 DREMIO_CLIENT_SERVICE_HOST=192.10.1.13 DREMIO_HOME=/opt/dremio ZK_CS_SERVICE_PORT_CLIENT=2181 DREMIO_CLIENT_SERVICE_PORT_WEB=9047 ZK_CS_SERVICE_PORT=2181 DREMIO_CLIENT_PORT_31010_TCP=tcp://192.10.1.13:31010 DREMIO_CLIENT_SERVICE_PORT_CLIENT=31010 DREMIO_CLIENT_PORT_9047_TCP=tcp://192.10.1.13:9047 DREMIO_PID_DIR=/var/run/dremio DREMIO_CLIENT_SERVICE_PORT=31010 MALLOC_TRIM_THRESHOLD_=131072 DREMIO_GC_OPTS=-XX:+UseG1GC SHLVL=0 DREMIO_CLIENT_PORT_31010_TCP_PORT=31010 DREMIO_GC_LOG_TO_CONSOLE=yes KUBERNETES_PORT_443_TCP_PROTO=tcp is_cygwin=false MALLOC_MMAP_THRESHOLD_=131072 KUBERNETES_PORT_443_TCP_ADDR=10.43.0.1 KUBERNETES_SERVICE_HOST=10.43.0.1 LC_ALL=en_US.UTF-8 AWS_SHARED_CREDENTIALS_FILE=/opt/dremio/aws/credentials KUBERNETES_PORT=tcp://10.43.0.1:443 DREMIO_CLIENT_PORT_9047_TCP_ADDR=192.10.1.13 KUBERNETES_PORT_443_TCP_PORT=443 PATH=/opt/java/openjdk/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin MALLOC_TOP_PAD_=131072 DREMIO_JAVA_OPTS=-Djava.util.logging.config.class=org.slf4j.bridge.SLF4JBridgeHandler -Djava.library.path=/opt/dremio/lib -XX:+PrintGCDetails -XX:+PrintGCDateStamps -Ddremio.plugins.path=/opt/dremio/plugins -Xmx2048m -XX:MaxDirectMemorySize=2048m -XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath=/var/log/dremio -Dio.netty.maxDirectMemory=0 -Dio.netty.tryReflectionSetAccessible=true -DMAPR_IMPALA_RA_THROTTLE -DMAPR_MAX_RA_STREAMS=400 -XX:+UseG1GC -Xloggc:/opt/dremio/data/logs/gc.log -XX:+PrintGCDetails -XX:+PrintGCDateStamps -XX:+PrintTenuringDistribution -XX:+PrintGCCause -XX:+UseGCLogFileRotation -XX:NumberOfGCLogFiles=10 -XX:GCLogFileSize=5M -Dzookeeper=zk-hs:2181 -Dservices.coordinator.enabled=true -Dservices.coordinator.master.enabled=true -Dservices.coordinator.master.embedded-zookeeper.enabled=false -Dservices.executor.enabled=false -Dservices.conduit.port=45679 -Ddremio.admin-only-mode=false -XX:+PrintClassHistogramBeforeFullGC -XX:+PrintClassHistogramAfterFullGC DREMIO_CLIENT_PORT_32010_TCP=tcp://192.10.1.1:32010 ZK_CS_SERVICE_HOST=192.10.1.1 DREMIO_CLIENT_SERVICE_PORT_FLIGHT=32010 DREMIO_LOG_TO_CONSOLE=1 DREMIO_CLIENT_PORT_32010_TCP_PORT=32010 JAVA_VERSION=jdk8u372-b07 ZK_CS_PORT_2181_TCP=tcp://192.10.1.1:2181`
	expected := "/var/log/dremio/backup"
	genericConfSetup("")

	// Parse the ps line for logs, expect fallback to env var
	psConf, err := conf.ParsePSForConfig(ps)
	if err != nil {
		t.Fatal(err)
	}
	// we expect the Log dir to not have picked things up from the PS config above, instead reading if from the ENV var
	if psConf.LogDir != expected {
		t.Errorf("\nexpected:\n%v\nactual:\n%v", expected, psConf.LogDir)
	}

	afterEachConfTest()
}

func TestDiagnosisModeWithoutPAT(t *testing.T) {
	// Without PAT token, diagnosis mode should still succeed but REST API features disabled
	genericConfSetup("")
	// Override to set specific values and remove PAT
	overrides["dremio-log-dir"] = filepath.Join("testdata", "logs")
	overrides["dremio-conf-dir"] = filepath.Join("testdata", "conf")
	overrides["dremio-endpoint"] = "http://localhost:9047"
	overrides["dremio-username"] = "admin"
	delete(overrides, "dremio-pat-token")
	defer afterEachConfTest()
	hook := shutdown.NewHook()
	defer hook.Cleanup()

	cfg, err := conf.ReadConf(hook, overrides, collects.DiagnosisCollection)
	if err != nil {
		t.Errorf("Expected success without PAT in diagnosis mode, got error: %v", err)
	}
	if cfg == nil {
		t.Fatal("invalid conf")
	}

}
