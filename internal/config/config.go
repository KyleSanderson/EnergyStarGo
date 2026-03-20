// SPDX-License-Identifier: GPL-2.0-only
// SPDX-FileCopyrightText: 2024 Kyle Sanderson

// Package config provides configuration management for EnergyStarGo.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Profile controls how aggressively background processes are throttled.
type Profile string

const (
	// ProfileBalanced is the default: protects responsiveness (audio, input,
	// shell, common services) while throttling everything else.
	ProfileBalanced Profile = "balanced"

	// ProfileAggressive maximises battery savings; only the bare minimum set of
	// kernel / realtime processes is exempted.
	ProfileAggressive Profile = "aggressive"
)

// ScheduleEntry defines a time window with an active profile.
type ScheduleEntry struct {
	From    string  `json:"from"` // 24h "HH:MM"
	To      string  `json:"to"`   // 24h "HH:MM"
	Profile Profile `json:"profile"`
}

// AutoProfileConfig controls automatic profile switching based on AC/battery state.
type AutoProfileConfig struct {
	Enabled   bool    `json:"enabled"`
	OnBattery Profile `json:"on_battery"` // default: "aggressive"
	OnAC      Profile `json:"on_ac"`      // default: "balanced"
}

// Config holds all application configuration.
type Config struct {
	// HousekeepingInterval is how often to re-scan and throttle background processes.
	HousekeepingInterval time.Duration `json:"-"`
	HousekeepingSeconds  int           `json:"housekeeping_interval_seconds"`

	// Profile selects the built-in bypass list. "balanced" (default) or "aggressive".
	Profile Profile `json:"profile"`

	// BypassProcesses overrides the built-in list when non-empty.
	BypassProcesses []string `json:"bypass_processes"`

	// ExtraBypassProcesses are user-added processes to also bypass on top of
	// whatever list the selected profile provides.
	ExtraBypassProcesses []string `json:"extra_bypass_processes"`

	// LogFile is the path to the log file. Empty means stderr only.
	LogFile string `json:"log_file"`

	// LogLevel is the minimum log level (debug, info, warn, error).
	LogLevel string `json:"log_level"`

	// EnableEventLog enables writing to the Windows Event Log.
	EnableEventLog bool `json:"enable_event_log"`

	// RestoreOnExit controls whether to restore all process priorities on exit.
	RestoreOnExit bool `json:"restore_on_exit"`

	// MinBuildNumber is the minimum Windows build required.
	MinBuildNumber uint32 `json:"min_build_number"`

	// BoostForegroundOnly if true, only boosts the foreground process without
	// throttling all background processes on startup.
	BoostForegroundOnly bool `json:"boost_foreground_only"`

	// Schedule defines time-based profile switching entries.
	Schedule []ScheduleEntry `json:"schedule"`

	// AutoProfile controls battery/AC-based automatic profile switching.
	AutoProfile AutoProfileConfig `json:"auto_profile"`

	// LowBatterySuspendPercent: suspend when battery ≤ this %. 0 = disabled.
	LowBatterySuspendPercent int `json:"low_battery_suspend_percent"`

	// IdleSuspendMinutes: suspend after this many idle minutes. 0 = disabled.
	IdleSuspendMinutes int `json:"idle_suspend_minutes"`

	// AutoStart controls whether EnergyStarGo launches at Windows startup.
	AutoStart bool `json:"auto_start"`

	// BatteryNotifications enables balloon notifications for battery events.
	BatteryNotifications bool `json:"battery_notifications"`

	// GPUThrottling enables GPU scheduling priority throttling for background
	// processes via D3DKMTSetProcessSchedulingPriorityClass.
	GPUThrottling bool `json:"gpu_throttling"`

	// EnableGameMode pauses throttling while a fullscreen application is in
	// the foreground (e.g. games, media players).
	EnableGameMode bool `json:"enable_game_mode"`

	// ThrottleOnDisplayOff switches to aggressive profile when the display
	// turns off (lid close, screen timeout, manual lock) and restores the
	// previous profile when it comes back on. Requires --tray mode.
	ThrottleOnDisplayOff bool `json:"throttle_on_display_off"`

	// IdleAggressiveMinutes: switch to aggressive profile after this many
	// minutes of keyboard/mouse inactivity. 0 = disabled.
	IdleAggressiveMinutes int `json:"idle_aggressive_minutes"`

	// RespectPowerPlan disables throttling when the active Windows power plan
	// is set to "High Performance".
	RespectPowerPlan bool `json:"respect_power_plan"`

	// MemoryPressureThresholdMB pauses throttling when available physical
	// memory drops below this many MB (throttled processes can swap more,
	// making memory pressure worse). 0 = disabled.
	MemoryPressureThresholdMB int `json:"memory_pressure_threshold_mb"`

	// DisableInVM exits immediately when a virtual machine is detected.
	DisableInVM bool `json:"disable_in_vm"`

	// BootDelaySeconds delays throttling until this many seconds after boot.
	// 0 = disabled.
	BootDelaySeconds int `json:"boot_delay_seconds"`

	// CustomProfiles maps profile name to a bypass process list. Allows
	// user-defined profiles beyond the built-in "balanced" and "aggressive".
	CustomProfiles map[string][]string `json:"custom_profiles"`

	// ThrottledAffinityMask, when non-zero, sets the CPU affinity of throttled
	// processes to this mask (e.g. 0xF0 to pin to cores 4-7). 0 = disabled.
	ThrottledAffinityMask uint64 `json:"throttled_affinity_mask"`

	// BypassWindowTitles bypasses processes whose foreground window title
	// contains any of these substrings (case-insensitive).
	BypassWindowTitles []string `json:"bypass_window_titles"`

	// resolved bypass set (lowercase names)
	bypassSet map[string]struct{}
}

