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
	"fmt"

	"github.com/manifoldco/promptui"
)

func PromptForPAT() (string, error) {
	prompt := promptui.Prompt{
		Label: "Enter Dremio personal access token",
		Mask:  '*',
	}

	result, err := prompt.Run()
	if err != nil {
		return "", fmt.Errorf("prompt failed %w", err)
	}

	return result, nil
}
