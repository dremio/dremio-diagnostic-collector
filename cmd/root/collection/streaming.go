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

package collection

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/simplelog"
)

// LocalStreamReceiver handles receiving a tar.gz stream from a remote
// process via an already-established io.Reader. It writes to a local file while
// computing SHA-256 for verification.
type LocalStreamReceiver struct {
	outputDir string
	node      string
}

// NewStreamReceiver creates a receiver for a given node's collector output.
func NewStreamReceiver(outputDir, node string) *LocalStreamReceiver {
	return &LocalStreamReceiver{outputDir: outputDir, node: node}
}

// Receive writes the stream to a file and returns the SHA-256 hash and byte count.
func (r *LocalStreamReceiver) Receive(collectorName string, stream io.Reader) (hash string, bytesReceived int64, err error) {
	destDir := filepath.Join(r.outputDir, r.node)
	if err := os.MkdirAll(destDir, 0o750); err != nil {
		return "", 0, fmt.Errorf("failed to create output dir %s: %w", destDir, err)
	}

	destPath := filepath.Join(destDir, collectorName+".tar.gz")
	f, err := os.Create(filepath.Clean(destPath))
	if err != nil {
		return "", 0, fmt.Errorf("failed to create output file %s: %w", destPath, err)
	}
	defer f.Close() //nolint:errcheck // best-effort close after copy

	hasher := sha256.New()
	mw := io.MultiWriter(f, hasher)

	n, err := io.Copy(mw, stream)
	if err != nil {
		// Clean up corrupt file
		_ = os.Remove(destPath) // best-effort cleanup
		return "", 0, fmt.Errorf("stream copy failed after %d bytes: %w", n, err)
	}

	return hex.EncodeToString(hasher.Sum(nil)), n, nil
}

// VerifyChecksum compares the received SHA-256 hash against the one reported
// on stderr. Returns an error on mismatch.
func VerifyChecksum(expected, actual string, expectedBytes, actualBytes int64) error {
	if expected != actual {
		return fmt.Errorf("SHA-256 mismatch: expected %s, got %s", expected, actual)
	}
	if expectedBytes != actualBytes {
		return fmt.Errorf("byte count mismatch: expected %d, got %d", expectedBytes, actualBytes)
	}
	return nil
}

// StderrParser parses structured lines from the remote process's stderr stream.
// Expected formats:
//   - KEEPALIVE: 150MB streamed
//   - CHECKSUM: sha256=<hex> bytes=<count>
type StderrParser struct {
	mu       sync.Mutex
	checksum string
	bytes    int64
	lastSeen time.Time
}

// NewStderrParser creates a parser for stderr lines.
func NewStderrParser() *StderrParser {
	return &StderrParser{}
}

// ParseLine processes a single stderr line.
func (p *StderrParser) ParseLine(line string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastSeen = time.Now()

	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "KEEPALIVE:") {
		simplelog.Debugf("keepalive from remote: %s", line)
		return
	}
	if strings.HasPrefix(line, "CHECKSUM:") {
		// Parse: CHECKSUM: sha256=<hex> bytes=<count>
		parts := strings.Fields(line)
		for _, part := range parts {
			if strings.HasPrefix(part, "sha256=") {
				p.checksum = strings.TrimPrefix(part, "sha256=")
			}
			if strings.HasPrefix(part, "bytes=") {
				_, _ = fmt.Sscanf(strings.TrimPrefix(part, "bytes="), "%d", &p.bytes) // parse error is non-fatal
			}
		}
		simplelog.Debugf("received checksum: sha256=%s bytes=%d", p.checksum, p.bytes)
		return
	}
	// Other stderr lines are logged as debug
	if line != "" {
		simplelog.Debugf("remote stderr: %s", line)
	}
}

// Checksum returns the parsed checksum and byte count.
func (p *StderrParser) Checksum() (string, int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.checksum, p.bytes
}

// LastSeen returns the time of the last stderr activity.
func (p *StderrParser) LastSeen() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastSeen
}
