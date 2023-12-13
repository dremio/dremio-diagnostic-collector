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
	"bufio"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/dremio/dremio-diagnostic-collector/cmd/root/cli"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
	"github.com/google/uuid"
)

type Args struct {
	SSHKeyLoc string
	SSHUser   string
}

func NewCmdSSHActions(sshArgs Args) *CmdSSHActions {
	return &CmdSSHActions{
		cli:     &cli.Cli{},
		sshKey:  sshArgs.SSHKeyLoc,
		sshUser: sshArgs.SSHUser,
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

func (c *CmdSSHActions) Name() string {
	return "SSH/SCP"
}

func (c *CmdSSHActions) HostExecuteAndStream(mask bool, hostString string, output cli.OutputHandler, _ bool, args ...string) (err error) {
	sshArgs := []string{"ssh", "-i", c.sshKey, "-o", "LogLevel=error", "-o", "UserKnownHostsFile=/dev/null", "-o", "StrictHostKeyChecking=no"}
	sshArgs = append(sshArgs, fmt.Sprintf("%v@%v", c.sshUser, hostString))
	sshArgs = append(sshArgs, strings.Join(args, " "))
	return c.cli.ExecuteAndStreamOutput(mask, output, sshArgs...)
}

func (c *CmdSSHActions) CopyFromHost(hostName string, _ bool, source, destination string) (string, error) {
	return c.cli.Execute(false, "scp", "-i", c.sshKey, "-o", "LogLevel=error", "-o", "UserKnownHostsFile=/dev/null", "-o", "StrictHostKeyChecking=no", fmt.Sprintf("%v@%v:%v", c.sshUser, hostName, source), destination)
}

func (c *CmdSSHActions) CopyFromHostSudo(hostName string, _ bool, sudoUser, source, destination string) (string, error) {
	sourceFileName := filepath.Base(source)
	// create a tmp dir for scp
	tmpDir := path.Join("/tmp/", "ddc-scp-"+uuid.New().String())
	out, err := c.HostExecute(false, hostName, false, "mkdir", "-p", tmpDir)
	if err != nil {
		return out, err
	}

	// cleanup the tmp dir
	defer func() {
		_, err = c.HostExecute(false, hostName, false, "rm", "-rf", tmpDir)
		if err != nil {
			simplelog.Errorf("host %v unable to remove tmp dir %v", hostName, tmpDir)
		}
	}()
	// first move to tmp dir from source as sudo
	tmpFilePath := path.Join(tmpDir, sourceFileName)
	out, err = c.HostExecuteSudo(false, hostName, sudoUser, "cp", source, tmpFilePath)
	if err != nil {
		return out, err
	}
	// next copy from tmp dir as non-sudo
	return c.cli.Execute(false, "scp", "-i", c.sshKey, "-o", "LogLevel=error", "-o", "UserKnownHostsFile=/dev/null", "-o", "StrictHostKeyChecking=no", tmpFilePath, fmt.Sprintf("%v@%v:%v", c.sshUser, hostName, destination))
}

func (c *CmdSSHActions) CopyToHost(hostName string, _ bool, source, destination string) (string, error) {
	return c.cli.Execute(false, "scp", "-i", c.sshKey, "-o", "LogLevel=error", "-o", "UserKnownHostsFile=/dev/null", "-o", "StrictHostKeyChecking=no", source, fmt.Sprintf("%v@%v:%v", c.sshUser, hostName, destination))
}

func (c *CmdSSHActions) CopyToHostSudo(hostName string, _ bool, sudoUser, source, destination string) (string, error) {
	sourceFileName := filepath.Base(source)
	// create a tmp dir for scp
	tmpDir := path.Join("/tmp/", "ddc-scp-"+uuid.New().String())
	out, err := c.HostExecute(false, hostName, false, "mkdir", "-p", tmpDir)
	if err != nil {
		return out, err
	}
	// cleanup the tmp dir
	defer func() {
		_, err = c.HostExecute(false, hostName, false, "rm", "-rf", tmpDir)
		if err != nil {
			simplelog.Errorf("host %v unable to remove tmp dir %v", hostName, tmpDir)
		}
	}()
	tmpFilePath := path.Join(tmpDir, sourceFileName)
	// first copy to tmp dir as non-sudo

	out, err = c.cli.Execute(false, "scp", "-i", c.sshKey, "-o", "LogLevel=error", "-o", "UserKnownHostsFile=/dev/null", "-o", "StrictHostKeyChecking=no", source, fmt.Sprintf("%v@%v:%v", c.sshUser, hostName, tmpFilePath))
	if err != nil {
		return out, err
	}

	// chmod dir for sudo user to be able to read it even if the two users cannot
	out, err = c.HostExecute(false, hostName, false, "chmod", "777", "-R", tmpDir)
	if err != nil {
		return out, err
	}

	// next move from tmp dir to destination as sudo
	return c.HostExecuteSudo(false, hostName, sudoUser, "cp", tmpFilePath, destination)
}

func (c *CmdSSHActions) HostExecute(mask bool, hostName string, _ bool, args ...string) (string, error) {
	sshArgs := []string{"ssh", "-i", c.sshKey, "-o", "LogLevel=error", "-o", "UserKnownHostsFile=/dev/null", "-o", "StrictHostKeyChecking=no"}
	sshArgs = append(sshArgs, fmt.Sprintf("%v@%v", c.sshUser, hostName))
	sshArgs = append(sshArgs, strings.Join(args, " "))
	out, err := c.cli.Execute(mask, sshArgs...)
	if err != nil {
		return out, err
	}
	//return CleanOut(out), nil
	return out, nil
}

func (c *CmdSSHActions) HostExecuteSudo(mask bool, hostName string, sudoUser string, args ...string) (string, error) {
	sudoArgs := []string{"sudo", "-u", sudoUser}
	sshArgs := []string{"ssh", "-i", c.sshKey, "-o", "LogLevel=error", "-o", "UserKnownHostsFile=/dev/null", "-o", "StrictHostKeyChecking=no"}
	sshArgs = append(sshArgs, fmt.Sprintf("%v@%v", c.sshUser, hostName))
	sshArgs = append(sshArgs, strings.Join(sudoArgs, " "))
	sshArgs = append(sshArgs, strings.Join(args, " "))
	out, err := c.cli.Execute(mask, sshArgs...)
	if err != nil {
		return out, err
	}
	//return CleanOut(out), nil
	return out, nil

}

func CleanOut(out string) string {
	//we expect there it be a warning with ssh that we will clean here
	// Create a scanner to split the output into lines
	scanner := bufio.NewScanner(strings.NewReader(out))

	var lines []string
	var counter int
	// Iterate over each line but skip the first one due to the Warning which is always present when using ssh
	for scanner.Scan() {
		if counter > 0 {
			lines = append(lines, scanner.Text())
		}
		counter++
	}
	cleanedOut := strings.Join(lines, "\n")
	return cleanedOut
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
