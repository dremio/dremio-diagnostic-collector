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

func TestGetDremioPIDFromText(t *testing.T) {
	jpsOutput1 := "12345 JavaProcess\n67890 AnotherProcess"
	pid1, err1 := autodetect.GetDremioPIDFromText(jpsOutput1)
	if err1 == nil || err1.Error() != "found no matching process named DremioDaemon in text 12345 JavaProcess, 67890 AnotherProcess therefore cannot get the pid" {
		t.Errorf("Unexpected error: %v", err1)
	}
	if pid1 != -1 {
		t.Errorf("Unexpected value for pid. Got %v, expected -1", pid1)
	}

	jpsOutput2 := "12345 DremioDaemon\n67890 AnotherProcess"
	pid2, err2 := autodetect.GetDremioPIDFromText(jpsOutput2)
	if err2 != nil {
		t.Errorf("Unexpected error: %v", err2)
	}
	if pid2 != 12345 {
		t.Errorf("Unexpected value for pid. Got %v, expected 12345", pid2)
	}

	jpsOutput3 := "1 DremioDaemon -Djava.util.logging.config.class=org.slf4j.bridge.SLF4JBridgeHandler -Djava.library.path=/opt/dremio/lib -XX:+PrintGCDetails -XX:+PrintGCDateStamps -Ddremio.plugins.path=/opt/dremio/plugins -Xmx2048m -XX:MaxDirectMemorySize=2048m -XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath=/var/log/dremio -Dio.netty.maxDirectMemory=0 -Dio.netty.tryReflectionSetAccessible=true -DMAPR_IMPALA_RA_THROTTLE -DMAPR_MAX_RA_STREAMS=400 -XX:+UseG1GC -Ddremio.log.path=/opt/dremio/data/logs -Xloggc:/opt/dremio/data/logs/gc.log -XX:+PrintGCDetails -XX:+PrintGCDateStamps -XX:+PrintTenuringDistribution -XX:+PrintGCCause -XX:+UseGCLogFileRotation -XX:NumberOfGCLogFiles=10 -XX:GCLogFileSize=5M -Dzookeeper=zk-hs:2181 -Dservices.coordinator.enabled=true -Dservices.coordinator.master.enabled=true -Dservices.coordinator.master.embedded-zookeeper.enabled=false -Dservices.executor.enabled=false -Dservices.connduit.port=45679 -Ddremio.admin-only-mode=false -XX:+PrintClassHistogramBeforeFullGC -XX:+PrintClassHistogramAfterFullGC\214 Jps -Dapplication.home=/opt/java/openjdk -Xms8m"
	pid3, err3 := autodetect.GetDremioPIDFromText(jpsOutput3)
	if err3 != nil {
		t.Errorf("Unexpected error: %v", err3)
	}
	if pid3 != 1 {
		t.Errorf("Unexpected value for pid. Got %v, expected 1", pid3)
	}
}
