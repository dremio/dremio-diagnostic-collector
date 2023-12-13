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

// package cli provides wrapper support for executing commands, this is so
// we can test the rest of the implementations quickly.
package cli

import (
	"bufio"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/dremio/dremio-diagnostic-collector/pkg/masking"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
)

type CmdExecutor interface {
	Execute(mask bool, args ...string) (out string, err error)
	ExecuteAndStreamOutput(mask bool, outputHandler OutputHandler, args ...string) error
}

type UnableToStartErr struct {
	Err error
	Cmd string
}

func (u UnableToStartErr) Error() string {
	return fmt.Sprintf("cmd '%v' failed: '%v'", u.Cmd, u.Err)
}

type ExecuteCliErr struct {
	Err error
	Cmd string
}

// OutputHandler is a function type that processes lines of output
type OutputHandler func(line string)

// Cli
type Cli struct {
	m sync.Mutex
}

// ExecuteAndStreamOutput runs a system command and streams the output (stdout)
// and errors (stderr) to the provided output handler function.
// This function will run the command specified by the args parameters.
// The first arg should be the command itself, and the rest of the args should be its parameters.
// The outputHandler is a callback function that is called with each line of output and error from the command.
// If the command runs successfully, the function will return nil. If there's an error executing the command,
// it will return an error. Note that an error from the command itself (e.g., a non-zero exit status) will also
// be returned as an error from this function.
func (c *Cli) ExecuteAndStreamOutput(mask bool, outputHandler OutputHandler, args ...string) error {
	// Log the command that's about to be run
	logArgs(mask, args)

	// Create the command based on the passed arguments
	cmd := exec.Command(args[0], args[1:]...)

	// Create a pipe to get the standard output from the command
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return UnableToStartErr{Err: err, Cmd: strings.Join(args, " ")}
	}
	stdOutScanner := bufio.NewScanner(stdout)

	// Create a pipe to get the error output from the command
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return UnableToStartErr{Err: err, Cmd: strings.Join(args, " ")}
	}
	stdErrScanner := bufio.NewScanner(stderr)

	// Start the command
	if err := cmd.Start(); err != nil {
		return UnableToStartErr{Err: err, Cmd: strings.Join(args, " ")}
	}
	var wg sync.WaitGroup
	wg.Add(2)

	// Asynchronously read the output from the command line by line
	// and pass it to the outputHandler. This runs in a goroutine
	// so that we can also read the error output at the same time.
	go func() {
		for stdOutScanner.Scan() {
			c.m.Lock()
			outputHandler(stdOutScanner.Text())
			c.m.Unlock()
		}
		wg.Done()
	}()

	// Asynchronously read the error output from the command line by line
	// and pass it to the outputHandler.
	go func() {
		for stdErrScanner.Scan() {
			c.m.Lock()
			outputHandler(stdErrScanner.Text())
			c.m.Unlock()
		}
		wg.Done()
	}()

	//wait for the wait group too so that we can finish writing the text
	wg.Wait()
	// Wait for the command to finish apparently should be called AFTER the capturing is done.
	//this seems counterintuitive to me but we will go with it
	if err := cmd.Wait(); err != nil {
		return UnableToStartErr{Err: err, Cmd: strings.Join(args, " ")}
	}

	// If there was no error, return nil
	return nil
}

func (c *Cli) Execute(mask bool, args ...string) (string, error) {
	// Log the command that's about to be run
	logArgs(mask, args)
	cmd := exec.Command(args[0], args[1:]...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), UnableToStartErr{Err: err, Cmd: strings.Join(args, " ")}
	}
	return string(output), nil
}

func logArgs(mask bool, args []string) {
	// log out args, mask if needed
	if mask {
		maskedOutput := masking.MaskPAT(strings.Join(args, " "))
		simplelog.Infof("args: %v", maskedOutput)
	} else {
		simplelog.Infof("args: %v", strings.Join(args, " "))
	}
}

func (c *Cli) ExecuteBytes(mask bool, args ...string) ([]byte, error) {
	logArgs(mask, args)
	cmd := exec.Command(args[0], args[1:]...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, UnableToStartErr{Err: err, Cmd: strings.Join(args, " ")}
	}
	return output, nil
}
