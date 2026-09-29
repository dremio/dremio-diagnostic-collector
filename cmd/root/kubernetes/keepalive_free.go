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
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/cli"
	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/collection"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/simplelog"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/httpstream/spdy"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/kubectl/pkg/scheme"
)

// newKeepaliveFreeSPDYExecutor is client-go's spdy.RoundTripperFor +
// NewSPDYExecutor with the 5 s SPDY keepalive ping disabled; authentication,
// TLS and proxy handling are identical.
//
// When a remote command (cat, gzip -c) exits while its output is still draining
// to a slow client, the server side closes its socket. A later client PING that
// reaches the closed socket makes the Linux kernel reset the connection
// (TCPAbortOnData), discarding the queued stdout tail and the exit status, and
// client-go then reports success on truncated output (#339). A streaming exec is
// never idle, so it needs no keepalive; OS-level TCP keepalive stays on.
func newKeepaliveFreeSPDYExecutor(config *rest.Config, method string, u *url.URL) (remotecommand.Executor, error) {
	tlsConfig, err := rest.TLSConfigFor(config)
	if err != nil {
		return nil, err
	}
	proxy := http.ProxyFromEnvironment
	if config.Proxy != nil {
		proxy = config.Proxy
	}
	upgrader, err := spdy.NewRoundTripperWithConfig(spdy.RoundTripperConfig{
		TLS:        tlsConfig,
		Proxier:    proxy,
		PingPeriod: 0,
	})
	if err != nil {
		return nil, err
	}
	wrapper, err := rest.HTTPWrappersForConfig(config, upgrader)
	if err != nil {
		return nil, err
	}
	return remotecommand.NewSPDYExecutorForTransports(wrapper, upgrader, method, u)
}

// eosMarker prefixes the line HostExecuteAndStreamNoKeepalive appends after the
// command's output; the rest of that line is the command's exit code.
const eosMarker = "__DDC_EOS__:"

// HostExecuteAndStreamNoKeepalive runs args through `sh -c` over a
// keepalive-free exec and streams stdout lines to output. The command is
// followed by `printf '\n__DDC_EOS__:%d\n' $?`; the marker shares stdout with
// the command's output, so it only arrives if every byte before it did. A
// stream that ends without it returns collection.ErrStreamIncomplete; a
// non-zero exit code carried by it is returned as an error. stderr lines are
// logged, never passed to output.
func (c *KubeCtlAPIActions) HostExecuteAndStreamNoKeepalive(hostString string, output cli.OutputHandler, args ...string) error {
	logArgs(false, args)
	containerName, err := c.getPrimaryContainer(hostString)
	if err != nil {
		return fmt.Errorf("failed looking for pod %v: %w", hostString, err)
	}
	script := strings.Join(args, " ") + "; printf '\\n" + eosMarker + "%d\\n' $?"
	req := c.client.CoreV1().RESTClient().Post().Resource("pods").Name(hostString).
		Namespace(c.namespace).SubResource("exec")
	req = req.VersionedParams(&v1.PodExecOptions{
		Container: containerName,
		Command:   []string{"sh", "-c", script},
		Stdout:    true,
		Stderr:    true,
	}, scheme.ParameterCodec)
	executor, err := c.newStreamExecutor("POST", req.URL())
	if err != nil {
		return err
	}
	stdout := &eosLineWriter{output: output}
	stderr := &K8SWriter{Output: func(line string) {
		simplelog.Warningf("%v stderr: %v", hostString, line)
	}}
	err = executor.StreamWithContext(c.hook.GetContext(), remotecommand.StreamOptions{
		Stdout: stdout,
		Stderr: stderr,
	})
	stderr.Flush()
	complete := stdout.finish() // always runs: delivers the last complete line
	if err != nil {
		return err
	}
	return complete
}

// eosLineWriter splits a stream into lines for output like K8SWriter, but holds
// back one complete line so it can drop the empty line created by the marker's
// leading newline, strips the marker line, and records the exit code it carries.
type eosLineWriter struct {
	output  cli.OutputHandler
	partial strings.Builder
	held    *string
	sawEOS  bool
	rc      int
}

func (w *eosLineWriter) Write(p []byte) (int, error) {
	data := string(p)
	for {
		idx := strings.IndexByte(data, '\n')
		if idx < 0 {
			w.partial.WriteString(data)
			return len(p), nil
		}
		w.partial.WriteString(data[:idx])
		w.line(w.partial.String())
		w.partial.Reset()
		data = data[idx+1:]
	}
}

func (w *eosLineWriter) line(l string) {
	if w.sawEOS {
		return // nothing is expected after the marker
	}
	if rest, ok := strings.CutPrefix(l, eosMarker); ok {
		if rc, err := strconv.Atoi(rest); err == nil {
			w.sawEOS, w.rc = true, rc
			if w.held != nil && *w.held != "" {
				w.output(*w.held)
			}
			w.held = nil
			return
		}
	}
	if w.held != nil {
		w.output(*w.held)
	}
	w.held = &l
}

// finish must be called once after the stream ends. Without the marker it
// delivers the held-back line, drops any trailing partial line (it may be a
// cut-off record) and returns collection.ErrStreamIncomplete.
func (w *eosLineWriter) finish() error {
	if !w.sawEOS {
		if w.held != nil {
			w.output(*w.held)
			w.held = nil
		}
		w.partial.Reset()
		return collection.ErrStreamIncomplete
	}
	if w.rc != 0 {
		return fmt.Errorf("remote command exited with code %d", w.rc)
	}
	return nil
}
