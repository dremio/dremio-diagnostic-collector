/*
   Copyright 2022 Ryan SVIHLA

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

//summary generates a summary file and writes it to the root of the diagnostic
package summary

import (
	"encoding/json"
	"fmt"
	"time"
)

type CollectionInfo struct {
	ClusterInfo         ClusterInfo     `json:"clusterInfo"`
	CollectedFiles      []CollectedFile `json:"collectedFiles"`
	FailedFiles         []FailedFiles   `json:"failedFiles"`
	StartTimeUTC        time.Time       `json:"startTimeUTC"`
	EndTimeUTC          time.Time       `json:"endTimeUTC"`
	TotalRuntimeSeconds int64           `json:"totalRuntimeSeconds"`
	TotalBytesCollected int64           `json:"totalBytesCollected"`
	Executors           []string        `json:"executors"`
	Coordinators        []string        `json:"coordinators"`
}

type ClusterInfo struct {
	NumberNodesContacted int `json:"numberNodesContacted"`
	TotalNodesAttempted  int `json:"totalNodesAttempted"`
}

type CollectedFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

type FailedFiles struct {
	Path string `json:"path"`
	Err  error  `json:"err"`
}

type CollectionInfoWriterError struct {
	CollectionInfo CollectionInfo
	Err            error
}

func (w CollectionInfoWriterError) Error() string {
	return fmt.Sprintf("This is a bug, unable to write summary %#v due to error %v", w.CollectionInfo, w.Err)
}

func (summary CollectionInfo) String() (string, error) {
	b, err := json.MarshalIndent(summary, "", "\t")
	if err != nil {
		return "", CollectionInfoWriterError{
			CollectionInfo: summary,
			Err:            err,
		}
	}
	return string(b), nil
}
