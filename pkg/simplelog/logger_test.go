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

// simplelog package provides a simple logger
package simplelog

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLogger(t *testing.T) {
	tests := []struct {
		level        int
		debugMessage string
		infoMessage  string
		warnMessage  string
		errMessage   string
	}{
		{LevelError, "", "", "", "ERROR: "},
		{LevelWarning, "", "", "WARN: ", "ERROR: "},
		{LevelInfo, "", "INFO: ", "WARN: ", "ERROR: "},
		{LevelDebug, "DEBUG: ", "INFO: ", "WARN: ", "ERROR: "},
	}

	for _, tt := range tests {
		buf := new(bytes.Buffer)

		logger := newLogger(tt.level)
		logger.debugLogger.SetOutput(buf)
		logger.infoLogger.SetOutput(buf)
		logger.warningLogger.SetOutput(buf)
		logger.errorLogger.SetOutput(buf)

		logger.Debugf("debug message")
		logger.Infof("info message")
		logger.Warningf("warn message")
		logger.Errorf("err message")

		output := buf.String()

		assert.Contains(t, output, tt.debugMessage)
		assert.Contains(t, output, tt.infoMessage)
		assert.Contains(t, output, tt.warnMessage)
		assert.Contains(t, output, tt.errMessage)
	}
}
