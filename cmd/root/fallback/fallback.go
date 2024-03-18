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
	"github.com/dremio/dremio-diagnostic-collector/cmd/root/cli"
	"os"
	"strings"
)

type Fallback struct {
	cli cli.CmdExecutor
}

func NewFallback() *Fallback {
	return &Fallback{
		cli: &cli.Cli{},
	}
}

func (c *Fallback) Name() string {
	return "Local Collect"
}

func (c *Fallback) HelpText() string {
	return "this occurs when k8s namespace detection is requested and the rights are not present"
}

func (c *Fallback) HostExecuteAndStream(mask bool, _ string, output cli.OutputHandler, args ...string) (err error) {
	return c.cli.ExecuteAndStreamOutput(mask, output, args...)
}

func (c *Fallback) HostExecute(mask bool, _ string, args ...string) (string, error) {
	var out strings.Builder
	writer := func(line string) {
		out.WriteString(line)
	}
	err := c.HostExecuteAndStream(mask, "", writer, args...)
	return out.String(), err
}

func (c *Fallback) CopyFromHost(_ string, source, destination string) (out string, err error) {
	return "", os.Rename(source, destination)
}

func (c *Fallback) CopyToHost(_ string, source, destination string) (out string, err error) {
	return "", os.Rename(source, destination)
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
