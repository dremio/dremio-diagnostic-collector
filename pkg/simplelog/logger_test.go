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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/output"
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

	for _, test := range tests {
		buf := new(bytes.Buffer)
		temp, err := os.CreateTemp(t.TempDir(), "logs")
		if err != nil {
			t.Fatal(err)
		}
		logger := newLogger(temp, func() { temp.Close() })
		logger.debugLogger.SetOutput(buf)
		logger.infoLogger.SetOutput(buf)
		logger.warningLogger.SetOutput(buf)
		logger.errorLogger.SetOutput(buf)

		logger.Debugf("debug message")
		logger.Infof("info message")
		logger.Warningf("warn message")
		logger.Errorf("err message")

		output := buf.String()

		if !strings.Contains(output, test.debugMessage) {
			t.Errorf("expected %v to contain %v but did not", output, test.debugMessage)
		}

		if !strings.Contains(output, test.infoMessage) {
			t.Errorf("expected %v to contain %v but did not", output, test.infoMessage)
		}
		if !strings.Contains(output, test.warnMessage) {
			t.Errorf("expected %v to contain %v but did not", output, test.warnMessage)
		}
		if !strings.Contains(output, test.errMessage) {
			t.Errorf("expected %v to contain %v but did not", output, test.errMessage)
		}
	}
}

func TestStartLogMessage(t *testing.T) {
	InitLogger()
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
	InitLogger()
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
	temp, err := os.CreateTemp(t.TempDir(), "log")
	if err != nil {
		t.Fatal(err)
	}
	logger := newLogger(temp, func() { temp.Close() })
	logger.debugLogger.SetOutput(dbbuf)
	logger.infoLogger.SetOutput(infobuf)
	logger.warningLogger.SetOutput(warnbuf)
	logger.errorLogger.SetOutput(errbuf)

	logger.Debug(msg)
	logger.Info(msg)
	logger.Warning(msg)
	logger.Error(msg)

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
func TestLogIsCreatedInOutputDir(t *testing.T) {
	// First, close any existing logger
	if err := Close(); err != nil {
		t.Logf("Error closing existing logger: %v", err)
	}

	tempDir := t.TempDir()
	tPath := filepath.Join(tempDir, "ddcout")
	expected := filepath.Join(tempDir, "ddcout", "ddc.log")

	// Create the directory if it doesn't exist
	if err := os.MkdirAll(tPath, 0755); err != nil {
		t.Fatalf("Failed to create directory: %v", err)
	}

	// Register cleanup function
	t.Cleanup(func() {
		if err := Close(); err != nil {
			t.Logf("Error closing logger during cleanup: %v", err)
		}
	})

	InitLoggerWithOutputDir(tPath)
	actual := GetLogLoc()

	// Verify log location is not empty
	if actual == "" {
		t.Error("expected returned log file location to not be empty but it was")
	}

	// Verify log file exists at expected location
	if _, err := os.Stat(expected); os.IsNotExist(err) {
		t.Fatalf("expected log file %v to exist but instead was %v", expected, actual)
	}

	// Verify the returned log location matches the expected path
	if actual != expected {
		t.Errorf("expected log location to be %v but got %v", expected, actual)
	}
}
func TestLogIsNotCreatedAtBasePathWithOutputDir(t *testing.T) {
	// First, get the default log location
	InitLogger()
	defaultLogPath := GetLogLoc()
	if defaultLogPath == "" {
		t.Fatal("Default log path should not be empty")
	}

	// Clean up the default logger
	if err := Close(); err != nil {
		t.Logf("Error closing default logger: %v", err)
	}

	// Remove the default log file if it exists
	if err := os.Remove(defaultLogPath); err != nil && !os.IsNotExist(err) {
		t.Logf("Error removing default log file: %v", err)
	}

	// Create a temp directory for the output
	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "custom_logs")

	// Create the output directory
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatalf("Failed to create output directory: %v", err)
	}

	// Initialize logger with the output directory
	InitLoggerWithOutputDir(outputDir)

	// Check that the log file exists in the output directory
	expectedLogPath := filepath.Join(outputDir, "ddc.log")
	if _, err := os.Stat(expectedLogPath); os.IsNotExist(err) {
		t.Errorf("Log file not created in output directory: %v", expectedLogPath)
	}

	// Check that no log file was created at the default path
	if _, err := os.Stat(defaultLogPath); !os.IsNotExist(err) {
		// If the file exists or there's another error, fail the test
		if err == nil {
			t.Errorf("Log file was incorrectly created at default path: %v", defaultLogPath)
		} else {
			t.Errorf("Error checking default path log: %v", err)
		}
	}

	// Clean up
	if err := Close(); err != nil {
		t.Logf("Error closing logger: %v", err)
	}
}
