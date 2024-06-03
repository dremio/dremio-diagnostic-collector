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

// packag fallback is only used when we are unable to collect with --detect namespace
package fallback

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/dremio/dremio-diagnostic-collector/cmd/root/cli"
	"github.com/dremio/dremio-diagnostic-collector/pkg/shutdown"
)

type Fallback struct {
	cli cli.CmdExecutor
}

func NewFallback(hook shutdown.CancelHook) *Fallback {
	return &Fallback{
		cli: cli.NewCli(hook),
	}
}

func (c *Fallback) SetHostPid(_, _ string) {
	// not needed as normal cancellation will work
}
func (c *Fallback) CleanupRemote() error {
	return nil
}

func (c *Fallback) Name() string {
	return "Local Collect"
}

func (c *Fallback) HelpText() string {
	return "this occurs when k8s namespace detection is requested and the rights are not present"
}

func (c *Fallback) HostExecuteAndStream(mask bool, _ string, output cli.OutputHandler, pat string, args ...string) (err error) {
	return c.cli.ExecuteAndStreamOutput(mask, output, pat, args...)
}

func (c *Fallback) HostExecute(mask bool, _ string, args ...string) (string, error) {
	var out strings.Builder
	writer := func(line string) {
		out.WriteString(line)
	}
	err := c.HostExecuteAndStream(mask, "", writer, "", args...)
	return out.String(), err
}

func (c *Fallback) CopyFromHost(_ string, source, destination string) (out string, err error) {
	src, err := os.Open(filepath.Clean(source))
	if err != nil {
		return "", err
	}
	defer src.Close()
	dst, err := os.Create(filepath.Clean(destination))
	if err != nil {
		return "", err
	}
	defer dst.Close()
	_, err = io.Copy(dst, src)
	return "", err
}

func (c *Fallback) CopyToHost(_ string, source, destination string) (out string, err error) {
	src, err := os.Open(filepath.Clean(source))
	if err != nil {
		return "", err
	}
	defer src.Close()
	dst, err := os.Create(filepath.Clean(destination))
	if err != nil {
		return "", err
	}
	defer dst.Close()
	_, err = io.Copy(dst, src)
	return "", err
}

func (c *Fallback) GetCoordinators() (podName []string, err error) {
	host, err := os.Hostname()
	if err != nil {
		return []string{}, err
	}
	return []string{host}, nil
}

func (c *Fallback) GetExecutors() (podName []string, err error) {
	return []string{}, nil
}
