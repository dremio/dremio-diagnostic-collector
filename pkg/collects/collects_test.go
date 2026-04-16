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

package collects_test

import (
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/collects"
)

func TestDiagnosisCollectionConstant(t *testing.T) {
	if got := collects.DiagnosisCollection; got != "diagnosis" {
		t.Errorf("expected DiagnosisCollection to be 'diagnosis', got %q", got)
	}
}

func TestStandardCollectionConstant(t *testing.T) {
	if got := collects.StandardCollection; got != "standard" {
		t.Errorf("expected StandardCollection to be 'standard', got %q", got)
	}
}
