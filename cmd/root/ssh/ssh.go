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
	"io"
	"os/exec"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/cli"
	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/collection"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/consoleprint"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/shutdown"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/simplelog"
	"github.com/google/uuid"
)

type Args struct {
	SSHKeyLoc      string
	SSHUser        string
	SudoUser       string
	ExecutorStr    string
	CoordinatorStr string
}

func NewCmdSSHActions(sshArgs Args, hook shutdown.Hook) *CmdSSHActions {
	uuid.EnableRandPool()
	return &CmdSSHActions{
		hook:           hook,
		cli:            cli.NewCli(hook),
		sshKey:         sshArgs.SSHKeyLoc,
		sshUser:        sshArgs.SSHUser,
		sudoUser:       sshArgs.SudoUser,
		executorStr:    sshArgs.ExecutorStr,
		coordinatorStr: sshArgs.CoordinatorStr,
		pidHosts:       make(map[string]string),
	}
}

// CmdSSHActions depends on the scp and ssh programs being present and
// then assumes ssh public key auth is in place since it has no support for using
// password based authentication
type CmdSSHActions struct {
	cli            cli.CmdExecutor
	sshKey         string
	sshUser        string
	sudoUser       string
	executorStr    string
	coordinatorStr string
	pidHosts       map[string]string
	strictHostKeys bool
	m              sync.Mutex
	hook           shutdown.Hook
}

func (c *CmdSSHActions) Name() string {
	return "SSH/SCP"
}

func (c *CmdSSHActions) Protocol() string {
	return "SSH"
}

func (c *CmdSSHActions) SetHostPid(host, pidFile string) {
	c.m.Lock()
	c.pidHosts[host] = pidFile
	c.m.Unlock()
}

func (c *CmdSSHActions) CleanupRemote() error {
	kill := func(host string, pidFile string) {
		if pidFile == "" {
			simplelog.Debugf("pidfile is blank for %v skipping", host)
			return
		}
		sshArgs := c.baseSSHArgs()
		sshArgs = append(sshArgs, fmt.Sprintf("%v@%v", c.sshUser, host))
		sshArgs = c.addSSHUser(sshArgs)
		sshArgs = append(sshArgs, "cat")
		sshArgs = append(sshArgs, pidFile)
		out, err := c.cli.Execute(false, sshArgs...)
		if err != nil {
			simplelog.Warningf("output of pidfile failed for host %v: %v", host, err)
			return
		}
		out = strings.TrimSpace(out)
		if matched, _ := regexp.MatchString(`^\d+$`, out); !matched {
			simplelog.Warningf("invalid PID %q from pidfile on host %v, skipping kill", out, host)
			return
		}
		sshArgs = c.baseSSHArgs()
		sshArgs = append(sshArgs, fmt.Sprintf("%v@%v", c.sshUser, host))
		sshArgs = c.addSSHUser(sshArgs)
		sshArgs = append(sshArgs, "kill")
		sshArgs = append(sshArgs, "-15")
		sshArgs = append(sshArgs, out)
		out, err = c.cli.Execute(false, sshArgs...)
		if err != nil {
			simplelog.Warningf("failed killing process %v host %v: %v", out, host, err)
			return
		}
		consoleprint.UpdateNodeState(consoleprint.NodeState{
			Node:     host,
			Status:   consoleprint.Starting,
			StatusUX: "FAILED - CANCELLED",
			Result:   consoleprint.ResultFailure,
		})
		c.m.Lock()
		// cancel out so we can skip if it's called again
		c.pidHosts[host] = ""
		c.m.Unlock()
	}
	var waitGroup sync.WaitGroup
	var criticalErrors []string
	coordinators, err := c.GetCoordinators()
	if err != nil {
		msg := fmt.Sprintf("unable to get coordinators for cleanup %v", err)
		simplelog.Error(msg)
		criticalErrors = append(criticalErrors, msg)
	} else {
		for _, coordinator := range coordinators {
			c.m.Lock()
			if v, ok := c.pidHosts[coordinator]; ok {
				waitGroup.Add(1)
				go func(host, pid string) {
					defer waitGroup.Done()
					kill(host, pid)
				}(coordinator, v)
			} else {
				simplelog.Errorf("missing key %v in pidHosts skipping host", coordinator)
			}
			c.m.Unlock()
		}
	}

	executors, err := c.GetExecutors()
	if err != nil {
		msg := fmt.Sprintf("unable to get executors for cleanup %v", err)
		simplelog.Error(msg)
		criticalErrors = append(criticalErrors, msg)
	} else {
		for _, executor := range executors {
			c.m.Lock()
			if v, ok := c.pidHosts[executor]; ok {
				waitGroup.Add(1)
				go func(host, pid string) {
					defer waitGroup.Done()
					kill(host, pid)
				}(executor, v)
			} else {
				simplelog.Errorf("missing key %v in pidHosts skipping host", executor)
			}
			c.m.Unlock()
		}
	}
	waitGroup.Wait()
	if len(criticalErrors) > 0 {
		return fmt.Errorf("critical errors trying to cleanup pods %v", strings.Join(criticalErrors, ", "))
	}
	return nil
}

