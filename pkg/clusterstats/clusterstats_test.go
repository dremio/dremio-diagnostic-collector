package clusterstats

import (
	"encoding/json"
	"testing"
)

func TestClusterStatsJSONRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		input ClusterStats
	}{
		{
			name:  "all fields populated",
			input: ClusterStats{DremioVersion: "25.0.0", ClusterID: "abc-123", NodeName: "node-0"},
		},
		{
			name:  "empty fields",
			input: ClusterStats{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.input)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}
			var got ClusterStats
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}
			if got != tc.input {
				t.Errorf("round-trip mismatch: got %+v, want %+v", got, tc.input)
			}
		})
	}
}

func TestClusterStatsJSONKeys(t *testing.T) {
	cs := ClusterStats{DremioVersion: "v1", ClusterID: "id1", NodeName: "n1"}
	data, err := json.Marshal(cs)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal to map failed: %v", err)
	}
	expectedKeys := []string{"dremioVersion", "clusterID", "nodeName"}
	for _, key := range expectedKeys {
		if _, ok := m[key]; !ok {
			t.Errorf("expected JSON key %q not found in %s", key, string(data))
		}
	}
}
