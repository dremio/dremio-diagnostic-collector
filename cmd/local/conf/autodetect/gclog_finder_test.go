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

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf/autodetect"
)

func TestParseGCLogFromFlags_WhenJVMFlagsAreGiven(t *testing.T) {
	//Should parse the GC log location correctly"
	processFlags := `1: 
jvm_args: -Djava.util.logging.config.class=org.slf4j.bridge.SLF4JBridgeHandler -Djava.library.path=/opt/dremio/lib -XX:+PrintGCDetails -XX:+PrintGCDateStamps -Ddremio.plugins.path=/opt/dremio/plugins -Xmx4096m -XX:MaxDirectMemorySize=2048m -XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath=/var/log/dremio -Dio.netty.maxDirectMemory=0 -Dio.netty.tryReflectionSetAccessible=true -DMAPR_IMPALA_RA_THROTTLE -DMAPR_MAX_RA_STREAMS=400 -XX:+UseG1GC -Ddremio.log.path=/opt/dremio/data/log -Xloggc:/opt/dremio/data/log/gc.log -XX:+UseGCLogFileRotation -XX:NumberOfGCLogFiles=5 -XX:GCLogFileSize=4000k -XX:+PrintGCDetails -XX:+PrintGCTimeStamps -XX:+PrintGCDateStamps -XX:+PrintClassHistogramBeforeFullGC -XX:+PrintClassHistogramAfterFullGC -XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath=/opt/dremio/data -XX:+UseG1GC -XX:G1HeapRegionSize=32M -XX:MaxGCPauseMillis=500 -XX:InitiatingHeapOccupancyPercent=25 -XX:+PrintAdaptiveSizePolicy -XX:+PrintReferenceGC -XX:ErrorFile=/opt/dremio/data/hs_err_pid%p.log -Dzookeeper=zk-hs:2181 -Dservices.coordinator.enabled=false -Dservices.coordinator.master.enabled=false -Dservices.coordinator.master.embedded-zookeeper.enabled=false -Dservices.executor.enabled=true -Dservices.conduit.port=45679 -Dservices.node-tag=default -XX:+PrintClassHistogramBeforeFullGC -XX:+PrintClassHistogramAfterFullGC 
java_command: com.dremio.dac.daemon.DremioDaemon
java_class_path (initial): /opt/dremio/conf:/opt/dremio/jars/dremio-services-coordinator-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-hive-function-registry-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-ce-sabot-serializer-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-hive2-plugin-launcher-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-ee-services-credentials-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-ee-services-accesscontrol-common-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-ce-sabot-scheduler-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-dac-common-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-services-usersessions-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-ee-services-sysflight-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-protocol-20.0.0-202201050826310141-8cc7162b-proto.jar:/opt/dremio/jars/dremio-services-telemetry-impl-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-services-jobtelemetry-client-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-hive3-plugin-launcher-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-ce-services-cachemanager-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-ee-dac-tools-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-services-base-rpc-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-services-datastore-20.0.0-202201050826310141-8cc7162b-proto.jar:/opt/dremio/jars/dremio-sabot-logical-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-services-transientstore-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-services-resourcescheduler-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-dac-daemon-20.0.0-202201050826310141-8cc7162b.jar:/opt/dremio/jars/dremio-ee-services-namespace-20.0.0-202201050826310141-8cc7162b-tests.jar:/opt/dremio/j
Launcher Type: SUN_STANDARD`
	gcLogLocation, err := autodetect.ParseGCLogFromFlags(processFlags)
	if err != nil {
		t.Errorf("expected no error but we have %v", err)
	}
	expected := "/opt/dremio/data/log"
	if gcLogLocation != expected {
		t.Errorf("expected %v but was %v", gcLogLocation, expected)
	}
}

