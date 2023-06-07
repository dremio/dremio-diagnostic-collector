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
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/pflag"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf"
)

var _ = Describe("Conf", func() {
	var (
		tmpDir      string
		cfgFilePath string
		overrides   map[string]*pflag.Flag
		err         error
		cfg         *conf.CollectConf
	)

	BeforeEach(func() {
		tmpDir, err = os.MkdirTemp("", "testdataabdc")
		Expect(err).NotTo(HaveOccurred())
		cfgFilePath = fmt.Sprintf("%s/%s", tmpDir, "ddc.yaml")

		// Create a sample configuration file.
		cfgContent := `
accept-collection-consent: true
collect-acceleration-log: true
collect-access-log: true
dremio-gclogs-dir: "/path/to/gclogs"
dremio-log-dir: "/path/to/dremio/logs"
node-name: "node1"
dremio-conf-dir: "/path/to/dremio/conf"
number-threads: 4
dremio-endpoint: "http://localhost:9047"
dremio-username: "admin"
dremio-pat-token: "your_personal_access_token"
dremio-rocksdb-dir: "/path/to/dremio/rocksdb"
collect-dremio-configuration: true
number-job-profiles: 10
capture-heap-dump: true
collect-metrics: true
collect-disk-usage: true
tmp-output-dir: "/path/to/tmp"
dremio-logs-num-days: 7
dremio-queries-json-num-days: 7
dremio-gc-file-pattern: "*.log"
collect-queries-json: true
collect-server-logs: true
collect-meta-refresh-log: true
collect-reflection-log: true
collect-gc-logs: true
collect-jfr: true
dremio-jfr-time-seconds: 60
collect-jstack: true
dremio-jstack-time-seconds: 60
dremio-jstack-freq-seconds: 10
collect-wlm: true
collect-system-tables-export: true
collect-kvstore-report: true
`

		// Write the sample configuration to a file.
		err := os.WriteFile(cfgFilePath, []byte(cfgContent), 0600)
		Expect(err).NotTo(HaveOccurred())

		overrides = map[string]*pflag.Flag{}
	})

	AfterEach(func() {
		// Remove the configuration file after each test.
		err := os.Remove(cfgFilePath)
		Expect(err).NotTo(HaveOccurred())
		// Remove the temporary directory after each test.
		err = os.RemoveAll(tmpDir)
		Expect(err).NotTo(HaveOccurred())
	})

	Context("with a valid configuration file", func() {
		It("should parse the configuration correctly", func() {
			cfg, err = conf.ReadConf(overrides, tmpDir)
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg).NotTo(BeNil())

			Expect(cfg.AcceptCollectionConsent()).To(BeTrue())
			//	Expect(cfg.CaptureHeapDump()).To(BeTrue()) no valid PID for dremio so should be false
			Expect(cfg.CollectAccelerationLogs()).To(BeTrue())
			Expect(cfg.CollectAccessLogs()).To(BeTrue())
			Expect(cfg.CollectDiskUsage()).To(BeTrue())
			Expect(cfg.CollectDremioConfiguration()).To(BeTrue())
			//	Expect(cfg.GcLogsDir()).To(Equal("/path/to/gclogs")) autodetect ends up overriding this
			//Expect(cfg.CollectJFR()).To(BeFalse())    // no valid PID for dremio so should be false
			//Expect(cfg.CollectJStack()).To(BeFalse()) // no valid PID for dremio so should be false
			Expect(cfg.CollectKVStoreReport()).To(BeTrue())
			Expect(cfg.CollectMetaRefreshLogs()).To(BeTrue())
			Expect(cfg.CollectNodeMetrics()).To(BeTrue())
			Expect(cfg.CollectQueriesJSON()).To(BeTrue())
			Expect(cfg.CollectReflectionLogs()).To(BeTrue())
			Expect(cfg.CollectServerLogs()).To(BeTrue())
			Expect(cfg.CollectSystemTablesExport()).To(BeTrue())
			Expect(cfg.CollectWLM()).To(BeTrue())
			Expect(cfg.DremioConfDir()).To(Equal("/path/to/dremio/conf"))
		})
	})
})
