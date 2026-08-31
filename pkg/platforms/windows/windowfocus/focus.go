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

// Package windowfocus activates windows created by launched Windows processes.
package windowfocus

import (
	"context"
	"errors"
	"time"
)

var errFocusTimeout = errors.New("timed out waiting for process window")

type processRelation struct {
	pid       uint32
	parentPID uint32
}

type nativeAPI interface {
	allowProcessForeground(pid uint32)
	findProcessWindow(pid uint32) (uintptr, bool)
	activateWindow(hwnd uintptr) bool
}

func processTree(rootPID uint32, relations []processRelation) map[uint32]struct{} {
	pids := map[uint32]struct{}{rootPID: {}}
	for changed := true; changed; {
		changed = false
		for _, relation := range relations {
			if _, known := pids[relation.pid]; known {
				continue
			}
			if _, parentKnown := pids[relation.parentPID]; parentKnown {
				pids[relation.pid] = struct{}{}
				changed = true
			}
		}
	}
	return pids
}

// Manager waits for a launched process to create a top-level window and then
// asks Windows to activate it.
type Manager struct {
	api          nativeAPI
	pollInterval time.Duration
	timeout      time.Duration
}

func newManager(api nativeAPI, pollInterval, timeout time.Duration) *Manager {
	return &Manager{
		api:          api,
		pollInterval: pollInterval,
		timeout:      timeout,
	}
}

// Focus polls until the process owns a window that Windows accepts as foreground.
func (m *Manager) Focus(ctx context.Context, pid uint32) error {
	if m == nil || m.api == nil || pid == 0 {
		return nil
	}

	m.api.allowProcessForeground(pid)

	ticker := time.NewTicker(m.pollInterval)
	defer ticker.Stop()
	timer := time.NewTimer(m.timeout)
	defer timer.Stop()

	for {
		if hwnd, found := m.api.findProcessWindow(pid); found && m.api.activateWindow(hwnd) {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return errFocusTimeout
		case <-ticker.C:
		}
	}
}
