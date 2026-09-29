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

package collection

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// withNoStreamBackoff removes the stream retry backoff for one test.
func withNoStreamBackoff(t *testing.T) {
	t.Helper()
	orig := streamBackoffSleep
	streamBackoffSleep = func(time.Duration) {}
	t.Cleanup(func() { streamBackoffSleep = orig })
}

// statCollector answers `stat` probes with size/statErr and rejects anything else.
func statCollector(size string, statErr error) *mockStreamCollector {
	return &mockStreamCollector{
		hostExecuteFunc: func(_ bool, _ string, args ...string) (string, error) {
			if len(args) > 0 && args[0] == "stat" {
				return size, statErr
			}
			return "", fmt.Errorf("unexpected command: %v", args)
		},
	}
}

func TestCheckStreamComplete(t *testing.T) {
	tests := []struct {
		name          string
		got, expected int64
		statOut       string
		statErr       error
		wantTruncated bool
	}{
		{name: "size unknown", got: 5, expected: 0},
		{name: "exact", got: 10, expected: 10},
		{name: "file grew", got: 12, expected: 10},
		{name: "rotated smaller", got: 4, expected: 10, statOut: "4"},
		{name: "rotated to empty", got: 0, expected: 10, statOut: "0"},
		{name: "rotated but still short", got: 3, expected: 10, statOut: "4", wantTruncated: true},
		{name: "not shrunk", got: 5, expected: 10, statOut: "10", wantTruncated: true},
		{name: "probe fails", got: 5, expected: 10, statErr: errors.New("exit code 1"), wantTruncated: true},
		{name: "probe unparsable", got: 5, expected: 10, statOut: "garbage", wantTruncated: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkStreamComplete(statCollector(tt.statOut, tt.statErr), "h", "/r/f.log", tt.got, tt.expected)
			if tt.wantTruncated != errors.Is(err, ErrStreamTruncated) {
				t.Fatalf("err = %v, wantTruncated = %v", err, tt.wantTruncated)
			}
			if !tt.wantTruncated && err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if tt.name == "rotated but still short" {
				var rot *fileRotatedError
				if !errors.As(err, &rot) || rot.Size != 4 {
					t.Fatalf("err = %v, want *fileRotatedError with Size 4", err)
				}
			}
		})
	}
}

func TestStreamFile_RetriesTruncatedStream(t *testing.T) {
	withNoStreamBackoff(t)
	content := []byte("0123456789")
	calls := 0
	mc := statCollector("10", nil)
	mc.streamFunc = func(_, _ string, w io.Writer) error {
		calls++
		if calls == 1 {
			_, err := w.Write(content[:4])
			return err
		}
		_, err := w.Write(content)
		return err
	}
	dest := filepath.Join(t.TempDir(), "f.log")
	n, hashCh, err := streamFile(mc, "h", "/r/f.log", dest, maxRetries, int64(len(content)), "f.log", "", false)
	if err != nil {
		t.Fatalf("streamFile: %v", err)
	}
	<-hashCh
	if n != int64(len(content)) || calls != 2 {
		t.Fatalf("n=%d calls=%d, want n=%d calls=2", n, calls, len(content))
	}
	if data, _ := os.ReadFile(dest); string(data) != string(content) {
		t.Errorf("file = %q, want %q", data, content)
	}
}

func TestStreamFile_PersistentTruncationSkips(t *testing.T) {
	withNoStreamBackoff(t)
	calls := 0
	mc := statCollector("10", nil)
	mc.streamFunc = func(_, _ string, w io.Writer) error {
		calls++
		_, err := w.Write([]byte("0123"))
		return err
	}
	dest := filepath.Join(t.TempDir(), "f.log")
	_, _, err := streamFile(mc, "h", "/r/f.log", dest, maxRetries, 10, "f.log", "", false)
	if !errors.Is(err, ErrStreamTruncated) {
		t.Fatalf("err = %v, want ErrStreamTruncated", err)
	}
	if calls != maxRetries {
		t.Errorf("calls = %d, want %d", calls, maxRetries)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Error("truncated file must be removed")
	}
}

// rotatingCollector answers stat with 50 on the first probe and 60 afterwards.
func rotatingCollector() *mockStreamCollector {
	probes := 0
	return &mockStreamCollector{
		hostExecuteFunc: func(_ bool, _ string, args ...string) (string, error) {
			if len(args) > 0 && args[0] == "stat" {
				probes++
				if probes == 1 {
					return "50", nil
				}
				return "60", nil
			}
			return "", fmt.Errorf("unexpected command: %v", args)
		},
	}
}

func TestStreamFile_RotatedAndGrowing(t *testing.T) {
	for _, useGzip := range []bool{false, true} {
		t.Run(fmt.Sprintf("gzip=%v", useGzip), func(t *testing.T) {
			withNoStreamBackoff(t)
			calls := 0
			mc := rotatingCollector()
			mc.streamFunc = func(_, _ string, w io.Writer) error {
				calls++
				size := 45
				if calls > 1 {
					size = 55
				}
				data := bytes.Repeat([]byte("x"), size)
				if useGzip {
					var buf bytes.Buffer
					gz := gzip.NewWriter(&buf)
					_, _ = gz.Write(data)
					_ = gz.Close()
					data = buf.Bytes()
				}
				_, err := w.Write(data)
				return err
			}
			dest := filepath.Join(t.TempDir(), "f.log")
			n, hashCh, err := streamFile(mc, "h", "/r/f.log", dest, maxRetries, 100, "f.log", "", useGzip)
			if err != nil {
				t.Fatalf("streamFile: %v", err)
			}
			<-hashCh
			if n != 55 || calls != 2 {
				t.Fatalf("n=%d calls=%d, want n=55 calls=2", n, calls)
			}
		})
	}
}

func TestStreamFile_RotatedPersistentTruncationSkips(t *testing.T) {
	withNoStreamBackoff(t)
	calls := 0
	mc := statCollector("50", nil)
	mc.streamFunc = func(_, _ string, w io.Writer) error {
		calls++
		_, err := w.Write(bytes.Repeat([]byte("x"), 45))
		return err
	}
	dest := filepath.Join(t.TempDir(), "f.log")
	_, _, err := streamFile(mc, "h", "/r/f.log", dest, maxRetries, 100, "f.log", "", false)
	if !errors.Is(err, ErrStreamTruncated) {
		t.Fatalf("err = %v, want ErrStreamTruncated", err)
	}
	if calls != maxRetries {
		t.Errorf("calls = %d, want %d", calls, maxRetries)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Error("truncated file must be removed")
	}
}

func TestIsTransientError_TruncationWithNotFoundPath(t *testing.T) {
	err := fmt.Errorf("%w for h:/var/log/not found/x.log: got 1 of 2 bytes", ErrStreamTruncated)
	if !isTransientError(err) {
		t.Error("truncation must be transient even when the path contains 'not found'")
	}
}
