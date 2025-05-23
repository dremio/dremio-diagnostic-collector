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
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"sync"

	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/consoleprint"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/strutils"
)

const (
	LevelError = iota
	LevelWarning
	LevelInfo
	LevelDebug
)

const msgMax = 1000

var (
	logger         *Logger
	ddcLogFilePath string
	ddcLogMut      = &sync.Mutex{}
)

func setDDCLog(filePath string) {
	ddcLogFilePath = filePath
}

type Logger struct {
	debugLogger   *log.Logger
	infoLogger    *log.Logger
	warningLogger *log.Logger
	errorLogger   *log.Logger
	hostLog       *log.Logger
	cleanup       func()
}

func (l *Logger) Close() {
	l.debugLogger = log.New(io.Discard, "DEBUG: ", log.Ldate|log.Ltime|log.Lshortfile)
	l.infoLogger = log.New(io.Discard, "INFO:  ", log.Ldate|log.Ltime|log.Lshortfile)
	l.warningLogger = log.New(io.Discard, "WARN:  ", log.Ldate|log.Ltime|log.Lshortfile)
	l.errorLogger = log.New(io.Discard, "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile)
	l.hostLog = log.New(io.Discard, "", 0)
	l.cleanup()
}

func init() {
	InitLogger()
}

func InitLogger() {
	// default location
	ddcLogMut.Lock()
	defer ddcLogMut.Unlock()
	f := createLog("", true)
	ddcLogLoc := GetLogLoc()
	logger = newLogger(f, func() {
		if err := f.Close(); err != nil {
			fmt.Printf("WARNING unable to close log file: %v\n", ddcLogLoc)
		}
	})
}

func InitLoggerWithOutputDir(outputDir string) {
	ddcLogMut.Lock()
	defer ddcLogMut.Unlock()
	// Create log file in the specified output directory
	if outputDir != "" {
		logPath := filepath.Join(outputDir, "ddc.log")
		f := createLog(logPath, true)
		ddcLogLoc := GetLogLoc()
		logger = newLogger(f, func() {
			if err := f.Close(); err != nil {
				fmt.Printf("WARNING unable to close log file: %v\n", ddcLogLoc)
			}
		})
	}
}

func LogStartMessage() {
	var logLine string
	if GetLogLoc() != "" {
		logLine = fmt.Sprintf("### logging to file: %v ###", GetLogLoc())
	} else {
		logLine = "### unable to write ddc.log using STDOUT ###"
	}
	padding := PaddingForStr(logLine)
	fmt.Printf("%v\n%v\n%v\n", padding, logLine, padding)
}

func PaddingForStr(str string) string {
	newStr := ""
	for i := 0; i < len(str); i++ {
		newStr += "#"
	}
	return newStr
}

func LogEndMessage() {
	var logLine string
	if GetLogLoc() != "" {
		logLine = fmt.Sprintf("### for any troubleshooting consult log: %v ###", GetLogLoc())
	} else {
		logLine = "### no log written ###"
	}
	padding := PaddingForStr(logLine)
	fmt.Printf("%v\n%v\n%v\n", padding, logLine, padding)
}

func createLog(fileName string, truncate bool) *os.File {
	var file *os.File
	var logLocation string
	var err error
	if fileName != "" {
		logLocation = fileName
		if truncate {
			file, err = os.OpenFile(filepath.Clean(fileName), os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0o600)
		} else {
			file, err = os.OpenFile(filepath.Clean(fileName), os.O_WRONLY|os.O_APPEND, 0o600)
		}
	} else {
		logLocation, file, err = getDefaultLogLoc()
	}
	if err != nil {
		fallbackPath := filepath.Clean(filepath.Join(os.TempDir(), "ddc.log"))
		var fallbackLog *os.File
		if truncate {
			fallbackLog, err = os.OpenFile(fallbackPath, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0o600)
		} else {
			fallbackLog, err = os.OpenFile(fallbackPath, os.O_WRONLY|os.O_APPEND, 0o600)
		}
		if err != nil {
			fmt.Println("falling back to standard out")
			return nil
		}
		setDDCLog(fallbackPath)
		consoleprint.WarningPrint(fmt.Sprintf("falling back to %v", fallbackPath))
		return fallbackLog
	}
	setDDCLog(logLocation)
	return file
}

func getDefaultLogLoc() (string, *os.File, error) {
	ddcLoc, err := os.Executable()
	if err != nil {
		return "", nil, fmt.Errorf("unable to to find ddc cannot copy it to hosts: %w", err)
	}
	ddcLogPath, err := filepath.Abs(path.Join(path.Dir(ddcLoc), "ddc.log"))
	if err != nil {
		return "", nil, fmt.Errorf("unable to get absolute path of ddc log: %w", err)
	}
	// abs has already cleaned this path so no need to ignore it again
	f, err := os.OpenFile(ddcLogPath, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0o600) // #nosec G304
	if err != nil {
		return "", nil, fmt.Errorf("unable to open ddc log %w", err)
	}
	return ddcLogPath, f, nil
}

func GetLogLoc() string {
	if ddcLogFilePath != "" {
		full, err := filepath.Abs(ddcLogFilePath)
		if err != nil {
			logger.Debugf("unable to get full path for %v: %v", ddcLogFilePath, err)
			return ddcLogFilePath
		}
		return full
	}
	return ddcLogFilePath
}

func CopyLog(dest string) error {
	// We need to get a lock on the log file to safely close it
	// to avoid any potential copy errors on Windows
	ddcLogMut.Lock()
	defer ddcLogMut.Unlock()

	// now the log is down and we are blocking until this is done
	logRead, err := os.ReadFile(filepath.Clean(GetLogLoc()))
	if err != nil {
		return err
	}
	// ok we copy the file out
	err = os.WriteFile(dest, logRead, 0o600)
	if err != nil {
		return err
	}

	f := createLog(GetLogLoc(), false)
	logger = newLogger(f, func() {
		if err := f.Close(); err != nil {
			fmt.Printf("unable to cleanup logger: %v\n", err)
		}
	})
	return nil
}

func Close() error {
	logger.Debug("Close called on log")
	ddcLogMut.Lock()
	defer ddcLogMut.Unlock()
	logger.Close()

	return nil
}

func newLogger(writer io.Writer, cleanup func()) *Logger {
	if writer == nil {
		return &Logger{
			debugLogger:   log.New(io.Discard, "DEBUG: ", log.Ldate|log.Ltime|log.Lshortfile),
			infoLogger:    log.New(io.Discard, "INFO:  ", log.Ldate|log.Ltime|log.Lshortfile),
			warningLogger: log.New(io.Discard, "WARN:  ", log.Ldate|log.Ltime|log.Lshortfile),
			errorLogger:   log.New(io.Discard, "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile),
			hostLog:       log.New(io.Discard, "", 0),
		}
	}
	return &Logger{
		debugLogger:   log.New(writer, "DEBUG: ", log.Ldate|log.Ltime|log.Lshortfile),
		infoLogger:    log.New(writer, "INFO:  ", log.Ldate|log.Ltime|log.Lshortfile),
		warningLogger: log.New(writer, "WARN:  ", log.Ldate|log.Ltime|log.Lshortfile),
		errorLogger:   log.New(writer, "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile),
		hostLog:       log.New(writer, "", 0),
		cleanup:       cleanup,
	}
}

func (l *Logger) Debug(format string) {
	trimmed := strutils.GetEndOfString(format, msgMax)
	handleLogError(l.debugLogger.Output(2, trimmed), trimmed, "DEBUG")
}

func (l *Logger) Info(format string) {
	trimmed := strutils.GetEndOfString(format, msgMax)
	handleLogError(l.infoLogger.Output(2, trimmed), trimmed, "INFO")
}

func (l *Logger) Warning(format string) {
	trimmed := strutils.GetEndOfString(format, msgMax)
	handleLogError(l.warningLogger.Output(2, trimmed), trimmed, "WARNING")
}

func (l *Logger) Error(format string) {
	trimmed := strutils.GetEndOfString(format, msgMax)
	handleLogError(l.errorLogger.Output(2, trimmed), trimmed, "ERROR")
}

func (l *Logger) Debugf(format string, v ...interface{}) {
	msg := strutils.GetEndOfString(fmt.Sprintf(format, v...), msgMax)
	handleLogError(l.debugLogger.Output(2, msg), msg, "DEBUGF")
}

func (l *Logger) Infof(format string, v ...interface{}) {
	msg := strutils.GetEndOfString(fmt.Sprintf(format, v...), msgMax)
	handleLogError(l.infoLogger.Output(2, msg), msg, "INFOF")
}

func (l *Logger) Warningf(format string, v ...interface{}) {
	msg := strutils.GetEndOfString(fmt.Sprintf(format, v...), msgMax)
	handleLogError(l.warningLogger.Output(2, msg), msg, "WARNINGF")
}

func (l *Logger) Errorf(format string, v ...interface{}) {
	msg := strutils.GetEndOfString(fmt.Sprintf(format, v...), msgMax)
	handleLogError(l.errorLogger.Output(2, msg), msg, "ERRORF")
}

// package functions

func Debug(format string) {
	ddcLogMut.Lock()
	defer ddcLogMut.Unlock()
	trimmed := strutils.GetEndOfString(format, msgMax)
	handleLogError(logger.debugLogger.Output(2, trimmed), trimmed, "DEBUG")
}

func Info(format string) {
	ddcLogMut.Lock()
	defer ddcLogMut.Unlock()
	trimmed := strutils.GetEndOfString(format, msgMax)
	handleLogError(logger.infoLogger.Output(2, trimmed), trimmed, "INFO")
}

func Warning(format string) {
	ddcLogMut.Lock()
	defer ddcLogMut.Unlock()
	trimmed := strutils.GetEndOfString(format, msgMax)
	handleLogError(logger.warningLogger.Output(2, trimmed), trimmed, "WARNING")
}

func Error(format string) {
	ddcLogMut.Lock()
	defer ddcLogMut.Unlock()
	trimmed := strutils.GetEndOfString(format, msgMax)
	handleLogError(logger.errorLogger.Output(2, trimmed), trimmed, "ERROR")
}

func Debugf(format string, v ...interface{}) {
	ddcLogMut.Lock()
	defer ddcLogMut.Unlock()
	msg := strutils.GetEndOfString(fmt.Sprintf(format, v...), msgMax)
	handleLogError(logger.debugLogger.Output(2, msg), msg, "DEBUGF")
}

func Infof(format string, v ...interface{}) {
	ddcLogMut.Lock()
	defer ddcLogMut.Unlock()
	msg := strutils.GetEndOfString(fmt.Sprintf(format, v...), msgMax)
	handleLogError(logger.infoLogger.Output(2, msg), msg, "INFOF")
}

func Warningf(format string, v ...interface{}) {
	ddcLogMut.Lock()
	defer ddcLogMut.Unlock()
	msg := strutils.GetEndOfString(fmt.Sprintf(format, v...), msgMax)
	handleLogError(logger.warningLogger.Output(2, msg), msg, "WARNINGF")
}

func Errorf(format string, v ...interface{}) {
	ddcLogMut.Lock()
	defer ddcLogMut.Unlock()
	msg := strutils.GetEndOfString(fmt.Sprintf(format, v...), msgMax)
	handleLogError(logger.errorLogger.Output(2, msg), msg, "ERRORF")
}

func HostLog(host, line string) {
	ddcLogMut.Lock()
	defer ddcLogMut.Unlock()
	msg := fmt.Sprintf("HOST %v - %v", host, line)
	handleLogError(logger.hostLog.Output(2, msg), line, "HOSTLOG")
}

func handleLogError(err error, attemptedMsg, level string) {
	if err != nil {
		log.Printf("critical error logging to level %v with message '%v' and therefore there is no log output: %v", level, attemptedMsg, err)
	}
}
