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
	"errors"
	"fmt"
	"net"
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	uievents "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/events"
)

// EmbeddedOptions supplies process-owned resources without CLI, signal, crash-output
// or executable-update ownership. Callbacks must return promptly and not panic.
// Only one runtime may run in a process; await Done before starting another.
// Listener ownership transfers on entry, including validation and startup failures.
// Private listeners are not advertised through mDNS.
type EmbeddedOptions struct {
	Context  context.Context
	Listener net.Listener
	Audio    audio.Player
	APIKeys  func() []string
	Renderer uievents.Renderer
	OnPhase  func(string)
	OnFatal  func(error)
}

// StartEmbedded initializes the actual service and databases using host-owned
// resources. Platform settings must supply absolute directories and disable
// executable updates. An error never means that a partially initialized runtime
// can be used. StopContext bounds the caller's wait, not the lifetime of native
// work: after a timeout the host must still await Done or terminate its process.
//
//nolint:gocritic // Snapshot host options for asynchronous lifecycle callbacks.
func StartEmbedded(pl platforms.Platform, cfg *config.Instance, opts EmbeddedOptions) (result *StartResult, err error) {
	defer func() {
		if err != nil {
			if opts.Listener != nil {
				_ = opts.Listener.Close()
			}
			if opts.OnFatal != nil {
				opts.OnFatal(err)
			}
		}
	}()
	if pl == nil || cfg == nil || opts.Context == nil || opts.Listener == nil || opts.Audio == nil {
		return nil, errors.New("embedded startup requires platform, config, context, listener and audio")
	}
	settings := pl.Settings()
	if !settings.HostManagedPaths || !settings.DisableSelfUpdate {
		return nil, errors.New("embedded startup requires host-managed paths and disabled self-updates")
	}
	for name, path := range map[string]string{
		"data": settings.DataDir, "config": settings.ConfigDir,
		"cache": settings.TempDir, "log": settings.LogDir,
	} {
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("embedded %s directory must be absolute", name)
		}
	}
	if contextErr := opts.Context.Err(); contextErr != nil {
		return nil, contextErr
	}
	opts.phase("starting")
	result, err = startServiceWithOptions(pl, cfg, &opts)
	if err != nil {
		return nil, err
	}
	opts.phase("ready")
	go func(runtime *StartResult) {
		<-runtime.Done
		if terminalErr := runtime.Err(); terminalErr != nil && opts.OnFatal != nil {
			opts.OnFatal(terminalErr)
		}
		opts.phase("stopped")
	}(result)
	return result, nil
}

func (opts *EmbeddedOptions) phase(phase string) {
	if opts != nil && opts.OnPhase != nil {
		opts.OnPhase(phase)
	}
}
