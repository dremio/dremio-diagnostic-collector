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

// package jvmcollect handles parsing of the jvm information
package jvmcollect

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Embedded asprof binaries for Linux. These are placeholder empty files that
// will be replaced with actual async-profiler release binaries during the
// release build process. When empty, ExtractAsprof returns ErrAsprofEmpty
// so callers can fall back to PATH lookup.
//
//go:embed asprof/asprof-linux-amd64
var asprofAMD64 []byte

//go:embed asprof/asprof-linux-arm64
var asprofARM64 []byte

//go:embed asprof/libasyncProfiler-linux-amd64.so
var libAsprofAMD64 []byte

//go:embed asprof/libasyncProfiler-linux-arm64.so
var libAsprofARM64 []byte

// ErrAsprofEmpty is returned when the embedded binary is a placeholder (empty).
var ErrAsprofEmpty = fmt.Errorf("embedded asprof binary is empty (placeholder)")

// AsprofFiles holds both the asprof binary and its companion libasyncProfiler.so.
type AsprofFiles struct {
	Binary []byte // the asprof ELF binary
	LibSO  []byte // libasyncProfiler.so (loaded by JVM via agentpath)
}

// GetAsprofBinary returns the embedded asprof binary bytes for the given
// architecture string (as returned by uname -m). It maps "x86_64" to the
// amd64 binary and "aarch64" to the arm64 binary. Returns ErrAsprofEmpty
// if the binary for the resolved architecture is an empty placeholder.
func GetAsprofBinary(arch string) ([]byte, error) {
	var bin []byte
	switch arch {
	case "x86_64", "amd64":
		bin = asprofAMD64
	case "aarch64", "arm64":
		bin = asprofARM64
	default:
		return nil, fmt.Errorf("unsupported architecture for embedded asprof: %q", arch)
	}
	if len(bin) == 0 {
		return nil, ErrAsprofEmpty
	}
	return bin, nil
}

// GetAsprofFiles returns both the asprof binary and libasyncProfiler.so for
// the given architecture. The caller must upload both: bin/asprof + lib/libasyncProfiler.so.
func GetAsprofFiles(arch string) (*AsprofFiles, error) {
	var bin, lib []byte
	switch arch {
	case "x86_64", "amd64":
		bin = asprofAMD64
		lib = libAsprofAMD64
	case "aarch64", "arm64":
		bin = asprofARM64
		lib = libAsprofARM64
	default:
		return nil, fmt.Errorf("unsupported architecture for embedded asprof: %q", arch)
	}
	if len(bin) == 0 {
		return nil, ErrAsprofEmpty
	}
	if len(lib) == 0 {
		return nil, fmt.Errorf("embedded libasyncProfiler.so is empty for %q", arch)
	}
	return &AsprofFiles{Binary: bin, LibSO: lib}, nil
}

// ExtractAsprof writes the embedded asprof binary and its companion
// libasyncProfiler.so for the current architecture into
// targetDir/bin/asprof and targetDir/lib/libasyncProfiler.so respectively,
// then returns the path to the launcher. async-profiler's launcher looks
// for libasyncProfiler.so under ../lib relative to its own location, so
// both files must be extracted in this layout.
//
// Returns an error if the current OS is not Linux, the architecture is
// unsupported, or either embedded file is an empty placeholder.
func ExtractAsprof(targetDir string) (string, error) {
	if runtime.GOOS != "linux" {
		return "", fmt.Errorf("asprof embedding is only supported on linux, current OS: %s", runtime.GOOS)
	}

	var bin, lib []byte
	switch runtime.GOARCH {
	case "amd64":
		bin = asprofAMD64
		lib = libAsprofAMD64
	case "arm64":
		bin = asprofARM64
		lib = libAsprofARM64
	default:
		return "", fmt.Errorf("unsupported architecture for embedded asprof: %s", runtime.GOARCH)
	}

	if len(bin) == 0 {
		return "", ErrAsprofEmpty
	}
	if len(lib) == 0 {
		return "", fmt.Errorf("embedded libasyncProfiler.so is empty for %s", runtime.GOARCH)
	}

	binDir := filepath.Join(targetDir, "bin")
	libDir := filepath.Join(targetDir, "lib")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		return "", fmt.Errorf("failed to create %s: %w", binDir, err)
	}
	if err := os.MkdirAll(libDir, 0o700); err != nil {
		return "", fmt.Errorf("failed to create %s: %w", libDir, err)
	}

	binPath := filepath.Join(binDir, "asprof")
	libPath := filepath.Join(libDir, "libasyncProfiler.so")

	if err := os.WriteFile(binPath, bin, 0o600); err != nil {
		return "", fmt.Errorf("failed to write embedded asprof to %s: %w", binPath, err)
	}
	if err := os.Chmod(binPath, 0o700); err != nil { // #nosec G302 -- asprof must be executable to run locally
		return "", fmt.Errorf("failed to chmod embedded asprof at %s: %w", binPath, err)
	}
	if err := os.WriteFile(libPath, lib, 0o600); err != nil {
		return "", fmt.Errorf("failed to write embedded libasyncProfiler.so to %s: %w", libPath, err)
	}

	return binPath, nil
}
