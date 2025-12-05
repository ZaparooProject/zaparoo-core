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
	"context"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
)

// LauncherManager manages the lifecycle of launcher contexts across the application.
// It provides thread-safe access to a shared context that gets canceled whenever
// a new launcher starts, allowing previous launcher cleanup routines to detect
// when they've been superseded and should skip their cleanup actions.
type LauncherManager struct {
	ctx    context.Context
	cancel context.CancelFunc
	mu     syncutil.RWMutex
}

func NewLauncherManager() *LauncherManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &LauncherManager{
		ctx:    ctx,
		cancel: cancel,
	}
}

// GetContext returns the current launcher context.
// This context will be canceled when a new launcher starts.
func (lm *LauncherManager) GetContext() context.Context {
	lm.mu.RLock()
	defer lm.mu.RUnlock()
	return lm.ctx
}

// NewContext cancels the current launcher context and creates a new one.
// This should be called when starting a new launcher to invalidate
// any cleanup routines from previous launchers.
// Returns the newly created context.
func (lm *LauncherManager) NewContext() context.Context {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	if lm.cancel != nil {
		lm.cancel()
	}

	lm.ctx, lm.cancel = context.WithCancel(context.Background())
	return lm.ctx
}
