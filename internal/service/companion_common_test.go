// SPDX-License-Identifier: GPL-2.0-only
// SPDX-FileCopyrightText: 2024 Kyle Sanderson

package service

import "testing"

func TestCompanionCommandLine_WithConfig(t *testing.T) {
	exePath := `C:\Program Files\EnergyStarGo\energystar.exe`
	cmd := companionCommandLine(exePath, []string{"--config", `C:\config\energystar.json`})
	want := `"C:\\Program Files\\EnergyStarGo\\energystar.exe" companion --config "C:\\config\\energystar.json"`
	if cmd != want {
		t.Fatalf("unexpected command line, got %q want %q", cmd, want)
	}
}

func TestCompanionCommandLine_WithoutConfig(t *testing.T) {
	exePath := `C:\EnergyStarGo\energystar.exe`
	cmd := companionCommandLine(exePath, []string{"--log-level", "debug"})
	want := `"C:\\EnergyStarGo\\energystar.exe" companion`
	if cmd != want {
		t.Fatalf("unexpected command line, got %q want %q", cmd, want)
	}
}
