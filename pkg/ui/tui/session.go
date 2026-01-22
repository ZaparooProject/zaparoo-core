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

import "github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"

// Session holds TUI session state that persists across page navigations.
// It is thread-safe and can be created per-test for parallel test execution.
type Session struct {
	writeTagZapScript     string
	searchMediaName       string
	searchMediaSystem     string
	searchMediaSystemName string
	mainMenuRow           int
	mainMenuCol           int
	mu                    syncutil.RWMutex
}

// NewSession creates a new TUI session with default values.
func NewSession() *Session {
	return &Session{
		searchMediaSystemName: "All",
	}
}

// GetWriteTagZapScript returns the current ZapScript for the write tag form.
func (s *Session) GetWriteTagZapScript() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.writeTagZapScript
}

// SetWriteTagZapScript sets the ZapScript for the write tag form.
func (s *Session) SetWriteTagZapScript(v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writeTagZapScript = v
}

// GetMainMenuFocus returns the current main menu focus position.
func (s *Session) GetMainMenuFocus() (row, col int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mainMenuRow, s.mainMenuCol
}

// SetMainMenuFocus sets the main menu focus position.
func (s *Session) SetMainMenuFocus(row, col int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mainMenuRow = row
	s.mainMenuCol = col
}

// GetSearchMediaName returns the search media name query.
func (s *Session) GetSearchMediaName() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.searchMediaName
}

// SetSearchMediaName sets the search media name query.
func (s *Session) SetSearchMediaName(v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.searchMediaName = v
}

// GetSearchMediaSystem returns the search media system filter.
func (s *Session) GetSearchMediaSystem() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.searchMediaSystem
}

// SetSearchMediaSystem sets the search media system filter.
func (s *Session) SetSearchMediaSystem(v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.searchMediaSystem = v
}

// GetSearchMediaSystemName returns the search media system display name.
func (s *Session) GetSearchMediaSystemName() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.searchMediaSystemName
}

// SetSearchMediaSystemName sets the search media system display name.
func (s *Session) SetSearchMediaSystemName(v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.searchMediaSystemName = v
}

// ClearSearchMedia resets all search media state to defaults.
func (s *Session) ClearSearchMedia() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.searchMediaName = ""
	s.searchMediaSystem = ""
	s.searchMediaSystemName = "All"
}

// defaultSession is the package-level session used in production.
var defaultSession = NewSession()

// DefaultSession returns the default session instance for production use.
func DefaultSession() *Session {
	return defaultSession
}
