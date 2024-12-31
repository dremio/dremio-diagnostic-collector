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

// main is the standard go entry point for the application
package main

import (
	"log"
	"os"

	"github.com/dremio/dremio-diagnostic-collector/v3/cmd"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/consoleprint"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/simplelog"
)

func main() {
	// initial logger before verbosity is parsed
	defer func() {
		if err := simplelog.Close(); err != nil {
			log.Printf("unable to close log: %v", err)
		}
	}()
	if err := cmd.Execute(os.Args); err != nil {
		consoleprint.ErrorPrint(err.Error())
		os.Exit(1)
	}
}
