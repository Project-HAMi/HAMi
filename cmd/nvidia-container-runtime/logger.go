/*
# Copyright (c) 2021, NVIDIA CORPORATION.  All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
*/

package main

import (
	"fmt"
	"io"
	"os"

	"github.com/sirupsen/logrus"
	"github.com/tsaikd/KDGoLib/logrusutil"
)

// Logger adds a way to manage output to a log file to a logrus.Logger
type Logger struct {
	*logrus.Logger
	previousOutput io.Writer
	logFile        *os.File
}

// NewLogger constructs a Logger with a preddefined formatter
func NewLogger() *Logger {
	logrusLogger := logrus.New()

	formatter := &logrusutil.ConsoleLogFormatter{
		TimestampFormat: "2006/01/02 15:04:07",
		Flag:            logrusutil.Ltime,
	}

	logger := &Logger{
		Logger: logrusLogger,
	}
	logger.SetFormatter(formatter)

	return logger
}

// LogToFile opens the specified file for appending and sets the logger to
// output to the opened file. A reference to the file pointer is stored to
// allow this to be closed.
func (l *Logger) LogToFile(filename string) error {
	logFile, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("error opening debug log file: %v", err)
	}

	l.logFile = logFile
	l.previousOutput = l.Out
	l.SetOutput(logFile)

	return nil
}

// CloseFile closes the log file (if any) and resets the logger output to what it
// was before LogToFile was called.
func (l *Logger) CloseFile() error {
	if l.logFile == nil {
		return nil
	}
	logFile := l.logFile
	l.SetOutput(l.previousOutput)
	l.logFile = nil

	return logFile.Close()
}
