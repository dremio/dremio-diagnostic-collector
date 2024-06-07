//go:build !windows

// build !windows

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

package dirs_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/dirs"
)

func TestGetFreeSpace(t *testing.T) {
	tmpFolder := t.TempDir()
	var tests map[string]struct {
		input uint64
		want  string
	}
	b, err := dirs.GetFreeSpaceOnFileSystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bytesFree := "0.00"
	if b > 0 {
		bytesFree = fmt.Sprintf("%.2f", float64(b)/(1024.0*1024.0*1024.0))
	}
	tests = map[string]struct {
		input uint64
		want  string
	}{
		"free space will fail": {input: 100000000, want: fmt.Sprintf("there are only %v GB free on %v and 100000000 GB is the minimum", bytesFree, tmpFolder)},
		"no requirement":       {input: 0, want: ""},
		"1 gb free":            {input: 1, want: ""},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			input := tc.input
			want := tc.want
			gotRaw := dirs.CheckFreeSpace(tmpFolder, input)
			var got string
			if gotRaw != nil {
				got = gotRaw.Error()
			}
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("expected:\n%q\ngot:\n%q", want, got)
			}
		})
	}
}
