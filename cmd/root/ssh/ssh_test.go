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

// package ssh uses ssh and scp binaries to execute commands remotely and translate the results back to the calling node
package ssh

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/pkg/tests"
)

func TestSSHExec(t *testing.T) {
	hostName := "pod"
	cli := &tests.MockCli{
		StoredResponse: []string{"Warning key not already added\nsuccess"},
		StoredErrors:   []error{nil},
	}
	sshUser := "root"
	k := &CmdSSHActions{
		cli:     cli,
		sshKey:  "id_rsa",
		sshUser: sshUser,
	}
	out, err := k.HostExecute(hostName, true, "ls", "-l")
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}

	if out != "success" {
		t.Errorf("expected success but got %v", out)
	}
	calls := cli.Calls
	if len(calls) != 1 {
		t.Errorf("expected 1 call but got %v", len(calls))
	}
	expectedCall := []string{"ssh", "-i", "id_rsa", "-o", "UserKnownHostsFile=/dev/null", "-o", "StrictHostKeyChecking=no", fmt.Sprintf("%v@%v", sshUser, hostName), "ls -l"}
	if !reflect.DeepEqual(calls[0], expectedCall) {
		t.Errorf("expected %v call but got %v", expectedCall, calls[0])
	}
}

func TestSCP(t *testing.T) {
	hostName := "pod"
	source := "/podroot/test.log"
	destination := "/mydir/test.log"
	cli := &tests.MockCli{
		StoredResponse: []string{"success"},
		StoredErrors:   []error{nil},
	}
	sshUser := "root"
	k := &CmdSSHActions{
		cli:     cli,
		sshKey:  "id_rsa",
		sshUser: sshUser,
	}
	out, err := k.CopyFromHost(hostName, false, source, destination)
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}

	if out != "success" {
		t.Errorf("expected success but got %v", out)
	}
	calls := cli.Calls
	if len(calls) != 1 {
		t.Errorf("expected 1 call but got %v", len(calls))
	}
	expectedCall := []string{"scp", "-i", "id_rsa", "-o", "UserKnownHostsFile=/dev/null", "-o", "StrictHostKeyChecking=no", fmt.Sprintf("%v@%v:%v", sshUser, hostName, source), destination}
	if !reflect.DeepEqual(calls[0], expectedCall) {
		t.Errorf("expected %v call but got %v", expectedCall, calls[0])
	}
}
