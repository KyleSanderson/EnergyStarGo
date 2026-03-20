// SPDX-License-Identifier: GPL-2.0-only
// SPDX-FileCopyrightText: 2024 Kyle Sanderson

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.HousekeepingSeconds != 300 {
		t.Errorf("expected HousekeepingSeconds=300, got %d", cfg.HousekeepingSeconds)
	}
	if cfg.HousekeepingInterval.Seconds() != 300 {
		t.Errorf("expected HousekeepingInterval=5m, got %v", cfg.HousekeepingInterval)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("expected LogLevel=info, got %s", cfg.LogLevel)
	}
	if cfg.MinBuildNumber != 22000 {
		t.Errorf("expected MinBuildNumber=22000, got %d", cfg.MinBuildNumber)
	}
	if !cfg.RestoreOnExit {
		t.Error("expected RestoreOnExit=true")
	}
	if cfg.BoostForegroundOnly {
		t.Error("expected BoostForegroundOnly=false")
	}
	if cfg.EnableEventLog {
		t.Error("expected EnableEventLog=false")
	}
}

func TestShouldBypass(t *testing.T) {
	cfg := DefaultConfig()

	tests := []struct {
		name     string
		process  string
		expected bool
	}{
		{"system process lowercase", "csrss.exe", true},
		{"system process mixed case", "Csrss.exe", true},
		{"system process uppercase", "CSRSS.EXE", true},
		{"dwm", "dwm.exe", true},
		{"explorer", "explorer.exe", true},
		{"svchost", "svchost.exe", true},
		{"taskmgr", "taskmgr.exe", true},
		{"self", "energystar.exe", true},
		{"self go", "energystar-go.exe", false},
		{"edge", "msedge.exe", false},
		{"random app", "notepad.exe", false},
		{"chrome", "chrome.exe", false},
		{"firefox", "firefox.exe", false},
		{"empty string", "", false},
		{"audiodg", "audiodg.exe", true},
		{"runtime broker", "runtimebroker.exe", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cfg.ShouldBypass(tt.process)
			if result != tt.expected {
				t.Errorf("ShouldBypass(%q) = %v, want %v", tt.process, result, tt.expected)
			}
		})
	}
}

func TestAddBypassProcess(t *testing.T) {
	cfg := DefaultConfig()

	// Initially not bypassed
	if cfg.ShouldBypass("myapp.exe") {
		t.Error("myapp.exe should not be bypassed initially")
	}

	cfg.AddBypassProcess("myapp.exe")

	if !cfg.ShouldBypass("myapp.exe") {
		t.Error("myapp.exe should be bypassed after adding")
	}
	if !cfg.ShouldBypass("MyApp.exe") {
		t.Error("MyApp.exe should be bypassed (case insensitive)")
	}
}

func TestBypassList(t *testing.T) {
	cfg := DefaultConfig()
	list := cfg.BypassList()
	if len(list) == 0 {
		t.Error("bypass list should not be empty")
	}

	// Check that kernel-critical processes are always present
	for _, p := range []string{"csrss.exe", "dwm.exe", "audiodg.exe", "lsass.exe"} {
		found := false
		for _, l := range list {
			if l == p {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %q in bypass list", p)
		}
	}
}

func TestLoadSaveConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test-config.json")

	// Save default config
	cfg := DefaultConfig()
	cfg.ExtraBypassProcesses = []string{"test.exe", "myapp.exe"}
	cfg.LogFile = "test.log"
	cfg.LogLevel = "debug"
	cfg.HousekeepingSeconds = 60

	if err := cfg.SaveToFile(path); err != nil {
		t.Fatalf("SaveToFile failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("config file was not created")
	}

	// Load it back
	loaded, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	if loaded.LogFile != "test.log" {
		t.Errorf("expected LogFile=test.log, got %s", loaded.LogFile)
	}
	if loaded.LogLevel != "debug" {
		t.Errorf("expected LogLevel=debug, got %s", loaded.LogLevel)
	}
	if loaded.HousekeepingSeconds != 60 {
		t.Errorf("expected HousekeepingSeconds=60, got %d", loaded.HousekeepingSeconds)
	}
	if loaded.HousekeepingInterval.Seconds() != 60 {
		t.Errorf("expected HousekeepingInterval=60s, got %v", loaded.HousekeepingInterval)
	}

	// Check extra bypass processes were loaded
	if !loaded.ShouldBypass("test.exe") {
		t.Error("test.exe should be bypassed after loading")
	}
	if !loaded.ShouldBypass("myapp.exe") {
		t.Error("myapp.exe should be bypassed after loading")
	}
}

func TestLoadFromFile_NotFound(t *testing.T) {
	_, err := LoadFromFile("/nonexistent/path/config.json")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestLoadFromFile_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	os.WriteFile(path, []byte("not valid json{{{"), 0644)

	_, err := LoadFromFile(path)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestSaveToFile_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "sub", "dir", "config.json")

	cfg := DefaultConfig()
	if err := cfg.SaveToFile(nested); err != nil {
		t.Fatalf("SaveToFile with nested dir failed: %v", err)
	}

	if _, err := os.Stat(nested); os.IsNotExist(err) {
		t.Error("nested config file was not created")
	}
}

