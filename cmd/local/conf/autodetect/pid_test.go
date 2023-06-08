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
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/conf/autodetect"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("DremioPID", func() {
	Context("GetDremioPIDFromText", func() {
		It("should return an error when no matching process name is found", func() {
			jpsOutput := "12345 JavaProcess\n67890 AnotherProcess"
			pid, err := autodetect.GetDremioPIDFromText(jpsOutput)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("found no matching process named DremioDaemon in text 12345 JavaProcess, 67890 AnotherProcess therefore cannot get the pid"))
			Expect(pid).To(Equal(-1))
		})

		It("should return PID when a matching process name is found", func() {
			jpsOutput := "12345 DremioDaemon\n67890 AnotherProcess"
			pid, err := autodetect.GetDremioPIDFromText(jpsOutput)
			Expect(err).NotTo(HaveOccurred())
			Expect(pid).To(Equal(12345))
		})
	})

})
