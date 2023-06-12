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
	"sync"
)

const (
	LevelError = iota
	LevelWarning
	LevelInfo
	LevelDebug
)

var logger *Logger
var internalDebugLogger *log.Logger
var ddcLog *os.File
var mut = &sync.Mutex{}

type Logger struct {
	debugLogger   *log.Logger
	infoLogger    *log.Logger
	warningLogger *log.Logger
	errorLogger   *log.Logger
}

func init() {
	logger = newLogger(LevelError)
	internalDebugLogger = log.New(os.Stdout, "LOG_DEBUG: ", log.Ldate|log.Ltime|log.Lshortfile)
}

func InitLogger(level int) {
	if level > 3 {
		logger = newLogger(LevelDebug)
	} else {
		logger = newLogger(level)
	}
}

func Close() error {
	logger.Info("Close called on log")
	if err := ddcLog.Close(); err != nil {
		return fmt.Errorf("unable to close ddc.log with error %v", err)
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

	debugOut, infoOut, warningOut, errorOut := io.Discard, io.Discard, io.Discard, io.Discard
	ddcLoc, err := os.Executable()
	if err != nil {
		log.Fatalf("unable to to find ddc cannot copy it to hosts due to error '%v'", err)
	}
	mut.Lock()
	if ddcLog != nil {
		internalDebug(adjustedLevel, "closing log")
		if err := Close(); err != nil {
			internalDebug(adjustedLevel, fmt.Sprintf("unable to close log %v", err))
		}
	}
	ddcLog, err = os.OpenFile(path.Join(path.Dir(ddcLoc), "ddc.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		log.Fatal(err)
	}
	mut.Unlock()

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

	//set logger levels because we rely on fall through we cannot use the above switch easily
	switch adjustedLevel {
	case LevelDebug:
		debugOut = io.MultiWriter(os.Stdout, ddcLog)
		fallthrough
	case LevelInfo:
		infoOut = io.MultiWriter(os.Stdout, ddcLog)
		fallthrough
	case LevelWarning:
		warningOut = io.MultiWriter(os.Stdout, ddcLog)
		fallthrough
	case LevelError:
		errorOut = io.MultiWriter(os.Stdout, ddcLog)
	}

	return &Logger{
		debugLogger:   log.New(debugOut, "DEBUG: ", log.Ldate|log.Ltime|log.Lshortfile),
		infoLogger:    log.New(infoOut, "INFO:  ", log.Ldate|log.Ltime|log.Lshortfile),
		warningLogger: log.New(warningOut, "WARN:  ", log.Ldate|log.Ltime|log.Lshortfile),
		errorLogger:   log.New(errorOut, "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile),
	}
}

func (l *Logger) Debug(format string) {
	handleLogError(l.debugLogger.Output(2, format), format, "DEBUG")
}

func (l *Logger) Info(format string) {
	handleLogError(l.infoLogger.Output(2, format), format, "INFO")
}

func (l *Logger) Warning(format string) {
	handleLogError(l.warningLogger.Output(2, format), format, "WARNING")
}

func (l *Logger) Error(format string) {
	handleLogError(l.errorLogger.Output(2, format), format, "ERROR")
}

func (l *Logger) Debugf(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	handleLogError(l.debugLogger.Output(2, msg), msg, "DEBUGF")
}

func (l *Logger) Infof(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	handleLogError(l.infoLogger.Output(2, msg), msg, "INFOF")
}

func (l *Logger) Warningf(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	handleLogError(l.warningLogger.Output(2, msg), msg, "WARNINGF")
}

func (l *Logger) Errorf(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	handleLogError(l.errorLogger.Output(2, msg), msg, "ERRORF")
}

// package functions

func Debug(format string) {
	handleLogError(logger.debugLogger.Output(2, format), format, "DEBUG")
}

func Info(format string) {
	handleLogError(logger.infoLogger.Output(2, format), format, "INFO")
}

func Warning(format string) {
	handleLogError(logger.warningLogger.Output(2, format), format, "WARNING")
}

func Error(format string) {
	handleLogError(logger.errorLogger.Output(2, format), format, "ERROR")
}

func Debugf(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	handleLogError(logger.debugLogger.Output(2, msg), msg, "DEBUGF")
}

func Infof(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	handleLogError(logger.infoLogger.Output(2, msg), msg, "INFOF")
}

func Warningf(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	handleLogError(logger.warningLogger.Output(2, msg), msg, "WARNINGF")
}

func Errorf(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	handleLogError(logger.errorLogger.Output(2, msg), msg, "ERRORF")
}

func handleLogError(err error, attemptedMsg, level string) {
	if err != nil {
		log.Fatalf("critical error logging to level %v with message '%v' and therefore there is no log output due to error '%v'", level, attemptedMsg, err)
	}
}
