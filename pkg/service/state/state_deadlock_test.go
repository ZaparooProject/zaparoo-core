// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// This file is part of Zaparoo Core.
//
// Zaparoo Core is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Zaparoo Core is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.

package state

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/stretchr/testify/assert"
)

// TestSetActiveCard_NoDeadlockWithSlowConsumer is a regression test for the
// "hold lock while sending to channel" deadlock bug.
//
// Previously, State methods held mu while calling sendNotification, which sends
// to the Notifications channel. If a consumer was slow or the channel buffer
// was full, the sender would block while holding the lock. Any other goroutine
// trying to acquire the lock would then deadlock.
//
// The fix was the "unlock before notify" pattern: prepare data under lock,
// unlock, then send notification.
//
// With -tags=deadlock, go-deadlock detects lock ordering violations,
// providing an additional safety net.
func TestSetActiveCard_NoDeadlockWithSlowConsumer(t *testing.T) {
	t.Parallel()

	state, notifications := NewState(nil, "test-boot-uuid")

	done := make(chan struct{})
	defer close(done)

	// Slow consumer - drains notifications with delay
	go func() {
		for {
			select {
			case <-notifications:
				time.Sleep(5 * time.Millisecond)
			case <-done:
				return
			}
		}
	}()

	// Concurrent writers
	var wg sync.WaitGroup
	for i := range 5 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := range 20 {
				token := tokens.Token{
					UID:      "uid-" + string(rune('A'+id)) + "-" + string(rune('0'+j%10)),
					Text:     "test",
					ScanTime: time.Now(),
				}
				state.SetActiveCard(token)
			}
		}(i)
	}

	// Concurrent reader
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 100 {
			_ = state.GetActiveCard()
			time.Sleep(time.Millisecond)
		}
	}()

	// Wait with timeout
	finished := make(chan struct{})
	go func() {
		wg.Wait()
		close(finished)
	}()

	select {
	case <-finished:
		// Success
	case <-time.After(10 * time.Second):
		t.Fatal("deadlock detected: SetActiveCard blocked while notification channel had backpressure")
	}
}

// TestSetActiveMedia_NoDeadlockWithHook is a regression test ensuring
// SetActiveMedia doesn't hold locks while calling the onMediaStartHook.
func TestSetActiveMedia_NoDeadlockWithHook(t *testing.T) {
	t.Parallel()

	state, notifications := NewState(nil, "test-boot-uuid")

	done := make(chan struct{})
	defer close(done)

	// Drain notifications
	go func() {
		for {
			select {
			case <-notifications:
			case <-done:
				return
			}
		}
	}()

	// Slow hook
	state.SetOnMediaStartHook(func(_ *models.ActiveMedia) {
		time.Sleep(5 * time.Millisecond)
	})

	var wg sync.WaitGroup

	// Multiple goroutines setting active media
	for i := range 3 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := range 10 {
				state.SetActiveMedia(&models.ActiveMedia{
					SystemID: "system-" + string(rune('0'+id)),
					Path:     "/path/" + string(rune('0'+j%10)),
				})
			}
		}(i)
	}

	// Concurrent reader
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 50 {
			_ = state.ActiveMedia()
		}
	}()

	finished := make(chan struct{})
	go func() {
		wg.Wait()
		close(finished)
	}()

	select {
	case <-finished:
		// Success
	case <-time.After(10 * time.Second):
		t.Fatal("deadlock detected: SetActiveMedia blocked while hook was executing")
	}
}

// TestConcurrentReaderOperations verifies reader operations don't deadlock.
func TestConcurrentReaderOperations(t *testing.T) {
	t.Parallel()

	state, notifications := NewState(nil, "test-boot-uuid")

	done := make(chan struct{})
	defer close(done)

	// Drain notifications
	go func() {
		for {
			select {
			case <-notifications:
			case <-done:
				return
			}
		}
	}()

	var wg sync.WaitGroup

	// Concurrent reader lookups
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				_, _ = state.GetReader("test:device")
				_ = state.ListReaders()
			}
		}()
	}

	finished := make(chan struct{})
	go func() {
		wg.Wait()
		close(finished)
	}()

	select {
	case <-finished:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("deadlock in concurrent reader operations")
	}
}

// TestSetActiveMedia_RaceCondition is a regression test for a race condition where
// oldMedia was read outside the lock. This could cause missed MediaStopped events
// when multiple goroutines set media concurrently.
//
// The fix was to read oldMedia inside the lock.
func TestSetActiveMedia_RaceCondition(t *testing.T) {
	t.Parallel()

	state, notifications := NewState(nil, "test-boot-uuid")

	done := make(chan struct{})
	defer close(done)

	// Count notifications
	var startedCount, stoppedCount atomic.Int32
	go func() {
		for {
			select {
			case n := <-notifications:
				switch n.Method {
				case models.NotificationStarted:
					startedCount.Add(1)
				case models.NotificationStopped:
					stoppedCount.Add(1)
				}
			case <-done:
				return
			}
		}
	}()

	// Rapid concurrent media changes - this would expose the race
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := range 20 {
				state.SetActiveMedia(&models.ActiveMedia{
					SystemID: "system",
					Path:     "/path/" + string(rune('A'+id)) + "/" + string(rune('0'+j%10)),
				})
			}
		}(i)
	}

	wg.Wait()
	time.Sleep(100 * time.Millisecond) // Let notifications drain

	// With the race condition fixed, we should have consistent state
	// The exact counts depend on timing, but we should have at least some events
	assert.Positive(t, startedCount.Load(), "should have received started notifications")
}

