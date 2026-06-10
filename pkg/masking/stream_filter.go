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

package masking

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// configPatterns lists filename patterns that require secret masking.
var configPatterns = []string{
	"dremio.conf",
	"dremio-env",
	"*-site.xml",
	"logback*.xml",
	"*.json",
	"*.jks",
	"*.keytab",
}

// StreamingMaskFilter processes a tar.gz stream inline, applying masking to
// config file entries and passing all other entries through unchanged.
// This avoids extracting to disk -- masking happens in-memory on each tar entry.
type StreamingMaskFilter struct {
	configPatterns []string
}

// NewStreamingMaskFilter returns a StreamingMaskFilter configured with the
// default set of config filename patterns that require masking.
func NewStreamingMaskFilter() *StreamingMaskFilter {
	return &StreamingMaskFilter{
		configPatterns: configPatterns,
	}
}

// matchesConfigPattern reports whether the base name of the given path matches
// any of the configured config patterns.
func (f *StreamingMaskFilter) matchesConfigPattern(name string) bool {
	base := filepath.Base(name)
	for _, pattern := range f.configPatterns {
		matched, err := filepath.Match(pattern, base)
		if err != nil {
			continue
		}
		if matched {
			return true
		}
	}
	return false
}

// MaskConfigData applies line-by-line secret masking to raw configuration file
// content. Lines containing secret keywords have their values replaced with a
// redaction marker. This is the in-memory equivalent of RemoveSecretsFromDremioConf.
func MaskConfigData(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if checkStringForSecret(line) {
			lines[i] = maskConfigSecret(line)
		}
	}
	return []byte(strings.Join(lines, "\n"))
}

// Filter reads a tar.gz stream from src, applies masking to entries whose
// filenames match config patterns, and writes the resulting tar.gz to dst.
// Non-matching entries pass through byte-for-byte unchanged.
func (f *StreamingMaskFilter) Filter(dst io.Writer, src io.Reader) error {
	gzr, err := gzip.NewReader(src)
	if err != nil {
		return fmt.Errorf("creating gzip reader: %w", err)
	}
	defer gzr.Close() //nolint:errcheck // gzip reader close error is non-fatal

	gzw := gzip.NewWriter(dst)
	defer gzw.Close() //nolint:errcheck // explicit Close below; deferred as safety net

	tr := tar.NewReader(gzr)
	tw := tar.NewWriter(gzw)
	defer tw.Close() //nolint:errcheck // explicit Close below; deferred as safety net

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar entry: %w", err)
		}

		if f.matchesConfigPattern(hdr.Name) && hdr.Typeflag == tar.TypeReg {
			// Read the full entry into memory (config files are small).
			var buf bytes.Buffer
			if _, err := io.CopyN(&buf, tr, hdr.Size); err != nil && !errors.Is(err, io.EOF) {
				return fmt.Errorf("reading config entry %s: %w", hdr.Name, err)
			}

			masked := MaskConfigData(buf.Bytes())

			// Update header size to reflect potentially changed content length.
			hdr.Size = int64(len(masked))
			if err := tw.WriteHeader(hdr); err != nil {
				return fmt.Errorf("writing header for %s: %w", hdr.Name, err)
			}
			if _, err := tw.Write(masked); err != nil {
				return fmt.Errorf("writing masked content for %s: %w", hdr.Name, err)
			}
		} else {
			// Pass entry through unchanged.
			if err := tw.WriteHeader(hdr); err != nil {
				return fmt.Errorf("writing header for %s: %w", hdr.Name, err)
			}
			if hdr.Typeflag == tar.TypeReg {
				if _, err := io.CopyN(tw, tr, hdr.Size); err != nil && !errors.Is(err, io.EOF) {
					return fmt.Errorf("copying entry %s: %w", hdr.Name, err)
				}
			}
		}
	}

	// Explicitly close writers to flush all data before returning.
	if err := tw.Close(); err != nil {
		return fmt.Errorf("closing tar writer: %w", err)
	}
	if err := gzw.Close(); err != nil {
		return fmt.Errorf("closing gzip writer: %w", err)
	}

	return nil
}
