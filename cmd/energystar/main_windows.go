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
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/KyleSanderson/EnergyStarGo/internal/autostart"
	"github.com/KyleSanderson/EnergyStarGo/internal/config"
	"github.com/KyleSanderson/EnergyStarGo/internal/logger"
	"github.com/KyleSanderson/EnergyStarGo/internal/power"
	"github.com/KyleSanderson/EnergyStarGo/internal/scheduler"
	"github.com/KyleSanderson/EnergyStarGo/internal/service"
	"github.com/KyleSanderson/EnergyStarGo/internal/throttle"
	"github.com/KyleSanderson/EnergyStarGo/internal/tray"
	"github.com/KyleSanderson/EnergyStarGo/internal/uac"
	"github.com/KyleSanderson/EnergyStarGo/internal/winapi"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"

	// consoleCtrlHandler holds the Win32 console ctrl callback to prevent GC.
	consoleCtrlHandler uintptr
)

func main() {
	// Check if running as a Windows service FIRST — the SCM starts us with
	// no subcommand, so we must detect this before checking os.Args.
	isSvc, _ := svc.IsWindowsService()
	if isSvc {
		runAsService()
		return
	}

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	// install, uninstall, start, stop require admin privileges — auto-elevate.
	switch command {
	case "install", "uninstall", "start", "stop":
		if !uac.IsElevated() {
			if err := uac.Elevate(os.Args[1:]...); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to elevate: %v\n", err)
				os.Exit(1)
			}
			os.Exit(0) // new elevated process is now running
		}
	}

	switch command {
	case "run":
		cmdRun()
	case "companion":
		cmdCompanion()
	case "install":
		cmdInstall()
	case "uninstall":
		cmdUninstall()
	case "start":
		cmdStart()
	case "stop":
		cmdStop()
	case "status":
		cmdStatus()
	case "config":
		cmdConfig()
	case "version":
		cmdVersion()
	case "bypass-list":
		cmdBypassList()
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
  stop         Stop the running service
  status       Query service status
  config       Generate default configuration file
  bypass-list  Print the effective bypass process list
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

	// ── VM detection ─────────────────────────────────────────────────────────
	if cfg.DisableInVM && isVirtualMachine() {
		log.Info("virtual machine detected, exiting per disable_in_vm setting")
		return
	}

	// ── Boot delay ───────────────────────────────────────────────────────────
	if cfg.BootDelaySeconds > 0 {
		uptimeMs := winapi.GetTickCount64()
		thresholdMs := uint64(cfg.BootDelaySeconds) * 1000
		if uptimeMs < thresholdMs {
			remaining := time.Duration(thresholdMs-uptimeMs) * time.Millisecond
			log.Info("boot delay: waiting before starting throttling",
				"remaining_seconds", int(remaining.Seconds()),
				"boot_delay_seconds", cfg.BootDelaySeconds)
			time.Sleep(remaining)
		}
	}

	// Create throttle engine
	engine := throttle.New(cfg, log)

	// Initial throttle sweep — skipped in boost_foreground_only mode.
	if !cfg.BoostForegroundOnly {
		count := engine.ThrottleAllUserBackgroundProcesses()
		log.Info("initial sweep complete", "processes_throttled", count)
	} else {
		log.Info("boost_foreground_only mode: skipping initial sweep")
	}

	// Sync auto-start registry setting with config
	if cfg.AutoStart {
		if enabled, err := autostart.IsEnabled(); err == nil && !enabled {
			if err := autostart.Enable(); err != nil {
				log.Warn("failed to enable auto-start", "error", err)
			}
		}
	}

	var wg sync.WaitGroup
	stopHK := make(chan struct{})
	var trayIcon *tray.Tray

	// ── Auto-profile based on AC/battery state ──────────────────────────────
	if cfg.AutoProfile.Enabled {
		onBattery := cfg.AutoProfile.OnBattery
		if onBattery == "" {
			onBattery = config.ProfileAggressive
		}
		onAC := cfg.AutoProfile.OnAC
		if onAC == "" {
			onAC = config.ProfileBalanced
		}

		// Set initial profile based on current power state
		if power.IsOnBattery() {
			cfg.SetProfile(onBattery)
			log.Info("auto-profile: on battery", "profile", string(onBattery))
		} else {
			cfg.SetProfile(onAC)
			log.Info("auto-profile: on AC", "profile", string(onAC))
		}

		// Power state changes are now handled by event-driven notifications from the engine
		// No polling timer needed
	}

	// ── Idle-time adaptive throttling ───────────────────────────────────────
	if cfg.IdleAggressiveMinutes > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			thresholdSec := uint32(cfg.IdleAggressiveMinutes * 60)
			var wasIdle bool
			var savedProfile config.Profile
			for {
				select {
				case <-ticker.C:
					idle := power.IdleSeconds() >= thresholdSec
					if idle && !wasIdle {
						wasIdle = true
						savedProfile = cfg.GetProfile()
						cfg.SetProfile(config.ProfileAggressive)
						log.Info("idle-throttle: user idle, switching to aggressive profile",
							"idle_minutes", cfg.IdleAggressiveMinutes)
						if !cfg.BoostForegroundOnly {
							engine.ThrottleAllUserBackgroundProcesses()
						}
					} else if !idle && wasIdle {
						wasIdle = false
						cfg.SetProfile(savedProfile)
						log.Info("idle-throttle: user active, restoring profile",
							"profile", string(savedProfile))
					}
				case <-stopHK:
					return
				}
			}
		}()
	}

	// ── Memory pressure awareness ───────────────────────────────────────────
	if cfg.MemoryPressureThresholdMB > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			var memoryPaused bool
			for {
				select {
				case <-ticker.C:
					ms, err := winapi.GetMemoryStatus()
					if err != nil {
						log.Debug("memory-pressure: failed to query memory status", "error", err)
						continue
					}
					availMB := ms.AvailPhys / (1024 * 1024)
					belowThreshold := availMB < uint64(cfg.MemoryPressureThresholdMB)
					if belowThreshold && !memoryPaused {
						memoryPaused = true
						engine.SetPaused(true)
						log.Warn("memory-pressure: available memory below threshold, pausing throttling",
							"avail_mb", availMB,
							"threshold_mb", cfg.MemoryPressureThresholdMB)
					} else if !belowThreshold && memoryPaused {
						memoryPaused = false
						engine.SetPaused(false)
						log.Info("memory-pressure: memory recovered, resuming throttling",
							"avail_mb", availMB,
							"threshold_mb", cfg.MemoryPressureThresholdMB)
						if !cfg.BoostForegroundOnly {
							engine.ThrottleAllUserBackgroundProcesses()
						}
					}
				case <-stopHK:
					return
				}
			}
		}()
	}

	// ── Scheduled profile switching ─────────────────────────────────────────
	var sched *scheduler.Scheduler
	if len(cfg.Schedule) > 0 {
		entries := make([]scheduler.Entry, len(cfg.Schedule))
		for i, e := range cfg.Schedule {
			entries[i] = scheduler.Entry{From: e.From, To: e.To, Profile: e.Profile}
		}
		sched = scheduler.New(log, entries, func(p config.Profile) {
			cfg.SetProfile(p)
			log.Info("scheduled profile switch", "profile", string(p))
			if cfg.BatteryNotifications && trayIcon != nil {
				trayIcon.ShowNotification("EnergyStarGo", fmt.Sprintf("Profile switched to %s (scheduled)", p))
			}
		})
		sched.Start()
		log.Info("schedule active", "entries", len(entries))
	}

	// Start housekeeping goroutine — skipped in boost_foreground_only mode.
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

	// Set up signal handling for graceful shutdown.
	// doneCh is closed exactly once to initiate shutdown from any source.
	doneCh := make(chan struct{})
	var doneOnce sync.Once
	triggerShutdown := func() { doneOnce.Do(func() { close(doneCh) }) }

	// Go's signal.Notify for portability (terminal SIGINT/SIGTERM).
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case <-sigCh:
			triggerShutdown()
		case <-doneCh:
		}
	}()

	// Also register a direct Win32 console ctrl handler. This ensures Ctrl+C
	// works even when window message loops might interfere with Go's signal
	// relay. The handler returns 0 so the event also chains to Go's internal
	// handler (belt-and-suspenders).
	consoleCtrlHandler = syscall.NewCallback(func(ctrlType uint32) uintptr {
		triggerShutdown()
		return 0 // let Go's runtime handler also see it
	})
	_ = winapi.SetConsoleCtrlHandler(consoleCtrlHandler, true)

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
				triggerShutdown()
			},
			GetStats: func() string {
				s := engine.Stats()
				return fmt.Sprintf("Throttled: %d | Boosted: %d | Sweeps: %d",
					s.TotalThrottled, s.TotalBoosted, s.HousekeepingRuns)
			},
			IsPaused: engine.IsPaused,
			OnInstallService: func() {
				if !uac.IsElevated() {
					if err := uac.Elevate("install"); err != nil {
						log.Error("failed to elevate for install", "error", err)
					}
					return
				}
				exe, _ := os.Executable()
				if err := service.Install(exe, nil); err != nil {
					log.Error("failed to install service", "error", err)
				} else {
					log.Info("service installed successfully")
					if cfg.BatteryNotifications {
						trayIcon.ShowNotification("EnergyStarGo", "Service installed successfully")
					}
				}
			},
			OnUninstallService: func() {
				if !uac.IsElevated() {
					if err := uac.Elevate("uninstall"); err != nil {
						log.Error("failed to elevate for uninstall", "error", err)
					}
					return
				}
				if err := service.Uninstall(); err != nil {
					log.Error("failed to uninstall service", "error", err)
				} else {
					log.Info("service uninstalled")
				}
			},
			OnStartService: func() {
				if !uac.IsElevated() {
					if err := uac.Elevate("start"); err != nil {
						log.Error("failed to elevate for start", "error", err)
					}
					return
				}
				if err := service.Start(); err != nil {
					log.Error("failed to start service", "error", err)
				} else {
					log.Info("service started")
				}
			},
			OnStopService: func() {
				if !uac.IsElevated() {
					if err := uac.Elevate("stop"); err != nil {
						log.Error("failed to elevate for stop", "error", err)
					}
					return
				}
				if err := service.Stop(); err != nil {
					log.Error("failed to stop service", "error", err)
				} else {
					log.Info("service stopped")
				}
			},
			GetServiceStatus: func() string {
				st, err := service.QueryStatus()
				if err != nil {
					return "Not installed"
				}
				switch st.State {
				case svc.Running:
					return "Running"
				case svc.Stopped:
					return "Stopped"
				default:
					return "Pending"
				}
			},
			OnToggleAutoStart: func() {
				if enabled, err := autostart.IsEnabled(); err == nil {
					if enabled {
						if err := autostart.Disable(); err != nil {
							log.Error("failed to disable auto-start", "error", err)
						} else {
							log.Info("auto-start disabled")
						}
					} else {
						if err := autostart.Enable(); err != nil {
							log.Error("failed to enable auto-start", "error", err)
						} else {
							log.Info("auto-start enabled")
						}
					}
				}
			},
			IsAutoStartEnabled: func() bool {
				enabled, _ := autostart.IsEnabled()
				return enabled
			},
			IsElevated: uac.IsElevated,
			OnRestartElevated: func() {
				// Re-launch with the same flags, elevated via UAC
				args := []string{"run", "--tray"}
				if err := uac.Elevate(args...); err != nil {
					log.Error("failed to elevate", "error", err)
					return
				}
				// Elevated instance is running — shut down the current one
				triggerShutdown()
			},
			OnSetProfile: func(profile string) {
				cfg.SetProfile(config.Profile(profile))
				if !cfg.BoostForegroundOnly {
					engine.ThrottleAllUserBackgroundProcesses()
				}
				log.Info("profile switched via tray", "profile", profile)
				if trayIcon != nil {
					trayIcon.ShowNotification("EnergyStarGo", fmt.Sprintf("Profile: %s", profile))
				}
			},
			GetProfile: func() string {
				return string(cfg.GetProfile())
			},
			OnPowerSourceChange: func(onAC bool) {
				if !cfg.AutoProfile.Enabled {
					return
				}
				onBatteryProfile := cfg.AutoProfile.OnBattery
				if onBatteryProfile == "" {
					onBatteryProfile = config.ProfileAggressive
				}
				onACProfile := cfg.AutoProfile.OnAC
				if onACProfile == "" {
					onACProfile = config.ProfileBalanced
				}

				selected := onBatteryProfile
				if onAC {
					selected = onACProfile
				}

				cfg.SetProfile(selected)
				powerSource := "battery"
				if onAC {
					powerSource = "AC"
				}
				log.Info("auto-profile event", "power_source", powerSource, "profile", string(selected))
				if !cfg.BoostForegroundOnly {
					engine.ThrottleAllUserBackgroundProcesses()
				}
			},
			OnPowerPlanChange: func(planGUID winapi.GUID) {
				if !cfg.RespectPowerPlan {
					return
				}
				isHighPerf := planGUID == winapi.GUID_POWER_PLAN_HIGH_PERFORMANCE
				if isHighPerf {
					engine.SetPaused(true)
					log.Info("power-plan event: High Performance detected, pausing throttling")
				} else {
					engine.SetPaused(false)
					log.Info("power-plan event: power plan changed, resuming throttling")
					if !cfg.BoostForegroundOnly {
						engine.ThrottleAllUserBackgroundProcesses()
					}
				}
			},
			OnSessionChange: func(eventType uint32) {
				if eventType == winapi.WTS_SESSION_UNLOCK || eventType == winapi.WTS_SESSION_LOGON {
					log.Info("session change event received", "event_type", eventType)
					if !cfg.BoostForegroundOnly {
						engine.ThrottleAllUserBackgroundProcesses()
					}
				}
			},
		}

		// Display-off → aggressive throttling when user is logged in
		if cfg.ThrottleOnDisplayOff {
			var displayOffMu sync.Mutex
			var savedProfile config.Profile
			var displayIsOff bool

			isUserSessionActive := func() bool {
				sessionID, err := winapi.WTSGetActiveConsoleSessionId()
				return err == nil && sessionID != 0xFFFFFFFF
			}

			callbacks.OnDisplayStateChange = func(displayOn bool) {
				displayOffMu.Lock()
				defer displayOffMu.Unlock()

				if !displayOn && !displayIsOff && isUserSessionActive() {
					displayIsOff = true
					savedProfile = cfg.GetProfile()
					cfg.SetProfile(config.ProfileAggressive)
					log.Info("display off: switching to aggressive profile")
					if !cfg.BoostForegroundOnly {
						engine.ThrottleAllUserBackgroundProcesses()
					}
				} else if displayOn && displayIsOff && isUserSessionActive() {
					displayIsOff = false
					cfg.SetProfile(savedProfile)
					log.Info("display on: restoring profile", "profile", string(savedProfile))
				}
			}
		}

		trayIcon = tray.New(log, callbacks)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := trayIcon.Run(); err != nil {
				log.Error("tray failed", "error", err)
			}
		}()

		// Periodically update tooltip with battery status
		wg.Add(1)
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(60 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					st, err := power.GetStatus()
					var battStr string
					if err == nil && st.BatteryPercent != 255 {
						acStr := "AC"
						if st.ACStatus == power.ACOffline {
							acStr = "battery"
						}
						battStr = fmt.Sprintf("%d%% (%s)", st.BatteryPercent, acStr)
					}
					throttledCount := engine.ThrottledCount()
					engineStatus := "Running"
					if engine.IsPaused() {
						engineStatus = "Paused"
					}
					engineStatus = fmt.Sprintf("%s | %d throttled", engineStatus, throttledCount)
					trayIcon.UpdateStatus(engineStatus, battStr)
				case <-stopHK:
					return
				}
			}
		}()

		// Start message loop in main goroutine
		go func() {
			<-doneCh
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
			<-doneCh
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

	if sched != nil {
		sched.Stop()
	}
	wg.Wait()
	log.Info("EnergyStarGo stopped")
}

