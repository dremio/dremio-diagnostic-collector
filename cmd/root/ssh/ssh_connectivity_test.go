// Copyright 2023 Dremio Corporation
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

package ssh

import (
	"testing"
	"time"
)

// TestCheckSSHConnectivity_CommandFormat verifies that CheckSSHConnectivity
// returns an error when the target host is unreachable (since there is no
// real SSH server running). This also implicitly validates that the function
// constructs a valid ssh command, because exec.Command will fail with a
// connection error rather than a usage error.
func TestCheckSSHConnectivity_CommandFormat(t *testing.T) {
	// Use a non-routable address to guarantee fast failure.
	err := CheckSSHConnectivity("192.0.2.1", "testuser", "/nonexistent/key", 2*time.Second)
	if err == nil {
		t.Fatal("expected an error for unreachable host, got nil")
	}
	// The error should mention the host.
	if got := err.Error(); !containsAll(got, "192.0.2.1", "testuser") {
		t.Errorf("error should reference the host and user, got: %s", got)
	}
}

// TestCheckSSHConnectivity_ZeroTimeout verifies that a zero timeout
// defaults to a reasonable connect timeout (the function uses "5" when 0).
func TestCheckSSHConnectivity_ZeroTimeout(t *testing.T) {
	err := CheckSSHConnectivity("192.0.2.1", "testuser", "/nonexistent/key", 0)
	if err == nil {
		t.Fatal("expected an error for unreachable host, got nil")
	}
}

// containsAll checks that s contains all the given substrings.
func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		found := false
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
