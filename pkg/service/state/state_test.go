// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
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
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
)

func TestSetOnMediaStartHook(t *testing.T) {
	t.Parallel()
	mockPlatform := mocks.NewMockPlatform()
	state, _ := NewState(mockPlatform, "test-boot-uuid")

	// Test hook gets called when media starts from nil
	var hookCalled bool
	var hookMedia *models.ActiveMedia
	var wg sync.WaitGroup
	wg.Add(1)

	state.SetOnMediaStartHook(func(media *models.ActiveMedia) {
		hookCalled = true
		hookMedia = media
		wg.Done()
	})

	// Set active media (from nil to something)
	testMedia := &models.ActiveMedia{
		LauncherID: "test-launcher",
		SystemID:   "test-system",
		SystemName: "Test System",
		Name:       "Test Game",
		Path:       "/test/path",
	}

	state.SetActiveMedia(testMedia)

	// Wait for async hook execution
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("Hook was not called within timeout")
	}

	if !hookCalled {
		t.Error("OnMediaStart hook was not called")
	}

	if hookMedia != testMedia {
		t.Error("Hook was not called with the correct media")
	}
}

func TestSetOnMediaStartHookMediaChange(t *testing.T) {
	t.Parallel()
	mockPlatform := mocks.NewMockPlatform()
	state, _ := NewState(mockPlatform, "test-boot-uuid")

	// Set initial media
	initialMedia := &models.ActiveMedia{
		LauncherID: "initial-launcher",
		SystemID:   "initial-system",
		Name:       "Initial Game",
	}
	state.SetActiveMedia(initialMedia)

	// Test hook gets called when media changes
	var hookCalled bool
	var wg sync.WaitGroup
	wg.Add(1)

	state.SetOnMediaStartHook(func(_ *models.ActiveMedia) {
		hookCalled = true
		wg.Done()
	})

	// Change to different media
	newMedia := &models.ActiveMedia{
		LauncherID: "new-launcher",
		SystemID:   "new-system",
		Name:       "New Game",
	}

	state.SetActiveMedia(newMedia)

	// Wait for async hook execution
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("Hook was not called within timeout")
	}

	if !hookCalled {
		t.Error("OnMediaStart hook was not called when media changed")
	}
}

func TestSetOnMediaStartHookNotCalledOnStop(t *testing.T) {
	t.Parallel()
	mockPlatform := mocks.NewMockPlatform()
	state, _ := NewState(mockPlatform, "test-boot-uuid")

	// Set initial media
	initialMedia := &models.ActiveMedia{
		LauncherID: "initial-launcher",
		SystemID:   "initial-system",
		Name:       "Initial Game",
	}
	state.SetActiveMedia(initialMedia)

	// Set up hook
	var hookCalled bool
	state.SetOnMediaStartHook(func(_ *models.ActiveMedia) {
		hookCalled = true
	})

	// Stop media (set to nil)
	state.SetActiveMedia(nil)

	// Give some time for any potential async execution
	time.Sleep(100 * time.Millisecond)

	if hookCalled {
		t.Error("OnMediaStart hook should not be called when media stops")
	}
}
