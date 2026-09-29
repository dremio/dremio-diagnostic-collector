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

package kubernetes

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/collection"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/shutdown"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// scriptedExecutor stands in for a remote command: it writes fixed stdout and
// stderr, then returns err.
type scriptedExecutor struct {
	stdout, stderr string
	err            error
}

func (s *scriptedExecutor) Stream(o remotecommand.StreamOptions) error {
	return s.StreamWithContext(context.Background(), o)
}

func (s *scriptedExecutor) StreamWithContext(_ context.Context, o remotecommand.StreamOptions) error {
	if o.Stdout != nil {
		_, _ = io.WriteString(o.Stdout, s.stdout)
	}
	if o.Stderr != nil {
		_, _ = io.WriteString(o.Stderr, s.stderr)
	}
	return s.err
}

// recordingFactory returns an ExecutorFactory that flags its use and hands out ex.
func recordingFactory(called *bool, ex remotecommand.Executor) ExecutorFactory {
	return func(_ *rest.Config, _ string, _ *url.URL) (remotecommand.Executor, error) {
		*called = true
		return ex, nil
	}
}

// newRoutingTestActions builds actions with both executor factories injected.
// The clientset only builds request URLs; nothing touches the network.
func newRoutingTestActions(t *testing.T, spdyFn, streamFn ExecutorFactory) *KubeCtlAPIActions {
	t.Helper()
	cfg := &rest.Config{Host: "https://fake"}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("NewForConfig: %v", err)
	}
	return &KubeCtlAPIActions{
		namespace:        "ns",
		client:           cs,
		config:           cfg,
		hook:             shutdown.NewHook(),
		pidHosts:         map[string]string{},
		containerCache:   map[string]string{"pod-0": "dremio-coordinator"},
		timeoutMinutes:   1,
		protocol:         "SPDY",
		spdyExecutorFn:   spdyFn,
		streamExecutorFn: streamFn,
	}
}

func TestNewKeepaliveFreeSPDYExecutor_Builds(t *testing.T) {
	cfg := &rest.Config{Host: "https://example.invalid", BearerToken: "token"}
	u, _ := url.Parse("https://example.invalid/api/v1/namespaces/ns/pods/p/exec")
	ex, err := newKeepaliveFreeSPDYExecutor(cfg, "POST", u)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ex == nil {
		t.Fatal("expected an executor")
	}
}

func TestNewKeepaliveFreeSPDYExecutor_BadTLSConfig(t *testing.T) {
	cfg := &rest.Config{
		Host:            "https://example.invalid",
		TLSClientConfig: rest.TLSClientConfig{CAFile: "does-not-exist.pem"},
	}
	u, _ := url.Parse("https://example.invalid/api/v1/namespaces/ns/pods/p/exec")
	if _, err := newKeepaliveFreeSPDYExecutor(cfg, "POST", u); err == nil {
		t.Fatal("expected an error for an unreadable CA file")
	}
}

func TestStreamFromHost_UsesKeepaliveFreeExecutor(t *testing.T) {
	var usedDefault, usedStream bool
	a := newRoutingTestActions(t,
		recordingFactory(&usedDefault, &scriptedExecutor{}),
		recordingFactory(&usedStream, &scriptedExecutor{stdout: "data"}))

	var buf bytes.Buffer
	if err := a.StreamFromHost("pod-0", "/opt/dremio/log/server.log", &buf, false); err != nil {
		t.Fatalf("StreamFromHost: %v", err)
	}
	if !usedStream || usedDefault {
		t.Fatalf("StreamFromHost must use the keepalive-free executor (stream=%v default=%v)", usedStream, usedDefault)
	}
	if buf.String() != "data" {
		t.Errorf("got %q, want %q", buf.String(), "data")
	}
}

func TestHostExecute_KeepsDefaultExecutor(t *testing.T) {
	var usedDefault, usedStream bool
	a := newRoutingTestActions(t,
		recordingFactory(&usedDefault, &scriptedExecutor{stdout: "hi\n"}),
		recordingFactory(&usedStream, &scriptedExecutor{}))

	out, err := a.HostExecute(false, "pod-0", "echo", "hi")
	if err != nil {
		t.Fatalf("HostExecute: %v", err)
	}
	if !usedDefault || usedStream {
		t.Fatalf("HostExecute must keep the default keepalive executor (default=%v stream=%v)", usedDefault, usedStream)
	}
	if out != "hi" {
		t.Errorf("got %q, want %q", out, "hi")
	}
}

// Compile-time check: the K8s API transport provides the keepalive-free stream.
var _ collection.KeepaliveFreeStreamer = (*KubeCtlAPIActions)(nil)

// runEOS feeds chunks through an eosLineWriter and returns the emitted lines
// and finish()'s verdict.
func runEOS(chunks ...string) ([]string, error) {
	var got []string
	w := &eosLineWriter{output: func(l string) { got = append(got, l) }}
	for _, c := range chunks {
		if _, err := w.Write([]byte(c)); err != nil {
			return got, err
		}
	}
	return got, w.finish()
}