func (c *CmdSSHActions) HostExecuteAndStream(mask bool, hostString string, output cli.OutputHandler, pat string, args ...string) (err error) {
	sshArgs := c.baseSSHArgs()
	sshArgs = append(sshArgs, fmt.Sprintf("%v@%v", c.sshUser, hostString))
	sshArgs = c.addSSHUser(sshArgs)
	sshArgs = append(sshArgs, strings.Join(args, " "))
	return c.cli.ExecuteAndStreamOutput(mask, output, pat, sshArgs...)
}

func (c *CmdSSHActions) CopyToHost(hostName, source, destination string) (string, error) {
	if c.sudoUser == "" {
		scpArgs := c.baseSCPArgs()
		scpArgs = append(scpArgs, source, fmt.Sprintf("%v@%v:%v", c.sshUser, hostName, destination))
		return c.cli.Execute(false, scpArgs...)
	}
	// have to do something more complex in this case and _unfortunately_ copy to the /tmp dir
	tmpFile := fmt.Sprintf("/tmp/%v-%v", path.Base(destination), uuid.New())

	scpArgs := c.baseSCPArgs()
	scpArgs = append(scpArgs, source, fmt.Sprintf("%v@%v:%v", c.sshUser, hostName, tmpFile))
	out, err := c.cli.Execute(false, scpArgs...)
	if err != nil {
		return out, err
	}
	cleanup := func() {
		rmArgs := c.baseSSHArgs()
		rmArgs = append(rmArgs, fmt.Sprintf("%v@%v", c.sshUser, hostName), "rm", tmpFile)
		out, err := c.cli.Execute(false, rmArgs...)
		if err != nil {
			simplelog.Warningf("failed to remove file %v on node %v: %v - %v", tmpFile, hostName, err, out)
		}
	}
	chmodArgs := c.baseSSHArgs()
	chmodArgs = append(chmodArgs, fmt.Sprintf("%v@%v", c.sshUser, hostName), "chmod", "o+r", tmpFile)
	out, err = c.cli.Execute(false, chmodArgs...)
	if err != nil {
		return out, err
	}
	c.hook.AddCancelOnlyTasks(cleanup, fmt.Sprintf("removing ssh transfer %v", tmpFile))
	// now we can move it to it's final destination
	out, err = c.HostExecute(false, hostName, "cp", tmpFile, destination)
	if err != nil {
		simplelog.Infof("removing file %v", tmpFile)
		cleanup()
		return out, err
	}
	simplelog.Infof("removing file %v", tmpFile)
	cleanup()
	return out, err
}

func (c *CmdSSHActions) HostExecute(mask bool, hostName string, args ...string) (string, error) {
	return cli.CollectOutput(c.HostExecuteAndStream, mask, hostName, args...)
}

func (c *CmdSSHActions) addSSHUser(arguments []string) []string {
	if c.sudoUser == "" {
		return arguments
	}
	arguments = append(arguments, "sudo")
	arguments = append(arguments, "-u")
	arguments = append(arguments, c.sudoUser)
	return arguments
}

// commonSSHOpts returns the shared SSH options (log level, host key settings)
// used by both SSH and SCP invocations.
func (c *CmdSSHActions) commonSSHOpts() []string {
	opts := []string{"-o", "LogLevel=error"}
	if !c.strictHostKeys {
		opts = append(opts, "-o", "UserKnownHostsFile=/dev/null", "-o", "StrictHostKeyChecking=no")
	}
	return opts
}

// baseSSHArgs returns the common SSH options used by all SSH invocations,
// including keepalive settings and optional strict host key checking.
func (c *CmdSSHActions) baseSSHArgs() []string {
	args := []string{"ssh", "-i", c.sshKey}
	args = append(args, c.commonSSHOpts()...)
	args = append(args, "-o", "ServerAliveInterval=30", "-o", "ServerAliveCountMax=3")
	return args
}

