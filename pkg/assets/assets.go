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
	"errors"
	"fmt"
	"sync"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
)

//go:embed _app
var App embed.FS

// SuccessSound by Tim Wilsie (timwilsie.com).
//
//go:embed sounds/success.ogg
var SuccessSound []byte

// FailSound by Tim Wilsie (timwilsie.com).
//
//go:embed sounds/fail.ogg
var FailSound []byte

// LimitSound by Tim Wilsie (timwilsie.com).
//
//go:embed sounds/limit.ogg
var LimitSound []byte

// PendingSound by Tim Wilsie (timwilsie.com).
//
//go:embed sounds/pending.ogg
var PendingSound []byte

// ReadySound by Tim Wilsie (timwilsie.com).
//
//go:embed sounds/ready.ogg
var ReadySound []byte

//go:embed systems/*
var Systems embed.FS

type SystemMetadata struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Category     string `json:"category"`
	ReleaseDate  string `json:"releaseDate"`
	Manufacturer string `json:"manufacturer"`
}

type systemMetadataLoad struct {
	err      error
	done     chan struct{}
	metadata *SystemMetadata
}

var (
	systemMetadataCache      sync.Map
	systemMetadataLoads      sync.Map
	readSystemMetadataFile   = Systems.ReadFile
	notifySystemMetadataLoad func()
)

func GetSystemMetadata(system string) (SystemMetadata, error) {
	// Resolve any aliases to the canonical system ID
	// This ensures backward compatibility when systems are renamed (e.g., Music → MusicTrack)
	resolvedSystem, err := systemdefs.LookupSystem(system)
	if err == nil && resolvedSystem != nil {
		system = resolvedSystem.ID
	}
	if cached, ok := systemMetadataCache.Load(system); ok {
		if cachedMetadata, valid := cached.(SystemMetadata); valid {
			return cachedMetadata, nil
		}
	}

	pending := &systemMetadataLoad{
		done:     make(chan struct{}),
		metadata: &SystemMetadata{},
	}
	actual, loaded := systemMetadataLoads.LoadOrStore(system, pending)
	if notifySystemMetadataLoad != nil {
		notifySystemMetadataLoad()
	}
	if loaded {
		shared, ok := actual.(*systemMetadataLoad)
		if !ok {
			return SystemMetadata{}, errors.New("invalid system metadata load state")
		}
		<-shared.done
		return *shared.metadata, shared.err
	}
	defer func() {
		close(pending.done)
		systemMetadataLoads.Delete(system)
	}()

	if cached, ok := systemMetadataCache.Load(system); ok {
		if cachedMetadata, valid := cached.(SystemMetadata); valid {
			*pending.metadata = cachedMetadata
			return *pending.metadata, nil
		}
	}

	data, readErr := readSystemMetadataFile("systems/" + system + ".json")
	if readErr != nil {
		pending.err = fmt.Errorf("failed to read system metadata file: %w", readErr)
		return *pending.metadata, pending.err
	}
	if unmarshalErr := json.Unmarshal(data, pending.metadata); unmarshalErr != nil {
		pending.err = fmt.Errorf("failed to unmarshal system metadata: %w", unmarshalErr)
		return *pending.metadata, pending.err
	}
	systemMetadataCache.Store(system, *pending.metadata)
	return *pending.metadata, nil
}
