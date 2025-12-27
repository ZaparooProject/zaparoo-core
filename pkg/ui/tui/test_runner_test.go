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

package tui

import (
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rivo/tview"
)

// TestAppRunner manages running tview apps in tests with SimulationScreen.
// It handles the complexity of running the app in a goroutine and provides
// methods for injecting events and verifying output.
type TestAppRunner struct {
	runErr  error
	app     *tview.Application
	screen  *TestScreen
	t       *testing.T
	stopMu  syncutil.Mutex
	stopped bool
}

// NewTestAppRunner creates a test runner with a simulation screen.
func NewTestAppRunner(t *testing.T, width, height int) *TestAppRunner {
	t.Helper()

	screen := NewTestScreen(t, width, height)
	app := tview.NewApplication()
	app.SetScreen(screen.SimulationScreen)

	return &TestAppRunner{
		app:    app,
		screen: screen,
		t:      t,
	}
}

// Start runs the app in a goroutine with the given root primitive.
func (r *TestAppRunner) Start(root tview.Primitive) {
	r.app.SetRoot(root, true)
	go func() {
		r.runErr = r.app.Run()
		r.stopMu.Lock()
		r.stopped = true
		r.stopMu.Unlock()
	}()
	// Brief pause to let app initialize
	time.Sleep(20 * time.Millisecond)
}

// Stop stops the application and cleans up.
// Note: tview.Application.Stop() internally calls screen.Fini(), so we don't
// call Cleanup() here to avoid double-close panics.
func (r *TestAppRunner) Stop() {
	r.stopMu.Lock()
	alreadyStopped := r.stopped
	if !alreadyStopped {
		r.stopped = true
	}
	r.stopMu.Unlock()

	if !alreadyStopped {
		r.app.Stop()
		time.Sleep(20 * time.Millisecond)
	}
}

// Screen returns the test screen for event injection and assertions.
func (r *TestAppRunner) Screen() *TestScreen {
	return r.screen
}

// App returns the tview application.
func (r *TestAppRunner) App() *tview.Application {
	return r.app
}

// Draw forces a synchronous draw and waits for it to complete.
func (r *TestAppRunner) Draw() {
	r.app.Draw()
	time.Sleep(10 * time.Millisecond)
}

// QueueUpdateDraw queues a UI update and waits for the draw to complete.
func (r *TestAppRunner) QueueUpdateDraw(f func()) {
	r.app.QueueUpdateDraw(f)
	time.Sleep(10 * time.Millisecond)
}

// SetFocus sets focus on a primitive and waits for the draw.
func (r *TestAppRunner) SetFocus(p tview.Primitive) {
	r.app.SetFocus(p)
	r.Draw()
}

// IsStopped returns whether the application has stopped.
func (r *TestAppRunner) IsStopped() bool {
	r.stopMu.Lock()
	defer r.stopMu.Unlock()
	return r.stopped
}

// RunError returns any error from the app's Run method.
func (r *TestAppRunner) RunError() error {
	return r.runErr
}

// WaitForCondition waits for a condition to be true, with a timeout.
func (*TestAppRunner) WaitForCondition(condition func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// WaitForText waits for specific text to appear on screen.
func (r *TestAppRunner) WaitForText(text string, timeout time.Duration) bool {
	return r.WaitForCondition(func() bool {
		r.Draw()
		return r.screen.ContainsText(text)
	}, timeout)
}
