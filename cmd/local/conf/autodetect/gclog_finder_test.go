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

package autodetect_test

import (
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/conf/autodetect"
)

func TestParseGCLogFromFlags_WhenJVMFlagsAreGiven(t *testing.T) {
	// Should parse the GC log location correctly"

	processFlags := `    519 ?        Ssl  192:04 /usr/lib/jvm/bellsoft-java8-amd64/bin/java -Djava.util.logging.config.class=org.slf4j.bridge.SLF4JBridgeHandler -Djava.library.path=/opt/dremio/lib -XX:+PrintGCDetails -XX:+PrintGCDateStamps -Xloggc:/opt/dremio/log/server.gc -Ddremio.log.path=/opt/dremio/log -Ddremio.plugins.path=/opt/dremio/plugins -Xmx4096m -XX:MaxDirectMemorySize=8192m -XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath=/opt/dremio/log -Dio.netty.maxDirectMemory=0 -Dio.netty.tryReflectionSetAccessible=true -DMAPR_IMPALA_RA_THROTTLE -DMAPR_MAX_RA_STREAMS=400 -XX:+UseG1GC -Ddremio.log.path=/opt/dremio/log/ -XX:+UseGCLogFileRotation -XX:NumberOfGCLogFiles=5 -XX:GCLogFileSize=4000k -XX:+UseGCLogFileRotation -Xloggc:/opt/dremio/log/gc.log -XX:+PrintGCDetails -XX:+PrintGCTimeStamps -XX:+PrintGCDateStamps -XX:+PrintClassHistogramBeforeFullGC -XX:+PrintClassHistogramAfterFullGC -XX:+PrintClassHistogramBeforeFullGC -XX:+PrintClassHistogramAfterFullGC -cp /opt/dremio/conf:/opt/dremio/jars/*:/opt/dremio/jars/ext/*:/opt/dremio/jars/3rdparty/*:/usr/lib/jvm/bellsoft-java8-amd64/lib/tools.jar com.dremio.dac.daemon.DremioDaemon`
	gcRegex, gcLogLocation, err := autodetect.ParseGCLogFromFlags(processFlags)
	if err != nil {
		t.Errorf("expected no error but we have %v", err)
	}
	expected := "/opt/dremio/log"
	if gcLogLocation != expected {
		t.Errorf("expected %v but was %v", gcLogLocation, expected)
	}
	expected = "*gc.log*"
	if gcRegex != expected {
		t.Errorf("expected %v but was %v", expected, gcRegex)
	}
}

func TestParseGCLogFromFlagsWithExtraLogFileLine(t *testing.T) {
	//"Should parse the GC log location correctly", func() {
	processFlags := `    519 ?        Ssl  192:04 /usr/lib/jvm/bellsoft-java8-amd64/bin/java -Djava.util.logging.config.class=org.slf4j.bridge.SLF4JBridgeHandler -Djava.library.path=/opt/dremio/lib -XX:+PrintGCDetails -XX:+PrintGCDateStamps -Xloggc:/opt/dremio/wrong/server.gc -Xloggc:/opt/dremio/log/server.gc -Ddremio.log.path=/opt/dremio/log -Ddremio.plugins.path=/opt/dremio/plugins -Xmx4096m -XX:MaxDirectMemorySize=8192m -XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath=/opt/dremio/log -Dio.netty.maxDirectMemory=0 -Dio.netty.tryReflectionSetAccessible=true -DMAPR_IMPALA_RA_THROTTLE -DMAPR_MAX_RA_STREAMS=400 -XX:+UseG1GC -Ddremio.log.path=/opt/dremio/log/ -XX:+UseGCLogFileRotation -XX:NumberOfGCLogFiles=5 -XX:GCLogFileSize=4000k -XX:+UseGCLogFileRotation -Xloggc:/opt/dremio/log/gc.log -XX:+PrintGCDetails -XX:+PrintGCTimeStamps -XX:+PrintGCDateStamps -XX:+PrintClassHistogramBeforeFullGC -XX:+PrintClassHistogramAfterFullGC -XX:+PrintClassHistogramBeforeFullGC -XX:+PrintClassHistogramAfterFullGC -cp /opt/dremio/conf:/opt/dremio/jars/*:/opt/dremio/jars/ext/*:/opt/dremio/jars/3rdparty/*:/usr/lib/jvm/bellsoft-java8-amd64/lib/tools.jar com.dremio.dac.daemon.DremioDaemon`

	gcRegex, gcLogLocation, err := autodetect.ParseGCLogFromFlags(processFlags)
	if err != nil {
		t.Errorf("expected no error but we have %v", err)
	}
	expected := "/opt/dremio/log"
	if gcLogLocation != expected {
		t.Errorf("expected %v but was %v", gcLogLocation, expected)
	}
	expected = "*gc.log*"
	if gcRegex != expected {
		t.Errorf("expected %v but was %v", expected, gcRegex)
	}
}

