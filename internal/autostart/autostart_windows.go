// SPDX-License-Identifier: GPL-2.0-only
// SPDX-FileCopyrightText: 2024 Kyle Sanderson <kyle@kylesanderson.dev>

//go:build windows

// Package autostart manages Windows startup registration via the registry Run key.
package autostart

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

const (
	// ValueName is the registry value name used under the Run key.
	ValueName = "EnergyStarGo"

	runKeyPath = `SOFTWARE\Microsoft\Windows\CurrentVersion\Run`
)

// IsEnabled returns true if EnergyStarGo has a Run key entry.
func IsEnabled() (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false, fmt.Errorf("autostart: open run key: %w", err)
	}
	defer k.Close()

	_, _, err = k.GetStringValue(ValueName)
	if err == registry.ErrNotExist {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("autostart: query value: %w", err)
	}
	return true, nil
}

// Enable adds the current executable with "run --tray" to the HKCU Run key.
func Enable() error {
	return EnableWithArgs("run --tray")
}

// EnableWithArgs adds the current executable with the given args to the HKCU Run key.
func EnableWithArgs(args string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("autostart: get executable path: %w", err)
	}

	value := fmt.Sprintf("%q %s", exe, args)

	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("autostart: open run key for writing: %w", err)
	}
	defer k.Close()

	if err := k.SetStringValue(ValueName, value); err != nil {
		return fmt.Errorf("autostart: set value: %w", err)
	}
	return nil
}

// Disable removes the Run key entry for EnergyStarGo.
func Disable() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("autostart: open run key for writing: %w", err)
	}
	defer k.Close()

	if err := k.DeleteValue(ValueName); err != nil && err != registry.ErrNotExist {
		return fmt.Errorf("autostart: delete value: %w", err)
	}
	return nil
}
