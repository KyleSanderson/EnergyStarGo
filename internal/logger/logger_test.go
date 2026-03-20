// SPDX-License-Identifier: GPL-2.0-only
// SPDX-FileCopyrightText: 2024 Kyle Sanderson

package logger

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewLogger_Stderr(t *testing.T) {
	log, err := New("", "info")
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if log == nil {
		t.Fatal("logger should not be nil")
	}
}

func TestNewLogger_WithFile(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "test.log")

	log, err := New(logFile, "debug")
	if err != nil {
		t.Fatalf("New with file failed: %v", err)
	}
	if log == nil {
		t.Fatal("logger should not be nil")
	}

	   // Write a log message
	   log.Info("test message", "key", "value")
	   log.Close()

	   // Verify file was created
	   if _, err := os.Stat(logFile); os.IsNotExist(err) {
		   t.Error("log file was not created")
	   }

	   // Verify file has content
	   data, err := os.ReadFile(logFile)
	   if err != nil {
		   t.Fatalf("failed to read log file: %v", err)
	   }
	   if len(data) == 0 {
		   t.Error("log file should have content")
	   }
}

func TestNewLogger_InvalidPath(t *testing.T) {
	_, err := New("/nonexistent/deep/path/that/cannot/exist/test.log", "info")
	if err == nil {
		t.Error("expected error for invalid log path")
	}
}

func TestNewLogger_LogLevels(t *testing.T) {
	levels := []string{"debug", "info", "warn", "warning", "error", "unknown"}
	for _, level := range levels {
		t.Run(level, func(t *testing.T) {
			log, err := New("", level)
			if err != nil {
				t.Fatalf("New with level %q failed: %v", level, err)
			}
			if log == nil {
				t.Fatal("logger should not be nil")
			}
		})
	}
}

func TestThrottleLog(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "throttle.log")

	log, err := New(logFile, "info")
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	   log.Throttle("notepad.exe", 1234)
	   log.Close()

	   data, _ := os.ReadFile(logFile)
	   content := string(data)
	   if len(content) == 0 {
		   t.Error("expected throttle log entry")
	   }
}

func TestBoostLog(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "boost.log")

	log, err := New(logFile, "info")
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	   log.Boost("chrome.exe", 5678)
	   log.Close()

	   data, _ := os.ReadFile(logFile)
	   content := string(data)
	   if len(content) == 0 {
		   t.Error("expected boost log entry")
	   }
}

func TestHousekeepingLog(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "hk.log")

	log, err := New(logFile, "info")
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	   log.Housekeeping(42)
	   log.Close()

	   data, _ := os.ReadFile(logFile)
	   content := string(data)
	   if len(content) == 0 {
		   t.Error("expected housekeeping log entry")
	   }
}

func TestForegroundChangeLog_DebugLevel(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "fg.log")

	// With debug level, foreground changes should appear
	log, err := New(logFile, "debug")
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	   log.ForegroundChange("notepad.exe", 1234)
	   log.Close()

	   data, _ := os.ReadFile(logFile)
	   content := string(data)
	   if len(content) == 0 {
		   t.Error("expected foreground change log entry at debug level")
	   }
}

func TestForegroundChangeLog_InfoLevel(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "fg-info.log")

	// With info level, debug messages should not appear
	log, err := New(logFile, "info")
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	   log.ForegroundChange("notepad.exe", 1234)
	   log.Close()

	   data, _ := os.ReadFile(logFile)
	   content := string(data)
	   if len(content) != 0 {
		   t.Error("foreground change should not appear at info level")
	   }
}

func TestLogFileAppend(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "append.log")

	// Write first entry
	   log1, err := New(logFile, "info")
	   if err != nil {
		   t.Fatalf("New failed: %v", err)
	   }
	   log1.Info("message1")
	   log1.Close()

	   data1, _ := os.ReadFile(logFile)
	   len1 := len(data1)

	   // Open again and write second entry
	   log2, err := New(logFile, "info")
	   if err != nil {
		   t.Fatalf("New failed: %v", err)
	   }
	   log2.Info("message2")
	   log2.Close()

	   data2, _ := os.ReadFile(logFile)
	   if len(data2) <= len1 {
		   t.Error("second write should have appended to log file")
	   }
}
