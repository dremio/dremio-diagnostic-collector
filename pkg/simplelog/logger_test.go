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
	"fmt"
	"strings"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/pkg/output"
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

		logger := newLogger()
		logger.debugLogger.SetOutput(buf)
		logger.infoLogger.SetOutput(buf)
		logger.warningLogger.SetOutput(buf)
		logger.errorLogger.SetOutput(buf)

		logger.Debugf("debug message")
		logger.Infof("info message")
		logger.Warningf("warn message")
		logger.Errorf("err message")

		output := buf.String()

		if !strings.Contains(output, tt.debugMessage) {
			t.Errorf("expected %v to contain %v but did not", output, tt.debugMessage)
		}

		if !strings.Contains(output, tt.infoMessage) {
			t.Errorf("expected %v to contain %v but did not", output, tt.infoMessage)
		}
		if !strings.Contains(output, tt.warnMessage) {
			t.Errorf("expected %v to contain %v but did not", output, tt.warnMessage)
		}
		if !strings.Contains(output, tt.errMessage) {
			t.Errorf("expected %v to contain %v but did not", output, tt.errMessage)
		}
	}
}

func TestStartLogMessage(t *testing.T) {
	InitLogger(4)
	loc := GetLogLoc()
	if loc == "" {
		t.Error("expected log file to not be empty but it was")
	}
	out, err := output.CaptureOutput(func() {
		LogStartMessage()
	})
	if err != nil {
		t.Fatalf("failed running capture %v", err)
	}
	if !strings.Contains(out, loc) {
		t.Errorf("expected %v in string %v", loc, out)
	}
}

func TestEndLogMessage(t *testing.T) {
	InitLogger(4)
	loc := GetLogLoc()
	out, err := output.CaptureOutput(func() {
		LogEndMessage()
	})
	if err != nil {
		t.Fatalf("failed running capture %v", err)
	}
	if loc == "" {
		t.Error("expected log file to not be empty but it was")
	}
	if !strings.Contains(out, loc) {
		t.Errorf("expected %v in string %v", loc, out)
	}
}

func TestLoggerMessageIsTruncated(t *testing.T) {
	var arr []string
	for i := 0; i < 2000; i++ {
		arr = append(arr, fmt.Sprintf("%v", i))
	}
	msg := strings.Join(arr, "-")
	dbbuf := new(bytes.Buffer)
	infobuf := new(bytes.Buffer)
	warnbuf := new(bytes.Buffer)
	errbuf := new(bytes.Buffer)

	logger := newLogger()
	logger.debugLogger.SetOutput(dbbuf)
	logger.infoLogger.SetOutput(infobuf)
	logger.warningLogger.SetOutput(warnbuf)
	logger.errorLogger.SetOutput(errbuf)

	logger.Debugf(msg)
	logger.Infof(msg)
	logger.Warningf(msg)
	logger.Errorf(msg)

	expected := 1000
	output := strings.TrimSpace(strings.Split(dbbuf.String(), ": ")[2])

	if len(output) != expected {
		t.Errorf("expected %q to be %v but was %v", string(output), expected, len(output))
	}
	output = strings.TrimSpace(strings.Split(infobuf.String(), ": ")[2])

	if len(output) != expected {
		t.Errorf("expected %q to be %v but was %v", string(output), expected, len(output))
	}
	output = strings.TrimSpace(strings.Split(warnbuf.String(), ": ")[2])
	if len(output) != expected {
		t.Errorf("expected %q to be %v but was %v", string(output), expected, len(output))
	}
	output = strings.TrimSpace(strings.Split(errbuf.String(), ": ")[2])
	if len(output) != expected {
		t.Errorf("expected %q to be %v but was %v", string(output), expected, len(output))
	}
}
