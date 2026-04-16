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

// package rockscollect handles embedding and extraction of the dremio-rocksdb-viewer binary.
package rockscollect

import (
	_ "embed"
	"fmt"
)

//go:embed rocksdb/dremio-rocksdb-viewer-linux-amd64
var rocksdbViewerAMD64 []byte

//go:embed rocksdb/dremio-rocksdb-viewer-linux-arm64
var rocksdbViewerARM64 []byte

// ErrRocksDBViewerEmpty is returned when the embedded binary is a placeholder (empty).
var ErrRocksDBViewerEmpty = fmt.Errorf("embedded dremio-rocksdb-viewer binary is empty (placeholder)")

// GetRocksDBViewerBinary returns the embedded dremio-rocksdb-viewer binary bytes
// for the given architecture string (as returned by uname -m). It maps "x86_64"
// to the amd64 binary and "aarch64" to the arm64 binary. Returns
// ErrRocksDBViewerEmpty if the binary for the resolved architecture is an empty
// placeholder.
func GetRocksDBViewerBinary(arch string) ([]byte, error) {
	var bin []byte
	switch arch {
	case "x86_64", "amd64":
		bin = rocksdbViewerAMD64
	case "aarch64", "arm64":
		bin = rocksdbViewerARM64
	default:
		return nil, fmt.Errorf("unsupported architecture for embedded dremio-rocksdb-viewer: %q", arch)
	}
	if len(bin) == 0 {
		return nil, ErrRocksDBViewerEmpty
	}
	return bin, nil
}