// mockPanicReader is a reader that panics when OnMediaChange is called after Close.
type mockPanicReader struct {
	closed atomic.Bool
}

func (*mockPanicReader) Metadata() readers.DriverMetadata {
	return readers.DriverMetadata{ID: "mock"}
}

func (*mockPanicReader) IDs() []string                                             { return []string{"mock"} }
func (*mockPanicReader) Open(_ config.ReadersConnect, _ chan<- readers.Scan) error { return nil }
func (m *mockPanicReader) Close() error                                            { m.closed.Store(true); return nil }
func (*mockPanicReader) Detect(_ []string) string                                  { return "" }
func (*mockPanicReader) Device() string                                            { return "mock:panic" }
func (m *mockPanicReader) Connected() bool                                         { return !m.closed.Load() }
func (*mockPanicReader) Info() string                                              { return "mock panic reader" }

//nolint:nilnil // mock implementation
func (*mockPanicReader) Write(_ string) (*tokens.Token, error) { return nil, nil }
func (*mockPanicReader) CancelWrite()                          {}
func (*mockPanicReader) Capabilities() []readers.Capability {
	return []readers.Capability{readers.CapabilityDisplay}
}

func (m *mockPanicReader) OnMediaChange(_ *models.ActiveMedia) error {
	if m.closed.Load() {
		panic("OnMediaChange called on closed reader")
	}
	return nil
}

// TestNotifyDisplayReaders_SkipsDisconnected verifies that disconnected readers
// are skipped via the Connected() fast path.
func TestNotifyDisplayReaders_SkipsDisconnected(t *testing.T) {
	t.Parallel()

	state, notifications := NewState(nil, "test-boot-uuid")

	done := make(chan struct{})
	defer close(done)

	// Drain notifications
	go func() {
		for {
			select {
			case <-notifications:
			case <-done:
				return
			}
		}
	}()

	// Add a mock reader that panics when called after close
	mockReader := &mockPanicReader{}
	state.SetReader("mock:panic", mockReader)

	// Close the reader - Connected() will now return false
	mockReader.closed.Store(true)

	// This should NOT panic - reader should be skipped via Connected() check
	assert.NotPanics(t, func() {
		state.notifyDisplayReaders(&models.ActiveMedia{
			SystemID: "test",
			Path:     "/test/path",
		})
	}, "notifyDisplayReaders should skip disconnected readers")
}

// mockRacyReader simulates a race condition where Connected() returns true
// but OnMediaChange panics (reader closes between the check and the call).
type mockRacyReader struct{}

func (*mockRacyReader) Metadata() readers.DriverMetadata {
	return readers.DriverMetadata{ID: "mock"}
}

func (*mockRacyReader) IDs() []string                                             { return []string{"mock"} }
func (*mockRacyReader) Open(_ config.ReadersConnect, _ chan<- readers.Scan) error { return nil }
func (*mockRacyReader) Close() error                                              { return nil }
func (*mockRacyReader) Detect(_ []string) string                                  { return "" }
func (*mockRacyReader) Device() string                                            { return "mock:racy" }
func (*mockRacyReader) Connected() bool                                           { return true } // Lies!
func (*mockRacyReader) Info() string                                              { return "mock racy reader" }

//nolint:nilnil // mock implementation
func (*mockRacyReader) Write(_ string) (*tokens.Token, error) { return nil, nil }
func (*mockRacyReader) CancelWrite()                          {}
func (*mockRacyReader) Capabilities() []readers.Capability {
	return []readers.Capability{readers.CapabilityDisplay}
}

func (*mockRacyReader) OnMediaChange(_ *models.ActiveMedia) error {
	panic("OnMediaChange called on closed reader") // Simulates race
}

// TestNotifyDisplayReaders_PanicRecovery is a regression test ensuring that
// panics in reader.OnMediaChange don't crash the service.
//
// This tests the race condition where Connected() returns true but the reader
// panics anyway (e.g., it closed between the check and the call).
func TestNotifyDisplayReaders_PanicRecovery(t *testing.T) {
	t.Parallel()

	state, notifications := NewState(nil, "test-boot-uuid")

	done := make(chan struct{})
	defer close(done)

	// Drain notifications
	go func() {
		for {
			select {
			case <-notifications:
			case <-done:
				return
			}
		}
	}()

	// Add a mock reader that lies about Connected() and panics
	state.SetReader("mock:racy", &mockRacyReader{})

	// This should NOT panic - the panic should be recovered
	assert.NotPanics(t, func() {
		state.notifyDisplayReaders(&models.ActiveMedia{
			SystemID: "test",
			Path:     "/test/path",
		})
	}, "notifyDisplayReaders should recover from panics in reader.OnMediaChange")
}