// baseSCPArgs returns the common SCP options used by all SCP invocations.
func (c *CmdSSHActions) baseSCPArgs() []string {
	args := []string{"scp", "-i", c.sshKey}
	args = append(args, c.commonSSHOpts()...)
	return args
}

func (c *CmdSSHActions) GetExecutors() (hosts []string, err error) {
	return c.findHosts(c.executorStr)
}

func (c *CmdSSHActions) GetCoordinators() (hosts []string, err error) {
	return c.findHosts(c.coordinatorStr)
}

func (c *CmdSSHActions) findHosts(searchTerm string) (hosts []string, err error) {
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

// StreamFromHost streams the raw bytes of a remote file to writer by executing
// "ssh ... <cmd> '<path>'" as a subprocess where <cmd> is "cat" or "gzip -c"
// depending on useGzip. Binary data integrity is preserved — stdout goes
// directly to writer with no line splitting or encoding.
// This bypasses cli.ExecuteAndStreamOutput which is line-oriented.
func (c *CmdSSHActions) StreamFromHost(host, remotePath string, writer io.Writer, useGzip bool) error {
	if remotePath == "" {
		return fmt.Errorf("StreamFromHost: remotePath is empty for host %v", host)
	}

	streamCmd := "cat"
	if useGzip {
		streamCmd = "gzip -c"
	}
	simplelog.Infof("StreamFromHost: streaming %v:%v via SSH (cmd=%s)", host, remotePath, streamCmd)

	sshArgs := c.baseSSHArgs()
	sshArgs = append(sshArgs, fmt.Sprintf("%v@%v", c.sshUser, host))
	sshArgs = c.addSSHUser(sshArgs)

	// Escape single quotes in remotePath to prevent shell injection.
	escapedPath := strings.ReplaceAll(remotePath, "'", "'\\''")
	sshArgs = append(sshArgs, fmt.Sprintf("%s '%s'", streamCmd, escapedPath))

	// sshArgs[0] is "ssh", rest are arguments.
	// #nosec G204 -- arguments are controlled by the caller (CLI flags and discovered paths)
	cmd := exec.Command(sshArgs[0], sshArgs[1:]...)
	cmd.Stdout = writer

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("StreamFromHost: failed to create stderr pipe for %v:%v: %w", host, remotePath, err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("StreamFromHost: failed to start ssh for %v:%v: %w", host, remotePath, err)
	}

	stderrBytes, _ := io.ReadAll(stderrPipe)

	if err := cmd.Wait(); err != nil {
		stderrMsg := strings.TrimSpace(string(stderrBytes))
		if stderrMsg != "" {
			return fmt.Errorf("StreamFromHost: %s failed on %v:%v: %w (stderr: %s)", streamCmd, host, remotePath, err, stderrMsg)
		}
		return fmt.Errorf("StreamFromHost: %s failed on %v:%v: %w", streamCmd, host, remotePath, err)
	}

	simplelog.Infof("StreamFromHost: completed streaming %v:%v", host, remotePath)
	return nil
}

// DiscoverFiles runs remote discovery shell commands on an SSH host to enumerate
// log files, config files, GC logs, and the Dremio PID.
func (c *CmdSSHActions) DiscoverFiles(host, logDir, confDir string) (*collection.RemoteNodeInfo, error) {
	return collection.RunDiscovery(func(h string, args ...string) (string, error) {
		return c.HostExecute(false, h, args...)
	}, host, logDir, confDir)
}

// CheckSSHConnectivity verifies that a host is reachable via SSH before
// collection begins. It runs a lightweight "echo ok" command with a
// connect timeout so unreachable nodes are detected early.
func CheckSSHConnectivity(host, user, keyPath string, timeout time.Duration) error {
	connectTimeout := fmt.Sprintf("%d", int(timeout.Seconds()))
	if connectTimeout == "0" {
		connectTimeout = "5"
	}
	// #nosec G204 -- arguments are controlled by the caller (CLI flags)
	cmd := exec.Command("ssh",
		"-o", fmt.Sprintf("ConnectTimeout=%s", connectTimeout),
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-i", keyPath,
		fmt.Sprintf("%s@%s", user, host),
		"echo", "ok",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ssh connectivity check failed for %s@%s: %w (output: %s)", user, host, err, strings.TrimSpace(string(output)))
	}
	return nil
}