// cmdCompanion runs the foreground detection companion in user session.
// Spawned by service via scheduled task at user logon.
// Communicates with service via named pipe.
func cmdCompanion() {
	// Parse companion-specific flags
	fs := flag.NewFlagSet("companion", flag.ContinueOnError)
	configPath := fs.String("config", "", "Path to configuration file")
	if err := fs.Parse(os.Args[2:]); err != nil {
		os.Exit(1)
	}

	// Load configuration
	var cfg *config.Config
	if *configPath != "" {
		cfg, _ = config.LoadFromFile(*configPath)
	}
	if cfg == nil {
		cfg = config.NewDefault()
	}

	// Initialize logger (console only for companion, no event log)
	log, err := logger.New("", "info")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	log.Info("EnergyStarGo companion starting")

	// Connect to service pipe (retry with backoff)
	pipeHandle, err := connectToServicePipe(log)
	if err != nil {
		log.Warn("companion: failed to connect to service pipe", "error", err)
		// Continue anyway - best effort
	}
	defer func() {
		if pipeHandle != 0 {
			windows.CloseHandle(pipeHandle)
		}
	}()

	// Install foreground window hook
	callback := syscall.NewCallback(func(
		hWinEventHook uintptr,
		event uint32,
		hwnd uintptr,
		idObject int32,
		idChild int32,
		dwEventThread uint32,
		dwmsEventTime uint32,
	) uintptr {
		if hwnd == 0 {
			return 0
		}
		var procID uint32
		if winapi.GetWindowThreadProcessId(hwnd, &procID) == 0 || procID == 0 {
			return 0
		}

		// Send PID to service via pipe
		if pipeHandle != 0 {
			pidBytes := [4]byte{
				byte(procID),
				byte(procID >> 8),
				byte(procID >> 16),
				byte(procID >> 24),
			}
			var written uint32
			_ = windows.WriteFile(pipeHandle, pidBytes[:], &written, nil)
		}
		return 0
	})

	hookHandle := winapi.SetWinEventHook(
		winapi.EVENT_SYSTEM_FOREGROUND,
		winapi.EVENT_SYSTEM_FOREGROUND,
		0,
		callback,
		0, 0,
		winapi.WINEVENT_OUTOFCONTEXT,
	)
	if hookHandle == 0 {
		log.Error("failed to install foreground hook")
		os.Exit(1)
	}
	defer winapi.UnhookWinEvent(hookHandle)
	log.Info("companion foreground hook installed")

	// Keep the process alive by running Windows message loop
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var msg winapi.MSG
	for {
		got, err := winapi.GetMessage(&msg)
		if err != nil {
			log.Error("GetMessage failed", "error", err)
			break
		}
		if !got { // WM_QUIT
			break
		}
		winapi.TranslateMessage(&msg)
		winapi.DispatchMessage(&msg)
	}

	log.Info("companion stopped")
}

