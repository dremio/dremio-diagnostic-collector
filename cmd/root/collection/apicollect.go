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

package collection

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/local/restclient"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/consoleprint"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/shutdown"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/simplelog"
)

// APICollectionArgs holds configuration for orchestrator-side REST API collections.
type APICollectionArgs struct {
	TmpDir           string
	CoordinatorNode  string
	DremioEndpoint   string
	DremioPAT        string
	AllowInsecureSSL bool
	RestHTTPTimeout  int
	Hook             shutdown.Hook
}

// RunCollectKVStore fetches the KV store report from the Dremio REST API.
func RunCollectKVStore(args APICollectionArgs) error {
	if args.DremioPAT == "" {
		simplelog.Info("Skipping KV store collection: no PAT token provided")
		return nil
	}

	restclient.InitClient(args.AllowInsecureSSL, args.RestHTTPTimeout)

	outDir := filepath.Join(args.TmpDir, "kvstore", args.CoordinatorNode)
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return fmt.Errorf("unable to create KV store output directory %v: %w", outDir, err)
	}

	hook, ok := args.Hook.(shutdown.CancelHook)
	if !ok {
		return errors.New("hook does not implement CancelHook")
	}

	consoleprint.UpdateResult("Collecting KV store report...")
	simplelog.Info("Collecting KV store report...")
	url := args.DremioEndpoint + "/apiv2/kvstore/report"
	headers := map[string]string{"Accept": "application/octet-stream"}
	body, err := restclient.APIRequest(hook, url, args.DremioPAT, "GET", headers)
	if err != nil {
		return fmt.Errorf("unable to retrieve KV store report from %v: %w", url, err)
	}
	outFile := filepath.Join(outDir, "kvstore-report.zip")
	if err := os.WriteFile(outFile, body, 0o600); err != nil {
		return fmt.Errorf("unable to write %v: %w", outFile, err)
	}
	simplelog.Info("Collected KV store report")
	return nil
}
