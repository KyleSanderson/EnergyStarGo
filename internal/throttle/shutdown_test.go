// SPDX-License-Identifier: GPL-2.0-only
// SPDX-FileCopyrightText: 2024 Kyle Sanderson

package throttle

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
	"testing"
	"time"
)

// TestShutdownOncePattern verifies the sync.Once shutdown coordination used by
// the main entry point: multiple concurrent calls to triggerShutdown close the
// channel exactly once (no panic).
func TestShutdownOncePattern(t *testing.T) {
	doneCh := make(chan struct{})
	var doneOnce sync.Once
	trigger := func() { doneOnce.Do(func() { close(doneCh) }) }

	// Fire from 10 goroutines simultaneously
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			trigger()
		}()
	}

	// doneCh must be closed within a reasonable time
	select {
	case <-doneCh:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("doneCh was not closed within timeout")
	}

	wg.Wait()
}

// TestSignalNotifyPropagation verifies that signal.Notify relays SIGINT to the
// channel, which the app uses as one of the shutdown triggers.
func TestSignalNotifyPropagation(t *testing.T) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT)
	defer signal.Stop(sigCh)

	// Send SIGINT to self
	p, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("FindProcess: %v", err)
	}
	if err := p.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("Signal: %v", err)
	}

	select {
	case sig := <-sigCh:
		if sig != syscall.SIGINT {
			t.Fatalf("got signal %v, want SIGINT", sig)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SIGINT not received within timeout")
	}
}

// TestShutdownChannelSelectBehavior ensures the select-case pattern used in
// main waits on EITHER the signal channel or done channel, whichever fires
// first.
func TestShutdownChannelSelectBehavior(t *testing.T) {
	doneCh := make(chan struct{})
	sigCh := make(chan os.Signal, 1)
	var doneOnce sync.Once
	trigger := func() { doneOnce.Do(func() { close(doneCh) }) }

	triggered := make(chan struct{})
	go func() {
		select {
		case <-sigCh:
			trigger()
		case <-doneCh:
		}
		close(triggered)
	}()

	// Trigger via signal channel
	sigCh <- syscall.SIGINT

	select {
	case <-triggered:
		// Success — the goroutine exited
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown goroutine did not exit within timeout")
	}

	// doneCh should be closed
	select {
	case <-doneCh:
		// success
	default:
		t.Fatal("doneCh should be closed after trigger")
	}
}
