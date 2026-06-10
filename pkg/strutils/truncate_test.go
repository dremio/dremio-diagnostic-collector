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

package strutils_test

import (
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/strutils"
)

func TestLimitStringTooLong(t *testing.T) {
	a := strutils.GetEndOfString("12345", 1)
	if a != "5" {
		t.Errorf("expected '1' but got '%v'", a)
	}
}

func TestLimitStringWhenStringIsShorterThanLimit(t *testing.T) {
	a := strutils.GetEndOfString("12345", 100)
	if a != "12345" {
		t.Errorf("expected '12345' but got '%v'", a)
	}
}

func TestLimitStringWhenStringIsEmpty(t *testing.T) {
	a := strutils.GetEndOfString("", 100)
	if a != "" {
		t.Errorf("expected '' but got '%v'", a)
	}
}

func TestLimitStringWhenLimitAndStringAreDefault(t *testing.T) {
	a := strutils.GetEndOfString("", 0)
	if a != "" {
		t.Errorf("expected '' but got '%v'", a)
	}
}

func TestLimitStringWhenLimitIsDefault(t *testing.T) {
	a := strutils.GetEndOfString("12345", 0)
	if a != "" {
		t.Errorf("expected '' but got '%v'", a)
	}
}

func TestLimitStringWhenLimitINegative(t *testing.T) {
	a := strutils.GetEndOfString("12345", -1)
	if a != "" {
		t.Errorf("expected '' but got '%v'", a)
	}
}

func TestGetStartOfString(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		maxLength int
		expected  string
	}{
		{name: "truncates keeping head", input: "abcdefghij", maxLength: 5, expected: "abcde...[truncated]"},
		{name: "within limit unchanged", input: "abc", maxLength: 10, expected: "abc"},
		{name: "exact limit unchanged", input: "abcde", maxLength: 5, expected: "abcde"},
		{name: "empty string", input: "", maxLength: 10, expected: ""},
		{name: "zero limit", input: "abcde", maxLength: 0, expected: "...[truncated]"},
		{name: "negative limit", input: "abcde", maxLength: -1, expected: "...[truncated]"},
		{name: "multibyte utf8", input: "日本語テスト", maxLength: 3, expected: "日本語...[truncated]"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := strutils.GetStartOfString(tc.input, tc.maxLength)
			if got != tc.expected {
				t.Errorf("GetStartOfString(%q, %d) = %q, want %q", tc.input, tc.maxLength, got, tc.expected)
			}
		})
	}
}
