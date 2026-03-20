// SPDX-License-Identifier: GPL-2.0-only
// SPDX-FileCopyrightText: 2024 Kyle Sanderson

//go:build windows

// EnergyStarGo - EcoQoS Process Throttler for Windows 11
//
// Automatically throttles background processes using Windows 11 EcoQoS
// (Efficiency Mode) for improved battery life and thermal management.
//
// Usage:
//
//	energystar run          Run in foreground (interactive mode)
//	energystar run --tray   Run with system tray icon
//	energystar install      Install as a Windows service
//	energystar uninstall    Remove the Windows service
//	energystar start        Start the installed service
//	energystar status       Query service status
//	energystar config       Generate default configuration file
//	energystar version      Print version information
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/KyleSanderson/EnergyStarGo/internal/config"
	"github.com/KyleSanderson/EnergyStarGo/internal/logger"
	"github.com/KyleSanderson/EnergyStarGo/internal/service"
	"github.com/KyleSanderson/EnergyStarGo/internal/throttle"
	"github.com/KyleSanderson/EnergyStarGo/internal/tray"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	// Check if running as a Windows service
	isSvc, _ := svc.IsWindowsService()
	if isSvc {
		runAsService()
		return
	}

	command := os.Args[1]
	switch command {
	case "run":
		cmdRun()
	case "install":
		cmdInstall()
	case "uninstall":
		cmdUninstall()
	case "start":
		cmdStart()
	case "status":
		cmdStatus()
	case "config":
		cmdConfig()
	case "version":
		cmdVersion()
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`EnergyStarGo - EcoQoS Process Throttler for Windows 11

Usage: energystar <command> [flags]

Commands:
  run          Run in foreground (interactive mode)
  install      Install as a Windows service
  uninstall    Remove the Windows service
  start        Start the installed service
  status       Query service status
  config       Generate default configuration file
  version      Print version information

Run Flags:
  --tray                  Show system tray icon
  --config <path>         Path to configuration file
  --log-file <path>       Path to log file
  --log-level <level>     Log level: debug, info, warn, error (default: info)
  --housekeeping <secs>   Housekeeping interval in seconds (default: 300)
  --restore-on-exit       Restore process priorities on exit (default: true)
  --bypass <proc,...>     Additional processes to bypass (comma-separated)
  --profile <name>        Throttle profile: balanced (default) or aggressive
  --no-tray               Disable system tray even if available
  --verbose               Enable verbose/debug logging`)
}