// BalancedBypassProcesses is the default profile: protects responsiveness for
// typical developer / knowledge-worker laptop use while throttling background
// work (indexing, update notifications, widgets, etc.).
var BalancedBypassProcesses = []string{
	// Self
	"energystar.exe",

	// ── Kernel & session management (non-negotiable) ──────────────────────
	"smss.exe",    // Session Manager Subsystem
	"csrss.exe",   // Client/Server Runtime Subsystem
	"wininit.exe", // Windows Init
	"winlogon.exe",
	"lsass.exe", // Authentication & token management
	"services.exe",
	"svchost.exe", // Service host processes (audio, network, …)
	"wudfrd.exe",  // Windows Driver Foundation

	// ── Realtime / perceptible-latency critical ───────────────────────────
	"dwm.exe",     // Desktop Window Manager — composition & rendering
	"audiodg.exe", // Audio Device Graph — glitches are immediately audible

	// ── Input (keyboard, touch, pen) ─────────────────────────────────────
	"ctfmon.exe",        // Text Services Framework / IME
	"chsime.exe",        // CJK IME
	"inputapp.exe",      // Touch & pen input dispatcher
	"textinputhost.exe", // Touch keyboard host

	// ── Explorer shell & UI infrastructure ───────────────────────────────
	"explorer.exe",
	"shellexperiencehost.exe",
	"startmenuexperiencehost.exe",
	"applicationframehost.exe", // UWP app container
	"searchhost.exe",           // Start menu search
	"sihost.exe",               // Shell Infrastructure (system tray)
	"runtimebroker.exe",        // App activation & permission brokering

	// ── Core system utilities ─────────────────────────────────────────────
	"taskhostw.exe", // Task Scheduler host
	"wmiprvse.exe",  // WMI (battery status, hardware monitoring)
	"conhost.exe",   // Console host (terminal windows)
	"dllhost.exe",   // COM/OLE surrogate
	"taskmgr.exe",   // Task Manager
	"lockapp.exe",   // Lock screen

	// ── Windows Security ──────────────────────────────────────────────────
	"msmpeng.exe",  // Windows Defender real-time protection
	"mpcmdrun.exe", // Defender command runner
	"securityhealthservice.exe",
	"securityhealthsystray.exe",

	// ── System settings & notifications ──────────────────────────────────
	"systemsettings.exe",

	// ── FontdrvHost — needed during text rendering ────────────────────────
	"fontdrvhost.exe",

	// ── Debugging / sysadmin tools ────────────────────────────────────────
	"procmon.exe",
	"procmon64.exe",

	// ── Audio / video / communication ─────────────────────────────────────
	"discord.exe",
	"teams.exe",
	"ms-teams.exe",
	"zoom.exe",
	"slack.exe",
	"skype.exe",
	"obs64.exe",
	"obs32.exe",
	"vlc.exe",
	"mpc-hc64.exe",
	"spotify.exe",
	"wmplayer.exe",
	"foobar2000.exe",
	"musicbee.exe",
}

