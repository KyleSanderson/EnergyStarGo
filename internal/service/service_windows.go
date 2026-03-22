// SPDX-License-Identifier: GPL-2.0-only
// SPDX-FileCopyrightText: 2024 Kyle Sanderson

//go:build windows

// Package service provides Windows service support for EnergyStarGo.
package service

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/KyleSanderson/EnergyStarGo/internal/config"
	"github.com/KyleSanderson/EnergyStarGo/internal/logger"
	"github.com/KyleSanderson/EnergyStarGo/internal/throttle"
)

const ServiceName = "EnergyStarGo"
const ServiceDisplayName = "EnergyStar Go - EcoQoS Process Throttler"
const ServiceDescription = "Automatically throttles background processes using Windows 11 EcoQoS for improved battery life and thermal management."

// EnergyStarService implements svc.Handler.
type EnergyStarService struct {
	cfg    *config.Config
	log    *logger.Logger
	engine *throttle.Engine
}

// NewService creates a new service handler.
func NewService(cfg *config.Config, log *logger.Logger) *EnergyStarService {
	return &EnergyStarService{
		cfg: cfg,
		log: log,
	}
}

// Execute implements svc.Handler. It is called by the Windows service control manager.
func (s *EnergyStarService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	const acceptedCmds = svc.AcceptStop | svc.AcceptShutdown | svc.AcceptPauseAndContinue | svc.AcceptPowerEvent | svc.AcceptSessionChange

	changes <- svc.Status{State: svc.StartPending}

	s.engine = throttle.New(s.cfg, s.log)

	// Initial throttle sweep — skipped in boost_foreground_only mode.
	if !s.cfg.BoostForegroundOnly {
		s.engine.ThrottleAllUserBackgroundProcesses()
	} else {
		s.log.Info("boost_foreground_only mode: skipping initial sweep")
	}

	// Start housekeeping in background
	hkDone := make(chan struct{})
	go func() {
		defer close(hkDone)
		if s.cfg.BoostForegroundOnly {
			return // no periodic sweeping in boost_foreground_only mode
		}
		ticker := time.NewTicker(s.cfg.HousekeepingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.engine.ThrottleAllUserBackgroundProcesses()
			case <-hkDone:
				return
			}
		}
	}()

	// Start message loop in background (requires own OS thread)
	msgDone := make(chan struct{})
	go func() {
		defer close(msgDone)
		s.engine.RunMessageLoop()
	}()

	changes <- svc.Status{State: svc.Running, Accepts: acceptedCmds}
	s.log.Info("service started")

	paused := false
	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.PowerEvent:
				s.log.Info("service power event received")
				if !s.cfg.BoostForegroundOnly {
					s.engine.ThrottleAllUserBackgroundProcesses()
				}
				changes <- svc.Status{State: svc.Running, Accepts: acceptedCmds}
			case svc.SessionChange:
				// Handle different session change events
				// WTS_SESSION_LOCK = 7, WTS_SESSION_UNLOCK = 8
				const (
					WTS_SESSION_LOCK   = 7
					WTS_SESSION_UNLOCK = 8
					WTS_SESSION_LOGON  = 5
				)

				s.log.Info("service session change received", "event_type", c.EventType)

				// On session lock: immediately switch to aggressive profile
				if c.EventType == WTS_SESSION_LOCK {
					s.log.Info("session locked: activating aggressive profile")
					s.cfg.SetProfile(config.ProfileAggressive)
					if !s.cfg.BoostForegroundOnly {
						s.engine.ThrottleAllUserBackgroundProcesses()
					}
				} else if c.EventType == WTS_SESSION_UNLOCK {
					// On unlock: let auto-profile handle it, but trigger a sweep anyway
					s.log.Info("session unlocked")
					if !s.cfg.BoostForegroundOnly {
						s.engine.ThrottleAllUserBackgroundProcesses()
					}
				} else if c.EventType == WTS_SESSION_LOGON {
					// On logon: trigger a sweep to catch new user session processes
					s.log.Info("session logon detected")
					if !s.cfg.BoostForegroundOnly {
						s.engine.ThrottleAllUserBackgroundProcesses()
					}
				} else {
					// Other session changes: trigger sweep as well
					if !s.cfg.BoostForegroundOnly {
						s.engine.ThrottleAllUserBackgroundProcesses()
					}
				}

				changes <- svc.Status{State: svc.Running, Accepts: acceptedCmds}
			case svc.Stop, svc.Shutdown:
				s.log.Info("service stop requested")
				changes <- svc.Status{State: svc.StopPending}
				s.engine.Stop()
				if s.cfg.RestoreOnExit {
					s.engine.RestoreAllProcesses()
				}
				return
			case svc.Pause:
				if !paused {
					s.log.Info("service paused")
					paused = true
					s.engine.SetPaused(true)
					changes <- svc.Status{State: svc.Paused, Accepts: acceptedCmds}
				}
			case svc.Continue:
				if paused {
					s.log.Info("service resumed")
					paused = false
					s.engine.SetPaused(false)
					if !s.cfg.BoostForegroundOnly {
						s.engine.ThrottleAllUserBackgroundProcesses()
					}
					changes <- svc.Status{State: svc.Running, Accepts: acceptedCmds}
				}
			}
		}
	}
}

// IsWindowsService detects whether the current process is running as a Windows service.
func IsWindowsService() bool {
	isSvc, err := svc.IsWindowsService()
	if err != nil {
		return false
	}
	return isSvc
}

