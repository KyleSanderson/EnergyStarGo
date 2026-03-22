// SPDX-License-Identifier: GPL-2.0-only
// SPDX-FileCopyrightText: 2024 Kyle Sanderson

//go:build windows

package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unsafe"

	"github.com/KyleSanderson/EnergyStarGo/internal/foregroundipc"
	"github.com/KyleSanderson/EnergyStarGo/internal/logger"
	"golang.org/x/sys/windows"
)

const noActiveConsoleSession = 0xFFFFFFFF

// foregroundPIDReceiver is implemented by throttle.Engine.
type foregroundPIDReceiver interface {
	HandleForegroundPID(pid uint32)
}

type companionRuntime struct {
	log      *logger.Logger
	receiver foregroundPIDReceiver
	exePath  string
	args     []string

	mu                 sync.Mutex
	companionBySession map[uint32]windows.Handle

	stopOnce sync.Once
	stopCh   chan struct{}
	doneCh   chan struct{}
}

func newCompanionRuntime(log *logger.Logger, receiver foregroundPIDReceiver, serviceArgs []string) (*companionRuntime, error) {
	if receiver == nil {
		return nil, fmt.Errorf("nil foreground PID receiver")
	}

	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve executable path: %w", err)
	}
	exePath, _ = filepath.Abs(exePath)

	args := append([]string(nil), serviceArgs...)
	if len(args) > 0 && args[0] == ServiceName {
		args = args[1:]
	}

	return &companionRuntime{
		log:                log,
		receiver:           receiver,
		exePath:            exePath,
		args:               args,
		companionBySession: make(map[uint32]windows.Handle),
		stopCh:             make(chan struct{}),
		doneCh:             make(chan struct{}),
	}, nil
}

func (c *companionRuntime) Start() {
	go c.serveForegroundPipe()
	c.EnsureActiveSessionCompanion()
}

func (c *companionRuntime) Stop() {
	c.stopOnce.Do(func() {
		close(c.stopCh)
		c.stopCompanions()
		c.nudgePipeServer()
		select {
		case <-c.doneCh:
		case <-time.After(2 * time.Second):
			c.log.Debug("timed out waiting for foreground pipe shutdown")
		}
	})
}

func (c *companionRuntime) EnsureActiveSessionCompanion() {
	sessionID := windows.WTSGetActiveConsoleSessionId()
	if sessionID == noActiveConsoleSession {
		c.log.Debug("no active console session available for companion")
		return
	}
	c.ensureCompanionForSession(sessionID)
}

func (c *companionRuntime) ensureCompanionForSession(sessionID uint32) {
	c.mu.Lock()
	existing, ok := c.companionBySession[sessionID]
	if ok && processHandleRunning(existing) {
		c.mu.Unlock()
		return
	}
	if ok {
		delete(c.companionBySession, sessionID)
	}
	c.mu.Unlock()

	if ok {
		_ = windows.CloseHandle(existing)
	}

	procHandle, err := spawnCompanionAsUser(c.exePath, c.args, sessionID)
	if err != nil {
		c.log.Warn("failed to start companion in session", "session_id", sessionID, "error", err)
		return
	}

	c.mu.Lock()
	old := c.companionBySession[sessionID]
	c.companionBySession[sessionID] = procHandle
	c.mu.Unlock()
	if old != 0 && old != procHandle {
		_ = windows.CloseHandle(old)
	}

	pid, _ := windows.GetProcessId(procHandle)
	c.log.Info("companion started", "session_id", sessionID, "pid", pid)
}

func (c *companionRuntime) stopCompanions() {
	c.mu.Lock()
	procs := c.companionBySession
	c.companionBySession = make(map[uint32]windows.Handle)
	c.mu.Unlock()

	for sessionID, handle := range procs {
		if handle == 0 {
			continue
		}
		_ = windows.TerminateProcess(handle, 0)
		_ = windows.CloseHandle(handle)
		c.log.Debug("companion stopped", "session_id", sessionID)
	}
}

func (c *companionRuntime) serveForegroundPipe() {
	defer close(c.doneCh)

	sa, err := foregroundPipeSecurityAttributes()
	if err != nil {
		c.log.Warn("failed to build foreground pipe security attributes", "error", err)
		return
	}

	pipeName, err := windows.UTF16PtrFromString(foregroundipc.PipeName)
	if err != nil {
		c.log.Warn("invalid foreground pipe name", "error", err)
		return
	}

	for {
		if c.isStopping() {
			return
		}

		handle, err := windows.CreateNamedPipe(
			pipeName,
			windows.PIPE_ACCESS_INBOUND,
			windows.PIPE_TYPE_MESSAGE|windows.PIPE_READMODE_MESSAGE|windows.PIPE_WAIT,
			1,
			64,
			64,
			0,
			sa,
		)
		if err != nil {
			if c.isStopping() {
				return
			}
			c.log.Warn("failed to create foreground pipe", "error", err)
			time.Sleep(500 * time.Millisecond)
			continue
		}

		err = windows.ConnectNamedPipe(handle, nil)
		if err != nil && !errors.Is(err, windows.ERROR_PIPE_CONNECTED) {
			_ = windows.CloseHandle(handle)
			if c.isStopping() {
				return
			}
			c.log.Debug("foreground pipe connect failed", "error", err)
			time.Sleep(250 * time.Millisecond)
			continue
		}

		c.readForegroundPipe(handle)
		_ = windows.DisconnectNamedPipe(handle)
		_ = windows.CloseHandle(handle)
	}
}

