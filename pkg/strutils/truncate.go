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

func GetEndOfString(str string, maxLength int) string {
	maxLength = max(maxLength, 0)
	// Check if the string is already within the desired length
	if utf8.RuneCountInString(str) <= maxLength {
		return str
	}

	// get the end of the string up to desired length
	runes := []rune(str)
	truncatedRunes := runes[len(runes)-maxLength:]
	return string(truncatedRunes)
}

func TruncateString(str string, maxLength int) string {
	maxLength = max(maxLength, 0)
	// Check if the string is already within the desired length
	if utf8.RuneCountInString(str) <= maxLength {
		return str
	}
	return str[:maxLength]
}

func GetLastLine(str string) string {
	index := strings.LastIndex(str, "\n")
	if index == -1 {
		return str // No newline character, return the whole string
	}
	return str[index+1:] // Return the substring after the last newline character
}
