// SPDX-License-Identifier: GPL-2.0-only
// SPDX-FileCopyrightText: 2024 Kyle Sanderson

// Package foregroundipc provides the shared wire format for foreground PID
// messages exchanged between the Session 0 service and the user-session
// companion process.
package foregroundipc

import (
	"encoding/binary"
	"fmt"
)

const PipeName = `\\.\pipe\EnergyStarGo-Foreground`

// EncodePID converts a process ID to the 4-byte little-endian wire format.
func EncodePID(pid uint32) [4]byte {
	var out [4]byte
	binary.LittleEndian.PutUint32(out[:], pid)
	return out
}

// DecodePID parses a 4-byte little-endian PID from the foreground IPC wire format.
func DecodePID(b []byte) (uint32, error) {
	if len(b) != 4 {
		return 0, fmt.Errorf("foreground PID payload must be 4 bytes, got %d", len(b))
	}
	pid := binary.LittleEndian.Uint32(b)
	if pid == 0 {
		return 0, fmt.Errorf("foreground PID payload decoded to invalid PID 0")
	}
	return pid, nil
}
