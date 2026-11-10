//go:build linux && !android

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

// Package esbase is the shared platform for systems whose EmulationStation
// is Batocera's fork, which runs games and reports them through an HTTP API
// on localhost:1234.
package esbase

import (
	"context"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esapi"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/updatepayload"
)

// Options describes how one operating system differs from the others that
// run Batocera's EmulationStation.
type Options struct {
	// API reaches EmulationStation. Nil uses its HTTP API on localhost:1234.
	API API
	// ExcludedSystems are ES-DE folders this system's EmulationStation has
	// no system for. A game indexed there cannot be started: ES's launch API
	// does nothing for it, while Core reports the launch as done.
	ExcludedSystems map[string]struct{}
	// HasKodi reports whether Kodi is installed. Nil means it always is.
	// Without Kodi the Kodi launchers are left out.
	HasKodi func() bool
	// SystemAliases maps this system's EmulationStation system names and
	// ROM folder names to the ES-DE folder that describes the same system,
	// for names ES-DE spells differently.
	SystemAliases map[string]string
	// NonGameSystems are EmulationStation systems that hold tools, music
	// or screenshots rather than games. Their paths are not ROM roots.
	NonGameSystems []string
	// UpdatePayload lists the platform-owned files an update installs.
	UpdatePayload func() []updatepayload.File
	// ManagedByPackageManager reports whether a package manager owns this
	// install. Nil means it never does.
	ManagedByPackageManager func() bool
	// ExitGame asks the running game to exit before the stop falls back to
	// KillGame. Nil skips straight to KillGame.
	ExitGame func(ctx context.Context) error
	// KillGame is the fallback a stop repeats while EmulationStation still
	// reports a game. Nil uses EmulationStation's /emukill.
	KillGame func() error
	// Controls builds the controls of every EmulationStation system launcher
	// around stop. Nil offers only stop.
	Controls func(stop func() error) map[string]platforms.Control
	// ControlNames lists the controls Controls defines, for games the
	// tracker finds running. Nil lists only stop.
	ControlNames func() []string
	// ID is the platform ID.
	ID string
	// DataDir, ConfigDir and LogDir are hardcoded because the service may
	// start before HOME is set.
	DataDir   string
	ConfigDir string
	LogDir    string
	// DefaultROMRoot is always indexed, and relative ES system paths resolve
	// against it.
	DefaultROMRoot string
	// ESConfigDirs hold es_systems.cfg and es_systems_*.cfg overlays, read
	// in order so a later directory overrides an earlier one.
	ESConfigDirs []string
	// KillRetryDelay is how long a stop waits between KillGame attempts.
	KillRetryDelay time.Duration
	// OnlyESSystems leaves out ES-DE folders that no system in the parsed
	// ES config uses, for systems whose ES system list varies by device.
	// It has no effect when no ES config could be read.
	OnlyESSystems bool
}

// API is the part of EmulationStation's HTTP API the platform uses.
type API interface {
	RunningGame() (esapi.RunningGameResponse, bool, error)
	EmuKill() error
	Launch(path string) error
	Notify(msg string) error
}

type httpAPI struct{}

func (httpAPI) RunningGame() (esapi.RunningGameResponse, bool, error) {
	return esapi.APIRunningGame() //nolint:wrapcheck // passthrough to the ES API client
}

func (httpAPI) EmuKill() error {
	return esapi.APIEmuKill() //nolint:wrapcheck // passthrough to the ES API client
}

func (httpAPI) Launch(path string) error {
	return esapi.APILaunch(path) //nolint:wrapcheck // passthrough to the ES API client
}

func (httpAPI) Notify(msg string) error {
	return esapi.APINotify(msg) //nolint:wrapcheck // passthrough to the ES API client
}
