// SPDX-License-Identifier: GPL-2.0-only
// SPDX-FileCopyrightText: 2024 Kyle Sanderson <kyle@kylesanderson.dev>

//go:build windows

package power

import "testing"

func TestGetStatus(t *testing.T) {
	status, err := GetStatus()
	if err != nil {
		t.Logf("GetStatus error (may be expected in VMs): %v", err)
		return
	}
	t.Logf("ACStatus: %d, BatteryPercent: %d, IsLow: %v, IsCritical: %v",
		status.ACStatus, status.BatteryPercent, status.IsLow, status.IsCritical)
}

func TestIsOnBattery(t *testing.T) {
	// Should not panic.
	onBattery := IsOnBattery()
	t.Logf("IsOnBattery: %v", onBattery)
}

func TestIdleSeconds(t *testing.T) {
	idle := IdleSeconds()
	t.Logf("IdleSeconds: %d", idle)
}
