package logger

import (
	"io"
	"log"
	"os"
	"strings"
)

// Level represents the logging level
type Level int

const (
	// DebugLevel enables all logging including debug messages
	DebugLevel Level = iota
	// InfoLevel enables info, warning, and error logging (default)
	InfoLevel
)

// Logger wraps the standard logger with level-based logging
type Logger struct {
	level       Level
	infoLogger  *log.Logger
	debugLogger *log.Logger
}

var defaultLogger *Logger

// Init initializes the default logger with the specified level
func Init(levelStr string) {
	level := InfoLevel
	switch strings.ToLower(levelStr) {
	case "debug":
		level = DebugLevel
	case "info":
		level = InfoLevel
	default:
		level = InfoLevel
	}

	defaultLogger = &Logger{
		level:       level,
		infoLogger:  log.New(os.Stdout, "", log.LstdFlags),
		debugLogger: log.New(os.Stdout, "[DEBUG] ", log.LstdFlags),
	}
}

// SetOutput sets the output destination for the logger
func SetOutput(w io.Writer) {
	if defaultLogger != nil {
		defaultLogger.infoLogger.SetOutput(w)
		defaultLogger.debugLogger.SetOutput(w)
	}
}

// Info logs an informational message
func Info(format string, v ...interface{}) {
	if defaultLogger == nil {
		Init("info")
	}
	defaultLogger.infoLogger.Printf(format, v...)
}

// Debug logs a debug message (only if debug level is enabled)
func Debug(format string, v ...interface{}) {
	if defaultLogger == nil {
		Init("info")
	}
	if defaultLogger.level == DebugLevel {
		defaultLogger.debugLogger.Printf(format, v...)
	}
}

// Fatal logs a fatal message and exits
func Fatal(format string, v ...interface{}) {
	if defaultLogger == nil {
		Init("info")
	}
	defaultLogger.infoLogger.Fatalf(format, v...)
}

// IsDebugEnabled returns true if debug logging is enabled
func IsDebugEnabled() bool {
	if defaultLogger == nil {
		return false
	}
	return defaultLogger.level == DebugLevel
}

// Reset resets the logger to nil (primarily for testing)
func Reset() {
	defaultLogger = nil
}
