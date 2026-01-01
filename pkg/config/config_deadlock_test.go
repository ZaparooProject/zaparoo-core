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

package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestAPIListen_NoRecursiveLock is a regression test for a recursive RLock bug.
// APIListen() previously called APIPort() while holding RLock, causing a deadlock
// with go-deadlock enabled. The fix was to inline the port logic.
//
// With -tags=deadlock, go-deadlock will panic on recursive locks, failing this test
// if the bug regresses.
func TestAPIListen_NoRecursiveLock(t *testing.T) {
	t.Parallel()

	cfg := &Instance{}

	// This would deadlock (or panic with go-deadlock) if APIListen
	// tried to call APIPort while holding RLock
	done := make(chan struct{})
	go func() {
		_ = cfg.APIListen()
		close(done)
	}()

	select {
	case <-done:
		// Success - no deadlock
	case <-time.After(2 * time.Second):
		t.Fatal("APIListen() deadlocked - recursive RLock bug has regressed")
	}
}

// TestAPIPort_ConcurrentAccess verifies APIPort is safe for concurrent access.
func TestAPIPort_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	cfg := &Instance{}

	done := make(chan struct{})
	for range 10 {
		go func() {
			for range 100 {
				_ = cfg.APIPort()
				_ = cfg.APIListen()
			}
			done <- struct{}{}
		}()
	}

	for range 10 {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("concurrent access deadlocked")
		}
	}
}

// TestAPIListen_DefaultPort verifies APIListen returns correct default when port is 0.
func TestAPIListen_DefaultPort(t *testing.T) {
	t.Parallel()

	cfg := &Instance{}
	assert.Equal(t, ":7497", cfg.APIListen())
}

// TestAPIListen_CustomPort verifies APIListen uses custom port when set.
func TestAPIListen_CustomPort(t *testing.T) {
	t.Parallel()

	cfg := &Instance{}
	port := 8080
	cfg.vals.Service.APIPort = &port
	assert.Equal(t, ":8080", cfg.APIListen())
}

// TestAPIListen_CustomListen verifies APIListen combines host with port.
func TestAPIListen_CustomListen(t *testing.T) {
	t.Parallel()

	cfg := &Instance{}
	cfg.vals.Service.APIListen = "127.0.0.1"
	port := 9000
	cfg.vals.Service.APIPort = &port
	assert.Equal(t, "127.0.0.1:9000", cfg.APIListen())
}
