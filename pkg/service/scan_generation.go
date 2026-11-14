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

package service

import (
	"context"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	zscript "github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
)

// scanGeneration is the lifetime of the scripts started by queued tokens.
// A token that launches media or works a playlist starts a new generation,
// which cuts short the delays still pending in scripts that came before it,
// so a delayed command from an earlier tap cannot act on what the latest one
// started.
type scanGeneration struct {
	ctx    context.Context
	cancel context.CancelFunc
	mu     syncutil.Mutex
}

// current returns the live generation, starting one under parent if there is
// none yet.
func (g *scanGeneration) current(parent context.Context) context.Context {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.ctx == nil {
		g.ctx, g.cancel = context.WithCancel(parent)
	}
	return g.ctx
}

// supersede ends the live generation and returns a new one under parent.
func (g *scanGeneration) supersede(parent context.Context) context.Context {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.cancel != nil {
		g.cancel()
	}
	g.ctx, g.cancel = context.WithCancel(parent)
	return g.ctx
}

// scriptSupersedesEarlierScans reports whether a scanned script replaces what
// earlier scans set going: it launches media or works a playlist. A script
// that only runs commands, such as a volume change, leaves them alone.
func scriptSupersedesEarlierScans(script *zapscript.Script) bool {
	if script == nil {
		return false
	}
	for _, cmd := range script.Cmds {
		if zscript.IsMediaLaunchingCommand(cmd.Name) || zscript.IsPlaylistCommand(cmd.Name) {
			return true
		}
	}
	return false
}

type scanCtxKey struct{}

// withScanContext carries a token's scan generation on the context its script
// runs under.
func withScanContext(runCtx, scanCtx context.Context) context.Context {
	return context.WithValue(runCtx, scanCtxKey{}, scanCtx)
}

// scanContextFrom returns the scan generation a script runs in, nil when it
// did not come from a queued token.
func scanContextFrom(ctx context.Context) context.Context {
	if scanCtx, ok := ctx.Value(scanCtxKey{}).(context.Context); ok {
		return scanCtx
	}
	return nil
}
