package versions

import "testing"

func TestGetCLIVersion(t *testing.T) {
	tests := []struct {
		name     string
		version  string
		gitSha   string
		expected string
	}{
		{name: "without GitSha", version: "4.0.0-alpha2", gitSha: "", expected: "ddc 4.0.0-alpha2"},
		{name: "with GitSha", version: "4.0.0-alpha2", gitSha: "abc123", expected: "ddc 4.0.0-alpha2-abc123"},
		{name: "release version", version: "4.0.0", gitSha: "deadbeef", expected: "ddc 4.0.0-deadbeef"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			origVersion := Version
			origSha := GitSha
			t.Cleanup(func() {
				Version = origVersion
				GitSha = origSha
			})

			Version = tc.version
			GitSha = tc.gitSha
			got := GetCLIVersion()
			if got != tc.expected {
				t.Errorf("GetCLIVersion() = %q, want %q", got, tc.expected)
			}
		})
	}
}
