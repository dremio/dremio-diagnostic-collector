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

package strutils

import (
	"strings"
	"unicode/utf8"
)

func LimitString(s string, maxLength int) string {
	max := 0
	if maxLength > 0 {
		max = maxLength
	}
	// Check if the string is already within the desired length
	if utf8.RuneCountInString(s) <= max {
		return s
	}

	// Truncate the string to the desired length
	runes := []rune(s)
	truncatedRunes := runes[:max]
	return string(truncatedRunes)
}

func GetLastLine(s string) string {
	index := strings.LastIndex(s, "\n")
	if index == -1 {
		return s // No newline character, return the whole string
	}
	return s[index+1:] // Return the substring after the last newline character
}
