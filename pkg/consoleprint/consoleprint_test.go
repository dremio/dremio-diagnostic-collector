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

package consoleprint_test

import (
	"strings"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/pkg/consoleprint"
	"github.com/dremio/dremio-diagnostic-collector/pkg/output"
)

func TestClearsScreen(t *testing.T) {
	out, err := output.CaptureOutput(func() {
		consoleprint.PrintState()
	})
	if err != nil {
		t.Fatal(err)
	}

	// relying on test mode detection
	if !strings.Contains(out, "CLEAR SCREEN") {
		t.Errorf("output %v did not contain 'CLEAR SCREEN'", out)
	}
}