func (c *companionRuntime) readForegroundPipe(handle windows.Handle) {
	var payload [4]byte
	for {
		if c.isStopping() {
			return
		}

		var read uint32
		err := windows.ReadFile(handle, payload[:], &read, nil)
		if err != nil {
			if errors.Is(err, windows.ERROR_BROKEN_PIPE) ||
				errors.Is(err, windows.ERROR_NO_DATA) ||
				errors.Is(err, windows.ERROR_PIPE_NOT_CONNECTED) {
				return
			}
			if c.isStopping() {
				return
			}
			c.log.Debug("foreground pipe read failed", "error", err)
			return
		}

		pid, err := foregroundipc.DecodePID(payload[:read])
		if err != nil {
			c.log.Debug("ignoring malformed foreground payload", "error", err)
			continue
		}
		c.receiver.HandleForegroundPID(pid)
	}
}

func (c *companionRuntime) nudgePipeServer() {
	pipeName, err := windows.UTF16PtrFromString(foregroundipc.PipeName)
	if err != nil {
		return
	}
	h, err := windows.CreateFile(
		pipeName,
		windows.GENERIC_WRITE,
		0,
		nil,
		windows.OPEN_EXISTING,
		0,
		0,
	)
	if err == nil {
		_ = windows.CloseHandle(h)
	}
}

func (c *companionRuntime) isStopping() bool {
	select {
	case <-c.stopCh:
		return true
	default:
		return false
	}
}

func processHandleRunning(handle windows.Handle) bool {
	if handle == 0 {
		return false
	}
	status, err := windows.WaitForSingleObject(handle, 0)
	return err == nil && status == uint32(windows.WAIT_TIMEOUT)
}

func spawnCompanionAsUser(exePath string, serviceArgs []string, sessionID uint32) (windows.Handle, error) {
	var userToken windows.Token
	if err := windows.WTSQueryUserToken(sessionID, &userToken); err != nil {
		return 0, fmt.Errorf("WTSQueryUserToken: %w", err)
	}
	defer userToken.Close()

	var primaryToken windows.Token
	if err := windows.DuplicateTokenEx(
		userToken,
		windows.MAXIMUM_ALLOWED,
		nil,
		windows.SecurityImpersonation,
		windows.TokenPrimary,
		&primaryToken,
	); err != nil {
		return 0, fmt.Errorf("DuplicateTokenEx: %w", err)
	}
	defer primaryToken.Close()

	var env *uint16
	envErr := windows.CreateEnvironmentBlock(&env, primaryToken, false)
	if envErr == nil && env != nil {
		defer windows.DestroyEnvironmentBlock(env)
	}

	appName, err := windows.UTF16PtrFromString(exePath)
	if err != nil {
		return 0, fmt.Errorf("UTF16 app name: %w", err)
	}
	cmdLine, err := windows.UTF16PtrFromString(companionCommandLine(exePath, serviceArgs))
	if err != nil {
		return 0, fmt.Errorf("UTF16 command line: %w", err)
	}
	currentDir, err := windows.UTF16PtrFromString(filepath.Dir(exePath))
	if err != nil {
		return 0, fmt.Errorf("UTF16 current dir: %w", err)
	}

	desktop, _ := windows.UTF16PtrFromString("winsta0\\default")
	si := windows.StartupInfo{
		Cb:      uint32(unsafe.Sizeof(windows.StartupInfo{})),
		Desktop: desktop,
	}
	var pi windows.ProcessInformation

	flags := uint32(windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NO_WINDOW)
	if env != nil {
		flags |= windows.CREATE_UNICODE_ENVIRONMENT
	}

	if err := windows.CreateProcessAsUser(
		primaryToken,
		appName,
		cmdLine,
		nil,
		nil,
		false,
		flags,
		env,
		currentDir,
		&si,
		&pi,
	); err != nil {
		return 0, fmt.Errorf("CreateProcessAsUser: %w", err)
	}

	_ = windows.CloseHandle(pi.Thread)
	return pi.Process, nil
}

func foregroundPipeSecurityAttributes() (*windows.SecurityAttributes, error) {
	// Allow SYSTEM/Administrators full access and Interactive users write access.
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;GW;;;IU)")
	if err != nil {
		return nil, err
	}
	return &windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: sd,
		InheritHandle:      0,
	}, nil
}