func TestEOSLineWriter(t *testing.T) {
	tests := []struct {
		name       string
		chunks     []string
		want       []string
		incomplete bool
		errSubstr  string
	}{
		{name: "output ending in newline", chunks: []string{"a\nb\n", "\n__DDC_EOS__:0\n"}, want: []string{"a", "b"}},
		{name: "last line without newline", chunks: []string{"a\nb", "\n__DDC_EOS__:0\n"}, want: []string{"a", "b"}},
		{name: "empty output", chunks: []string{"\n__DDC_EOS__:0\n"}, want: nil},
		{name: "legit trailing empty line", chunks: []string{"a\n\n", "\n__DDC_EOS__:0\n"}, want: []string{"a", ""}},
		{name: "marker split across writes", chunks: []string{"a\n\n__DDC_E", "OS__:0\n"}, want: []string{"a"}},
		{name: "marker-like data line", chunks: []string{"__DDC_EOS__:x\n", "\n__DDC_EOS__:0\n"}, want: []string{"__DDC_EOS__:x"}},
		{name: "missing marker drops partial tail", chunks: []string{"a\nb\npar"}, want: []string{"a", "b"}, incomplete: true},
		{name: "missing marker after full line", chunks: []string{"a\nb\n"}, want: []string{"a", "b"}, incomplete: true},
		{name: "non-zero exit code", chunks: []string{"a\n", "\n__DDC_EOS__:3\n"}, want: []string{"a"}, errSubstr: "exited with code 3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := runEOS(tt.chunks...)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("lines = %q, want %q", got, tt.want)
			}
			switch {
			case tt.incomplete:
				if !errors.Is(err, collection.ErrStreamIncomplete) {
					t.Errorf("err = %v, want ErrStreamIncomplete", err)
				}
			case tt.errSubstr != "":
				if err == nil || !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("err = %v, want it to contain %q", err, tt.errSubstr)
				}
			default:
				if err != nil {
					t.Errorf("unexpected err: %v", err)
				}
			}
		})
	}
}

// failIfCalled fails the test if the default keepalive executor is used.
func failIfCalled(t *testing.T) ExecutorFactory {
	return func(_ *rest.Config, _ string, _ *url.URL) (remotecommand.Executor, error) {
		t.Error("default keepalive executor must not be used for keepalive-free streams")
		return &scriptedExecutor{}, nil
	}
}

func TestHostExecuteAndStreamNoKeepalive_CompleteStream(t *testing.T) {
	var gotURL *url.URL
	streamFn := func(_ *rest.Config, _ string, u *url.URL) (remotecommand.Executor, error) {
		gotURL = u
		return &scriptedExecutor{
			stdout: "{\"a\":1}\n{\"a\":2}\n\n__DDC_EOS__:0\n",
			stderr: "warning: slow disk\n",
		}, nil
	}
	a := newRoutingTestActions(t, failIfCalled(t), streamFn)

	var lines []string
	err := a.HostExecuteAndStreamNoKeepalive("pod-0", func(l string) { lines = append(lines, l) },
		"/tmp/dremio-rocksdb-viewer", "-type", "queries_perf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := []string{`{"a":1}`, `{"a":2}`}; !reflect.DeepEqual(lines, want) {
		t.Errorf("lines = %q, want %q (stderr and marker must not reach the handler)", lines, want)
	}
	wantCmd := []string{"sh", "-c", "/tmp/dremio-rocksdb-viewer -type queries_perf; printf '\\n__DDC_EOS__:%d\\n' $?"}
	if got := gotURL.Query()["command"]; !reflect.DeepEqual(got, wantCmd) {
		t.Errorf("command = %q, want %q", got, wantCmd)
	}
}

func TestHostExecuteAndStreamNoKeepalive_TruncatedStream(t *testing.T) {
	a := newRoutingTestActions(t, failIfCalled(t), func(_ *rest.Config, _ string, _ *url.URL) (remotecommand.Executor, error) {
		return &scriptedExecutor{stdout: "{\"a\":1}\n{\"a\":2"}, nil
	})
	var lines []string
	err := a.HostExecuteAndStreamNoKeepalive("pod-0", func(l string) { lines = append(lines, l) }, "viewer")
	if !errors.Is(err, collection.ErrStreamIncomplete) {
		t.Fatalf("err = %v, want ErrStreamIncomplete", err)
	}
	if want := []string{`{"a":1}`}; !reflect.DeepEqual(lines, want) {
		t.Errorf("lines = %q, want %q (partial record must be dropped)", lines, want)
	}
}

func TestHostExecuteAndStreamNoKeepalive_TransportError(t *testing.T) {
	a := newRoutingTestActions(t, failIfCalled(t), func(_ *rest.Config, _ string, _ *url.URL) (remotecommand.Executor, error) {
		return &scriptedExecutor{stdout: "x\n", err: errors.New("connection reset by peer")}, nil
	})
	var lines []string
	err := a.HostExecuteAndStreamNoKeepalive("pod-0", func(l string) { lines = append(lines, l) }, "viewer")
	if err == nil || !strings.Contains(err.Error(), "connection reset") {
		t.Fatalf("err = %v, want the transport error", err)
	}
	if want := []string{"x"}; !reflect.DeepEqual(lines, want) {
		t.Errorf("lines = %q, want %q (complete lines are kept)", lines, want)
	}
}
