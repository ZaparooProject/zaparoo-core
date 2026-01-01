//go:build linux

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

package mister

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMiSTerConsoleManager_Open_CancelledContext(t *testing.T) {
	t.Parallel()

	pl := &Platform{}
	cm := newConsoleManager(pl)

	// Test with already cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := cm.Open(ctx, "7")
	require.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}

func TestMiSTerConsoleManager_ConcurrentActiveFlag(t *testing.T) {
	t.Parallel()

	pl := &Platform{}
	cm := newConsoleManager(pl)
	done := make(chan bool)

	// Concurrent readers
	for range 10 {
		go func() {
			defer func() { done <- true }()
			for range 100 {
				cm.mu.RLock()
				_ = cm.active
				cm.mu.RUnlock()
			}
		}()
	}

	// Concurrent writers
	for i := range 5 {
		go func(val bool) {
			defer func() { done <- true }()
			for range 50 {
				cm.mu.Lock()
				cm.active = val
				cm.mu.Unlock()
			}
		}(i%2 == 0)
	}

	// Wait for all goroutines
	for range 15 {
		<-done
	}
}
