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

func TestGetDremioPIDFromTextHasNoText(t *testing.T) {
	psOutput := ""
	pid, err := autodetect.GetDremioPIDFromText(psOutput)
	if err == nil || err.Error() != "no pid for dremio found in text ''" {
		t.Errorf("Unexpected error: %v", err)
	}
	if pid != -1 {
		t.Errorf("Unexpected value for pid. Got %v, expected -1", pid)
	}
}

func TestGetDremioPIDFromText(t *testing.T) {
	psOutput := `dremio    3139  6.5 20.7 8311440 3340972 ?     Ssl  08:04   2:21 /usr/lib/jvm/java-1.8.0-openjdk/bin/java -Djava.util.logging.config.class=org.slf4j.bridge.SLF4JBridgeHandler -Djava.library.path=/opt/dremio/lib -XX:+PrintGCDetails -XX:+PrintGCDateStamps -Xloggc:/var/log/dremio/server-%t.gc -Ddremio.log.path=/var/log/dremio -Ddremio.plugins.path=/opt/dremio/plugins -Xmx5491m -XX:MaxDirectMemorySize=2048m -XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath=/var/log/dremio -Dio.netty.maxDirectMemory=0 -Dio.netty.tryReflectionSetAccessible=true -DMAPR_IMPALA_RA_THROTTLE -DMAPR_MAX_RA_STREAMS=400 -Xloggc:/var/log/dremio/server-%t.gc -XX:+UseG1GC -XX:+UseGCLogFileRotation -XX:NumberOfGCLogFiles=2000 -XX:GCLogFileSize=50M -XX:+StartAttachListener -XX:+PrintClassHistogramBeforeFullGC -XX:+PrintClassHistogramAfterFullGC -cp /opt/dremio/conf:/opt/dremio/jars/*:/opt/dremio/jars/ext/*:/opt/dremio/jars/3rdparty/*:/var/dremio_efs/thirdparty/*:/usr/lib/jvm/java-1.8.0-openjdk/lib/tools.jar com.dremio.dac.daemon.AwsDremioDaemon`
	pid, err := autodetect.GetDremioPIDFromText(psOutput)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if pid != 3139 {
		t.Errorf("Unexpected value for pid. Got %v, expected 3139", pid)
	}
}

func TestGetDremioPIDFromTextWithTrailingSpace(t *testing.T) {
	psOutput := `dremio    3139  6.5 20.7 8311440 3340972 ?     Ssl  08:04   2:21 /usr/lib/jvm/java-1.8.0-openjdk/bin/java -Djava.util.logging.config.class=org.slf4j.bridge.SLF4JBridgeHandler -Djava.library.path=/opt/dremio/lib -XX:+PrintGCDetails -XX:+PrintGCDateStamps -Xloggc:/var/log/dremio/server-%t.gc -Ddremio.log.path=/var/log/dremio -Ddremio.plugins.path=/opt/dremio/plugins -Xmx5491m -XX:MaxDirectMemorySize=2048m -XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath=/var/log/dremio -Dio.netty.maxDirectMemory=0 -Dio.netty.tryReflectionSetAccessible=true -DMAPR_IMPALA_RA_THROTTLE -DMAPR_MAX_RA_STREAMS=400 -Xloggc:/var/log/dremio/server-%t.gc -XX:+UseG1GC -XX:+UseGCLogFileRotation -XX:NumberOfGCLogFiles=2000 -XX:GCLogFileSize=50M -XX:+StartAttachListener -XX:+PrintClassHistogramBeforeFullGC -XX:+PrintClassHistogramAfterFullGC -cp /opt/dremio/conf:/opt/dremio/jars/*:/opt/dremio/jars/ext/*:/opt/dremio/jars/3rdparty/*:/var/dremio_efs/thirdparty/*:/usr/lib/jvm/java-1.8.0-openjdk/lib/tools.jar com.dremio.dac.daemon.AwsDremioDaemon
`
	pid, err := autodetect.GetDremioPIDFromText(psOutput)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if pid != 3139 {
		t.Errorf("Unexpected value for pid. Got %v, expected 3139", pid)
	}
}

