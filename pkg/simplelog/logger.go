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

	"github.com/dremio/dremio-diagnostic-collector/pkg/strutils"
)

const (
	LevelError = iota
	LevelWarning
	LevelInfo
	LevelDebug
)

const msgMax = 1000

var logger *Logger
var internalDebugLogger *log.Logger
var ddcLog *os.File
var mut = &sync.Mutex{}

type Logger struct {
	debugLogger   *log.Logger
	infoLogger    *log.Logger
	warningLogger *log.Logger
	errorLogger   *log.Logger
	hostLog       *log.Logger
}

func init() {
	logger = newLogger(LevelError)
	internalDebugLogger = log.New(os.Stdout, "LOG_DEBUG: ", log.Ldate|log.Ltime|log.Lshortfile)
}

func InitLogger(level int) {
	createLog(level)
	adjustedLevel := level
	if level > 3 {
		adjustedLevel = LevelDebug
	}
	logger = newLogger(adjustedLevel)
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

func createLog(adjustedLevel int) {
	mut.Lock()
	defer mut.Unlock()
	if ddcLog != nil {
		internalDebug(adjustedLevel, "closing log")
		if err := Close(); err != nil {
			internalDebug(adjustedLevel, fmt.Sprintf("unable to close log %v", err))
		}
	}
	defaultLog, err := getDefaultLogLoc()
	if err != nil {
		fallbackPath := filepath.Clean(filepath.Join(os.TempDir(), "ddc.log"))
		fallbackLog, err := os.OpenFile(fallbackPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			fmt.Println("falling back to standard out")
		} else {
			ddcLog = fallbackLog
			fmt.Printf("falling back to %v\n", fallbackPath)
		}
	} else {
		ddcLog = defaultLog
	}

}

func getDefaultLogLoc() (*os.File, error) {
	ddcLoc, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("unable to to find ddc cannot copy it to hosts due to error '%v'", err)
	}
	ddcLogPath, err := filepath.Abs(path.Join(path.Dir(ddcLoc), "ddc.log"))
	if err != nil {
		return nil, fmt.Errorf("unable to get absolute path of ddc log %v", err)
	}
	// abs has already cleaned this path so no need to ignore it again
	f, err := os.OpenFile(ddcLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600) // #nosec G304
	if err != nil {
		return nil, fmt.Errorf("unable to open ddc log %v", err)
	}
	return f, nil
}

func GetLogLoc() string {
	if ddcLog != nil {
		return ddcLog.Name()
	}
	return ""
}