// AggressiveBypassProcesses is the minimum list for maximum battery savings.
// Only realtime / kernel-critical processes are exempted; everything else
// (indexing, update notifications, widgets, print spooler, …) is throttled.
var AggressiveBypassProcesses = []string{
	// Self
	"energystar.exe",

	// ── Kernel (absolute minimum) ─────────────────────────────────────────
	"smss.exe",
	"csrss.exe",
	"wininit.exe",
	"winlogon.exe",
	"lsass.exe",
	"services.exe",
	"svchost.exe",

	// ── Realtime critical ─────────────────────────────────────────────────
	"dwm.exe",
	"audiodg.exe",

	// ── Input ─────────────────────────────────────────────────────────────
	"ctfmon.exe",
	"inputapp.exe",

	// ── Shell (minimal) ───────────────────────────────────────────────────
	"explorer.exe",
	"applicationframehost.exe",

	// ── Security ──────────────────────────────────────────────────────────
	"msmpeng.exe",
	"lsass.exe",
}

// bypassListForProfile returns the built-in bypass list for the given profile.
func bypassListForProfile(p Profile) []string {
	if p == ProfileAggressive {
		return AggressiveBypassProcesses
	}
	return BalancedBypassProcesses
}

// DefaultConfig returns a Config with sensible defaults (Balanced profile).
func DefaultConfig() *Config {
	c := &Config{
		HousekeepingSeconds:  300, // 5 minutes
		Profile:              ProfileBalanced,
		BypassProcesses:      nil, // nil → use profile list
		ExtraBypassProcesses: nil,
		LogFile:              "",
		LogLevel:             "info",
		EnableEventLog:       false,
		RestoreOnExit:        true,
		MinBuildNumber:       22000,
		BoostForegroundOnly:  false,
	}
	c.resolve()
	return c
}

// DefaultAggressiveConfig returns a Config tuned for maximum battery savings.
func DefaultAggressiveConfig() *Config {
	c := DefaultConfig()
	c.Profile = ProfileAggressive
	c.HousekeepingSeconds = 120 // sweep more often so newly-spawned processes are caught quickly
	c.resolve()
	return c
}

// effectiveBypassList returns the active bypass list in priority order:
//  1. Explicit BypassProcesses field (user override)
//  2. Profile-selected built-in list
func (c *Config) effectiveBypassList() []string {
	if len(c.BypassProcesses) > 0 {
		return c.BypassProcesses
	}
	if c.CustomProfiles != nil {
		if list, ok := c.CustomProfiles[string(c.Profile)]; ok {
			return list
		}
	}
	return bypassListForProfile(c.Profile)
}

// resolve computes derived fields.
func (c *Config) resolve() {
	if c.Profile == "" {
		c.Profile = ProfileBalanced
	}
	c.HousekeepingInterval = time.Duration(c.HousekeepingSeconds) * time.Second
	if c.HousekeepingInterval <= 0 {
		c.HousekeepingInterval = 5 * time.Minute
	}

	base := c.effectiveBypassList()
	c.bypassSet = make(map[string]struct{}, len(base)+len(c.ExtraBypassProcesses))
	for _, p := range base {
		c.bypassSet[strings.ToLower(p)] = struct{}{}
	}
	for _, p := range c.ExtraBypassProcesses {
		c.bypassSet[strings.ToLower(p)] = struct{}{}
	}
}

// Resolve re-computes derived fields. It is the public counterpart of resolve.
func (c *Config) Resolve() { c.resolve() }

// ShouldBypass returns true if the given process name (case-insensitive) is in the bypass list.
func (c *Config) ShouldBypass(processName string) bool {
	_, ok := c.bypassSet[strings.ToLower(processName)]
	return ok
}

// AddBypassProcess adds a process name to the bypass set at runtime.
func (c *Config) AddBypassProcess(name string) {
	c.ExtraBypassProcesses = append(c.ExtraBypassProcesses, name)
	c.bypassSet[strings.ToLower(name)] = struct{}{}
}

// LoadFromFile loads configuration from a JSON file, merging with defaults.
func LoadFromFile(path string) (*Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	cfg.resolve()
	return cfg, nil
}

// SaveToFile writes the current configuration to a JSON file.
func (c *Config) SaveToFile(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// DefaultConfigPath returns the default config file path next to the executable.
func DefaultConfigPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "energystar.json"
	}
	return filepath.Join(filepath.Dir(exe), "energystar.json")
}

// BypassList returns a copy of all bypass process names.
func (c *Config) BypassList() []string {
	result := make([]string, 0, len(c.bypassSet))
	for k := range c.bypassSet {
		result = append(result, k)
	}
	return result
}
