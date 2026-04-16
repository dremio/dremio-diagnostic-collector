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

// masking hides secrets in files and replaces them with redacted text
package masking

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/local/conf"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/simplelog"
)

var secretKeywords = []string{
	"passw",
	"access_key",
	"secret",
}

var reConfigSecret = regexp.MustCompile(`:\s*([^,\n]+)`)
var rePATToken = regexp.MustCompile(`--` + conf.KeyDremioPatToken + ` [^ ]+`)
var rePATShort = regexp.MustCompile(`-t [^ ]+`)

// checkStringForSecret returns true if the string contains any keyword that might indicate a secret.
func checkStringForSecret(s string) bool {
	for _, keyword := range secretKeywords {
		if strings.Contains(strings.ToLower(s), keyword) {
			return true
		}
	}
	return false
}

// maskConfigSecret masks the potential secrets found in the given line of a configuration file.
// It uses a regular expression (regex) to identify potential secrets and replaces them with "<REMOVED_POTENTIAL_SECRET>".
// This pattern works as follows:
//
// : matches the colon character.
// \s* matches zero or more whitespace characters.
// ([^,\n]+) captures one or more characters that are not a comma or a newline. This is the value that you want to replace.
func maskConfigSecret(line string) string {
	matches := reConfigSecret.FindStringSubmatch(line)
	// If there is more than one match, the secret will be the second element in the slice (at index 1)
	if len(matches) > 1 {
		secret := matches[1]
		// Replace the secret with the masking text
		line = strings.ReplaceAll(line, secret, "\"<REMOVED_POTENTIAL_SECRET>\"")
	}
	return line
}

func MaskPAT(line string) string {
	matches := rePATToken.FindStringSubmatch(line)
	// If there is more than one match, the secret will be the second element in the slice (at index 1)
	if len(matches) >= 1 {
		secret := matches[0]
		// Replace the secret with the masking text
		line = strings.ReplaceAll(line, secret, "\"<REMOVED_PAT_TOKEN>\"")
	}
	matches2 := rePATShort.FindStringSubmatch(line)
	// If there is more than one match, the secret will be the second element in the slice (at index 1)
	if len(matches2) >= 1 {
		secret := matches2[0]
		// Replace the secret with the masking text
		line = strings.ReplaceAll(line, secret, "\"<REMOVED_PAT_TOKEN>\"")
	}
	return line
}

// RemoveSecretsFromDremioConf takes a configuration file as an input and masks any potential secrets.
// It returns an error if it encounters any issue during the process.
func RemoveSecretsFromDremioConf(configFile string) error {
	// Check if the input file is a Dremio configuration file
	if strings.HasSuffix(configFile, "dremio.conf") {
		simplelog.Debugf("... Removing potential secrets from %s\n", configFile)

		// Open the file. Clean the configFile path before opening to remove any relative or redundant path elements.
		// If there is an issue opening the file, an error is returned.
		file, err := os.Open(path.Clean(configFile))
		if err != nil {
			return fmt.Errorf("unable to open file %v: %w", configFile, err)
		}
		defer file.Close()
		scanner := bufio.NewScanner(file)

		// This slice will hold all the lines from the file, after potentially modifying them
		cleansedData := []string{}

		for scanner.Scan() {
			line := scanner.Text()
			// If the line contains a potential secret, mask the secret
			if checkStringForSecret(line) {
				line = maskConfigSecret(line)
			}
			cleansedData = append(cleansedData, line)
		}

		// Write the contents of cleansedData back into the file.
		// If there is an issue writing to the file, an error is returned.
		if err := os.WriteFile(configFile, []byte(strings.Join(cleansedData, "\n")), 0o600); err != nil {
			return fmt.Errorf("unable to write new file %v: %w", configFile, err)
		}
	} else {
		// If the configFile does not end with "dremio.conf", an error is returned.
		return fmt.Errorf("expected file with name '%s', got '%s' instead", "dremio.conf", configFile)
	}
	// If all steps complete without error, return nil to indicate success
	return nil
}
