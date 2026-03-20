// SPDX-License-Identifier: GPL-2.0-only
// SPDX-FileCopyrightText: 2024 Kyle Sanderson <kyle@kylesanderson.dev>

//go:build windows

// Package power provides battery and power state monitoring for Windows.
package power

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modkernel32 = windows.NewLazySystemDLL("kernel32.dll")
	moduser32   = windows.NewLazySystemDLL("user32.dll")
	modpowrprof = windows.NewLazySystemDLL("powrprof.dll")

	procGetSystemPowerStatus = modkernel32.NewProc("GetSystemPowerStatus")
	procGetTickCount         = modkernel32.NewProc("GetTickCount")
	procGetLastInputInfo     = moduser32.NewProc("GetLastInputInfo")
	procSetSuspendState      = modpowrprof.NewProc("SetSuspendState")
)

// ACStatus represents the AC power line status.
type ACStatus uint8

const (
	ACOffline ACStatus = 0
	ACOnline  ACStatus = 1
	ACUnknown ACStatus = 255
)

// Battery flag bitmask values from GetSystemPowerStatus.
const (
	batteryFlagLow      = 0x02
	batteryFlagCritical = 0x04
	batteryFlagNoBat    = 0x80
)

// Status holds the current battery and AC power state.
type Status struct {
	ACStatus       ACStatus
	BatteryPercent uint8 // 255 = unknown / no battery
	IsLow          bool  // Windows-detected low battery flag
	IsCritical     bool  // Windows-detected critical battery flag
}

// systemPowerStatus mirrors the SYSTEM_POWER_STATUS Win32 structure.
type systemPowerStatus struct {
	ACLineStatus        uint8
	BatteryFlag         uint8
	BatteryLifePercent  uint8
	SystemStatusFlag    uint8
	BatteryLifeTime     uint32
	BatteryFullLifeTime uint32
}

// lastInputInfo mirrors the LASTINPUTINFO Win32 structure.
type lastInputInfo struct {
	cbSize uint32
	dwTime uint32
}

// GetStatus reads the current battery/AC status via GetSystemPowerStatus.
func GetStatus() (Status, error) {
	var sps systemPowerStatus
	r1, _, err := procGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(&sps)))
	if r1 == 0 {
		return Status{}, fmt.Errorf("GetSystemPowerStatus: %w", err)
	}
	return Status{
		ACStatus:       ACStatus(sps.ACLineStatus),
		BatteryPercent: sps.BatteryLifePercent,
		IsLow:          sps.BatteryFlag&batteryFlagLow != 0,
		IsCritical:     sps.BatteryFlag&batteryFlagCritical != 0,
	}, nil
}

// IsOnBattery returns true if AC power is not connected.
func IsOnBattery() bool {
	s, err := GetStatus()
	if err != nil {
		return false
	}
	return s.ACStatus == ACOffline
}

// IdleSeconds returns the number of seconds since the last user input event.
func IdleSeconds() uint32 {
	info := lastInputInfo{cbSize: uint32(unsafe.Sizeof(lastInputInfo{}))}
	r1, _, _ := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&info)))
	if r1 == 0 {
		return 0
	}
	tickCount, _, _ := procGetTickCount.Call()
	return (uint32(tickCount) - info.dwTime) / 1000
}

// Suspend calls SetSuspendState to put the system to sleep or hibernate.
// If hibernate is true, the system hibernates (writes memory to disk).
// If force is true, the suspend is forced even if wake timers are set.
func Suspend(hibernate, force bool) error {
	hib := uintptr(0)
	if hibernate {
		hib = 1
	}
	frc := uintptr(0)
	if force {
		frc = 1
	}
	r1, _, err := procSetSuspendState.Call(hib, frc, 0)
	if r1 == 0 {
		return fmt.Errorf("SetSuspendState: %w", err)
	}
	return nil
}
