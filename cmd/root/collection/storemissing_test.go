// Copyright 2023 Dremio Corporation
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

package collection

import "testing"

func TestStoreMissingName(t *testing.T) {
	tests := []struct {
		name   string
		line   string
		wantCF string
		wantOK bool
	}{
		{"exact", "error: 'wlmqueue' column family not found", "wlmqueue", true},
		{"padded with CR", "  error: 'rulesmanager' column family not found\r", "rulesmanager", true},
		{"other error", "error: open db: IO error", "", false},
		{"trailing text", "error: 'x' column family not found extra", "", false},
		{"embedded in json", `{"msg":"error: 'x' column family not found"}`, "", false},
		{"empty", "", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cf, ok := storeMissingName(tc.line)
			if cf != tc.wantCF || ok != tc.wantOK {
				t.Errorf("storeMissingName(%q) = (%q, %v), want (%q, %v)", tc.line, cf, ok, tc.wantCF, tc.wantOK)
			}
		})
	}
}
