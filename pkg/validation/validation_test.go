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

package validation_test

import (
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/collects"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/validation"
)

func TestValidateCollectMode_Diagnosis(t *testing.T) {
	if err := validation.ValidateCollectMode(collects.DiagnosisCollection); err != nil {
		t.Errorf("expected diagnosis mode to be valid, got error: %v", err)
	}
}

func TestValidateCollectMode_Standard(t *testing.T) {
	if err := validation.ValidateCollectMode(collects.StandardCollection); err != nil {
		t.Errorf("expected standard mode to be valid, got error: %v", err)
	}
}

func TestValidateCollectMode_RejectsInvalid(t *testing.T) {
	invalidModes := []collects.CollectionMode{"light", "health-check", "waf", "standard+jstack", "invalid", ""}
	for _, mode := range invalidModes {
		if err := validation.ValidateCollectMode(mode); err == nil {
			t.Errorf("expected mode %q to be rejected, but it was accepted", mode)
		}
	}
}
