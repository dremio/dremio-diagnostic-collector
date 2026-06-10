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

package jvmcollect

import (
	"errors"
	"testing"
)

func TestGetAsprofBinary_UnsupportedArch(t *testing.T) {
	_, err := GetAsprofBinary("s390x")
	if err == nil {
		t.Fatal("expected error for unsupported arch")
	}
	if !contains(err.Error(), "unsupported") {
		t.Errorf("error should mention unsupported: %v", err)
	}
}

func TestGetAsprofBinary_RealBinaries(t *testing.T) {
	// Embedded binaries should be non-empty (real async-profiler v4.3 binaries).
	for _, arch := range []string{"x86_64", "aarch64", "amd64", "arm64"} {
		bin, err := GetAsprofBinary(arch)
		if err != nil {
			t.Errorf("arch %q: expected real binary, got error: %v", arch, err)
			continue
		}
		if len(bin) < 1000 {
			t.Errorf("arch %q: binary too small (%d bytes), expected real asprof binary", arch, len(bin))
		}
	}
}

func TestGetAsprofFiles_RealBinaries(t *testing.T) {
	for _, arch := range []string{"x86_64", "aarch64"} {
		files, err := GetAsprofFiles(arch)
		if err != nil {
			t.Errorf("arch %q: expected files, got error: %v", arch, err)
			continue
		}
		if len(files.Binary) < 1000 {
			t.Errorf("arch %q: binary too small (%d bytes)", arch, len(files.Binary))
		}
		if len(files.LibSO) < 1000 {
			t.Errorf("arch %q: .so too small (%d bytes)", arch, len(files.LibSO))
		}
	}
}

func TestGetAsprofBinary_ArchMapping(t *testing.T) {
	// We can't test non-empty return in dev (placeholders are empty),
	// but we can verify that the arch aliases map correctly by checking
	// they all return the same error (ErrAsprofEmpty) rather than
	// "unsupported architecture".
	aliases := map[string]string{
		"x86_64":  "amd64",
		"amd64":   "amd64",
		"aarch64": "arm64",
		"arm64":   "arm64",
	}
	for input := range aliases {
		_, err := GetAsprofBinary(input)
		if err == nil {
			continue // non-empty binary — fine
		}
		if !errors.Is(err, ErrAsprofEmpty) {
			t.Errorf("arch %q: expected ErrAsprofEmpty (correct mapping), got: %v", input, err)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
