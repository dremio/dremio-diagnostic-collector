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
	"os"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf/autodetect"
)

func TestIsAWSEFromText(t *testing.T) {
	//should return false when AwsDremioDaemon or DremioDaemon is not found in the text
	jpsText := "12345 JavaProcess\n67890 AnotherProcess"
	isAWSE, err := autodetect.IsAWSEFromJPSOutput(jpsText)
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	if isAWSE {
		t.Error("expected to not be AWSE but was detected as AWSE")
	}

	//should return true when AwsDremioDaemon is found in the text
	jpsText = "12345 AwsDremioDaemon\n67890 AnotherProcess"
	isAWSE, err = autodetect.IsAWSEFromJPSOutput(jpsText)
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	if !isAWSE {
		t.Error("expected to be AWSE but was detected as not AWSE")
	}

	// AWSE can show two DremioDaemon processes but one is the preview engine, this gives us indication of AWSE
	//should return true when DremioDaemon and preview is found in the text
	jpsText = `27059 Jps -Dapplication.home=/usr/lib/jvm/java-1.8.0-openjdk-1.8.0.362.b08-1.amzn2.0.1.x86_64 -Xms8m
31577 DremioDaemon -Djava.util.logging.config.class=org.slf4j.bridge.SLF4JBridgeHandler -Djava.library.path=/opt/dremio/lib -XX:+PrintGCDetails -XX:+PrintGCDateStamps -Xloggc:/var/log/dremio/preview/server.gc -Ddremio.log.path=/var/log/dremio/preview -Ddremio.plugins.path=/opt/dremio/plugins -Xmx2048m -XX:MaxDirectMemorySize=2048m -XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath=/var/log/dremio/preview -Dio.netty.maxDirectMemory=0 -Dio.netty.tryReflectionSetAccessible=true -DMAPR_IMPALA_RA_THROTTLE -DMAPR_MAX_RA_STREAMS=400 -Xloggc:/var/log/dremio/server-%t.gc -XX:+UseG1GC -XX:+UseGCLogFileRotation -XX:NumberOfGCLogFiles=2000 -XX:GCLogFileSize=50M -XX:+StartAttachListener -XX:+PrintClassHistogramBeforeFullGC -XX:+PrintClassHistogramAfterFullGC
28091 DremioDaemon -Djava.util.logging.config.class=org.slf4j.bridge.SLF4JBridgeHandler -Djava.library.path=/opt/dremio/lib -XX:+PrintGCDetails -XX:+PrintGCDateStamps -Xloggc:/var/log/dremio/server-%t.gc -Ddremio.log.path=/var/log/dremio -Ddremio.plugins.path=/opt/dremio/plugins -Xmx5491m -XX:MaxDirectMemorySize=2048m -XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath=/var/log/dremio -Dio.netty.maxDirectMemory=0 -Dio.netty.tryReflectionSetAccessible=true -DMAPR_IMPALA_RA_THROTTLE -DMAPR_MAX_RA_STREAMS=400 -Xloggc:/var/log/dremio/server-%t.gc -XX:+UseG1GC -XX:+UseGCLogFileRotation -XX:NumberOfGCLogFiles=2000 -XX:GCLogFileSize=50M -XX:+StartAttachListener -XX:+AlwaysPreTouch -Xms5g -Xmx5g -XX:MaxDirectMemorySize=5g -Xloggc:/opt/dremio/data/gc.log -XX:NumberOfGCLogFiles=20 -XX:GCLogFileSize=100m -XX:+PrintGCDetails -XX:+PrintGCTimeStamps -XX:+PrintGCDateStamps -XX:+PrintAdaptiveSizePolicy -XX:+UseGCLogFileRotation -XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath=/opt/dremio/data -XX:ErrorFile=/opt/dremio/data/hs_err_pid%p.log -XX:G1
`
	isAWSE, err = autodetect.IsAWSEFromJPSOutput(jpsText)
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	if !isAWSE {
		t.Error("expected to be AWSE but was detected as not AWSE")
	}
}

func TestIsAWSEExecutorUsingDir(t *testing.T) {
	var (
		testDir  string
		nodeName string
		err      error
	)

	beforeEach := func() {
		testDir, err = os.MkdirTemp("", "example")
		if err != nil {
			t.Errorf("unexpected error %v", err)
		}
		nodeName = "TestNode"

		subDir := testDir + "/SubDirectory"
		err = os.Mkdir(subDir, 0755)
		if err != nil {
			t.Errorf("unexpected error %v", err)
		}
	}

	afterEach := func() {
		os.RemoveAll(testDir)
	}

	//should return true when node name is found
	beforeEach()

	nodeDir := testDir + "/" + nodeName
	err = os.Mkdir(nodeDir, 0755)
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}

	isAWSE, err := autodetect.IsAWSEExecutorUsingDir(testDir, nodeName)
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	if !isAWSE {
		t.Error("expected to be AWSE but was detected as not AWSE")
	}
	afterEach()
	// end scenario

	//should return false when node name is not found
	beforeEach()
	isAWSE, err = autodetect.IsAWSEExecutorUsingDir(testDir, nodeName)
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	if isAWSE {
		t.Error("expected to not be AWSE but was detected as AWSE")
	}
	afterEach()
	// end scenario
}
