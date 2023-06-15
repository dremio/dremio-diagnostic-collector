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

// ssh package uses ssh and scp binaries to execute commands remotely and translate the results back to the calling node
package ssh

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/dremio/dremio-diagnostic-collector/cmd/root/cli"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
	"github.com/google/uuid"
)

func NewCmdSSHActions(sshKey, sshUser string) *CmdSSHActions {
	return &CmdSSHActions{
		cli:     &cli.Cli{},
		sshKey:  sshKey,
		sshUser: sshUser,
	}
}

// CmdSSHActions depends on the scp and ssh programs being present and
// then assumes ssh public key auth is in place since it has no support for using
// password based authentication
type CmdSSHActions struct {
	cli     cli.CmdExecutor
	sshKey  string
	sshUser string
}

var tmpDir string

func (c *CmdSSHActions) HostExecuteAndStream(hostString string, output cli.OutputHandler, _ bool, args ...string) (err error) {
	sshArgs := []string{"ssh", "-i", c.sshKey, "-o", "UserKnownHostsFile=/dev/null", "-o", "StrictHostKeyChecking=no"}
	sshArgs = append(sshArgs, fmt.Sprintf("%v@%v", c.sshUser, hostString))
	sshArgs = append(sshArgs, strings.Join(args, " "))
	return c.cli.ExecuteAndStreamOutput(output, sshArgs...)
}

func (c *CmdSSHActions) CopyFromHost(hostName string, _ bool, source, destination string) (string, error) {
	return c.cli.Execute("scp", "-i", c.sshKey, "-o", "UserKnownHostsFile=/dev/null", "-o", "StrictHostKeyChecking=no", fmt.Sprintf("%v@%v:%v", c.sshUser, hostName, source), destination)
}

func (c *CmdSSHActions) CopyFromHostSudo(hostName string, _ bool, sudoUser, source, destination string) (string, error) {
	var args []string
	sourceFile := filepath.Base(source)
	if !strings.Contains(tmpDir, hostName) {
		tmpDir = path.Join("/tmp/", hostName, uuid.NewString())
		args = []string{"mkdir", "-p", tmpDir}
		c.HostExecuteSudo(hostName, sudoUser, args...)
	}
	tmpSourceFile := path.Join(tmpDir, sourceFile)
	simplelog.Infof("testing: %v", tmpSourceFile)
	// first move to tmp dir from source as sudo
	args = []string{"cp", source, tmpSourceFile}
	simplelog.Infof("testing: %v", args)
	c.HostExecuteSudo(hostName, sudoUser, args...)
	// next copy from tmp dir as non-sudo
	out, err := c.cli.Execute("scp", "-i", c.sshKey, "-o", "UserKnownHostsFile=/dev/null", "-o", "StrictHostKeyChecking=no", tmpSourceFile, fmt.Sprintf("%v@%v:%v", c.sshUser, hostName, destination))
	if err != nil {
		return out, err
	}
	return c.cli.Execute("scp", "-i", c.sshKey, "-o", "UserKnownHostsFile=/dev/null", "-o", "StrictHostKeyChecking=no", fmt.Sprintf("%v@%v:%v", c.sshUser, hostName, tmpDir), destination)
}

func (c *CmdSSHActions) CopyToHost(hostName string, _ bool, source, destination string) (string, error) {
	return c.cli.Execute("scp", "-i", c.sshKey, "-o", "UserKnownHostsFile=/dev/null", "-o", "StrictHostKeyChecking=no", source, fmt.Sprintf("%v@%v:%v", c.sshUser, hostName, destination))
}

func (c *CmdSSHActions) CopyToHostSudo(hostName string, _ bool, sudoUser, source, destination string) (string, error) {
	var args []string
	sourceFile := filepath.Base(source)
	if !strings.Contains(tmpDir, hostName) {
		tmpDir = path.Join("/tmp/", hostName, uuid.NewString())
		args = []string{"mkdir", "-p", tmpDir}
		c.HostExecute(hostName, false, args...)
	}
	tmpDestFile := path.Join(tmpDir, sourceFile)
	simplelog.Infof("testing: %v", tmpDestFile)
	// first copy to tmp dir as non-sudo
	out, err := c.cli.Execute("scp", "-i", c.sshKey, "-o", "UserKnownHostsFile=/dev/null", "-o", "StrictHostKeyChecking=no", source, fmt.Sprintf("%v@%v:%v", c.sshUser, hostName, tmpDir))
	if err != nil {
		return out, err
	}
	// next move from tmp dir to destination as sudo
	args = []string{"cp", tmpDestFile, destination}
	simplelog.Infof("testing: %v", args)
	return c.HostExecuteSudo(hostName, sudoUser, args...)
}

func (c *CmdSSHActions) HostExecute(hostName string, _ bool, args ...string) (string, error) {
	simplelog.Infof("testing: %v", c.sshUser)
	sshArgs := []string{"ssh", "-i", c.sshKey, "-o", "UserKnownHostsFile=/dev/null", "-o", "StrictHostKeyChecking=no"}
	sshArgs = append(sshArgs, fmt.Sprintf("%v@%v", c.sshUser, hostName))
	sshArgs = append(sshArgs, strings.Join(args, " "))
	return c.cli.Execute(sshArgs...)
}

func (c *CmdSSHActions) HostExecuteSudo(hostName string, sudoUser string, args ...string) (string, error) {
	sudoArgs := []string{"sudo", "-u", sudoUser}
	sshArgs := []string{"ssh", "-i", c.sshKey, "-o", "UserKnownHostsFile=/dev/null", "-o", "StrictHostKeyChecking=no"}
	sshArgs = append(sshArgs, fmt.Sprintf("%v@%v", c.sshUser, hostName))
	sshArgs = append(sshArgs, strings.Join(sudoArgs, " "))
	sshArgs = append(sshArgs, strings.Join(args, " "))
	return c.cli.Execute(sshArgs...)
}

func (c *CmdSSHActions) FindHosts(searchTerm string) (hosts []string, err error) {
	rawHosts := strings.Split(searchTerm, ",")
	for _, host := range rawHosts {
		if host == "" {
			continue
		}
		hosts = append(hosts, strings.TrimSpace(host))
	}
	return hosts, nil
}

func (c *CmdSSHActions) HelpText() string {
	return "no hosts found did you specify a comma separated list for the ssh-hosts? Something like: ddc --coordinator 192.168.1.10,192.168.1.11 --excecutors 192.168.1.14,192.168.1.15"
}
