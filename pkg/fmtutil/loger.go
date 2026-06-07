package fmtutil

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

var debug bool

func Info(format string, args ...interface{}) {
	if !debug {
		return
	}
	PrintlnGreen(logMessage("INFO", format, args...))
}

func Warn(format string, args ...interface{}) {
	if !debug {
		return
	}
	PrintlnYellow(logMessage("WARN", format, args...))
}

func Error(format string, args ...interface{}) {
	PrintlnRed(logMessage("ERROR", format, args...))
}

func logMessage(level string, format string, args ...interface{}) string {
	currentTime := time.Now()
	formattedTime := currentTime.Format("2006-01-02 15:04:05.0000")
	message := fmt.Sprintf(format, args...)
	return fmt.Sprintf("[%v] [%s] %v", formattedTime, level, message)
}

func OpenLogFile(path string) (*os.File, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	logFile, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}
	return logFile, nil
}
