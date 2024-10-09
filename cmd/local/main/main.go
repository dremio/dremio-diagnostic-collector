package main

import (
	"log"
	"os"

	cmd "github.com/dremio/dremio-diagnostic-collector/v3/cmd/local"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/consoleprint"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/simplelog"
)

func main() {
	//initial logger before verbosity is parsed
	defer func() {
		if err := simplelog.Close(); err != nil {
			log.Printf("unable to close log due to error %v", err)
		}
	}()
	if err := cmd.LocalCollectCmd.Execute(); err != nil {
		consoleprint.ErrorPrint(err.Error())
		os.Exit(1)
	}
}