func Close() error {
	logger.Debug("Close called on log")
	logger.debugLogger = log.New(io.Discard, "DEBUG: ", log.Ldate|log.Ltime|log.Lshortfile)
	logger.infoLogger = log.New(io.Discard, "INFO:  ", log.Ldate|log.Ltime|log.Lshortfile)
	logger.warningLogger = log.New(io.Discard, "WARN:  ", log.Ldate|log.Ltime|log.Lshortfile)
	logger.errorLogger = log.New(io.Discard, "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile)
	logger.hostLog = log.New(io.Discard, "", 0)
	if ddcLog != nil {
		if err := ddcLog.Close(); err != nil {
			return fmt.Errorf("unable to close ddc.log with error %v", err)
		}
	}
	return nil
}
func internalDebug(level int, text string) {
	if level > 2 {
		internalDebugLogger.Print(text)
	}
}
func newLogger(level int) *Logger {
	//cap out logging level because we rely on switch case below to match logging level
	adjustedLevel := level
	if adjustedLevel > 3 {
		adjustedLevel = 3
	}

	var debugOut io.Writer
	var infoOut io.Writer
	var warningOut io.Writer
	var errorOut io.Writer

	var stringLevelText = "UNKNOWN"
	switch adjustedLevel {
	case LevelDebug:
		stringLevelText = "DEBUG"
	case LevelInfo:
		stringLevelText = "INFO"
	case LevelWarning:
		stringLevelText = "WARN"
	case LevelError:
		stringLevelText = "ERROR"
	}
	internalDebug(adjustedLevel, fmt.Sprintf("initialized log with level %v", stringLevelText))
	var output io.Writer
	mut.Lock()
	if ddcLog != nil {
		output = ddcLog
		// we log debug to log every time so we can figure out problems
		debugOut, infoOut, warningOut, errorOut = ddcLog, ddcLog, ddcLog, ddcLog
	} else {
		output = os.Stdout
		// we are putting everything out to discard since there is no valid file to write too
		debugOut, infoOut, warningOut, errorOut = io.Discard, io.Discard, io.Discard, io.Discard
	}
	mut.Unlock()
	//set logger levels because we rely on fall through we cannot use the above switch easily
	switch adjustedLevel {
	case LevelDebug:
		debugOut = io.MultiWriter(os.Stdout, output)
		fallthrough
	case LevelInfo:
		infoOut = io.MultiWriter(os.Stdout, output)
		fallthrough
	case LevelWarning:
		warningOut = io.MultiWriter(os.Stdout, output)
		fallthrough
	case LevelError:
		errorOut = io.MultiWriter(os.Stdout, output)
	}
	//always add this
	hostOut := io.MultiWriter(os.Stdout, output)
	return &Logger{
		debugLogger:   log.New(debugOut, "DEBUG: ", log.Ldate|log.Ltime|log.Lshortfile),
		infoLogger:    log.New(infoOut, "INFO:  ", log.Ldate|log.Ltime|log.Lshortfile),
		warningLogger: log.New(warningOut, "WARN:  ", log.Ldate|log.Ltime|log.Lshortfile),
		errorLogger:   log.New(errorOut, "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile),
		hostLog:       log.New(hostOut, "", 0),
	}
}

func (l *Logger) Debug(format string) {
	trimmed := strutils.LimitString(format, msgMax)
	handleLogError(l.debugLogger.Output(2, trimmed), trimmed, "DEBUG")
}

func (l *Logger) Info(format string) {
	trimmed := strutils.LimitString(format, msgMax)
	handleLogError(l.infoLogger.Output(2, trimmed), trimmed, "INFO")
}

func (l *Logger) Warning(format string) {
	trimmed := strutils.LimitString(format, msgMax)
	handleLogError(l.warningLogger.Output(2, trimmed), trimmed, "WARNING")
}

func (l *Logger) Error(format string) {
	trimmed := strutils.LimitString(format, msgMax)
	handleLogError(l.errorLogger.Output(2, trimmed), trimmed, "ERROR")
}

func (l *Logger) Debugf(format string, v ...interface{}) {
	msg := strutils.LimitString(fmt.Sprintf(format, v...), msgMax)
	handleLogError(l.debugLogger.Output(2, msg), msg, "DEBUGF")
}

func (l *Logger) Infof(format string, v ...interface{}) {
	msg := strutils.LimitString(fmt.Sprintf(format, v...), msgMax)
	handleLogError(l.infoLogger.Output(2, msg), msg, "INFOF")
}

func (l *Logger) Warningf(format string, v ...interface{}) {
	msg := strutils.LimitString(fmt.Sprintf(format, v...), msgMax)
	handleLogError(l.warningLogger.Output(2, msg), msg, "WARNINGF")
}

func (l *Logger) Errorf(format string, v ...interface{}) {
	msg := strutils.LimitString(fmt.Sprintf(format, v...), msgMax)
	handleLogError(l.errorLogger.Output(2, msg), msg, "ERRORF")
}

// package functions

func Debug(format string) {
	trimmed := strutils.LimitString(format, msgMax)
	handleLogError(logger.debugLogger.Output(2, trimmed), trimmed, "DEBUG")
}

func Info(format string) {
	trimmed := strutils.LimitString(format, msgMax)
	handleLogError(logger.infoLogger.Output(2, trimmed), trimmed, "INFO")
}

func Warning(format string) {
	trimmed := strutils.LimitString(format, msgMax)
	handleLogError(logger.warningLogger.Output(2, trimmed), trimmed, "WARNING")
}

func Error(format string) {
	trimmed := strutils.LimitString(format, msgMax)
	handleLogError(logger.errorLogger.Output(2, trimmed), trimmed, "ERROR")
}

func Debugf(format string, v ...interface{}) {
	msg := strutils.LimitString(fmt.Sprintf(format, v...), msgMax)
	handleLogError(logger.debugLogger.Output(2, msg), msg, "DEBUGF")
}

func Infof(format string, v ...interface{}) {
	msg := strutils.LimitString(fmt.Sprintf(format, v...), msgMax)
	handleLogError(logger.infoLogger.Output(2, msg), msg, "INFOF")
}

func Warningf(format string, v ...interface{}) {
	msg := strutils.LimitString(fmt.Sprintf(format, v...), msgMax)
	handleLogError(logger.warningLogger.Output(2, msg), msg, "WARNINGF")
}

func Errorf(format string, v ...interface{}) {
	msg := strutils.LimitString(fmt.Sprintf(format, v...), msgMax)
	handleLogError(logger.errorLogger.Output(2, msg), msg, "ERRORF")
}

func HostLog(host, line string) {
	msg := fmt.Sprintf("HOST %v - %v", host, line)
	handleLogError(logger.hostLog.Output(2, msg), line, "HOSTLOG")
}

func handleLogError(err error, attemptedMsg, level string) {
	if err != nil {
		log.Fatalf("critical error logging to level %v with message '%v' and therefore there is no log output due to error '%v'", level, attemptedMsg, err)
	}
}