func TestGetDremioPIDFromTextMatchesTwoRecords(t *testing.T) {
	psOutput := `dremio    3139  6.5 20.7 8311440 3340972 ?     Ssl  08:04   2:21 /usr/lib/jvm/java-1.8.0-openjdk/bin/java -Djava.util.logging.config.class=org.slf4j.bridge.SLF4JBridgeHandler -Djava.library.path=/opt/dremio/lib -XX:+PrintGCDetails -XX:+PrintGCDateStamps -Xloggc:/var/log/dremio/server-%t.gc -Ddremio.log.path=/var/log/dremio -Ddremio.plugins.path=/opt/dremio/plugins -Xmx5491m -XX:MaxDirectMemorySize=2048m -XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath=/var/log/dremio -Dio.netty.maxDirectMemory=0 -Dio.netty.tryReflectionSetAccessible=true -DMAPR_IMPALA_RA_THROTTLE -DMAPR_MAX_RA_STREAMS=400 -Xloggc:/var/log/dremio/server-%t.gc -XX:+UseG1GC -XX:+UseGCLogFileRotation -XX:NumberOfGCLogFiles=2000 -XX:GCLogFileSize=50M -XX:+StartAttachListener -XX:+PrintClassHistogramBeforeFullGC -XX:+PrintClassHistogramAfterFullGC -cp /opt/dremio/conf:/opt/dremio/jars/*:/opt/dremio/jars/ext/*:/opt/dremio/jars/3rdparty/*:/var/dremio_efs/thirdparty/*:/usr/lib/jvm/java-1.8.0-openjdk/lib/tools.jar com.dremio.dac.daemon.AwsDremioDaemon
dremio    3139  6.5 20.7 8311440 3340972 ?     Ssl  08:04   2:21 /usr/lib/jvm/java-1.8.0-openjdk/bin/java -Djava.util.logging.config.class=org.slf4j.bridge.SLF4JBridgeHandler -Djava.library.path=/opt/dremio/lib -XX:+PrintGCDetails -XX:+PrintGCDateStamps -Xloggc:/var/log/dremio/server-%t.gc -Ddremio.log.path=/var/log/dremio -Ddremio.plugins.path=/opt/dremio/plugins -Xmx5491m -XX:MaxDirectMemorySize=2048m -XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath=/var/log/dremio -Dio.netty.maxDirectMemory=0 -Dio.netty.tryReflectionSetAccessible=true -DMAPR_IMPALA_RA_THROTTLE -DMAPR_MAX_RA_STREAMS=400 -Xloggc:/var/log/dremio/server-%t.gc -XX:+UseG1GC -XX:+UseGCLogFileRotation -XX:NumberOfGCLogFiles=2000 -XX:GCLogFileSize=50M -XX:+StartAttachListener -XX:+PrintClassHistogramBeforeFullGC -XX:+PrintClassHistogramAfterFullGC -cp /opt/dremio/conf:/opt/dremio/jars/*:/opt/dremio/jars/ext/*:/opt/dremio/jars/3rdparty/*:/var/dremio_efs/thirdparty/*:/usr/lib/jvm/java-1.8.0-openjdk/lib/tools.jar com.dremio.dac.daemon.AwsDremioDaemon
`
	pid, err := autodetect.GetDremioPIDFromText(psOutput)
	if err == nil {
		t.Error("expected error")
	}
	if pid != -1 {
		t.Errorf("Unexpected value for pid. Got %v, expected -1", pid)
	}
}

func TestGetK8sPID(t *testing.T) {
	psOutput := `dremio         1  0.3  2.1 5169980 2891424 ?     Ssl  Aug26  96:42 /opt/java/openjdk/bin/java -Djava.util.logging.config.class=org.slf4j.bridge.SLF4JBridgeHandler -Djava.library.path=/opt/dremio/lib --add-opens=java.base/java.lang=ALL-UNNAMED --add-opens=java.base/java.nio=ALL-UNNAMED --add-opens=java.base/java.util=ALL-UNNAMED -XX:UseAVX=2 -Xlog:gc*::time,uptime,tags,level -Ddremio.plugins.path=/opt/dremio/plugins -Xmx2048m -XX:MaxDirectMemorySize=2048m -XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath=/var/log/dremio -Dio.netty.maxDirectMemory=0 -Dio.netty.tryReflectionSetAccessible=true -DMAPR_IMPALA_RA_THROTTLE -DMAPR_MAX_RA_STREAMS=400 -XX:+UseG1GC -Ddremio.log.path=/opt/dremio/data/logs -Xlog:gc*,classhisto*=trace:file=/opt/dremio/data/gc-%t.log:uptime,time,tags,level:filecount=1,filesize=4M -Dzookeeper=zk-hs:2181 -Dservices.coordinator.enabled=true -Dservices.coordinator.master.enabled=true -Dservices.coordinator.master.embedded-zookeeper.enabled=false -Dservices.executor.enabled=false -Dservices.conduit.port=45679 -cp /opt/dremio/conf:/opt/dremio/jars/*:/opt/dremio/jars/ext/*:/opt/dremio/jars/3rdparty/* com.dremio.dac.daemon.DremioDaemon`
	pid, err := autodetect.GetDremioPIDFromText(psOutput)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if pid != 1 {
		t.Errorf("Unexpected value for pid. Got %v, expected 1", pid)
	}
}
