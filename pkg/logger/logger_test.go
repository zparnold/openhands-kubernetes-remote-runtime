package logger

import (
	"bytes"
	"strings"
	"testing"
)

func TestInit(t *testing.T) {
	tests := []struct {
		name          string
		level         string
		expectedLevel Level
	}{
		{"Debug level", "debug", DebugLevel},
		{"Debug level uppercase", "DEBUG", DebugLevel},
		{"Info level", "info", InfoLevel},
		{"Info level uppercase", "INFO", InfoLevel},
		{"Unknown level defaults to info", "unknown", InfoLevel},
		{"Empty level defaults to info", "", InfoLevel},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Init(tt.level)
			if defaultLogger.level != tt.expectedLevel {
				t.Errorf("Expected level %v, got %v", tt.expectedLevel, defaultLogger.level)
			}
		})
	}
}

func TestInfo(t *testing.T) {
	var buf bytes.Buffer
	Init("info")
	SetOutput(&buf)

	Info("test message: %s", "hello")

	output := buf.String()
	if !strings.Contains(output, "test message: hello") {
		t.Errorf("Expected output to contain 'test message: hello', got: %s", output)
	}
}

func TestDebugWithDebugLevel(t *testing.T) {
	var buf bytes.Buffer
	Init("debug")
	SetOutput(&buf)

	Debug("debug message: %s", "world")

	output := buf.String()
	if !strings.Contains(output, "debug message: world") {
		t.Errorf("Expected output to contain 'debug message: world', got: %s", output)
	}
	if !strings.Contains(output, "[DEBUG]") {
		t.Errorf("Expected output to contain '[DEBUG]' prefix, got: %s", output)
	}
}

func TestDebugWithInfoLevel(t *testing.T) {
	var buf bytes.Buffer
	Init("info")
	SetOutput(&buf)

	Debug("debug message: %s", "should not appear")

	output := buf.String()
	if strings.Contains(output, "debug message: should not appear") {
		t.Errorf("Expected debug message to be suppressed at info level, got: %s", output)
	}
}

func TestIsDebugEnabled(t *testing.T) {
	tests := []struct {
		name     string
		level    string
		expected bool
	}{
		{"Debug enabled", "debug", true},
		{"Debug disabled at info level", "info", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Init(tt.level)
			if IsDebugEnabled() != tt.expected {
				t.Errorf("Expected IsDebugEnabled() to be %v, got %v", tt.expected, IsDebugEnabled())
			}
		})
	}
}

func TestInfoWithoutInit(t *testing.T) {
	// Reset default logger to test auto-initialization
	defaultLogger = nil
	var buf bytes.Buffer

	// This should auto-initialize
	Info("test")
	if defaultLogger == nil {
		t.Error("Expected defaultLogger to be initialized automatically")
	}

	// Now set output and test
	SetOutput(&buf)
	Info("auto init test")

	output := buf.String()
	if !strings.Contains(output, "auto init test") {
		t.Errorf("Expected output to contain 'auto init test', got: %s", output)
	}
}

func TestDebugWithoutInit(t *testing.T) {
	// Reset default logger to test auto-initialization
	defaultLogger = nil
	var buf bytes.Buffer

	// This should auto-initialize with info level
	Debug("should not appear")
	if defaultLogger == nil {
		t.Error("Expected defaultLogger to be initialized automatically")
	}

	// Debug should be suppressed at default info level
	SetOutput(&buf)
	Debug("still should not appear")

	output := buf.String()
	if strings.Contains(output, "should not appear") {
		t.Errorf("Expected debug to be suppressed with auto-init, got: %s", output)
	}
}
