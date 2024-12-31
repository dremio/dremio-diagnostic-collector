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

// output provides functinos around capturing output
package output

import (
	"fmt"
	"testing"
)

// TestCaptureOutput will test the CaptureOutput function with a simple print function
func TestCaptureOutput(t *testing.T) {
	expected := "Hello, world!\n"
	out, err := CaptureOutput(func() {
		fmt.Println("Hello, world!")
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if out != expected {
		t.Fatalf("expected %q, got %q", expected, out)
	}
}
