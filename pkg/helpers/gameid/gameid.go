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

// Package gameid wraps go-gameid disc identification for the two things that
// need it: the indexer (identify a disc image to store as a media property)
// and the optical drive reader (identify an inserted disc live). It has no
// knowledge of how its result is stored or matched — that's a plain media
// property to everything downstream.
package gameid

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/go-gameid"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
)

// Candidate is a system/ID pair identified from a disc image or live disc.
type Candidate struct {
	SystemID string
	ID       string
}

var systemConsoles = map[string]gameid.Console{
	systemdefs.SystemGameCube: gameid.ConsoleGC,
	systemdefs.SystemMegaCD:   gameid.ConsoleSegaCD,
	systemdefs.SystemNeoGeoCD: gameid.ConsoleNeoGeoCD,
	systemdefs.SystemPS2:      gameid.ConsolePS2,
	systemdefs.SystemPSP:      gameid.ConsolePSP,
	systemdefs.SystemPSX:      gameid.ConsolePSX,
	systemdefs.SystemSaturn:   gameid.ConsoleSaturn,
}

var consoleSystems = map[gameid.Console]string{
	gameid.ConsoleGC:       systemdefs.SystemGameCube,
	gameid.ConsoleSegaCD:   systemdefs.SystemMegaCD,
	gameid.ConsoleNeoGeoCD: systemdefs.SystemNeoGeoCD,
	gameid.ConsolePS2:      systemdefs.SystemPS2,
	gameid.ConsolePSP:      systemdefs.SystemPSP,
	gameid.ConsolePSX:      systemdefs.SystemPSX,
	gameid.ConsoleSaturn:   systemdefs.SystemSaturn,
}

// preferredDiscExtensions lists disc-image extensions go-gameid can read. .cso,
// .gcz, and .rvz are compressed containers with unsupported header formats, so
// identification always fails for them; excluding them avoids repeated failures.
var preferredDiscExtensions = map[string]struct{}{
	".chd": {},
	".cue": {},
	".gcm": {},
	".iso": {},
}

func ConsoleForSystem(systemID string) (gameid.Console, bool) {
	if console, ok := systemConsoles[systemID]; ok {
		return console, true
	}

	def, err := systemdefs.LookupSystem(systemID)
	if err != nil {
		return "", false
	}
	console, ok := systemConsoles[def.ID]
	return console, ok
}

func ShouldIndexPath(path string) bool {
	_, ok := preferredDiscExtensions[strings.ToLower(filepath.Ext(path))]
	return ok
}

// IsCandidate reports whether path/systemID is eligible for gameid
// identification: a matching disc-image extension on a system with a known
// console mapping. Callers can use this to skip more expensive gameid-specific
// work for systems/extensions that will never match.
func IsCandidate(path, systemID string) bool {
	if !ShouldIndexPath(path) {
		return false
	}
	_, ok := ConsoleForSystem(systemID)
	return ok
}

func safeCall[T any](fn func() (T, error), panicErr func(any) error) (value T, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			var zero T
			value = zero
			err = panicErr(recovered)
		}
	}()

	return fn()
}

func IdentifyPathForSystem(path, systemID string) (string, error) {
	return safeCall(func() (string, error) {
		if !IsCandidate(path, systemID) {
			return "", nil
		}

		console, _ := ConsoleForSystem(systemID)

		result, err := gameid.IdentifyWithConsole(path, console, nil)
		if err != nil {
			return "", fmt.Errorf("identify %s as %s: %w", path, systemID, err)
		}

		return NormalizeID(result.ID), nil
	}, func(recovered any) error {
		return fmt.Errorf("identify %s as %s panicked: %v", path, systemID, recovered)
	})
}

func identifyLiveDiscForSystem(path, systemID string, console gameid.Console) (Candidate, bool) {
	candidate, err := safeCall(func() (Candidate, error) {
		result, identifyErr := gameid.IdentifyWithConsole(path, console, nil)
		if identifyErr != nil {
			return Candidate{}, fmt.Errorf("identify live disc: %w", identifyErr)
		}
		return Candidate{SystemID: systemID, ID: NormalizeID(result.ID)}, nil
	}, func(recovered any) error {
		return fmt.Errorf("identify live disc panicked: %v", recovered)
	})
	return candidate, err == nil && candidate.ID != ""
}

func IdentifyLiveDisc(path string) []Candidate {
	console, ok := detectLiveConsole(path)
	if !ok {
		return nil
	}

	systemID, ok := consoleSystems[console]
	if !ok {
		return nil
	}

	candidate, ok := identifyLiveDiscForSystem(path, systemID, console)
	if !ok {
		return nil
	}
	return []Candidate{candidate}
}

func detectLiveConsole(path string) (gameid.Console, bool) {
	console, err := safeCall(func() (gameid.Console, error) {
		return gameid.DetectConsole(path)
	}, func(recovered any) error {
		return fmt.Errorf("detect live disc console panicked: %v", recovered)
	})
	return console, err == nil
}

func NormalizeID(id string) string {
	id = strings.TrimSpace(id)
	id = strings.ReplaceAll(id, "_", "-")
	return strings.ToUpper(id)
}