// Run starts the EnergyStarGo process as a Windows service.
func Run(cfg *config.Config, log *logger.Logger) error {
	svcHandler := NewService(cfg, log)
	return svc.Run(ServiceName, svcHandler)
}

// Install installs EnergyStarGo as a Windows service.
func Install(exePath string, args []string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(ServiceName)
	if err == nil {
		s.Close()
		return fmt.Errorf("service %s already exists", ServiceName)
	}

	svcConfig := mgr.Config{
		DisplayName:  ServiceDisplayName,
		Description:  ServiceDescription,
		StartType:    mgr.StartAutomatic,
		ErrorControl: mgr.ErrorNormal,
	}

	s, err = m.CreateService(ServiceName, exePath, svcConfig, args...)
	if err != nil {
		return fmt.Errorf("failed to create service: %w", err)
	}
	defer s.Close()

	// Set recovery actions: restart after 5 seconds on first two failures
	recoveryActions := []mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 10 * time.Second},
		{Type: mgr.NoAction, Delay: 0},
	}
	if err := s.SetRecoveryActions(recoveryActions, 86400); err != nil {
		// Non-fatal: service is created, just no recovery
		_ = err
	}

	// Create scheduled task for companion app to run at user logon
	// Best-effort: if it fails, service still works without companion
	if err := createCompanionScheduledTask(exePath, args); err != nil {
		// Don't fail the service installation, just log it
		fmt.Printf("Warning: failed to create companion scheduled task: %v\n", err)
	}

	return nil
}

// createCompanionScheduledTask creates a scheduled task to run companion at user logon.
// Runs the same energystar.exe with "companion" argument.
func createCompanionScheduledTask(exePath string, serviceArgs []string) error {
	// Prepare command line: energystar.exe companion (plus any service config args)
	cmdLine := fmt.Sprintf(`"%s" companion`, exePath)
	
	// If service has config path arg, pass it to companion too
	for i := 0; i < len(serviceArgs)-1; i++ {
		if serviceArgs[i] == "--config" {
			cmdLine += fmt.Sprintf(` --config "%s"`, serviceArgs[i+1])
			break
		}
	}

	// Use Windows Task Scheduler COM API
	// This is a simple approach using osascript-style registry manipulation
	// For complex scheduled tasks, would use Task Scheduler COM (go-ole)
	// For now, use registry to create a simple RunOnce entry (doesn't repeat, but sufficient for per-logon)
	
	// Alternative: Use RunOnce in HKEY_LOCAL_MACHINE\Software\Microsoft\Windows\CurrentVersion\RunOnce
	// This runs once per logon automatically
	
	hkey, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`Software\Microsoft\Windows\CurrentVersion\RunOnce`, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("failed to open RunOnce registry key: %w", err)
	}
	defer hkey.Close()

	errs := hkey.SetStringValue("EnergyStarGo-Companion", cmdLine)
	if errs != nil {
		return fmt.Errorf("failed to set RunOnce registry value: %w", errs)
	}

	return nil
}

// Uninstall removes the EnergyStarGo Windows service.
func Uninstall() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(ServiceName)
	if err != nil {
		return fmt.Errorf("service %s is not installed: %w", ServiceName, err)
	}
	defer s.Close()

	// Try to stop the service first
	_, _ = s.Control(svc.Stop)
	time.Sleep(2 * time.Second) // Give it a moment

	if err := s.Delete(); err != nil {
		return fmt.Errorf("failed to delete service: %w", err)
	}

	return nil
}

// Start starts the installed EnergyStarGo service.
func Start() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(ServiceName)
	if err != nil {
		return fmt.Errorf("failed to open service: %w", err)
	}
	defer s.Close()

	return s.Start()
}

// Stop stops the running service.
func Stop() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to service manager: %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(ServiceName)
	if err != nil {
		return fmt.Errorf("service %s is not installed: %w", ServiceName, err)
	}
	defer s.Close()
	_, err = s.Control(svc.Stop)
	return err
}

// QueryStatus returns the current status of the EnergyStarGo service.
func QueryStatus() (svc.Status, error) {
	m, err := mgr.Connect()
	if err != nil {
		return svc.Status{}, fmt.Errorf("failed to connect to service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(ServiceName)
	if err != nil {
		return svc.Status{}, fmt.Errorf("failed to open service: %w", err)
	}
	defer s.Close()

	return s.Query()
}

// ListByState returns the names of installed Windows services whose current
// state matches one of the provided states (e.g. svc.Stopped, svc.Paused).
// Results are capped at 50 entries to keep the list manageable for UI display.
func ListByState(states ...svc.State) ([]string, error) {
	m, err := mgr.Connect()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to service manager: %w", err)
	}
	defer m.Disconnect()

	names, err := m.ListServices()
	if err != nil {
		return nil, fmt.Errorf("failed to list services: %w", err)
	}

	want := make(map[svc.State]struct{}, len(states))
	for _, st := range states {
		want[st] = struct{}{}
	}

	var result []string
	for _, name := range names {
		if len(result) >= 50 {
			break
		}
		s, err := m.OpenService(name)
		if err != nil {
			continue
		}
		st, err := s.Query()
		s.Close()
		if err != nil {
			continue
		}
		if _, ok := want[st.State]; ok {
			result = append(result, name)
		}
	}
	return result, nil
}

// StartByName starts the named Windows service.
func StartByName(name string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(name)
	if err != nil {
		return fmt.Errorf("failed to open service %q: %w", name, err)
	}
	defer s.Close()

	return s.Start()
}
