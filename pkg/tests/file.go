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

package tests

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

func MatchFile(expectedFile, actualFile string) (success bool, err error) {

	expectedLines, err := readLines(expectedFile)
	if err != nil {
		return false, err
	}

	actualLines, err := readLines(actualFile)
	if err != nil {
		return false, err
	}
	//remove cross platform line endings to make the tests work on windows and linux
	expectedText := normalizeText(expectedLines)
	actualText := normalizeText(actualLines)

	return expectedText == actualText, nil
}

func readLines(filePath string) ([]string, error) {
	file, err := os.Open(filepath.Clean(filePath))
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return lines, nil
}

func normalizeText(lines []string) string {
	return strings.Join(lines, "\n")
}