func TestParseGCLogFromFlagsWithExtraLogFileLine(t *testing.T) {
	//"Should parse the GC log location correctly", func() {
	processFlags := `jvm_args: -Djava.util.logging.config.class=org.slf4j.bridge.SLF4JBridgeHandler -Djava.library.path=/opt/dremio/lib -XX:+PrintGCDetails -XX:+PrintGCDateStamps -Xloggc:/var/log/dremio/server.gc -Ddremio.log.path=/var/log/dremio -Ddremio.plugins.path=/opt/dremio/plugins -Xmx6144m -XX:MaxDirectMemorySize=2048m -XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath=/var/log/dremio -Dio.netty.maxDirectMemory=0 -Dio.netty.tryReflectionSetAccessible=true -DMAPR_IMPALA_RA_THROTTLE -DMAPR_MAX_RA_STREAMS=400 -XX:+UseG1GC -Ddremio.log.path=/opt/dremio/data/log -Xloggc:/opt/dremio/data/log/gc.log -XX:NumberOfGCLogFiles=5 -XX:GCLogFileSize=4000k -XX:+PrintGCDetails -XX:+PrintGCTimeStamps -XX:+PrintGCDateStamps -XX:+PrintClassHistogramBeforeFullGC -XX:+PrintClassHistogramAfterFullGC -XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath=/opt/dremio/data -XX:+UseG1GC -XX:G1HeapRegionSize=32M -XX:MaxGCPauseMillis=500 -XX:InitiatingHeapOccupancyPercent=25 -XX:+PrintAdaptiveSizePolicy -XX:+PrintReferenceGC -XX:ErrorFile=/opt/dremio/data/hs_err_pid%p.log -Dzookeeper=zk-hs:2181 -Dservices.coordinator.enabled=true -Dservices.coordinator.master.enabled=true -Dservices.coordinator.master.embedded-zookeeper.enabled=false -Dservices.executor.enabled=false -Dservices.conduit.port=45679 -XX:+PrintClassHistogramBeforeFullGC -XX:+PrintClassHistogramAfterFullGC`
	gcLogLocation, err := autodetect.ParseGCLogFromFlags(processFlags)
	if err != nil {
		t.Errorf("expected no error but we have %v", err)
	}
	expected := "/opt/dremio/data/log"
	if gcLogLocation != expected {
		t.Errorf("expected %v but was %v", gcLogLocation, expected)
	}
}

func TestParseGCLogFromFlagsWithExtraLogFileLineDremio25Plus(t *testing.T) {
	//"Should parse the GC log location correctly", func() {
	processFlags := `jvm_args: -Djava.util.logging.config.class=org.slf4j.bridge.SLF4JBridgeHandler -Djava.library.path=/opt/dremio/lib -XX:+PrintGCDetails -XX:+PrintGCDateStamps -Xlog:gc*,classhisto*=trace:file=/opt/dremio/data/gclog/%%t-gc.log:uptime,time,tags,level:filecount=1,filesize=4M -Ddremio.log.path=/var/log/dremio -Ddremio.plugins.path=/opt/dremio/plugins -Xmx6144m -XX:MaxDirectMemorySize=2048m -XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath=/var/log/dremio -Dio.netty.maxDirectMemory=0 -Dio.netty.tryReflectionSetAccessible=true -DMAPR_IMPALA_RA_THROTTLE -DMAPR_MAX_RA_STREAMS=400 -XX:+UseG1GC -Ddremio.log.path=/opt/dremio/data/log -Xloggc:/opt/dremio/data/log/gc.log -XX:NumberOfGCLogFiles=5 -XX:GCLogFileSize=4000k -XX:+PrintGCDetails -XX:+PrintGCTimeStamps -XX:+PrintGCDateStamps -XX:+PrintClassHistogramBeforeFullGC -XX:+PrintClassHistogramAfterFullGC -XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath=/opt/dremio/data -XX:+UseG1GC -XX:G1HeapRegionSize=32M -XX:MaxGCPauseMillis=500 -XX:InitiatingHeapOccupancyPercent=25 -XX:+PrintAdaptiveSizePolicy -XX:+PrintReferenceGC -XX:ErrorFile=/opt/dremio/data/hs_err_pid%p.log -Dzookeeper=zk-hs:2181 -Dservices.coordinator.enabled=true -Dservices.coordinator.master.enabled=true -Dservices.coordinator.master.embedded-zookeeper.enabled=false -Dservices.executor.enabled=false -Dservices.conduit.port=45679 -XX:+PrintClassHistogramBeforeFullGC -XX:+PrintClassHistogramAfterFullGC
`
	gcLogLocation, err := autodetect.ParseGCLogFromFlags(processFlags)
	if err != nil {
		t.Errorf("expected no error but we have %v", err)
	}
	expected := "/opt/dremio/data/gclog"
	if gcLogLocation != expected {
		t.Errorf("expected %v but was %v", expected, gcLogLocation)
	}
}