// connectToServicePipe attempts to connect to the service's named pipe.
func connectToServicePipe(log *logger.Logger) (windows.Handle, error) {
	const pipeNameFormat = `\\.\pipe\EnergyStarGo-Foreground`

	// Retry up to 5 times with 100ms backoff (service might not have created pipe yet)
	for attempt := 0; attempt < 5; attempt++ {
		handle, err := windows.CreateFile(
			syscall.StringToUTF16Ptr(pipeNameFormat),
			windows.GENERIC_WRITE,
			0,
			nil,
			windows.OPEN_EXISTING,
			0,
			0,
		)
		if err == nil {
			log.Debug("connected to service pipe")
			return handle, nil
		}

		if attempt < 4 {
			time.Sleep(100 * time.Millisecond)
		}
	}

	return 0, fmt.Errorf("failed to connect to service pipe after retries")
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

func cmdStop() {
	if err := service.Stop(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to stop service: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Service stopped.")
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

func cmdBypassList() {
	cfg := config.DefaultConfig()
	cfgPath := config.DefaultConfigPath()
	if loaded, err := config.LoadFromFile(cfgPath); err == nil {
		cfg = loaded
	}
	fmt.Printf("Profile: %s\n", cfg.Profile)
	list := cfg.BypassList()
	sort.Strings(list)
	fmt.Printf("Bypass list (%d processes):\n", len(list))
	for _, p := range list {
		fmt.Printf("  %s\n", p)
	}
}

func runAsService() {
	cfg := loadConfigForService()

	// For service mode, event logging should be enabled by default so service
	// events are recorded in Windows Application log.
	cfg.EnableEventLog = true

	// Ensure there is always a local log file for easier debugging when
	// EventLog message mapping is incomplete.
	if cfg.LogFile == "" {
		defaultDir := filepath.Join(os.Getenv("ProgramData"), "EnergyStarGo")
		_ = os.MkdirAll(defaultDir, 0o755)
		cfg.LogFile = filepath.Join(defaultDir, "energystar.log")
	}

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

func isVirtualMachine() bool {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Control\SystemInformation`, registry.READ)
	if err != nil {
		return false
	}
	defer key.Close()

	manufacturer, _, _ := key.GetStringValue("SystemManufacturer")
	product, _, _ := key.GetStringValue("SystemProductName")
	combined := strings.ToLower(manufacturer + " " + product)

	vmIndicators := []string{"vmware", "virtual", "virtualbox", "qemu", "xen", "hyper-v", "innotek"}
	for _, indicator := range vmIndicators {
		if strings.Contains(combined, indicator) {
			return true
		}
	}
	return false
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
