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

package rockscollect

import (
	"errors"
	"testing"
)

func TestGetRocksDBViewerBinary_UnsupportedArch(t *testing.T) {
	_, err := GetRocksDBViewerBinary("s390x")
	if err == nil {
		t.Fatal("expected error for unsupported arch s390x")
	}
}

func TestGetRocksDBViewerBinary_RealBinaries(t *testing.T) {
	for _, arch := range []string{"x86_64", "aarch64"} {
		bin, err := GetRocksDBViewerBinary(arch)
		if errors.Is(err, ErrRocksDBViewerEmpty) {
			t.Skipf("embedded binary for %s is a placeholder, skipping size check", arch)
		}
		if err != nil {
			t.Fatalf("unexpected error for arch %s: %v", arch, err)
		}
		if len(bin) < 1000 {
			t.Errorf("binary for %s unexpectedly small: %d bytes", arch, len(bin))
		}
	}
}

func TestGetRocksDBViewerBinary_ArchMapping(t *testing.T) {
	a, err1 := GetRocksDBViewerBinary("x86_64")
	b, err2 := GetRocksDBViewerBinary("amd64")
	if err1 != nil || err2 != nil {
		if errors.Is(err1, ErrRocksDBViewerEmpty) && errors.Is(err2, ErrRocksDBViewerEmpty) {
			t.Skip("placeholders — skipping alias check")
		}
		t.Fatalf("errors: x86_64=%v, amd64=%v", err1, err2)
	}
	if len(a) != len(b) {
		t.Errorf("x86_64 and amd64 should resolve to same binary, got %d vs %d bytes", len(a), len(b))
	}

	c, err3 := GetRocksDBViewerBinary("aarch64")
	d, err4 := GetRocksDBViewerBinary("arm64")
	if err3 != nil || err4 != nil {
		if errors.Is(err3, ErrRocksDBViewerEmpty) && errors.Is(err4, ErrRocksDBViewerEmpty) {
			t.Skip("placeholders — skipping alias check")
		}
		t.Fatalf("errors: aarch64=%v, arm64=%v", err3, err4)
	}
	if len(c) != len(d) {
		t.Errorf("aarch64 and arm64 should resolve to same binary, got %d vs %d bytes", len(c), len(d))
	}
}
