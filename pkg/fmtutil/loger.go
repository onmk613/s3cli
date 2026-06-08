package fmtutil

import (
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

// 简单的日志记录
var debug atomic.Bool

func Info(format string, args ...interface{}) {
	if !debug.Load() {
		return
	}
	PrintlnGreen(logMessage("INFO", format, args...))
}

func Warn(format string, args ...interface{}) {
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

// OpenLogFile 创建日志文件并确保目录存在
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
