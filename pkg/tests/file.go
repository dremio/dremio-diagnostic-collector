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
	"testing"
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

func MatchLines(t *testing.T, expectedLines []string, actualFile string) (bool, error) {

	type MatchLines struct {
		word    string
		matched bool
	}
	var matchLine MatchLines
	var matchLines []MatchLines
	var success int

	actualLines, err := readLines(actualFile)
	if err != nil {
		return false, err
	}

	// We take each expected line and check for matches
	mi := 0
	for _, expectedLine := range expectedLines {
		matchLine.word = expectedLine
		matchLine.matched = false
		matchLines = append(matchLines, matchLine)
		for _, actualLine := range actualLines {
			if strings.Contains(actualLine, expectedLine) {
				matchLines[mi].word = expectedLine
				matchLines[mi].matched = true
			}
		}
		mi++
	}
	// We have to see at least one match on each expected line
	// if not then the whole check fails
	for _, m := range matchLines {
		if !m.matched {
			t.Errorf("Did not find a match for %v in file %v", m.word, actualFile)
			return false, nil
		}
		success++
	}
	// the count of success must equal the count of expected words
	// or expressions
	if len(expectedLines) == success {
		return true, nil
	}
	return false, nil
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

func AssertFileHasContent(t *testing.T, filePath string) {
	if f, err := os.Stat(filePath); err != nil {
		t.Errorf("file %v failed %v", filePath, err)
	} else {
		if f.Size() == 0 {
			t.Errorf("file %v is empty", filePath)
		}
	}
}

func AssertFileHasExpectedLines(t *testing.T, expectedLines []string, filePath string) {
	success, _ := MatchLines(t, expectedLines, filePath)
	if !success {
		t.Errorf("file %v did not contain expected lines %v", filePath, expectedLines)
		err := echoLines(t, filePath)
		if err != nil {
			t.Errorf("when trying to echo lines from file %v, the following error was seen\n%v", filePath, err)
		}
	}
}

// echo out lines if an error is seen with unexpected lines
func echoLines(t *testing.T, filePath string) error {
	file, err := os.Open(filepath.Clean(filePath))
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		t.Log(scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	return nil
}