func cmdRun() {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	showTray := fs.Bool("tray", false, "Show system tray icon")
	configPath := fs.String("config", "", "Path to configuration file")
	logFile := fs.String("log-file", "", "Path to log file")
	logLevel := fs.String("log-level", "info", "Log level: debug, info, warn, error")
	housekeeping := fs.Int("housekeeping", 0, "Housekeeping interval in seconds (0 = use config/profile default)")
	restoreOnExit := fs.Bool("restore-on-exit", true, "Restore process priorities on exit")
	bypassExtra := fs.String("bypass", "", "Additional processes to bypass (comma-separated)")
	profileFlag := fs.String("profile", "", "Throttle profile: balanced or aggressive")
	verbose := fs.Bool("verbose", false, "Enable verbose/debug logging")
	_ = fs.Bool("no-tray", false, "Disable system tray")

	if err := fs.Parse(os.Args[2:]); err != nil {
		os.Exit(1)
	}

	// Load config first so MinBuildNumber may be taken from it.
	cfg := loadConfig(*configPath)

	// Apply CLI overrides before version check (profile doesn't affect version check,
	// but min_build_number may have been in the config file).
	if *profileFlag != "" {
		cfg.Profile = config.Profile(*profileFlag)
	}
	if *logFile != "" {
		cfg.LogFile = *logFile
	}
	if *verbose {
		cfg.LogLevel = "debug"
	} else if *logLevel != "info" {
		cfg.LogLevel = *logLevel
	}
	if *housekeeping != 0 {
		cfg.HousekeepingSeconds = *housekeeping
		cfg.HousekeepingInterval = time.Duration(*housekeeping) * time.Second
	}
	cfg.RestoreOnExit = *restoreOnExit

	if *bypassExtra != "" {
		for _, p := range splitAndTrim(*bypassExtra) {
			cfg.AddBypassProcess(p)
		}
	}

	// Check Windows 11 (uses MinBuildNumber from config, default 22000).
	if err := checkWindowsVersion(cfg.MinBuildNumber); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger
	log, err := logger.New(cfg.LogFile, cfg.LogLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	if cfg.EnableEventLog {
		if err := log.EnableEventLog("EnergyStarGo"); err != nil {
			log.Warn("failed to enable Windows Event Log", "error", err)
		}
	}

	log.Info("EnergyStarGo starting",
		"version", Version,
		"build", BuildTime,
		"pid", os.Getpid(),
	)

	// Create throttle engine
	engine := throttle.New(cfg, log)

	// Initial throttle sweep — skipped in boost_foreground_only mode.
	if !cfg.BoostForegroundOnly {
		count := engine.ThrottleAllUserBackgroundProcesses()
		log.Info("initial sweep complete", "processes_throttled", count)
	} else {
		log.Info("boost_foreground_only mode: skipping initial sweep")
	}

	// Start housekeeping goroutine — skipped in boost_foreground_only mode.
	var wg sync.WaitGroup
	stopHK := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		if cfg.BoostForegroundOnly {
			// In this mode we only boost the foreground; no periodic sweeps.
			<-stopHK
			return
		}
		ticker := time.NewTicker(cfg.HousekeepingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				engine.ThrottleAllUserBackgroundProcesses()
			case <-stopHK:
				return
			}
		}
	}()

	// Set up signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Optionally start system tray
	if *showTray {
		callbacks := tray.TrayCallbacks{
			OnPause: func() {
				engine.SetPaused(true)
				log.Info("throttling paused via tray")
			},
			OnResume: func() {
				engine.SetPaused(false)
				if !cfg.BoostForegroundOnly {
					engine.ThrottleAllUserBackgroundProcesses()
				}
				log.Info("throttling resumed via tray")
			},
			OnRestore: func() {
				restored := engine.RestoreAllProcesses()
				log.Info("processes restored via tray", "count", restored)
			},
			OnExit: func() {
				select {
				case sigCh <- syscall.SIGTERM:
				default:
				}
			},
			GetStats: func() string {
				s := engine.Stats()
				return fmt.Sprintf("Throttled: %d | Boosted: %d | Sweeps: %d",
					s.TotalThrottled, s.TotalBoosted, s.HousekeepingRuns)
			},
			IsPaused: engine.IsPaused,
		}

		trayIcon := tray.New(log, callbacks)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := trayIcon.Run(); err != nil {
				log.Error("tray failed", "error", err)
			}
		}()

		// Start message loop in main goroutine
		go func() {
			<-sigCh
			log.Info("shutdown signal received")
			engine.Stop()
			trayIcon.Stop()
			close(stopHK)
		}()

		// The engine message loop needs the main thread
		engine.RunMessageLoop()
	} else {
		// No tray: run message loop in main goroutine
		go func() {
			<-sigCh
			log.Info("shutdown signal received")
			engine.Stop()
			close(stopHK)
		}()

		fmt.Println("EnergyStarGo is running. Press Ctrl+C to stop.")
		engine.RunMessageLoop()
	}

	// Cleanup
	if cfg.RestoreOnExit {
		restored := engine.RestoreAllProcesses()
		log.Info("restored processes on exit", "count", restored)
	}

	wg.Wait()
	log.Info("EnergyStarGo stopped")
}

func cmdInstall() {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to configuration file for service")
	if err := fs.Parse(os.Args[2:]); err != nil {
		os.Exit(1)
	}

	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get executable path: %v\n", err)
		os.Exit(1)
	}
	exe, _ = filepath.Abs(exe)

	var args []string
	if *configPath != "" {
		absConfig, _ := filepath.Abs(*configPath)
		args = append(args, "--config", absConfig)
	}

	if err := service.Install(exe, args); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to install service: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Service installed successfully.")
	fmt.Println("Run 'energystar start' to start the service.")
}

