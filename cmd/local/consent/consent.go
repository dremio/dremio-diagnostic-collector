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

// package consent contains the logic for showing what files are collected as well the boilerplate text
package consent

import (
	"fmt"
	"os"
	"strings"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
)

type ErrorlessStringBuilder struct {
	builder strings.Builder
}

func (e *ErrorlessStringBuilder) WriteString(s string) {
	if _, err := e.builder.WriteString(s); err != nil {
		simplelog.Errorf("this should never return an error so this is truly critical: %v", err)
		os.Exit(1)
	}
}
func (e *ErrorlessStringBuilder) String() string {
	return e.builder.String()
}

func OutputConsent(conf *conf.CollectConf) string {
	builder := ErrorlessStringBuilder{}
	builder.WriteString(`
	Dremio Data Collection Consent Form

	Introduction

	Dremio ("we", "us", "our") requests your consent to collect and use certain data files from your device for the purposes of diagnostics. We take your privacy seriously and will only use these files to improve our services and troubleshoot any issues you may be experiencing. 

	Data Collection and Use

	We would like to collect the following files from your device:

`)

	if conf.CollectNodeMetrics() {
		simplelog.Info("collecting node metrics")
		builder.WriteString("\t* cpu, io and memory metrics\n")
	}

	if conf.CollectDiskUsage() {
		simplelog.Info("collecting disk usage")
		builder.WriteString("\t* df -h output\n")
	}

	if conf.CollectDremioConfiguration() {
		simplelog.Info("collecting dremio configuration")
		builder.WriteString("\t* dremio-env, dremio.conf, logback.xml, and logback-access.xml\n")
	}

	if conf.CollectSystemTablesExport() {
		simplelog.Info("collecting system tables")
		builder.WriteString(fmt.Sprintf("\t* the following system tables: %v\n", strings.Join(conf.Systemtables()[:], ",")))
	}

	if conf.CollectKVStoreReport() {
		simplelog.Info("collecting kv store report")
		builder.WriteString("\t* usage statistics on the internal Key Value Store (KVStore)\n")
		builder.WriteString("\t* list of all sources, their type and name\n")
	}

	if conf.CollectServerLogs() {
		simplelog.Info("collecting metadata server logs")
		builder.WriteString("\t* server.log including any archived versions, and server.out\n")
	}

	if conf.CollectQueriesJSON() {
		simplelog.Info("collecting queries.json")
		builder.WriteString("\t* queries.json including archived versions\n")
	}

	if conf.CollectMetaRefreshLogs() {
		simplelog.Info("collecting metadata refresh logs")
		builder.WriteString("\t* metadata_refresh.log including any archived versions\n")
	}

	if conf.CollectReflectionLogs() {
		simplelog.Info("\tcollecting reflection logs")
		builder.WriteString("\t* reflection.log including archived versions\n")
	}

	if conf.CollectAccelerationLogs() {
		simplelog.Info("collecting acceleration logs")
		builder.WriteString("\t* acceleration.log including archived versions\n")
	}

	if conf.NumberJobProfilesToCollect() > 0 {
		simplelog.Infof("collecting %v job profiles", conf.NumberJobProfilesToCollect())
		builder.WriteString(fmt.Sprintf("\t* %v job profiles randomly selected\n", conf.NumberJobProfilesToCollect()))
	}

	if conf.CollectAccessLogs() {
		simplelog.Info("collecting access logs")
		builder.WriteString("\t* access.log including archived versions\n")
	}

	if conf.CollectGCLogs() {
		simplelog.Info("collecting gc logs")
		builder.WriteString("\t* all gc.log files produced by dremio\n")
	}

	if conf.CollectWLM() {
		simplelog.Info("collecting Workload Manager information")
		builder.WriteString("\t* Work Load Manager queue names and rule names\n")
	}

	if conf.CaptureHeapDump() {
		simplelog.Info("collecting Java Heap Dump")
		builder.WriteString("\t*A Java heap dump which contains a copy of all data in the JVM heap\n")
	}

	if conf.CollectJStack() {
		simplelog.Info("collecting JStacks")
		builder.WriteString("\t* Java thread dumps collected via jstack\n")
	}

	if conf.CollectJFR() {
		simplelog.Info("collecting JFR")
		builder.WriteString("\t* Java Flight Recorder diagnostic information\n")
	}
	builder.WriteString(`

	Please note that the files we collect may contain confidential data. We will minimize the collection of confidential data wherever possible and will anonymize the data where feasible. 

We will use these files to:

1. Identify and diagnose problems with our products or services that you are using.
2. Improve our products and services.
3. Carry out other purposes that we will disclose to you at the time we collect the files.

Withdrawal of Consent

You have the right to withdraw your consent at any time. If you wish to do so, please contact us at support@dremio.com. Upon receipt of your withdrawal request, we will stop collecting new files and will delete any files we have already collected, unless we are required by law to retain them.

Changes to this Consent Form

We reserve the right to update this consent form from time to time.

Consent

By running ddc with the --accept-collection-consent flag, you acknowledge that you have read, understood, and agree to the data collection practices described in this consent form.

`)
	return builder.String()
}
