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
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/proxy"
	remotecommandconsts "k8s.io/apimachinery/pkg/util/remotecommand"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	kubeletrc "k8s.io/kubelet/pkg/cri/streaming/remotecommand"
)

// payloadExec is a kubelet exec backend that writes payload to stdout as fast
// as the connection accepts it and exits, like `gzip -1 -c file.gz`.
type payloadExec struct{ payload []byte }

func (p payloadExec) ExecInContainer(_ context.Context, _ string, _ types.UID, _ string, _ []string, _ io.Reader, out, _ io.WriteCloser, _ bool, _ <-chan remotecommand.TerminalSize, _ time.Duration) error {
	// 32 KB writes like the container runtime's io.Copy: realistic SPDY frame
	// sizes, so backpressure reaches the socket buffers. One huge write becomes a
	// single frame the client reads into memory at once, which hides the bug.
	const chunk = 32 << 10
	for off := 0; off < len(p.payload); off += chunk {
		if _, err := out.Write(p.payload[off:min(off+chunk, len(p.payload))]); err != nil {
			return err
		}
	}
	return nil
}

type proxyErrResponder struct{}

func (proxyErrResponder) Error(w http.ResponseWriter, _ *http.Request, err error) {
	http.Error(w, err.Error(), http.StatusBadGateway)
}

// newExecLoopback starts a TLS kubelet exec endpoint (real ServeExec) behind two
// apimachinery UpgradeAwareHandler hops — kubelet proxy and API server proxy —
// and returns the exec URL a client POSTs to plus the PEM of the certificate
// all httptest TLS servers share (it covers 127.0.0.1), so every hop verifies TLS.
func newExecLoopback(t *testing.T, payload []byte) (*url.URL, []byte) {
	t.Helper()
	kubelet := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		kubeletrc.ServeExec(w, r, payloadExec{payload}, "pod", "", "c", []string{"stream"},
			&kubeletrc.Options{Stdout: true, Stderr: true}, time.Hour, 30*time.Second,
			remotecommandconsts.SupportedStreamingProtocols)
	}))
	t.Cleanup(kubelet.Close)
	roots := x509.NewCertPool()
	roots.AddCert(kubelet.Certificate())
	target := kubelet.URL
	for i := 0; i < 2; i++ {
		u, _ := url.Parse(target + "/exec")
		h := proxy.NewUpgradeAwareHandler(u, &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
		}, false, true, proxyErrResponder{})
		h.UseLocationHost = true
		srv := httptest.NewTLSServer(h)
		t.Cleanup(srv.Close)
		target = srv.URL
	}
	u, _ := url.Parse(target + "/exec")
	return u, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: kubelet.Certificate().Raw})
}

// throttledBuffer drains at bytesPerSec, modelling a slow client link.
type throttledBuffer struct {
	buf         bytes.Buffer // not embedded: a promoted ReadFrom would let io.Copy bypass Write
	bytesPerSec float64
	start       time.Time
}

func (b *throttledBuffer) Write(p []byte) (int, error) {
	if b.start.IsZero() {
		b.start = time.Now()
	}
	n, err := b.buf.Write(p)
	want := time.Duration(float64(b.buf.Len()) / b.bytesPerSec * float64(time.Second))
	if d := want - time.Since(b.start); d > 0 {
		time.Sleep(d)
	}
	return n, err
}

func loopbackConfig(u *url.URL, caPEM []byte) *rest.Config {
	return &rest.Config{Host: u.Scheme + "://" + u.Host, TLSClientConfig: rest.TLSClientConfig{CAData: caPEM}}
}

// streamOnce runs one exec and reports whether the whole payload arrived intact.
func streamOnce(t *testing.T, ex remotecommand.Executor, payload []byte, bytesPerSec float64) (bool, error) {
	t.Helper()
	out := &throttledBuffer{bytesPerSec: bytesPerSec}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	err := ex.StreamWithContext(ctx, remotecommand.StreamOptions{Stdout: out, Stderr: io.Discard})
	return out.buf.Len() == len(payload) && sha256.Sum256(out.buf.Bytes()) == sha256.Sum256(payload), err
}

// testPayload returns n deterministic bytes.
func testPayload(n int) []byte {
	p := make([]byte, n)
	for i := range p {
		p[i] = byte(i * 7)
	}
	return p
}

func TestKeepaliveFreeExecutor_DeliversSlowStreamCompletely(t *testing.T) {
	payload := testPayload(4 << 20)
	u, caPEM := newExecLoopback(t, payload)
	ex, err := newKeepaliveFreeSPDYExecutor(loopbackConfig(u, caPEM), "POST", u)
	if err != nil {
		t.Fatalf("executor: %v", err)
	}
	if ok, err := streamOnce(t, ex, payload, 2<<20); err != nil || !ok {
		t.Fatalf("stream incomplete: ok=%v err=%v", ok, err)
	}
}

// TestKeepaliveTruncation_Regression339 first proves the environment reproduces
// the bug with client-go's default executor (5 s pings), then requires the
// keepalive-free executor to deliver every byte under the same conditions.
//
// It never runs under -short or on non-Linux hosts. To run it manually
// (~60 s), cross-compile and run the binary on any Linux host:
//
//	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c -o k8s-loopback.test ./cmd/root/kubernetes/ && ./k8s-loopback.test -test.run Regression339 -test.v
func TestKeepaliveTruncation_Regression339(t *testing.T) {
	if testing.Short() {
		t.Skip("slow (~60 s)")
	}
	if runtime.GOOS != "linux" {
		t.Skip("keepalive truncation needs Linux abort-on-data semantics on the server side")
	}
	// 8 MB at 500 KB/s (~16 s per transfer): the payload must exceed what the slow
	// client's receive buffer absorbs, so data is still queued server-side when a
	// ping lands. 4 MB at 400 KB/s did not reproduce in 3/3 trials.
	payload := testPayload(8 << 20)
	const slow = 500 << 10
	u, caPEM := newExecLoopback(t, payload)

	truncated := false
	for i := 0; i < 3 && !truncated; i++ {
		ex, err := remotecommand.NewSPDYExecutor(loopbackConfig(u, caPEM), "POST", u) // client-go default: 5 s pings
		if err != nil {
			t.Fatalf("default executor: %v", err)
		}
		ok, _ := streamOnce(t, ex, payload, slow)
		truncated = !ok
	}
	if !truncated {
		t.Skip("environment does not reproduce keepalive truncation; nothing to compare against")
	}

	for i := 1; i <= 3; i++ {
		ex, err := newKeepaliveFreeSPDYExecutor(loopbackConfig(u, caPEM), "POST", u)
		if err != nil {
			t.Fatalf("keepalive-free executor: %v", err)
		}
		if ok, err := streamOnce(t, ex, payload, slow); err != nil || !ok {
			t.Fatalf("trial %d: keepalive-free executor lost data (err=%v)", i, err)
		}
	}
}