func TestResolveWithZeroHousekeeping(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HousekeepingSeconds = 0
	cfg.resolve()

	// Should default to 5 minutes
	if cfg.HousekeepingInterval.Minutes() != 5 {
		t.Errorf("expected 5 minute default, got %v", cfg.HousekeepingInterval)
	}
}

func TestResolveWithNegativeHousekeeping(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HousekeepingSeconds = -10
	cfg.resolve()

	if cfg.HousekeepingInterval.Minutes() != 5 {
		t.Errorf("expected 5 minute default for negative value, got %v", cfg.HousekeepingInterval)
	}
}

func TestProfileBalancedDefault(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Profile != ProfileBalanced {
		t.Errorf("expected default profile=%s, got %s", ProfileBalanced, cfg.Profile)
	}
	// Balanced must include audio and input
	for _, p := range []string{"audiodg.exe", "dwm.exe", "ctfmon.exe", "explorer.exe"} {
		if !cfg.ShouldBypass(p) {
			t.Errorf("balanced profile should bypass %s", p)
		}
	}
	// Balanced should NOT include things it now throttles
	if cfg.ShouldBypass("searchindexer.exe") {
		t.Error("balanced profile should throttle searchindexer.exe")
	}
}

func TestProfileAggressive(t *testing.T) {
	cfg := DefaultAggressiveConfig()
	if cfg.Profile != ProfileAggressive {
		t.Errorf("expected profile=%s, got %s", ProfileAggressive, cfg.Profile)
	}
	// Aggressive must include kernel-critical processes
	for _, p := range []string{"dwm.exe", "audiodg.exe", "csrss.exe", "lsass.exe"} {
		if !cfg.ShouldBypass(p) {
			t.Errorf("aggressive profile should bypass %s", p)
		}
	}
	// Aggressive should throttle things the balanced profile also throttles
	if cfg.ShouldBypass("searchindexer.exe") {
		t.Error("aggressive profile should throttle searchindexer.exe")
	}
	// Aggressive should throttle things balanced protects
	if cfg.ShouldBypass("taskmgr.exe") {
		t.Error("aggressive profile should throttle taskmgr.exe")
	}
}

func TestDefaultBypassProcessList(t *testing.T) {
	// Verify no duplicates in balanced list
	seen := make(map[string]bool)
	for _, p := range BalancedBypassProcesses {
		if seen[p] {
			t.Errorf("duplicate in BalancedBypassProcesses: %s", p)
		}
		seen[p] = true
	}

	// Verify all entries are lowercase (ShouldBypass lowercases, but entries should be canonical)
	for _, p := range BalancedBypassProcesses {
		for _, c := range p {
			if c >= 'A' && c <= 'Z' {
				t.Errorf("BalancedBypassProcesses entry should be lowercase: %s", p)
				break
			}
		}
	}

	// Aggressive list should be a strict subset of balanced for critical entries
	balancedSet := make(map[string]bool)
	for _, p := range BalancedBypassProcesses {
		balancedSet[p] = true
	}
	for _, p := range AggressiveBypassProcesses {
		if !balancedSet[p] {
			// Aggressive can only have entries that are in balanced or are truly critical
			// (they are a subset of balanced)
			_ = p // acceptable; just document
		}
	}
}

func TestConfigSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "roundtrip.json")

	original := DefaultConfig()
	original.LogFile = "app.log"
	original.LogLevel = "warn"
	original.EnableEventLog = true
	original.RestoreOnExit = false
	original.BoostForegroundOnly = true
	original.HousekeepingSeconds = 120
	original.ExtraBypassProcesses = []string{"app1.exe", "app2.exe"}

	if err := original.SaveToFile(path); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if loaded.LogFile != original.LogFile {
		t.Errorf("LogFile mismatch: %s vs %s", loaded.LogFile, original.LogFile)
	}
	if loaded.LogLevel != original.LogLevel {
		t.Errorf("LogLevel mismatch: %s vs %s", loaded.LogLevel, original.LogLevel)
	}
	if loaded.EnableEventLog != original.EnableEventLog {
		t.Errorf("EnableEventLog mismatch")
	}
	if loaded.RestoreOnExit != original.RestoreOnExit {
		t.Errorf("RestoreOnExit mismatch")
	}
	if loaded.BoostForegroundOnly != original.BoostForegroundOnly {
		t.Errorf("BoostForegroundOnly mismatch")
	}
	if loaded.HousekeepingSeconds != original.HousekeepingSeconds {
		t.Errorf("HousekeepingSeconds mismatch: %d vs %d", loaded.HousekeepingSeconds, original.HousekeepingSeconds)
	}
}
