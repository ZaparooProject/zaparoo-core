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

package tui

import (
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/gdamore/tcell/v2"
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

// Draw forces a synchronous draw.
func (r *TestAppRunner) Draw() {
	r.app.Draw()
}

// QueueUpdateDraw queues a UI update and waits for it to complete.
func (r *TestAppRunner) QueueUpdateDraw(f func()) {
	done := make(chan struct{})
	r.app.QueueUpdateDraw(func() {
		f()
		close(done)
	})
	<-done
}

// SetFocus sets focus on a primitive and waits for the draw.
func (r *TestAppRunner) SetFocus(p tview.Primitive) {
	r.app.SetFocus(p)
	r.Draw()
}

// SimulateKey simulates a key press by calling the focused primitive's
// InputHandler directly. This is synchronous and race-free.
func (r *TestAppRunner) SimulateKey(key tcell.Key, ch rune, mod tcell.ModMask) {
	r.QueueUpdateDraw(func() {
		focused := r.app.GetFocus()
		if focused == nil {
			return
		}
		handler := focused.InputHandler()
		if handler == nil {
			return
		}
		event := tcell.NewEventKey(key, ch, mod)
		handler(event, func(p tview.Primitive) { r.app.SetFocus(p) })
	})
}

// SimulateRune simulates typing a character.
func (r *TestAppRunner) SimulateRune(ch rune) {
	r.SimulateKey(tcell.KeyRune, ch, tcell.ModNone)
}

// SimulateString simulates typing a string of characters.
func (r *TestAppRunner) SimulateString(str string) {
	for _, ch := range str {
		r.SimulateRune(ch)
	}
}

// SimulateEnter simulates pressing Enter.
func (r *TestAppRunner) SimulateEnter() {
	r.SimulateKey(tcell.KeyEnter, 0, tcell.ModNone)
}

// SimulateEscape simulates pressing Escape.
func (r *TestAppRunner) SimulateEscape() {
	r.SimulateKey(tcell.KeyEscape, 0, tcell.ModNone)
}

// SimulateTab simulates pressing Tab.
func (r *TestAppRunner) SimulateTab() {
	r.SimulateKey(tcell.KeyTab, 0, tcell.ModNone)
}

// SimulateBacktab simulates pressing Shift+Tab.
func (r *TestAppRunner) SimulateBacktab() {
	r.SimulateKey(tcell.KeyBacktab, 0, tcell.ModNone)
}

// SimulateArrowUp simulates pressing the Up arrow.
func (r *TestAppRunner) SimulateArrowUp() {
	r.SimulateKey(tcell.KeyUp, 0, tcell.ModNone)
}

// SimulateArrowDown simulates pressing the Down arrow.
func (r *TestAppRunner) SimulateArrowDown() {
	r.SimulateKey(tcell.KeyDown, 0, tcell.ModNone)
}

// SimulateArrowLeft simulates pressing the Left arrow.
func (r *TestAppRunner) SimulateArrowLeft() {
	r.SimulateKey(tcell.KeyLeft, 0, tcell.ModNone)
}

// SimulateArrowRight simulates pressing the Right arrow.
func (r *TestAppRunner) SimulateArrowRight() {
	r.SimulateKey(tcell.KeyRight, 0, tcell.ModNone)
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
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

// WaitForSignal waits for a channel to be closed or receive a value.
func (*TestAppRunner) WaitForSignal(ch <-chan struct{}, timeout time.Duration) bool {
	select {
	case <-ch:
		return true
	case <-time.After(timeout):
		return false
	}
}

// WaitForText waits for specific text to appear on screen.
func (r *TestAppRunner) WaitForText(text string, timeout time.Duration) bool {
	return r.WaitForCondition(func() bool {
		return r.ContainsText(text)
	}, timeout)
}

// ContainsText checks if the screen contains the specified text.
// This method is synchronized with the app's draw cycle to avoid races.
func (r *TestAppRunner) ContainsText(text string) bool {
	var result bool
	done := make(chan struct{})
	r.app.QueueUpdate(func() {
		result = r.screen.ContainsText(text)
		close(done)
	})
	<-done
	return result
}

// GetScreenText returns all screen content as a single string.
// This method is synchronized with the app's draw cycle to avoid races.
func (r *TestAppRunner) GetScreenText() string {
	var result string
	done := make(chan struct{})
	r.app.QueueUpdate(func() {
		result = r.screen.GetScreenText()
		close(done)
	})
	<-done
	return result
}
