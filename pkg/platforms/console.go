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

package platforms

import "context"

// ConsoleManager handles platform-specific console/TTY switching operations.
// This is primarily used by MiSTer for video playback and script execution.
type ConsoleManager interface {
	// Open switches to console mode on the specified VT.
	// The provided context can be used to cancel the operation if the launcher is superseded.
	Open(ctx context.Context, vt string) error

	// Close exits console mode and returns to normal display
	Close() error

	// Clean prepares a console for use (clears screen, hides cursor)
	Clean(vt string) error

	// Restore restores console cursor state
	Restore(vt string) error
}

// NoOpConsoleManager is a console manager that does nothing.
// Used by platforms that don't have console switching (MiSTeX, etc).
type NoOpConsoleManager struct{}

func (NoOpConsoleManager) Open(_ context.Context, _ string) error { return nil }
func (NoOpConsoleManager) Close() error                           { return nil }
func (NoOpConsoleManager) Clean(_ string) error                   { return nil }
func (NoOpConsoleManager) Restore(_ string) error                 { return nil }
