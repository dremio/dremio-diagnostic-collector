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

// collection package provides the interface for collection implementation and the actual collection execution
package collection

import (
	"fmt"
	"strings"

	"github.com/dremio/dremio-diagnostic-collector/cmd/root/cli"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
)

// Adds the sudo part into the HostExecute call
func ComposeExecuteAndStream(mask bool, conf HostCaptureConfiguration, output cli.OutputHandler, command []string) error {
	host := conf.Host
	c := conf.Collector
	isCoordinator := conf.IsCoordinator
	sudoUser := conf.SudoUser
	var err error

	if sudoUser == "" {
		err = c.HostExecuteAndStream(mask, host, output, isCoordinator, command...)
		if err != nil {
			cmdString := strings.Join(command, " ")
			simplelog.Errorf("host %v failed to run command %v with error '%v'", host, cmdString, err)
		}
	} else {
		sudoCommand := append([]string{"sudo", "-u", sudoUser}, command...)
		err = c.HostExecuteAndStream(mask, host, output, isCoordinator, sudoCommand...)
		if err != nil {
			cmdString := strings.Join(command, " ")
			simplelog.Errorf("host %v failed to run sudo command %v with error '%v'", host, cmdString, err)
		}
	}
	return err
}

// Adds the sudo part into the HostExecute call
func ComposeExecute(mask bool, conf HostCaptureConfiguration, command []string) (string, error) {
	host := conf.Host
	c := conf.Collector
	isCoordinator := conf.IsCoordinator
	sudoUser := conf.SudoUser

	if sudoUser == "" {
		stdOut, err := c.HostExecute(mask, host, isCoordinator, command...)
		if err != nil {
			cmdString := strings.Join(command, " ")
			return "", fmt.Errorf("host %v failed to run command %v with error %v: output was: '%v'", host, cmdString, err, stdOut)
		}
		return stdOut, nil
	}
	sudoCommand := append([]string{"sudo", "-u", sudoUser}, command...)
	stdOut, err := c.HostExecute(mask, host, isCoordinator, sudoCommand...)
	if err != nil {
		cmdString := strings.Join(command, " ")
		return "", fmt.Errorf("host %v failed to run sudo command %v with error %v; output was '%v'", host, cmdString, err, stdOut)
	}
	return stdOut, nil
}

// Some execute actions should never change regardless of the sudo user being passed or not
func ComposeExecuteNoSudo(mask bool, conf HostCaptureConfiguration, command []string) (stdOut string, err error) {
	host := conf.Host
	c := conf.Collector
	isCoordinator := conf.IsCoordinator
	stdOut, err = c.HostExecute(mask, host, isCoordinator, command...)
	if err != nil {
		cmdString := strings.Join(command, " ")
		simplelog.Errorf("host %v failed to run command %v with error %v; output was '%v'", host, cmdString, err, stdOut)
	}

	return stdOut, err
}

// Adds the sudo part into the CopyFromHost call
func ComposeCopy(conf HostCaptureConfiguration, source, destination string) (stdOut string, err error) {
	host := conf.Host
	c := conf.Collector
	isCoordinator := conf.IsCoordinator
	sudoUser := conf.SudoUser

	if sudoUser == "" {
		stdOut, err = c.CopyFromHost(host, isCoordinator, source, destination)
		if err != nil {
			simplelog.Errorf("failed to copy from %v:%v with error %v; output was '%v'", host, destination, err, stdOut)
		}
	} else {
		stdOut, err = c.CopyFromHostSudo(host, isCoordinator, sudoUser, source, destination)
		if err != nil {
			simplelog.Errorf("failed to sudo copy from %v%v with error %v; output was '%v'", host, destination, err, stdOut)
		}
	}
	return stdOut, err
}

// Some copy back actions should never change regardless of the sudo user being passed or not
func ComposeCopyNoSudo(conf HostCaptureConfiguration, source, destination string) (stdOut string, err error) {
	host := conf.Host
	c := conf.Collector
	isCoordinator := conf.IsCoordinator

	stdOut, err = c.CopyFromHost(host, isCoordinator, source, destination)
	if err != nil {
		simplelog.Errorf("failed to sudo copy from %v:%v with error %v; output was '%v'", host, destination, err, stdOut)
	}

	return stdOut, err
}

// Adds the sudo part into the CopyFromHost call
func ComposeCopyTo(conf HostCaptureConfiguration, source, destination string) (stdOut string, err error) {
	host := conf.Host
	c := conf.Collector
	isCoordinator := conf.IsCoordinator
	sudoUser := conf.SudoUser

	if sudoUser == "" {
		stdOut, err = c.CopyToHost(host, isCoordinator, source, destination)
		if err != nil {
			simplelog.Errorf("failed to copy to %v:%v with error %v; output was '%v'", host, destination, err, stdOut)
		}
	} else {
		stdOut, err = c.CopyToHostSudo(host, isCoordinator, sudoUser, source, destination)
		if err != nil {
			simplelog.Errorf("failed to sudo copy to %v:%v with error %v; output '%v'", host, destination, err, stdOut)
		}
	}
	return stdOut, err
}
