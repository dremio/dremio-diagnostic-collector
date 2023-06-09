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
}
