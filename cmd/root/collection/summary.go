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
	"encoding/json"
	"fmt"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/cmd/root/helpers"
)

type SummaryInfo struct {
	ClusterInfo         ClusterInfo             `json:"clusterInfo"`
	CollectedFiles      []helpers.CollectedFile `json:"collectedFiles"`
	FailedFiles         []string                `json:"failedFiles"`
	SkippedFiles        []string                `json:"skippedFiles"`
	StartTimeUTC        time.Time               `json:"startTimeUTC"`
	EndTimeUTC          time.Time               `json:"endTimeUTC"`
	TotalRuntimeSeconds int64                   `json:"totalRuntimeSeconds"`
	TotalBytesCollected int64                   `json:"totalBytesCollected"`
	Executors           []string                `json:"executors"`
	Coordinators        []string                `json:"coordinators"`
	DremioVersion       map[string]string       `json:"dremioVersion"`
	ClusterID           map[string]string       `json:"clusterID"`
	DDCVersion          string                  `json:"ddcVersion"`
}

type ClusterInfo struct {
	NumberNodesContacted int `json:"numberNodesContacted"`
	TotalNodesAttempted  int `json:"totalNodesAttempted"`
}

type SummaryInfoWriterError struct {
	SummaryInfo SummaryInfo
	Err         error
}

func (w SummaryInfoWriterError) Error() string {
	return fmt.Sprintf("This is a bug, unable to write summary %#v due to error %v", w.SummaryInfo, w.Err)
}

func (summary SummaryInfo) String() (string, error) {
	b, err := json.MarshalIndent(summary, "", "\t")
	if err != nil {
		return "", SummaryInfoWriterError{
			SummaryInfo: summary,
			Err:         err,
		}
	}
	return string(b), nil
}
