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

package assets

import (
	"embed"
	"encoding/json"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
)

//go:embed _app
var App embed.FS

// SuccessSound Breviceps (https://freesound.org/people/Breviceps/sounds/445978/)
// Licence: CC0 1.0 Universal (CC0 1.0) Public Domain Dedication
//
//go:embed sounds/success.wav
var SuccessSound []byte

// FailSound PaulMorek (https://freesound.org/people/PaulMorek/sounds/330046/)
// Licence: CC0 1.0 Universal (CC0 1.0) Public Domain Dedication
//
//go:embed sounds/fail.wav
var FailSound []byte

// LimitSound CogFireStudios (https://freesound.org/people/CogFireStudios/sounds/636679/)
// Licence: CC0 1.0 Universal (CC0 1.0) Public Domain Dedication
//
//go:embed sounds/limit.wav
var LimitSound []byte

//go:embed systems/*
var Systems embed.FS

type SystemMetadata struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Category     string `json:"category"`
	ReleaseDate  string `json:"releaseDate"`
	Manufacturer string `json:"manufacturer"`
}

func GetSystemMetadata(system string) (SystemMetadata, error) {
	var metadata SystemMetadata

	// Resolve any aliases to the canonical system ID
	// This ensures backward compatibility when systems are renamed (e.g., Music → MusicTrack)
	resolvedSystem, err := systemdefs.LookupSystem(system)
	if err == nil && resolvedSystem != nil {
		system = resolvedSystem.ID
	}

	data, err := Systems.ReadFile("systems/" + system + ".json")
	if err != nil {
		return metadata, fmt.Errorf("failed to read system metadata file: %w", err)
	}

	err = json.Unmarshal(data, &metadata)
	if err != nil {
		return metadata, fmt.Errorf("failed to unmarshal system metadata: %w", err)
	}
	return metadata, nil
}