func TestParseGCLogFromFlagsWithExtraLogFileLineDremio25Plus(t *testing.T) {
	//"Should parse the GC log location correctly", func() {
	psOut := `      1 ?        Ssl   97:20 /opt/java/openjdk/bin/java -Djava.util.logging.config.class=org.slf4j.bridge.SLF4JBridgeHandler -Djava.library.path=/opt/dremio/lib --add-opens=java.base/java.lang=ALL-UNNAMED --add-opens=java.base/java.nio=ALL-UNNAMED --add-opens=java.base/java.util=ALL-UNNAMED -XX:UseAVX=2 -Xlog:gc*::time,uptime,tags,level -Ddremio.plugins.path=/opt/dremio/plugins -Xmx2048m -XX:MaxDirectMemorySize=2048m -XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath=/var/log/dremio -Dio.netty.maxDirectMemory=0 -Dio.netty.tryReflectionSetAccessible=true -DMAPR_IMPALA_RA_THROTTLE -DMAPR_MAX_RA_STREAMS=400 -XX:+UseG1GC -Ddremio.log.path=/opt/dremio/data/logs -Xlog:gc*,classhisto*=trace:file=/opt/dremio/data/gclog/gc-%t.log:uptime,time,tags,level:filecount=1,filesize=4M -Dzookeeper=zk-hs:2181 -Dservices.coordinator.enabled=true -Dservices.coordinator.master.enabled=true -Dservices.coordinator.master.embedded-zookeeper.enabled=false -Dservices.executor.enabled=false -Dservices.conduit.port=45679 -cp /opt/dremio/conf:/opt/dremio/jars/*:/opt/dremio/jars/ext/*:/opt/dremio/jars/3rdparty/* com.dremio.dac.daemon.DremioDaemon DREMIO_PLUGINS_DIR=/opt/dremio/plugins KUBERNETES_SERVICE_PORT_HTTPS=443 KUBERNETES_SERVICE_PORT=443 DREMIO_LOG_DIR=/var/log/dremio JAVA_MAJOR_VERSION=11 DREMIO_IN_CONTAINER=1 HOSTNAME=dremio-master-0 LANGUAGE=en_US:en JAVA_HOME=/opt/java/openjdk AWS_CREDENTIAL_PROFILES_FILE=/opt/dremio/aws/credentials DREMIO_CLIENT_PORT_32010_TCP_PROTO=tcp MALLOC_ARENA_MAX=4 ZK_CS_PORT_2181_TCP_ADDR=192.168.8.30 DREMIO_GC_LOGS_ENABLED=yes DREMIO_CLASSPATH=/opt/dremio/conf:/opt/dremio/jars/*:/opt/dremio/jars/ext/*:/opt/dremio/jars/3rdparty/* DREMIO_MAX_HEAP_MEMORY_SIZE_MB=2048 DREMIO_CLIENT_PORT_9047_TCP_PORT=9047 PWD=/opt/dremio JAVA_VERSION_STRING=11.0.22 DREMIO_JAVA_SERVER_EXTRA_OPTS=-Ddremio.log.path=/opt/dremio/data/logs -Xlog:gc*,classhisto*=trace:file=/opt/dremio/data/gc-%t.log:uptime,time,tags,level:filecount=1,filesize=4M -Dzookeeper=zk-hs:2181 -Dservices.coordinator.enabled=true -Dservices.coordinator.master.enabled=true -Dservices.coordinator.master.embedded-zookeeper.enabled=false -Dservices.executor.enabled=false -Dservices.conduit.port=45679 DREMIO_MAX_DIRECT_MEMORY_SIZE_MB=2048 ZK_CS_PORT_2181_TCP_PROTO=tcp MALLOC_MMAP_MAX_=65536 DREMIO_CLIENT_PORT_32010_TCP_ADDR=192.168.8.30 DREMIO_CLIENT_PORT_31010_TCP_PROTO=tcp DREMIO_CONF_DIR=/opt/dremio/conf TZ=UTC ZK_CS_PORT=tcp://192.168.8.30:2181 DREMIO_ENV_SCRIPT=dremio-env DREMIO_CLIENT_PORT_31010_TCP_ADDR=192.168.8.30 HOME=/var/lib/dremio/dremio LANG=en_US.UTF-8 KUBERNETES_PORT_443_TCP=tcp://192.168.0.1:443 ZK_CS_PORT_2181_TCP_PORT=2181 DREMIO_CLIENT_PORT_9047_TCP_PROTO=tcp LOG_TO_CONSOLE=0 DREMIO_CLIENT_PORT=tcp://192.168.8.30:31010 DREMIO_CLIENT_SERVICE_HOST=192.168.19.122 DREMIO_HOME=/opt/dremio ZK_CS_SERVICE_PORT_CLIENT=2181 DREMIO_CLIENT_SERVICE_PORT_WEB=9047 ZK_CS_SERVICE_PORT=2181 DREMIO_CLIENT_PORT_31010_TCP=tcp://192.168.8.30:31010 DREMIO_CLIENT_SERVICE_PORT_CLIENT=31010 DREMIO_CLIENT_PORT_9047_TCP=tcp://192.168.8.30:9047 DREMIO_PID_DIR=/var/run/dremio DREMIO_CLIENT_SERVICE_PORT=31010 MALLOC_TRIM_THRESHOLD_=131072 DREMIO_GC_OPTS=-XX:+UseG1GC SHLVL=0 DREMIO_CLIENT_PORT_31010_TCP_PORT=31010 DREMIO_GC_LOG_TO_CONSOLE=yes KUBERNETES_PORT_443_TCP_PROTO=tcp is_cygwin=false MALLOC_MMAP_THRESHOLD_=131072 KUBERNETES_PORT_443_TCP_ADDR=192.168.0.1 KUBERNETES_SERVICE_HOST=192.168.0.1 LC_ALL=en_US.UTF-8 AWS_SHARED_CREDENTIALS_FILE=/opt/dremio/aws/credentials KUBERNETES_PORT=tcp://192.168.8.30:443 DREMIO_CLIENT_PORT_9047_TCP_ADDR=192.168.8.30 KUBERNETES_PORT_443_TCP_PORT=443 PATH=/opt/java/openjdk/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin MALLOC_TOP_PAD_=131072 DREMIO_JAVA_OPTS=-Djava.util.logging.config.class=org.slf4j.bridge.SLF4JBridgeHandler -Djava.library.path=/opt/dremio/lib --add-opens=java.base/java.lang=ALL-UNNAMED --add-opens=java.base/java.nio=ALL-UNNAMED --add-opens=java.base/java.util=ALL-UNNAMED -XX:UseAVX=2 -Xlog:gc*::time,uptime,tags,level -Ddremio.plugins.path=/opt/dremio/plugins -Xmx2048m -XX:MaxDirectMemorySize=2048m -XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath=/var/log/dremio -Dio.netty.maxDirectMemory=0 -Dio.netty.tryReflectionSetAccessible=true -DMAPR_IMPALA_RA_THROTTLE -DMAPR_MAX_RA_STREAMS=400 -XX:+UseG1GC -Ddremio.log.path=/opt/dremio/data/logs -Xlog:gc*,classhisto*=trace:file=/opt/dremio/data/gclog/gc-%t.log:uptime,time,tags,level:filecount=1,filesize=4M -Dzookeeper=zk-hs:2181 -Dservices.coordinator.enabled=true -Dservices.coordinator.master.enabled=true -Dservices.coordinator.master.embedded-zookeeper.enabled=false -Dservices.executor.enabled=false -Dservices.conduit.port=45679   DREMIO_CLIENT_PORT_32010_TCP=tcp://192.168.8.30:32010 ZK_CS_SERVICE_HOST=192.168.8.30 DREMIO_CLIENT_SERVICE_PORT_FLIGHT=32010 DREMIO_LOG_TO_CONSOLE=1 DREMIO_CLIENT_PORT_32010_TCP_PORT=32010 JAVA_VERSION=jdk-11.0.22+7 ZK_CS_PORT_2181_TCP=tcp://192.168.8.30:2181`
	gcRegex, gcLogLocation, err := autodetect.ParseGCLogFromFlags(psOut)
	if err != nil {
		t.Errorf("expected no error but we have %v", err)
	}
	expected := "/opt/dremio/data/gclog"
	if gcLogLocation != expected {
		t.Errorf("expected %v but was %v", expected, gcLogLocation)
	}

	expected = "*gc-*.log*"
	if gcRegex != expected {
		t.Errorf("expected %v but was %v", expected, gcRegex)
	}
}