func cmdUninstall() {
	if err := service.Uninstall(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to uninstall service: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Service uninstalled successfully.")
}

func cmdStart() {
	if err := service.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start service: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Service started.")
}

func cmdStatus() {
	status, err := service.QueryStatus()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to query service status: %v\n", err)
		os.Exit(1)
	}

	stateStr := "Unknown"
	switch status.State {
	case svc.Stopped:
		stateStr = "Stopped"
	case svc.StartPending:
		stateStr = "Start Pending"
	case svc.StopPending:
		stateStr = "Stop Pending"
	case svc.Running:
		stateStr = "Running"
	case svc.ContinuePending:
		stateStr = "Continue Pending"
	case svc.PausePending:
		stateStr = "Pause Pending"
	case svc.Paused:
		stateStr = "Paused"
	}

	fmt.Printf("Service: %s\n", service.ServiceName)
	fmt.Printf("Status:  %s\n", stateStr)
}

func cmdConfig() {
	fs := flag.NewFlagSet("config", flag.ExitOnError)
	output := fs.String("output", "", "Output path (default: energystar.json next to exe)")
	if err := fs.Parse(os.Args[2:]); err != nil {
		os.Exit(1)
	}

	path := *output
	if path == "" {
		path = config.DefaultConfigPath()
	}

	cfg := config.DefaultConfig()
	if err := cfg.SaveToFile(path); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Default configuration written to: %s\n", path)

	// Also print to stdout for reference
	data, _ := json.MarshalIndent(cfg, "", "  ")
	fmt.Println(string(data))
}

func cmdVersion() {
	fmt.Printf("EnergyStarGo %s\n", Version)
	fmt.Printf("  Build:    %s\n", BuildTime)
	fmt.Printf("  Commit:   %s\n", GitCommit)
	fmt.Printf("  Go:       %s\n", runtime.Version())
	fmt.Printf("  OS/Arch:  %s/%s\n", runtime.GOOS, runtime.GOARCH)
}

func runAsService() {
	cfg := loadConfigForService()

	log, err := logger.New(cfg.LogFile, cfg.LogLevel)
	if err != nil {
		// Can't log, just exit
		os.Exit(1)
	}

	if cfg.EnableEventLog {
		if err := log.EnableEventLog("EnergyStarGo"); err != nil {
			log.Warn("failed to enable Windows Event Log", "error", err)
		}
	}

	log.Info("starting as Windows service")

	if err := service.Run(cfg, log); err != nil {
		log.Error("service failed", "error", err)
		os.Exit(1)
	}
}

func checkWindowsVersion(minBuild uint32) error {
	if minBuild == 0 {
		minBuild = 22000
	}
	ver := windows.RtlGetVersion()
	if ver.BuildNumber < minBuild {
		return fmt.Errorf(
			"Windows build %d or later is required. Current build: %d. "+
				"EcoQoS/Efficiency Mode is only available on Windows 11 (build 22000+)",
			minBuild, ver.BuildNumber,
		)
	}
	return nil
}

func loadConfig(path string) *config.Config {
	if path != "" {
		cfg, err := config.LoadFromFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to load config from %s: %v. Using defaults.\n", path, err)
			return config.DefaultConfig()
		}
		return cfg
	}

	// Try default location
	defaultPath := config.DefaultConfigPath()
	if _, err := os.Stat(defaultPath); err == nil {
		cfg, err := config.LoadFromFile(defaultPath)
		if err == nil {
			return cfg
		}
	}

	return config.DefaultConfig()
}

func loadConfigForService() *config.Config {
	// Check if --config was passed as a service argument
	for i, arg := range os.Args {
		if arg == "--config" && i+1 < len(os.Args) {
			cfg, err := config.LoadFromFile(os.Args[i+1])
			if err == nil {
				return cfg
			}
		}
	}
	return loadConfig("")
}

func splitAndTrim(s string) []string {
	var result []string
	for _, part := range splitString(s, ',') {
		trimmed := trimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func splitString(s string, sep byte) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func trimSpace(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}
