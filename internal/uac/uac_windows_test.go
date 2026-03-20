// SPDX-License-Identifier: GPL-2.0-only
// SPDX-FileCopyrightText: 2024 Kyle Sanderson <kyle@kylesanderson.dev>

//go:build windows

package uac

import "testing"

func TestIsElevated(t *testing.T) {
	// Verify IsElevated doesn't panic and returns a bool value.
	elevated := IsElevated()
	t.Logf("IsElevated: %v", elevated)
}
