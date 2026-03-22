// SPDX-License-Identifier: GPL-2.0-only
// SPDX-FileCopyrightText: 2024 Kyle Sanderson

package foregroundipc

import (
	"fmt"
	"testing"
)

func TestPipeName(t *testing.T) {
	if PipeName != `\\.\pipe\EnergyStarGo-Foreground` {
		t.Fatalf("PipeName = %q, want %q", PipeName, `\\.\pipe\EnergyStarGo-Foreground`)
	}
}

func TestEncodePID(t *testing.T) {
	got := EncodePID(0x11223344)
	want := [4]byte{0x44, 0x33, 0x22, 0x11}
	if got != want {
		t.Fatalf("EncodePID() = %v, want %v", got, want)
	}
}

func TestDecodePID(t *testing.T) {
	pid, err := DecodePID([]byte{0x44, 0x33, 0x22, 0x11})
	if err != nil {
		t.Fatalf("DecodePID() returned error: %v", err)
	}
	if pid != 0x11223344 {
		t.Fatalf("DecodePID() = %d, want %d", pid, 0x11223344)
	}
}

func TestDecodePIDRejectsInvalidLength(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{name: "empty", data: nil},
		{name: "short", data: []byte{0x01, 0x02, 0x03}},
		{name: "long", data: []byte{0x01, 0x02, 0x03, 0x04, 0x05}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pid, err := DecodePID(tt.data)
			if err == nil {
				t.Fatal("expected error for invalid length")
			}
			if pid != 0 {
				t.Fatalf("pid = %d, want 0 on error", pid)
			}
		})
	}
}

func TestDecodePIDRejectsZeroPID(t *testing.T) {
	pid, err := DecodePID([]byte{0x00, 0x00, 0x00, 0x00})
	if err == nil {
		t.Fatal("expected error for PID 0")
	}
	if pid != 0 {
		t.Fatalf("pid = %d, want 0 on error", pid)
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	inputs := []uint32{1, 42, 1234, 0x7FFFFFFF, 0xDEADBEEF}
	for _, pid := range inputs {
		t.Run(fmt.Sprintf("pid_%d", pid), func(t *testing.T) {
			encoded := EncodePID(pid)
			decoded, err := DecodePID(encoded[:])
			if err != nil {
				t.Fatalf("DecodePID() returned error: %v", err)
			}
			if decoded != pid {
				t.Fatalf("roundtrip = %d, want %d", decoded, pid)
			}
		})
	}
}
